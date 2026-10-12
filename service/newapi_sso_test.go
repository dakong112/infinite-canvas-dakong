package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// fakeNewapi serves /api/sso/exchange and records the last request body.
func fakeNewapi(t *testing.T, role int, status int) (*httptest.Server, *map[string]string) {
	t.Helper()
	last := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sso/exchange" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&last)
		if last["code"] != "good-code" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success":false,"message":"invalid or expired code"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"success":true,"data":{"user":{"id":42,"username":"admin","display_name":"Alice","email":"a@example.com","role":` + itoa(role) + `,"group":"default"},"access_token":"nap_pat","access_token_expires_at":1900000000,"api_key":"sk-user-key"}}`))
	}))
	t.Cleanup(server.Close)
	return server, &last
}

func itoa(value int) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func TestLoginWithNewapiCode(t *testing.T) {
	const marker = "NEWAPI_SSO_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoginWithNewapiCode$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated newapi sso test: %v\n%s", err, output)
		}
		return
	}
	server, last := fakeNewapi(t, 1, http.StatusOK)
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "test-secret", JWTExpireHours: 1,
		NewapiBaseURL: server.URL, NewapiClientID: "infinite", NewapiClientSecret: "s3cret", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	// A pre-existing local user owns the username "admin"; the new-api user must not take it over.
	if err := db.Create(&model.User{ID: "local-admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive, AffCode: "LOCALADM"}).Error; err != nil {
		t.Fatal(err)
	}

	if !NewapiSSOEnabled() {
		t.Fatal("expected SSO enabled with full config")
	}

	session, err := LoginWithNewapiCode("good-code")
	if err != nil {
		t.Fatal(err)
	}
	if (*last)["client_id"] != "infinite" || (*last)["client_secret"] != "s3cret" || (*last)["redirect_uri"] != "https://www.example.com/sso/callback" {
		t.Fatalf("unexpected exchange request: %#v", *last)
	}
	if session.Token == "" || session.User.Role != model.UserRoleUser {
		t.Fatalf("unexpected session: %#v", session)
	}
	if session.User.Username == "admin" {
		t.Fatal("new-api user must not reuse a local username")
	}
	stored, ok, err := repository.GetUserByNewapiID("42")
	if err != nil || !ok {
		t.Fatalf("linked user not stored: %v", err)
	}
	if stored.NewapiAPIKey != "sk-user-key" || stored.NewapiAccessToken != "nap_pat" || stored.Email != "a@example.com" {
		t.Fatalf("credentials not stored: %#v", stored)
	}

	// Second login updates the same local user instead of creating another.
	again, err := LoginWithNewapiCode("good-code")
	if err != nil {
		t.Fatal(err)
	}
	if again.User.ID != session.User.ID {
		t.Fatalf("expected same local user, got %s and %s", again.User.ID, session.User.ID)
	}

	if _, err := LoginWithNewapiCode("bad-code"); err == nil {
		t.Fatal("expected bad code to fail")
	}

	// A banned local user cannot sign in through SSO.
	stored.Status = model.UserStatusBan
	if _, err := repository.SaveUser(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := LoginWithNewapiCode("good-code"); err == nil {
		t.Fatal("expected banned user to be rejected")
	}
}

func TestNewapiAdminRoleMapping(t *testing.T) {
	if role := newapiRole(10); role != model.UserRoleAdmin {
		t.Fatalf("role 10 should map to admin, got %s", role)
	}
	if role := newapiRole(100); role != model.UserRoleAdmin {
		t.Fatalf("role 100 should map to admin, got %s", role)
	}
	if role := newapiRole(1); role != model.UserRoleUser {
		t.Fatalf("role 1 should map to user, got %s", role)
	}
}
