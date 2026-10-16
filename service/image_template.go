package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	templateMaxInputGroups  = 4
	templateMaxInputFiles   = 6
	templateMaxPromptRunes  = 2000
	templateMaxSystemPrompt = 8000
	templateMaxExamples     = 20
	templateMaxFeeCredits   = 10000
	templateFeeSettleBatch  = 20
	// templateFeeMaxUSD is new-api's cap on one /api/sso/consume charge (10 × quotaPerUnit).
	templateFeeMaxUSD = 10
)

var (
	templateCategories = []string{"ecommerce", "creative"}
	// videoTemplateCategories: 带货开箱 / 同城到店 / 上门服务 / 节日促销.
	videoTemplateCategories = []string{"video-unboxing", "video-local", "video-service", "video-promo"}
	templateRatios          = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "9:16", "16:9"}
	templateSlugPattern     = regexp.MustCompile(`^[a-z0-9-]{2,64}$`)
	templateKeyPattern      = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	templateImageRef        = regexp.MustCompile(`@图片(\d+)`)
	templateFileURL         = regexp.MustCompile(`^/api/files/([A-Za-z0-9-]+)/content$`)
	templateFeeSettling     atomic.Bool
	// templateFeeRetryDelays spaces charge retries after each failed call (about a day in all); then the fee is failed.
	templateFeeRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour}
	errTemplateNotFound    = safeMessageError{message: "模板不存在"}
	templateResolutionPx   = map[string]float64{"1K": 1024, "2K": 2048, "4K": 2880}
	// templateResolutionQuality mirrors the web image page: 1K/2K/4K are the low/medium/high quality tiers.
	templateResolutionQuality = map[string]string{"1K": "low", "2K": "medium", "4K": "high"}
)

// TemplateGenerateInput is a template run: Files maps each input key to its storage file IDs.
type TemplateGenerateInput struct {
	Files      map[string][]string `json:"files"`
	Prompt     string              `json:"prompt"`
	Ratio      string              `json:"ratio"`
	Resolution string              `json:"resolution"`
	Count      int                 `json:"count"`
}

// ListPublishedImageTemplates is the public library of one kind (image when empty); the system prompt never leaves the server.
func ListPublishedImageTemplates(kind string, category string) ([]model.ImageTemplate, error) {
	items, err := repository.ListImageTemplates(model.ImageTemplateStatusPublished, firstNonEmpty(kind, model.ImageTemplateKindImage), category)
	if err != nil {
		return nil, err
	}
	imageModel := templateDefaultModel()
	for i := range items {
		items[i] = publicImageTemplate(items[i], imageModel)
	}
	return items, nil
}

func GetPublishedImageTemplate(slug string) (model.ImageTemplate, error) {
	item, err := publishedImageTemplate(slug)
	if err != nil {
		return model.ImageTemplate{}, err
	}
	return publicImageTemplate(item, templateDefaultModel()), nil
}

func publishedImageTemplate(slug string) (model.ImageTemplate, error) {
	item, ok, err := repository.GetImageTemplateBySlug(strings.TrimSpace(slug))
	if err != nil {
		return model.ImageTemplate{}, err
	}
	if !ok || item.Status != model.ImageTemplateStatusPublished {
		return model.ImageTemplate{}, errTemplateNotFound
	}
	return item, nil
}

// publicImageTemplate hides the prompt and shows the model the run will use, for price estimates.
func publicImageTemplate(item model.ImageTemplate, imageModel string) model.ImageTemplate {
	item.Prompt = ""
	item.Model = firstNonEmpty(item.Model, imageModel)
	return item
}

func templateDefaultModel() string {
	settings, err := PublicSettings()
	if err != nil {
		log.Printf("load settings for templates failed err=%v", err)
	}
	return settings.Ecommerce.ImageModel
}

func AdminListImageTemplates() ([]model.ImageTemplate, error) {
	return repository.ListImageTemplates("", "", "")
}

