package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

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
	// A signed response caches only until the signature expires; one read with credentials is never stored.
	expiry, _ := strconv.ParseInt(query.Get("exp"), 10, 64)
	response := get(signed, nil)
	maxAge, _ := strconv.ParseInt(strings.TrimPrefix(response.Header().Get("Cache-Control"), "private, max-age="), 10, 64)
	if left := expiry - time.Now().Unix(); response.Code != http.StatusOK || response.Body.String() != "png-bytes" || maxAge < left || maxAge > left+5 {
		t.Errorf("valid signature: %d %q %q, want max-age about %d", response.Code, response.Body, response.Header().Get("Cache-Control"), left)
	}
	if response := get(plain, &owner); response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Errorf("owner with auth: status %d %q", response.Code, response.Header().Get("Cache-Control"))
	}

	// File info: the storage location only for the owner (or an admin).
	info := func(user *model.AuthUser) model.StorageObject {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/files/"+uploaded.ID, nil)
		if user != nil {
			request = request.WithContext(service.WithUser(request.Context(), *user))
		}
		recorder := httptest.NewRecorder()
		FileInfo(recorder, request, uploaded.ID)
		var body struct {
			Data model.StorageObject `json:"data"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return body.Data
	}
	if mine := info(&owner); mine.ObjectKey == "" || mine.MimeType != "image/png" {
		t.Errorf("owner info: %#v", mine)
	}
	if theirs := info(&other); theirs.ObjectKey != "" || theirs.ProviderID != "" || theirs.CreatedBy != "" || theirs.MimeType != "image/png" {
		t.Errorf("another user's info: %#v", theirs)
	}
	config.Cfg.FileURLSigning = false
	if response := get(plain, nil); response.Code != http.StatusOK {
		t.Errorf("signing disabled: status %d", response.Code)
	}
	config.Cfg.FileURLSigning = true
	if err := repository.SetStorageObjectsPublic([]string{uploaded.ID}, true); err != nil {
		t.Fatal(err)
	}
	if response := get(plain, nil); response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("public file: %d %q", response.Code, response.Header().Get("Cache-Control"))
	}
}
