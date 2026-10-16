package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
)

func TestNewapiQuotaMessage(t *testing.T) {
	team := model.ModelChannel{ID: "newapi@team:7"}
	personal := model.ModelChannel{ID: "newapi@vip"}
	const member, wallet, topUp = "你的团队额度已用完，请联系团队管理员调整", "团队余额不足，请联系团队管理员充值", "积分不足，请先充值"
	for _, tc := range []struct {
		body    string
		channel model.ModelChannel
		want    string
	}{
		{`{"error":{"message":"token quota is not enough, token remain quota: $0.01, need quota: $0.05"}}`, team, member},
		{`{"error":{"message":"x","code":"pre_consume_token_quota_failed"}}`, team, member},
		{`{"error":{"message":"user quota is not enough, user quota: $0, need quota: $1"}}`, team, wallet},
		{`{"error":{"message":"用户额度不足, 剩余额度: $0","code":"insufficient_user_quota"}}`, team, wallet},
		{`{"error":{"message":"Insufficient user quota, remaining quota: $0"}}`, personal, topUp},
		{`{"error":{"message":"token quota is not enough"}}`, personal, topUp},
		{`{"error":{"message":"预扣费额度失败, 用户剩余额度: $0"}}`, model.ModelChannel{}, topUp},
		{`{"error":{"message":"invalid size"}}`, team, ""},
	} {
		if got := newapiQuotaMessage([]byte(tc.body), tc.channel); got != tc.want {
			t.Errorf("%s on %q: %q, want %q", tc.body, tc.channel.ID, got, tc.want)
		}
	}
}

func TestGenerationRetryableStatus(t *testing.T) {
	cases := map[int]bool{
		0:                                true, // network error or timeout before any response
		http.StatusRequestTimeout:        true,
		http.StatusTooManyRequests:       true,
		http.StatusInternalServerError:   true,
		http.StatusBadGateway:            true,
		http.StatusServiceUnavailable:    true,
		http.StatusGatewayTimeout:        true,
		http.StatusBadRequest:            false, // parameter errors and content-policy rejections
		http.StatusUnauthorized:          false,
		http.StatusForbidden:             false, // includes new-api insufficient quota
		http.StatusNotFound:              false,
		http.StatusRequestEntityTooLarge: false,
		http.StatusUnprocessableEntity:   false,
	}
	for status, want := range cases {
		if got := generationRetryableStatus(status); got != want {
			t.Errorf("status %d: retryable=%v, want %v", status, got, want)
		}
	}
}

func TestGenerationOutputText(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"responses output_text field", `{"output_text":"直接文本"}`, "直接文本"},
		{"responses message parts", `{"output":[{"type":"reasoning","summary":[]},{"type":"message","content":[{"type":"output_text","text":"第一段"},{"type":"output_text","text":"第二段"}]}]}`, "第一段第二段"},
		{"chat completions", `{"choices":[{"message":{"content":"聊天文本"}}]}`, "聊天文本"},
		{"not json", `oops`, ""},
	}
	for _, test := range cases {
		if got := generationOutputText([]byte(test.payload)); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

func TestGenerationImageBytesRejectsInternalAndNonImages(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer internal.Close()
	// The server listens on 127.0.0.1: a model-supplied URL pointing there must never be fetched.
	if _, _, err := generationImageBytes(context.Background(), internal.URL); err == nil {
		t.Fatal("internal URL was fetched")
	}
	data, mimeType, err := generationImageBytes(context.Background(), base64.StdEncoding.EncodeToString(png))
	if err != nil || mimeType != "image/png" || len(data) == 0 {
		t.Fatalf("base64 png: %v %q", err, mimeType)
	}
	if _, _, err := generationImageBytes(context.Background(), base64.StdEncoding.EncodeToString([]byte("<html>not an image</html>"))); err == nil {
		t.Fatal("non-image bytes accepted")
	}
}

func TestUpstreamNetworkStatus(t *testing.T) {
	dial := &url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	if got := upstreamNetworkStatus(dial); got != 0 {
		t.Errorf("dial error status = %d, want 0", got)
	}
	read := &url.Error{Op: "Post", URL: "http://x", Err: &net.OpError{Op: "read", Err: errors.New("timeout")}}
	if got := upstreamNetworkStatus(read); got != -1 {
		t.Errorf("read error status = %d, want -1", got)
	}
	if generationRetryableStatus(-1) || !generationRetryableStatus(0) {
		t.Error("only a never-connected failure (0) may be retried")
	}
	if generationJobRetryable("image", 504) || generationJobRetryable("image", 524) || !generationJobRetryable("text", 504) {
		t.Error("image 504/524 must be final, other kinds retried")
	}
}