// SaveImageTemplate creates (empty id) or replaces a template after validation.
func SaveImageTemplate(id string, item model.ImageTemplate) (model.ImageTemplate, error) {
	if err := normalizeImageTemplate(&item); err != nil {
		return model.ImageTemplate{}, err
	}
	current := time.Now().UTC()
	item.ID, item.CreatedAt, item.UpdatedAt = newID("imgtpl"), current, current
	var existing model.ImageTemplate
	if id != "" {
		var ok bool
		var err error
		existing, ok, err = repository.GetImageTemplate(id)
		if err != nil {
			return model.ImageTemplate{}, err
		}
		if !ok {
			return model.ImageTemplate{}, errTemplateNotFound
		}
		item.ID, item.CreatedAt = existing.ID, existing.CreatedAt
	}
	if other, ok, err := repository.GetImageTemplateBySlug(item.Slug); err != nil {
		return model.ImageTemplate{}, err
	} else if ok && other.ID != item.ID {
		return model.ImageTemplate{}, safeMessageError{message: "标识已被其他模板使用"}
	}
	if err := repository.SaveImageTemplate(item); err != nil {
		return model.ImageTemplate{}, err
	}
	// Works used as cover or examples must outlive the works retention, or the template images vanish,
	// and are public so the template page needs no signed URLs.
	if err := repository.KeepGenerationOutputs(templateFileIDs(item), time.Now().UTC().AddDate(100, 0, 0)); err != nil {
		return item, err
	}
	if err := repository.SetStorageObjectsPublic(templateFileIDs(item), true); err != nil {
		return item, err
	}
	return item, releaseTemplateFiles(templateFileIDs(existing))
}

// releaseTemplateFiles undoes SaveImageTemplate for the ids no template or setting uses anymore:
// their works expire again at creation + retention, and the files stop being public.
func releaseTemplateFiles(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	referenced, err := referencedFileIDs()
	if err != nil {
		return err
	}
	ids = slices.DeleteFunc(ids, func(id string) bool { return slices.Contains(referenced, id) })
	if len(ids) == 0 {
		return nil
	}
	jobs, err := repository.ListGenerationOutputJobs(ids)
	if err != nil {
		return err
	}
	works := worksSetting()
	for _, job := range jobs {
		days := works.RetentionDays
		if job.Member {
			days = works.MemberRetentionDays
		}
		if err := repository.SetGenerationJobExpiry(job.ID, job.CreatedAt.AddDate(0, 0, days)); err != nil {
			return err
		}
	}
	return repository.SetStorageObjectsPublic(ids, false)
}

// templateFileIDs lists the stored files a template's cover and examples point at.
func templateFileIDs(item model.ImageTemplate) []string {
	links := []string{item.CoverURL}
	for _, example := range item.Examples {
		links = append(append(links, example.Output), example.Inputs...)
	}
	ids := []string{}
	for _, link := range links {
		if match := templateFileURL.FindStringSubmatch(link); match != nil {
			ids = append(ids, match[1])
		}
	}
	return ids
}

// RequireTemplatePublish refuses a save or delete that publishes, unpublishes or changes a published template
// unless the user in ctx holds canvas.template.publish. status is the status being saved ("" for a delete).
func RequireTemplatePublish(ctx context.Context, id string, status string) error {
	user, _ := UserFromContext(ctx)
	if HasPermission(user, model.PermTemplatePublish) {
		return nil
	}
	published := status == model.ImageTemplateStatusPublished
	if !published && id != "" {
		existing, ok, err := repository.GetImageTemplate(id)
		if err != nil {
			return err
		}
		published = ok && existing.Status == model.ImageTemplateStatusPublished
	}
	if published {
		return safeMessageError{message: "需要模板发布权限"}
	}
	return nil
}

func DeleteImageTemplate(id string) error {
	item, _, err := repository.GetImageTemplate(id)
	if err != nil {
		return err
	}
	if err := repository.DeleteImageTemplate(id); err != nil {
		return err
	}
	return releaseTemplateFiles(templateFileIDs(item))
}

