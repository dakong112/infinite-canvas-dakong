package service

import (
	"cmp"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
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
}

// ModelCatalog is what the model picker shows in SSO mode.
type ModelCatalog struct {
	Platform       ModelCatalogPlatform `json:"platform"`
	UserGroup      string               `json:"userGroup"`
	Groups         []ModelCatalogGroup  `json:"groups"`
	Vendors        []ModelCatalogVendor `json:"vendors"`
	Models         []ModelCatalogModel  `json:"models"`
	CreditsPerUnit float64              `json:"creditsPerUnit"`
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
	response, err := newapiRequestLimit(user, http.MethodGet, "/api/pricing", nil, pricingMaxBytes)
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
			BillingMode            string   `json:"billing_mode"`
			BillingExpr            string   `json:"billing_expr"`
			EnableGroups           []string `json:"enable_groups"`
			SupportedEndpointTypes []string `json:"supported_endpoint_types"`
		} `json:"data"`
		Vendors     []ModelCatalogVendor `json:"vendors"`
		GroupRatio  map[string]float64   `json:"group_ratio"`
		UsableGroup map[string]string    `json:"usable_group"`
	}
	if err := json.Unmarshal(response, &payload); err != nil || !payload.Success {
		return ModelCatalog{}, safeMessageError{message: "模型目录暂不可用"}
	}
	catalog := ModelCatalog{Platform: platform, UserGroup: self.Group, Groups: []ModelCatalogGroup{}, Vendors: payload.Vendors, Models: []ModelCatalogModel{}, CreditsPerUnit: billing.CreditsPerUnit}
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
	for _, item := range payload.Data {
		kind, listed := catalogModelKind(item.ModelName, item.SupportedEndpointTypes)
		if !listed {
			continue
		}
		groups := []string{}
		for _, group := range catalog.Groups {
			if slices.Contains(item.EnableGroups, group.Name) || slices.Contains(item.EnableGroups, "all") {
				groups = append(groups, group.Name)
			}
		}
		if len(groups) == 0 {
			continue
		}
		entry := ModelCatalogModel{Name: item.ModelName, VendorID: item.VendorID, Kind: kind, Groups: groups, QuotaType: item.QuotaType, ModelPrice: item.ModelPrice, ModelRatio: item.ModelRatio}
		// Expression-based billing cannot be priced per call; show it as usage-based.
		if item.BillingExpr != "" || item.BillingMode != "" && item.BillingMode != "ratio" {
			entry.QuotaType, entry.ModelPrice = 0, 0
		}
		catalog.Models = append(catalog.Models, entry)
	}
	catalogCache.set(user.ID, catalog, pricingCacheTTL)
	return catalog, nil
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
	for _, word := range []string{"tts", "whisper", "audio", "speech"} {
		if strings.Contains(lower, word) {
			return "audio", true
		}
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
