package service

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
)

func TestValidateEnabledStorageProviderTypes(t *testing.T) {
	if err := validateEnabledStorageProviderTypes([]model.StorageProvider{
		{Type: model.StorageProviderTypeS3, Enabled: true},
		{Type: model.StorageProviderTypeS3, Enabled: true},
	}); err != nil {
		t.Fatalf("same provider type should be allowed: %v", err)
	}
	if err := validateEnabledStorageProviderTypes([]model.StorageProvider{
		{Type: model.StorageProviderTypeWebDAV, Enabled: true},
		{Type: model.StorageProviderTypeWebDAV, Enabled: true},
	}); err != nil {
		t.Fatalf("same WebDAV type should be allowed: %v", err)
	}
	if err := validateEnabledStorageProviderTypes([]model.StorageProvider{
		{Type: model.StorageProviderTypeS3, Enabled: true},
		{Type: model.StorageProviderTypeWebDAV, Enabled: true},
	}); err == nil {
		t.Fatal("mixed enabled provider types should be rejected")
	}
	if err := validateEnabledStorageProviderTypes([]model.StorageProvider{{Type: "unknown"}}); err == nil {
		t.Fatal("unknown provider type should be rejected")
	}
}

func TestValidateUserStorageProviderTypes(t *testing.T) {
	enabled := true
	disabled := false
	if err := validateUserStorageProviderTypes(UserStorageProviders{
		S3:     &StorageObjectProviderInput{Enabled: &enabled},
		WebDAV: &StorageObjectProviderInput{Enabled: &disabled},
	}); err != nil {
		t.Fatalf("only one enabled user provider should be allowed: %v", err)
	}
	if err := validateUserStorageProviderTypes(UserStorageProviders{
		S3:     &StorageObjectProviderInput{Enabled: &enabled},
		WebDAV: &StorageObjectProviderInput{Enabled: &enabled},
	}); err == nil {
		t.Fatal("two enabled user provider types should be rejected")
	}
}

func TestStorageProviderConfigured(t *testing.T) {
	if !storageProviderConfigured(model.StorageProvider{
		Type: model.StorageProviderTypeS3, Endpoint: "https://s3.example.com", Bucket: "media", AccessKeyID: "key", SecretAccessKey: "secret",
	}) {
		t.Fatal("complete S3 provider should be configured")
	}

	webDAV := normalizeStorageProvider(model.StorageProvider{
		Type: model.StorageProviderTypeWebDAV, Endpoint: "https://dav.example.com", Username: "user", Password: "password",
	})
	if webDAV.PathPrefix != "canvas" {
		t.Fatalf("WebDAV default path prefix = %q, want canvas", webDAV.PathPrefix)
	}
	if !storageProviderConfigured(webDAV) {
		t.Fatal("complete WebDAV provider should be configured")
	}
	if storageProviderConfigured(model.StorageProvider{
		Type: model.StorageProviderTypeWebDAV, Endpoint: "https://dav.example.com",
	}) {
		t.Fatal("WebDAV provider without credentials should be incomplete")
	}
}

func TestCleanStoragePath(t *testing.T) {
	if got, err := cleanStoragePath("/infinite-canvas/user/file.png/"); err != nil || got != "infinite-canvas/user/file.png" {
		t.Fatalf("cleanStoragePath() = %q, %v", got, err)
	}
	for _, value := range []string{"", ".", "..", "a//b", "a/../b"} {
		if _, err := cleanStoragePath(value); err == nil {
			t.Fatalf("cleanStoragePath(%q) should fail", value)
		}
	}
}

func TestNewWebDAVClientValidatesEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "ftp://dav.example.com", "https://user:pass@dav.example.com", "https://dav.example.com/#fragment"} {
		if _, err := newWebDAVClient(model.StorageProvider{Endpoint: endpoint}); err == nil {
			t.Fatalf("newWebDAVClient(%q) should fail", endpoint)
		}
	}
	if _, err := newWebDAVClient(model.StorageProvider{Endpoint: "https://dav.example.com/webdav"}); err != nil {
		t.Fatalf("valid WebDAV endpoint should be accepted: %v", err)
	}
}

