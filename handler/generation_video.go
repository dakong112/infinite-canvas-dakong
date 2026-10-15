package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// generationDownloadSlots caps concurrent video result downloads: each may buffer up to 200MB because
// storage uploads take bytes.
var generationDownloadSlots = make(chan struct{}, 3)

// cappedRecorder records a proxied response but refuses bytes past limit+1, so an oversized /content
// reply stops the copy (and is reported as too large) instead of filling memory.
type cappedRecorder struct {
	*httptest.ResponseRecorder
	limit int
}

func (recorder cappedRecorder) Write(data []byte) (int, error) {
	if room := recorder.limit + 1 - recorder.Body.Len(); len(data) > room {
		written, _ := recorder.ResponseRecorder.Write(data[:max(room, 0)])
		return written, errors.New("response exceeds limit")
	}
	return recorder.ResponseRecorder.Write(data)
}

func (recorder cappedRecorder) WriteString(text string) (int, error) {
	return recorder.Write([]byte(text))
}

// generationVideoTaskID is the video_tasks row of a video job; a fixed ID lets a re-run after a crash
// find the earlier submit instead of submitting (and billing) again.
func generationVideoTaskID(job model.GenerationJob) string {
	return "client_video_task_" + job.ID
}

// executeVideoGenerationJob submits a video job through POST /api/v1/videos (channel choice, protocol
// normalization, local credits and refunds) and hands its task row to the polling stage.
func executeVideoGenerationJob(ctx context.Context, user model.AuthUser, job model.GenerationJob) (service.GenerationJobResult, error) {
	taskID := generationVideoTaskID(job)
	task, found, err := service.GetUserVideoTask(user.ID, taskID)
	if err != nil {
		return service.GenerationJobResult{}, service.GenerationError{Message: "视频任务读取失败", Retryable: true}
	}
	if found && !service.IsFailedVideoTaskStatus(task.Status) {
		return service.GenerationJobResult{UpstreamTaskID: task.ID}, nil
	}
	if found {
		// A retry of a failed video starts a new upstream task under the same row ID.
		if err := service.DeleteUserVideoTask(user.ID, taskID); err != nil {
			return service.GenerationJobResult{}, service.GenerationError{Message: "视频任务读取失败", Retryable: true}
		}
	}
	stored, err := service.CanStoreGenerationOutputs(service.WithUser(ctx, user))
	if err != nil {
		return service.GenerationJobResult{}, service.GenerationError{Message: "存储状态检查失败", Retryable: true}
	}
	if !stored {
		return service.GenerationJobResult{}, service.GenerationError{Message: "服务端存储未启用，无法保存生成结果"}
	}
	body, contentType, err := service.BuildGenerationRequest(job)
	if err != nil {
		if failure, ok := err.(service.GenerationError); ok {
			return service.GenerationJobResult{}, failure
		}
		return service.GenerationJobResult{}, service.GenerationError{Message: err.Error()}
	}
	request := httptest.NewRequest(http.MethodPost, "http://canvas.local/api/v1/videos", bytes.NewReader(body)).WithContext(service.WithUser(ctx, user))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Model-Channel-ID", job.ChannelID)
	request.Header.Set("X-Client-Video-Task-ID", taskID)
	request.Header.Set("X-Video-Task-Source", "generation")
	request.Header.Set("X-Video-Task-Source-ID", job.ID)
	recorder := httptest.NewRecorder()
	proxyAIVideoTaskRequest(recorder, request)
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.ID == "" {
		// Never retried: the upstream may have accepted (and billed) the request.
		return service.GenerationJobResult{}, service.GenerationError{Message: firstNonEmpty(envelope.Msg, "视频任务提交失败")}
	}
	return service.GenerationJobResult{UpstreamTaskID: envelope.Data.ID}, nil
}

