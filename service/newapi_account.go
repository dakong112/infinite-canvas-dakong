package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const newapiReauthMessage = "账户授权已过期，请退出后重新登录"

type AccountMember struct {
	Active    bool   `json:"active"`
	PlanID    int    `json:"planId"`
	PlanTitle string `json:"planTitle"`
	ExpiresAt int64  `json:"expiresAt"`
}

type AccountSummary struct {
	Credits     float64       `json:"credits"`
	UsedCredits float64       `json:"usedCredits"`
	Group       string        `json:"group"`
	CreditName  string        `json:"creditName"`
	Member      AccountMember `json:"member"`
}

type AccountPlan struct {
	ID              int     `json:"id"`
	Title           string  `json:"title"`
	Subtitle        string  `json:"subtitle"`
	PriceAmount     float64 `json:"priceAmount"`
	Currency        string  `json:"currency"`
	DurationUnit    string  `json:"durationUnit"`
	DurationValue   int     `json:"durationValue"`
	AllowBalancePay bool    `json:"allowBalancePay"`
}

var epayExcludedTypes = map[string]bool{"stripe": true, "creem": true, "waffo": true, "waffo_pancake": true}

type AccountTopupInfo struct {
	EnableOnlineTopup bool              `json:"enableOnlineTopup"`
	EnableRedemption  bool              `json:"enableRedemption"`
	PayMethods        []json.RawMessage `json:"payMethods"`
	MinTopup          float64           `json:"minTopup"`
	AmountOptions     []float64         `json:"amountOptions"`
	TopupLink         string            `json:"topupLink"`
}

type EpayOrder struct {
	URL    string            `json:"url"`
	Params map[string]string `json:"params"`
}

type ModelPrice struct {
	QuotaType  int     `json:"quotaType"`
	ModelPrice float64 `json:"modelPrice"`
}

type AccountPricing struct {
	GroupRatio     float64               `json:"groupRatio"`
	CreditsPerUnit float64               `json:"creditsPerUnit"`
	Models         map[string]ModelPrice `json:"models"`
}

type newapiEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// ttlCache is a tiny mutex-guarded cache.
// ponytail: in-process, so each instance keeps its own copy; use a shared cache if infinite runs multi-instance.
type ttlCache[T any] struct {
	mu sync.Mutex
	m  map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value   T
	expires time.Time
}

func (c *ttlCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.m[key]
	if !ok || time.Now().After(entry.expires) {
		var zero T
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) set(key string, value T, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]ttlEntry[T]{}
	}
	c.m[key] = ttlEntry[T]{value: value, expires: time.Now().Add(ttl)}
}

func (c *ttlCache[T]) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
}

// Caches keep us under new-api's per-IP rate limits.
var (
	summaryCache ttlCache[AccountSummary]
	plansCache   ttlCache[[]AccountPlan]
	pricingCache ttlCache[AccountPricing]
)

const (
	summaryCacheTTL = 15 * time.Second
	plansCacheTTL   = 5 * time.Minute
	pricingCacheTTL = 5 * time.Minute
	pricingMaxBytes = 8 << 20
	defaultMaxBytes = 1 << 20
)

const newapiGenericMessage = "操作失败，请稍后重试"

var newapiMessages = map[string]string{
	"Redemption failed, please try again later": "兑换失败，请确认兑换码有效且未被使用",
	"redeem.failed":                                                "兑换失败，请确认兑换码有效且未被使用",
	"invalid redemption code":                                      "兑换码无效",
	"redemption code already used":                                 "兑换码已被使用",
	"redemption code expired":                                      "兑换码已过期",
	"Insufficient balance":                                         "余额不足",
	"Subscription plan is not enabled":                             "该套餐已下架",
	"This plan does not allow balance redemption":                  "该套餐不支持余额购买",
	"Purchase limit for this plan has been reached":                "已达到该套餐的购买上限",
	"record not found":                                             "套餐不存在",
	"Payment method does not exist":                                "支付方式不存在",
	"Payment amount is too low":                                    "支付金额过低",
	"Payment information has not been configured by administrator": "管理员尚未配置支付信息",
	"Top-up is processing, please try again later":                 "充值处理中，请稍后再试",
	"Invalid parameters":                                           "参数无效",
	"Invalid top-up amount":                                        "充值金额无效",
	"Failed to start payment":                                      "发起支付失败",
	"Failed to create order":                                       "创建订单失败",
}

