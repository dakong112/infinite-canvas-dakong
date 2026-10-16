package service

import (
	"cmp"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

type ModelCatalogDefaults struct {
	Group string `json:"group"`
	Image string `json:"image"`
	Text  string `json:"text"`
	Video string `json:"video"`
	Audio string `json:"audio"`
}

// ModelCatalogPlatform is what the new-api admin configured for this application.
type ModelCatalogPlatform struct {
	Name     string               `json:"name"`
	Defaults ModelCatalogDefaults `json:"defaults"`
}

type ModelCatalogGroup struct {
	Name  string  `json:"name"`
	Desc  string  `json:"desc"`
	Ratio float64 `json:"ratio"`
}

type ModelCatalogVendor struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// ModelCatalogModel is one model and the groups offering it. Kind is image, video
// or audio when new-api's metadata says so, and "" when the web should decide by name.
type ModelCatalogModel struct {
	Name       string   `json:"name"`
	VendorID   int      `json:"vendorId"`
	Kind       string   `json:"kind"`
	Groups     []string `json:"groups"`
	QuotaType  int      `json:"quotaType"`
	ModelPrice float64  `json:"modelPrice"`
	ModelRatio float64  `json:"modelRatio"`
	// CompletionRatio prices output tokens relative to input tokens (usage-based models).
	CompletionRatio float64 `json:"completionRatio"`
}

// ModelCatalog is what the model picker shows in SSO mode.
type ModelCatalog struct {
	Platform       ModelCatalogPlatform `json:"platform"`
	UserGroup      string               `json:"userGroup"`
	Groups         []ModelCatalogGroup  `json:"groups"`
	Vendors        []ModelCatalogVendor `json:"vendors"`
	Models         []ModelCatalogModel  `json:"models"`
	CreditsPerUnit float64              `json:"creditsPerUnit"`
	// QuotaPerUnit is new-api's quota per currency unit; 0 when unknown, and the web then skips usage estimates.
	QuotaPerUnit float64 `json:"quotaPerUnit"`
}

// ponytail: per-user cache (5 min, like pricing); new-api model or permission
// changes show up within that window, which the spec accepts.
var (
	catalogCache  ttlCache[ModelCatalog]
	platformCache ttlCache[ModelCatalogPlatform]
)

// ModelCatalogForUser lists the new-api groups the user may use, the models of
// each and their prices.
func ModelCatalogForUser(userID string) (ModelCatalog, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return ModelCatalog{}, err
	}
	return modelCatalog(user)
}

