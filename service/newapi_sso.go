package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

// newapiAdminRole is new-api's RoleAdminUser; root (100) is above it.
const newapiAdminRole = 10

var newapiHTTPClient = &http.Client{Timeout: 10 * time.Second}

type newapiGrant struct {
	User struct {
		ID          int    `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Role        int    `json:"role"`
		Group       string `json:"group"`
	} `json:"user"`
	AccessToken          string `json:"access_token"`
	AccessTokenExpiresAt int64  `json:"access_token_expires_at"`
	APIKey               string `json:"api_key"`
}

// NewapiSSOEnabled reports whether new-api is the user source for this deployment.
func NewapiSSOEnabled() bool {
	cfg := config.Cfg
	return strings.TrimSpace(cfg.NewapiBaseURL) != "" &&
		strings.TrimSpace(cfg.NewapiClientID) != "" &&
		strings.TrimSpace(cfg.NewapiClientSecret) != "" &&
		strings.TrimSpace(cfg.NewapiRedirectURI) != ""
}

// NewapiPublicURL is the new-api origin as reached by browsers.
func NewapiPublicURL() string {
	if value := strings.TrimSpace(config.Cfg.NewapiPublicURL); value != "" {
		return strings.TrimRight(value, "/")
	}
	return strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/")
}

// LoginWithNewapiCode redeems a new-api one-time code and signs the linked
// local user in, creating it on first login.
func LoginWithNewapiCode(code string) (model.AuthSession, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return model.AuthSession{}, safeMessageError{message: "登录凭证缺失"}
	}
	grant, err := exchangeNewapiCode(code)
	if err != nil {
		return model.AuthSession{}, err
	}
	if grant.User.ID <= 0 || grant.APIKey == "" {
		return model.AuthSession{}, safeMessageError{message: "统一账号返回数据无效"}
	}
	newapiID := strconv.Itoa(grant.User.ID)
	user, ok, err := repository.GetUserByNewapiID(newapiID)
	if err != nil {
		return model.AuthSession{}, err
	}
	if !ok {
		username, err := availableNewapiUsername(grant.User.Username, newapiID)
		if err != nil {
			return model.AuthSession{}, err
		}
		user = model.User{
			ID:        newID("user"),
			Username:  username,
			NewapiID:  newapiID,
			Status:    model.UserStatusActive,
			CreatedAt: now(),
		}
	} else if user.Status == model.UserStatusBan {
		return model.AuthSession{}, safeMessageError{message: "账号已被禁用"}
	}
	user.Role = newapiRole(grant.User.Role)
	user.Email = grant.User.Email
	user.DisplayName = firstNonEmpty(grant.User.DisplayName, grant.User.Username, user.DisplayName)
	user.NewapiAPIKey = grant.APIKey
	user.NewapiAccessToken = grant.AccessToken
	user.NewapiTokenExpiresAt = grant.AccessTokenExpiresAt
	user.LastLoginAt = now()
	user.UpdatedAt = now()
	normalizeUserDefaults(&user)
	user, err = repository.SaveUser(user)
	if err != nil {
		return model.AuthSession{}, err
	}
	return newSession(user)
}

func newapiRole(role int) model.UserRole {
	if role >= newapiAdminRole {
		return model.UserRoleAdmin
	}
	return model.UserRoleUser
}

// availableNewapiUsername keeps the new-api username unless a local account
// already owns it, in which case the new-api id is appended.
func availableNewapiUsername(username string, newapiID string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "newapi-" + newapiID, nil
	}
	_, taken, err := repository.GetUserByUsername(username)
	if err != nil {
		return "", err
	}
	if taken {
		candidate := username + "-n" + newapiID
		if _, taken, err := repository.GetUserByUsername(candidate); err != nil {
			return "", err
		} else if !taken {
			return candidate, nil
		}
		return "newapi-" + newapiID, nil
	}
	return username, nil
}

func exchangeNewapiCode(code string) (newapiGrant, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     config.Cfg.NewapiClientID,
		"client_secret": config.Cfg.NewapiClientSecret,
		"code":          code,
		"redirect_uri":  config.Cfg.NewapiRedirectURI,
	})
	endpoint := strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/") + "/api/sso/exchange"
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return newapiGrant{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := newapiHTTPClient.Do(request)
	if err != nil {
		return newapiGrant{}, fmt.Errorf("new-api exchange request failed: %w", err)
	}
	defer response.Body.Close()
	var payload struct {
		Success bool        `json:"success"`
		Message string      `json:"message"`
		Data    newapiGrant `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return newapiGrant{}, fmt.Errorf("new-api exchange response invalid: status %d", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK || !payload.Success {
		// The upstream message carries no secrets; log it for operators and show
		// the user a fixed sentence.
		log.Printf("new-api sso exchange rejected: status=%d message=%q", response.StatusCode, payload.Message)
		return newapiGrant{}, safeMessageError{message: "统一账号登录失败，请重新登录"}
	}
	return payload.Data, nil
}
