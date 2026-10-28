package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/service"
)

const newapiStateCookie = "ic_newapi_state"
const newapiStateCookiePath = "/api/auth/newapi"

func NewapiAuthConfig(w http.ResponseWriter, r *http.Request) {
	if !service.NewapiSSOEnabled() {
		OK(w, map[string]any{"enabled": false})
		return
	}
	OK(w, map[string]any{
		"enabled":     true,
		"newapiUrl":   service.NewapiPublicURL(),
		"clientId":    config.Cfg.NewapiClientID,
		"redirectUri": config.Cfg.NewapiRedirectURI,
	})
}

func NewapiAuthState(w http.ResponseWriter, r *http.Request) {
	if !service.NewapiSSOEnabled() {
		Fail(w, "未启用统一账号登录")
		return
	}
	state, err := setNewapiState(w, r)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, map[string]string{"state": state})
}

func NewapiAuthStart(w http.ResponseWriter, r *http.Request) {
	if !service.NewapiSSOEnabled() {
		http.Redirect(w, r, service.RequestOrigin(r)+"/login", http.StatusFound)
		return
	}
	state, err := setNewapiState(w, r)
	if err != nil {
		http.Redirect(w, r, service.RequestOrigin(r)+"/login?error="+url.QueryEscape("登录初始化失败"), http.StatusFound)
		return
	}
	query := url.Values{}
	query.Set("client_id", config.Cfg.NewapiClientID)
	query.Set("redirect_uri", config.Cfg.NewapiRedirectURI)
	query.Set("state", state)
	// prompt=none: new-api answers at once with a code, or error=login_required when nobody is signed in there.
	if r.URL.Query().Get("prompt") == "none" {
		query.Set("prompt", "none")
	}
	http.Redirect(w, r, service.NewapiPublicURL()+"/api/sso/authorize?"+query.Encode(), http.StatusFound)
}

// NewapiAuthLogout ends the new-api login session of the caller's token; the web drops its token either way.
func NewapiAuthLogout(w http.ResponseWriter, r *http.Request) {
	if err := service.LogoutNewapiSession(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
		FailError(w, err)
		return
	}
	OK(w, nil)
}

func NewapiAuthExchange(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	_ = json.NewDecoder(r.Body).Decode(&request)
	cookie, err := r.Cookie(newapiStateCookie)
	clearNewapiState(w, r)
	if !service.NewapiSSOEnabled() {
		Fail(w, "未启用统一账号登录")
		return
	}
	if err != nil || request.State == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(request.State)) != 1 {
		Fail(w, "登录状态已失效，请重新登录")
		return
	}
	session, err := service.LoginWithNewapiCode(request.Code)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, session)
}

func setNewapiState(w http.ResponseWriter, r *http.Request) (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	state := base64.RawURLEncoding.EncodeToString(buffer)
	http.SetCookie(w, &http.Cookie{
		Name:     newapiStateCookie,
		Value:    state,
		Path:     newapiStateCookiePath,
		MaxAge:   600,
		HttpOnly: true,
		Secure:   strings.HasPrefix(service.RequestOrigin(r), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
	return state, nil
}

func clearNewapiState(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     newapiStateCookie,
		Value:    "",
		Path:     newapiStateCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   strings.HasPrefix(service.RequestOrigin(r), "https://"),
		SameSite: http.SameSiteLaxMode,
	})
}