func modelCatalog(user model.User) (ModelCatalog, error) {
	billing, err := publicBillingSetting()
	if err != nil {
		return ModelCatalog{}, err
	}
	if cached, ok := catalogCache.get(user.ID); ok {
		cached.CreditsPerUnit = billing.CreditsPerUnit
		return cached, nil
	}
	platform, err := newapiPlatform()
	if err != nil {
		return ModelCatalog{}, err
	}
	var self struct {
		Group string `json:"group"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/user/self", nil, &self); err != nil {
		return ModelCatalog{}, err
	}
	response, err := newapiPricing(user)
	if err != nil {
		return ModelCatalog{}, err
	}
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			ModelName              string   `json:"model_name"`
			VendorID               int      `json:"vendor_id"`
			QuotaType              int      `json:"quota_type"`
			ModelPrice             float64  `json:"model_price"`
			ModelRatio             float64  `json:"model_ratio"`
			CompletionRatio        float64  `json:"completion_ratio"`
			BillingMode            string   `json:"billing_mode"`
			BillingExpr            string   `json:"billing_expr"`
			EnableGroups           []string `json:"enable_groups"`
			SupportedEndpointTypes []string `json:"supported_endpoint_types"`
		} `json:"data"`
		Vendors     []ModelCatalogVendor `json:"vendors"`
		GroupRatio  map[string]float64   `json:"group_ratio"`
		UsableGroup map[string]string    `json:"usable_group"`
		AutoGroups  []string             `json:"auto_groups"`
	}
	if err := json.Unmarshal(response, &payload); err != nil || !payload.Success {
		return ModelCatalog{}, safeMessageError{message: "模型目录暂不可用"}
	}
	catalog := ModelCatalog{Platform: platform, UserGroup: self.Group, Groups: []ModelCatalogGroup{}, Vendors: payload.Vendors, Models: []ModelCatalogModel{}, CreditsPerUnit: billing.CreditsPerUnit}
	// ponytail: an unreachable /api/status only hides usage estimates, it does not fail the catalog.
	catalog.QuotaPerUnit, _ = newapiQuotaPerUnit()
	if catalog.Vendors == nil {
		catalog.Vendors = []ModelCatalogVendor{}
	}
	// group_ratio is already limited to the user's usable groups and adjusted by group-to-group ratios.
	for name, ratio := range payload.GroupRatio {
		catalog.Groups = append(catalog.Groups, ModelCatalogGroup{Name: name, Desc: payload.UsableGroup[name], Ratio: ratio})
	}
	// new-api always lets a user call with their own group, priced or not.
	if _, ok := payload.GroupRatio[self.Group]; !ok && self.Group != "" {
		catalog.Groups = append(catalog.Groups, ModelCatalogGroup{Name: self.Group, Desc: payload.UsableGroup[self.Group], Ratio: 1})
	}
	slices.SortFunc(catalog.Groups, func(a, b ModelCatalogGroup) int {
		return cmp.Or(cmp.Compare(a.Ratio, b.Ratio), strings.Compare(a.Name, b.Name))
	})
	// "auto" has no ratio of its own: new-api routes each call over auto_groups and bills the group that
	// serves it. Its models are those of any auto group; estimates use the highest ratio among them.
	_, autoUsable := payload.UsableGroup[newapiAutoGroup]
	autoGroups := []string{}
	if autoUsable && !slices.ContainsFunc(catalog.Groups, func(group ModelCatalogGroup) bool { return group.Name == newapiAutoGroup }) {
		autoGroups = payload.AutoGroups
	}
	if len(autoGroups) > 0 {
		catalog.Groups = append(catalog.Groups, ModelCatalogGroup{Name: newapiAutoGroup, Desc: "自动", Ratio: autoGroupRatio(catalog.Groups, autoGroups)})
	}
	for _, item := range payload.Data {
		kind, listed := catalogModelKind(item.ModelName, item.SupportedEndpointTypes)
		if !listed {
			continue
		}
		groups := []string{}
		for _, group := range catalog.Groups {
			if slices.Contains(item.EnableGroups, group.Name) || slices.Contains(item.EnableGroups, "all") ||
				group.Name == newapiAutoGroup && slices.ContainsFunc(autoGroups, func(name string) bool { return slices.Contains(item.EnableGroups, name) }) {
				groups = append(groups, group.Name)
			}
		}
		if len(groups) == 0 {
			continue
		}
		entry := ModelCatalogModel{Name: item.ModelName, VendorID: item.VendorID, Kind: kind, Groups: groups, QuotaType: item.QuotaType, ModelPrice: item.ModelPrice, ModelRatio: item.ModelRatio, CompletionRatio: item.CompletionRatio}
		// Expression-based billing cannot be priced per call; show it as usage-based.
		if item.BillingExpr != "" || item.BillingMode != "" && item.BillingMode != "ratio" {
			entry.QuotaType, entry.ModelPrice = 0, 0
		}
		catalog.Models = append(catalog.Models, entry)
	}
	catalogCache.set(user.ID, catalog, pricingCacheTTL)
	return catalog, nil
}

// autoGroupRatio is the highest ratio among the usable groups auto routes over (a free group stays 0).
func autoGroupRatio(groups []ModelCatalogGroup, autoGroups []string) float64 {
	ratio := 0.0
	for _, group := range groups {
		if slices.Contains(autoGroups, group.Name) {
			ratio = max(ratio, group.Ratio)
		}
	}
	return ratio
}

// newapiAutoGroup is new-api's group that routes each call over its auto groups.
const newapiAutoGroup = "auto"

// newapiPricing reads models and group ratios from POST /api/sso/catalog (same body as /api/pricing, but
// served when the admin turned the pricing page off), or GET /api/pricing on a new-api without it (404).
func newapiPricing(user model.User) ([]byte, error) {
	response, status, err := newapiRequestStatus(user, http.MethodPost, "/api/sso/catalog", newapiClientBody(nil), pricingMaxBytes)
	if err == nil && status == http.StatusNotFound {
		// ponytail: fallback for new-api releases before /api/sso/catalog; drop once none are deployed.
		return newapiRequestLimit(user, http.MethodGet, "/api/pricing", nil, pricingMaxBytes)
	}
	return response, err
}

// catalogModelKind classifies a model by new-api's endpoint types. Embedding and
// rerank models are not listed. new-api reports "openai" for most channel types,
// so anything not clearly image/video/audio stays "" and the web decides by name.
func catalogModelKind(name string, endpoints []string) (string, bool) {
	switch {
	case slices.Contains(endpoints, "image-generation"):
		return "image", true
	case slices.Contains(endpoints, "openai-video"):
		return "video", true
	case slices.Contains(endpoints, "embeddings"), slices.Contains(endpoints, "jina-rerank"):
		return "", false
	}
	lower := strings.ToLower(name)
	for _, word := range []string{"tts", "whisper", "speech"} {
		if strings.Contains(lower, word) {
			return "audio", true
		}
	}
	if strings.Contains(lower, "audio") {
		return "text", true // gpt-4o-audio-preview etc. are chat models
	}
	return "", true
}

// newapiPlatform reads the display name and default models the new-api admin set
// for this application, authenticating with the application credentials.
func newapiPlatform() (ModelCatalogPlatform, error) {
	if cached, ok := platformCache.get(""); ok {
		return cached, nil
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/")+"/api/sso/client", nil)
	if err != nil {
		return ModelCatalogPlatform{}, err
	}
	request.SetBasicAuth(config.Cfg.NewapiClientID, config.Cfg.NewapiClientSecret)
	response, err := newapiHTTPClient.Do(request)
	if err != nil {
		log.Printf("new-api GET /api/sso/client failed: %v", err)
		return ModelCatalogPlatform{}, safeMessageError{message: "账户服务暂时不可用"}
	}
	defer response.Body.Close()
	var payload struct {
		Success bool                 `json:"success"`
		Message string               `json:"message"`
		Data    ModelCatalogPlatform `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, defaultMaxBytes)).Decode(&payload); err != nil || !payload.Success {
		log.Printf("new-api GET /api/sso/client rejected: status=%d message=%q (check NEWAPI_CLIENT_ID/SECRET)", response.StatusCode, payload.Message)
		return ModelCatalogPlatform{}, safeMessageError{message: "账户服务配置异常，请联系管理员"}
	}
	platformCache.set("", payload.Data, pricingCacheTTL)
	return payload.Data, nil
}

