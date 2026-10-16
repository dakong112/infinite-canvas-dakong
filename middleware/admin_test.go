package middleware_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"github.com/tigerowo/infinite-canvas/router"
	"github.com/tigerowo/infinite-canvas/service"
)

// fakeTeamsNewapi answers the new-api SSO endpoints the permission and space code uses; users are told apart by PAT.
type fakeTeamsNewapi struct {
	mu          sync.Mutex
	opsCodes    []string
	failPerms   bool
	teamKeyHits int
}

func (f *fakeTeamsNewapi) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	pat := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/sso/") && !strings.Contains(string(body), `"client_secret":"x"`) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"message":"invalid client credentials"}`))
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/sso/permissions":
		if f.failPerms {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		codes, _ := json.Marshal(f.opsCodes)
		if pat != "pat-ops" {
			codes = []byte("[]")
		}
		_, _ = fmt.Fprintf(w, `{"success":true,"data":{"permissions":%s}}`, codes)
	case "POST /api/sso/teams":
		if pat == "pat-a" || pat == "pat-b" {
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":7,"name":"设计组","role":"member","limit_quota":500000,"balance":1000000,"key_remain_quota":250000}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	case "POST /api/sso/team-key":
		if (pat != "pat-a" && pat != "pat-b") || !strings.Contains(string(body), `"team_id":7`) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"message":"team not found"}`))
			return
		}
		f.teamKeyHits++
		_, _ = fmt.Fprintf(w, `{"success":true,"data":{"key":"sk-team-%d"}}`, f.teamKeyHits)
	case "GET /api/status":
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
	case "GET /api/user/self":
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":500000,"used_quota":0,"group":"default"}}`))
	case "GET /api/subscription/self":
		_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[]}}`))
	case "GET /api/sso/client":
		_, _ = w.Write([]byte(`{"success":true,"data":{"name":"p"}}`))
	default:
		http.NotFound(w, r)
	}
}

func TestPermissionsAndSpaces(t *testing.T) {
	const marker = "PERMISSIONS_SPACES_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPermissionsAndSpaces$", "-test.timeout=55s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated permissions test: %v\n%s", err, output)
		}
		return
	}
	fake := &fakeTeamsNewapi{opsCodes: []string{model.PermUserRead}}
	server := httptest.NewServer(http.HandlerFunc(fake.handle))
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
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Public.Billing.CreditsPerUnit = 10
	settings.Private.Channels = []model.ModelChannel{{ID: "na", Name: "new-api", Protocol: "openai", BaseURL: server.URL, APIKey: "sk-admin", Enabled: true, Weight: 1}}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	future, fresh := time.Now().Add(time.Hour).Unix(), time.Now().Unix()
	users := []model.User{
		{ID: "local", Role: model.UserRoleAdmin},
		{ID: "ops", NewapiID: "10", NewapiAccessToken: "pat-ops", Permissions: []string{model.PermTemplateRead}, PermissionsAt: fresh},
		// A new-api admin whose application grants no canvas code: the role alone opens nothing.
		{ID: "napadmin", Role: model.UserRoleAdmin, NewapiID: "11", NewapiAccessToken: "pat-none", Permissions: []string{}, PermissionsAt: fresh},
		{ID: "a", NewapiID: "20", NewapiAccessToken: "pat-a", NewapiAPIKey: "sk-login-a"},
		{ID: "b", NewapiID: "21", NewapiAccessToken: "pat-b", CurrentTeamID: 7},
		{ID: "c", NewapiID: "22", NewapiAccessToken: "pat-c"},
		{ID: "d", NewapiID: "23", NewapiAccessToken: "pat-d", CurrentTeamID: 7},
	}
	tokens := map[string]string{}
	for _, user := range users {
		user.Username, user.AffCode, user.Status, user.NewapiTokenExpiresAt = user.ID, user.ID, model.UserStatusActive, future
		if user.Role == "" {
			user.Role = model.UserRoleUser
		}
		if _, err := repository.SaveUser(user); err != nil {
			t.Fatal(err)
		}
		tokens[user.ID], _ = jwt.NewWithClaims(jwt.SigningMethodHS256, service.TokenClaims{UserID: user.ID,
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}).SignedString([]byte("s"))
	}
	gin.SetMode(gin.TestMode)
	engine := router.New()
	call := func(userID string, method string, path string, body string, header ...string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+tokens[userID])
		request.Header.Set("Content-Type", "application/json")
		if len(header) == 2 {
			request.Header.Set(header[0], header[1])
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	// AdminAuth + RequirePermission matrix.
	for _, tc := range []struct {
		user, path string
		status     int
	}{
		{"local", "/api/admin/settings", http.StatusOK},
		{"local", "/api/admin/users", http.StatusOK},
		{"ops", "/api/admin/templates", http.StatusOK},
		{"ops", "/api/admin/settings", http.StatusForbidden},
		{"ops", "/api/admin/users", http.StatusForbidden},
		{"ops", "/api/admin/templates/fees", http.StatusForbidden},
		{"napadmin", "/api/admin/templates", http.StatusUnauthorized},
		{"a", "/api/admin/templates", http.StatusUnauthorized},
	} {
		if status, body := call(tc.user, http.MethodGet, tc.path, ""); status != tc.status {
			t.Errorf("%s GET %s: %d %s, want %d", tc.user, tc.path, status, body, tc.status)
		}
	}
	// Publishing needs canvas.template.publish on top of write.
	ctx := service.WithUser(context.Background(), model.AuthUser{ID: "ops", Permissions: []string{model.PermTemplateWrite}})
	if err := service.RequireTemplatePublish(ctx, "", model.ImageTemplateStatusPublished); err == nil {
		t.Error("publishing without canvas.template.publish")
	}
	if err := service.RequireTemplatePublish(ctx, "", model.ImageTemplateStatusDraft); err != nil {
		t.Errorf("saving a draft: %v", err)
	}

	// Codes older than a minute are fetched again: new-api now grants users, not templates.
	if err := repository.SaveUserPermissions("ops", []string{model.PermTemplateRead}, time.Now().Add(-2*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if status, _ := call("ops", http.MethodGet, "/api/admin/users", ""); status != http.StatusOK {
		t.Errorf("refreshed codes not applied: %d", status)
	}
	if status, _ := call("ops", http.MethodGet, "/api/admin/templates", ""); status != http.StatusForbidden {
		t.Errorf("revoked code still works: %d", status)
	}
	if _, body := call("ops", http.MethodGet, "/api/auth/me", ""); !strings.Contains(body, `"permissions":["canvas.user.read"]`) || !strings.Contains(body, `"isLocalAdmin":false`) {
		t.Errorf("auth/me: %s", body)
	}
	// new-api down: the last codes keep working for ten minutes, then nothing is granted.
	fake.mu.Lock()
	fake.failPerms = true
	fake.mu.Unlock()
	if err := repository.SaveUserPermissions("ops", []string{model.PermUserRead}, time.Now().Add(-5*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if status, _ := call("ops", http.MethodGet, "/api/admin/users", ""); status != http.StatusOK {
		t.Errorf("stale codes within the grace period: %d", status)
	}
	if err := repository.SaveUserPermissions("ops", []string{model.PermUserRead}, time.Now().Add(-11*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	if status, _ := call("ops", http.MethodGet, "/api/admin/users", ""); status != http.StatusUnauthorized {
		t.Errorf("stale codes past the grace period: %d", status)
	}

	// Spaces: only teams new-api lists can be entered.
	if _, body := call("a", http.MethodGet, "/api/spaces", ""); !strings.Contains(body, `"current":""`) || !strings.Contains(body, `"id":"team:7"`) {
		t.Errorf("spaces: %s", body)
	}
	if _, body := call("a", http.MethodPut, "/api/space", `{"teamId":9}`); !strings.Contains(body, "不是该团队的成员") {
		t.Errorf("switching to a foreign team: %s", body)
	}
	if _, body := call("a", http.MethodPut, "/api/space", `{"teamId":7}`); !strings.Contains(body, `"current":"team:7"`) {
		t.Errorf("switching to team 7: %s", body)
	}
	// A member who left goes back to the personal space.
	if _, body := call("d", http.MethodGet, "/api/auth/me", ""); !strings.Contains(body, `"spaceId":""`) {
		t.Errorf("former member space: %s", body)
	}
	if stored, _, _ := repository.GetUserByID("d"); stored.CurrentTeamID != 0 {
		t.Errorf("former member keeps team %d", stored.CurrentTeamID)
	}
	// A page still showing the personal space cannot write into the team space.
	if status, body := call("a", http.MethodGet, "/api/v1/canvas/projects", "", "X-Space-Id", "personal"); status != http.StatusConflict {
		t.Errorf("space guard: %d %s", status, body)
	}

	// Team space billing: any platform group becomes the member's team key, stored and renewed once on rejection.
	memberA := model.AuthUser{ID: "a", SpaceID: "team:7"}
	channel, err := service.SelectModelChannelForUser("a", "gpt-4o", service.SpaceChannelID(memberA, "newapi@vip"), true)
	if err != nil || channel.ID != "newapi@team:7" || channel.APIKey != "sk-team-1" {
		t.Fatalf("team channel: %#v %v", channel, err)
	}
	if stored, ok, _ := repository.GetNewapiGroupKey("a", "team:7"); !ok || stored.APIKey != "sk-team-1" {
		t.Errorf("team key not stored: %#v", stored)
	}
	if again, _ := service.SelectModelChannelForUser("a", "gpt-4o", "newapi@team:7", true); again.APIKey != "sk-team-1" {
		t.Errorf("stored team key not reused: %#v", again)
	}
	renewed, ok, err := service.RenewNewapiGroupChannel("a", "gpt-4o", channel)
	if err != nil || !ok || renewed.APIKey != "sk-team-2" || renewed.ID != "newapi@team:7" {
		t.Errorf("team key renewal: %#v %v %v", renewed, ok, err)
	}
	// Personal space keeps the picked group; a team channel asked for outside its space falls back to the account group.
	if got := service.SpaceChannelID(model.AuthUser{ID: "a"}, "newapi@vip"); got != "newapi@vip" {
		t.Errorf("personal channel: %s", got)
	}
	if got := service.SpaceChannelID(model.AuthUser{ID: "c"}, "newapi@team:7"); got != "newapi@" {
		t.Errorf("team channel outside the space: %s", got)
	}
	if _, err := service.SelectModelChannelForUser("c", "gpt-4o", "newapi@team:7", true); err == nil || !strings.Contains(err.Error(), "不在该团队") {
		t.Errorf("non-member team key: %v", err)
	}
	summary, err := service.AccountSummaryForUser("a")
	if err != nil || summary.Team == nil || summary.Team.Balance != 20 || summary.Team.Remaining != 5 || summary.Team.Unlimited {
		t.Errorf("team summary: %#v %v", summary.Team, err)
	}

	// Space-scoped canvas projects: team members share them, other spaces never see them.
	project := func(id string) string {
		return `{"data":{"id":"` + id + `","title":"t","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}}`
	}
	if _, body := call("a", http.MethodPost, "/api/v1/canvas/projects", project("team-p")); !strings.Contains(body, `"code":0`) {
		t.Fatalf("save team project: %s", body)
	}
	if _, body := call("b", http.MethodGet, "/api/v1/canvas/projects", ""); !strings.Contains(body, "team-p") {
		t.Errorf("teammate cannot see the team project: %s", body)
	}
	if _, body := call("c", http.MethodGet, "/api/v1/canvas/projects", ""); strings.Contains(body, "team-p") {
		t.Errorf("outsider sees the team project: %s", body)
	}
	call("a", http.MethodPut, "/api/space", `{"teamId":0}`)
	if _, body := call("a", http.MethodGet, "/api/v1/canvas/projects", ""); strings.Contains(body, "team-p") {
		t.Errorf("personal space shows the team project: %s", body)
	}
	call("a", http.MethodPost, "/api/v1/canvas/projects", project("own-p"))
	// Deleting from the personal space never touches a team row with the same id.
	call("a", http.MethodPost, "/api/v1/canvas/projects/delete", `{"ids":["team-p"]}`)
	if _, body := call("b", http.MethodGet, "/api/v1/canvas/projects", ""); !strings.Contains(body, "team-p") || strings.Contains(body, "own-p") {
		t.Errorf("teammate view after a personal delete: %s", body)
	}
	call("b", http.MethodPost, "/api/v1/canvas/projects/delete", `{"ids":["team-p"]}`)
	if _, body := call("b", http.MethodGet, "/api/v1/canvas/projects", ""); strings.Contains(body, "team-p") {
		t.Errorf("teammate could not delete the team project: %s", body)
	}

	// Works: team jobs are listed and deletable by members only; personal ones stay with their owner.
	now := time.Now().UTC()
	jobs := []model.GenerationJob{
		{ID: "job-team", UserID: "a", SpaceID: "team:7", Kind: model.GenerationKindImage, Source: model.GenerationSourceImage, Status: model.GenerationStatusFailed, CreatedAt: now, NextRunAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "job-own", UserID: "a", Kind: model.GenerationKindImage, Source: model.GenerationSourceImage, Status: model.GenerationStatusFailed, CreatedAt: now, NextRunAt: now, ExpiresAt: now.Add(time.Hour)},
	}
	if err := repository.CreateGenerationBatch(model.GenerationBatch{ID: "batch-team", UserID: "a", SpaceID: "team:7", CreatedAt: now}, jobs); err != nil {
		t.Fatal(err)
	}
	if page, err := service.ListWorks("team:7", service.WorksQuery{}); err != nil || len(page.Items) != 1 || page.Items[0].ID != "job-team" {
		t.Errorf("team works: %#v %v", page.Items, err)
	}
	if page, _ := service.ListWorks("a", service.WorksQuery{}); len(page.Items) != 1 || page.Items[0].ID != "job-own" {
		t.Errorf("personal works: %#v", page.Items)
	}
	if found, _ := service.GenerationJobsByIDs("c", []string{"job-team", "job-own"}); len(found) != 0 {
		t.Errorf("outsider reads jobs: %#v", found)
	}
	if err := service.DeleteWork(context.Background(), "c", "job-team"); err == nil {
		t.Error("outsider deleted a team work")
	}
	if err := service.DeleteWork(service.WithUser(context.Background(), model.AuthUser{ID: "b", SpaceID: "team:7"}), "team:7", "job-team"); err != nil {
		t.Errorf("teammate delete: %v", err)
	}
}
