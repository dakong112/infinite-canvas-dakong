package service

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Prompt templates ported verbatim from the 1688 image playground FALLBACK_TEMPLATES
// (dashboard/image-playground/src/lib/promptTemplates.ts); keep them byte-identical.
// The optional blocks {{contextLines}}, {{specBlock}} and {{dedupNote}} carry their own newlines.
// ponytail: constants for now; move them into admin settings when operators need to edit them.
const ecommerceAnalyzeTemplate = `你是顶级电商视觉总监与产品策划。请仔细观察我附上的商品图，提炼能高效转化的卖点。
注意：要按三种电商图用途【分别】输出卖点，三者侧重不同：
- main（主图）：用于点击转化的强爆点，简短有冲击力；
- detail（详情图）：用于深度种草的卖点，可展开材质/功能/场景；
- white（白底图）：不是营销卖点，而是"白底呈现要点"——仅正面单图、画面只一个商品主体、不要多视图拼图/背面/侧面、保持纯白合规简洁。
另外推荐 5 个适合该商品的电商视觉风格标签（每个 2-6 字短语，如"极简棚拍""生活方式"），放入 styleOptions。
只输出一个合法 JSON 对象，不要任何 Markdown 代码块标记或多余文字，结构严格如下：
{
  "sellingPoints": { "main": "主图卖点", "white": "白底呈现要点", "detail": "详情卖点" },
  "targetAudience": "推测的目标人群",
  "designStyle": "整体设计规范（一段文字，须涵盖：色彩主调+辅助色+背景色、标题/正文字体层级、视觉语言与装饰风格、摄影风格、模特一致性原则），作为整套详情页统一基调",
  "designSpec": "整体设计规范全文(Markdown)，须含：画面语言铁律(所有文字默认中文，某页选无文字则不出字)、色彩规范(主色调含#hex+辅助色+背景色)、字体规范(主标题字体+字号如72pt、副标题字体+字号如36pt、深/浅背景两套字色含#hex)、视觉语言(留白比例+装饰风格)、摄影风格(光线+景深)、模特一致性(needsModel为真时设定同一模特性别/年龄/气质/发型/肤色/体型/穿搭及肢体结构正常约束)、画面质量(高清1K+材质真实感)、尺寸比例",
  "needsModel": true,
  "modelPrompt": "needsModel 为 true 时推荐的模特设定（性别/年龄段/气质）；false 时填不建议模特的原因",
  "styleOptions": ["风格标签1", "风格标签2", "风格标签3", "风格标签4", "风格标签5"]
}`

const ecommerceSellingPointsTemplate = `你是顶级电商产品策划。请观察我附上的商品图，针对以下用途提炼 3 个方向截然不同但都有吸引力的方案：
用途：{{purpose}}{{contextLines}}
只输出一个合法 JSON 对象，不要 Markdown 代码块或多余文字，结构：{"options":["方案1","方案2","方案3"]}`

const ecommerceSellingPointRewriteTemplate = `{{head}}
用途：{{purpose}}{{contextLines}}
只输出一个合法 JSON 对象，不要 Markdown 代码块或多余文字，结构：{"text":"优化后的文案"}`

