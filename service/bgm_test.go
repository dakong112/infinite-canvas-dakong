package service

import (
	"context"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func TestBgmLibrary(t *testing.T) {
	if runGenerationTestIsolated(t, "TestBgmLibrary") {
		return
	}
	generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Video: model.PublicVideoSetting{Model: "sora-2", ScriptModel: "gpt-4o-mini"}}, "sora-2", "gpt-4o-mini")
	admin, user := model.AuthUser{ID: "admin", Role: model.UserRoleAdmin}, model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	adminCtx, userCtx := WithUser(context.Background(), admin), WithUser(context.Background(), user)
	track, err := UploadStorageObject(adminCtx, "calm.mp3", "audio/mpeg", []byte("not really mp3"))
	if err != nil {
		t.Fatal(err)
	}
	private, err := UploadStorageObject(adminCtx, "private.mp3", "audio/mpeg", []byte("admin's own audio"))
	if err != nil {
		t.Fatal(err)
	}
	image, _ := UploadStorageObject(adminCtx, "a.png", "image/png", testPNG)

	if _, err := AddBgmTrack(adminCtx, BgmTrackInput{FileID: image.ID, Name: "图"}); err == nil {
		t.Fatal("an image became a track")
	}
	if _, err := AddBgmTrack(userCtx, BgmTrackInput{FileID: track.ID, Name: "偷"}); err == nil {
		t.Fatal("another user's file became a track")
	}
	tracks, err := AddBgmTrack(adminCtx, BgmTrackInput{FileID: track.ID, Name: " 轻快 ", Tags: []string{"欢快", " ", "欢快", "电子"}, DurationSec: 92.5})
	if err != nil || len(tracks) != 1 || tracks[0].Name != "轻快" || len(tracks[0].Tags) != 2 || tracks[0].URL != "/api/files/"+track.ID+"/content" {
		t.Fatalf("add track: %#v %v", tracks, err)
	}
	if object, _ := repository.GetStorageObject(track.ID); !object.Public {
		t.Fatal("track file must be public")
	}
	// The settings page resends settings without the library; it must survive.
	settings, _ := repository.GetSettings()
	settings.Private.BgmTracks = nil
	if _, err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	if listed, _ := BgmTracks(); len(listed) != 1 {
		t.Fatalf("settings save dropped the library: %#v", listed)
	}

	product, err := UploadStorageObject(userCtx, "p.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	input := func(bgm string) *ProductVideoInput {
		return &ProductVideoInput{Form: ProductVideoForm{ProductFileIDs: []string{product.ID}, Name: "杯子"}, Settings: ProductVideoSettings{Duration: 15, BgmFileID: bgm}}
	}
	if err := normalizeProductVideoInput(input(track.ID), user); err != nil {
		t.Fatalf("library track rejected: %v", err)
	}
	if err := normalizeProductVideoInput(input(private.ID), user); err == nil {
		t.Fatal("another user's audio accepted as background music")
	}
	// The compose job may read the public track as an input (the empty request then fails its own check).
	compose := func(id string) error {
		_, err := prepareGenerationJob(user, GenerationJobInput{Kind: model.GenerationKindCompose, Endpoint: "ffmpeg", Model: "ffmpeg", Request: map[string]any{}, InputFileIDs: []string{id}})
		return err
	}
	if err := compose(track.ID); err == nil || err.Error() == "输入图片不存在" {
		t.Fatalf("compose input of a library track: %v", err)
	}
	if err := compose(private.ID); err == nil || err.Error() != "输入图片不存在" {
		t.Fatalf("compose read another user's private file: %v", err)
	}

	if tracks, err := DeleteBgmTrack(track.ID); err != nil || len(tracks) != 0 {
		t.Fatalf("delete: %#v %v", tracks, err)
	}
	if err := normalizeProductVideoInput(input(track.ID), user); err == nil {
		t.Fatal("a removed track is still accepted")
	}
}
