package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tigerowo/infinite-canvas/config"
)

func TestNewapiAuthStartPassesPromptNone(t *testing.T) {
	saved := config.Cfg
	t.Cleanup(func() { config.Cfg = saved })
	config.Cfg = config.Config{NewapiBaseURL: "http://newapi:3000", NewapiPublicURL: "https://account.example.com", NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	for query, prompt := range map[string]string{"?prompt=none": "none", "": "", "?prompt=login": ""} {
		recorder := httptest.NewRecorder()
		NewapiAuthStart(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/newapi/start"+query, nil))
		location, err := url.Parse(recorder.Header().Get("Location"))
		if err != nil || recorder.Code != http.StatusFound || location.Host != "account.example.com" || location.Path != "/api/sso/authorize" {
			t.Fatalf("%q: redirect %d %q", query, recorder.Code, recorder.Header().Get("Location"))
		}
		values := location.Query()
		if values.Get("prompt") != prompt || values.Get("client_id") != "infinite" || values.Get("state") == "" || values.Get("redirect_uri") != config.Cfg.NewapiRedirectURI {
			t.Fatalf("%q: query %v", query, values)
		}
		if cookies := recorder.Result().Cookies(); len(cookies) != 1 || cookies[0].Value != values.Get("state") {
			t.Fatalf("%q: state cookie %v", query, cookies)
		}
	}
}