func normalizeImageTemplate(item *model.ImageTemplate) error {
	item.Slug = strings.TrimSpace(item.Slug)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.CoverURL = unsignedFileURL(item.CoverURL)
	item.Prompt = strings.TrimSpace(item.Prompt)
	item.PromptPlaceholder = strings.TrimSpace(item.PromptPlaceholder)
	item.Model = strings.TrimSpace(item.Model)
	item.Credit = strings.TrimSpace(item.Credit)
	item.Kind = firstNonEmpty(strings.TrimSpace(item.Kind), model.ImageTemplateKindImage)
	if !templateSlugPattern.MatchString(item.Slug) {
		return safeMessageError{message: "标识只能使用 2-64 位小写字母、数字和 -"}
	}
	if item.Title == "" || runeLen(item.Title) > 100 {
		return safeMessageError{message: "标题需为 1-100 字"}
	}
	if runeLen(item.Description) > 300 || runeLen(item.PromptPlaceholder) > 300 || runeLen(item.Credit) > 300 {
		return safeMessageError{message: "说明、提示词占位和来源署名不能超过 300 字"}
	}
	if len(item.CoverURL) > 2048 || len(item.Model) > 128 {
		return safeMessageError{message: "封面地址或模型名过长"}
	}
	if !templateImageURL(item.CoverURL) {
		return safeMessageError{message: "封面地址需为 http(s) 地址或 /api/files/ 路径"}
	}
	if item.Status != model.ImageTemplateStatusPublished {
		item.Status = model.ImageTemplateStatusDraft
	}
	switch item.Kind {
	case model.ImageTemplateKindVideo:
		return normalizeVideoTemplate(item)
	case model.ImageTemplateKindImage:
		item.VideoDefaults = nil
	default:
		return safeMessageError{message: "模板类型无效"}
	}
	if !slices.Contains(templateCategories, item.Category) {
		return safeMessageError{message: "分类无效"}
	}
	if item.Prompt == "" || runeLen(item.Prompt) > templateMaxSystemPrompt {
		return safeMessageError{message: fmt.Sprintf("提示词需为 1-%d 字", templateMaxSystemPrompt)}
	}
	if len(item.Inputs) > templateMaxInputGroups {
		return safeMessageError{message: fmt.Sprintf("最多 %d 组输入图片", templateMaxInputGroups)}
	}
	total, keys := 0, map[string]bool{}
	for i := range item.Inputs {
		input := &item.Inputs[i]
		input.Key, input.Label = strings.TrimSpace(input.Key), strings.TrimSpace(input.Label)
		if !templateKeyPattern.MatchString(input.Key) || keys[input.Key] {
			return safeMessageError{message: "输入标识需唯一，且只能使用小写字母、数字、_ 和 -"}
		}
		if input.Label == "" || runeLen(input.Label) > 20 {
			return safeMessageError{message: "输入名称需为 1-20 字"}
		}
		if input.Max < 1 || input.Max > 4 {
			return safeMessageError{message: "每组输入图片数量需为 1-4 张"}
		}
		keys[input.Key] = true
		total += input.Max
	}
	if total > templateMaxInputFiles {
		return safeMessageError{message: fmt.Sprintf("输入图片合计最多 %d 张", templateMaxInputFiles)}
	}
	for _, match := range templateImageRef.FindAllStringSubmatch(item.Prompt, -1) {
		if n, _ := strconv.Atoi(match[1]); n < 1 || n > total {
			return safeMessageError{message: "提示词中的 " + match[0] + " 超出输入图片数量"}
		}
	}
	var err error
	if item.Ratios, err = templateOptions(item.Ratios, templateRatios, "比例"); err != nil {
		return err
	}
	if item.Resolutions, err = templateOptions(item.Resolutions, []string{"1K", "2K", "4K"}, "分辨率"); err != nil {
		return err
	}
	if math.IsNaN(item.FeeCredits) || item.FeeCredits < 0 || item.FeeCredits > templateMaxFeeCredits {
		return safeMessageError{message: fmt.Sprintf("模板费需为 0-%d", templateMaxFeeCredits)}
	}
	item.FeeCredits = math.Round(item.FeeCredits*100) / 100
	if billing, err := publicBillingSetting(); err != nil {
		return err
	} else if item.FeeCredits/billing.CreditsPerUnit > templateFeeMaxUSD {
		return safeMessageError{message: fmt.Sprintf("模板费每张不能超过 %g 积分", templateFeeMaxUSD*billing.CreditsPerUnit)}
	}
	if len(item.Examples) > templateMaxExamples {
		return safeMessageError{message: fmt.Sprintf("示例最多 %d 个", templateMaxExamples)}
	}
	for i := range item.Examples {
		example := &item.Examples[i]
		example.Output = unsignedFileURL(example.Output)
		for j := range example.Inputs {
			example.Inputs[j] = unsignedFileURL(example.Inputs[j])
		}
		if len(example.Inputs) > templateMaxInputFiles || len(example.Output) > 2048 || runeLen(example.Prompt) > templateMaxPromptRunes {
			return safeMessageError{message: "示例内容过长"}
		}
		for _, link := range append([]string{example.Output}, example.Inputs...) {
			if !templateImageURL(link) {
				return safeMessageError{message: "示例图片需为 http(s) 地址或 /api/files/ 路径"}
			}
			if len(link) > 2048 {
				return safeMessageError{message: "示例内容过长"}
			}
		}
	}
	if item.Inputs == nil {
		item.Inputs = []model.ImageTemplateInput{}
	}
	if item.Examples == nil {
		item.Examples = []model.ImageTemplateExample{}
	}
	return nil
}

