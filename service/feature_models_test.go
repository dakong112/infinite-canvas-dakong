package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func TestNormalizeFeatureModelsMigratesOldFields(t *testing.T) {
	old := model.Settings{Public: model.PublicSetting{
		ModelChannel: model.PublicModelChannelSetting{AvailableModels: []string{"gpt-4o-mini"}, DefaultTextModel: "gpt-4o-mini"},
		Ecommerce:    model.PublicEcommerceSetting{ImageModel: " gpt-image-1 "},
		Video:        model.PublicVideoSetting{Model: "sora-2", TTSModel: "tts-1"},
	}, Private: model.PrivateSetting{Channels: []model.ModelChannel{{Name: "c", BaseURL: "https://api.example.com", Models: []string{"gpt-4o-mini"}, Enabled: true}}}}
	once := normalizeSettings(old)
	want := map[string][]model.FeatureModel{
		FeatureVision: {}, FeatureImage: {{Model: "gpt-image-1"}}, FeatureVideoScript: {}, FeatureKeyframe: {},
		FeatureVideo: {{Model: "sora-2"}}, FeatureTTS: {{Model: "tts-1"}},
	}
	if !reflect.DeepEqual(once.Private.FeatureModels, want) {
		t.Fatalf("migrated = %#v", once.Private.FeatureModels)
	}
	// Empty lists keep the old fallbacks: vision → default text model, keyframe → image, script → vision.
	if e, v := once.Public.Ecommerce, once.Public.Video; e.VisionModel != "gpt-4o-mini" || e.ImageModel != "gpt-image-1" || v.KeyframeModel != "gpt-image-1" || v.ScriptModel != "gpt-4o-mini" || v.Model != "sora-2" || v.TTSModel != "tts-1" {
		t.Fatalf("public fields = %#v %#v", e, v)
	}
	if twice := normalizeSettings(once); !reflect.DeepEqual(twice.Private.FeatureModels, want) || twice.Public.Ecommerce != once.Public.Ecommerce || twice.Public.Video != once.Public.Video {
		t.Fatalf("migration is not idempotent: %#v", twice.Private.FeatureModels)
	}
	// Saved lists win over the old fields; entries are trimmed, deduped and capped at five.
	lists := map[string][]model.FeatureModel{FeatureImage: {{Group: " vip ", Model: "a"}, {Group: "vip", Model: "a"}, {Model: " "}, {Model: "b"}, {Model: "c"}, {Model: "d"}, {Model: "e"}, {Model: "f"}}}
	saved := normalizeSettings(model.Settings{Public: old.Public, Private: model.PrivateSetting{Channels: old.Private.Channels, FeatureModels: lists}})
	if got := saved.Private.FeatureModels[FeatureImage]; len(got) != 5 || got[0] != (model.FeatureModel{Group: "vip", Model: "a"}) || got[4].Model != "e" {
		t.Fatalf("image list = %#v", got)
	}
	if saved.Public.Ecommerce.ImageModel != "a" || saved.Public.Video.TTSModel != "" || len(saved.Private.FeatureModels[FeatureTTS]) != 0 {
		t.Fatalf("public fields follow the lists: %#v %#v", saved.Public.Ecommerce, saved.Public.Video)
	}
	// Public names keep the list order without groups, dedupe a model listed in two groups, and follow the parent list.
	lists = map[string][]model.FeatureModel{FeatureVision: {{Group: "vip", Model: "gone"}, {Group: "a", Model: "v"}, {Group: "b", Model: "v"}}, FeatureVideoScript: {}}
	names := normalizeSettings(model.Settings{Public: old.Public, Private: model.PrivateSetting{Channels: old.Private.Channels, FeatureModels: lists}}).Public.FeatureModelNames
	if want := []string{"gone", "v"}; !reflect.DeepEqual(names[FeatureVision], want) || !reflect.DeepEqual(names[FeatureVideoScript], want) || len(names[FeatureTTS]) != 0 || len(names) != 6 {
		t.Fatalf("names = %#v", names)
	}
}

func TestFeatureModelJobs(t *testing.T) {
	if runGenerationTestIsolated(t, "TestFeatureModelJobs") {
		return
	}
	db := generationTestDB(t)
	enabled := true
	save := func(lists map[string][]model.FeatureModel) {
		t.Helper()
		settings := model.Settings{
			Public: model.PublicSetting{ModelChannel: model.PublicModelChannelSetting{AllowUserRemoteChannel: &enabled}},
			Private: model.PrivateSetting{FeatureModels: lists, Channels: []model.ModelChannel{
				{ID: "c1", Protocol: "openai", Name: "one", BaseURL: "http://127.0.0.1:9", APIKey: "k", Models: []string{"v1", "v2", "v3"}, Weight: 1, Enabled: true},
			}},
		}
		if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	user := model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	ctx := WithUser(context.Background(), user)
	input := func(feature string) GenerationBatchInput {
		return GenerationBatchInput{Items: []GenerationJobInput{{Kind: model.GenerationKindVision, Endpoint: "/responses", Feature: feature, Request: map[string]any{"input": "hi"}}}}
	}

	// The job starts with the first usable model (an unknown channel is skipped) and keeps the rest in order.
	save(map[string][]model.FeatureModel{FeatureVision: {{Group: "gone", Model: "v1"}, {Model: "v2"}, {Group: "c1", Model: "v3"}, {Model: "v1"}}})
	_, jobs, err := CreateGenerationBatch(ctx, user, input(FeatureVision))
	if err != nil {
		t.Fatal(err)
	}
	job := jobs[0]
	wantRest := []model.GenerationCandidate{{Model: "v3", ChannelID: "c1"}, {Model: "v1"}}
	if job.Model != "v2" || !strings.Contains(job.RequestJSON, `"model":"v2"`) || !reflect.DeepEqual(job.Candidates, wantRest) {
		t.Fatalf("job = %s %s %#v", job.Model, job.RequestJSON, job.Candidates)
	}
	next, err := NextGenerationCandidate(job)
	if err != nil || next.Model != "v3" || next.ChannelID != "c1" || !strings.Contains(next.RequestJSON, `"model":"v3"`) || len(next.Candidates) != 1 {
		t.Fatalf("next = %#v %v", next, err)
	}

	// videoScript falls back to the vision list.
	if _, jobs, err := CreateGenerationBatch(ctx, user, input(FeatureVideoScript)); err != nil || jobs[0].Model != "v2" {
		t.Fatalf("script fallback: %#v %v", jobs, err)
	}
	// An empty list with no fallback model names the feature.
	if _, _, err := CreateGenerationBatch(ctx, user, input(FeatureTTS)); err == nil || err.Error() != "管理员尚未配置配音模型" {
		t.Fatalf("unconfigured tts: %v", err)
	}

	// The worker records the model that actually ran, also on failure.
	claimed := claimTestJob(t, job.ID, generationNow().Add(time.Second))
	if ok, err := settleGenerationJob(claimed, GenerationJobResult{UsedModel: "v3"}, GenerationError{Message: "x"}, generationNow()); !ok || err != nil {
		t.Fatalf("settle: %v %v", ok, err)
	}
	var stored model.GenerationJob
	if err := db.First(&stored, "id = ?", job.ID).Error; err != nil || stored.UsedModel != "v3" || stored.Model != "v2" {
		t.Fatalf("stored = %#v %v", stored, err)
	}
}
