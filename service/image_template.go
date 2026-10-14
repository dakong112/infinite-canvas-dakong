package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
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
	templateFeeMaxAttempts  = 3
	templateFeeSettleBatch  = 20
)

var (
	templateCategories   = []string{"ecommerce", "creative"}
	templateRatios       = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "9:16", "16:9"}
	templateSlugPattern  = regexp.MustCompile(`^[a-z0-9-]{2,64}$`)
	templateKeyPattern   = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	templateImageRef     = regexp.MustCompile(`@图片(\d+)`)
	templateFeeSettling  atomic.Bool
	errTemplateNotFound  = safeMessageError{message: "模板不存在"}
	templateResolutionPx = map[string]float64{"1K": 1024, "2K": 2048, "4K": 2880}
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

// ListPublishedImageTemplates is the public library; the system prompt never leaves the server.
func ListPublishedImageTemplates(category string) ([]model.ImageTemplate, error) {
	items, err := repository.ListImageTemplates(model.ImageTemplateStatusPublished, category)
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
	return repository.ListImageTemplates("", "")
}

// SaveImageTemplate creates (empty id) or replaces a template after validation.
func SaveImageTemplate(id string, item model.ImageTemplate) (model.ImageTemplate, error) {
	if err := normalizeImageTemplate(&item); err != nil {
		return model.ImageTemplate{}, err
	}
	current := time.Now().UTC()
	item.ID, item.CreatedAt, item.UpdatedAt = newID("imgtpl"), current, current
	if id != "" {
		existing, ok, err := repository.GetImageTemplate(id)
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
	return item, repository.SaveImageTemplate(item)
}

func DeleteImageTemplate(id string) error {
	return repository.DeleteImageTemplate(id)
}

func normalizeImageTemplate(item *model.ImageTemplate) error {
	item.Slug = strings.TrimSpace(item.Slug)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.CoverURL = strings.TrimSpace(item.CoverURL)
	item.Prompt = strings.TrimSpace(item.Prompt)
	item.PromptPlaceholder = strings.TrimSpace(item.PromptPlaceholder)
	item.Model = strings.TrimSpace(item.Model)
	if !templateSlugPattern.MatchString(item.Slug) {
		return safeMessageError{message: "标识只能使用 2-64 位小写字母、数字和 -"}
	}
	if item.Title == "" || runeLen(item.Title) > 100 {
		return safeMessageError{message: "标题需为 1-100 字"}
	}
	if runeLen(item.Description) > 300 || runeLen(item.PromptPlaceholder) > 300 {
		return safeMessageError{message: "说明和提示词占位不能超过 300 字"}
	}
	if !slices.Contains(templateCategories, item.Category) {
		return safeMessageError{message: "分类无效"}
	}
	if len(item.CoverURL) > 2048 || len(item.Model) > 128 {
		return safeMessageError{message: "封面地址或模型名过长"}
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
	if len(item.Examples) > templateMaxExamples {
		return safeMessageError{message: fmt.Sprintf("示例最多 %d 个", templateMaxExamples)}
	}
	for _, example := range item.Examples {
		if len(example.Inputs) > templateMaxInputFiles || len(example.Output) > 2048 || runeLen(example.Prompt) > templateMaxPromptRunes {
			return safeMessageError{message: "示例内容过长"}
		}
		for _, url := range example.Inputs {
			if len(url) > 2048 {
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
	if item.Status != model.ImageTemplateStatusPublished {
		item.Status = model.ImageTemplateStatusDraft
	}
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

func runeLen(value string) int {
	return len([]rune(value))
}

// GenerateWithImageTemplate queues count image jobs (source "template") for a published template.
func GenerateWithImageTemplate(ctx context.Context, user model.AuthUser, slug string, input TemplateGenerateInput) (model.GenerationBatch, []model.GenerationJob, error) {
	item, err := publishedImageTemplate(slug)
	if err != nil {
		return model.GenerationBatch{}, nil, err
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
			if err := checkEcommerceImage(user.ID, id, false); err != nil {
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
	feeStatus, feeQuota, err := templateFee(user, item, count)
	if err != nil {
		return model.GenerationBatch{}, nil, err
	}
	prompt := buildTemplatePrompt(item.Prompt, userPrompt, legend)
	endpoint := "/images/generations"
	if len(fileIDs) > 0 {
		endpoint = "/images/edits"
	}
	return CreateGenerationBatch(ctx, user, GenerationBatchInput{Title: item.Title, Source: model.GenerationSourceTemplate, Items: []GenerationJobInput{{
		Kind: model.GenerationKindImage, Endpoint: endpoint, Model: imageModel, ChannelID: ecommerceChannelID(user.ID),
		Request:      map[string]any{"prompt": prompt, "size": templateImageSize(ratio, resolution), "quality": templateResolutionQuality[resolution]},
		InputFileIDs: fileIDs, Prompt: userPrompt, Count: count, TemplateID: item.ID, FeeStatus: feeStatus, FeeQuota: feeQuota,
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
// and members of member-free templates; otherwise pending in new-api quota, after a balance pre-check.
// ponytail: the pre-check covers the template fee only; model fees are billed by new-api per call.
func templateFee(user model.AuthUser, item model.ImageTemplate, count int) (string, int64, error) {
	if item.FeeCredits <= 0 || !NewapiSSOEnabled() {
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
	if item.MemberFree && summary.Member.Active {
		return model.TemplateFeeWaived, 0, nil
	}
	if summary.Credits < item.FeeCredits*float64(count) {
		return "", 0, safeMessageError{message: fmt.Sprintf("余额不足：模板费需 %g %s", item.FeeCredits*float64(count), summary.CreditName)}
	}
	billing, err := publicBillingSetting()
	if err != nil {
		return "", 0, err
	}
	perUnit, err := newapiQuotaPerUnit()
	if err != nil {
		return "", 0, err
	}
	return model.TemplateFeePending, templateFeeQuota(item.FeeCredits, billing.CreditsPerUnit, perUnit), nil
}

// templateFeeQuota converts credits to new-api quota, rounded up (ignoring float noise) and at least 1.
func templateFeeQuota(credits float64, creditsPerUnit float64, quotaPerUnit float64) int64 {
	quota := math.Round(credits/creditsPerUnit*quotaPerUnit*1e6) / 1e6
	return max(int64(math.Ceil(quota)), 1)
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

// settleTemplateFees charges succeeded jobs once each (idempotency key = job ID); after
// templateFeeMaxAttempts failed calls the fee is marked failed and the job stays delivered.
func settleTemplateFees() {
	jobs, err := repository.ListPendingTemplateFeeJobs(templateFeeSettleBatch)
	if err != nil {
		log.Printf("list pending template fees failed err=%v", err)
		return
	}
	for _, job := range jobs {
		values := map[string]any{"fee_status": model.TemplateFeeCharged}
		if err := consumeTemplateFee(job); err != nil {
			attempts := job.FeeAttempts + 1
			log.Printf("template fee charge failed job=%s user=%s attempt=%d err=%v", job.ID, job.UserID, attempts, err)
			values = map[string]any{"fee_attempts": attempts}
			if attempts >= templateFeeMaxAttempts {
				values["fee_status"] = model.TemplateFeeFailed
			}
		}
		if err := repository.UpdatePendingTemplateFee(job.ID, values); err != nil {
			log.Printf("record template fee failed job=%s err=%v", job.ID, err)
		}
	}
}

func consumeTemplateFee(job model.GenerationJob) error {
	user, err := newapiLinkedUser(job.UserID)
	if err != nil {
		return err
	}
	title := "模板生成"
	if item, ok, _ := repository.GetImageTemplate(job.TemplateID); ok {
		title = item.Title
	}
	err = newapiCall(user, http.MethodPost, "/api/sso/consume", map[string]any{
		"client_id": config.Cfg.NewapiClientID, "client_secret": config.Cfg.NewapiClientSecret,
		"idempotency_key": job.ID, "quota": job.FeeQuota, "title": title,
	}, nil)
	if err == nil {
		summaryCache.delete(user.ID)
	}
	return err
}