const ecommercePlanTemplate = `你是顶级电商视觉总监和 AI 绘画提示词工程师。请观察我附上的全部商品图，为下面每个图片槽位（=详情页的一页）撰写一套完整的、结构化的电商详情页设计提示词。
{{specBlock}}每一页都必须严格按以下结构输出（字段顺序与标签固定，主标题严格≤13字、副标题严格≤18字，直接输出标题文案不带字数说明）：
【本页画面要求】：<本页核心主题>
- **展示方式与视角**：<正面/45°/特写/剖面等>
- **设计目标与核心表达**：<本页要传达的信息>
- **产品呈现与占比**：<产品在画面的位置与大小占比>
- **本页图片尺寸比例**：<本页比例>，构图严格按该比例设计。
- **构图与排版**：<居中/对称/分布式 + 图文排版>
- **必须显示的画面文字**：
  - 主标题必须显示为："<按本页语言，≤13字；本页无文字则写：无>"
  - 副标题必须显示为："<按本页语言，≤18字；本页无文字则写：无>"
- **文字位置与排版**：<安全区位置 + 字号 + 字色，引用整体设计规范的字体规范>
- **视觉重点与卖点**：<突出本页卖点>
- **背景与装饰元素**：<背景 + 装饰>
- **氛围、情绪与光影**：<氛围关键词 + 光影>
- **模特要求**：<按本页模特要求；不需要则写：本页不出现模特或人物肢体>
- **禁止事项**：<文字乱码、产品变形、材质失真、背景喧宾夺主；有模特补：额外/缺失手指、扭曲关节>
- **AI生图Prompt(中文)**：<一段可直接用于AI生图的执行型Prompt，含本页比例、清晰度、产品主体、构图、背景、光影、必须显示的主副标题及文字位置、模特一致性/人体结构要求、禁止事项；不写分析过程>
逐页配置（共 {{slotCount}} 页，按此为每页生成上面整套结构）：
{{perSlotLines}}
{{dedupNote}}只输出一个 JSON 对象，key 为槽位 id，value 为该页「整套结构块」的 Markdown 字符串（含上面所有字段），不要输出任何其它文字。
必须包含全部 {{slotCount}} 个 key：{{allIds}}。
例如：{{{exampleSlotIds}}}`

// Slot variants ported verbatim from ecommercePreset.ts.
var ecommerceMainVariants = []string{
	"电商产品主图，浅灰色简约棚拍背景，柔和均匀的顶部漫射光配以轻微补光；图中商品置于画面正中、正面朝向镜头，完整呈现整体造型与比例；表面材质纹理清晰、颜色真实饱和；正面平视镜头，约 50mm 等效焦段，主体居中、四周留适度负空间，浅景深突出主体；高分辨率商业产品摄影质感。约束：无文字、无水印、无logo、无多余道具。",
	"电商产品主图，纯净浅色台面平拍背景，柔和无影散射光；图中商品背面朝上平整铺开，完整展示背部结构、走线/接缝/标签等细节；面料/材质纹理与缝合工艺清晰可见；正俯拍 90 度 top-down 视角，主体居中、构图整洁；干净写实的商业平铺摄影。约束：无文字、无水印、无logo、无杂物。",
	"电商产品细节特写主图，柔和定向侧光勾勒立体感的深色或中性背景；镜头聚焦图中商品的关键工艺细节，微距特写、浅景深背景虚化；材质质感与做工锐利清晰；微距镜头，主体偏置并留呼吸空间；高端商业微距质感。约束：无文字、无水印、无logo。",
	"电商材质质感主图，柔和掠射光强化表面纹理的中性背景；近距离展示图中商品的材质，凸显织纹/纹理/光泽/手感；自然真实的颜色还原；近景特写、低角度掠光突出立体纹理，浅景深；细腻写实的产品材质摄影。约束：无文字、无水印、无logo。",
	"电商局部细节主图，干净中性背景配柔和均匀光；放大展示图中商品上的印花图案/标识/纹样局部，图案边缘锐利、色彩准确、与载体材质贴合自然；局部微距特写、正面平视、主体居中；清晰写实的细节呈现。约束：不要凭空生成虚构文字或额外水印，仅保留既有图案；无多余logo。",
	"电商场景化主图，贴合商品调性的真实生活方式环境，左侧窗户射入的柔和自然光、阴影柔和；图中商品在真实使用/穿着状态下自然呈现，体现使用情境与尺度感；约三分之四视角、平视、浅景深虚化背景突出商品；自然写实的生活方式商业摄影。约束：无文字、无水印、无品牌logo乱入、无杂乱元素。",
}

var ecommerceWhiteVariants = []string{
	"电商纯白底主图，背景为纯白色 #FFFFFF、干净无缝、完全不透明；画面中只有一个商品主体、正面朝向镜头、置于画面正中，完整呈现整体造型；柔和均匀的无影散射光，颜色真实；轮廓边缘干净锐利、无光晕无描边毛刺；正面平视、主体居中并四周留白；干净的电商白底产品图。约束：画面只允许出现单个商品主体且仅正面，严禁多视图拼图、严禁同框出现背面/侧面/多个角度、严禁重复或多件同款商品；纯白背景不透明，无任何阴影、无道具、无文字、无水印、无logo、无背景渐变或杂色。",
}