func TestLocalStorageProvider(t *testing.T) {
	previous := config.Cfg.LocalStorageDir
	t.Cleanup(func() { config.Cfg.LocalStorageDir = previous })

	config.Cfg.LocalStorageDir = ""
	if _, ok := localStorageProvider(); ok {
		t.Fatal("local provider must be off without LOCAL_STORAGE_DIR")
	}
	if HasAdminStorageProvider(model.PrivateStorageSetting{}) {
		t.Fatal("no provider should be available")
	}

	config.Cfg.LocalStorageDir = t.TempDir()
	provider, err := selectStorageProvider(model.PrivateStorageSetting{})
	if err != nil || provider.Type != model.StorageProviderTypeLocal || !storageProviderConfigured(provider) {
		t.Fatalf("local fallback not selected: %#v %v", provider, err)
	}
	if !HasAdminStorageProvider(model.PrivateStorageSetting{}) {
		t.Fatal("local provider should count as server storage")
	}
	s3 := model.StorageProvider{ID: "s3", Type: model.StorageProviderTypeS3, Endpoint: "https://s3.example.com", Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s", Weight: 1, Enabled: true}
	if selected, _ := selectStorageProvider(model.PrivateStorageSetting{Providers: []model.StorageProvider{s3}}); selected.ID != "s3" {
		t.Fatalf("configured S3 must win over local disk, got %q", selected.ID)
	}

	key := "u1/2026/10/14/a.png"
	if err := putStorageObject(provider, key, "image/png", strings.NewReader("0123456789"), 10); err != nil {
		t.Fatal(err)
	}
	stream, err := getLocalObjectStream(provider, key, "bytes=2-4")
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(stream.Body)
	_ = stream.Body.Close()
	if string(part) != "234" || stream.StatusCode != 206 || stream.ContentRange != "bytes 2-4/10" || stream.ContentLength != 3 {
		t.Fatalf("range read = %q status=%d range=%q length=%d", part, stream.StatusCode, stream.ContentRange, stream.ContentLength)
	}
	full, err := getLocalObjectStream(provider, key, "")
	if err != nil {
		t.Fatal(err)
	}
	all, _ := io.ReadAll(full.Body)
	_ = full.Body.Close()
	if string(all) != "0123456789" || full.StatusCode != 200 || full.ContentLength != 10 {
		t.Fatalf("full read = %q status=%d length=%d", all, full.StatusCode, full.ContentLength)
	}
	if err := deleteStorageObjectData(provider, key); err != nil {
		t.Fatal(err)
	}
	if _, err := getLocalObjectStream(provider, key, ""); err == nil {
		t.Fatal("deleted object is still readable")
	}
	if err := deleteStorageObjectData(provider, key); err != nil {
		t.Fatalf("deleting a missing file must succeed: %v", err)
	}
	if err := putStorageObject(provider, "../escape.png", "image/png", strings.NewReader("x"), 1); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestUserStorageProviderCannotUseLocalDisk(t *testing.T) {
	provider := normalizeUserStorageProviderForOwner(StorageObjectProviderInput{Type: "LOCAL", Endpoint: "/etc"}, "u1")
	if provider.Type != model.StorageProviderTypeS3 {
		t.Fatalf("user provider type = %q, want s3", provider.Type)
	}
	webDAV := normalizeUserStorageProviderForOwner(StorageObjectProviderInput{Type: "WebDAV", Endpoint: "https://dav.example.com"}, "u1")
	if webDAV.Type != model.StorageProviderTypeWebDAV {
		t.Fatalf("webdav provider type = %q", webDAV.Type)
	}
}

func TestLocalStorageRejectsEscapingKeys(t *testing.T) {
	previous := config.Cfg.LocalStorageDir
	t.Cleanup(func() { config.Cfg.LocalStorageDir = previous })
	config.Cfg.LocalStorageDir = t.TempDir()
	provider, _ := localStorageProvider()
	for _, key := range []string{`a/..\..\x`, `..\x`, `/etc/passwd`, `C:/x/y.png`, `C:\x.png`, "a/b\x00c.png", "a/../../x", "a//b.png", "a:b/c.png"} {
		if err := putLocalObject(provider, key, strings.NewReader("x")); err == nil {
			t.Errorf("put accepted key %q", key)
		}
		if _, err := getLocalObjectStream(provider, key, ""); err == nil {
			t.Errorf("get accepted key %q", key)
		}
		if err := deleteLocalObject(provider, key); err == nil {
			t.Errorf("delete accepted key %q", key)
		}
	}
	if err := putLocalObject(provider, "u1/2026/10/14/ok.png", strings.NewReader("x")); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
}

func TestStorageExtensionPattern(t *testing.T) {
	for ext, ok := range map[string]bool{".png": true, ".JPEG": true, ".webp": true, ".": false, "": false, ".a b": false, ".tar.gz": false, ".averyverylong": false, ".p/g": false, ".p\\g": false} {
		if storageExtPattern.MatchString(ext) != ok {
			t.Errorf("ext %q match=%v, want %v", ext, !ok, ok)
		}
	}
}

// TestPresignS3GetURL matches botocore's generate_presigned_url (path style, region auto) for the same inputs.
func TestPresignS3GetURL(t *testing.T) {
	provider := model.StorageProvider{Type: model.StorageProviderTypeS3, Endpoint: "https://acct.r2.cloudflarestorage.com/", Bucket: "media", AccessKeyID: "AKID", SecretAccessKey: "SECRET"}
	got := presignS3GetURL(provider, "canvas/u1/2026/10/17/abc.png", 7*24*time.Hour, time.Date(2026, 10, 17, 8, 30, 0, 0, time.UTC))
	want := "https://acct.r2.cloudflarestorage.com/media/canvas/u1/2026/10/17/abc.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKID%2F20261017%2Fauto%2Fs3%2Faws4_request&X-Amz-Date=20261017T083000Z&X-Amz-Expires=604800&X-Amz-SignedHeaders=host&X-Amz-Signature=d4546df56bf1236025fd3280d1229b8a3e6d03edb04a17f4800558d42c3880ab"
	if got != want {
		t.Fatalf("presigned URL\n got %s\nwant %s", got, want)
	}
}