// normalizeVideoTemplate checks the wizard defaults against the product video limits and clears
// the image-only fields (a video template is never run as an image app).
func normalizeVideoTemplate(item *model.ImageTemplate) error {
	if !slices.Contains(videoTemplateCategories, item.Category) {
		return safeMessageError{message: "分类无效"}
	}
	video := item.VideoDefaults
	if video == nil {
		video = &model.VideoTemplateDefaults{}
	}
	video.Ratio, video.Style = firstNonEmpty(strings.TrimSpace(video.Ratio), "9:16"), firstNonEmpty(strings.TrimSpace(video.Style), "scene")
	video.Name, video.Intro, video.SellingPoints = strings.TrimSpace(video.Name), strings.TrimSpace(video.Intro), strings.TrimSpace(video.SellingPoints)
	if video.Duration == 0 {
		video.Duration = 30
	}
	if !slices.Contains(productVideoDurations, video.Duration) {
		return safeMessageError{message: "视频时长需为 15 / 30 / 45 / 60 秒"}
	}
	if _, ok := productVideoRatios[video.Ratio]; !ok {
		return safeMessageError{message: "视频比例无效"}
	}
	if _, ok := productVideoStyles[video.Style]; !ok || video.Style == "custom" {
		return safeMessageError{message: "视频风格无效"}
	}
	if runeLen(video.Name) > 50 || runeLen(video.Intro) > 1000 || runeLen(video.SellingPoints) > 1000 {
		return safeMessageError{message: "示例产品名称 ≤50 字，介绍与卖点各 ≤1000 字"}
	}
	if len(video.Shots) > productVideoMaxShots {
		return safeMessageError{message: fmt.Sprintf("分镜最多 %d 个", productVideoMaxShots)}
	}
	total := 0
	for i := range video.Shots {
		shot := &video.Shots[i]
		shot.Scene, shot.Camera, shot.Voiceover, shot.Subtitle = strings.TrimSpace(shot.Scene), strings.TrimSpace(shot.Camera), strings.TrimSpace(shot.Voiceover), strings.TrimSpace(shot.Subtitle)
		// ponytail: 15 s is the admin's maxShotSeconds ceiling; the wizard re-checks against the live setting on save.
		if shot.Seconds < productVideoMinShotSecs || shot.Seconds > 15 || shot.Scene == "" || runeLen(shot.Scene) > 500 || runeLen(shot.Camera) > 100 || runeLen(shot.Voiceover) > 300 || runeLen(shot.Subtitle) > 50 {
			return safeMessageError{message: fmt.Sprintf("第 %d 镜无效：时长 %d-15 秒，画面描述必填（≤500 字），镜头 ≤100 字，旁白 ≤300 字，字幕 ≤50 字", i+1, productVideoMinShotSecs)}
		}
		total += shot.Seconds
	}
	if total > video.Duration {
		return safeMessageError{message: fmt.Sprintf("分镜总时长 %d 秒超过视频时长 %d 秒", total, video.Duration)}
	}
	if video.Shots == nil {
		video.Shots = []model.VideoTemplateShot{}
	}
	item.VideoDefaults = video
	item.Prompt, item.PromptPlaceholder, item.PromptRequired, item.Model = "", "", false, ""
	item.Inputs, item.Ratios, item.Resolutions, item.Examples = []model.ImageTemplateInput{}, []string{}, []string{}, []model.ImageTemplateExample{}
	item.FeeCredits, item.MemberFree = 0, false
	return nil
}