var newapiMessagePrefixes = map[string]string{
	"Top-up amount cannot be less than":      "充值金额低于最低限额",
	"Top-up amount cannot exceed":            "充值金额超过上限",
	"Payment, redemption, subscription, and": "支付功能暂未开放，请联系管理员",
}

// newapiMessage maps a new-api English message to Chinese; unknown ones are logged and replaced by a generic text.
func newapiMessage(message string) string {
	message = strings.TrimSpace(message)
	if zh, ok := newapiMessages[message]; ok {
		return zh
	}
	for prefix, zh := range newapiMessagePrefixes {
		if strings.HasPrefix(message, prefix) {
			return zh
		}
	}
	if message != "" {
		log.Printf("new-api message not mapped: %s", message)
	}
	return newapiGenericMessage
}

var quotaPerUnitCache struct {
	sync.Mutex
	value     float64
	expiresAt time.Time
}

// AccountSummaryForUser returns the caller's balance and membership from new-api.
func AccountSummaryForUser(userID string) (AccountSummary, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return AccountSummary{}, err
	}
	if cached, ok := summaryCache.get(user.ID); ok {
		return cached, nil
	}
	summary, err := loadAccountSummary(user)
	if err == nil {
		summaryCache.set(user.ID, summary, summaryCacheTTL)
	}
	return summary, err
}

func loadAccountSummary(user model.User) (AccountSummary, error) {
	var self struct {
		Quota     int64  `json:"quota"`
		UsedQuota int64  `json:"used_quota"`
		Group     string `json:"group"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/user/self", nil, &self); err != nil {
		return AccountSummary{}, err
	}
	billing, err := publicBillingSetting()
	if err != nil {
		return AccountSummary{}, err
	}
	perUnit, err := newapiQuotaPerUnit()
	if err != nil {
		return AccountSummary{}, err
	}
	member, err := activeMembership(user)
	if err != nil {
		return AccountSummary{}, err
	}
	return AccountSummary{
		Credits:     quotaToCredits(self.Quota, perUnit, billing.CreditsPerUnit),
		UsedCredits: quotaToCredits(self.UsedQuota, perUnit, billing.CreditsPerUnit),
		Group:       self.Group,
		CreditName:  billing.CreditName,
		Member:      member,
	}, nil
}

// AccountPlansForUser lists the subscription plans the user can buy.
func AccountPlansForUser(userID string) ([]AccountPlan, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return nil, err
	}
	return fetchPlans(user)
}

// SubscribeWithBalance buys a plan with the user's own balance.
func SubscribeWithBalance(userID string, planID int) error {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return err
	}
	if planID <= 0 {
		return safeMessageError{message: "请选择会员套餐"}
	}
	if err := newapiCall(user, http.MethodPost, "/api/subscription/balance/pay", map[string]int{"plan_id": planID}, nil); err != nil {
		return err
	}
	summaryCache.delete(user.ID)
	return nil
}

// RedeemTopupCode applies a redemption code to the user's balance.
func RedeemTopupCode(userID string, key string) error {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return safeMessageError{message: "请输入兑换码"}
	}
	if err := newapiCall(user, http.MethodPost, "/api/user/topup", map[string]string{"key": key}, nil); err != nil {
		return err
	}
	summaryCache.delete(user.ID)
	return nil
}

// AccountTopupInfoForUser returns the online top-up options enabled in new-api.
func AccountTopupInfoForUser(userID string) (AccountTopupInfo, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return AccountTopupInfo{}, err
	}
	var raw struct {
		EnableOnlineTopup bool              `json:"enable_online_topup"`
		EnableRedemption  bool              `json:"enable_redemption"`
		PayMethods        []json.RawMessage `json:"pay_methods"`
		MinTopup          float64           `json:"min_topup"`
		AmountOptions     []float64         `json:"amount_options"`
		TopupLink         string            `json:"topup_link"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/user/topup/info", nil, &raw); err != nil {
		return AccountTopupInfo{}, err
	}
	info := AccountTopupInfo{EnableOnlineTopup: raw.EnableOnlineTopup, EnableRedemption: raw.EnableRedemption, PayMethods: []json.RawMessage{},
		MinTopup: raw.MinTopup, AmountOptions: raw.AmountOptions, TopupLink: raw.TopupLink}
	// Only Epay-compatible methods can be started through /api/user/pay.
	for _, method := range raw.PayMethods {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(method, &kind) == nil && !epayExcludedTypes[kind.Type] {
			info.PayMethods = append(info.PayMethods, method)
		}
	}
	return info, nil
}

