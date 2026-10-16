package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

// uploadRequest streams a multipart body of size zero bytes through a pipe, so the test itself never holds the file.
func uploadRequest(size int64) *http.Request {
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		part, _ := form.CreateFormFile("file", "lesson.mp4")
		_, err := io.Copy(part, io.LimitReader(zeroReader{}, size))
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
	}()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/files", reader)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request.WithContext(service.WithUser(request.Context(), model.AuthUser{ID: "admin", Role: model.UserRoleAdmin}))
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
	recorder := httptest.NewRecorder()
	UploadFile(recorder, uploadRequest(size))
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

	previous := uploadFileMaxBytes
	uploadFileMaxBytes = 1 << 20
	t.Cleanup(func() { uploadFileMaxBytes = previous })
	recorder = httptest.NewRecorder()
	UploadFile(recorder, uploadRequest(3<<20))
	if !strings.Contains(recorder.Body.String(), "文件不能超过") {
		t.Fatalf("oversized upload accepted: %s", recorder.Body)
	}
}

// TestFileContentRedirectsS3PublicBase: with a public base URL, private files redirect to a presigned GET
// (only for readers the signature admits) and public files to the public link.
func TestFileContentRedirectsS3PublicBase(t *testing.T) {
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1, FileURLSigning: true}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	provider := model.StorageProvider{ID: "r2", Name: "R2", Type: model.StorageProviderTypeS3, Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "media",
		AccessKeyID: "AKID", SecretAccessKey: "SECRET", PublicBaseURL: "https://cdn.example.com", Weight: 1, Enabled: true}
	if _, err := repository.SaveSettings(model.Settings{Private: model.PrivateSetting{Storage: model.PrivateStorageSetting{Providers: []model.StorageProvider{provider}}}}, "now"); err != nil {
		t.Fatal(err)
	}
	for _, object := range []model.StorageObject{
		{ID: "private-1", ProviderID: "r2", Bucket: "media", ObjectKey: "canvas/u1/a.png", PublicURL: "https://cdn.example.com/canvas/u1/a.png", MimeType: "image/png", CreatedBy: "u1"},
		{ID: "public-1", ProviderID: "r2", Bucket: "media", ObjectKey: "canvas/u1/b.png", PublicURL: "https://cdn.example.com/canvas/u1/b.png", MimeType: "image/png", CreatedBy: "u1", Public: true},
	} {
		if _, err := repository.SaveStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	get := func(id string, user *model.AuthUser) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/files/"+id+"/content", nil)
		if user != nil {
			request = request.WithContext(service.WithUser(request.Context(), *user))
		}
		recorder := httptest.NewRecorder()
		FileContent(recorder, request, id)
		return recorder
	}
	if recorder := get("private-1", nil); recorder.Code != http.StatusForbidden {
		t.Fatalf("anonymous read of a private file: %d %s", recorder.Code, recorder.Header().Get("Location"))
	}
	location := get("private-1", &model.AuthUser{ID: "u1", Role: model.UserRoleUser}).Header().Get("Location")
	if !strings.HasPrefix(location, "https://acct.r2.cloudflarestorage.com/media/canvas/u1/a.png?") || !strings.Contains(location, "X-Amz-Expires=604800") || !strings.Contains(location, "X-Amz-Signature=") {
		t.Fatalf("private file must redirect to a presigned GET, got %q", location)
	}
	if location := get("public-1", nil).Header().Get("Location"); location != "https://cdn.example.com/canvas/u1/b.png" {
		t.Fatalf("public file must redirect to its public link, got %q", location)
	}
}
