package model

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openSecretTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSecretSerializerEncryptsAndReadsLegacyPlaintext(t *testing.T) {
	previous := secretKey
	t.Cleanup(func() { secretKey = previous })
	if err := SetSecretKey(bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	db := openSecretTestDB(t)

	user := User{ID: "u1", Username: "u1", AffCode: "A1", NewapiAPIKey: "sk-secret", NewapiAccessToken: "nap_secret"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	var rawKey, rawToken string
	if err := db.Raw("select newapi_api_key, newapi_access_token from users where id = ?", "u1").Row().Scan(&rawKey, &rawToken); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawKey, secretPrefix) || strings.Contains(rawKey, "sk-secret") {
		t.Fatalf("api key not encrypted at rest: %q", rawKey)
	}
	if !strings.HasPrefix(rawToken, secretPrefix) {
		t.Fatalf("access token not encrypted at rest: %q", rawToken)
	}

	var loaded User
	if err := db.First(&loaded, "id = ?", "u1").Error; err != nil {
		t.Fatal(err)
	}
	if loaded.NewapiAPIKey != "sk-secret" || loaded.NewapiAccessToken != "nap_secret" {
		t.Fatalf("decrypt mismatch: %q %q", loaded.NewapiAPIKey, loaded.NewapiAccessToken)
	}

	// Rows written before encryption stay readable.
	if err := db.Exec("insert into users (id, username, aff_code, newapi_api_key) values (?, ?, ?, ?)", "u2", "u2", "A2", "sk-legacy").Error; err != nil {
		t.Fatal(err)
	}
	var legacy User
	if err := db.First(&legacy, "id = ?", "u2").Error; err != nil {
		t.Fatal(err)
	}
	if legacy.NewapiAPIKey != "sk-legacy" {
		t.Fatalf("legacy plaintext not readable: %q", legacy.NewapiAPIKey)
	}

	// Empty values stay empty (no ciphertext for "").
	var rawEmpty string
	if err := db.Raw("select coalesce(newapi_access_token, '') from users where id = ?", "u2").Row().Scan(&rawEmpty); err != nil {
		t.Fatal(err)
	}
	if rawEmpty != "" {
		t.Fatalf("empty secret should stay empty, got %q", rawEmpty)
	}

	// Without the key, encrypted rows must fail loudly instead of returning garbage.
	secretKey = nil
	var noKey User
	if err := db.First(&noKey, "id = ?", "u1").Error; err == nil {
		t.Fatal("expected error reading encrypted secret without key")
	}
}

func TestSetSecretKeyRejectsWrongLength(t *testing.T) {
	if err := SetSecretKey([]byte("short")); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}
