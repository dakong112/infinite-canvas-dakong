package handler

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

// testMP4 is just an ftyp box: enough for content sniffing to call it video/mp4.
var testMP4 = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")

// A video job submits once through the /videos proxy path, then polls the provider until done and
// stores the result; a re-run after a crash finds the earlier submit and does not submit again.
func TestVideoAndAudioGenerationJobs(t *testing.T) {
	const marker = "VIDEO_JOB_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVideoAndAudioGenerationJobs$", "-test.v", "-test.timeout=50s")
		cmd.Env = append(os.Environ(), marker+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated video job test: %v\n%s", err, output)
		}
		return
	}
	var mu sync.Mutex
	submits, polls := []string{}, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			submits = append(submits, r.FormValue("prompt")+"|"+r.FormValue("seconds")+"|"+r.MultipartForm.File["input_reference[]"][0].Header.Get("Content-Type"))
			_, _ = w.Write([]byte(`{"id":"video_mock1","object":"video","status":"queued","progress":0}`))
		case r.URL.Path == "/v1/videos/video_mock1":
			polls++
			status := "in_progress"
			if polls >= 2 {
				status = "completed"
			}
			_, _ = w.Write([]byte(`{"id":"video_mock1","object":"video","status":"` + status + `","progress":50}`))
		case r.URL.Path == "/v1/videos/video_mock1/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(testMP4)
		case r.URL.Path == "/v1/audio/speech":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"voice":"alloy"`) {
				http.Error(w, "voice missing", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3\x03\x00\x00\x00\x00\x00\x00fake-mp3"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1, LocalStorageDir: t.TempDir()}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	if err := db.Create(&model.User{ID: "u1", Username: "u1", AffCode: "U1", Role: model.UserRoleUser, Status: model.UserStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	enabled := true
	settings := model.Settings{
		Public:  model.PublicSetting{ModelChannel: model.PublicModelChannelSetting{AllowUserRemoteChannel: &enabled}},
		Private: model.PrivateSetting{Channels: []model.ModelChannel{{ID: "c1", Protocol: "openai", Name: "mock", BaseURL: server.URL, APIKey: "k", Models: []string{"sora-2", "tts-1"}, Weight: 1, Enabled: true}}},
	}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	user := model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	ctx := service.WithUser(context.Background(), user)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	keyframe, err := service.UploadStorageObject(ctx, "k.png", "image/png", png)
	if err != nil {
		t.Fatal(err)
	}
	_, jobs, err := service.CreateGenerationBatch(ctx, user, service.GenerationBatchInput{Source: model.GenerationSourceProductVideo, Items: []service.GenerationJobInput{
		{Kind: model.GenerationKindVideo, Endpoint: "/videos", Model: "sora-2", Request: map[string]any{"prompt": "镜头一", "seconds": "4", "size": "720x1280"}, InputFileIDs: []string{keyframe.ID}},
		{Kind: model.GenerationKindAudio, Endpoint: "/audio/speech", Model: "tts-1", Request: map[string]any{"input": "你好", "voice": "alloy", "response_format": "mp3"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	claim := func(id string) model.GenerationJob {
		at := time.Now().UTC().Add(time.Second)
		job, err := repository.ClaimGenerationJob(at, "lease-"+id, at.Add(time.Minute), repository.GenerationClaimExclusion{}, func(candidate model.GenerationJob) bool { return candidate.ID == id })
		if err != nil || job == nil {
			t.Fatalf("claim %s: %v", id, err)
		}
		return *job
	}

	video := claim(jobs[0].ID)
	result, err := executeGenerationJob(context.Background(), video)
	if err != nil || result.UpstreamTaskID != "client_video_task_"+video.ID {
		t.Fatalf("submit: %#v %v", result, err)
	}
	again, err := executeGenerationJob(context.Background(), video)
	mu.Lock()
	if err != nil || again.UpstreamTaskID != result.UpstreamTaskID || len(submits) != 1 || submits[0] != "镜头一|4|image/png" {
		t.Fatalf("a re-run must not submit again: %#v %v %q", again, err, submits)
	}
	mu.Unlock()
	if task, found, _ := service.GetUserVideoTask("u1", result.UpstreamTaskID); !found || task.Source != "generation" || task.UpstreamTaskID != "video_mock1" {
		t.Fatalf("task row: %#v", task)
	}
	if listed, _ := service.ListUserVideoTasks("u1", "video-workbench", 10); len(listed) != 0 {
		t.Fatalf("queue video tasks must stay out of the workbench list: %v", listed)
	}
	video.UpstreamTaskID = result.UpstreamTaskID
	if _, done, err := pollGenerationJob(context.Background(), video); done || err != nil {
		t.Fatalf("first poll: %v %v", done, err)
	}
	polled, done, err := pollGenerationJob(context.Background(), video)
	if err != nil || !done || polled.OutputFileID == "" {
		t.Fatalf("second poll: %#v %v %v", polled, done, err)
	}
	stored, err := service.DownloadStorageObject(polled.OutputFileID, "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(stored.Stream)
	_ = stored.Stream.Close()
	if stored.Object.MimeType != "video/mp4" || string(data) != string(testMP4) || !strings.HasSuffix(stored.Object.ObjectKey, ".mp4") {
		t.Fatalf("stored video: %#v", stored.Object)
	}
	mu.Lock()
	if len(submits) != 1 || polls != 2 {
		t.Fatalf("upstream calls: submits=%d polls=%d", len(submits), polls)
	}
	mu.Unlock()

	audio, err := executeGenerationJob(context.Background(), claim(jobs[1].ID))
	if err != nil || audio.OutputFileID == "" {
		t.Fatalf("audio: %#v %v", audio, err)
	}
	if object, err := repository.GetStorageObject(audio.OutputFileID); err != nil || object.MimeType != "audio/mpeg" || !strings.HasSuffix(object.ObjectKey, ".mp3") {
		t.Fatalf("stored audio: %#v %v", object, err)
	}
}
