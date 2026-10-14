package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
	"gorm.io/gorm"
)

const (
	generationMaxJobsPerBatch = 20
	generationMaxInputFiles   = 8
	generationMaxRequestBytes = 64 << 10
	generationMaxInputBytes   = 10 << 20
	generationMaxAttempts     = 3
	// generationMaxActivePerUser bounds queued + running jobs per user so one user cannot flood the queue.
	generationMaxActivePerUser = 100
	// generationFileRefPrefix marks a JSON string the worker replaces with the file's data URL.
	generationFileRefPrefix = "input-file:"
	worksCleanupCron        = "0 * * * *"
	// ponytail: 500 per hourly run; raise or loop when daily expirations exceed ~12k.
	worksCleanupBatch = 500
)

// generationEndpoints lists the upstream endpoints each job kind may call.
var generationEndpoints = map[string][]string{
	model.GenerationKindImage:  {"/images/generations", "/images/edits", "/chat/completions", "/responses"},
	model.GenerationKindVision: {"/responses", "/chat/completions"},
	model.GenerationKindText:   {"/responses", "/chat/completions"},
}

var generationFileRefPattern = regexp.MustCompile(`input-file:([A-Za-z0-9-]+)`)

// worksKindSources maps the works page type tabs to job sources.
var worksKindSources = map[string]string{
	"image":    model.GenerationSourceImage,
	"set":      model.GenerationSourceEcommerceSet,
	"tool":     model.GenerationSourceEcommerceTool,
	"template": model.GenerationSourceTemplate,
}

// worksStatusGroups maps the works page status filter to job statuses.
var worksStatusGroups = map[string][]string{
	"succeeded": {model.GenerationStatusSucceeded},
	"running":   {model.GenerationStatusQueued, model.GenerationStatusRunning},
	"failed":    {model.GenerationStatusFailed, model.GenerationStatusCancelled},
}

var worksCleanupOnce sync.Once

// GenerationJobInput is one requested job; Count expands it into identical jobs.
// SlotKind, Stage and the template fields are set by server-side callers, never by clients.
type GenerationJobInput struct {
	Kind         string         `json:"kind"`
	Endpoint     string         `json:"endpoint"`
	Model        string         `json:"model"`
	ChannelID    string         `json:"channelId"`
	Request      map[string]any `json:"request"`
	InputFileIDs []string       `json:"inputFileIds"`
	Prompt       string         `json:"prompt"`
	Count        int            `json:"count"`
	SlotKind     string         `json:"-"`
	Stage        string         `json:"-"`
	TemplateID   string         `json:"-"`
	FeeStatus    string         `json:"-"`
	FeeQuota     int64          `json:"-"`
}

type GenerationBatchInput struct {
	Title  string               `json:"title"`
	Source string               `json:"-"`
	Items  []GenerationJobInput `json:"items"`
}

type WorksQuery struct {
	Cursor     string
	Kind       string
	TemplateID string
	Status     string
	Limit      int
}

type WorksCounts struct {
	Succeeded int64 `json:"succeeded"`
	Running   int64 `json:"running"`
	Failed    int64 `json:"failed"`
}

type WorksPage struct {
	Items      []model.GenerationJob `json:"items"`
	NextCursor string                `json:"nextCursor"`
	Counts     WorksCounts           `json:"counts"`
}

// generationNow is the queue clock: UTC (SQLite compares times as text) truncated to the millisecond MySQL keeps.
func generationNow() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// CreateGenerationBatch validates and queues the requested jobs as one batch.
func CreateGenerationBatch(ctx context.Context, user model.AuthUser, input GenerationBatchInput) (model.GenerationBatch, []model.GenerationJob, error) {
	batch := model.GenerationBatch{ID: newID("genbatch"), UserID: user.ID, Source: input.Source, Title: strings.TrimSpace(input.Title), CreatedAt: generationNow()}
	jobs, err := buildGenerationJobs(ctx, user, batch, input.Items)
	if err != nil {
		return model.GenerationBatch{}, nil, err
	}
	if batch.Title == "" {
		batch.Title = jobs[0].Prompt
	}
	if runes := []rune(batch.Title); len(runes) > 100 {
		batch.Title = string(runes[:100])
	}
	if err := repository.CreateGenerationBatch(batch, jobs); err != nil {
		return model.GenerationBatch{}, nil, err
	}
	WakeGenerationWorker()
	return batch, jobs, nil
}

