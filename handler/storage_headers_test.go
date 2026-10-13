package handler

import (
	"net/http/httptest"
	"testing"
)

func TestFileContentSecurityHeaders(t *testing.T) {
	cases := map[string]bool{ // content type -> forced download
		"image/png":                 false,
		"image/jpeg":                false,
		"video/mp4":                 false,
		"audio/mpeg":                false,
		"IMAGE/PNG; charset=binary": false,
		"image/svg+xml":             true,
		"text/html":                 true,
		"text/html; charset=utf-8":  true,
		"application/xhtml+xml":     true,
		"application/octet-stream":  true,
		"":                          true,
	}
	for mimeType, attachment := range cases {
		recorder := httptest.NewRecorder()
		setFileSecurityHeaders(recorder, mimeType)
		header := recorder.Header()
		if header.Get("X-Content-Type-Options") != "nosniff" || header.Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%q: missing hardening headers: %v", mimeType, header)
		}
		if got := header.Get("Content-Disposition") == "attachment"; got != attachment {
			t.Errorf("%q: attachment=%v, want %v", mimeType, got, attachment)
		}
	}
}