// CreateEpayOrder starts an Epay top-up; the browser must POST Params to URL.
func CreateEpayOrder(userID string, amount int64, method string) (EpayOrder, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return EpayOrder{}, err
	}
	if amount <= 0 || strings.TrimSpace(method) == "" {
		return EpayOrder{}, safeMessageError{message: "请选择充值金额和支付方式"}
	}
	response, err := newapiRequest(user, http.MethodPost, "/api/user/pay", map[string]any{"amount": amount, "payment_method": method})
	if err != nil {
		return EpayOrder{}, err
	}
	// /api/user/pay does not use the {success,data} envelope.
	var payload struct {
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
		URL     string          `json:"url"`
	}
	if err := json.Unmarshal(response, &payload); err != nil {
		return EpayOrder{}, safeMessageError{message: "支付服务返回异常"}
	}
	if payload.Message != "success" {
		var reason string
		_ = json.Unmarshal(payload.Data, &reason)
		if reason == "" {
			return EpayOrder{}, safeMessageError{message: "下单失败"}
		}
		return EpayOrder{}, safeMessageError{message: newapiMessage(reason)}
	}
	order := EpayOrder{URL: payload.URL}
	if err := json.Unmarshal(payload.Data, &order.Params); err != nil || !strings.HasPrefix(order.URL, "https://") && !strings.HasPrefix(order.URL, "http://") {
		return EpayOrder{}, safeMessageError{message: "支付服务返回异常"}
	}
	return order, nil
}

