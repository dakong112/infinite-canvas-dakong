package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

func TestSignedFileContent(t *testing.T) {
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		LocalStorageDir: t.TempDir(), FileURLSigning: true}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	owner, other := model.AuthUser{ID: "u1", Role: model.UserRoleUser}, model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	uploaded, err := service.UploadStorageObject(service.WithUser(context.Background(), model.AuthUser{ID: "u1", Role: model.UserRoleAdmin}), "a.png", "image/png", []byte("png-bytes"))
	if err != nil {
		t.Fatal(err)
	}

	// A works response through the router middleware: outputUrl and inputUrls come back signed for the owner.
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), owner))
	}, SignFileURLs)
	engine.GET("/works", func(c *gin.Context) {
		OK(c.Writer, map[string]any{"items": []model.GenerationJob{{ID: "j1", OutputFileID: uploaded.ID, OutputURL: "/api/files/" + uploaded.ID + "/content", InputFileIDs: []string{uploaded.ID}}}})
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/works", nil))
	var works struct {
		Data struct {
			Items []struct {
				OutputURL string   `json:"outputUrl"`
				InputURLs []string `json:"inputUrls"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &works); err != nil || len(works.Data.Items) != 1 {
		t.Fatalf("works: %v %s", err, recorder.Body)
	}
	signed := works.Data.Items[0].OutputURL
	if !strings.Contains(signed, "exp=") || !strings.Contains(signed, "sig=") || works.Data.Items[0].InputURLs[0] != signed {
		t.Fatalf("works urls not signed: %s", recorder.Body)
	}

	get := func(link string, user *model.AuthUser) *httptest.ResponseRecorder {
		t.Helper()
		parsed, _ := url.Parse(link)
		request := httptest.NewRequest(http.MethodGet, link, nil)
		if user != nil {
			request = request.WithContext(service.WithUser(request.Context(), *user))
		}
		recorder := httptest.NewRecorder()
		FileContent(recorder, request, strings.Split(parsed.Path, "/")[3])
		return recorder
	}
	plain := "/api/files/" + uploaded.ID + "/content"
	query, _ := url.ParseQuery(strings.SplitN(signed, "?", 2)[1])
	for name, link := range map[string]string{
		"no signature":      plain,
		"bad signature":     plain + "?exp=" + query.Get("exp") + "&sig=bad",
		"expired signature": plain + "?exp=1&sig=" + query.Get("sig"),
	} {
		if response := get(link, nil); response.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", name, response.Code)
		}
	}
	if response := get(plain, &other); response.Code != http.StatusForbidden {
		t.Errorf("another user: status %d", response.Code)
	}
	if response := get(signed, nil); response.Code != http.StatusOK || response.Body.String() != "png-bytes" || !strings.HasPrefix(response.Header().Get("Cache-Control"), "private") {
		t.Errorf("valid signature: %d %q %q", response.Code, response.Body, response.Header().Get("Cache-Control"))
	}
	if response := get(plain, &owner); response.Code != http.StatusOK {
		t.Errorf("owner with auth: status %d", response.Code)
	}
	config.Cfg.FileURLSigning = false
	if response := get(plain, nil); response.Code != http.StatusOK {
		t.Errorf("signing disabled: status %d", response.Code)
	}
	config.Cfg.FileURLSigning = true
	if err := repository.MarkStorageObjectsPublic([]string{uploaded.ID}); err != nil {
		t.Fatal(err)
	}
	if response := get(plain, nil); response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Cache-Control"), "public") {
		t.Errorf("public file: %d %q", response.Code, response.Header().Get("Cache-Control"))
	}
}
