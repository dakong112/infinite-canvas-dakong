package service

import (
	"context"
	"encoding/json"
	"errors"
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
		finishTestJob(t, third.Pending[0].JobID, model.GenerationStatusSucceeded, `{"slot_01":"被覆盖","slot_08":"【本页画面要求】：第八页"}`)
		done, err := GetEcommerceSet(ctx1, u1, view.ID)
		if err != nil || !done.Planned || done.SlotPrompts["slot_08"] != "【本页画面要求】：第八页" || !strings.Contains(done.SlotPrompts["slot_01"], "主题1") || done.SlotPrompts["slot_09"] != "" || done.Notice != "1 个槽位提示词规划失败，已用默认模板" {
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
		if slot09 := loadTestJob(t, db, preview.Slots[8].PreviewJobID); !strings.HasSuffix(slot09.Prompt, buildSlotPrompt(ecommercePromptDefaults(), "detail", 3, nil)) {
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

	t.Run("limits and image checks", func(t *testing.T) {
		long := strings.Repeat("卖", 501)
		if _, err := UpdateEcommerceSet(ctx1, u1, view.ID, EcommerceSetForm{Market: "中国大陆", Platform: "淘宝", Language: "zh", SellingPoints: &EcommerceSellingPoints{Main: long}}); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("long selling point accepted: %v", err)
		}
		if ecommerceMaxSlots+ecommerceMaxSkus > generationMaxJobsPerBatch {
			t.Fatal("max slots + SKUs exceed the batch job cap")
		}
		txt, err := UploadStorageObject(ctx1, "a.txt", "text/plain", []byte("hello"))
		if err != nil {
			t.Skipf("non-image upload unsupported: %v", err)
		}
		if _, err := UpdateEcommerceSet(ctx1, u1, view.ID, EcommerceSetForm{Market: "中国大陆", Platform: "淘宝", Language: "zh", Skus: []EcommerceSku{{Label: "红", FileID: txt.ID}}}); err == nil {
			t.Fatal("non-image SKU file accepted")
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

	t.Run("ecommerce AI jobs cannot be retried from the generic endpoint", func(t *testing.T) {
		second, err := CreateEcommerceSet(ctx1, u1, form)
		if err != nil {
			t.Fatal(err)
		}
		analyzed, err := AnalyzeEcommerceSet(ctx1, u1, second.ID)
		if err != nil || len(analyzed.Pending) != 1 {
			t.Fatalf("analyze: %v", err)
		}
		finishTestJob(t, analyzed.Pending[0].JobID, model.GenerationStatusFailed, "")
		if _, err := RetryGenerationJob("u1", "u1", analyzed.Pending[0].JobID); err == nil || !strings.Contains(err.Error(), "商品套图流程") {
			t.Fatalf("retry of set job: %v", err)
		}
		if job := loadTestJob(t, db, analyzed.Pending[0].JobID); job.Status != model.GenerationStatusFailed {
			t.Fatalf("job requeued: %#v", job)
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

// "再来一次" keeps every image of a slot as a version; admin prompt overrides apply at use time.
func TestEcommerceSlotVersionsAndPromptOverrides(t *testing.T) {
	if runGenerationTestIsolated(t, "TestEcommerceSlotVersionsAndPromptOverrides") {
		return
	}
	db := generationTestDB(t)
	saveGenerationTestSettings(t, model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{VisionModel: "gpt-4o-mini", ImageModel: "gpt-image-1", PreviewQuality: "low", FinalQuality: "high"}}, "gpt-image-1", "gpt-4o-mini")
	setPrompts := func(prompts model.EcommercePrompts) {
		t.Helper()
		settings, err := repository.GetSettings()
		if err != nil {
			t.Fatal(err)
		}
		settings.Private.EcommercePrompts = prompts
		if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	u1, u2 := model.AuthUser{ID: "u1", Role: model.UserRoleUser}, model.AuthUser{ID: "u2", Role: model.UserRoleUser}
	ctx1, ctx2 := WithUser(context.Background(), u1), WithUser(context.Background(), u2)
	front, err := UploadStorageObject(ctx1, "front.png", "image/png", testPNG)
	if err != nil {
		t.Fatal(err)
	}
	view, err := CreateEcommerceSet(ctx1, u1, EcommerceSetForm{ProductFileIDs: []string{front.ID}, Market: "中国大陆", Platform: "淘宝", Language: "zh", Skus: []EcommerceSku{{Label: "红色"}}})
	if err != nil {
		t.Fatal(err)
	}

	setPrompts(model.EcommercePrompts{Analyze: "自定义解析模板", Variants: map[string][]string{"sku": {"自定义SKU变体"}}})
	generated, err := GenerateEcommerceSet(ctx1, u1, view.ID, "preview", []string{"sku_01"})
	if err != nil {
		t.Fatal(err)
	}
	sku := generated.Slots[9]
	first := loadTestJob(t, db, sku.PreviewJobID)
	if !strings.HasPrefix(first.Prompt, "自定义SKU变体\n") || !slices.Equal(sku.PreviewJobIDs, []string{first.ID}) {
		t.Fatalf("override variant not used: %q %#v", first.Prompt, sku)
	}
	analyzed, err := AnalyzeEcommerceSet(ctx1, u1, view.ID)
	if err != nil || !strings.Contains(loadTestJob(t, db, analyzed.Pending[0].JobID).RequestJSON, "自定义解析模板") {
		t.Fatalf("override analyze template not used: %v", err)
	}
	finishTestJob(t, analyzed.Pending[0].JobID, model.GenerationStatusFailed, "")

	if _, err := RegenerateEcommerceSlot(ctx1, u1, view.ID, "sku_01", "preview"); err == nil || !strings.Contains(err.Error(), "还没有生成成功") {
		t.Fatalf("regenerated before any success: %v", err)
	}
	finishTestJob(t, first.ID, model.GenerationStatusSucceeded, "")
	if _, err := RegenerateEcommerceSlot(ctx2, u2, view.ID, "sku_01", "preview"); err == nil || err.Error() != "套图不存在" {
		t.Fatalf("u2 regenerated u1's slot: %v", err)
	}
	if _, err := ChooseEcommerceSlotVersion(ctx2, u2, view.ID, "sku_01", "preview", first.ID); err == nil || err.Error() != "套图不存在" {
		t.Fatalf("u2 chose u1's version: %v", err)
	}
	if _, err := RegenerateEcommerceSlot(ctx1, u1, view.ID, "sku_01", "final"); err == nil {
		t.Fatal("regenerated a stage without images")
	}

	// A set saved before version history only knows the current job ID.
	batch, set, err := loadEcommerceSet("u1", view.ID)
	if err != nil {
		t.Fatal(err)
	}
	set.Slots[9].PreviewJobIDs = nil
	raw, _ := json.Marshal(set)
	if strings.Contains(string(raw), "previewJobIds") {
		t.Fatal("old context still has a history")
	}
	if ok, err := repository.UpdateGenerationBatchContext("u1", view.ID, batch.Version, string(raw)); err != nil || !ok {
		t.Fatalf("write old context: %v %v", ok, err)
	}
	if old, err := GetEcommerceSet(ctx1, u1, view.ID); err != nil || !slices.Equal(old.Slots[9].PreviewJobIDs, []string{first.ID}) {
		t.Fatalf("old context: %#v %v", old.Slots[9], err)
	}

	stale, staleSet, err := loadEcommerceSet("u1", view.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := RegenerateEcommerceSlot(ctx1, u1, view.ID, "sku_01", "preview")
	if err != nil {
		t.Fatal(err)
	}
	slot := again.Slots[9]
	second := loadTestJob(t, db, slot.PreviewJobID)
	if second.ID == first.ID || !slices.Equal(slot.PreviewJobIDs, []string{first.ID, second.ID}) || second.Prompt != first.Prompt || second.RequestJSON != first.RequestJSON || second.Stage != "preview" || !slices.Equal(second.InputFileIDs, first.InputFileIDs) {
		t.Fatalf("regenerated job: %#v slot %#v", second, slot)
	}
	if err := commitEcommerceSet(stale, staleSet, nil); !errors.Is(err, errEcommerceSetChanged) {
		t.Fatalf("stale write after regenerate: %v", err)
	}

	chosen, err := ChooseEcommerceSlotVersion(ctx1, u1, view.ID, "sku_01", "preview", first.ID)
	if err != nil || chosen.Slots[9].PreviewJobID != first.ID || len(chosen.Slots[9].PreviewJobIDs) != 2 {
		t.Fatalf("choose: %#v %v", chosen.Slots[9], err)
	}
	for name, choice := range map[string][2]string{"unknown job": {"preview", "genjob-x"}, "other stage": {"final", first.ID}, "bad stage": {"draft", first.ID}} {
		if _, err := ChooseEcommerceSlotVersion(ctx1, u1, view.ID, "sku_01", choice[0], choice[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	setPrompts(model.EcommercePrompts{})
	reset, err := GenerateEcommerceSet(ctx1, u1, view.ID, "preview", []string{"sku_01"})
	if err != nil {
		t.Fatal(err)
	}
	if job := loadTestJob(t, db, reset.Slots[9].PreviewJobID); !strings.HasPrefix(job.Prompt, ecommerceSkuVariants[0]) || len(reset.Slots[9].PreviewJobIDs) != 3 {
		t.Fatalf("empty override must fall back to the default: %q", job.Prompt)
	}
}

func TestEcommerceImageSpaceAccess(t *testing.T) {
	if runGenerationTestIsolated(t, "TestEcommerceImageSpaceAccess") {
		return
	}
	generationTestDB(t)
	for _, object := range []model.StorageObject{
		{ID: "team-file", ObjectKey: "k1", CreatedBy: "mate", SpaceID: "team:7", MimeType: "image/png"},
		{ID: "mate-personal", ObjectKey: "k2", CreatedBy: "mate", MimeType: "image/png"},
	} {
		if _, err := repository.SaveStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	member, outsider := model.AuthUser{ID: "u1", SpaceID: "team:7"}, model.AuthUser{ID: "u2", SpaceID: "team:8"}
	if err := checkEcommerceImage(member, "team-file", false); err != nil {
		t.Fatalf("teammate could not use a team file: %v", err)
	}
	if checkEcommerceImage(outsider, "team-file", false) == nil || checkEcommerceImage(model.AuthUser{ID: "u2"}, "team-file", false) == nil {
		t.Fatal("outsider used a team file")
	}
	if checkEcommerceImage(member, "mate-personal", false) == nil {
		t.Fatal("teammate's personal file usable in team space")
	}
}