// AccountPricingForUser returns per-model prices seen through the user's group.
func AccountPricingForUser(userID string) (AccountPricing, error) {
	user, err := newapiLinkedUser(userID)
	if err != nil {
		return AccountPricing{}, err
	}
	var self struct {
		Group string `json:"group"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/user/self", nil, &self); err != nil {
		return AccountPricing{}, err
	}
	billing, err := publicBillingSetting()
	if err != nil {
		return AccountPricing{}, err
	}
	if cached, ok := pricingCache.get(self.Group); ok {
		cached.CreditsPerUnit = billing.CreditsPerUnit
		return cached, nil
	}
	response, err := newapiRequestLimit(user, http.MethodGet, "/api/pricing", nil, pricingMaxBytes)
	if err != nil {
		return AccountPricing{}, err
	}
	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			ModelName   string  `json:"model_name"`
			QuotaType   int     `json:"quota_type"`
			ModelPrice  float64 `json:"model_price"`
			BillingMode string  `json:"billing_mode"`
			BillingExpr string  `json:"billing_expr"`
		} `json:"data"`
		GroupRatio map[string]float64 `json:"group_ratio"`
	}
	if err := json.Unmarshal(response, &payload); err != nil || !payload.Success {
		return AccountPricing{}, safeMessageError{message: "价格信息暂不可用"}
	}
	// Keys in the "auto" group are billed by auto_groups, so this estimate uses the
	// user's own group ratio (an approximation).
	ratio, ok := payload.GroupRatio[self.Group]
	if !ok || ratio <= 0 {
		ratio = 1
	}
	pricing := AccountPricing{GroupRatio: ratio, CreditsPerUnit: billing.CreditsPerUnit, Models: make(map[string]ModelPrice, len(payload.Data))}
	for _, item := range payload.Data {
		price := ModelPrice{QuotaType: item.QuotaType, ModelPrice: item.ModelPrice}
		// Expression-based billing cannot be estimated per call; report it as usage-based.
		if item.BillingExpr != "" || item.BillingMode != "" && item.BillingMode != "ratio" {
			price = ModelPrice{}
		}
		pricing.Models[item.ModelName] = price
	}
	pricingCache.set(self.Group, pricing, pricingCacheTTL)
	return pricing, nil
}

func activeMembership(user model.User) (AccountMember, error) {
	var self struct {
		Subscriptions []struct {
			Subscription struct {
				PlanID  int   `json:"plan_id"`
				EndTime int64 `json:"end_time"`
			} `json:"subscription"`
		} `json:"subscriptions"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/subscription/self", nil, &self); err != nil {
		return AccountMember{}, err
	}
	member := AccountMember{}
	for _, item := range self.Subscriptions {
		if item.Subscription.EndTime > member.ExpiresAt {
			member = AccountMember{Active: true, PlanID: item.Subscription.PlanID, ExpiresAt: item.Subscription.EndTime}
		}
	}
	if !member.Active {
		return member, nil
	}
	plans, err := fetchPlans(user)
	if err != nil {
		return member, nil
	}
	for _, plan := range plans {
		if plan.ID == member.PlanID {
			member.PlanTitle = plan.Title
		}
	}
	return member, nil
}

func fetchPlans(user model.User) ([]AccountPlan, error) {
	if cached, ok := plansCache.get(""); ok {
		return cached, nil
	}
	var raw []struct {
		Plan struct {
			ID              int     `json:"id"`
			Title           string  `json:"title"`
			Subtitle        string  `json:"subtitle"`
			PriceAmount     float64 `json:"price_amount"`
			Currency        string  `json:"currency"`
			DurationUnit    string  `json:"duration_unit"`
			DurationValue   int     `json:"duration_value"`
			AllowBalancePay *bool   `json:"allow_balance_pay"`
		} `json:"plan"`
	}
	if err := newapiCall(user, http.MethodGet, "/api/subscription/plans", nil, &raw); err != nil {
		return nil, err
	}
	plans := make([]AccountPlan, 0, len(raw))
	for _, item := range raw {
		plans = append(plans, AccountPlan{
			ID:              item.Plan.ID,
			Title:           item.Plan.Title,
			Subtitle:        item.Plan.Subtitle,
			PriceAmount:     item.Plan.PriceAmount,
			Currency:        item.Plan.Currency,
			DurationUnit:    item.Plan.DurationUnit,
			DurationValue:   item.Plan.DurationValue,
			AllowBalancePay: item.Plan.AllowBalancePay == nil || *item.Plan.AllowBalancePay,
		})
	}
	plansCache.set("", plans, plansCacheTTL)
	return plans, nil
}

func newapiLinkedUser(userID string) (model.User, error) {
	if !NewapiSSOEnabled() {
		return model.User{}, safeMessageError{message: "未启用统一账号"}
	}
	user, ok, err := repository.GetUserByID(userID)
	if err != nil {
		return model.User{}, err
	}
	if !ok || user.NewapiID == "" {
		return model.User{}, safeMessageError{message: "当前账号未关联统一账号"}
	}
	if user.NewapiAccessToken == "" || user.NewapiTokenExpiresAt > 0 && user.NewapiTokenExpiresAt <= time.Now().Unix() {
		return model.User{}, safeMessageError{message: newapiReauthMessage}
	}
	return user, nil
}

