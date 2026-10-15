package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	ecommercePurposeAnalyze = "analyze"
	ecommercePurposePlan    = "plan"
	ecommercePurposeRewrite = "rewrite"
	ecommercePurposeOptions = "options"
	ecommerceMaxPlanRounds  = 3
	ecommerceMaxProducts    = 6
	ecommerceMaxImageBytes  = 10 << 20
	ecommerceReferenceNote  = "最后一张图是用户提供的参考排版：请参考它的版式结构、配色与构图节奏，不要照搬其中的商品、品牌和文字。"
)

var (
	errEcommerceSetChanged = safeMessageError{message: "套图状态已更新，请刷新后重试"}
	errEcommerceBusy       = safeMessageError{message: "AI 任务进行中，请稍候"}
	hexColorPattern        = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// EcommercePendingJob is an AI job whose result has not been applied to the set yet.
type EcommercePendingJob struct {
	JobID   string   `json:"jobId"`
	Purpose string   `json:"purpose"`
	Kind    string   `json:"kind,omitempty"`
	SlotIDs []string `json:"slotIds,omitempty"`
}

// EcommerceSet is the workflow state saved in generation_batches.context_json.
type EcommerceSet struct {
	Form           EcommerceSetForm      `json:"form"`
	Slots          []EcommerceSlot       `json:"slots"`
	Analysis       *EcommerceAnalysis    `json:"analysis"`
	SellingOptions map[string][]string   `json:"sellingOptions"`
	SlotPrompts    map[string]string     `json:"slotPrompts"`
	Planned        bool                  `json:"planned"`
	PlanRound      int                   `json:"planRound"`
	Pending        []EcommercePendingJob `json:"pending"`
	Notice         string                `json:"notice"`
}

type EcommerceSetView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	EcommerceSet
	Jobs []model.GenerationJob `json:"jobs"`
}

type EcommerceSetSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Preset    string    `json:"preset"`
	CoverURL  string    `json:"coverUrl"`
	CreatedAt time.Time `json:"createdAt"`
}

type EcommerceToolInput struct {
	FileIDs []string          `json:"fileIds"`
	Params  map[string]string `json:"params"`
	Count   int               `json:"count"`
}

// ecommerceTools are thin presets over the job queue; every tool calls /images/edits once per image.
var ecommerceTools = map[string]struct {
	Title     string
	MinInputs int
	MaxInputs int
	Params    []string
}{
	"replicate": {"图片复刻", 2, 4, []string{"extra", "size"}},
	"remove-bg": {"去除背景", 1, 1, []string{"background", "color", "size"}},
	"tryon":     {"AI 换装 / 模特图", 1, 4, []string{"mode", "description", "hasModelImage", "extra", "size"}},
}

const ecommerceReplicatePrompt = "请以第一张图为参考图，生成一张同风格、同构图的电商图片：保持参考图的构图布局、拍摄视角、光影氛围、配色与排版风格；画面中的商品换成其余图片中的商品，如实还原商品的外观、材质、颜色与结构；不要照搬参考图中的商品、品牌标识和文字。{{extra}}\n" + ecommerceSlotForbidSuffix

const ecommerceRemoveBackgroundPrompt = "请去除图片背景，只保留商品主体：主体轮廓边缘干净精细，无残留背景、无光晕毛刺；商品本身的形状、颜色、材质与细节保持不变；不要添加任何文字、水印或装饰。背景要求：{{background}}。"

const ecommerceTryonWearPrompt = "请把商品图中的服饰自然地穿到模特身上，生成真实的电商模特展示图：{{model}}服饰的款式、颜色、图案、材质与版型必须与商品图一致，穿着贴合自然、褶皱合理；模特动作自然，手、脚、腿、手指、关节、肢体比例和面部结构必须符合正常人类生理结构，禁止畸形肢体、额外手指、缺失手指、扭曲关节、反向肢体。{{extra}}"

const ecommerceTryonScenePrompt = "请为商品图中的商品添加合适的模特与使用场景，生成真实的电商场景图：{{model}}商品的外观、颜色、材质与结构必须与商品图一致，商品在画面中清晰突出；场景贴合商品调性，光影自然；模特肢体结构必须符合正常人类生理结构，禁止畸形肢体、额外手指、扭曲关节。{{extra}}"

func ecommerceSetting() (model.PublicEcommerceSetting, error) {
	settings, err := PublicSettings()
	if err != nil {
		return model.PublicEcommerceSetting{}, err
	}
	if settings.Ecommerce.VisionModel == "" || settings.Ecommerce.ImageModel == "" {
		return settings.Ecommerce, safeMessageError{message: "管理员尚未配置电商识图模型或生图模型"}
	}
	return settings.Ecommerce, nil
}