// templateOptions requires at least one value, each from allowed, and drops duplicates.
func templateOptions(values []string, allowed []string, name string) ([]string, error) {
	out := []string{}
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return nil, safeMessageError{message: name + "无效：" + value}
		}
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, safeMessageError{message: "请至少选择一个" + name}
	}
	return out, nil
}

// templateImageURL allows an empty value, an absolute http(s) URL or a stored file path.
func templateImageURL(value string) bool {
	if value == "" || strings.HasPrefix(value, "/api/files/") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func runeLen(value string) int {
	return len([]rune(value))
}

// GenerateWithImageTemplate queues count image jobs (source "template") for a published template.
func GenerateWithImageTemplate(ctx context.Context, user model.AuthUser, slug string, input TemplateGenerateInput) (model.GenerationBatch, []model.GenerationJob, error) {
	item, err := publishedImageTemplate(slug)
	if err != nil {
		return model.GenerationBatch{}, nil, err
	}
	return generateWithTemplate(ctx, user, item, input, false)
}

// AdminGenerateTemplateExamples queues an admin's example run (source "template-example") for any template,
// drafts included. Inputs may be any admin's files (shared samples) and the template fee is never charged.
func AdminGenerateTemplateExamples(ctx context.Context, user model.AuthUser, id string, input TemplateGenerateInput) (model.GenerationBatch, []model.GenerationJob, error) {
	if !HasPermission(user, model.PermTemplateWrite) {
		return model.GenerationBatch{}, nil, safeMessageError{message: "未登录或权限不足"}
	}
	item, ok, err := repository.GetImageTemplate(id)
	if err != nil {
		return model.GenerationBatch{}, nil, err
	}
	if !ok {
		return model.GenerationBatch{}, nil, errTemplateNotFound
	}
	return generateWithTemplate(ctx, user, item, input, true)
}

// AdminListTemplateExampleJobs lists the latest 100 example runs, of one template when templateID is set.
func AdminListTemplateExampleJobs(templateID string) ([]model.GenerationJob, error) {
	return repository.ListTemplateExampleJobs(strings.TrimSpace(templateID), 100)
}

func generateWithTemplate(ctx context.Context, user model.AuthUser, item model.ImageTemplate, input TemplateGenerateInput, example bool) (model.GenerationBatch, []model.GenerationJob, error) {
	if item.Kind == model.ImageTemplateKindVideo {
		return model.GenerationBatch{}, nil, safeMessageError{message: "视频模板请在带货视频中使用"}
	}
	owner := user.ID
	if example {
		owner = ""
	}
	for key := range input.Files {
		if !slices.ContainsFunc(item.Inputs, func(i model.ImageTemplateInput) bool { return i.Key == key }) {
			return model.GenerationBatch{}, nil, safeMessageError{message: "未知的输入：" + key}
		}
	}
	var fileIDs []string
	var legend []string
	for _, group := range item.Inputs {
		ids := input.Files[group.Key]
		if len(ids) > group.Max {
			return model.GenerationBatch{}, nil, safeMessageError{message: fmt.Sprintf("「%s」最多 %d 张", group.Label, group.Max)}
		}
		if group.Required && len(ids) == 0 {
			return model.GenerationBatch{}, nil, safeMessageError{message: "请上传「" + group.Label + "」"}
		}
		for _, id := range ids {
			if err := checkEcommerceImage(owner, id, false); err != nil {
				return model.GenerationBatch{}, nil, err
			}
		}
		if len(ids) > 0 {
			refs := make([]string, len(ids))
			for i := range ids {
				refs[i] = fmt.Sprintf("@图片%d", len(fileIDs)+i+1)
			}
			legend = append(legend, strings.Join(refs, "、")+" 为「"+group.Label+"」")
			fileIDs = append(fileIDs, ids...)
		}
	}
	userPrompt := strings.TrimSpace(input.Prompt)
	if runeLen(userPrompt) > templateMaxPromptRunes {
		return model.GenerationBatch{}, nil, safeMessageError{message: fmt.Sprintf("提示词不能超过 %d 字", templateMaxPromptRunes)}
	}
	if item.PromptRequired && userPrompt == "" {
		return model.GenerationBatch{}, nil, safeMessageError{message: "请填写提示词"}
	}
	ratio, resolution := firstNonEmpty(input.Ratio, item.Ratios[0]), firstNonEmpty(input.Resolution, item.Resolutions[0])
	if !slices.Contains(item.Ratios, ratio) || !slices.Contains(item.Resolutions, resolution) {
		return model.GenerationBatch{}, nil, safeMessageError{message: "该模板不支持所选比例或分辨率"}
	}
	count := max(input.Count, 1)
	if count > 4 {
		return model.GenerationBatch{}, nil, safeMessageError{message: "单次最多生成 4 张"}
	}
	imageModel := firstNonEmpty(item.Model, templateDefaultModel())
	if imageModel == "" {
		return model.GenerationBatch{}, nil, safeMessageError{message: "管理员尚未配置模板生图模型"}
	}
	feeStatus, feeQuota, source := model.TemplateFeeWaived, int64(0), model.GenerationSourceTemplateExample
	if !example {
		source = model.GenerationSourceTemplate
		var err error
		if feeStatus, feeQuota, err = templateFee(user, item, imageModel, count); err != nil {
			return model.GenerationBatch{}, nil, err
		}
	}
	prompt := buildTemplatePrompt(item.Prompt, userPrompt, legend)
	endpoint := "/images/generations"
	if len(fileIDs) > 0 {
		endpoint = "/images/edits"
	}
	return CreateGenerationBatch(ctx, user, GenerationBatchInput{Title: item.Title, Source: source, Items: []GenerationJobInput{{
		Kind: model.GenerationKindImage, Endpoint: endpoint, Model: imageModel, ChannelID: ecommerceChannelID(user.ID),
		Request:      map[string]any{"prompt": prompt, "size": templateImageSize(ratio, resolution), "quality": templateResolutionQuality[resolution]},
		InputFileIDs: fileIDs, Prompt: userPrompt, Count: count, TemplateID: item.ID, FeeStatus: feeStatus, FeeQuota: feeQuota, SharedInputs: example,
	}}})
}

// buildTemplatePrompt fills {{prompt}} (or appends the user's text) and explains which upload each @图片N is.
func buildTemplatePrompt(system string, userPrompt string, legend []string) string {
	prompt := system
	if strings.Contains(prompt, "{{prompt}}") {
		prompt = strings.ReplaceAll(prompt, "{{prompt}}", userPrompt)
	} else if userPrompt != "" {
		prompt += "\n补充要求：" + userPrompt
	}
	if len(legend) > 0 {
		prompt = "图片编号按上传顺序：" + strings.Join(legend, "；") + "。\n" + prompt
	}
	return prompt
}

// templateImageSize turns a ratio and 1K/2K/4K into pixels the way the web image page does
// (the long-standing size math in web/src/services/api/image.ts resolveSize).
func templateImageSize(ratio string, resolution string) string {
	w, h, _ := strings.Cut(ratio, ":")
	width, _ := strconv.Atoi(w)
	height, _ := strconv.Atoi(h)
	base := templateResolutionPx[resolution]
	unit := int(math.Round(math.Sqrt(base*base/float64(width*height))/16) * 16)
	return fmt.Sprintf("%dx%d", width*unit, height*unit)
}

// templateFee decides the per-image template fee at submit time: waived for non-SSO users, free templates
// and members of member-free templates; otherwise pending in new-api quota. SSO users must hold enough
// credits for the model fee estimate plus the template fee of every image.
func templateFee(user model.AuthUser, item model.ImageTemplate, imageModel string, count int) (string, int64, error) {
	if !NewapiSSOEnabled() {
		return model.TemplateFeeWaived, 0, nil
	}
	if stored, ok, err := repository.GetUserByID(user.ID); err != nil {
		return "", 0, err
	} else if !ok || stored.NewapiID == "" {
		return model.TemplateFeeWaived, 0, nil
	}
	summary, err := AccountSummaryForUser(user.ID)
	if err != nil {
		return "", 0, err
	}
	feeCredits := max(item.FeeCredits, 0)
	if item.MemberFree && summary.Member.Active {
		feeCredits = 0
	}
	status, quota := model.TemplateFeeWaived, int64(0)
	if feeCredits > 0 {
		billing, err := publicBillingSetting()
		if err != nil {
			return "", 0, err
		}
		perUnit, err := newapiQuotaPerUnit()
		if err != nil {
			return "", 0, err
		}
		if quota, err = feeQuota(feeCredits, billing.CreditsPerUnit, perUnit, templateFeeMaxUSD); err != nil {
			return "", 0, err
		}
		status = model.TemplateFeePending
	}
	modelCredits, err := templateModelCredits(user.ID, imageModel)
	if err != nil {
		return "", 0, err
	}
	if need := math.Round((modelCredits+feeCredits)*float64(count)*100) / 100; summary.Credits < need {
		return "", 0, safeMessageError{message: fmt.Sprintf("余额不足：预计需 %g %s", need, summary.CreditName)}
	}
	return status, quota, nil
}

// templateModelCredits estimates one image's model fee in credits: the catalog's per-call price times the
// account group ratio. Token-priced or unknown models count as 0.
func templateModelCredits(userID string, imageModel string) (float64, error) {
	catalog, err := ModelCatalogForUser(userID)
	if err != nil {
		return 0, err
	}
	m := slices.IndexFunc(catalog.Models, func(item ModelCatalogModel) bool { return item.Name == imageModel })
	g := slices.IndexFunc(catalog.Groups, func(item ModelCatalogGroup) bool { return item.Name == catalog.UserGroup })
	if m < 0 || g < 0 || catalog.Models[m].QuotaType != 1 {
		return 0, nil
	}
	return catalog.Models[m].ModelPrice * catalog.Groups[g].Ratio * catalog.CreditsPerUnit, nil
}

// feeQuota converts credits to new-api quota, rounded up (ignoring float noise) and at least 1;
// more than maxUSD worth, or a non-finite result, is an error.
func feeQuota(credits float64, creditsPerUnit float64, quotaPerUnit float64, maxUSD float64) (int64, error) {
	quota := max(math.Ceil(math.Round(credits/creditsPerUnit*quotaPerUnit*1e6)/1e6), 1)
	if math.IsNaN(quota) || math.IsInf(quota, 0) || quota >= math.MaxInt64 || quota > math.Floor(maxUSD*quotaPerUnit) {
		return 0, safeMessageError{message: "费用超出单次收费上限，请联系管理员"}
	}
	return int64(quota), nil
}

// settleTemplateFeesAsync charges pending template fees in the background so the worker tick never waits on new-api.
func settleTemplateFeesAsync() {
	if !templateFeeSettling.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer templateFeeSettling.Store(false)
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("template fee settle panic: %v", recovered)
			}
		}()
		settleTemplateFees()
	}()
}

