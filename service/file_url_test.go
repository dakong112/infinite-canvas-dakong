package service

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

func TestFileReadable(t *testing.T) {
	config.Cfg = config.Config{JWTSecret: "s", FileURLSigning: true}
	private, public := model.StorageObject{ID: "f1", CreatedBy: "u1"}, model.StorageObject{ID: "f2", CreatedBy: "u1", Public: true}
	future, past := time.Now().Add(time.Hour).Unix(), time.Now().Add(-time.Second).Unix()
	exp := func(value int64) string { return strconv.FormatInt(value, 10) }
	as := func(id string, role model.UserRole) context.Context {
		return WithUser(context.Background(), model.AuthUser{ID: id, Role: role})
	}
	cases := []struct {
		name     string
		ctx      context.Context
		object   model.StorageObject
		exp, sig string
		want     bool
	}{
		{"public without signature", context.Background(), public, "", "", true},
		{"private without signature", context.Background(), private, "", "", false},
		{"private with a bad signature", context.Background(), private, exp(future), "bad", false},
		{"signature of another file", context.Background(), private, exp(future), fileURLSignature("f2", future), false},
		{"expired signature", context.Background(), private, exp(past), fileURLSignature("f1", past), false},
		{"valid signature", context.Background(), private, exp(future), fileURLSignature("f1", future), true},
		{"owner signed in", as("u1", model.UserRoleUser), private, "", "", true},
		{"admin signed in", as("a1", model.UserRoleAdmin), private, "", "", true},
		{"another user signed in", as("u2", model.UserRoleUser), private, "", "", false},
	}
	for _, test := range cases {
		if got := FileReadable(test.ctx, test.object, test.exp, test.sig); got != test.want {
			t.Errorf("%s: got %v, want %v", test.name, got, test.want)
		}
	}
	config.Cfg.FileURLSigning = false
	if !FileReadable(context.Background(), private, "", "") {
		t.Error("FILE_URL_SIGNING=false still requires a signature")
	}
}

