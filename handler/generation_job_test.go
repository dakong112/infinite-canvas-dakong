package handler

import (
	"net/http"
	"testing"
)

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