// buildGenerationJobs validates items for batch and returns jobs ready to insert (IDs assigned, nothing written).
func buildGenerationJobs(ctx context.Context, user model.AuthUser, batch model.GenerationBatch, items []GenerationJobInput) ([]model.GenerationJob, error) {
	if len(items) == 0 {
		return nil, safeMessageError{message: "请至少提交一个生成任务"}
	}
	if !UserCanUseRemoteModelChannel(user) {
		return nil, safeMessageError{message: "当前账号未开放云端渠道"}
	}
	works := worksSetting()
	member := false
	if NewapiSSOEnabled() {
		if summary, err := AccountSummaryForUser(user.ID); err == nil {
			member = summary.Member.Active
		}
	}
	retention := works.RetentionDays
	if member {
		retention = works.MemberRetentionDays
	}
	current := generationNow()
	jobs := make([]model.GenerationJob, 0, len(items))
	storesImages := false
	for _, item := range items {
		template, err := prepareGenerationJob(user, item)
		if err != nil {
			return nil, err
		}
		if item.Count < 0 {
			return nil, safeMessageError{message: "任务数量无效"}
		}
		count := max(item.Count, 1)
		// Compare against the remaining room so a huge Count cannot overflow the sum.
		if count > generationMaxJobsPerBatch-len(jobs) {
			return nil, safeMessageError{message: fmt.Sprintf("单次最多提交 %d 个任务", generationMaxJobsPerBatch)}
		}
		storesImages = storesImages || template.Kind == model.GenerationKindImage
		for range count {
			job := template
			job.ID = newID("genjob")
			job.BatchID = batch.ID
			job.Source = batch.Source
			job.Member = member
			job.NextRunAt = current
			job.CreatedAt = current
			job.ExpiresAt = current.AddDate(0, 0, retention)
			jobs = append(jobs, job)
		}
	}
	active, err := repository.CountUserActiveGenerationJobs(user.ID)
	if err != nil {
		return nil, err
	}
	if active+int64(len(jobs)) > generationMaxActivePerUser {
		return nil, safeMessageError{message: "排队中的任务过多，请等待部分任务完成后再提交"}
	}
	if storesImages {
		ok, err := CanStoreGenerationOutputs(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, safeMessageError{message: "服务端存储未启用，无法保存生成结果，请联系管理员"}
		}
	}
	return jobs, nil
}

// prepareGenerationJob validates one requested job and fills everything except identity and timing.
func prepareGenerationJob(user model.AuthUser, item GenerationJobInput) (model.GenerationJob, error) {
	item.Model = strings.TrimSpace(item.Model)
	if item.Model == "" {
		return model.GenerationJob{}, safeMessageError{message: "缺少模型名称"}
	}
	if !slices.Contains(generationEndpoints[item.Kind], item.Endpoint) {
		return model.GenerationJob{}, safeMessageError{message: "不支持的任务类型"}
	}
	if len(item.InputFileIDs) > generationMaxInputFiles {
		return model.GenerationJob{}, safeMessageError{message: fmt.Sprintf("输入图片最多 %d 张", generationMaxInputFiles)}
	}
	if item.Endpoint == "/images/edits" && len(item.InputFileIDs) == 0 {
		return model.GenerationJob{}, safeMessageError{message: "图片编辑至少需要一张输入图片"}
	}
	for _, id := range item.InputFileIDs {
		object, err := repository.GetStorageObject(id)
		if err != nil || object.CreatedBy != user.ID {
			return model.GenerationJob{}, safeMessageError{message: "输入图片不存在"}
		}
		if object.Bytes > generationMaxInputBytes {
			return model.GenerationJob{}, safeMessageError{message: "输入图片超过 10MB"}
		}
	}
	if item.Request == nil {
		return model.GenerationJob{}, safeMessageError{message: "缺少请求参数"}
	}
	request := maps.Clone(item.Request)
	request["model"] = item.Model
	if item.Kind == model.GenerationKindImage && strings.HasPrefix(item.Endpoint, "/images/") {
		request["n"] = 1
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return model.GenerationJob{}, safeMessageError{message: "请求参数无效"}
	}
	if len(raw) > generationMaxRequestBytes || bytes.Contains(raw, []byte(";base64,")) {
		return model.GenerationJob{}, safeMessageError{message: "请求参数过大，图片请通过输入文件提交"}
	}
	for _, match := range generationFileRefPattern.FindAllStringSubmatch(string(raw), -1) {
		if !slices.Contains(item.InputFileIDs, match[1]) {
			return model.GenerationJob{}, safeMessageError{message: "输入图片引用无效"}
		}
	}
	channel, err := SelectModelChannelForUser(user.ID, item.Model, strings.TrimSpace(item.ChannelID), true)
	if err != nil {
		if safe, ok := err.(interface{ SafeMessage() string }); ok {
			return model.GenerationJob{}, safeMessageError{message: safe.SafeMessage()}
		}
		log.Printf("generation channel select failed: user=%s model=%s err=%v", user.ID, item.Model, err)
		return model.GenerationJob{}, safeMessageError{message: "模型 " + item.Model + " 暂不可用"}
	}
	prompt := strings.TrimSpace(item.Prompt)
	// Template jobs show only the user's own text: the template's system prompt stays server-side.
	if prompt == "" && item.TemplateID == "" {
		prompt, _ = request["prompt"].(string)
	}
	return model.GenerationJob{
		UserID: user.ID, Kind: item.Kind, Endpoint: item.Endpoint, Model: item.Model, ChannelID: channel.ID,
		RequestJSON: string(raw), InputFileIDs: append([]string{}, item.InputFileIDs...), Status: model.GenerationStatusQueued,
		MaxAttempts: generationMaxAttempts, Prompt: prompt, SlotKind: item.SlotKind, Stage: item.Stage,
		TemplateID: item.TemplateID, FeeStatus: item.FeeStatus, FeeQuota: item.FeeQuota,
	}, nil
}