// pollGenerationJob polls a submitted video job's task row with the provider poller, then stores the
// finished video so the work outlives the upstream URL and the video_tasks cleanup.
func pollGenerationJob(ctx context.Context, job model.GenerationJob) (service.GenerationJobResult, bool, error) {
	user, err := service.GenerationJobUser(job.UserID)
	if err != nil {
		return service.GenerationJobResult{}, false, service.GenerationError{Message: "账号不可用"}
	}
	task, found, err := service.GetUserVideoTask(job.UserID, job.UpstreamTaskID)
	if err != nil {
		return service.GenerationJobResult{}, false, err
	}
	if !found {
		return service.GenerationJobResult{}, false, service.GenerationError{Message: "视频任务记录不存在"}
	}
	if !service.IsCompletedVideoTaskStatus(task.Status) && !service.IsFailedVideoTaskStatus(task.Status) {
		update, err := pollVideoTaskFromUpstream(ctx, task)
		if err != nil {
			log.Printf("generation video poll failed: job=%s task=%s err=%v", job.ID, task.ID, err)
			return service.GenerationJobResult{}, false, nil
		}
		if err := service.UpdateVideoTaskFromPoll(task, update); err != nil {
			return service.GenerationJobResult{}, false, err
		}
		if task, found, err = service.GetUserVideoTask(job.UserID, job.UpstreamTaskID); err != nil || !found {
			return service.GenerationJobResult{}, false, err
		}
	}
	if service.IsFailedVideoTaskStatus(task.Status) {
		return service.GenerationJobResult{}, false, service.GenerationError{Message: firstNonEmpty(task.Error, "视频生成失败")}
	}
	if !service.IsCompletedVideoTaskStatus(task.Status) {
		return service.GenerationJobResult{}, false, nil
	}
	select {
	case generationDownloadSlots <- struct{}{}:
		defer func() { <-generationDownloadSlots }()
	case <-ctx.Done():
		return service.GenerationJobResult{}, false, service.GenerationError{Message: "视频结果下载排队超时", Retryable: true}
	}
	data, mimeType, err := videoTaskContent(ctx, user, task)
	if err != nil {
		log.Printf("generation video download failed: job=%s task=%s err=%v", job.ID, task.ID, err)
		return service.GenerationJobResult{}, false, service.GenerationError{Message: "视频结果下载失败", Retryable: true}
	}
	uploaded, err := service.UploadStorageObject(service.WithUser(ctx, user), "", mimeType, data)
	if err != nil {
		log.Printf("generation video upload failed: job=%s err=%v", job.ID, err)
		return service.GenerationJobResult{}, false, service.GenerationError{Message: "视频结果保存失败", Retryable: true}
	}
	return service.GenerationJobResult{OutputFileID: uploaded.ID, OutputURL: uploaded.URL}, true, nil
}

// videoTaskContent loads a finished video: its public URL when one works, else the provider content
// endpoint (GET /videos/{id}/content, which also covers Gemini's authenticated downloads).
func videoTaskContent(ctx context.Context, user model.AuthUser, task model.VideoTask) ([]byte, string, error) {
	if strings.HasPrefix(task.VideoURL, "http://") || strings.HasPrefix(task.VideoURL, "https://") {
		data, contentType, err := generationRemoteBytes(ctx, task.VideoURL, service.GenerationMaxMediaBytes)
		if mimeType := mediaType(data, contentType, "video/"); err == nil && mimeType != "" {
			return data, mimeType, nil
		}
	}
	id := firstNonEmpty(task.UpstreamTaskID, task.ID)
	request := httptest.NewRequest(http.MethodGet, "http://canvas.local/api/v1/videos/"+url.PathEscape(id)+"/content?model="+url.QueryEscape(task.Model), nil).WithContext(service.WithUser(ctx, user))
	request.Header.Set("X-Model-Channel-ID", task.ChannelID)
	recorder := cappedRecorder{httptest.NewRecorder(), service.GenerationMaxMediaBytes}
	AIVideoContent(recorder, request, id)
	data := recorder.Body.Bytes()
	mimeType := mediaType(data, recorder.Header().Get("Content-Type"), "video/")
	if recorder.Code >= http.StatusBadRequest || mimeType == "" {
		return nil, "", &aiError{message: "视频内容接口没有返回视频: " + string(data[:min(len(data), 300)])}
	}
	if len(data) > service.GenerationMaxMediaBytes {
		return nil, "", &aiError{message: "视频结果过大"}
	}
	return data, mimeType, nil
}

// storeGenerationAudio saves a speech result: raw audio bytes, or the audio_url of a JSON reply.
func storeGenerationAudio(ctx context.Context, user model.AuthUser, job model.GenerationJob, payload []byte, contentType string) (service.GenerationJobResult, error) {
	if strings.Contains(contentType, "json") {
		var reply struct {
			AudioURL string `json:"audio_url"`
		}
		if json.Unmarshal(payload, &reply) != nil || reply.AudioURL == "" {
			return service.GenerationJobResult{}, service.GenerationError{Message: "音频接口没有返回音频文件"}
		}
		var err error
		if payload, contentType, err = generationRemoteBytes(ctx, reply.AudioURL, service.GenerationMaxMediaBytes); err != nil {
			log.Printf("generation audio download failed: job=%s err=%v", job.ID, err)
			return service.GenerationJobResult{}, service.GenerationError{Message: "音频结果下载失败"}
		}
	}
	mimeType := mediaType(payload, contentType, "audio/")
	if mimeType == "" {
		return service.GenerationJobResult{}, service.GenerationError{Message: "音频接口没有返回音频文件"}
	}
	uploaded, err := service.UploadStorageObject(service.WithUser(ctx, user), "", mimeType, payload)
	if err != nil {
		log.Printf("generation audio upload failed: job=%s err=%v", job.ID, err)
		return service.GenerationJobResult{}, service.GenerationError{Message: "音频结果保存失败"}
	}
	return service.GenerationJobResult{OutputFileID: uploaded.ID, OutputURL: uploaded.URL}, nil
}

// mediaType is the claimed type when it has the wanted prefix, else the sniffed one; "" if neither matches.
func mediaType(data []byte, contentType string, prefix string) string {
	if len(data) == 0 {
		return ""
	}
	for _, candidate := range []string{contentType, http.DetectContentType(data)} {
		if candidate = strings.ToLower(strings.TrimSpace(strings.Split(candidate, ";")[0])); strings.HasPrefix(candidate, prefix) {
			return candidate
		}
	}
	return ""
}
