package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

// uploadRequest streams a multipart body of size zero bytes through a pipe, so the test itself never holds the file.
// The Content-Length header is a rough size, as a browser would send.
func uploadRequest(size int64, filename string, user model.AuthUser) *http.Request {
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		part, _ := form.CreateFormFile("file", filename)
		_, err := io.Copy(part, io.LimitReader(zeroReader{}, size))
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
	}()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/files", reader)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.ContentLength = size + 1024
	return request.WithContext(service.WithUser(request.Context(), user))
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestUploadFileStreamsToStorage(t *testing.T) {
	storageDir := t.TempDir()
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1, LocalStorageDir: storageDir}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)

	const size = 50 << 20
	admin := model.AuthUser{ID: "admin", Role: model.UserRoleAdmin}
	recorder := httptest.NewRecorder()
	UploadFile(recorder, uploadRequest(size, "lesson.mp4", admin))
	var response struct {
		Code int                           `json:"code"`
		Msg  string                        `json:"msg"`
		Data service.UploadedStorageObject `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Data.Bytes != size {
		t.Fatalf("upload: %v %s", err, recorder.Body)
	}
	object, err := repository.GetStorageObject(response.Data.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.Open(filepath.Join(storageDir, filepath.FromSlash(object.ObjectKey)))
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	sum := sha256.New()
	if written, _ := io.Copy(sum, stored); written != size || hex.EncodeToString(sum.Sum(nil)) != object.SHA256 || object.Bytes != size {
		t.Fatalf("stored file: %d bytes, sha %s, record %#v", written, hex.EncodeToString(sum.Sum(nil)), object)
	}
	if object.MimeType != "application/octet-stream" {
		t.Fatalf("sniffed type = %q", object.MimeType)
	}

	// Past the general cap only local admins (or academy editors) upload, and only videos.
	previous, previousVideo := uploadFileMaxBytes, uploadVideoMaxBytes
	uploadFileMaxBytes, uploadVideoMaxBytes = 1<<20, 4<<20
	t.Cleanup(func() { uploadFileMaxBytes, uploadVideoMaxBytes = previous, previousVideo })
	localAdmin := model.AuthUser{ID: "admin", Role: model.UserRoleAdmin, IsLocalAdmin: true}
	for _, tc := range []struct {
		name     string
		user     model.AuthUser
		filename string
		size     int64
		want     string
	}{
		{"user video", model.AuthUser{ID: "u1", Role: model.UserRoleUser}, "lesson.mp4", 3 << 20, "文件不能超过 1MB"},
		{"admin image", localAdmin, "big.png", 3 << 20, "文件不能超过 1MB"},
		{"admin video", localAdmin, "lesson.mp4", 3 << 20, `"code":0`},
		{"admin huge video", localAdmin, "lesson.mp4", 6 << 20, "文件不能超过 4MB"},
	} {
		recorder = httptest.NewRecorder()
		UploadFile(recorder, uploadRequest(tc.size, tc.filename, tc.user))
		if !strings.Contains(recorder.Body.String(), tc.want) {
			t.Errorf("%s: %s, want %s", tc.name, recorder.Body, tc.want)
		}
	}
}

// TestFileContentRedirectsS3PublicBase: a provider streams through the server unless Redirect is on; then
// private files go to a presigned GET living as long as the incoming signature (an hour for a signed-in
// owner), inline-safe public files to the public link, and other types download as attachments.
func TestFileContentRedirectsS3PublicBase(t *testing.T) {
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1, FileURLSigning: true}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	// A loopback endpoint the SSRF guard refuses, so streaming fails fast instead of reaching the network.
	provider := model.StorageProvider{ID: "r2", Name: "R2", Type: model.StorageProviderTypeS3, Endpoint: "https://127.0.0.1:9", Bucket: "media",
		AccessKeyID: "AKID", SecretAccessKey: "SECRET", PublicBaseURL: "https://127.0.0.1:9/pub", Weight: 1, Enabled: true}
	saveProvider := func() {
		if _, err := repository.SaveSettings(model.Settings{Private: model.PrivateSetting{Storage: model.PrivateStorageSetting{Providers: []model.StorageProvider{provider}}}}, "now"); err != nil {
			t.Fatal(err)
		}
	}
	saveProvider()
	for _, object := range []model.StorageObject{
		{ID: "private-1", ProviderID: "r2", Bucket: "media", ObjectKey: "canvas/u1/a.png", PublicURL: "https://127.0.0.1:9/pub/canvas/u1/a.png", MimeType: "image/png", CreatedBy: "u1"},
		{ID: "public-1", ProviderID: "r2", Bucket: "media", ObjectKey: "canvas/u1/b.png", PublicURL: "https://127.0.0.1:9/pub/canvas/u1/b.png", MimeType: "image/png", CreatedBy: "u1", Public: true},
		{ID: "public-svg", ProviderID: "r2", Bucket: "media", ObjectKey: "canvas/u1/c.svg", PublicURL: "https://127.0.0.1:9/pub/canvas/u1/c.svg", MimeType: "image/svg+xml", CreatedBy: "u1", Public: true},
	} {
		if _, err := repository.SaveStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	owner := &model.AuthUser{ID: "u1", Role: model.UserRoleUser}
	get := func(target string, user *model.AuthUser) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		if user != nil {
			request = request.WithContext(service.WithUser(request.Context(), *user))
		}
		recorder := httptest.NewRecorder()
		FileContent(recorder, request, strings.Split(target, "/")[3])
		return recorder
	}
	if recorder := get("/api/files/private-1/content", owner); recorder.Header().Get("Location") != "" {
		t.Fatalf("redirect off must stream, got %d %q", recorder.Code, recorder.Header().Get("Location"))
	}

	provider.Redirect = true
	saveProvider()
	if recorder := get("/api/files/private-1/content", nil); recorder.Code != http.StatusForbidden {
		t.Fatalf("anonymous read of a private file: %d %s", recorder.Code, recorder.Header().Get("Location"))
	}
	recorder := get("/api/files/private-1/content", owner)
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "https://127.0.0.1:9/media/canvas/u1/a.png?") || !strings.Contains(location, "X-Amz-Expires=3600&") || !strings.Contains(location, "response-content-type=image%2Fpng") ||
		strings.Contains(location, "response-content-disposition") || recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("owner read must get an hour-long inline presigned GET, got %q %q", location, recorder.Header().Get("Cache-Control"))
	}
	// A signed link: the presigned GET expires with it.
	signed := string(service.SignFileURLsInJSON(service.WithUser(context.Background(), *owner), []byte(`"/api/files/private-1/content"`)))
	signed = strings.ReplaceAll(strings.Trim(signed, `"`), `\u0026`, "&")
	parsed, _ := url.Parse(signed)
	exp, _ := strconv.ParseInt(parsed.Query().Get("exp"), 10, 64)
	location = get(signed, nil).Header().Get("Location")
	presigned, _ := url.Parse(location)
	if ttl, _ := strconv.ParseInt(presigned.Query().Get("X-Amz-Expires"), 10, 64); exp == 0 || ttl < exp-time.Now().Unix()-5 || ttl > exp-time.Now().Unix() {
		t.Fatalf("signed read must presign for the signature's remaining %ds, got %q", exp-time.Now().Unix(), location)
	}
	if location := get("/api/files/public-1/content", nil).Header().Get("Location"); location != "https://127.0.0.1:9/pub/canvas/u1/b.png" {
		t.Fatalf("public file must redirect to its public link, got %q", location)
	}
	recorder = get("/api/files/public-svg/content", nil)
	location = recorder.Header().Get("Location")
	if !strings.HasPrefix(location, "https://127.0.0.1:9/media/canvas/u1/c.svg?") || !strings.Contains(location, "response-content-disposition=attachment") ||
		!strings.Contains(location, "X-Amz-Expires=3600&") || recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("a public SVG must download from a presigned GET, got %q %q", location, recorder.Header().Get("Cache-Control"))
	}
}