func GenerationJobsByIDs(userID string, ids []string) ([]model.GenerationJob, error) {
	if len(ids) > 200 {
		ids = ids[:200]
	}
	return repository.ListUserGenerationJobs(userID, ids)
}

func BatchGenerationJobs(userID string, batchID string) ([]model.GenerationJob, error) {
	if strings.TrimSpace(batchID) == "" {
		return nil, safeMessageError{message: "缺少批次 ID"}
	}
	return repository.ListUserBatchGenerationJobs(userID, batchID)
}

func CancelGenerationJob(userID string, id string) (model.GenerationJob, error) {
	ok, err := repository.CancelUserGenerationJob(userID, id, generationNow())
	if err != nil {
		return model.GenerationJob{}, err
	}
	if !ok {
		return model.GenerationJob{}, safeMessageError{message: "任务不存在或已结束"}
	}
	if entry, found := generationJobCancels.Load(id); found {
		entry.(*generationCancel).cancel()
	}
	job, _, err := repository.GetUserGenerationJob(userID, id)
	return job, err
}

func RetryGenerationJob(userID string, id string) (model.GenerationJob, error) {
	ok, err := repository.RetryUserGenerationJob(userID, id, generationNow())
	if err != nil {
		return model.GenerationJob{}, err
	}
	if !ok {
		if job, found, _ := repository.GetUserGenerationJob(userID, id); found && job.Kind == model.GenerationKindVision && job.Source == model.GenerationSourceEcommerceSet {
			return model.GenerationJob{}, safeMessageError{message: "该任务属于商品套图流程，请在套图页面重新操作"}
		}
		return model.GenerationJob{}, safeMessageError{message: "只有失败或已取消的任务可以重试"}
	}
	WakeGenerationWorker()
	job, _, err := repository.GetUserGenerationJob(userID, id)
	return job, err
}

// ListWorks pages the user's image results with per-status counters.
func ListWorks(userID string, query WorksQuery) (WorksPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	source, known := worksKindSources[query.Kind]
	if query.Kind != "" && !known {
		return WorksPage{}, safeMessageError{message: "作品类型无效"}
	}
	var cursorAt time.Time
	cursorID := ""
	if raw, id, ok := strings.Cut(query.Cursor, "|"); ok {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return WorksPage{}, safeMessageError{message: "分页参数无效"}
		}
		cursorAt, cursorID = parsed, id
	}
	items, err := repository.ListUserWorks(userID, source, query.TemplateID, worksStatusGroups[query.Status], cursorAt, cursorID, limit)
	if err != nil {
		return WorksPage{}, err
	}
	page := WorksPage{Items: items}
	if len(items) == limit {
		last := items[len(items)-1]
		page.NextCursor = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID
	}
	counts, err := repository.CountUserWorks(userID, source, query.TemplateID)
	if err != nil {
		return WorksPage{}, err
	}
	page.Counts = WorksCounts{
		Succeeded: counts[model.GenerationStatusSucceeded],
		Running:   counts[model.GenerationStatusQueued] + counts[model.GenerationStatusRunning],
		Failed:    counts[model.GenerationStatusFailed] + counts[model.GenerationStatusCancelled],
	}
	return page, nil
}

// DeleteWork cancels the job if still active, deletes its stored output and hides it from works.
func DeleteWork(ctx context.Context, userID string, id string) error {
	job, ok, err := repository.GetUserGenerationJob(userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return safeMessageError{message: "作品不存在"}
	}
	if job.Status == model.GenerationStatusQueued || job.Status == model.GenerationStatusRunning {
		if _, err := CancelGenerationJob(userID, id); err != nil {
			return err
		}
	}
	if job.OutputFileID != "" {
		if err := DeleteStorageObject(ctx, job.OutputFileID, nil); errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, os.ErrNotExist) {
			// The file is already gone: deleting the work must still succeed.
			log.Printf("delete work output: file already missing: job=%s file=%s", job.ID, job.OutputFileID)
		} else if err != nil {
			log.Printf("delete work output failed: job=%s file=%s err=%v", job.ID, job.OutputFileID, err)
			return safeMessageError{message: "作品文件删除失败，请稍后重试"}
		}
	}
	return repository.MarkGenerationJobDeleted(job.ID, generationNow())
}

