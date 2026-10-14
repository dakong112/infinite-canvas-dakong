package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// finishTestJob plays the worker for one job: claim it and record the given outcome.
func finishTestJob(t *testing.T, id string, status string, outputText string) {
	t.Helper()
	at := generationNow().Add(time.Hour)
	job := claimTestJob(t, id, at)
	values := map[string]any{"status": status, "output_text": outputText, "finished_at": at, "lease_token": "", "lease_expires_at": nil}
	if ok, err := repository.FinishGenerationJob(job.ID, job.LeaseToken, values); err != nil || !ok {
		t.Fatalf("finish %s: %v %v", id, ok, err)
	}
}

func TestEcommerceSetFlow(t *testing.T) {
	if runGenerationTestIsolated(t, "TestEcommerceSetFlow") {
		return
	}
	db := generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{VisionModel: "gpt-4o-mini", ImageModel: "gpt-image-1", PreviewQuality: "low", FinalQuality: "high"}}, "gpt-image-1", "gpt-4o-mini")
	u1, u2 := model.AuthUser{ID: "u1", Role: model.UserRoleUser}, model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	ctx1, ctx2 := WithUser(context.Background(), u1), WithUser(context.Background(), u2)
	front, err := UploadStorageObject(ctx1, "front.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UploadStorageObject(ctx1, "back.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	form := EcommerceSetForm{ProductFileIDs: []string{front.ID, back.ID}, Market: "中国大陆", Platform: "淘宝", Language: "zh", Skus: []EcommerceSku{{Label: "红色XL", Desc: "纯棉"}}}
	view, err := CreateEcommerceSet(ctx1, u1, form)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Slots) != 10 || view.Form.Preset != "set" || view.Form.StyleMode != "ai" || len(view.Pending) != 0 {
		t.Fatalf("created set: %#v", view)
	}

	t.Run("other users cannot see or use the set or its files", func(t *testing.T) {
		if _, err := GetEcommerceSet(ctx2, u2, view.ID); err == nil || err.Error() != "套图不存在" {
			t.Fatalf("u2 read the set: %v", err)
		}
		if _, err := AnalyzeEcommerceSet(ctx2, u2, view.ID); err == nil {
			t.Fatal("u2 analyzed the set")
		}
		if _, err := GenerateEcommerceSet(ctx2, u2, view.ID, "preview", nil); err == nil {
			t.Fatal("u2 generated the set")
		}
		if _, err := CreateEcommerceSet(ctx2, u2, form); err == nil || err.Error() != "图片不存在" {
			t.Fatalf("u2 used u1 product images: %v", err)
		}
		if sets, err := ListEcommerceSets("u2", ""); err != nil || len(sets) != 0 {
			t.Fatalf("u2 sets: %v %v", sets, err)
		}
	})

	t.Run("analysis job reads every product image and fills the set", func(t *testing.T) {
		analyzed, err := AnalyzeEcommerceSet(ctx1, u1, view.ID)
		if err != nil || len(analyzed.Pending) != 1 || analyzed.Pending[0].Purpose != "analyze" {
			t.Fatalf("analyze: %#v %v", analyzed.Pending, err)
		}
		job := loadTestJob(t, db, analyzed.Pending[0].JobID)
		// Without new-api SSO the admin channel serves the job.
		if job.Kind != model.GenerationKindVision || job.Endpoint != "/responses" || job.Model != "gpt-4o-mini" || job.ChannelID != "c1" || !slices.Equal(job.InputFileIDs, []string{front.ID, back.ID}) {
			t.Fatalf("analyze job: %#v", job)
		}
		if !strings.Contains(job.RequestJSON, "目标市场：中国大陆；目标平台：淘宝。") || !strings.Contains(job.RequestJSON, "你是顶级电商视觉总监与产品策划") || strings.Count(job.RequestJSON, `"input_image"`) != 2 {
			t.Fatalf("analyze request: %s", job.RequestJSON)
		}
		if _, err := PlanEcommerceSet(ctx1, u1, view.ID); err == nil {
			t.Fatal("a second AI job was queued while one is pending")
		}
		finishTestJob(t, job.ID, model.GenerationStatusSucceeded, "```json\n"+`{"sellingPoints":{"main":"轻盈透气","white":"正面单图","detail":"三层面料"},"targetAudience":"通勤白领","designStyle":"极简","designSpec":"主色 #1E3A8A","needsModel":true,"modelPrompt":"25岁女性","styleOptions":["极简棚拍","生活方式"]}`+"\n```")
		got, err := GetEcommerceSet(ctx1, u1, view.ID)
		if err != nil || got.Analysis == nil || got.Analysis.SellingPoints.Main != "轻盈透气" || !got.Analysis.NeedsModel || len(got.Pending) != 0 {
			t.Fatalf("analysis not applied: %#v %v", got.Analysis, err)
		}
	})

	t.Run("plan retries missing slots and falls back after three rounds", func(t *testing.T) {
		planned, err := PlanEcommerceSet(ctx1, u1, view.ID)
		if err != nil || len(planned.Pending) != 1 || len(planned.Pending[0].SlotIDs) != 9 || planned.PlanRound != 1 {
			t.Fatalf("plan: %#v %v", planned.Pending, err)
		}
		round1 := loadTestJob(t, db, planned.Pending[0].JobID)
		if !strings.Contains(round1.RequestJSON, "逐页配置（共 9 页") || strings.Contains(round1.RequestJSON, "sku_01") {
			t.Fatalf("plan request: %s", round1.RequestJSON)
		}
		// slot_01..slot_07 complete, slot_08 cut off mid-string, slot_09 missing.
		var reply strings.Builder
		reply.WriteString("{")
		for i := 1; i <= 7; i++ {
			fmt.Fprintf(&reply, `"slot_%02d":"【本页画面要求】：主题%d\n- **必须显示的画面文字**：\n  - 主标题必须显示为：\"标题%d\"",`, i, i, i)
		}
		reply.WriteString(`"slot_08":"【本页画面要求】：截断`)
		finishTestJob(t, round1.ID, model.GenerationStatusSucceeded, reply.String())
		second, err := GetEcommerceSet(ctx1, u1, view.ID)
		if err != nil || len(second.SlotPrompts) != 7 || len(second.Pending) != 1 || second.PlanRound != 2 || !slices.Equal(second.Pending[0].SlotIDs, []string{"slot_08", "slot_09"}) {
			t.Fatalf("round 2: %#v %v", second, err)
		}
		round2 := loadTestJob(t, db, second.Pending[0].JobID)
		if !strings.Contains(round2.RequestJSON, "【重要·避免重复】") || !strings.Contains(round2.RequestJSON, "slot_01：主题「主题1」｜主标题「标题1」｜副标题「无」") {
			t.Fatalf("round 2 request lacks dedup note: %s", round2.RequestJSON)
		}
		finishTestJob(t, round2.ID, model.GenerationStatusFailed, "")
		third, err := GetEcommerceSet(ctx1, u1, view.ID)
		if err != nil || third.PlanRound != 3 || len(third.Pending) != 1 {
			t.Fatalf("round 3: %#v %v", third, err)
		}
		finishTestJob(t, third.Pending[0].JobID, model.GenerationStatusSucceeded, `{"slot_08":"【本页画面要求】：第八页"}`)
		done, err := GetEcommerceSet(ctx1, u1, view.ID)
		if err != nil || !done.Planned || done.SlotPrompts["slot_08"] != "【本页画面要求】：第八页" || done.SlotPrompts["slot_09"] != "" || done.Notice != "1 个槽位提示词规划失败，已用默认模板" {
			t.Fatalf("plan result: %#v %v", done, err)
		}
	})

	t.Run("preview and final jobs compose the design spec with each slot", func(t *testing.T) {
		preview, err := GenerateEcommerceSet(ctx1, u1, view.ID, "preview", nil)
		if err != nil {
			t.Fatal(err)
		}
		slot01 := loadTestJob(t, db, preview.Slots[0].PreviewJobID)
		if !strings.HasPrefix(slot01.Prompt, "主色 #1E3A8A\n\n---\n\n【本页画面要求】：主题1") || !strings.Contains(slot01.RequestJSON, `"quality":"low"`) ||
			!strings.Contains(slot01.RequestJSON, `"size":"1024x1024"`) || slot01.Endpoint != "/images/edits" || slot01.ChannelID != "c1" || slot01.Stage != "preview" || slot01.SlotKind != "main" || len(slot01.InputFileIDs) != 2 {
			t.Fatalf("slot_01 preview job: %#v", slot01)
		}
		if slot09 := loadTestJob(t, db, preview.Slots[8].PreviewJobID); !strings.HasSuffix(slot09.Prompt, buildSlotPrompt("detail", 3, nil)) {
			t.Fatalf("unplanned slot must fall back to its template: %s", slot09.Prompt)
		}
		sku := loadTestJob(t, db, preview.Slots[9].PreviewJobID)
		if !strings.Contains(sku.Prompt, "本图呈现的 SKU：红色XL（纯棉）。") || !slices.Equal(sku.InputFileIDs, []string{front.ID}) {
			t.Fatalf("sku job: %#v", sku)
		}
		final, err := GenerateEcommerceSet(ctx1, u1, view.ID, "final", []string{"slot_01"})
		if err != nil {
			t.Fatal(err)
		}
		if job := loadTestJob(t, db, final.Slots[0].FinalJobID); job.Stage != "final" || !strings.Contains(job.RequestJSON, `"quality":"high"`) {
			t.Fatalf("final job: %#v", job)
		}
		if _, err := GenerateEcommerceSet(ctx1, u1, view.ID, "final", nil); err == nil {
			t.Fatal("final stage must name the slots")
		}
	})

	t.Run("structure edits rebuild slots and reset the plan", func(t *testing.T) {
		counts := map[string]int{"white": 1, "brand": 1, "selling": 1, "material": 1, "craft": 1, "spec": 1, "scene": 1}
		updated, err := UpdateEcommerceSet(ctx1, u1, view.ID, EcommerceSetForm{Market: "中国大陆", Platform: "淘宝", Language: "en", Structure: EcommerceStructure{Mode: "custom", Counts: counts}})
		if err != nil || len(updated.Slots) != 7 || updated.Planned || len(updated.SlotPrompts) != 0 || updated.Slots[1].Theme != "品牌主视觉海报" || updated.Slots[1].TextLang != "en" {
			t.Fatalf("update: %#v %v", updated, err)
		}
		if _, err := UpdateEcommerceSet(ctx1, u1, view.ID, EcommerceSetForm{Market: "中国大陆", Platform: "淘宝", Language: "zh", Structure: EcommerceStructure{Mode: "custom", Counts: map[string]int{"white": 1}}}); err == nil || !strings.Contains(err.Error(), "套图需要 7-15 张") {
			t.Fatalf("short structure accepted: %v", err)
		}
		sets, err := ListEcommerceSets("u1", "set")
		if err != nil || len(sets) != 1 || sets[0].ID != view.ID || sets[0].CoverURL != "/api/files/"+front.ID+"/content" {
			t.Fatalf("history: %#v %v", sets, err)
		}
	})

	t.Run("tools validate inputs and build their prompts", func(t *testing.T) {
		_, removed, err := RunEcommerceTool(ctx1, u1, "remove-bg", EcommerceToolInput{FileIDs: []string{front.ID}, Params: map[string]string{"background": "transparent"}})
		if err != nil || len(removed) != 1 || removed[0].Source != model.GenerationSourceEcommerceTool || removed[0].ChannelID != "c1" || !strings.Contains(removed[0].RequestJSON, `"background":"transparent"`) || !strings.Contains(removed[0].Prompt, "透明背景") {
			t.Fatalf("remove-bg: %#v %v", removed, err)
		}
		_, replicate, err := RunEcommerceTool(ctx1, u1, "replicate", EcommerceToolInput{FileIDs: []string{back.ID, front.ID}, Params: map[string]string{"extra": "暖色调"}, Count: 2})
		if err != nil || len(replicate) != 2 || !slices.Equal(replicate[0].InputFileIDs, []string{back.ID, front.ID}) || !strings.Contains(replicate[0].Prompt, "补充要求：暖色调") {
			t.Fatalf("replicate: %#v %v", replicate, err)
		}
		_, tryon, err := RunEcommerceTool(ctx1, u1, "tryon", EcommerceToolInput{FileIDs: []string{front.ID, back.ID}, Params: map[string]string{"mode": "wear", "hasModelImage": "true"}})
		if err != nil || !strings.Contains(tryon[0].Prompt, "使用最后一张图中的模特形象") {
			t.Fatalf("tryon: %#v %v", tryon, err)
		}
		rejected := map[string]struct {
			tool  string
			input EcommerceToolInput
		}{
			"replicate needs a reference and a product": {"replicate", EcommerceToolInput{FileIDs: []string{front.ID}}},
			"unknown tool":                       {"upscale", EcommerceToolInput{FileIDs: []string{front.ID}}},
			"bad background color":               {"remove-bg", EcommerceToolInput{FileIDs: []string{front.ID}, Params: map[string]string{"background": "color", "color": "red"}}},
			"unknown parameter":                  {"remove-bg", EcommerceToolInput{FileIDs: []string{front.ID}, Params: map[string]string{"prompt": "x"}}},
			"model image flag without the image": {"tryon", EcommerceToolInput{FileIDs: []string{front.ID}, Params: map[string]string{"hasModelImage": "true"}}},
		}
		for name, test := range rejected {
			if _, _, err := RunEcommerceTool(ctx1, u1, test.tool, test.input); err == nil {
				t.Errorf("%s: accepted", name)
			}
		}
		if _, _, err := RunEcommerceTool(ctx2, u2, "remove-bg", EcommerceToolInput{FileIDs: []string{front.ID}}); err == nil || err.Error() != "输入图片不存在" {
			t.Fatalf("u2 used u1 image in a tool: %v", err)
		}
	})
}