// newapiCall performs an authenticated call and decodes the {success,message,data} envelope into out.
func newapiCall(user model.User, method string, path string, body any, out any) error {
	response, err := newapiRequest(user, method, path, body)
	if err != nil {
		return err
	}
	var envelope newapiEnvelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		return safeMessageError{message: "账户服务返回异常"}
	}
	if !envelope.Success {
		return safeMessageError{message: newapiMessage(envelope.Message)}
	}
	if out == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return safeMessageError{message: "账户服务返回异常"}
	}
	return nil
}

func newapiRequest(user model.User, method string, path string, body any) ([]byte, error) {
	return newapiRequestLimit(user, method, path, body, defaultMaxBytes)
}

func newapiRequestLimit(user model.User, method string, path string, body any, limit int64) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/")+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+user.NewapiAccessToken)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := newapiHTTPClient.Do(request)
	if err != nil {
		log.Printf("new-api %s %s failed: %v", method, path, err)
		return nil, safeMessageError{message: "账户服务暂时不可用"}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return nil, safeMessageError{message: "账户服务暂时不可用"}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, safeMessageError{message: "操作过于频繁，请稍后再试"}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden && newapiTokenDenied(data) {
		log.Printf("new-api %s %s rejected the user token: status=%d", method, path, response.StatusCode)
		return nil, safeMessageError{message: newapiReauthMessage}
	}
	if response.StatusCode >= http.StatusInternalServerError {
		log.Printf("new-api %s %s server error: status=%d", method, path, response.StatusCode)
		return nil, safeMessageError{message: "账户服务暂时不可用"}
	}
	return data, nil
}

// newapiTokenDenied reports whether a 403 body says the access token itself is the problem
// (ACCESS_TOKEN_SCOPE_DENIED, ACCESS_TOKEN_ROUTE_UNDECLARED, ...).
func newapiTokenDenied(body []byte) bool {
	var payload struct {
		Code string `json:"code"`
	}
	return json.Unmarshal(body, &payload) == nil && strings.HasPrefix(payload.Code, "ACCESS_TOKEN_")
}

func newapiQuotaPerUnit() (float64, error) {
	quotaPerUnitCache.Lock()
	cached, fresh := quotaPerUnitCache.value, time.Now().Before(quotaPerUnitCache.expiresAt)
	quotaPerUnitCache.Unlock()
	if cached > 0 && fresh {
		return cached, nil
	}
	value, err := fetchQuotaPerUnit()
	if err != nil {
		log.Printf("new-api status failed: %v", err)
		if cached > 0 {
			return cached, nil
		}
		return 0, safeMessageError{message: "账户服务暂时不可用"}
	}
	quotaPerUnitCache.Lock()
	quotaPerUnitCache.value = value
	quotaPerUnitCache.expiresAt = time.Now().Add(10 * time.Minute)
	quotaPerUnitCache.Unlock()
	return value, nil
}

func fetchQuotaPerUnit() (float64, error) {
	response, err := newapiHTTPClient.Get(strings.TrimRight(strings.TrimSpace(config.Cfg.NewapiBaseURL), "/") + "/api/status")
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", response.StatusCode)
	}
	var payload struct {
		Data struct {
			QuotaPerUnit float64 `json:"quota_per_unit"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, defaultMaxBytes)).Decode(&payload); err != nil || payload.Data.QuotaPerUnit <= 0 {
		return 0, fmt.Errorf("new-api status missing quota_per_unit")
	}
	return payload.Data.QuotaPerUnit, nil
}

func publicBillingSetting() (model.PublicBillingSetting, error) {
	settings, err := PublicSettings()
	if err != nil {
		return model.PublicBillingSetting{}, err
	}
	return settings.Billing, nil
}

func quotaToCredits(quota int64, perUnit float64, creditsPerUnit float64) float64 {
	return math.Round(float64(quota)/perUnit*creditsPerUnit*100) / 100
}