// settleTemplateFees charges due succeeded jobs once each (idempotency key = job ID). A failed call is retried
// on templateFeeRetryDelays; HTTP 400 (invalid or over new-api's cap) fails the fee at once. The job stays delivered.
func settleTemplateFees() {
	now := generationNow()
	jobs, err := repository.ListPendingTemplateFeeJobs(now, templateFeeSettleBatch)
	if err != nil {
		log.Printf("list pending template fees failed err=%v", err)
		return
	}
	for _, job := range jobs {
		values := map[string]any{"fee_status": model.TemplateFeeCharged, "fee_next_at": nil, "fee_error": ""}
		if permanent, err := consumeTemplateFee(job); err != nil {
			attempts := job.FeeAttempts + 1
			log.Printf("template fee charge failed job=%s user=%s attempt=%d err=%v", job.ID, job.UserID, attempts, err)
			values = map[string]any{"fee_attempts": attempts, "fee_error": truncateGenerationError(err.Error()), "fee_next_at": nil}
			if permanent || attempts > len(templateFeeRetryDelays) {
				values["fee_status"] = model.TemplateFeeFailed
			} else {
				values["fee_next_at"] = now.Add(templateFeeRetryDelays[attempts-1])
			}
		}
		if err := repository.UpdatePendingTemplateFee(job.ID, values); err != nil {
			log.Printf("record template fee failed job=%s err=%v", job.ID, err)
		}
	}
}

