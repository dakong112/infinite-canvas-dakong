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
