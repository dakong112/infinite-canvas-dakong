package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"gorm.io/gorm"
)

// fakeAcademyNewapi charges once per idempotency key like new-api; pat-poor gets 402, pat-member is a member.
type fakeAcademyNewapi struct {
	mu       sync.Mutex
	charged  map[string]int64
	calls    int
	appCalls int
}

func (f *fakeAcademyNewapi) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pat := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.Method + " " + r.URL.Path {
	case "GET /api/status":
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
	case "GET /api/subscription/self":
		if pat == "pat-member" {
			_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[{"subscription":{"plan_id":3,"end_time":4000000000,"amount_total":0}}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[]}}`))
	case "GET /api/subscription/plans":
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	case "POST /api/sso/consume":
		f.calls++
		var body struct {
			Key   string `json:"idempotency_key"`
			Quota int64  `json:"quota"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if pat == "pat-poor" {
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"success":false,"message":"insufficient quota"}`))
			return
		}
		if _, ok := f.charged[body.Key]; !ok {
			f.charged[body.Key] = body.Quota
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1,"balance":1}}`))
	case "GET /api/sso/apps":
		f.appCalls++
		_, _ = w.Write([]byte(`{"success":true,"data":[{"client_id":"infinite","name":"无限画布","launch_url":"https://canvas.example.com"},` +
			`{"client_id":"arcreel","name":"短剧","launch_url":"https://drama.example.com/sso"},{"client_id":"evil","name":"x","launch_url":"javascript:alert(1)"}]}`))
	default:
		http.NotFound(w, r)
	}
}

