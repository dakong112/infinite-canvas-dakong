package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

func StartGenerationWorker() {
	service.StartGenerationWorker(executeGenerationJob)
}

// executeGenerationJob runs one job through the same proxy path as live requests (channel choice,
// SSO user key, local credits), then stores image results so the database never holds image bytes.
func executeGenerationJob(ctx context.Context, job model.GenerationJob) (service.GenerationJobResult, error) {
	user, err := service.GenerationJobUser(job.UserID)
	if err != nil {
		log.Printf("generation job user unavailable: job=%s user=%s err=%v", job.ID, job.UserID, err)
		return service.GenerationJobResult{}, service.GenerationError{Message: "账号不可用"}
	}
	if job.Kind == model.GenerationKindImage {
		// Fail before calling (and billing) the upstream if the result could not be saved anyway.
		stored, err := service.CanStoreGenerationOutputs(service.WithUser(ctx, user))
		if err != nil {
			return service.GenerationJobResult{}, service.GenerationError{Message: "存储状态检查失败", Retryable: true}
		}
		if !stored {
			return service.GenerationJobResult{}, service.GenerationError{Message: "服务端存储未启用，无法保存生成结果"}
		}
	}
	body, contentType, err := service.BuildGenerationRequest(job)
	if err != nil {
		// Nothing was sent upstream yet, so retrying cannot bill twice; the error says whether it can succeed later.
		var failure service.GenerationError
		if errors.As(err, &failure) {
			return service.GenerationJobResult{}, failure
		}
		return service.GenerationJobResult{}, service.GenerationError{Message: err.Error()}
	}
	payload, status, responseContentType, _ := executeCanvasAIRequestRaw(ctx, user, job.Endpoint, body, contentType, job.ChannelID, "")
	if ctx.Err() != nil {
		return service.GenerationJobResult{}, ctx.Err()
	}
	if status <= 0 || status >= http.StatusBadRequest {
		return service.GenerationJobResult{}, service.GenerationError{Message: readUpstreamAIErrorMessage(payload, max(status, 0)), Retryable: generationJobRetryable(job.Kind, status)}
	}
	if message := readWrappedTaskError(payload); message != "" {
		return service.GenerationJobResult{}, service.GenerationError{Message: message}
	}
	if job.Kind != model.GenerationKindImage {
		text := strings.TrimSpace(generationOutputText(payload))
		if text == "" {
			return service.GenerationJobResult{}, service.GenerationError{Message: "模型没有返回文本"}
		}
		return service.GenerationJobResult{OutputText: text}, nil
	}
	urls, _, _, err := imageURLsFromAIResponse(payload, responseContentType, false, job.Endpoint == "/chat/completions")
	if err != nil {
		return service.GenerationJobResult{}, service.GenerationError{Message: err.Error()}
	}
	// The upstream already billed this call: later failures are final so a retry cannot charge again.
	data, mimeType, err := generationImageBytes(ctx, urls[0])
	if err != nil || len(data) == 0 {
		log.Printf("generation job image download failed: job=%s err=%v", job.ID, err)
		return service.GenerationJobResult{}, service.GenerationError{Message: "图片结果下载失败"}
	}
	uploaded, err := service.UploadStorageObject(service.WithUser(ctx, user), "", mimeType, data)
	if err != nil {
		log.Printf("generation job upload failed: job=%s err=%v", job.ID, err)
		return service.GenerationJobResult{}, service.GenerationError{Message: "图片结果保存失败"}
	}
	width, height := imageSize(data)
	return service.GenerationJobResult{OutputFileID: uploaded.ID, OutputURL: uploaded.URL, Width: width, Height: height}, nil
}

// generationImageBytes loads a result image for a job. Unlike imageCandidateBytes it never reaches
// internal addresses (model output is untrusted, and the bytes become a user-visible work) and only
// accepts real images, storing the sniffed type rather than the claimed one.
func generationImageBytes(ctx context.Context, value string) ([]byte, string, error) {
	var data []byte
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
		if err != nil {
			return nil, "", err
		}
		response, err := service.SafeProxyHTTPClient().Do(request)
		if err != nil {
			return nil, "", err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, "", errors.New(response.Status)
		}
		data, err = io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
		if err != nil {
			return nil, "", err
		}
	} else {
		var err error
		if data, _, err = imageCandidateBytes(value); err != nil {
			return nil, "", err
		}
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", errors.New("结果不是图片")
	}
	return data, mimeType, nil
}

// generationJobRetryable decides whether a failed upstream call is worth repeating. 504/524 on image
// endpoints mean the upstream may still be generating (and billing), so they are final there.
func generationJobRetryable(kind string, status int) bool {
	if kind == model.GenerationKindImage && (status == http.StatusGatewayTimeout || status == 524) {
		return false
	}
	return generationRetryableStatus(status)
}

// generationRetryableStatus: network errors (0), timeouts, rate limits and upstream 5xx are retried;
// -1 (a network error after the request was sent) and other 4xx — bad parameters, content-policy rejections, auth or quota problems — fail at once.
func generationRetryableStatus(status int) bool {
	return status == 0 || status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// generationOutputText reads the assistant text of a /responses or /chat/completions reply.
func generationOutputText(payload []byte) string {
	var response struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(payload, &response) != nil {
		return ""
	}
	if strings.TrimSpace(response.OutputText) != "" {
		return response.OutputText
	}
	var text strings.Builder
	for _, item := range response.Output {
		for _, part := range item.Content {
			if part.Type == "output_text" {
				text.WriteString(part.Text)
			}
		}
	}
	if text.Len() > 0 {
		return text.String()
	}
	if len(response.Choices) > 0 {
		return response.Choices[0].Message.Content
	}
	return ""
}

func requireGenerationUser(w http.ResponseWriter, r *http.Request) (model.AuthUser, bool) {
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		Fail(w, "未登录或权限不足")
	}
	return user, ok
}

func CreateGenerationJobs(w http.ResponseWriter, r *http.Request) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	var input service.GenerationBatchInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		Fail(w, "任务参数无效")
		return
	}
	input.Source = model.GenerationSourceImage
	batch, jobs, err := service.CreateGenerationBatch(r.Context(), user, input)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, map[string]any{"batchId": batch.ID, "jobs": jobs})
}

func GenerationJobsStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); err != nil {
		Fail(w, "任务参数无效")
		return
	}
	jobs, err := service.GenerationJobsByIDs(user.ID, request.IDs)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, jobs)
}

func BatchGenerationJobs(w http.ResponseWriter, r *http.Request) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	jobs, err := service.BatchGenerationJobs(user.ID, r.URL.Query().Get("batch_id"))
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, jobs)
}

func CancelGenerationJob(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	job, err := service.CancelGenerationJob(user.ID, id)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, job)
}

func RetryGenerationJob(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	job, err := service.RetryGenerationJob(user.ID, id)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, job)
}

func UserWorks(w http.ResponseWriter, r *http.Request) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	page, err := service.ListWorks(user.ID, service.WorksQuery{Cursor: query.Get("cursor"), Kind: query.Get("kind"), Status: query.Get("status"), Limit: limit})
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, page)
}

func DeleteUserWork(w http.ResponseWriter, r *http.Request, id string) {
	user, ok := requireGenerationUser(w, r)
	if !ok {
		return
	}
	if err := service.DeleteWork(r.Context(), user.ID, id); err != nil {
		FailError(w, err)
		return
	}
	OK(w, map[string]bool{"deleted": true})
}
