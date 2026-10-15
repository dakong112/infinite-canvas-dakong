package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"gorm.io/gorm"
)

// fakeConsume records /api/sso/consume calls of the fake new-api; failures makes the next calls return
// status (500 when 0).
var fakeConsume struct {
	mu       sync.Mutex
	bodies   []string
	failures int
	status   int
}

func failConsume(times int, status int) {
	fakeConsume.mu.Lock()
	defer fakeConsume.mu.Unlock()
	fakeConsume.failures, fakeConsume.status = times, status
}

func consumeBodies() []string {
	fakeConsume.mu.Lock()
	defer fakeConsume.mu.Unlock()
	return slices.Clone(fakeConsume.bodies)
}

func templateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := generationTestDB(t)
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.ImageTemplate{}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func validTestTemplate(slug string) model.ImageTemplate {
	return model.ImageTemplate{
		Slug: slug, Title: "测试模板", Category: "ecommerce", Status: model.ImageTemplateStatusPublished,
		Inputs: []model.ImageTemplateInput{{Key: "product", Label: "商品图", Max: 2, Required: true}, {Key: "model", Label: "模特图", Max: 1}},
		Prompt: "把 @图片1 的商品放到模特身上。{{prompt}}", Ratios: []string{"3:4", "1:1"}, Resolutions: []string{"2K"},
	}
}