var ecommerceDetailVariants = []string{
	"竖版电商详情页配图，2:3 竖向构图，干净的浅色品牌背景；图中商品置于画面下部或一侧、突出展示主体与材质细节；上方/另一侧预留大面积干净留白区域作为后期文案排版空间（此区域保持空白，不要由模型生成文字）；柔和均匀商业棚拍光，颜色真实；主体清晰、构图留白平衡；高质感电商详情页视觉。约束：预留区不要生成任何文字，整图无水印、无logo、无牛皮癣文字横幅。",
	"竖版电商详情页配图，2:3 竖向构图，简洁中性背景；图中商品居中偏下突出展示；上方区域用简洁中文短句标注 2-3 个核心卖点（文字简短、字体清晰、排版整齐）；柔和均匀光、真实颜色、细节锐利。约束：文字仅限简短卖点短语，无水印、无logo、无杂乱排版。",
}

var ecommerceSkuVariants = []string{
	"电商 SKU 展示图，纯白色背景 #FFFFFF、干净无缝、完全不透明；画面中只有一个商品主体、正面朝向镜头、置于画面正中，完整呈现该款式的整体造型；柔和均匀的无影散射光，颜色真实还原；轮廓边缘干净锐利、无光晕无描边毛刺；正面平视、主体居中并四周留白。约束：画面只允许出现单个商品主体，纯白背景不透明，无阴影、无道具、无文字、无水印、无logo。",
}

var ecommerceVariantsByKind = map[string][]string{
	"main":   ecommerceMainVariants,
	"white":  ecommerceWhiteVariants,
	"detail": ecommerceDetailVariants,
	"sku":    ecommerceSkuVariants,
}

const ecommerceSlotForbidSuffix = "禁止事项：无文字乱码、无产品结构变形、无材质失真、背景不喧宾夺主。"

var ecommerceTextLangLabels = map[string]string{
	"none": "无文字", "zh": "简体中文", "zh-Hant": "繁体中文", "en": "英文", "ja": "日文", "th": "泰文", "ru": "俄文", "es": "西班牙语",
}

var ecommerceSlotKindLabels = map[string]string{"main": "产品图", "white": "白底图", "detail": "详情页", "sku": "SKU图"}

var ecommerceSlotAspectText = map[string]string{
	"1024x1024": "正方形 1:1 构图，须严格按 1:1 比例设计",
	"1024x1536": "竖版 2:3 构图，须严格按 2:3 比例设计",
	"1536x1024": "横版 3:2 构图，须严格按 3:2 比例设计",
}

var ecommerceKindSellingLabels = map[string]string{
	"main":   "主图（点击转化强爆点，简短有冲击力）",
	"white":  "白底图（白底呈现要点：仅正面单图、画面只一个商品主体、纯白合规简洁，非营销卖点）",
	"detail": "详情图（深度种草卖点，可展开材质/功能/场景）",
	"sku":    "SKU图（纯白底呈现某一具体款式，非营销卖点）",
}

var ecommerceKindPlanRequirements = map[string]string{
	"main":   "产品主图：各视角有区分度、突出产品本体、干净合规",
	"white":  "白底图：纯白背景 #FFFFFF、无阴影无道具、轮廓干净锐利；画面只一个商品主体且仅正面，不要多视图拼图、不要背面/侧面同框、不要重复商品",
	"detail": "详情页：体现详情卖点、留文案排版空间",
	"sku":    "SKU图：纯白底呈现某一具体款式",
}

// ecommerceSellingKinds are the slot kinds that carry selling points; SKU images skip analyze/plan.
var ecommerceSellingKinds = []string{"main", "white", "detail"}

// ecommerceSlotTypes are the R-SET-4 standard set image types mapped onto the 1688 slot kinds.
var ecommerceSlotTypes = []struct{ Key, Title, Kind, Size string }{
	{"white", "白底主图", "white", "1024x1024"},
	{"brand", "品牌主视觉海报", "main", "1024x1024"},
	{"selling", "核心卖点海报", "main", "1024x1024"},
	{"material", "材质与结构说明图", "detail", "1024x1536"},
	{"craft", "工艺细节图", "detail", "1024x1536"},
	{"spec", "规格与品质信任图", "detail", "1024x1536"},
	{"scene", "真实使用场景图", "main", "1024x1024"},
	{"closing", "收官价值视觉图", "detail", "1024x1536"},
}