// Users linked to new-api are billed through their account group: every ecommerce job
// (AI assist, slot images, tools) asks for the "newapi@" channel, resolved to the user's group.
func TestEcommerceJobsUseNewapiGroup(t *testing.T) {
	if runGenerationTestIsolated(t, "TestEcommerceJobsUseNewapiGroup") {
		return
	}
	server := fakeNewapiAccount(t, &callLog{})
	db := generationTestDB(t)
	config.Cfg.NewapiBaseURL, config.Cfg.NewapiClientID, config.Cfg.NewapiClientSecret, config.Cfg.NewapiRedirectURI = server.URL, "infinite", "x", "https://www.example.com/sso/callback"
	if err := model.SetSecretKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: "g1", Username: "g1", AffCode: "G1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "50",
		NewapiAccessToken: testPAT, NewapiAPIKey: "sk-login", NewapiTokenExpiresAt: time.Now().Add(time.Hour).Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	settings := model.Settings{
		Public:  model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{VisionModel: "gpt-image-1", ImageModel: "gpt-image-1", PreviewQuality: "low", FinalQuality: "high"}},
		Private: model.PrivateSetting{Channels: []model.ModelChannel{{ID: "na", Protocol: "openai", Name: "new-api", BaseURL: server.URL, APIKey: "sk-admin", Models: []string{"gpt-image-1"}, Weight: 1, Enabled: true}}},
	}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	user := model.AuthUser{ID: "g1", Role: model.UserRoleUser}
	ctx := WithUser(context.Background(), user)
	product, err := UploadStorageObject(ctx, "front.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	view, err := CreateEcommerceSet(ctx, user, EcommerceSetForm{ProductFileIDs: []string{product.ID}, Market: "中国大陆", Platform: "淘宝", Language: "zh"})
	if err != nil {
		t.Fatal(err)
	}
	analyzed, err := AnalyzeEcommerceSet(ctx, user, view.ID)
	if err != nil || len(analyzed.Pending) != 1 {
		t.Fatalf("analyze: %#v %v", analyzed.Pending, err)
	}
	if job := loadTestJob(t, db, analyzed.Pending[0].JobID); job.ChannelID != "newapi@vip" {
		t.Fatalf("analyze job channel = %q, want the account group", job.ChannelID)
	}
	_, jobs, err := RunEcommerceTool(ctx, user, "remove-bg", EcommerceToolInput{FileIDs: []string{product.ID}})
	if err != nil || len(jobs) != 1 || jobs[0].ChannelID != "newapi@vip" {
		t.Fatalf("tool job: %#v %v", jobs, err)
	}
}
