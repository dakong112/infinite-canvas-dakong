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
		_, _ = w.Write([]byte(`{"success":true,"data":{"user":{"id":42,"username":"admin","display_name":"Alice","email":"a@example.com","role":` + itoa(role) + `,"group":"default"},"access_token":"nap_pat","access_token_expires_at":1900000000,"api_key":"sk-user-key","session_id":"sid-login"}}`))
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
	if claims, err := ParseToken(session.Token); err != nil || claims.SessionID != "sid-login" {
		t.Fatalf("token must carry the new-api session: %#v %v", claims, err)
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

	// Banning through the admin edit path keeps the new-api link, so SSO login stays rejected.
	if _, err := SaveUser(model.AuthUser{IsLocalAdmin: true}, model.User{ID: stored.ID, Username: stored.Username, Role: stored.Role, Status: model.UserStatusBan}, ""); err != nil {
		t.Fatal(err)
	}
	banned, _, _ := repository.GetUserByID(stored.ID)
	if banned.NewapiID != "42" || banned.NewapiAPIKey != "sk-user-key" || banned.NewapiAccessToken != "nap_pat" || banned.NewapiTokenExpiresAt != 1900000000 {
		t.Fatalf("admin edit wiped new-api link: %#v", banned)
	}
	if _, err := LoginWithNewapiCode("good-code"); err == nil {
		t.Fatal("expected banned user to be rejected")
	}
	if _, err := SaveUser(model.AuthUser{IsLocalAdmin: true}, model.User{ID: stored.ID, Username: stored.Username, Role: stored.Role, Status: model.UserStatusActive}, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := LinuxDoAuthorizeURL(httptest.NewRequest(http.MethodGet, "/", nil), "/"); err == nil {
		t.Fatal("linux.do authorize must be closed in SSO mode")
	}
	if _, _, err := LoginWithLinuxDo(httptest.NewRequest(http.MethodGet, "/", nil), "c", "s"); err == nil {
		t.Fatal("linux.do login must be closed in SSO mode")
	}

	linked, _, _ := repository.GetUserByNewapiID("42")
	newapiChannel := model.ModelChannel{ID: "c1", Protocol: "openai", BaseURL: server.URL + "/v1", APIKey: "placeholder", Models: []string{"gpt-image-1"}, Weight: 1, Enabled: true}
	bound, err := BindNewapiUserKey(newapiChannel, model.AuthUser{ID: linked.ID})
	if err != nil {
		t.Fatal(err)
	}
	if bound.APIKey != "sk-user-key" {
		t.Fatalf("expected user key, got %q", bound.APIKey)
	}
	foreign := model.ModelChannel{ID: "c2", Protocol: "openai", BaseURL: "https://other.example.com", APIKey: "admin-key", Models: []string{"gpt-image-1"}, Weight: 1, Enabled: true}
	if _, err := BindNewapiUserKey(foreign, model.AuthUser{ID: linked.ID}); err == nil {
		t.Fatal("non new-api channel must be rejected for linked users")
	}
	// http vs https on the same host is a different origin.
	httpsServer := newapiChannel
	httpsServer.BaseURL = "https" + server.URL[len("http"):] + "/v1"
	if _, err := BindNewapiUserKey(httpsServer, model.AuthUser{ID: linked.ID}); err == nil {
		t.Fatal("scheme mismatch must be rejected")
	}
	// A banned user is refused by Bind even with a valid key stored.
	if _, err := SaveUser(model.AuthUser{IsLocalAdmin: true}, model.User{ID: linked.ID, Username: linked.Username, Role: linked.Role, Status: model.UserStatusBan}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := BindNewapiUserKey(newapiChannel, model.AuthUser{ID: linked.ID}); err == nil {
		t.Fatal("banned user must be refused")
	}
	if _, err := SaveUser(model.AuthUser{IsLocalAdmin: true}, model.User{ID: linked.ID, Username: linked.Username, Role: linked.Role, Status: model.UserStatusActive}, ""); err != nil {
		t.Fatal(err)
	}
	// The local emergency admin has no new-api key and keeps the channel key.
	adminBound, err := BindNewapiUserKey(foreign, model.AuthUser{ID: "local-admin"})
	if err != nil || adminBound.APIKey != "admin-key" {
		t.Fatalf("local admin should keep channel key, got %q %v", adminBound.APIKey, err)
	}

	if _, err := Register("someone", "password"); err == nil {
		t.Fatal("local registration must be closed in SSO mode")
	}
	if err := db.Create(&model.User{ID: "local-user", Username: "local-user", Password: mustHash(t, "pw"), Role: model.UserRoleUser, Status: model.UserStatusActive, AffCode: "LOCALUSR"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Login("local-user", "pw"); err == nil {
		t.Fatal("local non-admin login must be rejected in SSO mode")
	}
}

// A token issued under a new-api login session works only while new-api reports that session signed in.
func TestNewapiSharedSession(t *testing.T) {
	const marker = "NEWAPI_SESSION_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNewapiSharedSession$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated session test: %v\n%s", err, output)
		}
		return
	}
	server := fakeNewapiAccount(t, &callLog{})
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		NewapiBaseURL: server.URL, NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	user := model.User{ID: "u1", Username: "u1", AffCode: "A1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "42"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	fakeSessions.active = map[string]bool{"sid-a": true, "sid-b": true, "sid-c": true}
	token := func(sid string) string {
		value, err := newToken(user, sid)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	hits := func() int {
		fakeSessions.mu.Lock()
		defer fakeSessions.mu.Unlock()
		return fakeSessions.statusHits
	}
	setFake := func(change func()) {
		fakeSessions.mu.Lock()
		defer fakeSessions.mu.Unlock()
		change()
	}
	// age pretends the last answer for sid is older than it is.
	age := func(sid string, by time.Duration, ttl time.Duration) {
		state, _ := newapiSessions.get(sid)
		state.checked = time.Now().Add(-by)
		newapiSessions.set(sid, state, ttl)
	}

	if _, err := CurrentAuthUser(token("sid-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := CurrentAuthUser(token("sid-a")); err != nil || hits() != 1 {
		t.Fatalf("second check must come from the cache: hits=%d err=%v", hits(), err)
	}
	// Signed out at new-api: refused once the cached answer is older than 30 s.
	setFake(func() { fakeSessions.active["sid-a"] = false })
	if _, err := CurrentAuthUser(token("sid-a")); err != nil {
		t.Fatalf("fresh cache still admits: %v", err)
	}
	age("sid-a", 31*time.Second, time.Minute)
	if _, err := CurrentAuthUser(token("sid-a")); err != ErrSessionExpired {
		t.Fatalf("ended session must be refused, got %v", err)
	}

	// new-api unreachable: the last answer stands for 5 minutes, then the token is refused.
	if _, err := CurrentAuthUser(token("sid-b")); err != nil {
		t.Fatal(err)
	}
	setFake(func() { fakeSessions.down = true })
	age("sid-b", time.Minute, 4*time.Minute)
	if _, err := CurrentAuthUser(token("sid-b")); err != nil {
		t.Fatalf("transient failure must keep the last answer: %v", err)
	}
	age("sid-b", 6*time.Minute, -time.Second)
	if _, err := CurrentAuthUser(token("sid-b")); err != ErrSessionExpired {
		t.Fatalf("after 5 minutes without an answer the token must be refused, got %v", err)
	}
	setFake(func() { fakeSessions.down = false })

	// Tokens without a session (local accounts) are never checked.
	before := hits()
	if _, err := CurrentAuthUser(token("")); err != nil || hits() != before {
		t.Fatalf("local token: hits %d->%d err=%v", before, hits(), err)
	}

	// Logout ends the new-api session and refuses the token right away.
	if _, err := CurrentAuthUser(token("sid-c")); err != nil {
		t.Fatal(err)
	}
	if err := LogoutNewapiSession(token("sid-c")); err != nil {
		t.Fatal(err)
	}
	if len(fakeSessions.logouts) != 1 || fakeSessions.logouts[0] != "sid-c" {
		t.Fatalf("logout calls: %#v", fakeSessions.logouts)
	}
	if _, err := CurrentAuthUser(token("sid-c")); err != ErrSessionExpired {
		t.Fatalf("logged-out token must be refused, got %v", err)
	}
	if err := LogoutNewapiSession(token("")); err != nil || len(fakeSessions.logouts) != 1 {
		t.Fatalf("local token logout must not call new-api: %v %#v", err, fakeSessions.logouts)
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

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestPublicSettingsHidesModelCostsInSSO(t *testing.T) {
	const marker = "NEWAPI_SSO_COSTS_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPublicSettingsHidesModelCostsInSSO$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated costs test: %v\n%s", err, output)
		}
		return
	}
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "test-secret", JWTExpireHours: 1,
		NewapiBaseURL: "http://newapi", NewapiClientID: "infinite", NewapiClientSecret: "s3cret", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Public.ModelChannel.ModelCosts = []model.ModelCost{{Model: "gpt-image-1", Credits: 5}}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	public, err := PublicSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(public.ModelChannel.ModelCosts) != 0 || !public.Auth.NewapiSSO {
		t.Fatalf("expected empty costs and newapiSso=true, got %#v / %v", public.ModelChannel.ModelCosts, public.Auth.NewapiSSO)
	}
}
