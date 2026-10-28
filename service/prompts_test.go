package service

import (
	"context"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func TestVideoPrompts(t *testing.T) {
	if runGenerationTestIsolated(t, "TestVideoPrompts") {
		return
	}
	generationTestDB(t)
	adminCtx := WithUser(context.Background(), model.AuthUser{ID: "admin", Role: model.UserRoleAdmin})
	clip, err := UploadStorageObject(adminCtx, "clip.mp4", "video/mp4", []byte("not really mp4"))
	if err != nil {
		t.Fatal(err)
	}
	videoCategory := repository.VideoPromptCategoryCodes()[0]
	if _, err := SavePrompt(model.Prompt{Title: "图", Prompt: "a cat", Category: "system"}); err != nil {
		t.Fatal(err)
	}
	link := "/api/files/" + clip.ID + "/content"
	saved, err := SavePrompt(model.Prompt{ID: "vp-1", Title: "视频", Prompt: "slow dolly in", Category: videoCategory, VideoURL: link + "?exp=1&sig=x"})
	if err != nil || saved.VideoURL != link || saved.CreatedAt == "" {
		t.Fatalf("save video prompt: %#v %v", saved, err)
	}
	if object, _ := repository.GetStorageObject(clip.ID); !object.Public {
		t.Fatal("preview clip must be public")
	}

	videos, err := ListPrompts(model.Query{Media: "video"})
	if err != nil || len(videos.Items) != 1 || videos.Items[0].Media != "video" || videos.Items[0].Title != "视频" {
		t.Fatalf("video tab: %#v %v", videos.Items, err)
	}
	for _, code := range videos.Categories {
		if category, _ := repository.PromptCategoryByCode(code); category.Media != "video" {
			t.Fatalf("video tab lists image category %s", code)
		}
	}
	images, _ := ListPrompts(model.Query{Media: "image"})
	if len(images.Items) != 1 || images.Items[0].Title != "图" || images.Items[0].Media != "" {
		t.Fatalf("image tab: %#v", images.Items)
	}
	if all, _ := ListPrompts(model.Query{}); all.Total != 2 {
		t.Fatalf("all tab total %d", all.Total)
	}
}
