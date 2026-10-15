package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"gorm.io/gorm"
)

// finishTestJobWithFile plays the worker for a job that produced a stored file.
func finishTestJobWithFile(t *testing.T, id string, fileID string) {
	t.Helper()
	at := generationNow().Add(time.Hour)
	job := claimTestJob(t, id, at)
	values := map[string]any{"status": model.GenerationStatusSucceeded, "output_file_id": fileID, "finished_at": at, "lease_token": "", "lease_expires_at": nil}
	if ok, err := repository.FinishGenerationJob(job.ID, job.LeaseToken, values); err != nil || !ok {
		t.Fatalf("finish %s: %v %v", id, ok, err)
	}
}

func batchJobs(t *testing.T, db *gorm.DB, batchID string, kind string) []model.GenerationJob {
	t.Helper()
	var jobs []model.GenerationJob
	if err := db.Where("batch_id = ? AND kind = ?", batchID, kind).Order("created_at, id").Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestProductVideoFlow(t *testing.T) {
	if runGenerationTestIsolated(t, "TestProductVideoFlow") {
		return
	}
	db := generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Video: model.PublicVideoSetting{Model: "sora-2", KeyframeModel: "gpt-image-1", ScriptModel: "gpt-4o-mini", TTSModel: "tts-1", MaxShotSeconds: 5}},
		"sora-2", "gpt-image-1", "gpt-4o-mini", "tts-1")
	u1, u2 := model.AuthUser{ID: "u1", Role: model.UserRoleUser}, model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	ctx1, ctx2 := WithUser(context.Background(), u1), WithUser(context.Background(), u2)
	product, err := UploadStorageObject(ctx1, "p.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	input := ProductVideoInput{Form: ProductVideoForm{ProductFileIDs: []string{product.ID}, Name: "保温杯", SellingPoints: "长效保温"},
		Settings: ProductVideoSettings{Duration: 15, Voice: "alloy", Subtitles: true}}
	view, err := CreateProductVideo(ctx1, u1, input)
	if err != nil {
		t.Fatal(err)
	}
	if view.Settings.Ratio != "9:16" || view.Settings.Resolution != "720p" || view.Settings.Style != "scene" || len(view.Shots) != 0 {
		t.Fatalf("created: %#v", view)
	}

	t.Run("other users cannot see or use it", func(t *testing.T) {
		if _, err := GetProductVideo(ctx2, u2, view.ID); err == nil || err.Error() != "带货视频不存在" {
			t.Fatalf("u2 read it: %v", err)
		}
		if _, err := GenerateProductVideoScript(ctx2, u2, view.ID); err == nil {
			t.Fatal("u2 queued a script")
		}
		if _, err := CreateProductVideo(ctx2, u2, input); err == nil || err.Error() != "图片不存在" {
			t.Fatalf("u2 used u1 product images: %v", err)
		}
		if list, _ := ListProductVideos("u2"); len(list) != 0 {
			t.Fatalf("u2 list: %v", list)
		}
		if list, _ := ListProductVideos("u1"); len(list) != 1 || list[0].ID != view.ID {
			t.Fatalf("u1 list: %v", list)
		}
	})

	t.Run("script job fills a clamped storyboard", func(t *testing.T) {
		scripted, err := GenerateProductVideoScript(ctx1, u1, view.ID)
		if err != nil || scripted.ScriptJobID == "" {
			t.Fatalf("script: %v", err)
		}
		job := loadTestJob(t, db, scripted.ScriptJobID)
		if job.Kind != model.GenerationKindVision || job.Model != "gpt-4o-mini" || !slices.Equal(job.InputFileIDs, []string{product.ID}) ||
			!strings.Contains(job.RequestJSON, "15 秒的带货短视频分镜脚本") || !strings.Contains(job.RequestJSON, "每个镜头 2-5 秒") {
			t.Fatalf("script job: %#v", job)
		}
		if _, err := GenerateProductVideoScript(ctx1, u1, view.ID); err == nil {
			t.Fatal("a second script job was queued while one is pending")
		}
		// A chatty, truncated reply: the closed shots are kept, 8 seconds is cut to the 5-second cap.
		finishTestJob(t, job.ID, model.GenerationStatusSucceeded, `好的：{"shots":[{"seconds":8,"scene":"保温杯特写","camera":"推近","voiceover":"一杯热水保温一整天","subtitle":"长效保温"},{"seconds":4,"scene":"办公桌上倒水","camera":"固定","voiceover":"","subtitle":"随手一杯"},{"seconds":3,"scene":"被截断`)
		got, err := GetProductVideo(ctx1, u1, view.ID)
		if err != nil || got.ScriptJobID != "" || len(got.Shots) != 2 || got.Shots[0].Seconds != 5 || got.Shots[0].ID != "shot_01" || got.Shots[1].Scene != "办公桌上倒水" {
			t.Fatalf("storyboard: %#v %v", got.Shots, err)
		}
	})

	t.Run("script edits are validated and keep shot ids", func(t *testing.T) {
		if _, err := SaveProductVideoScript(ctx1, u1, view.ID, []StoryboardShot{{Seconds: 6, Scene: "x"}}); err == nil || !strings.Contains(err.Error(), "时长需为 2-5 秒") {
			t.Fatalf("too long shot: %v", err)
		}
		over := []StoryboardShot{{Seconds: 5, Scene: "a"}, {Seconds: 5, Scene: "b"}, {Seconds: 5, Scene: "c"}, {Seconds: 2, Scene: "d"}}
		if _, err := SaveProductVideoScript(ctx1, u1, view.ID, over); err == nil || !strings.Contains(err.Error(), "超过视频时长") {
			t.Fatalf("over duration: %v", err)
		}
		saved, err := SaveProductVideoScript(ctx1, u1, view.ID, []StoryboardShot{
			{ID: "shot_01", Seconds: 5, Scene: "保温杯特写", Camera: "推近", Voiceover: "一杯热水保温一整天", Subtitle: "长效保温"},
			{Seconds: 3, Scene: "户外露营倒热水", Camera: "环绕", Voiceover: "露营也能喝热水", Subtitle: "户外必备"},
		})
		if err != nil || len(saved.Shots) != 2 || saved.Shots[0].ID != "shot_01" || saved.Shots[1].ID != "shot_03" {
			t.Fatalf("saved: %#v %v", saved.Shots, err)
		}
	})

	keyframe, _ := UploadStorageObject(ctx1, "k.png", "image/png", testPNG)
	t.Run("keyframes chain into one video job each, even under concurrent reads", func(t *testing.T) {
		if _, err := ComposeProductVideo(ctx1, u1, view.ID); err == nil || err.Error() != "第 1 镜视频尚未生成完成" {
			t.Fatalf("compose too early: %v", err)
		}
		generated, err := GenerateProductVideoShots(ctx1, u1, view.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		keyframes, audios := batchJobs(t, db, view.ID, model.GenerationKindImage), batchJobs(t, db, view.ID, model.GenerationKindAudio)
		if len(keyframes) != 2 || len(audios) != 2 || keyframes[0].Endpoint != "/images/edits" || keyframes[0].Model != "gpt-image-1" ||
			!strings.Contains(keyframes[0].RequestJSON, `"size":"1024x1536"`) || !strings.Contains(audios[0].RequestJSON, `"voice":"alloy"`) {
			t.Fatalf("shot jobs: %#v %#v", keyframes, audios)
		}
		if generated.Shots[0].KeyframeJobID == "" || generated.Shots[0].AudioJobID == "" || generated.Shots[0].VideoJobID != "" {
			t.Fatalf("shot state: %#v", generated.Shots[0])
		}
		if _, err := GenerateProductVideoShots(ctx1, u1, view.ID, nil); err == nil {
			t.Fatal("generate-all re-queued running shots")
		}
		for _, job := range keyframes {
			finishTestJobWithFile(t, job.ID, keyframe.ID)
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				if _, err := GetProductVideo(ctx1, u1, view.ID); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		clips := batchJobs(t, db, view.ID, model.GenerationKindVideo)
		if len(clips) != 2 || !slices.Equal(clips[0].InputFileIDs, []string{keyframe.ID}) || !strings.Contains(clips[0].RequestJSON, `"seconds":"5"`) || !strings.Contains(clips[0].RequestJSON, `"size":"720x1280"`) {
			t.Fatalf("clips: %#v", clips)
		}
		got, _ := GetProductVideo(ctx1, u1, view.ID)
		if got.Shots[0].VideoJobID != clips[0].ID && got.Shots[0].VideoJobID != clips[1].ID {
			t.Fatalf("shot not pointed at its clip: %#v", got.Shots[0])
		}
	})

	t.Run("compose joins finished clips and voiceovers; the result is a video work", func(t *testing.T) {
		clip, _ := UploadStorageObject(ctx1, "c.mp4", "video/mp4", []byte("clip"))
		voice, _ := UploadStorageObject(ctx1, "v.mp3", "audio/mpeg", []byte("ID3voice"))
		for _, job := range batchJobs(t, db, view.ID, model.GenerationKindVideo) {
			finishTestJobWithFile(t, job.ID, clip.ID)
		}
		// Jobs created in the same millisecond have no stable order: take the voiceovers in shot order.
		current, _ := GetProductVideo(ctx1, u1, view.ID)
		audios := []model.GenerationJob{{ID: current.Shots[0].AudioJobID}, {ID: current.Shots[1].AudioJobID}}
		finishTestJobWithFile(t, audios[0].ID, voice.ID)
		if _, err := ComposeProductVideo(ctx1, u1, view.ID); err == nil || err.Error() != "第 2 镜配音尚未生成完成" {
			t.Fatalf("compose without voiceover: %v", err)
		}
		finishTestJobWithFile(t, audios[1].ID, voice.ID)
		composed, err := ComposeProductVideo(ctx1, u1, view.ID)
		if err != nil || composed.ComposeJobID == "" {
			t.Fatalf("compose: %v", err)
		}
		if _, err := ComposeProductVideo(ctx1, u1, view.ID); err == nil {
			t.Fatal("a second compose was queued while one runs")
		}
		foreign := map[string]any{"width": 720, "height": 1280, "shots": []any{map[string]any{"video": "someone-elses-file", "seconds": 3}}}
		if _, err := buildGenerationJobs(ctx1, u1, model.GenerationBatch{ID: "x"}, []GenerationJobInput{{Kind: model.GenerationKindCompose, Endpoint: "ffmpeg", Model: "ffmpeg", Request: foreign, InputFileIDs: []string{clip.ID}}}); err == nil || err.Error() != "合成素材无效" {
			t.Fatalf("compose of a file outside the job inputs: %v", err)
		}
		job := loadTestJob(t, db, composed.ComposeJobID)
		var request ComposeRequest
		if err := json.Unmarshal([]byte(job.RequestJSON), &request); err != nil || job.Kind != model.GenerationKindCompose || job.ChannelID != "" ||
			request.Width != 720 || request.Height != 1280 || !request.Subtitles || len(request.Shots) != 2 ||
			request.Shots[1] != (ComposeShot{VideoFileID: clip.ID, AudioFileID: voice.ID, Seconds: 3, Subtitle: "户外必备"}) || len(job.InputFileIDs) != 2 {
			t.Fatalf("compose job: %#v %#v %v", job, request, err)
		}
		page, err := ListWorks("u1", WorksQuery{Kind: "video"})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != job.ID || page.Counts.Running != 1 {
			t.Fatalf("video works: %#v %v", page, err)
		}
		if all, _ := ListWorks("u1", WorksQuery{}); len(all.Items) != 1 {
			t.Fatalf("keyframes, clips and voiceovers must stay out of the works page: %d items", len(all.Items))
		}
	})
}

func TestVideoSettingNormalization(t *testing.T) {
	setting := normalizePublicSetting(model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{VisionModel: "gpt-4o-mini", ImageModel: "gpt-image-1"}})
	if video := setting.Video; video.KeyframeModel != "gpt-image-1" || video.ScriptModel != "gpt-4o-mini" || video.TTSVoice != "alloy" || video.MaxShotSeconds != 5 || video.TTSModel != "" {
		t.Fatalf("defaults: %#v", video)
	}
	for in, want := range map[int]int{1: 2, 8: 8, 100: 15, -3: 5} {
		setting := normalizePublicSetting(model.PublicSetting{Video: model.PublicVideoSetting{Model: " sora-2 ", TTSModel: " tts-1 ", TTSVoice: " nova ", MaxShotSeconds: in}})
		if setting.Video.MaxShotSeconds != want || setting.Video.Model != "sora-2" || setting.Video.TTSModel != "tts-1" || setting.Video.TTSVoice != "nova" {
			t.Errorf("max %d: %#v", in, setting.Video)
		}
	}
}