func TestUnsignedFileURL(t *testing.T) {
	cases := map[string]string{
		" /api/files/a-1/content?exp=1&sig=x ":   "/api/files/a-1/content",
		"/api/files/a/content?direct=1&sig=x":    "/api/files/a/content?direct=1",
		"/api/files/a/content":                   "/api/files/a/content",
		"https://cdn.example.com/a.png?sig=keep": "https://cdn.example.com/a.png?sig=keep",
	}
	for in, want := range cases {
		if got := unsignedFileURL(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

// signedQuery returns the query of the first content URL of id in body.
func signedQuery(t *testing.T, body string, id string) url.Values {
	t.Helper()
	parts := fileURLPattern.FindAllStringSubmatch(body, -1)
	for _, part := range parts {
		if part[1] == id {
			return fileURLQuery(part[2])
		}
	}
	t.Fatalf("no url of %s in %s", id, body)
	return nil
}

func TestSignFileURLsInJSON(t *testing.T) {
	if runGenerationTestIsolated(t, "TestSignFileURLsInJSON") {
		return
	}
	generationTestDB(t)
	config.Cfg.FileURLSigning = true
	for _, object := range []model.StorageObject{
		{ID: "mine", ObjectKey: "k1", CreatedBy: "u1"}, {ID: "pub", ObjectKey: "k2", CreatedBy: "a1", Public: true}, {ID: "theirs", ObjectKey: "k3", CreatedBy: "u2"},
	} {
		if _, err := repository.SaveStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	owner := WithUser(context.Background(), model.AuthUser{ID: "u1", Role: model.UserRoleUser})
	job := model.GenerationJob{ID: "j1", OutputFileID: "mine", OutputURL: "/api/files/mine/content", InputFileIDs: []string{"mine", "pub"}}
	raw, err := json.Marshal(map[string]any{"work": job, "old": "/api/files/pub/content?exp=1&sig=old", "other": "/api/files/theirs/content?direct=1"})
	if err != nil {
		t.Fatal(err)
	}
	body := string(SignFileURLsInJSON(owner, raw))
	var decoded struct {
		Work struct {
			OutputURL string   `json:"outputUrl"`
			InputURLs []string `json:"inputUrls"`
		} `json:"work"`
		Old   string `json:"old"`
		Other string `json:"other"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("signed body is not JSON: %v\n%s", err, body)
	}
	for _, link := range []string{decoded.Work.OutputURL, decoded.Work.InputURLs[0]} {
		query := signedQuery(t, link, "mine")
		if !FileReadable(context.Background(), model.StorageObject{ID: "mine"}, query.Get("exp"), query.Get("sig")) {
			t.Errorf("work url not signed: %s", link)
		}
	}
	if decoded.Work.InputURLs[1] != "/api/files/pub/content" || decoded.Old != "/api/files/pub/content" {
		t.Errorf("public urls should be plain: %q %q", decoded.Work.InputURLs[1], decoded.Old)
	}
	if decoded.Other != "/api/files/theirs/content?direct=1" {
		t.Errorf("another user's file was signed: %q", decoded.Other)
	}
	if anonymous := string(SignFileURLsInJSON(context.Background(), raw)); strings.Contains(anonymous, "/api/files/mine/content?") {
		t.Errorf("signed for nobody: %s", anonymous)
	}
	config.Cfg.FileURLSigning = false
	if disabled := string(SignFileURLsInJSON(owner, raw)); strings.Contains(disabled, "/api/files/mine/content?") {
		t.Errorf("signed with FILE_URL_SIGNING=false: %s", disabled)
	}
}

func TestTemplateAndSettingsFilesBecomePublic(t *testing.T) {
	if runGenerationTestIsolated(t, "TestTemplateAndSettingsFilesBecomePublic") {
		return
	}
	templateTestDB(t)
	for _, id := range []string{"cover", "in", "out", "sample", "tool"} {
		if _, err := repository.SaveStorageObject(model.StorageObject{ID: id, ObjectKey: "k-" + id, CreatedBy: "a1"}); err != nil {
			t.Fatal(err)
		}
	}
	item := validTestTemplate("tpl-public")
	item.CoverURL = "/api/files/cover/content?exp=9999999999&sig=abc"
	item.Examples = []model.ImageTemplateExample{{Inputs: []string{"/api/files/in/content?exp=1&sig=x"}, Output: "/api/files/out/content"}}
	saved, err := SaveImageTemplate("", item)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CoverURL != "/api/files/cover/content" || saved.Examples[0].Inputs[0] != "/api/files/in/content" {
		t.Fatalf("signatures were stored: %q %q", saved.CoverURL, saved.Examples[0].Inputs[0])
	}
	if _, err := SaveTemplateSamples(model.TemplateSamples{Product: []string{"/api/files/sample/content?exp=1&sig=x"}}); err != nil {
		t.Fatal(err)
	}
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Public.ImageExamples = map[string]model.ImageToolExamples{"set": {Cover: "/api/files/tool/content"}}
	if _, err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	objects, err := repository.GetStorageObjects([]string{"cover", "in", "out", "sample", "tool"})
	if err != nil {
		t.Fatal(err)
	}
	for id, object := range objects {
		if !object.Public {
			t.Errorf("%s is not public", id)
		}
	}
	if len(objects) != 5 {
		t.Fatalf("objects: %d", len(objects))
	}
	db, _ := repository.DB()
	if err := db.Model(&model.StorageObject{}).Where("1 = 1").Update("public", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := MarkReferencedFilesPublic(); err != nil {
		t.Fatal(err)
	}
	if objects, _ = repository.GetStorageObjects([]string{"cover", "in", "out", "sample", "tool"}); !objects["cover"].Public || !objects["sample"].Public || !objects["tool"].Public {
		t.Fatalf("boot backfill: %#v", objects)
	}
}