// consumeTemplateFee charges one job's fee; permanent is true only when new-api answered 400.
// Network errors, 5xx, 429, 401/403 (token needs re-login) and 402 (insufficient quota) are retried.
func consumeTemplateFee(job model.GenerationJob) (bool, error) {
	user, err := newapiLinkedUser(job.UserID)
	if err != nil {
		return false, err
	}
	title := "模板生成"
	if item, ok, _ := repository.GetImageTemplate(job.TemplateID); ok {
		title = item.Title
	}
	status, err := newapiConsume(user, job.ID, job.FeeQuota, title)
	return status == http.StatusBadRequest, err
}

// newapiConsume charges quota to the user's new-api wallet once per idempotency key (a replay charges
// nothing). It returns new-api's HTTP status (0 when no answer came) and an error unless the charge succeeded.
func newapiConsume(user model.User, key string, quota int64, title string) (int, error) {
	body, err := json.Marshal(map[string]any{
		"client_id": config.Cfg.NewapiClientID, "client_secret": config.Cfg.NewapiClientSecret,
		"idempotency_key": key, "quota": quota, "title": title,
	})
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/")+"/api/sso/consume", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+user.NewapiAccessToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := newapiHTTPClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	var envelope newapiEnvelope
	_ = json.NewDecoder(io.LimitReader(response.Body, defaultMaxBytes)).Decode(&envelope)
	if response.StatusCode == http.StatusOK && envelope.Success {
		summaryCache.delete(user.ID)
		return response.StatusCode, nil
	}
	return response.StatusCode, fmt.Errorf("new-api consume HTTP %d: %s", response.StatusCode, envelope.Message)
}