func TestImageTemplateAdminAndPublic(t *testing.T) {
	if runGenerationTestIsolated(t, "TestImageTemplateAdminAndPublic") {
		return
	}
	templateTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{ImageModel: "gpt-image-1"}}, "gpt-image-1")

	t.Run("seeds are valid drafts, inserted once", func(t *testing.T) {
		for range 2 {
			if err := EnsureDefaultImageTemplates(); err != nil {
				t.Fatal(err)
			}
		}
		items, err := AdminListImageTemplates()
		if err != nil || len(items) != 12 {
			t.Fatalf("seeded %d: %v", len(items), err)
		}
		for _, item := range items {
			if item.Status != model.ImageTemplateStatusDraft {
				t.Fatalf("%s seeded as %s", item.Slug, item.Status)
			}
			if err := normalizeImageTemplate(&item); err != nil {
				t.Errorf("seed %s invalid: %v", item.Slug, err)
			}
		}
		if public, err := ListPublishedImageTemplates(""); err != nil || len(public) != 0 {
			t.Fatalf("drafts listed publicly: %d %v", len(public), err)
		}
		if _, err := GetPublishedImageTemplate("outfit-scenes"); err == nil {
			t.Fatal("draft readable publicly")
		}
	})

	t.Run("validation", func(t *testing.T) {
		rejected := map[string]func(*model.ImageTemplate){
			"bad slug":   func(i *model.ImageTemplate) { i.Slug = "Bad_Slug" },
			"short slug": func(i *model.ImageTemplate) { i.Slug = "a" },
			"five groups": func(i *model.ImageTemplate) {
				i.Inputs = slices.Repeat([]model.ImageTemplateInput{{Key: "a", Label: "a", Max: 1}}, 5)
			},
			"duplicate keys": func(i *model.ImageTemplate) { i.Inputs[1].Key = "product" },
			"max 5":          func(i *model.ImageTemplate) { i.Inputs[0].Max = 5 },
			"max 0":          func(i *model.ImageTemplate) { i.Inputs[0].Max = 0 },
			"total 7": func(i *model.ImageTemplate) {
				i.Inputs = append(i.Inputs, model.ImageTemplateInput{Key: "c", Label: "c", Max: 4})
			},
			"bad ratio":        func(i *model.ImageTemplate) { i.Ratios = []string{"5:7"} },
			"no resolution":    func(i *model.ImageTemplate) { i.Resolutions = nil },
			"bad resolution":   func(i *model.ImageTemplate) { i.Resolutions = []string{"8K"} },
			"long title":       func(i *model.ImageTemplate) { i.Title = strings.Repeat("长", 101) },
			"bad category":     func(i *model.ImageTemplate) { i.Category = "misc" },
			"empty prompt":     func(i *model.ImageTemplate) { i.Prompt = " " },
			"image ref beyond": func(i *model.ImageTemplate) { i.Prompt = "@图片4" },
			"negative fee":     func(i *model.ImageTemplate) { i.FeeCredits = -1 },
			"fee over 10 USD":  func(i *model.ImageTemplate) { i.FeeCredits = 10.01 }, // 1 credit per unit by default
			"script cover":     func(i *model.ImageTemplate) { i.CoverURL = "javascript:alert(1)" },
			"relative cover":   func(i *model.ImageTemplate) { i.CoverURL = "/etc/cover.png" },
			"hostless cover":   func(i *model.ImageTemplate) { i.CoverURL = "https:///a.png" },
			"ftp example":      func(i *model.ImageTemplate) { i.Examples = []model.ImageTemplateExample{{Output: "ftp://x.com/a.png"}} },
			"data example": func(i *model.ImageTemplate) {
				i.Examples = []model.ImageTemplateExample{{Inputs: []string{"data:image/png;base64,AA"}}}
			},
		}
		for name, mutate := range rejected {
			item := validTestTemplate("tpl-valid")
			mutate(&item)
			if _, err := SaveImageTemplate("", item); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}
		item := validTestTemplate("tpl-urls")
		item.CoverURL, item.FeeCredits, item.Status = "https://cdn.example.com/a.png", 10, model.ImageTemplateStatusDraft
		item.Examples = []model.ImageTemplateExample{{Inputs: []string{"/api/files/abc", "http://x.com/b.png"}, Output: "/api/files/out"}}
		if _, err := SaveImageTemplate("", item); err != nil {
			t.Fatalf("valid urls and a 10 USD fee rejected: %v", err)
		}
	})

	t.Run("crud and public view", func(t *testing.T) {
		saved, err := SaveImageTemplate("", validTestTemplate("tpl-one"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := SaveImageTemplate("", validTestTemplate("tpl-one")); err == nil {
			t.Fatal("duplicate slug accepted")
		}
		draft := validTestTemplate("tpl-draft")
		draft.Status = "whatever"
		if saved, err := SaveImageTemplate("", draft); err != nil || saved.Status != model.ImageTemplateStatusDraft {
			t.Fatalf("unknown status must save as draft: %#v %v", saved, err)
		}
		public, err := ListPublishedImageTemplates("ecommerce")
		if err != nil || len(public) != 1 || public[0].Slug != "tpl-one" || public[0].Prompt != "" || public[0].Model != "gpt-image-1" {
			t.Fatalf("public list: %#v %v", public, err)
		}
		if creative, _ := ListPublishedImageTemplates("creative"); len(creative) != 0 {
			t.Fatalf("category filter: %#v", creative)
		}
		if _, err := GetPublishedImageTemplate("tpl-draft"); err == nil {
			t.Fatal("draft readable by slug")
		}
		update := validTestTemplate("tpl-renamed")
		update.Title = "改名"
		updated, err := SaveImageTemplate(saved.ID, update)
		if err != nil || updated.ID != saved.ID || !updated.CreatedAt.Equal(saved.CreatedAt) {
			t.Fatalf("update: %#v %v", updated, err)
		}
		if detail, err := GetPublishedImageTemplate("tpl-renamed"); err != nil || detail.Title != "改名" || detail.Prompt != "" {
			t.Fatalf("detail: %#v %v", detail, err)
		}
		if _, err := SaveImageTemplate("imgtpl-missing", update); err == nil {
			t.Fatal("update of a missing template accepted")
		}
		if err := DeleteImageTemplate(saved.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := GetPublishedImageTemplate("tpl-renamed"); err == nil {
			t.Fatal("deleted template still readable")
		}
	})
}

func TestImageTemplateGenerate(t *testing.T) {
	if runGenerationTestIsolated(t, "TestImageTemplateGenerate") {
		return
	}
	templateTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{ImageModel: "gpt-image-1"}}, "gpt-image-1")
	u1, u2 := model.AuthUser{ID: "u1", Role: model.UserRoleUser}, model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	ctx1, ctx2 := WithUser(context.Background(), u1), WithUser(context.Background(), u2)
	product, err := UploadStorageObject(ctx1, "p.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	person, err := UploadStorageObject(ctx1, "m.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	item := validTestTemplate("tpl-gen")
	item.FeeCredits = 5
	tpl, err := SaveImageTemplate("", item)
	if err != nil {
		t.Fatal(err)
	}

	batch, jobs, err := GenerateWithImageTemplate(ctx1, u1, "tpl-gen", TemplateGenerateInput{
		Files: map[string][]string{"model": {person.ID}, "product": {product.ID}}, Prompt: " 红色背景 ", Ratio: "3:4", Resolution: "2K", Count: 2,
	})
	if err != nil || len(jobs) != 2 || batch.Title != "测试模板" || batch.Source != model.GenerationSourceTemplate {
		t.Fatalf("generate: %#v %#v %v", batch, jobs, err)
	}
	job := jobs[0]
	// Non-SSO users keep the admin channel and never pay the template fee.
	if job.Endpoint != "/images/edits" || job.Model != "gpt-image-1" || job.ChannelID != "c1" || job.Source != model.GenerationSourceTemplate ||
		job.TemplateID != tpl.ID || job.FeeStatus != model.TemplateFeeWaived || job.Prompt != "红色背景" || !slices.Equal(job.InputFileIDs, []string{product.ID, person.ID}) {
		t.Fatalf("job: %#v", job)
	}
	for _, want := range []string{`"size":"1776x2368"`, `"quality":"medium"`, "图片编号按上传顺序：@图片1 为「商品图」；@图片2 为「模特图」。", "把 @图片1 的商品放到模特身上。红色背景"} {
		if !strings.Contains(job.RequestJSON, want) {
			t.Errorf("request lacks %s: %s", want, job.RequestJSON)
		}
	}

	t.Run("works filter by kind and template", func(t *testing.T) {
		for _, query := range []WorksQuery{{Kind: "template"}, {TemplateID: tpl.ID}} {
			if page, err := ListWorks("u1", query); err != nil || len(page.Items) != 2 || page.Counts.Running != 2 {
				t.Fatalf("works %#v: %#v %v", query, page, err)
			}
		}
		if page, _ := ListWorks("u1", WorksQuery{Kind: "tool"}); len(page.Items) != 0 {
			t.Fatalf("template jobs listed as tools: %#v", page.Items)
		}
	})

	t.Run("an empty user prompt keeps the system prompt private", func(t *testing.T) {
		_, jobs, err := GenerateWithImageTemplate(ctx1, u1, "tpl-gen", TemplateGenerateInput{Files: map[string][]string{"product": {product.ID}}})
		if err != nil || jobs[0].Prompt != "" || !strings.Contains(jobs[0].RequestJSON, `"size":"1776x2368"`) {
			t.Fatalf("defaults: %#v %v", jobs, err)
		}
	})

	t.Run("text-only templates call generations", func(t *testing.T) {
		textOnly := validTestTemplate("tpl-text")
		textOnly.Inputs, textOnly.Prompt, textOnly.PromptRequired, textOnly.Ratios, textOnly.Resolutions = nil, "画一张海报：{{prompt}}", true, []string{"16:9"}, []string{"1K"}
		if _, err := SaveImageTemplate("", textOnly); err != nil {
			t.Fatal(err)
		}
		_, jobs, err := GenerateWithImageTemplate(ctx1, u1, "tpl-text", TemplateGenerateInput{Prompt: "中秋"})
		if err != nil || jobs[0].Endpoint != "/images/generations" || !strings.Contains(jobs[0].RequestJSON, `"size":"1280x720"`) || !strings.Contains(jobs[0].RequestJSON, `"quality":"low"`) {
			t.Fatalf("text-only: %#v %v", jobs, err)
		}
		if _, _, err := GenerateWithImageTemplate(ctx1, u1, "tpl-text", TemplateGenerateInput{}); err == nil {
			t.Fatal("required prompt missing accepted")
		}
	})

	t.Run("rejections", func(t *testing.T) {
		txt, err := UploadStorageObject(ctx1, "a.txt", "text/plain", []byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		draft := validTestTemplate("tpl-hidden")
		draft.Status = model.ImageTemplateStatusDraft
		if _, err := SaveImageTemplate("", draft); err != nil {
			t.Fatal(err)
		}
		files := map[string][]string{"product": {product.ID}}
		rejected := map[string]TemplateGenerateInput{
			"missing required input": {Files: map[string][]string{"model": {person.ID}}},
			"unknown input":          {Files: map[string][]string{"product": {product.ID}, "logo": {person.ID}}},
			"too many files":         {Files: map[string][]string{"product": {product.ID}, "model": {person.ID, product.ID}}},
			"non-image file":         {Files: map[string][]string{"product": {txt.ID}}},
			"long prompt":            {Files: files, Prompt: strings.Repeat("字", 2001)},
			"ratio not allowed":      {Files: files, Ratio: "16:9"},
			"resolution not allowed": {Files: files, Resolution: "4K"},
			"count 5":                {Files: files, Count: 5},
		}
		for name, input := range rejected {
			if _, _, err := GenerateWithImageTemplate(ctx1, u1, "tpl-gen", input); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}
		if _, _, err := GenerateWithImageTemplate(ctx2, u2, "tpl-gen", TemplateGenerateInput{Files: files}); err == nil || err.Error() != "图片不存在" {
			t.Fatalf("u2 used u1's file: %v", err)
		}
		if _, _, err := GenerateWithImageTemplate(ctx1, u1, "tpl-hidden", TemplateGenerateInput{Files: files}); err == nil || err.Error() != "模板不存在" {
			t.Fatalf("draft generated: %v", err)
		}
	})
}

// SSO users pay the template fee once per succeeded job through new-api /api/sso/consume.
func TestTemplateFeeSettlement(t *testing.T) {
	if runGenerationTestIsolated(t, "TestTemplateFeeSettlement") {
		return
	}
	server := fakeNewapiAccount(t, &callLog{})
	db := templateTestDB(t)
	config.Cfg.NewapiBaseURL, config.Cfg.NewapiClientID, config.Cfg.NewapiClientSecret, config.Cfg.NewapiRedirectURI = server.URL, "infinite", "x", "https://www.example.com/sso/callback"
	if err := model.SetSecretKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	// The fake reports testPAT as an active member with 30 credits (10 credits per unit).
	if err := db.Create(&model.User{ID: "g1", Username: "g1", AffCode: "G1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "50",
		NewapiAccessToken: testPAT, NewapiAPIKey: "sk-login", NewapiTokenExpiresAt: time.Now().Add(time.Hour).Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	settings := model.Settings{
		Public:  model.PublicSetting{Billing: model.PublicBillingSetting{CreditsPerUnit: 10}, Ecommerce: model.PublicEcommerceSetting{ImageModel: "gpt-image-1"}},
		Private: model.PrivateSetting{Channels: []model.ModelChannel{{ID: "na", Protocol: "openai", Name: "new-api", BaseURL: server.URL, APIKey: "sk-admin", Models: []string{"gpt-image-1"}, Weight: 1, Enabled: true}}},
	}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	user := model.AuthUser{ID: "g1", Role: model.UserRoleUser}
	ctx := WithUser(context.Background(), user)
	product, err := UploadStorageObject(ctx, "p.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	newTemplate := func(slug string, fee float64, memberFree bool) {
		item := validTestTemplate(slug)
		item.Title, item.FeeCredits, item.MemberFree = "付费模板", fee, memberFree
		if _, err := SaveImageTemplate("", item); err != nil {
			t.Fatal(err)
		}
	}
	newTemplate("paid", 1.5, false)
	newTemplate("member-free", 1.5, true)
	newTemplate("free", 0, false)
	newTemplate("pricey", 20, false)
	files := map[string][]string{"product": {product.ID}}

	_, paid, err := GenerateWithImageTemplate(ctx, user, "paid", TemplateGenerateInput{Files: files, Count: 3})
	if err != nil || len(paid) != 3 {
		t.Fatal(err)
	}
	// 1.5 credits / 10 credits per unit * 500,000 quota per unit.
	if paid[0].FeeStatus != model.TemplateFeePending || paid[0].FeeQuota != 75000 || paid[0].ChannelID != "newapi@vip" {
		t.Fatalf("paid job: %#v", paid[0])
	}
	for _, slug := range []string{"member-free", "free"} {
		if _, jobs, err := GenerateWithImageTemplate(ctx, user, slug, TemplateGenerateInput{Files: files}); err != nil || jobs[0].FeeStatus != model.TemplateFeeWaived || jobs[0].FeeQuota != 0 {
			t.Fatalf("%s: %#v %v", slug, jobs, err)
		}
	}
	// The pre-check counts the model fee too: gpt-image-1 costs 0.2 USD per call × 0.65 (vip) × 10 = 1.3 credits.
	newTemplate("seven", 7, false)
	newTemplate("seven-member-free", 7, true)
	if _, _, err := GenerateWithImageTemplate(ctx, user, "pricey", TemplateGenerateInput{Files: files, Count: 2}); err == nil || !strings.Contains(err.Error(), "余额不足") {
		t.Fatalf("balance pre-check: %v", err)
	}
	// 4 × 7 = 28 fits the 30 credits, 4 × (7 + 1.3) = 33.2 does not.
	if _, _, err := GenerateWithImageTemplate(ctx, user, "seven", TemplateGenerateInput{Files: files, Count: 4}); err == nil || !strings.Contains(err.Error(), "预计需 33.2") {
		t.Fatalf("model fee not in pre-check: %v", err)
	}
	if _, jobs, err := GenerateWithImageTemplate(ctx, user, "seven-member-free", TemplateGenerateInput{Files: files, Count: 4}); err != nil || jobs[0].FeeStatus != model.TemplateFeeWaived {
		t.Fatalf("member-free 4 × 1.3: %#v %v", jobs, err)
	}
	// A fee raised past new-api's 10 USD cap behind the admin form is refused at submit.
	if err := db.Model(&model.ImageTemplate{}).Where("slug = ?", "pricey").Update("fee_credits", 101).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := GenerateWithImageTemplate(ctx, user, "pricey", TemplateGenerateInput{Files: files}); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("over-max fee: %v", err)
	}

	// dueFees moves every scheduled retry into the past, as if the wait had passed (or the process restarted later).
	dueFees := func() {
		if err := db.Model(&model.GenerationJob{}).Where("fee_next_at IS NOT NULL").Update("fee_next_at", generationNow().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}

	// paid[0] succeeds, paid[1] fails, paid[2] succeeds but new-api fails twice.
	finishTestJob(t, paid[0].ID, model.GenerationStatusSucceeded, "")
	finishTestJob(t, paid[1].ID, model.GenerationStatusFailed, "")
	settleTemplateFees() // nothing for paid[2] yet: it is still queued
	if bodies := consumeBodies(); len(bodies) != 1 || !strings.Contains(bodies[0], `"idempotency_key":"`+paid[0].ID+`"`) ||
		!strings.Contains(bodies[0], `"quota":75000`) || !strings.Contains(bodies[0], `"title":"付费模板"`) || !strings.Contains(bodies[0], `"client_secret":"x"`) {
		t.Fatalf("consume calls: %v", bodies)
	}
	if job := loadTestJob(t, db, paid[0].ID); job.FeeStatus != model.TemplateFeeCharged {
		t.Fatalf("paid[0] fee: %s", job.FeeStatus)
	}
	if job := loadTestJob(t, db, paid[1].ID); job.FeeStatus != model.TemplateFeeWaived {
		t.Fatalf("failed job fee: %s", job.FeeStatus)
	}

	finishTestJob(t, paid[2].ID, model.GenerationStatusSucceeded, "")
	failConsume(2, 0)
	before := time.Now()
	settleTemplateFees()
	settleTemplateFees() // not due yet: no call
	job := loadTestJob(t, db, paid[2].ID)
	if job.FeeStatus != model.TemplateFeePending || job.FeeAttempts != 1 || len(consumeBodies()) != 2 || job.FeeNextAt == nil ||
		job.FeeNextAt.Sub(before) < 59*time.Second || job.FeeNextAt.Sub(before) > 61*time.Second || !strings.Contains(job.FeeError, "HTTP 500") {
		t.Fatalf("after one failure: %#v", job)
	}
	dueFees()
	settleTemplateFees()
	if job := loadTestJob(t, db, paid[2].ID); job.FeeAttempts != 2 || job.FeeNextAt.Sub(before) < 299*time.Second {
		t.Fatalf("second delay: %#v", job)
	}
	dueFees()
	settleTemplateFees()
	settleTemplateFees()
	bodies := consumeBodies()
	// Retries reuse the job ID as the idempotency key; settled jobs are never charged again.
	if len(bodies) != 4 || bodies[1] != bodies[2] || bodies[2] != bodies[3] || !strings.Contains(bodies[3], paid[2].ID) {
		t.Fatalf("consume calls: %v", bodies)
	}
	if job := loadTestJob(t, db, paid[2].ID); job.FeeStatus != model.TemplateFeeCharged || job.FeeNextAt != nil || job.FeeError != "" {
		t.Fatalf("paid[2] fee: %#v", job)
	}

	newPaidJob := func() string {
		_, jobs, err := GenerateWithImageTemplate(ctx, user, "paid", TemplateGenerateInput{Files: files})
		if err != nil {
			t.Fatal(err)
		}
		finishTestJob(t, jobs[0].ID, model.GenerationStatusSucceeded, "")
		return jobs[0].ID
	}

	t.Run("only 400 fails at once", func(t *testing.T) {
		for status, want := range map[int]string{400: model.TemplateFeeFailed, 401: model.TemplateFeePending, 402: model.TemplateFeePending,
			403: model.TemplateFeePending, 429: model.TemplateFeePending, 503: model.TemplateFeePending} {
			id := newPaidJob()
			failConsume(1, status)
			settleTemplateFees()
			if job := loadTestJob(t, db, id); job.FeeStatus != want || job.FeeAttempts != 1 || !strings.Contains(job.FeeError, fmt.Sprintf("HTTP %d", status)) {
				t.Errorf("status %d: %#v", status, job)
			}
			if want == model.TemplateFeePending { // settle it so later cases only see their own job
				if err := db.Model(&model.GenerationJob{}).Where("id = ?", id).Update("fee_status", model.TemplateFeeWaived).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	})

	t.Run("gives up after the retry schedule", func(t *testing.T) {
		id := newPaidJob()
		failConsume(100, 0)
		calls := len(consumeBodies())
		for i := range templateFeeRetryDelays {
			start := time.Now()
			settleTemplateFees()
			job := loadTestJob(t, db, id)
			if job.FeeStatus != model.TemplateFeePending || job.FeeAttempts != i+1 || job.FeeNextAt.Sub(start) < templateFeeRetryDelays[i]-time.Second {
				t.Fatalf("retry %d: %#v", i, job)
			}
			dueFees()
		}
		settleTemplateFees()
		settleTemplateFees()
		job := loadTestJob(t, db, id)
		if job.FeeStatus != model.TemplateFeeFailed || job.FeeAttempts != 7 || len(consumeBodies())-calls != 7 {
			t.Fatalf("fee %#v after %d calls", job, len(consumeBodies())-calls)
		}
		failConsume(0, 0)
	})

	t.Run("admins list failed fees", func(t *testing.T) {
		items, err := AdminListTemplateFees("")
		if err != nil || len(items) != 2 { // the 400 one and the exhausted one
			t.Fatalf("failed fees: %#v %v", items, err)
		}
		for _, item := range items {
			if item.UserID != "g1" || item.TemplateID == "" || item.FeeQuota != 75000 || item.FeeError == "" || item.FinishedAt == nil {
				t.Fatalf("failed fee: %#v", item)
			}
		}
		if _, err := AdminListTemplateFees("charged"); err == nil {
			t.Fatal("unknown status accepted")
		}
	})

	t.Run("cancel waives, retry owes again", func(t *testing.T) {
		_, jobs, err := GenerateWithImageTemplate(ctx, user, "paid", TemplateGenerateInput{Files: files})
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := repository.CancelUserGenerationJob("g1", jobs[0].ID, time.Now()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if job := loadTestJob(t, db, jobs[0].ID); job.FeeStatus != model.TemplateFeeWaived {
			t.Fatalf("cancelled fee: %s", job.FeeStatus)
		}
		if ok, err := repository.RetryUserGenerationJob("g1", jobs[0].ID, time.Now()); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if job := loadTestJob(t, db, jobs[0].ID); job.FeeStatus != model.TemplateFeePending {
			t.Fatalf("retried fee: %s", job.FeeStatus)
		}
	})
}

func TestTemplateFeeQuotaAndSize(t *testing.T) {
	cases := []struct {
		credits, perCredit, perUnit float64
		want                        int64
	}{{1.1, 1, 500000, 550000}, {0.01, 10, 1, 1}, {1, 3, 500000, 166667}, {10, 1, 500000, 5000000}}
	for _, c := range cases {
		if got, err := templateFeeQuota(c.credits, c.perCredit, c.perUnit); err != nil || got != c.want {
			t.Errorf("templateFeeQuota(%v, %v, %v) = %d %v, want %d", c.credits, c.perCredit, c.perUnit, got, err, c.want)
		}
	}
	for _, c := range [][3]float64{{10.01, 1, 500000}, {1, 0, 500000}, {1, 1, math.Inf(1)}, {1, 1, math.NaN()}, {1e30, 1e-30, 1e30}} {
		if got, err := templateFeeQuota(c[0], c[1], c[2]); err == nil {
			t.Errorf("templateFeeQuota(%v) = %d, want an error", c, got)
		}
	}
	for input, want := range map[[2]string]string{{"1:1", "1K"}: "1024x1024", {"16:9", "4K"}: "3840x2160", {"2:3", "1K"}: "832x1248", {"9:16", "2K"}: "1584x2816"} {
		if got := templateImageSize(input[0], input[1]); got != want {
			t.Errorf("templateImageSize(%v) = %s, want %s", input, got, want)
		}
	}
}
