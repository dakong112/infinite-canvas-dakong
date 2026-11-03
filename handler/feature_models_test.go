package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestGenerationFallback(t *testing.T) {
	vision := model.GenerationJob{Kind: model.GenerationKindVision, Attempts: 1, MaxAttempts: 3}
	lastTry := vision
	lastTry.Attempts = 3
	image := model.GenerationJob{Kind: model.GenerationKindImage, Attempts: 1, MaxAttempts: 3}
	video := model.GenerationJob{Kind: model.GenerationKindVideo, Attempts: 1, MaxAttempts: 3}
	for _, tc := range []struct {
		name   string
		job    model.GenerationJob
		status int
		body   string
		want   bool
	}{
		{"model not found", vision, 404, `{"error":{"message":"model gpt-x not found","code":"model_not_found"}}`, true},
		{"not in group", vision, 200, `{"code":1,"msg":"该模型在当前分组不可用"}`, true},
		{"no available channel", image, 503, `{"error":{"message":"当前分组 vip 下对于模型 x 无可用渠道"}}`, true},
		{"image input unsupported", vision, 400, `{"error":{"message":"image_url is only supported by certain models"}}`, true},
		{"upstream 5xx", vision, 502, `bad gateway`, true},
		{"connection refused", image, 0, ``, true},
		{"vision timeout", vision, -1, ``, true},
		{"image timeout may bill", image, 504, ``, false},
		{"image sent then lost", image, -1, ``, false},
		{"video gateway timeout", video, 524, ``, false},
		{"429 before the job's retries", vision, 429, `rate limited`, false},
		{"429 on the last attempt", lastTry, 429, `rate limited`, true},
		{"429 on a video submit", video, 429, `rate limited`, true},
		{"user quota", vision, 403, `{"error":{"message":"user quota is not enough","code":"insufficient_user_quota"}}`, false},
		{"quota message", video, 200, `余额不足，请先充值`, false},
		{"team quota", vision, 503, `{"code":1,"msg":"你的团队额度已用完，请联系团队管理员调整"}`, false},
		{"content policy", image, 400, `{"error":{"message":"Your request was rejected by our safety system","code":"content_policy_violation"}}`, false},
		{"bad request", vision, 400, `{"error":{"message":"prompt is required"}}`, false},
	} {
		if got := generationFallback(tc.job, tc.status, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: fallback = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A job walks its candidates in order while each one is unusable, records the model that ran, and stops at
// quota or policy refusals; a video moves on only after a refused submit and is submitted once.
func TestFeatureModelFallbackExecution(t *testing.T) {
	const marker = "FEATURE_FALLBACK_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFeatureModelFallbackExecution$", "-test.v", "-test.timeout=50s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated fallback test: %v\n%s", err, output)
		}
		return
	}
	var mu sync.Mutex
	calls := []string{}
	replies := map[string]struct {
		status int
		body   string
	}{
		"busy":     {503, `{"error":{"message":"no available channel"}}`},
		"missing":  {404, `{"error":{"message":"model not found","code":"model_not_found"}}`},
		"broke":    {403, `{"error":{"message":"user quota is not enough","code":"insufficient_user_quota"}}`},
		"strict":   {400, `{"error":{"message":"rejected by safety system","code":"content_policy_violation"}}`},
		"good":     {200, `{"output_text":"ok"}`},
		"sora-bad": {404, `{"error":{"message":"model not found","code":"model_not_found"}}`},
		"sora-504": {504, `{"error":{"message":"timeout"}}`},
		"sora-2":   {200, `{"id":"video_mock1","object":"video","status":"queued","progress":0}`},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := ""
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			_ = r.ParseMultipartForm(1 << 20)
			name = r.FormValue("model")
		} else {
			var body struct {
				Model string `json:"model"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			name = body.Model
		}
		mu.Lock()
		calls = append(calls, name)
		mu.Unlock()
		reply := replies[name]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
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
	models := []string{}
	for name := range replies {
		models = append(models, name)
	}
	save := func(feature string, names ...string) {
		lists := map[string][]model.FeatureModel{}
		for _, name := range names {
			lists[feature] = append(lists[feature], model.FeatureModel{Model: name})
		}
		settings := model.Settings{
			Public:  model.PublicSetting{ModelChannel: model.PublicModelChannelSetting{AllowUserRemoteChannel: &enabled}},
			Private: model.PrivateSetting{FeatureModels: lists, Channels: []model.ModelChannel{{ID: "c1", Protocol: "openai", Name: "mock", BaseURL: server.URL, APIKey: "k", Models: models, Weight: 1, Enabled: true}}},
		}
		if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	user := model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	ctx := service.WithUser(context.Background(), user)
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	frame, err := service.UploadStorageObject(ctx, "k.png", "image/png", png)
	if err != nil {
		t.Fatal(err)
	}
	run := func(item service.GenerationJobInput, attempts int) (service.GenerationJobResult, error, []string) {
		t.Helper()
		_, jobs, err := service.CreateGenerationBatch(ctx, user, service.GenerationBatchInput{Items: []service.GenerationJobInput{item}})
		if err != nil {
			t.Fatal(err)
		}
		job := jobs[0]
		job.Attempts = attempts
		mu.Lock()
		calls = nil
		mu.Unlock()
		result, err := executeGenerationJob(context.Background(), job)
		mu.Lock()
		defer mu.Unlock()
		return result, err, append([]string{}, calls...)
	}
	vision := service.GenerationJobInput{Kind: model.GenerationKindVision, Endpoint: "/responses", Feature: service.FeatureVision, Request: map[string]any{"input": "hi"}}

	save(service.FeatureVision, "busy", "missing", "good", "broke")
	result, err, seen := run(vision, 1)
	if err != nil || result.OutputText != "ok" || result.UsedModel != "good" || strings.Join(seen, ",") != "busy,missing,good" {
		t.Fatalf("fallback order: %#v %v %v", result, err, seen)
	}
	for _, stop := range []string{"broke", "strict"} {
		save(service.FeatureVision, stop, "good")
		if result, err, seen := run(vision, 1); err == nil || result.UsedModel != stop || len(seen) != 1 {
			t.Fatalf("%s must not fall back: %#v %v %v", stop, result, err, seen)
		}
	}
	save(service.FeatureVision, "missing")
	if _, err, seen := run(vision, 1); err == nil || len(seen) != 1 {
		t.Fatalf("last candidate fails: %v %v", err, seen)
	}

	clip := service.GenerationJobInput{Kind: model.GenerationKindVideo, Endpoint: "/videos", Feature: service.FeatureVideo, Request: map[string]any{"prompt": "p", "seconds": "4"}, InputFileIDs: []string{frame.ID}}
	save(service.FeatureVideo, "sora-bad", "sora-2", "busy")
	if result, err, seen := run(clip, 1); err != nil || result.UpstreamTaskID == "" || result.UsedModel != "sora-2" || strings.Join(seen, ",") != "sora-bad,sora-2" {
		t.Fatalf("video fallback: %#v %v %v", result, err, seen)
	}
	save(service.FeatureVideo, "sora-504", "sora-2")
	if _, err, seen := run(clip, 1); err == nil || len(seen) != 1 {
		t.Fatalf("a video that may have been accepted is never resubmitted: %v %v", err, seen)
	}
}