// newapiChannelPrefix marks the virtual channel of one new-api group: "newapi@vip".
const newapiChannelPrefix = "newapi@"

// SpaceChannelID bills calls made in a team space to the member's team key: every non-local channel
// (a platform group, a global channel or none) becomes "newapi@team:<id>", since a team space uses the
// team account's group and must never fall back to the personal key. Jobs store the result, so they
// bill the space they were created in. A team channel asked for outside its space falls back to the
// account group. Other channels are unchanged.
func SpaceChannelID(user model.AuthUser, channelID string) string {
	if !NewapiSSOEnabled() {
		return channelID
	}
	if user.SpaceID != "" {
		return newapiChannelPrefix + user.SpaceID
	}
	if strings.HasPrefix(channelID, newapiChannelPrefix+model.TeamSpacePrefix) {
		return newapiChannelPrefix
	}
	return channelID
}

// selectNewapiGroupChannel resolves "newapi@<group>" to the admin's new-api channel
// carrying the user's key for that group. Calls (publicOnly) check the catalog;
// reads of created tasks use whatever key the user holds for the group.
func selectNewapiGroupChannel(userID string, modelName string, group string, publicOnly bool) (model.ModelChannel, error) {
	var key string
	var err error
	if publicOnly {
		group, key, err = newapiCallGroupKey(userID, modelName, group)
	} else {
		key, err = newapiReadGroupKey(userID, group)
	}
	if err != nil {
		return model.ModelChannel{}, err
	}
	if key == "" {
		return model.ModelChannel{}, safeMessageError{message: "请退出后重新登录以开通模型服务"}
	}
	channel, err := newapiPlatformChannel()
	if err != nil {
		return model.ModelChannel{}, err
	}
	channel.ID = newapiChannelPrefix + group
	channel.APIKey = key
	if channel.UploadAPIKey != "" {
		channel.UploadAPIKey = key
	}
	return channel, nil
}

// newapiCallGroupKey checks the group and model against the catalog. The account
// group (or an empty group) uses the login key; other groups use a key requested
// from new-api on first use. A "team:<id>" group (see SpaceChannelID) uses the member's
// team key without the catalog: new-api checks membership, the member limit and the
// models of the team account's group.
func newapiCallGroupKey(userID string, modelName string, group string) (string, string, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return "", "", err
	}
	if user.Status == model.UserStatusBan {
		return "", "", safeMessageError{message: "账号已被禁用"}
	}
	if strings.HasPrefix(group, model.TeamSpacePrefix) {
		key, err := newapiGroupKey(user, group)
		return group, key, err
	}
	catalog, err := modelCatalog(user)
	if err != nil {
		return "", "", err
	}
	group = cmp.Or(group, catalog.UserGroup)
	if !slices.ContainsFunc(catalog.Groups, func(item ModelCatalogGroup) bool { return item.Name == group }) {
		return "", "", safeMessageError{message: "该分组不可用，请换一个分组"}
	}
	if !slices.ContainsFunc(catalog.Models, func(item ModelCatalogModel) bool {
		return item.Name == modelName && slices.Contains(item.Groups, group)
	}) {
		return "", "", safeMessageError{message: "该模型在当前分组不可用"}
	}
	if group == catalog.UserGroup {
		return group, user.NewapiAPIKey, nil
	}
	key, err := newapiGroupKey(user, group)
	return group, key, err
}