// TemplateFeeJob is one template fee in the admin fee list.
type TemplateFeeJob struct {
	JobID       string     `json:"jobId"`
	UserID      string     `json:"userId"`
	TemplateID  string     `json:"templateId"`
	FeeQuota    int64      `json:"feeQuota"`
	FeeAttempts int        `json:"feeAttempts"`
	FeeError    string     `json:"feeError"`
	FinishedAt  *time.Time `json:"finishedAt"`
}

// AdminListTemplateFees lists the latest 100 template fees with a status (failed by default).
func AdminListTemplateFees(status string) ([]TemplateFeeJob, error) {
	status = firstNonEmpty(status, model.TemplateFeeFailed)
	if status != model.TemplateFeeFailed && status != model.TemplateFeePending {
		return nil, safeMessageError{message: "状态只能是 failed 或 pending"}
	}
	jobs, err := repository.ListTemplateFeeJobs(status, 100)
	if err != nil {
		return nil, err
	}
	items := make([]TemplateFeeJob, len(jobs))
	for i, job := range jobs {
		items[i] = TemplateFeeJob{JobID: job.ID, UserID: job.UserID, TemplateID: job.TemplateID, FeeQuota: job.FeeQuota, FeeAttempts: job.FeeAttempts, FeeError: job.FeeError, FinishedAt: job.FinishedAt}
	}
	return items, nil
}