// CreateEcommerceSet starts a product set (preset "set") or a detail/A+ page (preset "detail").
func CreateEcommerceSet(ctx context.Context, user model.AuthUser, form EcommerceSetForm) (EcommerceSetView, error) {
	form.SellingPoints = nil
	if err := normalizeEcommerceForm(&form, user.ID, true); err != nil {
		return EcommerceSetView{}, err
	}
	slots, err := buildEcommerceSlots(form)
	if err != nil {
		return EcommerceSetView{}, err
	}
	set := EcommerceSet{Form: form, Slots: slots, SellingOptions: map[string][]string{}, SlotPrompts: map[string]string{}, Pending: []EcommercePendingJob{}}
	raw, err := json.Marshal(set)
	if err != nil {
		return EcommerceSetView{}, err
	}
	label := "商品套图"
	if form.Preset == "detail" {
		label = "详情图"
	}
	batch := model.GenerationBatch{
		ID: newID("genbatch"), UserID: user.ID, Source: model.GenerationSourceEcommerceSet,
		Title: label + " · " + form.Platform + " · " + time.Now().Format("01-02 15:04"), ContextJSON: string(raw), Version: 1, CreatedAt: generationNow(),
	}
	if err := repository.CreateGenerationBatch(batch, nil); err != nil {
		return EcommerceSetView{}, err
	}
	return ecommerceSetView(batch, set, []model.GenerationJob{}), nil
}