const (
	ecommerceMinSlots       = 7
	ecommerceMaxSlots       = 15
	ecommerceMaxPerSlotType = 5
	ecommerceMaxSkus        = 10
)

var (
	promptTemplateVar   = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)
	fencedJSONPattern   = regexp.MustCompile("(?is)```(?:json)?\\s*(.*?)```")
	salvageSlotPattern  = regexp.MustCompile(`"(slot_\d+)"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	slotTopicPattern    = regexp.MustCompile(`【本页画面要求】\s*[:：]\s*(.+)`)
	slotTitlePattern    = regexp.MustCompile(`(?m)主标题必须显示为\s*[:：]\s*["“]?\s*(.*?)\s*["”]?\s*$`)
	slotSubtitlePattern = regexp.MustCompile(`(?m)副标题必须显示为\s*[:：]\s*["“]?\s*(.*?)\s*["”]?\s*$`)
)

type EcommerceSellingPoints struct {
	Main   string `json:"main"`
	White  string `json:"white"`
	Detail string `json:"detail"`
}

func (points EcommerceSellingPoints) forKind(kind string) string {
	switch kind {
	case "main":
		return points.Main
	case "white":
		return points.White
	case "detail":
		return points.Detail
	}
	return ""
}

func (points *EcommerceSellingPoints) setKind(kind string, text string) {
	switch kind {
	case "main":
		points.Main = text
	case "white":
		points.White = text
	case "detail":
		points.Detail = text
	}
}

// EcommerceAnalysis is the parsed analyze reply (1688 AnalysisResult).
type EcommerceAnalysis struct {
	SellingPoints  EcommerceSellingPoints `json:"sellingPoints"`
	TargetAudience string                 `json:"targetAudience"`
	DesignStyle    string                 `json:"designStyle"`
	DesignSpec     string                 `json:"designSpec"`
	NeedsModel     bool                   `json:"needsModel"`
	ModelPrompt    string                 `json:"modelPrompt"`
	StyleOptions   []string               `json:"styleOptions"`
}

// EcommerceSlot is one image of a set (1688 ImageSlot plus the queue job IDs).
type EcommerceSlot struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Size         string `json:"size"`
	Title        string `json:"title"`
	Theme        string `json:"theme,omitempty"`
	TextLang     string `json:"textLang"`
	NeedsModel   *bool  `json:"needsModel,omitempty"`
	SkuDesc      string `json:"skuDesc,omitempty"`
	SkuFileID    string `json:"skuFileId,omitempty"`
	PreviewJobID string `json:"previewJobId,omitempty"`
	FinalJobID   string `json:"finalJobId,omitempty"`
}

type EcommerceStructure struct {
	Mode   string         `json:"mode"`
	Counts map[string]int `json:"counts,omitempty"`
}

type EcommerceSku struct {
	Label  string `json:"label"`
	Desc   string `json:"desc"`
	FileID string `json:"fileId"`
}

// EcommerceSetForm is what the user fills in on the left of the set page.
type EcommerceSetForm struct {
	Preset           string                  `json:"preset"`
	ProductFileIDs   []string                `json:"productFileIds"`
	Market           string                  `json:"market"`
	Platform         string                  `json:"platform"`
	Language         string                  `json:"language"`
	StyleMode        string                  `json:"styleMode"`
	StyleText        string                  `json:"styleText"`
	ReferenceFileID  string                  `json:"referenceFileId"`
	ModelDescription string                  `json:"modelDescription"`
	Structure        EcommerceStructure      `json:"structure"`
	Skus             []EcommerceSku          `json:"skus"`
	SellingPoints    *EcommerceSellingPoints `json:"sellingPoints,omitempty"`
}

type ecommercePlanOptions struct {
	SellingPoints    EcommerceSellingPoints
	NeedsModel       bool
	ModelDescription string
	QualityLabel     string
	DesignSpec       string
	DedupNote        string
}

type sellingPointContext struct {
	Current          string
	DesignStyle      string
	OtherPoints      EcommerceSellingPoints
	ModelDescription string
	NeedsModel       bool
}

// renderPromptTemplate replaces {{name}}; unknown names become "" so no placeholder leaks to the model.
// Only double braces are consumed, so "{{{x}}}" renders as "{" + x + "}".
func renderPromptTemplate(template string, vars map[string]string) string {
	return promptTemplateVar.ReplaceAllStringFunc(template, func(match string) string {
		return vars[promptTemplateVar.FindStringSubmatch(match)[1]]
	})
}

func slotAspectClause(size string) string {
	return ecommerceSlotAspectText[size]
}

func slotTextClause(textLang string) string {
	if textLang == "" {
		textLang = "zh"
	}
	if textLang == "none" {
		return "本页不出现任何画面文字（无主标题、无副标题）。"
	}
	return "本页须含主标题（≤13 字）+ 副标题（≤18 字），语言：" + ecommerceTextLangLabels[textLang] + "；直接输出标题文案，不带字数说明。"
}

func qualityToLabel(quality string) string {
	switch quality {
	case "medium":
		return "标准质感"
	case "low":
		return "轻量质感"
	default:
		return "高清高质感"
	}
}

// buildModelClause: false → no model; true → model with the description; nil → no constraint ("").
func buildModelClause(needsModel *bool, modelDescription string) string {
	if needsModel != nil && !*needsModel {
		return "本图不要出现模特、人物主体或人物肢体。"
	}
	if needsModel == nil {
		return ""
	}
	who := "请根据产品与目标人群选择合适模特，明确性别、年龄段、气质。"
	if desc := strings.TrimSpace(modelDescription); desc != "" {
		who = "模特设定「" + desc + "」。"
	}
	return "本图需出现模特：" + who + "模特动作自然，手、脚、腿、手指、关节、肢体比例和面部结构必须符合正常人类生理结构，禁止畸形肢体、额外手指、缺失手指、扭曲关节、反向肢体。"
}

// buildSlotPrompt is the template prompt used when a slot has no planned prompt.
func buildSlotPrompt(kind string, indexInKind int, needsModel *bool) string {
	list := ecommerceVariantsByKind[kind]
	base := list[indexInKind%len(list)] + "\n" + ecommerceSlotForbidSuffix
	if clause := buildModelClause(needsModel, ""); clause != "" {
		return base + "\n" + clause
	}
	return base
}

// buildSkuSlotPrompt: SKU images skip planning and use the white SKU template plus the SKU description.
func buildSkuSlotPrompt(skuDesc string) string {
	base := buildSlotPrompt("sku", 0, nil)
	desc := strings.TrimSpace(skuDesc)
	if desc == "" {
		return base
	}
	return base + "\n本图呈现的 SKU：" + desc + "。请如实还原该款式的颜色、材质与形态，不要臆造其它款式。"
}

// composeSlotGenerationPrompt prefixes the shared design spec to one page block.
func composeSlotGenerationPrompt(designSpec string, slotPrompt string) string {
	spec := strings.TrimSpace(designSpec)
	if spec == "" {
		return slotPrompt
	}
	return spec + "\n\n---\n\n" + slotPrompt
}

func buildAnalyzeInstruction() string {
	return renderPromptTemplate(ecommerceAnalyzeTemplate, nil)
}

func sellingPointContextLines(kind string, ctx sellingPointContext) string {
	var lines []string
	if style := strings.TrimSpace(ctx.DesignStyle); style != "" {
		lines = append(lines, "设计风格："+style)
	}
	var others []string
	for _, other := range ecommerceSellingKinds {
		if other == kind {
			continue
		}
		if value := strings.TrimSpace(ctx.OtherPoints.forKind(other)); value != "" {
			others = append(others, ecommerceSlotKindLabels[other]+"「"+value+"」")
		}
	}
	if len(others) > 0 {
		lines = append(lines, "其它卖点（供保持整体调性参考，请勿照抄）："+strings.Join(others, "，"))
	}
	if desc := strings.TrimSpace(ctx.ModelDescription); ctx.NeedsModel && desc != "" {
		lines = append(lines, "模特设定："+desc)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

func buildSellingPointsInstruction(kind string, ctx sellingPointContext) string {
	return renderPromptTemplate(ecommerceSellingPointsTemplate, map[string]string{
		"purpose":      ecommerceKindSellingLabels[kind],
		"contextLines": sellingPointContextLines(kind, ctx),
	})
}

func buildSellingPointRewriteInstruction(kind string, ctx sellingPointContext) string {
	head := "你是顶级电商产品策划。请观察我附上的商品图，为以下用途写一段有吸引力的卖点文案："
	if current := strings.TrimSpace(ctx.Current); current != "" {
		head = "你是顶级电商产品策划。请基于我附上的商品图，在保持以下文案核心诉求与方向不变的前提下，将其优化得更专业、更有冲击力、更贴合用途：\n当前文案：" + current
	}
	return renderPromptTemplate(ecommerceSellingPointRewriteTemplate, map[string]string{
		"head":         head,
		"purpose":      ecommerceKindSellingLabels[kind],
		"contextLines": sellingPointContextLines(kind, ctx),
	})
}

// buildPlanInstruction ports 1688 buildPlanInstruction; SKU slots are never planned.
func buildPlanInstruction(slots []EcommerceSlot, opts ecommercePlanOptions) string {
	targets := ecommercePlanTargets(slots)
	lines := make([]string, 0, len(targets))
	ids := make([]string, 0, len(targets))
	for _, slot := range targets {
		needsModel := opts.NeedsModel
		if slot.NeedsModel != nil {
			needsModel = *slot.NeedsModel
		}
		modelLine := "  本页模特：本图不要出现模特、人物主体或人物肢体。"
		if clause := buildModelClause(&needsModel, opts.ModelDescription); clause != "" {
			modelLine = "  本页模特：" + clause
		}
		quality := ""
		if opts.QualityLabel != "" {
			quality = "；画面质量：" + opts.QualityLabel
		}
		sellingPoint := opts.SellingPoints.forKind(slot.Kind)
		if sellingPoint == "" {
			sellingPoint = "无，请据图自行提炼"
		}
		block := []string{
			fmt.Sprintf(`- "%s"（%s）：`, slot.ID, ecommerceKindPlanRequirements[slot.Kind]),
			"  本页尺寸比例：" + slotAspectClause(slot.Size) + quality + "。",
			"  本页卖点（写进「视觉重点与卖点」与「AI生图Prompt」）：「" + sellingPoint + "」。",
			"  本页画面文字：" + slotTextClause(slot.TextLang),
			modelLine,
		}
		// Not in 1688: custom structures name the R-SET-4 image type of each page.
		if slot.Theme != "" {
			block = append(block, "  本页主题："+slot.Theme+"（围绕该主题展开本页画面）。")
		}
		lines = append(lines, strings.Join(block, "\n"))
		ids = append(ids, `"`+slot.ID+`"`)
	}
	specBlock := ""
	if spec := strings.TrimSpace(opts.DesignSpec); spec != "" {
		specBlock = "整体设计规范（所有页必须严格遵循此规范的色彩/字体/模特一致性/比例）：\n" + spec + "\n"
	}
	dedupNote := ""
	if strings.TrimSpace(opts.DedupNote) != "" {
		dedupNote = opts.DedupNote + "\n"
	}
	examples := make([]string, 0, 2)
	for _, id := range ids[:min(len(ids), 2)] {
		examples = append(examples, id+`:"..."`)
	}
	return renderPromptTemplate(ecommercePlanTemplate, map[string]string{
		"specBlock":      specBlock,
		"slotCount":      fmt.Sprint(len(targets)),
		"perSlotLines":   strings.Join(lines, "\n"),
		"dedupNote":      dedupNote,
		"allIds":         strings.Join(ids, "、"),
		"exampleSlotIds": strings.Join(examples, ","),
	})
}

func ecommercePlanTargets(slots []EcommerceSlot) []EcommerceSlot {
	targets := make([]EcommerceSlot, 0, len(slots))
	for _, slot := range slots {
		if slot.Kind != "sku" {
			targets = append(targets, slot)
		}
	}
	return targets
}

// buildDedupNote lists pages planned in earlier rounds and asks this round's pages to differ.
// Unlike 1688 (object insertion order) earlier pages are listed by slot ID.
func buildDedupNote(got map[string]string, batch []EcommerceSlot) string {
	var sections []string
	var items []string
	for _, id := range slices.Sorted(maps.Keys(got)) {
		block := got[id]
		if strings.TrimSpace(block) == "" {
			continue
		}
		topic, title, subtitle := extractSlotSummary(block)
		items = append(items, fmt.Sprintf("  · %s：主题「%s」｜主标题「%s」｜副标题「%s」", id, firstNonEmpty(topic, "?"), firstNonEmpty(title, "无"), firstNonEmpty(subtitle, "无")))
	}
	if len(items) > 0 {
		lines := append([]string{"", "【重要·避免重复】以下页面已在之前批次生成，本批新页面必须与它们形成差异化："}, items...)
		lines = append(lines,
			"差异化要求：",
			"- 视角/展示方式不得与上方任一页雷同（已用过的 正面/45°/特写/俯视/剖面 等要换新角度）。",
			"- 主标题、副标题文案不得与上方重复或近义复述，要覆盖不同卖点维度。",
			"- 核心主题（本页要讲的点）必须是上方尚未覆盖的新角度。",
			"- 但配色/字体/模特形象/整体风格必须与整体设计规范完全一致（只在内容上差异化，不在风格上跑偏）。",
		)
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(batch) > 1 {
		ids := make([]string, 0, len(batch))
		for _, slot := range batch {
			ids = append(ids, slot.ID)
		}
		sections = append(sections, strings.Join([]string{
			"",
			"【本批内部去重】本次请求包含多个槽位（" + strings.Join(ids, "、") + "），",
			"这些槽位之间的「核心主题 / 主标题 / 副标题 / 视角」也必须各不相同，",
			"严禁给本批不同槽位写出相同或近义复述的主标题或副标题，每页覆盖不同卖点维度与展示角度。",
		}, "\n"))
	}
	return strings.Join(sections, "\n")
}

func extractSlotSummary(block string) (string, string, string) {
	read := func(pattern *regexp.Regexp) string {
		if match := pattern.FindStringSubmatch(block); match != nil {
			return strings.TrimSpace(match[1])
		}
		return ""
	}
	return read(slotTopicPattern), read(slotTitlePattern), read(slotSubtitlePattern)
}

// extractJSONObject takes the first {...} span (inside a code fence when present) and decodes it.
func extractJSONObject(text string) map[string]any {
	candidate := text
	if match := fencedJSONPattern.FindStringSubmatch(text); match != nil {
		candidate = match[1]
	}
	start, end := strings.Index(candidate, "{"), strings.LastIndex(candidate, "}")
	if start < 0 || end <= start {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(candidate[start:end+1]), &object) != nil {
		return nil
	}
	return object
}

func jsonText(value any) string {
	text, _ := value.(string)
	return text
}

func parseRegeneratedPrompts(text string) map[string]string {
	result := map[string]string{}
	for key, value := range extractJSONObject(text) {
		if prompt, ok := value.(string); ok {
			result[key] = prompt
		}
	}
	return result
}

// salvageTruncatedJSON recovers the fully closed "slot_xx":"..." entries of a reply cut off at the
// gateway's output limit; the unterminated tail entry and unexpected IDs are dropped.
func salvageTruncatedJSON(text string, expectedIDs []string) map[string]string {
	result := map[string]string{}
	for _, match := range salvageSlotPattern.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(expectedIDs, match[1]) {
			continue
		}
		var value string
		if json.Unmarshal([]byte(`"`+match[2]+`"`), &value) == nil {
			result[match[1]] = value
		}
	}
	return result
}

// parsePlanOutput reads one plan reply: complete JSON wins, truncated replies keep their closed entries.
func parsePlanOutput(text string, targetIDs []string) map[string]string {
	merged := salvageTruncatedJSON(text, targetIDs)
	maps.Copy(merged, parseRegeneratedPrompts(text))
	result := map[string]string{}
	for id, prompt := range merged {
		if slices.Contains(targetIDs, id) && strings.TrimSpace(prompt) != "" {
			result[id] = prompt
		}
	}
	return result
}

func parseAnalysis(text string) EcommerceAnalysis {
	analysis := EcommerceAnalysis{StyleOptions: []string{}}
	object := extractJSONObject(text)
	if object == nil {
		return analysis
	}
	points, _ := object["sellingPoints"].(map[string]any)
	analysis.SellingPoints = EcommerceSellingPoints{Main: jsonText(points["main"]), White: jsonText(points["white"]), Detail: jsonText(points["detail"])}
	analysis.TargetAudience = jsonText(object["targetAudience"])
	analysis.DesignStyle = jsonText(object["designStyle"])
	analysis.DesignSpec = jsonText(object["designSpec"])
	analysis.NeedsModel = object["needsModel"] == true
	analysis.ModelPrompt = jsonText(object["modelPrompt"])
	styles, _ := object["styleOptions"].([]any)
	for _, style := range styles {
		if text, ok := style.(string); ok {
			analysis.StyleOptions = append(analysis.StyleOptions, text)
		}
	}
	return analysis
}

func parseSellingOptions(text string) []string {
	options := []string{}
	values, _ := extractJSONObject(text)["options"].([]any)
	for _, value := range values {
		if option, ok := value.(string); ok {
			options = append(options, option)
		}
	}
	return options
}

func parseSellingPointText(text string) string {
	return strings.TrimSpace(jsonText(extractJSONObject(text)["text"]))
}

// buildEcommerceSlots lays out the pages of a set: smart = the 1688 default 9, custom = counts per
// standard image type (7–15 pages), detail preset = 7 vertical detail screens; SKU images follow.
func buildEcommerceSlots(form EcommerceSetForm) ([]EcommerceSlot, error) {
	var slots []EcommerceSlot
	switch {
	case form.Preset == "detail":
		for _, slotType := range ecommerceSlotTypes {
			if slotType.Kind != "white" {
				slots = append(slots, EcommerceSlot{Kind: "detail", Size: "1024x1536", Title: slotType.Title, Theme: slotType.Title})
			}
		}
	case form.Structure.Mode == "custom":
		total := 0
		for key, count := range form.Structure.Counts {
			known := slices.ContainsFunc(ecommerceSlotTypes, func(slotType struct{ Key, Title, Kind, Size string }) bool { return slotType.Key == key })
			if !known || count < 0 || count > ecommerceMaxPerSlotType {
				return nil, safeMessageError{message: fmt.Sprintf("套图结构无效：每类图片 0-%d 张", ecommerceMaxPerSlotType)}
			}
			total += count
		}
		if total < ecommerceMinSlots || total > ecommerceMaxSlots {
			return nil, safeMessageError{message: fmt.Sprintf("套图需要 %d-%d 张，当前 %d 张", ecommerceMinSlots, ecommerceMaxSlots, total)}
		}
		for _, slotType := range ecommerceSlotTypes {
			count := form.Structure.Counts[slotType.Key]
			for index := range count {
				title := slotType.Title
				if count > 1 {
					title = fmt.Sprintf("%s %d", slotType.Title, index+1)
				}
				slots = append(slots, EcommerceSlot{Kind: slotType.Kind, Size: slotType.Size, Title: title, Theme: slotType.Title})
			}
		}
	default:
		// 1688 DEFAULT_LAYOUT: 4 product images, 1 white image (no model, no text), 4 detail images.
		for index := range 4 {
			slots = append(slots, EcommerceSlot{Kind: "main", Size: "1024x1024", Title: fmt.Sprintf("%s %d", ecommerceSlotKindLabels["main"], index+1)})
		}
		slots = append(slots, EcommerceSlot{Kind: "white", Size: "1024x1024", Title: ecommerceSlotKindLabels["white"]})
		for index := range 4 {
			slots = append(slots, EcommerceSlot{Kind: "detail", Size: "1024x1536", Title: fmt.Sprintf("%s %d", ecommerceSlotKindLabels["detail"], index+1)})
		}
	}
	noModel := false
	for index := range slots {
		slots[index].ID = fmt.Sprintf("slot_%02d", index+1)
		slots[index].TextLang = form.Language
		if slots[index].Kind == "white" {
			slots[index].TextLang = "none"
			slots[index].NeedsModel = &noModel
		}
	}
	for index, sku := range form.Skus {
		desc := sku.Label
		if sku.Desc != "" {
			desc += "（" + sku.Desc + "）"
		}
		slots = append(slots, EcommerceSlot{
			ID: fmt.Sprintf("sku_%02d", index+1), Kind: "sku", Size: "1024x1024", Title: "SKU图-" + sku.Label,
			TextLang: "none", NeedsModel: &noModel, SkuDesc: desc, SkuFileID: sku.FileID,
		})
	}
	return slots, nil
}