func TestParseStoryboard(t *testing.T) {
	fenced := "```json\n{\"shots\":[{\"seconds\":3,\"scene\":\" 开箱 \",\"camera\":\"推近\",\"voiceover\":\"来看看\",\"subtitle\":\"开箱\"},{\"seconds\":2,\"scene\":\"\"}]}\n```"
	if shots := parseStoryboard(fenced); len(shots) != 1 || shots[0].Scene != "开箱" || shots[0].Seconds != 3 {
		t.Fatalf("fenced: %#v", shots)
	}
	if shots := parseStoryboard("没有 JSON"); len(shots) != 0 {
		t.Fatalf("garbage: %#v", shots)
	}
	long := strings.Repeat("长", 600)
	shot := trimStoryboardShot(StoryboardShot{Scene: long, Camera: long, Voiceover: long, Subtitle: long})
	if len([]rune(shot.Scene)) != 500 || len([]rune(shot.Camera)) != 100 || len([]rune(shot.Voiceover)) != 300 || len([]rune(shot.Subtitle)) != 50 {
		t.Fatalf("model output must obey the edit limits: %d %d %d %d", len([]rune(shot.Scene)), len([]rune(shot.Camera)), len([]rune(shot.Voiceover)), len([]rune(shot.Subtitle)))
	}
}

func TestGenerationRetryLater(t *testing.T) {
	for err, want := range map[error]bool{errGenerationQueueFull: true, errors.New("database is locked"): true, safeMessageError{message: "当前账号未开放云端渠道"}: false} {
		if got := generationRetryLater(err); got != want {
			t.Errorf("%v: got %v", err, got)
		}
	}
}
