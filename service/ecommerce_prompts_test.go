package service

import (
	"maps"
	"strings"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
)

func TestRenderPromptTemplate(t *testing.T) {
	got := renderPromptTemplate("a{{ x }}b{{missing}}c{{{y}}}", map[string]string{"x": "1", "y": "2"})
	if got != "a1bc{2}" {
		t.Fatalf("render = %q", got)
	}
}

func TestBuildPlanInstructionMatches1688(t *testing.T) {
	slots, err := buildEcommerceSlots(EcommerceSetForm{Preset: "set", Language: "zh", Structure: EcommerceStructure{Mode: "smart"}})
	if err != nil {
		t.Fatal(err)
	}
	instruction := buildPlanInstruction(ecommercePromptDefaults(), slots, ecommercePlanOptions{
		SellingPoints: EcommerceSellingPoints{Main: "轻盈透气", Detail: "三层面料"},
		NeedsModel:    true, ModelDescription: "25岁女性", QualityLabel: "高清高质感", DesignSpec: " 主色 #1E3A8A ",
	})
	wants := []string{
		"你是顶级电商视觉总监和 AI 绘画提示词工程师。请观察我附上的全部商品图，为下面每个图片槽位（=详情页的一页）撰写一套完整的、结构化的电商详情页设计提示词。\n",
		"整体设计规范（所有页必须严格遵循此规范的色彩/字体/模特一致性/比例）：\n主色 #1E3A8A\n每一页都必须严格按以下结构输出",
		"逐页配置（共 9 页，按此为每页生成上面整套结构）：\n" +
			"- \"slot_01\"（产品主图：各视角有区分度、突出产品本体、干净合规）：\n" +
			"  本页尺寸比例：正方形 1:1 构图，须严格按 1:1 比例设计；画面质量：高清高质感。\n" +
			"  本页卖点（写进「视觉重点与卖点」与「AI生图Prompt」）：「轻盈透气」。\n" +
			"  本页画面文字：本页须含主标题（≤13 字）+ 副标题（≤18 字），语言：简体中文；直接输出标题文案，不带字数说明。\n" +
			"  本页模特：本图需出现模特：模特设定「25岁女性」。模特动作自然，手、脚、腿、手指、关节、肢体比例和面部结构必须符合正常人类生理结构，禁止畸形肢体、额外手指、缺失手指、扭曲关节、反向肢体。\n" +
			"- \"slot_02\"",
		"- \"slot_05\"（白底图：纯白背景 #FFFFFF、无阴影无道具、轮廓干净锐利；画面只一个商品主体且仅正面，不要多视图拼图、不要背面/侧面同框、不要重复商品）：\n" +
			"  本页尺寸比例：正方形 1:1 构图，须严格按 1:1 比例设计；画面质量：高清高质感。\n" +
			"  本页卖点（写进「视觉重点与卖点」与「AI生图Prompt」）：「无，请据图自行提炼」。\n" +
			"  本页画面文字：本页不出现任何画面文字（无主标题、无副标题）。\n" +
			"  本页模特：本图不要出现模特、人物主体或人物肢体。\n",
		"  本页尺寸比例：竖版 2:3 构图，须严格按 2:3 比例设计；画面质量：高清高质感。\n  本页卖点（写进「视觉重点与卖点」与「AI生图Prompt」）：「三层面料」。",
		"反向肢体。\n只输出一个 JSON 对象，key 为槽位 id",
		"必须包含全部 9 个 key：\"slot_01\"、\"slot_02\"、\"slot_03\"、\"slot_04\"、\"slot_05\"、\"slot_06\"、\"slot_07\"、\"slot_08\"、\"slot_09\"。\n例如：{\"slot_01\":\"...\",\"slot_02\":\"...\"}",
	}
	for _, want := range wants {
		if !strings.Contains(instruction, want) {
			t.Errorf("plan instruction missing:\n%s\n--- got ---\n%s", want, instruction)
		}
	}
	if strings.Contains(instruction, "{{") || strings.Contains(instruction, "本页主题") {
		t.Fatal("smart layout must render exactly like 1688 (no placeholders, no theme lines)")
	}
}