// newapiReadGroupKey serves polling and downloads of created tasks without the
// catalog, so a model leaving the group or new-api being briefly down does not
// break them: the stored group key, else the login key, of the same new-api user.
// A team group never falls back to the login key: that would bill the member personally.
func newapiReadGroupKey(userID string, group string) (string, error) {
	user, ok, err := repository.GetUserByID(userID)
	if err != nil {
		return "", err
	}
	if !ok || user.NewapiID == "" {
		return "", safeMessageError{message: "当前账号未关联统一账号"}
	}
	if user.Status == model.UserStatusBan {
		return "", safeMessageError{message: "账号已被禁用"}
	}
	stored, ok, err := repository.GetNewapiGroupKey(user.ID, group)
	if err != nil {
		return "", err
	}
	if !ok && strings.HasPrefix(group, model.TeamSpacePrefix) {
		return "", safeMessageError{message: "团队调用密钥不可用，请回到团队空间重试"}
	}
	if !ok {
		return user.NewapiAPIKey, nil
	}
	return stored.APIKey, nil
}

// newapiPlatformChannel is the enabled admin channel pointing at new-api, preferring
// an OpenAI-protocol one; it only supplies the address and protocol, the model list
// comes from the catalog.
func newapiPlatformChannel() (model.ModelChannel, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return model.ModelChannel{}, err
	}
	var found *model.ModelChannel
	for _, channel := range normalizePrivateSetting(settings.Private).Channels {
		if !channel.Enabled || isWorkflowChannelProtocol(channel.Protocol) || !IsNewapiChannel(channel) {
			continue
		}
		if channel.Protocol == "openai" {
			return channel, nil
		}
		if found == nil {
			found = &channel
		}
	}
	if found != nil {
		return *found, nil
	}
	return model.ModelChannel{}, safeMessageError{message: "平台渠道未配置，请联系管理员"}
}

// newapiGroupKey returns the stored key of a group, asking new-api once when there is none.
// A "team:<id>" group is the user's member key of that team.
func newapiGroupKey(user model.User, group string) (string, error) {
	stored, ok, err := repository.GetNewapiGroupKey(user.ID, group)
	if err != nil {
		return "", err
	}
	if ok {
		return stored.APIKey, nil
	}
	path, body := "/api/sso/key", newapiClientBody(map[string]any{"group": group})
	if raw, isTeam := strings.CutPrefix(group, model.TeamSpacePrefix); isTeam {
		teamID, err := strconv.Atoi(raw)
		if err != nil || teamID <= 0 {
			return "", safeMessageError{message: "团队无效"}
		}
		path, body = "/api/sso/team-key", newapiClientBody(map[string]any{"team_id": teamID})
	}
	response, err := newapiRequest(user, http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	var envelope newapiEnvelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		return "", safeMessageError{message: "账户服务返回异常"}
	}
	if !envelope.Success {
		// The stored token predates this application's grant.
		if strings.Contains(envelope.Message, "not issued for this client") {
			return "", safeMessageError{message: newapiReauthMessage}
		}
		return "", safeMessageError{message: newapiMessage(envelope.Message)}
	}
	var data struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil || data.Key == "" {
		return "", safeMessageError{message: "账户服务返回异常"}
	}
	if err := repository.SaveNewapiGroupKey(model.NewapiGroupKey{UserID: user.ID, GroupName: group, APIKey: data.Key, CreatedAt: now()}); err != nil {
		return "", err
	}
	return data.Key, nil
}

// RenewNewapiGroupChannel drops the stored key of a platform group channel after
// new-api rejected it (deleted in the console) and resolves the channel again.
// renewed is false when there was no stored key to replace (ordinary channels,
// the account group, or a user's own channel reusing the id), so the caller
// must not retry; err carries the user-facing reason when renewal failed.
func RenewNewapiGroupChannel(userID string, modelName string, channel model.ModelChannel) (model.ModelChannel, bool, error) {
	group, ok := strings.CutPrefix(channel.ID, newapiChannelPrefix)
	if !ok || group == "" || !NewapiSSOEnabled() {
		return model.ModelChannel{}, false, nil
	}
	// Only the platform channel itself: a user channel reusing the id, even on the new-api origin, never touches group keys.
	if platform, err := newapiPlatformChannel(); err != nil || platform.BaseURL != channel.BaseURL {
		return model.ModelChannel{}, false, nil
	}
	deleted, err := repository.DeleteNewapiGroupKey(userID, group)
	if err != nil || !deleted {
		return model.ModelChannel{}, false, err
	}
	renewed, err := selectNewapiGroupChannel(userID, modelName, group, true)
	return renewed, err == nil, err
}