func TestAcademyEnroll(t *testing.T) {
	if runGenerationTestIsolated(t, "TestAcademyEnroll") {
		return
	}
	fake := &fakeAcademyNewapi{charged: map[string]int64{}}
	server := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer server.Close()
	db := generationTestDB(t)
	config.Cfg.NewapiBaseURL, config.Cfg.NewapiClientID, config.Cfg.NewapiClientSecret, config.Cfg.NewapiRedirectURI = server.URL, "infinite", "x", "https://www.example.com/sso/callback"
	config.Cfg.FileURLSigning = true
	if err := model.SetSecretKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{&model.AcademyCourse{}, &model.AcademyEnrollment{}} {
		if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.SaveSettings(model.Settings{Public: model.PublicSetting{Billing: model.PublicBillingSetting{CreditsPerUnit: 10}}}, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	newUser := func(id string, pat string) model.AuthUser {
		if err := db.Create(&model.User{ID: id, Username: id, AffCode: id, Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: id,
			NewapiAccessToken: pat, NewapiTokenExpiresAt: time.Now().Add(time.Hour).Unix()}).Error; err != nil {
			t.Fatal(err)
		}
		return model.AuthUser{ID: id, Role: model.UserRoleUser}
	}
	plain, member, poor := newUser("plain", "pat-plain"), newUser("member", "pat-member"), newUser("poor", "pat-poor")
	course := func(slug string, edit func(*model.AcademyCourse)) model.AcademyCourse {
		item := model.AcademyCourse{Slug: slug, Title: "课程 " + slug, Category: "AI技能提升", Status: model.AcademyStatusPublished,
			Lessons: []model.AcademyLesson{{Title: "第一课", VideoURL: "/api/files/vid-1/content"}, {Title: "第二课", VideoURL: "https://video.example.com/2"}}}
		edit(&item)
		saved, err := SaveAcademyCourse(0, item)
		if err != nil {
			t.Fatalf("save %s: %v", slug, err)
		}
		return saved
	}
	seats := func(id uint) int {
		item, _, _ := repository.GetAcademyCourse(id)
		return item.EnrolledCount
	}

	// Capacity race: 30 users, 5 seats, a 2-credit fee each; never more than 5 enrollments or charges.
	race := course("race", func(c *model.AcademyCourse) { c.Capacity, c.FeeCredits = 5, 2 })
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for i := range 30 {
		user := newUser(fmt.Sprintf("racer-%d", i), "pat-plain")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := EnrollAcademyCourse(user, "race"); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			} else if !strings.Contains(err.Error(), "名额已满") {
				t.Errorf("race enroll: %v", err)
			}
		}()
	}
	wg.Wait()
	if total, _ := repository.CountAcademyEnrollments(race.ID); won != 5 || total != 5 || seats(race.ID) != 5 || len(fake.charged) != 5 {
		t.Fatalf("race: won=%d rows=%d seats=%d charges=%d", won, total, seats(race.ID), len(fake.charged))
	}
	if list, _ := ListPublishedAcademyCourses(""); len(list) != 1 || list[0].State != model.AcademyStateFull || list[0].Lessons != nil {
		t.Fatalf("full course in the list: %#v", list)
	}

	// Replay: the same user twice is one charge (key ac:<course>:<user>, 2 credits = 100000 quota) and one row.
	paid := course("paid", func(c *model.AcademyCourse) { c.FeeCredits = 2 })
	first, err := EnrollAcademyCourse(plain, "paid")
	if err != nil || first.FeeStatus != model.AcademyFeeCharged || first.FeeQuota != 100000 {
		t.Fatalf("paid enroll: %#v %v", first, err)
	}
	callsBefore := fake.calls
	again, err := EnrollAcademyCourse(plain, "paid")
	if err != nil || again.ID != first.ID || fake.calls != callsBefore || seats(paid.ID) != 1 {
		t.Fatalf("second enroll: %#v %v calls=%d seats=%d", again, err, fake.calls-callsBefore, seats(paid.ID))
	}
	if quota, ok := fake.charged[fmt.Sprintf("ac:%d:plain", paid.ID)]; !ok || quota != 100000 {
		t.Fatalf("charge key: %v", fake.charged)
	}
	// A lost row after a charge: the retry replays the key (new-api charges once) and records the enrollment.
	// (A failed insert gives the seat back, as below.)
	if err := db.Delete(&model.AcademyEnrollment{}, first.ID).Error; err != nil || repository.ReleaseAcademySeat(paid.ID) != nil {
		t.Fatal(err)
	}
	charges := len(fake.charged)
	if retried, err := EnrollAcademyCourse(plain, "paid"); err != nil || retried.FeeStatus != model.AcademyFeeCharged || len(fake.charged) != charges {
		t.Fatalf("retry after a lost row: %#v %v", retried, err)
	}

	// 402 gives the seat back and says 积分不足.
	tight := course("tight", func(c *model.AcademyCourse) { c.Capacity, c.FeeCredits = 1, 2 })
	if _, err := EnrollAcademyCourse(poor, "tight"); err == nil || !strings.Contains(err.Error(), "积分不足") {
		t.Fatalf("poor enroll: %v", err)
	}
	if total, _ := repository.CountAcademyEnrollments(tight.ID); seats(tight.ID) != 0 || total != 0 {
		t.Fatalf("seat not released: %d rows=%d", seats(tight.ID), total)
	}
	if _, err := EnrollAcademyCourse(plain, "tight"); err != nil {
		t.Fatalf("released seat not usable: %v", err)
	}

	// Member-only gating, and member-free courses cost members nothing.
	course("members", func(c *model.AcademyCourse) { c.MemberOnly, c.FeeCredits, c.MemberFree = true, 5, true })
	if _, err := EnrollAcademyCourse(plain, "members"); err == nil || !strings.Contains(err.Error(), "仅限会员") {
		t.Fatalf("non-member enroll: %v", err)
	}
	if got, err := EnrollAcademyCourse(member, "members"); err != nil || got.FeeStatus != model.AcademyFeeFree || got.FeeQuota != 0 {
		t.Fatalf("member enroll: %#v %v", got, err)
	}

	// Ended courses refuse new enrollments.
	past := time.Now().Add(-time.Hour)
	course("ended", func(c *model.AcademyCourse) { c.EndsAt = &past })
	if _, err := EnrollAcademyCourse(plain, "ended"); err == nil || !strings.Contains(err.Error(), "报名已结束") {
		t.Fatalf("ended enroll: %v", err)
	}

	// Lessons: hidden from strangers and anonymous visitors of paid courses, signed for enrolled users; free courses
	// show links to everyone but uploaded files only to enrolled users.
	view := func(user *model.AuthUser, slug string) model.AcademyCourse {
		ctx := context.Background()
		if user != nil {
			ctx = WithUser(ctx, *user)
		}
		item, err := GetPublishedAcademyCourse(ctx, slug)
		if err != nil {
			t.Fatalf("view %s: %v", slug, err)
		}
		return item
	}
	if item := view(nil, "paid"); !item.Locked || item.Lessons[0].VideoURL != "" || item.Lessons[1].VideoURL != "" || item.Lessons[0].Title != "第一课" {
		t.Errorf("anonymous paid view: %#v", item)
	}
	if item := view(&poor, "paid"); !item.Locked || item.Lessons[1].VideoURL != "" {
		t.Errorf("stranger paid view: %#v", item)
	}
	if item := view(&plain, "paid"); item.Locked || !item.Enrolled || !strings.Contains(item.Lessons[0].VideoURL, "sig=") || item.Lessons[1].VideoURL != "https://video.example.com/2" {
		t.Errorf("enrolled view: %#v", item)
	}
	course("free", func(c *model.AcademyCourse) {})
	if item := view(nil, "free"); item.Locked || item.Lessons[0].VideoURL != "" || item.Lessons[1].VideoURL == "" {
		t.Errorf("free view: %#v", item)
	}
	if _, err := EnrollAcademyCourse(plain, "free"); err != nil {
		t.Fatalf("free enroll: %v", err)
	}
	if item := view(&plain, "free"); !strings.Contains(item.Lessons[0].VideoURL, "sig=") {
		t.Errorf("enrolled free view: %#v", item)
	}
	course("draft", func(c *model.AcademyCourse) { c.Status = model.AcademyStatusDraft })
	if _, err := GetPublishedAcademyCourse(context.Background(), "draft"); err == nil {
		t.Error("draft course is public")
	}

	// Saving never takes the seat counter from the input; courses with enrollments cannot be deleted.
	paid.EnrolledCount, paid.Title = 0, "改名"
	if _, err := SaveAcademyCourse(paid.ID, paid); err != nil || seats(paid.ID) != 1 {
		t.Fatalf("update kept seats: %v %d", err, seats(paid.ID))
	}
	if err := DeleteAcademyCourse(paid.ID); err == nil {
		t.Error("deleted a course with enrollments")
	}
	if _, err := SaveAcademyCourse(0, model.AcademyCourse{Slug: "x", Title: "t", Category: "c"}); err == nil {
		t.Error("bad slug accepted")
	}
	bad := model.AcademyCourse{Slug: "bad-video", Title: "t", Category: "c", Lessons: []model.AcademyLesson{{Title: "a", VideoURL: "javascript:alert(1)"}}}
	if _, err := SaveAcademyCourse(0, bad); err == nil {
		t.Error("javascript video url accepted")
	}
	if rows, err := AdminListAcademyEnrollments(paid.ID); err != nil || len(rows) != 1 || rows[0].Username != "plain" {
		t.Errorf("admin enrollments: %#v %v", rows, err)
	}

	// Apps: infinite itself and non-http launch URLs are dropped, and the list is cached per user.
	apps, err := AccountAppsForUser("plain")
	if err != nil || len(apps) != 1 || apps[0].ClientID != "arcreel" || apps[0].LaunchURL != "https://drama.example.com/sso" {
		t.Fatalf("apps: %#v %v", apps, err)
	}
	if _, err := AccountAppsForUser("plain"); err != nil || fake.appCalls != 1 {
		t.Errorf("apps not cached: %d calls %v", fake.appCalls, err)
	}
	if _, err := AccountAppsForUser("member"); err != nil || fake.appCalls != 2 {
		t.Errorf("apps cache shared between users: %d calls %v", fake.appCalls, err)
	}

	// Seeds: four drafts once, never on a non-empty table.
	if err := EnsureDefaultAcademyCourses(); err != nil {
		t.Fatal(err)
	}
	if all, _ := AdminListAcademyCourses(); len(all) != 7 {
		t.Error("seeded into a non-empty academy")
	}
	db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.AcademyCourse{})
	if err := EnsureDefaultAcademyCourses(); err != nil {
		t.Fatal(err)
	}
	all, _ := AdminListAcademyCourses()
	if len(all) != 4 || all[0].Status != model.AcademyStatusDraft {
		t.Fatalf("seeds: %#v", all)
	}
	for _, item := range all {
		if err := normalizeAcademyCourse(&item); err != nil {
			t.Errorf("seed %s invalid: %v", item.Slug, err)
		}
	}
}