// GenerationJobUser loads the job owner the worker acts for.
func GenerationJobUser(userID string) (model.AuthUser, error) {
	user, ok, err := repository.GetUserByID(userID)
	if err != nil {
		return model.AuthUser{}, err
	}
	if !ok {
		return model.AuthUser{}, safeMessageError{message: "用户不存在"}
	}
	if user.Status == model.UserStatusBan {
		return model.AuthUser{}, safeMessageError{message: "账号已被禁用"}
	}
	return model.PublicUser(user), nil
}

type generationInput struct {
	id       string
	mimeType string
	data     []byte
}

// BuildGenerationRequest rebuilds the upstream body: multipart with "image" parts for /images/edits,
// otherwise the stored JSON with each "input-file:<id>" string replaced by the file's data URL.
func BuildGenerationRequest(job model.GenerationJob) ([]byte, string, error) {
	inputs := make([]generationInput, 0, len(job.InputFileIDs))
	for _, id := range job.InputFileIDs {
		download, err := DownloadStorageObject(id, "")
		if err != nil {
			log.Printf("generation input read failed: job=%s file=%s err=%v", job.ID, id, err)
			return nil, "", GenerationError{Message: "输入图片读取失败", Retryable: true}
		}
		data, err := io.ReadAll(io.LimitReader(download.Stream, generationMaxInputBytes+1))
		_ = download.Stream.Close()
		if err != nil {
			log.Printf("generation input read failed: job=%s file=%s err=%v", job.ID, id, err)
			return nil, "", GenerationError{Message: "输入图片读取失败", Retryable: true}
		}
		if len(data) > generationMaxInputBytes {
			return nil, "", GenerationError{Message: "输入图片超过 10MB"}
		}
		mimeType := download.Object.MimeType
		if !strings.HasPrefix(mimeType, "image/") {
			mimeType = http.DetectContentType(data)
		}
		inputs = append(inputs, generationInput{id: id, mimeType: mimeType, data: data})
	}
	if job.Endpoint != "/images/edits" {
		body := job.RequestJSON
		for _, input := range inputs {
			dataURL := "data:" + input.mimeType + ";base64," + base64.StdEncoding.EncodeToString(input.data)
			body = strings.ReplaceAll(body, strconv.Quote(generationFileRefPrefix+input.id), strconv.Quote(dataURL))
		}
		return []byte(body), "application/json", nil
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(job.RequestJSON), &fields); err != nil {
		return nil, "", GenerationError{Message: "任务参数已损坏"}
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		text, ok := fields[key].(string)
		if !ok {
			encoded, _ := json.Marshal(fields[key])
			text = string(encoded)
		}
		if err := writer.WriteField(key, text); err != nil {
			return nil, "", err
		}
	}
	for index, input := range inputs {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "image", "filename": fmt.Sprintf("input-%d%s", index+1, extensionForContentType(input.mimeType))}))
		header.Set("Content-Type", input.mimeType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err := part.Write(input.data); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func worksSetting() model.PublicWorksSetting {
	settings, err := repository.GetSettings()
	if err != nil {
		log.Printf("load works setting failed err=%v", err)
	}
	return normalizePublicSetting(settings.Public).Works
}

// StartWorksCleanupScheduler deletes expired works every hour and once at startup.
func StartWorksCleanupScheduler() {
	worksCleanupOnce.Do(func() {
		scheduler := cron.New()
		if _, err := scheduler.AddFunc(worksCleanupCron, cleanupExpiredWorks); err != nil {
			log.Printf("add works cleanup cron failed err=%v", err)
			return
		}
		scheduler.Start()
	})
	go cleanupExpiredWorks()
}

func cleanupExpiredWorks() {
	current := generationNow()
	jobs, err := repository.ListExpiredGenerationJobs(current, worksCleanupBatch)
	if err != nil {
		log.Printf("list expired works failed err=%v", err)
		return
	}
	for _, job := range jobs {
		if job.OutputFileID != "" {
			if err := DeleteStorageObject(context.Background(), job.OutputFileID, nil); err != nil {
				// Mark the row deleted anyway: a file that keeps failing to delete must not stall every later cleanup.
				log.Printf("works cleanup delete file failed (file left orphaned): job=%s file=%s err=%v", job.ID, job.OutputFileID, err)
			}
		}
		if err := repository.MarkGenerationJobDeleted(job.ID, current); err != nil {
			log.Printf("works cleanup mark deleted failed: job=%s err=%v", job.ID, err)
		}
	}
}
