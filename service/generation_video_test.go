package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func TestGenerationVideoPolling(t *testing.T) {
	if runGenerationTestIsolated(t, "TestGenerationVideoPolling") {
		return
	}
	db := generationTestDB(t)
	now := generationNow()
	video := func(id string) model.GenerationJob {
		job := queuedTestJob(id, "u1", now)
		job.Kind, job.Endpoint, job.Model = model.GenerationKindVideo, "/videos", "sora-2"
		return job
	}
	jobs := []model.GenerationJob{video("v1"), video("v2"), video("v3")}
	if err := db.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}

	claimed := claimTestJob(t, "v1", now)
	if ok, err := settleGenerationJob(claimed, GenerationJobResult{UpstreamTaskID: "client_video_task_v1"}, nil, now); err != nil || !ok {
		t.Fatalf("settle submit: %v %v", ok, err)
	}
	job := loadTestJob(t, db, "v1")
	if job.Status != model.GenerationStatusPolling || job.UpstreamTaskID != "client_video_task_v1" || job.LeaseToken != "" || job.LeaseExpiresAt != nil ||
		!job.NextRunAt.Equal(now.Add(generationPollInterval)) || job.PollDeadline == nil || !job.PollDeadline.Equal(now.Add(generationPollTimeout)) {
		t.Fatalf("submitted job must release its lease and wait for polls: %#v", job)
	}
	if counts, _ := repository.RunningGenerationJobCounts(); counts["u1"] != 0 {
		t.Fatalf("a polling job must not hold a concurrency slot: %v", counts)
	}

	t.Run("a submitted job is never claimed for submit again", func(t *testing.T) {
		if err := repository.RecoverExpiredGenerationJobs(now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		later, err := repository.ClaimGenerationJob(now.Add(time.Hour), "lease-x", now.Add(2*time.Hour), repository.GenerationClaimExclusion{}, func(candidate model.GenerationJob) bool { return candidate.ID == "v1" })
		if err != nil || later != nil {
			t.Fatalf("polling job claimed for execution: %#v %v", later, err)
		}
	})

	t.Run("polls survive a restart and keep going until done", func(t *testing.T) {
		if due, err := repository.ClaimPollingGenerationJobs(now, now.Add(generationPollLease), 10); err != nil || len(due) != 0 {
			t.Fatalf("not due yet: %v %v", due, err)
		}
		at := now.Add(6 * time.Second)
		due, err := repository.ClaimPollingGenerationJobs(at, at.Add(generationPollLease), 10)
		if err != nil || len(due) != 1 || due[0].ID != "v1" {
			t.Fatalf("due polls: %v %v", due, err)
		}
		if again, _ := repository.ClaimPollingGenerationJobs(at, at.Add(generationPollLease), 10); len(again) != 0 {
			t.Fatal("a claimed poll was handed out twice")
		}
		// The process dies mid-poll: after the poll lease the job is due again, read from the database alone.
		restart := at.Add(generationPollLease)
		due, err = repository.ClaimPollingGenerationJobs(restart, restart.Add(generationPollLease), 10)
		if err != nil || len(due) != 1 {
			t.Fatalf("poll after restart: %v %v", due, err)
		}
		if ok, err := settlePolledGenerationJob(due[0], GenerationJobResult{}, false, nil, restart); err != nil || !ok {
			t.Fatalf("settle still running: %v %v", ok, err)
		}
		if job := loadTestJob(t, db, "v1"); job.Status != model.GenerationStatusPolling || !job.NextRunAt.Equal(restart.Add(generationPollInterval)) {
			t.Fatalf("still running: %#v", job)
		}
		if ok, err := settlePolledGenerationJob(due[0], GenerationJobResult{}, false, GenerationError{Message: "网络抖动", Retryable: true}, restart); err != nil || !ok {
			t.Fatalf("settle transient: %v %v", ok, err)
		}
		if job := loadTestJob(t, db, "v1"); job.Status != model.GenerationStatusPolling || job.Error != "网络抖动" {
			t.Fatalf("transient poll error must keep polling: %#v", job)
		}
		// The real worker loop (on the wall clock) picks it up and stores the result.
		if err := db.Model(&model.GenerationJob{}).Where("id = ?", "v1").Update("next_run_at", generationNow().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		polled := make(chan model.GenerationJob, 1)
		pollGenerationJobs(func(_ context.Context, job model.GenerationJob) (GenerationJobResult, bool, error) {
			polled <- job
			return GenerationJobResult{OutputFileID: "f1", OutputURL: "/api/files/f1/content"}, true, nil
		})
		select {
		case job := <-polled:
			if job.UpstreamTaskID != "client_video_task_v1" {
				t.Fatalf("polled job: %#v", job)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("worker did not poll the due job")
		}
		deadline := time.Now().Add(5 * time.Second)
		for loadTestJob(t, db, "v1").Status != model.GenerationStatusSucceeded && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if job := loadTestJob(t, db, "v1"); job.Status != model.GenerationStatusSucceeded || job.OutputFileID != "f1" || job.FinishedAt == nil {
			t.Fatalf("finished job: %#v", job)
		}
	})

	t.Run("deadline, final errors and cancel", func(t *testing.T) {
		for _, id := range []string{"v2", "v3"} {
			if ok, err := settleGenerationJob(claimTestJob(t, id, now), GenerationJobResult{UpstreamTaskID: "t-" + id}, nil, now); err != nil || !ok {
				t.Fatal(ok, err)
			}
		}
		v2 := loadTestJob(t, db, "v2")
		if ok, err := settlePolledGenerationJob(v2, GenerationJobResult{}, false, nil, now.Add(generationPollTimeout)); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if job := loadTestJob(t, db, "v2"); job.Status != model.GenerationStatusFailed || job.Error != "视频生成超时" {
			t.Fatalf("timed out job: %#v", job)
		}
		if _, err := CancelGenerationJob("u1", "v3"); err != nil {
			t.Fatalf("cancel polling job: %v", err)
		}
		if ok, _ := settlePolledGenerationJob(loadTestJob(t, db, "v3"), GenerationJobResult{OutputFileID: "late"}, true, nil, now); ok {
			t.Fatal("a cancelled job took a late poll result")
		}
		if job := loadTestJob(t, db, "v3"); job.Status != model.GenerationStatusCancelled || job.OutputFileID != "" {
			t.Fatalf("cancelled job: %#v", job)
		}
		// A final upstream failure ends the job at once.
		if err := db.Model(&model.GenerationJob{}).Where("id = ?", "v2").Updates(map[string]any{"status": model.GenerationStatusPolling}).Error; err != nil {
			t.Fatal(err)
		}
		if ok, err := settlePolledGenerationJob(loadTestJob(t, db, "v2"), GenerationJobResult{}, false, GenerationError{Message: "内容违规"}, now); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if job := loadTestJob(t, db, "v2"); job.Status != model.GenerationStatusFailed || job.Error != "内容违规" {
			t.Fatalf("final poll failure: %#v", job)
		}
	})
}

// TestComposeVideo runs the real ffmpeg on two generated clips with a voiceover and background music.
func TestComposeVideo(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not on PATH: compose test skipped")
	}
	if runGenerationTestIsolated(t, "TestComposeVideo") {
		return
	}
	generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{})
	user := model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	ctx := WithUser(context.Background(), user)
	dir := t.TempDir()
	clip := func(name string, args ...string) string {
		out := filepath.Join(dir, name)
		if output, err := exec.Command(ffmpeg, append(append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...), out)...).CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v %s", name, err, output)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		mimeType := http.DetectContentType(data)
		if strings.HasSuffix(name, ".wav") {
			mimeType = "audio/wav"
		}
		uploaded, err := UploadStorageObject(ctx, name, mimeType, data)
		if err != nil {
			t.Fatal(err)
		}
		return uploaded.ID
	}
	// Different sizes, rates and lengths: the compose step must normalize them.
	clip1 := clip("a.mp4", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25", "-t", "1", "-pix_fmt", "yuv420p")
	clip2 := clip("b.mp4", "-f", "lavfi", "-i", "testsrc2=size=240x320:rate=24", "-t", "3", "-pix_fmt", "yuv420p")
	voice := clip("v.wav", "-f", "lavfi", "-i", "sine=frequency=440:duration=1")
	bgm := clip("m.wav", "-f", "lavfi", "-i", "sine=frequency=220:duration=1")

	request, _ := json.Marshal(ComposeRequest{Width: 360, Height: 640, Subtitles: true, BgmFileID: bgm, Shots: []ComposeShot{
		{VideoFileID: clip1, AudioFileID: voice, Seconds: 2, Subtitle: "第一镜"},
		{VideoFileID: clip2, AudioFileID: voice, Seconds: 2, Subtitle: "第二镜"},
	}})
	result, err := ComposeGenerationJob(context.Background(), user, model.GenerationJob{ID: "c1", RequestJSON: string(request)})
	if err != nil {
		t.Fatal(err)
	}
	download, err := DownloadStorageObject(result.OutputFileID, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = download.Stream.Close()
	if download.Object.MimeType != "video/mp4" || result.Width != 360 || result.Height != 640 {
		t.Fatalf("output: %#v %#v", download.Object, result)
	}
	var output ComposeOutput
	if err := json.Unmarshal([]byte(result.OutputText), &output); err != nil || output.SubtitleFileID == "" {
		t.Fatalf("subtitle output: %q %v", result.OutputText, err)
	}
	t.Logf("subtitles burned: %v", output.SubtitlesBurned)
	subtitle, err := DownloadStorageObject(output.SubtitleFileID, "")
	if err != nil {
		t.Fatal(err)
	}
	srt := make([]byte, 200)
	n, _ := subtitle.Stream.Read(srt)
	_ = subtitle.Stream.Close()
	if !strings.Contains(string(srt[:n]), "00:00:02,000 --> 00:00:04,000\n第二镜") {
		t.Fatalf("srt: %q", srt[:n])
	}
	out := filepath.Join(dir, "out.mp4")
	stream, _ := DownloadStorageObject(result.OutputFileID, "")
	file, _ := os.Create(out)
	_, _ = file.ReadFrom(stream.Stream)
	_ = file.Close()
	_ = stream.Stream.Close()
	info, _ := exec.Command(ffmpeg, "-hide_banner", "-i", out).CombinedOutput()
	match := regexp.MustCompile(`Duration: 00:00:(\d+\.\d+)`).FindStringSubmatch(string(info))
	if match == nil {
		t.Fatalf("no duration: %s", info)
	}
	seconds, _ := strconv.ParseFloat(match[1], 64)
	if seconds < 3.9 || seconds > 4.2 || !strings.Contains(string(info), "Audio: aac") || !strings.Contains(string(info), "360x640") || !strings.Contains(string(info), "30 fps") {
		t.Fatalf("composed video: %.2fs\n%s", seconds, info)
	}

	t.Run("missing ffmpeg is reported", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := ComposeGenerationJob(context.Background(), user, model.GenerationJob{ID: "c2", RequestJSON: string(request)})
		if err == nil || err.Error() != "服务器未安装 ffmpeg" {
			t.Fatalf("got %v", err)
		}
	})
}