func TestBuildEcommerceSlots(t *testing.T) {
	smart, err := buildEcommerceSlots(EcommerceSetForm{Preset: "set", Language: "en", Structure: EcommerceStructure{Mode: "smart"}, Skus: []EcommerceSku{{Label: "红色XL", Desc: "纯棉"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(smart) != 10 || smart[0].ID != "slot_01" || smart[0].TextLang != "en" || smart[0].NeedsModel != nil || smart[8].Kind != "detail" || smart[8].Size != "1024x1536" {
		t.Fatalf("smart slots: %#v", smart)
	}
	if white := smart[4]; white.Kind != "white" || white.TextLang != "none" || white.NeedsModel == nil || *white.NeedsModel {
		t.Fatalf("white slot: %#v", white)
	}
	if sku := smart[9]; sku.ID != "sku_01" || sku.Kind != "sku" || sku.SkuDesc != "红色XL（纯棉）" || sku.Title != "SKU图-红色XL" {
		t.Fatalf("sku slot: %#v", sku)
	}
	custom, err := buildEcommerceSlots(EcommerceSetForm{Preset: "set", Language: "zh", Structure: EcommerceStructure{Mode: "custom", Counts: map[string]int{"white": 1, "brand": 1, "selling": 2, "material": 1, "craft": 1, "scene": 1}}})
	if err != nil || len(custom) != 7 || custom[1].Theme != "品牌主视觉海报" || custom[2].Title != "核心卖点海报 1" {
		t.Fatalf("custom slots: %#v %v", custom, err)
	}
	if !strings.Contains(buildPlanInstruction(ecommercePromptDefaults(), custom, ecommercePlanOptions{}), "  本页主题：品牌主视觉海报（围绕该主题展开本页画面）。") {
		t.Fatal("custom slots must carry their standard image type into the plan")
	}
	detail, err := buildEcommerceSlots(EcommerceSetForm{Preset: "detail", Language: "zh"})
	if err != nil || len(detail) != 7 || detail[0].Kind != "detail" || detail[0].Theme != "品牌主视觉海报" || detail[6].Theme != "收官价值视觉图" {
		t.Fatalf("detail preset: %#v %v", detail, err)
	}
	for name, counts := range map[string]map[string]int{
		"too few":  {"white": 1, "brand": 1},
		"unknown":  {"white": 3, "brand": 3, "poster": 3},
		"too many": {"brand": 6, "white": 1},
	} {
		if _, err := buildEcommerceSlots(EcommerceSetForm{Preset: "set", Language: "zh", Structure: EcommerceStructure{Mode: "custom", Counts: counts}}); err == nil {
			t.Errorf("%s: custom structure accepted", name)
		}
	}
}

func TestSlotPromptsAndComposition(t *testing.T) {
	no := false
	white := buildSlotPrompt(ecommercePromptDefaults(), "white", 0, &no)
	if white != ecommerceWhiteVariants[0]+"\n禁止事项：无文字乱码、无产品结构变形、无材质失真、背景不喧宾夺主。\n本图不要出现模特、人物主体或人物肢体。" {
		t.Fatalf("white prompt = %q", white)
	}
	if buildSlotPrompt(ecommercePromptDefaults(), "main", 7, nil) != ecommerceMainVariants[1]+"\n禁止事项：无文字乱码、无产品结构变形、无材质失真、背景不喧宾夺主。" {
		t.Fatal("main variants must cycle by index without a model clause")
	}
	if !strings.HasSuffix(buildSkuSlotPrompt(ecommercePromptDefaults(), " 红色XL（纯棉） "), "\n本图呈现的 SKU：红色XL（纯棉）。请如实还原该款式的颜色、材质与形态，不要臆造其它款式。") || buildSkuSlotPrompt(ecommercePromptDefaults(), "") != buildSlotPrompt(ecommercePromptDefaults(), "sku", 0, nil) {
		t.Fatal("sku prompt")
	}
	if composeSlotGenerationPrompt("  ", "块") != "块" || composeSlotGenerationPrompt(" 规范 ", "块") != "规范\n\n---\n\n块" {
		t.Fatal("compose prompt")
	}
}

func TestSellingPointInstructions(t *testing.T) {
	context := sellingPointContext{Current: " 轻盈 ", DesignStyle: "极简", OtherPoints: EcommerceSellingPoints{Main: "轻盈", White: "正面单图", Detail: "三层面料"}, ModelDescription: "25岁女性", NeedsModel: true}
	rewrite := buildSellingPointRewriteInstruction(ecommercePromptDefaults(), "main", context)
	want := "你是顶级电商产品策划。请基于我附上的商品图，在保持以下文案核心诉求与方向不变的前提下，将其优化得更专业、更有冲击力、更贴合用途：\n当前文案：轻盈\n" +
		"用途：主图（点击转化强爆点，简短有冲击力）\n设计风格：极简\n其它卖点（供保持整体调性参考，请勿照抄）：白底图「正面单图」，详情页「三层面料」\n模特设定：25岁女性\n" +
		"只输出一个合法 JSON 对象，不要 Markdown 代码块或多余文字，结构：{\"text\":\"优化后的文案\"}"
	if rewrite != want {
		t.Fatalf("rewrite instruction:\n%s\nwant:\n%s", rewrite, want)
	}
	options := buildSellingPointsInstruction(ecommercePromptDefaults(), "detail", sellingPointContext{})
	if options != "你是顶级电商产品策划。请观察我附上的商品图，针对以下用途提炼 3 个方向截然不同但都有吸引力的方案：\n用途：详情图（深度种草卖点，可展开材质/功能/场景）\n只输出一个合法 JSON 对象，不要 Markdown 代码块或多余文字，结构：{\"options\":[\"方案1\",\"方案2\",\"方案3\"]}" {
		t.Fatalf("options instruction: %s", options)
	}
	if got := parseSellingOptions("```\n{\"options\":[\"甲\",1,\"乙\"]}\n```"); len(got) != 2 || got[1] != "乙" {
		t.Fatalf("options = %v", got)
	}
	if got := parseSellingPointText(`前言 {"text":"  新文案 "} 后记`); got != "新文案" {
		t.Fatalf("text = %q", got)
	}
}

func TestPlanOutputParsing(t *testing.T) {
	truncated := `{"slot_01":"第一页\n含\"引号\"","slot_99":"幻觉","slot_02":"截断`
	if got := salvageTruncatedJSON(truncated, []string{"slot_01", "slot_02"}); !maps.Equal(got, map[string]string{"slot_01": "第一页\n含\"引号\""}) {
		t.Fatalf("salvage = %#v", got)
	}
	fenced := "说明\n```json\n{\"slot_01\":\"A\",\"slot_02\":3}\n```"
	if got := parseRegeneratedPrompts(fenced); !maps.Equal(got, map[string]string{"slot_01": "A"}) {
		t.Fatalf("regenerated = %#v", got)
	}
	if got := parseRegeneratedPrompts("没有 JSON"); len(got) != 0 {
		t.Fatalf("garbage parsed: %#v", got)
	}
	if got := parsePlanOutput(`{"slot_01":"A","slot_02":" ","slot_77":"B"}`, []string{"slot_01", "slot_02"}); !maps.Equal(got, map[string]string{"slot_01": "A"}) {
		t.Fatalf("plan output = %#v", got)
	}
	analysis := parseAnalysis("```json\n{\"sellingPoints\":{\"main\":\"爆点\",\"white\":\"正面\",\"detail\":\"细节\"},\"designSpec\":\"规范\",\"needsModel\":true,\"modelPrompt\":\"女性\",\"styleOptions\":[\"极简\",2]}\n```")
	if analysis.SellingPoints.Main != "爆点" || analysis.DesignSpec != "规范" || !analysis.NeedsModel || len(analysis.StyleOptions) != 1 {
		t.Fatalf("analysis = %#v", analysis)
	}
	if empty := parseAnalysis("oops"); empty.NeedsModel || empty.DesignSpec != "" || empty.StyleOptions == nil {
		t.Fatalf("empty analysis = %#v", empty)
	}
}

func TestBuildDedupNote(t *testing.T) {
	got := map[string]string{
		"slot_02": "【本页画面要求】：侧面\n  - 主标题必须显示为：“透气”\n  - 副标题必须显示为：\"清爽一夏\"",
		"slot_01": "【本页画面要求】：正面",
	}
	note := buildDedupNote(got, []EcommerceSlot{{ID: "slot_03"}, {ID: "slot_04"}})
	wants := []string{
		"\n【重要·避免重复】以下页面已在之前批次生成，本批新页面必须与它们形成差异化：\n  · slot_01：主题「正面」｜主标题「无」｜副标题「无」\n  · slot_02：主题「侧面」｜主标题「透气」｜副标题「清爽一夏」\n差异化要求：",
		"\n\n【本批内部去重】本次请求包含多个槽位（slot_03、slot_04），\n这些槽位之间的「核心主题 / 主标题 / 副标题 / 视角」也必须各不相同，",
	}
	for _, want := range wants {
		if !strings.Contains(note, want) {
			t.Errorf("dedup note missing %q in:\n%s", want, note)
		}
	}
	if buildDedupNote(map[string]string{}, []EcommerceSlot{{ID: "slot_01"}}) != "" {
		t.Fatal("first single-slot batch must have no note")
	}
}

func TestNormalizeEcommercePrompts(t *testing.T) {
	defaults := ecommercePromptDefaults()
	got, err := normalizeEcommercePrompts(model.EcommercePrompts{
		Analyze: "  ", Plan: " " + defaults.Plan + " ", SellingPoints: "新模板 {{purpose}}{{contextLines}}",
		Variants: map[string][]string{"main": {" 主图A ", ""}, "white": defaults.Variants["white"], "poster": {"x"}},
	})
	if err != nil || got.Analyze != "" || got.Plan != "" || got.SellingPoints != "新模板 {{purpose}}{{contextLines}}" || len(got.Variants) != 1 || got.Variants["main"][0] != "主图A" {
		t.Fatalf("normalized = %#v %v", got, err)
	}
	_, err = normalizeEcommercePrompts(model.EcommercePrompts{Plan: "只剩 {{perSlotLines}} 和 {{slotCount}}"})
	if err == nil || err.Error() != "套图方案规划模板缺少占位符：{{specBlock}}、{{dedupNote}}、{{allIds}}、{{exampleSlotIds}}" {
		t.Fatalf("missing placeholders: %v", err)
	}
	if names := AdminEcommercePromptDefaults().Placeholders; len(names["analyze"]) != 0 || strings.Join(names["sellingPointRewrite"], ",") != "head,purpose,contextLines" {
		t.Fatalf("placeholders = %#v", names)
	}
}