// GetEcommerceSet returns the set after applying any AI jobs that finished since the last read.
func GetEcommerceSet(ctx context.Context, user model.AuthUser, id string) (EcommerceSetView, error) {
	batch, set, err := loadEcommerceSet(user.ID, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	jobs, err := repository.ListUserBatchGenerationJobs(user.ID, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	created, changed := advanceEcommerceSet(ctx, user, batch, &set, jobs)
	if !changed {
		return ecommerceSetView(batch, set, jobs), nil
	}
	if err := commitEcommerceSet(batch, set, created); err != nil {
		if !errors.Is(err, errEcommerceSetChanged) {
			return EcommerceSetView{}, err
		}
		// Another request advanced the set first; show its result.
		if batch, set, err = loadEcommerceSet(user.ID, id); err != nil {
			return EcommerceSetView{}, err
		}
		if jobs, err = repository.ListUserBatchGenerationJobs(user.ID, id); err != nil {
			return EcommerceSetView{}, err
		}
		return ecommerceSetView(batch, set, jobs), nil
	}
	return ecommerceSetView(batch, set, append(jobs, created...)), nil
}

// UpdateEcommerceSet saves form edits; layout changes rebuild the slots, and anything the plan
// depends on resets it. Product images and the preset are fixed for the life of a set.
func UpdateEcommerceSet(ctx context.Context, user model.AuthUser, id string, form EcommerceSetForm) (EcommerceSetView, error) {
	batch, set, created, err := openEcommerceSet(ctx, user, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	if len(set.Pending) > 0 {
		return EcommerceSetView{}, errEcommerceBusy
	}
	sellingPoints := form.SellingPoints
	form.SellingPoints = nil
	form.Preset, form.ProductFileIDs = set.Form.Preset, set.Form.ProductFileIDs
	if err := normalizeEcommerceForm(&form, user.ID, false); err != nil {
		return EcommerceSetView{}, err
	}
	before := ecommercePlanKey(set)
	if ecommerceLayoutKey(set.Form) != ecommerceLayoutKey(form) {
		slots, err := buildEcommerceSlots(form)
		if err != nil {
			return EcommerceSetView{}, err
		}
		set.Slots = slots
	}
	set.Form = form
	if sellingPoints != nil && set.Analysis != nil {
		for _, text := range []string{sellingPoints.Main, sellingPoints.White, sellingPoints.Detail} {
			if len([]rune(strings.TrimSpace(text))) > 500 {
				return EcommerceSetView{}, safeMessageError{message: "单条卖点不能超过 500 字"}
			}
		}
		set.Analysis.SellingPoints = EcommerceSellingPoints{Main: strings.TrimSpace(sellingPoints.Main), White: strings.TrimSpace(sellingPoints.White), Detail: strings.TrimSpace(sellingPoints.Detail)}
	}
	if ecommercePlanKey(set) != before {
		resetEcommercePlan(&set)
	}
	if err := commitEcommerceSet(batch, set, created); err != nil {
		return EcommerceSetView{}, err
	}
	return GetEcommerceSet(ctx, user, id)
}

// AnalyzeEcommerceSet queues the vision job that extracts selling points, style tags and the design spec.
func AnalyzeEcommerceSet(ctx context.Context, user model.AuthUser, id string) (EcommerceSetView, error) {
	batch, set, created, err := openEcommerceSet(ctx, user, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	if len(set.Pending) > 0 {
		return EcommerceSetView{}, errEcommerceBusy
	}
	setting, err := ecommerceSetting()
	if err != nil {
		return EcommerceSetView{}, err
	}
	job, err := newEcommerceVisionJob(ctx, user, batch, setting.VisionModel, ecommerceTargetClause(set.Form)+buildAnalyzeInstruction(), set.Form.ProductFileIDs, "一键解析卖点")
	if err != nil {
		return EcommerceSetView{}, err
	}
	set.Pending = append(set.Pending, EcommercePendingJob{JobID: job.ID, Purpose: ecommercePurposeAnalyze})
	set.Notice = ""
	if err := commitEcommerceSet(batch, set, append(created, job)); err != nil {
		return EcommerceSetView{}, err
	}
	return GetEcommerceSet(ctx, user, id)
}

// RequestEcommerceSellingPoints rewrites one selling point (mode "rewrite") or proposes three
// alternatives (mode "options") with the 1688 sellingPointRewrite / sellingPoints templates.
func RequestEcommerceSellingPoints(ctx context.Context, user model.AuthUser, id string, kind string, mode string) (EcommerceSetView, error) {
	if !slices.Contains(ecommerceSellingKinds, kind) {
		return EcommerceSetView{}, safeMessageError{message: "卖点类型无效"}
	}
	if mode != ecommercePurposeRewrite && mode != ecommercePurposeOptions {
		return EcommerceSetView{}, safeMessageError{message: "操作无效"}
	}
	batch, set, created, err := openEcommerceSet(ctx, user, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	if len(set.Pending) > 0 {
		return EcommerceSetView{}, errEcommerceBusy
	}
	if set.Analysis == nil {
		return EcommerceSetView{}, safeMessageError{message: "请先一键解析卖点"}
	}
	setting, err := ecommerceSetting()
	if err != nil {
		return EcommerceSetView{}, err
	}
	pointContext := sellingPointContext{DesignStyle: set.Analysis.DesignStyle, OtherPoints: set.Analysis.SellingPoints, ModelDescription: ecommerceModelDescription(set), NeedsModel: set.Analysis.NeedsModel}
	instruction := buildSellingPointsInstruction(kind, pointContext)
	if mode == ecommercePurposeRewrite {
		pointContext.Current = set.Analysis.SellingPoints.forKind(kind)
		instruction = buildSellingPointRewriteInstruction(kind, pointContext)
	}
	job, err := newEcommerceVisionJob(ctx, user, batch, setting.VisionModel, ecommerceTargetClause(set.Form)+instruction, set.Form.ProductFileIDs, "AI 卖点")
	if err != nil {
		return EcommerceSetView{}, err
	}
	set.Pending = append(set.Pending, EcommercePendingJob{JobID: job.ID, Purpose: mode, Kind: kind})
	if err := commitEcommerceSet(batch, set, append(created, job)); err != nil {
		return EcommerceSetView{}, err
	}
	return GetEcommerceSet(ctx, user, id)
}

// PlanEcommerceSet queues round 1 of the page plan for every non-SKU slot.
func PlanEcommerceSet(ctx context.Context, user model.AuthUser, id string) (EcommerceSetView, error) {
	batch, set, created, err := openEcommerceSet(ctx, user, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	if len(set.Pending) > 0 {
		return EcommerceSetView{}, errEcommerceBusy
	}
	if set.Analysis == nil {
		return EcommerceSetView{}, safeMessageError{message: "请先一键解析卖点"}
	}
	resetEcommercePlan(&set)
	targets := ecommercePlanTargets(set.Slots)
	if len(targets) == 0 {
		set.Planned = true
	} else {
		job, err := newEcommercePlanJob(ctx, user, batch, set, targets)
		if err != nil {
			return EcommerceSetView{}, err
		}
		set.PlanRound = 1
		set.Pending = append(set.Pending, EcommercePendingJob{JobID: job.ID, Purpose: ecommercePurposePlan, SlotIDs: ecommerceSlotIDs(targets)})
		created = append(created, job)
	}
	if err := commitEcommerceSet(batch, set, created); err != nil {
		return EcommerceSetView{}, err
	}
	return GetEcommerceSet(ctx, user, id)
}

// GenerateEcommerceSet queues one image job per slot: preview at the cheap quality for all (or the
// given) slots, final at full quality for the slots the user picked.
func GenerateEcommerceSet(ctx context.Context, user model.AuthUser, id string, stage string, slotIDs []string) (EcommerceSetView, error) {
	if stage != "preview" && stage != "final" {
		return EcommerceSetView{}, safeMessageError{message: "生成阶段无效"}
	}
	if stage == "final" && len(slotIDs) == 0 {
		return EcommerceSetView{}, safeMessageError{message: "请选择要生成正式图的图片"}
	}
	batch, set, created, err := openEcommerceSet(ctx, user, id)
	if err != nil {
		return EcommerceSetView{}, err
	}
	setting, err := ecommerceSetting()
	if err != nil {
		return EcommerceSetView{}, err
	}
	quality := setting.PreviewQuality
	if stage == "final" {
		quality = setting.FinalQuality
	}
	designSpec := ecommerceDesignSpec(set)
	indexInKind := map[string]int{}
	counters := map[string]int{}
	for _, slot := range set.Slots {
		indexInKind[slot.ID] = counters[slot.Kind]
		counters[slot.Kind]++
	}
	channelID := ecommerceChannelID(user.ID)
	var selected []int
	var items []GenerationJobInput
	for index, slot := range set.Slots {
		if len(slotIDs) > 0 && !slices.Contains(slotIDs, slot.ID) {
			continue
		}
		prompt := set.SlotPrompts[slot.ID]
		inputs := set.Form.ProductFileIDs
		if slot.Kind == "sku" {
			prompt = buildSkuSlotPrompt(slot.SkuDesc)
			inputs = []string{firstNonEmpty(slot.SkuFileID, set.Form.ProductFileIDs[0])}
		} else if !set.Planned {
			return EcommerceSetView{}, safeMessageError{message: "请先生成套图方案"}
		} else if prompt == "" {
			prompt = buildSlotPrompt(slot.Kind, indexInKind[slot.ID], slot.NeedsModel)
		}
		full := composeSlotGenerationPrompt(designSpec, prompt)
		selected = append(selected, index)
		items = append(items, GenerationJobInput{
			Kind: model.GenerationKindImage, Endpoint: "/images/edits", Model: setting.ImageModel, ChannelID: channelID,
			Request: map[string]any{"prompt": full, "size": slot.Size, "quality": quality}, InputFileIDs: inputs,
			Prompt: full, SlotKind: slot.Kind, Stage: stage,
		})
	}
	if len(items) == 0 {
		return EcommerceSetView{}, safeMessageError{message: "请选择要生成的图片"}
	}
	jobs, err := buildGenerationJobs(ctx, user, batch, items)
	if err != nil {
		return EcommerceSetView{}, err
	}
	for i, index := range selected {
		if stage == "preview" {
			set.Slots[index].PreviewJobID = jobs[i].ID
		} else {
			set.Slots[index].FinalJobID = jobs[i].ID
		}
	}
	if err := commitEcommerceSet(batch, set, append(created, jobs...)); err != nil {
		return EcommerceSetView{}, err
	}
	return GetEcommerceSet(ctx, user, id)
}

// ListEcommerceSets is the "历史" panel: the user's recent sets, optionally one preset only.
func ListEcommerceSets(userID string, preset string) ([]EcommerceSetSummary, error) {
	batches, err := repository.ListUserGenerationBatches(userID, model.GenerationSourceEcommerceSet, 50)
	if err != nil {
		return nil, err
	}
	summaries := []EcommerceSetSummary{}
	for _, batch := range batches {
		var set EcommerceSet
		if json.Unmarshal([]byte(batch.ContextJSON), &set) != nil || preset != "" && set.Form.Preset != preset {
			continue
		}
		cover := ""
		if len(set.Form.ProductFileIDs) > 0 {
			cover = "/api/files/" + set.Form.ProductFileIDs[0] + "/content"
		}
		summaries = append(summaries, EcommerceSetSummary{ID: batch.ID, Title: batch.Title, Preset: set.Form.Preset, CoverURL: cover, CreatedAt: batch.CreatedAt})
	}
	return summaries, nil
}

// RunEcommerceTool queues a one-shot tool (图片复刻、去除背景、AI 换装) as image edit jobs.
func RunEcommerceTool(ctx context.Context, user model.AuthUser, tool string, input EcommerceToolInput) (model.GenerationBatch, []model.GenerationJob, error) {
	preset, ok := ecommerceTools[tool]
	if !ok {
		return model.GenerationBatch{}, nil, safeMessageError{message: "不支持的工具"}
	}
	if len(input.FileIDs) < preset.MinInputs || len(input.FileIDs) > preset.MaxInputs {
		return model.GenerationBatch{}, nil, safeMessageError{message: fmt.Sprintf("请上传 %d-%d 张图片", preset.MinInputs, preset.MaxInputs)}
	}
	params := map[string]string{}
	for key, value := range input.Params {
		if !slices.Contains(preset.Params, key) {
			return model.GenerationBatch{}, nil, safeMessageError{message: "参数无效：" + key}
		}
		if value = strings.TrimSpace(value); len([]rune(value)) > 500 {
			return model.GenerationBatch{}, nil, safeMessageError{message: "参数过长"}
		}
		params[key] = value
	}
	size := firstNonEmpty(params["size"], "1024x1024")
	if ecommerceSlotAspectText[size] == "" {
		return model.GenerationBatch{}, nil, safeMessageError{message: "图片尺寸无效"}
	}
	setting, err := ecommerceSetting()
	if err != nil {
		return model.GenerationBatch{}, nil, err
	}
	vars := map[string]string{"extra": ""}
	if params["extra"] != "" {
		vars["extra"] = "\n补充要求：" + params["extra"]
	}
	request := map[string]any{"size": size, "quality": setting.FinalQuality}
	template := ecommerceReplicatePrompt
	switch tool {
	case "remove-bg":
		template = ecommerceRemoveBackgroundPrompt
		switch params["background"] {
		case "", "transparent":
			vars["background"] = "透明背景（输出带透明通道的 PNG）"
			request["background"] = "transparent"
		case "white":
			vars["background"] = "纯白色 #FFFFFF 背景"
		case "color":
			if !hexColorPattern.MatchString(params["color"]) {
				return model.GenerationBatch{}, nil, safeMessageError{message: "背景色需为 #RRGGBB"}
			}
			vars["background"] = "纯色背景 " + params["color"]
		default:
			return model.GenerationBatch{}, nil, safeMessageError{message: "背景类型无效"}
		}
	case "tryon":
		switch params["mode"] {
		case "", "wear":
			template = ecommerceTryonWearPrompt
		case "scene":
			template = ecommerceTryonScenePrompt
		default:
			return model.GenerationBatch{}, nil, safeMessageError{message: "换装模式无效"}
		}
		vars["model"] = "根据商品选择合适的模特；"
		if params["description"] != "" {
			vars["model"] = "模特设定「" + params["description"] + "」；"
		}
		if params["hasModelImage"] == "true" {
			if len(input.FileIDs) < 2 {
				return model.GenerationBatch{}, nil, safeMessageError{message: "请同时上传商品图与模特图"}
			}
			vars["model"] = "使用最后一张图中的模特形象（保持其面部、发型与体型一致）；"
		}
	}
	prompt := renderPromptTemplate(template, vars)
	request["prompt"] = prompt
	return CreateGenerationBatch(ctx, user, GenerationBatchInput{Title: preset.Title, Source: model.GenerationSourceEcommerceTool, Items: []GenerationJobInput{{
		Kind: model.GenerationKindImage, Endpoint: "/images/edits", Model: setting.ImageModel, ChannelID: ecommerceChannelID(user.ID), Request: request,
		InputFileIDs: input.FileIDs, Prompt: prompt, Count: min(max(input.Count, 1), 4), SlotKind: tool,
	}}})
}

// advanceEcommerceSet applies AI jobs that have finished. It may build the next plan round; those
// jobs are returned unsaved so the caller persists them only after winning the version check.
func advanceEcommerceSet(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, set *EcommerceSet, jobs []model.GenerationJob) ([]model.GenerationJob, bool) {
	byID := make(map[string]model.GenerationJob, len(jobs))
	for _, job := range jobs {
		byID[job.ID] = job
	}
	changed := false
	var created []model.GenerationJob
	remaining := []EcommercePendingJob{}
	for _, pending := range set.Pending {
		job, found := byID[pending.JobID]
		if found && (job.Status == model.GenerationStatusQueued || job.Status == model.GenerationStatusRunning) {
			remaining = append(remaining, pending)
			continue
		}
		changed = true
		succeeded := found && job.Status == model.GenerationStatusSucceeded
		switch pending.Purpose {
		case ecommercePurposeAnalyze:
			if !succeeded {
				set.Notice = "卖点解析失败：" + firstNonEmpty(job.Error, "任务未完成")
				continue
			}
			analysis := parseAnalysis(job.OutputText)
			if analysis.DesignSpec == "" && analysis.SellingPoints == (EcommerceSellingPoints{}) {
				set.Notice = "卖点解析结果无法识别，请重试"
				continue
			}
			set.Analysis = &analysis
			set.Notice = ""
			resetEcommercePlan(set)
		case ecommercePurposeRewrite:
			if text := parseSellingPointText(job.OutputText); succeeded && text != "" && set.Analysis != nil {
				set.Analysis.SellingPoints.setKind(pending.Kind, text)
				resetEcommercePlan(set)
			}
		case ecommercePurposeOptions:
			if succeeded {
				set.SellingOptions[pending.Kind] = parseSellingOptions(job.OutputText)
			}
		case ecommercePurposePlan:
			if succeeded {
				for id, prompt := range parsePlanOutput(job.OutputText, pending.SlotIDs) {
					set.SlotPrompts[id] = prompt
				}
			}
			var missing []EcommerceSlot
			for _, slot := range ecommercePlanTargets(set.Slots) {
				if strings.TrimSpace(set.SlotPrompts[slot.ID]) == "" {
					missing = append(missing, slot)
				}
			}
			if len(missing) == 0 || set.PlanRound >= ecommerceMaxPlanRounds {
				set.Planned = true
				set.Notice = ""
				if len(missing) > 0 {
					set.Notice = fmt.Sprintf("%d 个槽位提示词规划失败，已用默认模板", len(missing))
				}
				continue
			}
			next, err := newEcommercePlanJob(ctx, user, batch, *set, missing)
			if err != nil {
				log.Printf("queue next plan round failed: batch=%s err=%v", batch.ID, err)
				set.Planned = true
				set.Notice = fmt.Sprintf("%d 个槽位提示词规划失败，已用默认模板", len(missing))
				continue
			}
			set.PlanRound++
			created = append(created, next)
			remaining = append(remaining, EcommercePendingJob{JobID: next.ID, Purpose: ecommercePurposePlan, SlotIDs: ecommerceSlotIDs(missing)})
		}
	}
	set.Pending = remaining
	return created, changed
}

// openEcommerceSet loads a set and applies finished AI jobs in memory so actions see the latest state.
func openEcommerceSet(ctx context.Context, user model.AuthUser, id string) (model.GenerationBatch, EcommerceSet, []model.GenerationJob, error) {
	batch, set, err := loadEcommerceSet(user.ID, id)
	if err != nil {
		return model.GenerationBatch{}, EcommerceSet{}, nil, err
	}
	jobs, err := repository.ListUserBatchGenerationJobs(user.ID, id)
	if err != nil {
		return model.GenerationBatch{}, EcommerceSet{}, nil, err
	}
	created, _ := advanceEcommerceSet(ctx, user, batch, &set, jobs)
	return batch, set, created, nil
}

func loadEcommerceSet(userID string, id string) (model.GenerationBatch, EcommerceSet, error) {
	batch, ok, err := repository.GetUserGenerationBatch(userID, id)
	if err != nil {
		return model.GenerationBatch{}, EcommerceSet{}, err
	}
	if !ok || batch.Source != model.GenerationSourceEcommerceSet {
		return model.GenerationBatch{}, EcommerceSet{}, safeMessageError{message: "套图不存在"}
	}
	var set EcommerceSet
	if err := json.Unmarshal([]byte(batch.ContextJSON), &set); err != nil {
		return model.GenerationBatch{}, EcommerceSet{}, err
	}
	if set.SlotPrompts == nil {
		set.SlotPrompts = map[string]string{}
	}
	if set.SellingOptions == nil {
		set.SellingOptions = map[string][]string{}
	}
	if set.Pending == nil {
		set.Pending = []EcommercePendingJob{}
	}
	return batch, set, nil
}

// commitEcommerceSet saves the state if no other request saved since it was read, then queues jobs.
func commitEcommerceSet(batch model.GenerationBatch, set EcommerceSet, jobs []model.GenerationJob) error {
	raw, err := json.Marshal(set)
	if err != nil {
		return err
	}
	ok, err := repository.UpdateGenerationBatchContext(batch.UserID, batch.ID, batch.Version, string(raw))
	if err != nil {
		return err
	}
	if !ok {
		return errEcommerceSetChanged
	}
	if err := repository.CreateGenerationJobs(jobs); err != nil {
		return err
	}
	if len(jobs) > 0 {
		WakeGenerationWorker()
	}
	return nil
}

func ecommerceSetView(batch model.GenerationBatch, set EcommerceSet, jobs []model.GenerationJob) EcommerceSetView {
	return EcommerceSetView{ID: batch.ID, Title: batch.Title, CreatedAt: batch.CreatedAt, EcommerceSet: set, Jobs: jobs}
}

func newEcommerceVisionJob(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, modelName string, instruction string, fileIDs []string, title string) (model.GenerationJob, error) {
	content := []any{map[string]any{"type": "input_text", "text": instruction}}
	for _, id := range fileIDs {
		content = append(content, map[string]any{"type": "input_image", "image_url": generationFileRefPrefix + id})
	}
	jobs, err := buildGenerationJobs(ctx, user, batch, []GenerationJobInput{{
		Kind: model.GenerationKindVision, Endpoint: "/responses", Model: modelName, ChannelID: ecommerceChannelID(user.ID),
		Request: map[string]any{"input": []any{map[string]any{"role": "user", "content": content}}}, InputFileIDs: fileIDs, Prompt: title,
	}})
	if err != nil {
		return model.GenerationJob{}, err
	}
	return jobs[0], nil
}

func newEcommercePlanJob(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, set EcommerceSet, slots []EcommerceSlot) (model.GenerationJob, error) {
	setting, err := ecommerceSetting()
	if err != nil {
		return model.GenerationJob{}, err
	}
	instruction := ecommerceTargetClause(set.Form) + buildPlanInstruction(slots, ecommercePlanOptions{
		SellingPoints:    set.Analysis.SellingPoints,
		NeedsModel:       set.Analysis.NeedsModel,
		ModelDescription: ecommerceModelDescription(set),
		QualityLabel:     qualityToLabel(setting.FinalQuality),
		DesignSpec:       ecommercePlanSpec(set),
		DedupNote:        buildDedupNote(set.SlotPrompts, slots),
	})
	inputs := set.Form.ProductFileIDs
	if set.Form.StyleMode == "reference" && set.Form.ReferenceFileID != "" {
		inputs = append(slices.Clone(inputs), set.Form.ReferenceFileID)
	}
	return newEcommerceVisionJob(ctx, user, batch, setting.VisionModel, instruction, inputs, "规划套图方案")
}

// ecommerceChannelID bills a new-api linked user's ecommerce jobs to their account group ("newapi@"
// resolves to it in SelectModelChannelForUser); everyone else keeps the admin channel pick ("").
func ecommerceChannelID(userID string) string {
	if !NewapiSSOEnabled() {
		return ""
	}
	if user, ok, err := repository.GetUserByID(userID); err != nil || !ok || user.NewapiID == "" {
		return ""
	}
	return newapiChannelPrefix
}

// ecommerceTargetClause is the R-SET-2 storefront line put in front of every AI instruction (not in 1688).
func ecommerceTargetClause(form EcommerceSetForm) string {
	return "目标市场：" + form.Market + "；目标平台：" + form.Platform + "。请让卖点表达、视觉风格与构图符合该平台和市场的电商审美与规范。\n"
}

// ecommerceDesignSpec is the shared prefix of every generation prompt: the analyzed spec plus the chosen style.
func ecommerceDesignSpec(set EcommerceSet) string {
	spec := ""
	if set.Analysis != nil {
		spec = strings.TrimSpace(set.Analysis.DesignSpec)
	}
	if style := strings.TrimSpace(set.Form.StyleText); style != "" && set.Form.StyleMode != "reference" {
		spec = strings.TrimSpace(spec + "\n设计风格要求：" + style)
	}
	return spec
}

// ecommercePlanSpec adds the reference-layout note; only the plan sees the reference image.
func ecommercePlanSpec(set EcommerceSet) string {
	spec := ecommerceDesignSpec(set)
	if set.Form.StyleMode == "reference" && set.Form.ReferenceFileID != "" {
		spec = strings.TrimSpace(spec + "\n" + ecommerceReferenceNote)
	}
	return spec
}

func ecommerceModelDescription(set EcommerceSet) string {
	if set.Form.ModelDescription != "" {
		return set.Form.ModelDescription
	}
	if set.Analysis != nil && set.Analysis.NeedsModel {
		return set.Analysis.ModelPrompt
	}
	return ""
}

func resetEcommercePlan(set *EcommerceSet) {
	set.SlotPrompts = map[string]string{}
	set.Planned = false
	set.PlanRound = 0
}

func ecommerceSlotIDs(slots []EcommerceSlot) []string {
	ids := make([]string, 0, len(slots))
	for _, slot := range slots {
		ids = append(ids, slot.ID)
	}
	return ids
}

// ecommerceLayoutKey changes when the slot layout must be rebuilt.
func ecommerceLayoutKey(form EcommerceSetForm) string {
	raw, _ := json.Marshal([]any{form.Structure, form.Skus, form.Language})
	return string(raw)
}

// ecommercePlanKey changes when anything the page plan was built from changes.
func ecommercePlanKey(set EcommerceSet) string {
	raw, _ := json.Marshal([]any{set.Form, set.Slots, set.Analysis})
	return string(raw)
}

func normalizeEcommerceForm(form *EcommerceSetForm, userID string, creating bool) error {
	if creating {
		if form.Preset != "detail" {
			form.Preset = "set"
		}
		if len(form.ProductFileIDs) < 1 || len(form.ProductFileIDs) > ecommerceMaxProducts {
			return safeMessageError{message: fmt.Sprintf("请上传 1-%d 张商品图", ecommerceMaxProducts)}
		}
		for _, id := range form.ProductFileIDs {
			if err := checkEcommerceImage(userID, id, true); err != nil {
				return err
			}
		}
	}
	form.Market, form.Platform = strings.TrimSpace(form.Market), strings.TrimSpace(form.Platform)
	if form.Market == "" || form.Platform == "" || len([]rune(form.Market)) > 20 || len([]rune(form.Platform)) > 20 {
		return safeMessageError{message: "请选择目标市场与平台"}
	}
	if _, ok := ecommerceTextLangLabels[form.Language]; !ok {
		return safeMessageError{message: "文案语言无效"}
	}
	form.StyleText, form.ModelDescription = strings.TrimSpace(form.StyleText), strings.TrimSpace(form.ModelDescription)
	if len([]rune(form.StyleText)) > 500 || len([]rune(form.ModelDescription)) > 200 {
		return safeMessageError{message: "风格要求或模特描述过长"}
	}
	switch form.StyleMode {
	case "", "ai":
		form.StyleMode, form.ReferenceFileID = "ai", ""
	case "reference":
		if form.ReferenceFileID == "" {
			return safeMessageError{message: "请上传参考排版图"}
		}
		if err := checkEcommerceImage(userID, form.ReferenceFileID, false); err != nil {
			return err
		}
	case "custom":
		if form.StyleText == "" {
			return safeMessageError{message: "请填写自定义风格要求"}
		}
		form.ReferenceFileID = ""
	default:
		return safeMessageError{message: "设计风格无效"}
	}
	if form.Structure.Mode != "custom" {
		form.Structure = EcommerceStructure{Mode: "smart"}
	}
	if len(form.Skus) > ecommerceMaxSkus {
		return safeMessageError{message: fmt.Sprintf("SKU 图最多 %d 张", ecommerceMaxSkus)}
	}
	for index := range form.Skus {
		sku := &form.Skus[index]
		sku.Label, sku.Desc = strings.TrimSpace(sku.Label), strings.TrimSpace(sku.Desc)
		if sku.Label == "" || len([]rune(sku.Label)) > 30 || len([]rune(sku.Desc)) > 200 {
			return safeMessageError{message: "SKU 名称必填（≤30 字），描述 ≤200 字"}
		}
		if sku.FileID != "" {
			if err := checkEcommerceImage(userID, sku.FileID, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkEcommerceImage requires the file to belong to the user; strict also enforces R-SET-1 (JPG/PNG ≤10MB).
func checkEcommerceImage(userID string, id string, strict bool) error {
	object, err := repository.GetStorageObject(id)
	// An empty userID skips the owner check: admin template examples use shared samples.
	if err != nil || (userID != "" && object.CreatedBy != userID) {
		return safeMessageError{message: "图片不存在"}
	}
	if strict && ((object.MimeType != "image/jpeg" && object.MimeType != "image/png") || object.Bytes > ecommerceMaxImageBytes) {
		return safeMessageError{message: "商品图需为 JPG/PNG 且不超过 10MB"}
	}
	if !strict && object.MimeType != "image/jpeg" && object.MimeType != "image/png" && object.MimeType != "image/webp" {
		return safeMessageError{message: "图片需为 JPG/PNG/WebP"}
	}
	return nil
}
