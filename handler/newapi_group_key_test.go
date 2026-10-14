package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/service"
)

// A group key deleted in new-api answers 401: the proxy fetches a fresh key and resends once.
func TestCopyAIResponseRenewsNewapiGroupKey(t *testing.T) {
	const marker = "NEWAPI_GROUP_RETRY_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCopyAIResponseRenewsNewapiGroupKey$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated retry test: %v\n%s", err, output)
		}
		return
	}
	var mu sync.Mutex
	var upstream []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"group":"vip"}}`))
		case "/api/sso/client":
			_, _ = w.Write([]byte(`{"success":true,"data":{"name":"p"}}`))
		case "/api/pricing":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"model_name":"gpt-4o","enable_groups":["default"]}],"group_ratio":{"default":1,"vip":0.5}}`))
		case "/api/sso/key":
			_, _ = w.Write([]byte(`{"success":true,"data":{"key":"sk-new"}}`))
		case "/v1/chat/completions":
			mu.Lock()
			upstream = append(upstream, r.Header.Get("Authorization")+" "+string(body))
			mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer sk-new" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		NewapiBaseURL: server.URL, NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	if err := model.SetSecretKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	if err := db.Create(&model.User{ID: "g1", Username: "g1", AffCode: "G1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "50",
		NewapiAccessToken: "nap", NewapiAPIKey: "sk-login", NewapiTokenExpiresAt: time.Now().Add(time.Hour).Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Private.Channels = []model.ModelChannel{{ID: "na", Name: "new-api", Protocol: "openai", BaseURL: server.URL, APIKey: "sk-admin", Enabled: true, Weight: 1}}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveNewapiGroupKey(model.NewapiGroupKey{UserID: "g1", GroupName: "default", APIKey: "sk-old"}); err != nil {
		t.Fatal(err)
	}

	channel, err := service.SelectModelChannelForUser("g1", "gpt-4o", "newapi@default", true)
	if err != nil || channel.APIKey != "sk-old" {
		t.Fatalf("stored group key: %#v %v", channel, err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o"}`)))
	service.SetModelChannelAuthHeader(request, channel)
	recorder := httptest.NewRecorder()
	copyAIResponse(recorder, request, channel, aiLogContext{Endpoint: "/chat/completions", Method: http.MethodPost, Model: "gpt-4o", Channel: channel, UserID: "g1"}, nil)

	mu.Lock()
	defer mu.Unlock()
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ok":true`) {
		t.Fatalf("retried response: %d %s", recorder.Code, recorder.Body.String())
	}
	want := []string{`Bearer sk-old {"model":"gpt-4o"}`, `Bearer sk-new {"model":"gpt-4o"}`}
	if len(upstream) != 2 || upstream[0] != want[0] || upstream[1] != want[1] {
		t.Fatalf("upstream calls: %q", upstream)
	}
	if stored, ok, err := repository.GetNewapiGroupKey("g1", "default"); err != nil || !ok || stored.APIKey != "sk-new" {
		t.Fatalf("renewed key must be stored: %#v %v %v", stored, ok, err)
	}
}
