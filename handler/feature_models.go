package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// AdminTestFeatureModel runs one tiny real call of a feature model with the admin's own key and reports
// pass/fail, latency and the error text.
func AdminTestFeatureModel(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Feature string `json:"feature"`
		Group   string `json:"group"`
		Model   string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input); err != nil || !service.IsFeatureModelKey(input.Feature) || strings.TrimSpace(input.Model) == "" {
		Fail(w, "测试参数无效")
		return
	}
	user, ok := service.UserFromContext(r.Context())
	if !ok {
		Fail(w, "未登录或权限不足")
		return
	}
	user.SpaceID = "" // the admin's own key, never a team's
	channelID := service.FeatureChannelID(user, strings.TrimSpace(input.Group))
	if service.NewapiSSOEnabled() && channelID == "" {
		// A local admin has no new-api key; the admin channel only holds a placeholder.
		Fail(w, "本地管理员没有 new-api 账户，无法测试；请用 new-api 管理员账号登录后测试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	started := time.Now()
	detail, err := testFeatureModel(ctx, user, input.Feature, channelID, strings.TrimSpace(input.Model))
	result := map[string]any{"ok": err == nil, "latencyMs": time.Since(started).Milliseconds(), "detail": detail, "error": ""}
	if err != nil {
		result["error"] = err.Error()
	}
	OK(w, result)
}

func testFeatureModel(ctx context.Context, user model.AuthUser, feature string, channelID string, modelName string) (string, error) {
	switch feature {
	case service.FeatureVision, service.FeatureVideoScript:
		body, _ := json.Marshal(map[string]any{"model": modelName, "input": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "图片里写着三个数字，请只回答这三个数字，不要其他内容。"},
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(featureTestDigitsPNG())},
		}}}})
		payload, _, err := featureTestCall(ctx, user, "/responses", body, "application/json", channelID)
		if err != nil {
			return "", err
		}
		reply := strings.TrimSpace(generationOutputText(payload))
		if !strings.Contains(strings.Join(strings.Fields(reply), ""), "472") {
			return reply, errors.New("没有读出图中的数字 472")
		}
		return reply, nil
	case service.FeatureImage, service.FeatureKeyframe:
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		for key, value := range map[string]string{"model": modelName, "prompt": "把背景改成浅蓝色，数字保持不变", "size": "1024x1024", "quality": "low", "n": "1"} {
			_ = writer.WriteField(key, value)
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="image"; filename="test.png"`)
		header.Set("Content-Type", "image/png")
		part, _ := writer.CreatePart(header)
		_, _ = part.Write(featureTestDigitsPNG())
		_ = writer.Close()
		payload, contentType, err := featureTestCall(ctx, user, "/images/edits", buffer.Bytes(), writer.FormDataContentType(), channelID)
		if err != nil {
			return "", err
		}
		if _, _, _, err := imageURLsFromAIResponse(payload, contentType, false, false); err != nil {
			return "", err
		}
		return "已生成图片", nil
	case service.FeatureVideo:
		body, _ := json.Marshal(map[string]any{"model": modelName, "prompt": "一只白色陶瓷杯放在木桌上，镜头缓慢推近"})
		request := httptest.NewRequest(http.MethodPost, "http://canvas.local/api/v1/videos", bytes.NewReader(body)).WithContext(service.WithUser(ctx, user))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Model-Channel-ID", channelID)
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
			return "", errors.New(firstNonEmpty(envelope.Msg, "视频任务提交失败"))
		}
		return "任务已受理：" + envelope.Data.ID, nil
	case service.FeatureTTS:
		settings, _ := service.PublicSettings()
		body, _ := json.Marshal(map[string]any{"model": modelName, "input": "你好", "voice": firstNonEmpty(settings.Video.TTSVoice, "alloy"), "response_format": "mp3"})
		payload, contentType, err := featureTestCall(ctx, user, "/audio/speech", body, "application/json", channelID)
		if err != nil {
			return "", err
		}
		if len(payload) == 0 || strings.Contains(contentType, "json") {
			return "", errors.New("没有返回音频")
		}
		return "已返回音频", nil
	}
	return "", errors.New("未知功能")
}

// featureTestCall sends one request through the proxy and turns any failure into its user-facing message.
func featureTestCall(ctx context.Context, user model.AuthUser, endpoint string, body []byte, contentType string, channelID string) ([]byte, string, error) {
	payload, status, responseType, _ := executeCanvasAIRequestRaw(ctx, user, endpoint, body, contentType, channelID, "")
	if status <= 0 || status >= http.StatusBadRequest {
		return nil, "", errors.New(readUpstreamAIErrorMessage(payload, max(status, 0)))
	}
	if message := readWrappedTaskError(payload); message != "" {
		return nil, "", errors.New(message)
	}
	return payload, responseType, nil
}

// featureTestDigits are 5×7 bitmaps of 4, 7 and 2.
var featureTestDigits = [][7]string{
	{"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	{"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	{".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
}

// featureTestDigitsPNG draws "4 7 2" in black on white for the vision test.
func featureTestDigitsPNG() []byte {
	const scale, margin, gap = 20, 40, 60
	canvas := image.NewGray(image.Rect(0, 0, 2*margin+3*5*scale+2*gap, 2*margin+7*scale))
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}
	for index, glyph := range featureTestDigits {
		left := margin + index*(5*scale+gap)
		for row, line := range glyph {
			for column, cell := range line {
				if cell != '#' {
					continue
				}
				for y := range scale {
					for x := range scale {
						canvas.SetGray(left+column*scale+x, margin+row*scale+y, color.Gray{})
					}
				}
			}
		}
	}
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, canvas)
	return buffer.Bytes()
}
