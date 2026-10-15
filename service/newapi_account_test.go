package service

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const testPAT = "nap_test_token"

// callLog records requests from the fake server's goroutines.
type callLog struct {
	mu   sync.Mutex
	list []string
}

func (c *callLog) add(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, call)
}

func (c *callLog) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.list)
}

// fakeNewapiAccount serves the wallet endpoints used by the account service.
func fakeNewapiAccount(t *testing.T, calls *callLog) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.add(r.Method + " " + r.URL.Path)
		switch r.Header.Get("Authorization") {
		case "Bearer nap_scope":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"message":"The access token lacks the required permission: wallet:read","code":"ACCESS_TOKEN_SCOPE_DENIED"}`))
			return
		case "Bearer nap_forbid":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"message":"Unauthorized, insufficient privileges","code":"AUTH_INSUFFICIENT_PRIVILEGE"}`))
			return
		case "Bearer nap_notissued":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"message":"token not issued for this client"}`))
			return
		case "Bearer nap_badclient":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"message":"invalid client credentials"}`))
			return
		case "Bearer nap_admin":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"message":"role not allowed"}`))
			return
		case "Bearer nap_rate":
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.URL.Path != "/api/status" && r.URL.Path != "/api/sso/client" && r.Header.Get("Authorization") != "Bearer "+testPAT && r.Header.Get("Authorization") != "Bearer nap_unlimited" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		case "GET /api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1500000,"used_quota":500000,"group":"vip"}}`))
		case "GET /api/subscription/self":
			if r.Header.Get("Authorization") == "Bearer nap_unlimited" {
				_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[{"subscription":{"plan_id":4,"end_time":1900000000,"amount_total":0,"amount_used":123}}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[{"subscription":{"plan_id":3,"end_time":1900000000,"status":"active","amount_total":2500000,"amount_used":500000}}]}}`))
		case "GET /api/subscription/plans":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"plan":{"id":3,"title":"年度会员","subtitle":"全年畅享","price_amount":299,"currency":"CNY","duration_unit":"year","duration_value":1,"allow_balance_pay":true,"total_amount":2500000}},{"plan":{"id":4,"title":"无限会员","price_amount":999,"currency":"CNY","duration_unit":"month","duration_value":1,"total_amount":0}}]}`))
		case "POST /api/subscription/balance/pay":
			if string(body) != `{"plan_id":3}` {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"message":""}`))
		case "POST /api/user/topup":
			if string(body) == `{"key":"bad"}` {
				_, _ = w.Write([]byte(`{"success":false,"message":"Invalid redemption code"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":500000}`))
		case "GET /api/user/topup/info":
			_, _ = w.Write([]byte(`{"success":true,"data":{"enable_online_topup":true,"enable_redemption":true,"pay_methods":[{"name":"支付宝","type":"alipay"},{"name":"Stripe","type":"stripe"}],"min_topup":10,"amount_options":[10,50,100],"topup_link":""}}`))
		case "POST /api/user/pay":
			if strings.Contains(string(body), `"amount":999`) {
				_, _ = w.Write([]byte(`{"message":"error","data":"Payment method does not exist"}`))
				return
			}
			_, _ = w.Write([]byte(`{"message":"success","data":{"pid":"1","sign":"abc"},"url":"https://pay.example.com/submit.php"}`))
		case "POST /api/sso/ticket":
			if string(body) != `{"client_id":"infinite","client_secret":"x"}` {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"invalid client credentials"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"ticket":"tk+/1","expires_at":1900000000}}`))
		case "GET /api/pricing":
			_, _ = w.Write([]byte(`{"success":true,"data":[` +
				`{"model_name":"gpt-image-1","vendor_id":1,"quota_type":1,"model_price":0.2,"enable_groups":["default","vip"],"supported_endpoint_types":["image-generation","openai"]},` +
				`{"model_name":"gpt-4o","vendor_id":1,"quota_type":0,"model_ratio":2.5,"completion_ratio":4,"enable_groups":["default"],"supported_endpoint_types":["openai"]},` +
				`{"model_name":"tiered","quota_type":1,"model_price":0.5,"billing_mode":"tiered_expr","billing_expr":"p*2","enable_groups":["vip"],"supported_endpoint_types":["openai"]},` +
				`{"model_name":"text-embedding-3-small","quota_type":0,"model_ratio":0.01,"enable_groups":["default"],"supported_endpoint_types":["embeddings"]}],` +
				`"vendors":[{"id":1,"name":"OpenAI","icon":"OpenAI"}],"group_ratio":{"default":1,"vip":0.65},"usable_group":{"default":"Default group","vip":"VIP group"}}`))
		case "GET /api/sso/client":
			if id, secret, _ := r.BasicAuth(); id != "infinite" || secret != "x" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"invalid client credentials"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"name":"紫紫 AI","defaults":{"group":"default","image":"gpt-image-1","text":"gpt-4o","video":"","audio":""}}}`))
		case "POST /api/sso/key":
			if !strings.Contains(string(body), `"client_id":"infinite"`) || !strings.Contains(string(body), `"client_secret":"x"`) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"invalid client credentials"}`))
				return
			}
			if !strings.Contains(string(body), `"group":"default"`) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"success":false,"message":"group not available"}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"key":"sk-default-%d"}}`, calls.count())
		case "POST /api/sso/consume":
			fakeConsume.mu.Lock()
			defer fakeConsume.mu.Unlock()
			fakeConsume.bodies = append(fakeConsume.bodies, string(body))
			if fakeConsume.failures > 0 {
				fakeConsume.failures--
				w.WriteHeader(cmp.Or(fakeConsume.status, http.StatusInternalServerError))
				_, _ = w.Write([]byte(`{"success":false,"message":"refused"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":1,"balance":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNewapiAccountService(t *testing.T) {
	const marker = "NEWAPI_ACCOUNT_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNewapiAccountService$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated account test: %v\n%s", err, output)
		}
		return
	}
	calls := &callLog{}
	server := fakeNewapiAccount(t, calls)
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		NewapiBaseURL: server.URL, NewapiPublicURL: "https://account.example.com/", NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	future := time.Now().Add(time.Hour).Unix()
	if err := db.Create(&model.User{ID: "u1", Username: "u1", AffCode: "A1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "42", NewapiAccessToken: testPAT, NewapiTokenExpiresAt: future}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: "u2", Username: "u2", AffCode: "A2", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "43", NewapiAccessToken: testPAT, NewapiTokenExpiresAt: time.Now().Add(-time.Hour).Unix()}).Error; err != nil {
		t.Fatal(err)
	}
	for id, pat := range map[string]string{"u3": "nap_scope", "u4": "nap_forbid", "u5": "nap_rate", "u6": "nap_bad", "u7": "nap_unlimited", "u8": "nap_notissued", "u9": "nap_badclient", "u10": "nap_admin"} {
		if err := db.Create(&model.User{ID: id, Username: id, AffCode: "A" + id, Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: id, NewapiAccessToken: pat, NewapiTokenExpiresAt: future}).Error; err != nil {
			t.Fatal(err)
		}
	}
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Public.Billing.CreditsPerUnit = 10
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	summary, err := AccountSummaryForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	// 1,500,000 quota / 500,000 per unit * 10 credits per unit = 30 credits.
	if summary.Credits != 30 || summary.UsedCredits != 10 || summary.Group != "vip" || summary.CreditName != "积分" {
		t.Fatalf("summary: %#v", summary)
	}
	if !summary.Member.Active || summary.Member.PlanID != 3 || summary.Member.PlanTitle != "年度会员" || summary.Member.ExpiresAt != 1900000000 {
		t.Fatalf("member: %#v", summary.Member)
	}

	plans, err := AccountPlansForUser("u1")
	if err != nil || len(plans) != 2 || plans[0].Title != "年度会员" || !plans[0].AllowBalancePay {
		t.Fatalf("plans: %#v %v", plans, err)
	}
	// 2,500,000 quota / 500,000 per unit * 10 = 50 credits; total_amount 0 means unlimited.
	if plans[0].IncludedCredits != 50 || plans[0].Unlimited || !plans[1].Unlimited || plans[1].IncludedCredits != 0 {
		t.Fatalf("plan credits: %#v", plans)
	}
	if summary.Member.RemainingCredits != 40 || summary.Member.Unlimited {
		t.Fatalf("member credits: %#v", summary.Member)
	}
	unlimited, err := AccountSummaryForUser("u7")
	if err != nil || !unlimited.Member.Unlimited || unlimited.Member.PlanTitle != "无限会员" {
		t.Fatalf("unlimited member: %#v %v", unlimited.Member, err)
	}
	if err := SubscribeWithBalance("u1", 3); err != nil {
		t.Fatal(err)
	}
	if err := RedeemTopupCode("u1", "good"); err != nil {
		t.Fatal(err)
	}
	if err := RedeemTopupCode("u1", "bad"); err == nil {
		t.Fatal("expected redeem failure")
	}
	info, err := AccountTopupInfoForUser("u1")
	if err != nil || !info.EnableOnlineTopup || !info.EnableRedemption || len(info.AmountOptions) != 3 || len(info.PayMethods) != 1 {
		t.Fatalf("topup info: %#v %v", info, err)
	}
	order, err := CreateEpayOrder("u1", 50, "alipay")
	if err != nil || order.URL != "https://pay.example.com/submit.php" || order.Params["sign"] != "abc" {
		t.Fatalf("epay: %#v %v", order, err)
	}
	pricing, err := AccountPricingForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	image := pricing.Models["gpt-image-1"]
	if pricing.GroupRatio != 0.65 || pricing.CreditsPerUnit != 10 || image.QuotaType != 1 || image.ModelPrice != 0.2 {
		t.Fatalf("pricing: %#v", pricing)
	}

	if tiered := pricing.Models["tiered"]; tiered.QuotaType != 0 || tiered.ModelPrice != 0 {
		t.Fatalf("expression-billed model must be usage-based: %#v", tiered)
	}

	// Error mapping.
	if _, err := AccountSummaryForUser("u6"); err == nil || err.Error() != newapiReauthMessage {
		t.Fatalf("401 should ask to sign in again, got %v", err)
	}
	if _, err := AccountSummaryForUser("u3"); err == nil || err.Error() != newapiReauthMessage {
		t.Fatalf("token scope denial should ask to sign in again, got %v", err)
	}
	if _, err := AccountSummaryForUser("u4"); err == nil || err.Error() == newapiReauthMessage {
		t.Fatalf("non-token 403 must not ask to sign in again, got %v", err)
	}
	if _, err := AccountSummaryForUser("u5"); err == nil || !strings.Contains(err.Error(), "频繁") {
		t.Fatalf("429 should be rate-limit message, got %v", err)
	}
	if _, err := CreateEpayOrder("u1", 999, "alipay"); err == nil || err.Error() != "支付方式不存在" {
		t.Fatalf("epay error should be translated, got %v", err)
	}
	if err := RedeemTopupCode("u1", "bad"); err == nil || err.Error() != "操作失败，请稍后重试" && !strings.Contains(err.Error(), "兑换") {
		t.Fatalf("redeem error: %v", err)
	}

	// Login ticket: URL built from the public base, `to` limited to the whitelist.
	loginURL, err := CreateNewapiLoginTicket("u1", "/wallet")
	if err != nil || loginURL != "https://account.example.com/sso/ticket#ticket=tk%2B%2F1&to=%2Fwallet" {
		t.Fatalf("login ticket: %q %v", loginURL, err)
	}
	if loginURL, err = CreateNewapiLoginTicket("u1", "https://evil.example.com"); err != nil || !strings.HasSuffix(loginURL, "&to=%2F") {
		t.Fatalf("non-whitelisted target must fall back to /: %q %v", loginURL, err)
	}
	for _, id := range []string{"u3", "u8"} {
		if _, err := CreateNewapiLoginTicket(id, "/"); err == nil || err.Error() != newapiReauthMessage {
			t.Fatalf("%s: rejected token should ask to sign in again, got %v", id, err)
		}
	}

	if _, err := CreateNewapiLoginTicket("u9", "/"); err == nil || err.Error() != "账户服务配置异常，请联系管理员" {
		t.Fatalf("bad client credentials must not ask the user to sign in again, got %v", err)
	}
	if _, err := CreateNewapiLoginTicket("u10", "/"); err == nil || err.Error() != "管理员请直接登录账户中心" {
		t.Fatalf("admin refusal message, got %v", err)
	}

	// Summary cache: a second read makes no new-api calls; a purchase invalidates it.
	if _, err := AccountSummaryForUser("u1"); err != nil {
		t.Fatal(err)
	}
	cached := calls.count()
	if _, err := AccountSummaryForUser("u1"); err != nil || calls.count() != cached {
		t.Fatalf("summary should be served from cache (calls %d -> %d, err %v)", cached, calls.count(), err)
	}
	if err := SubscribeWithBalance("u1", 3); err != nil {
		t.Fatal(err)
	}
	afterPay := calls.count()
	if _, err := AccountSummaryForUser("u1"); err != nil || calls.count() == afterPay {
		t.Fatalf("purchase should invalidate the summary cache (calls %d -> %d, err %v)", afterPay, calls.count(), err)
	}

	// Expired PAT never reaches new-api and asks the user to sign in again.
	before := calls.count()
	if _, err := AccountSummaryForUser("u2"); err == nil || err.Error() != newapiReauthMessage {
		t.Fatalf("expected reauth error, got %v", err)
	}
	if calls.count() != before {
		t.Fatalf("expired token must not call new-api, calls: %v", calls.list[before:])
	}
}

func TestCatalogModelKind(t *testing.T) {
	cases := []struct {
		name      string
		endpoints []string
		kind      string
		listed    bool
	}{
		{"gpt-image-1", []string{"image-generation", "openai"}, "image", true},
		{"sora-2", []string{"openai-video"}, "video", true},
		{"text-embedding-3-small", []string{"embeddings"}, "", false},
		{"jina-reranker-v2", []string{"jina-rerank"}, "", false},
		{"gpt-4o-mini-tts", []string{"openai"}, "audio", true},
		{"whisper-1", []string{"openai"}, "audio", true},
		{"gpt-4o-realtime-speech", []string{"openai"}, "audio", true},
		// Chat models with audio I/O are text; the web name rules would call them audio.
		{"gpt-4o-audio-preview", []string{"openai"}, "text", true},
		// new-api reports "openai" for most channels, so video/text on OpenAI-compatible
		// channels stay unknown and the web decides by its name rules.
		{"kling-v2-master", []string{"openai"}, "", true},
		{"deepseek-chat", []string{"openai"}, "", true},
	}
	for _, tc := range cases {
		kind, listed := catalogModelKind(tc.name, tc.endpoints)
		if kind != tc.kind || listed != tc.listed {
			t.Errorf("%s %v: got (%q, %v), want (%q, %v)", tc.name, tc.endpoints, kind, listed, tc.kind, tc.listed)
		}
	}
}

func TestNewapiModelCatalog(t *testing.T) {
	const marker = "NEWAPI_CATALOG_CHILD"
	if os.Getenv(marker) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNewapiModelCatalog$", "-test.timeout=40s")
		cmd.Env = append(os.Environ(), marker+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated catalog test: %v\n%s", err, output)
		}
		return
	}
	calls := &callLog{}
	server := fakeNewapiAccount(t, calls)
	config.Cfg = config.Config{StorageDriver: "sqlite", DatabaseDSN: ":memory:", AILogDir: t.TempDir(), JWTSecret: "s", JWTExpireHours: 1,
		NewapiBaseURL: server.URL, NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
	if err := model.SetSecretKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	db, err := repository.DB()
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	connection.SetMaxOpenConns(1)
	future := time.Now().Add(time.Hour).Unix()
	if err := db.Create(&model.User{ID: "g1", Username: "g1", AffCode: "G1", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "50",
		NewapiAccessToken: testPAT, NewapiAPIKey: "sk-login", NewapiTokenExpiresAt: future}).Error; err != nil {
		t.Fatal(err)
	}
	settings, err := repository.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Public.Billing.CreditsPerUnit = 10
	// A non-OpenAI channel at the same address comes first; the OpenAI one is the platform channel.
	settings.Private.Channels = []model.ModelChannel{
		{ID: "na-gemini", Name: "new-api gemini", Protocol: "gemini", BaseURL: server.URL, APIKey: "sk-admin", Enabled: true, Weight: 1},
		{ID: "na", Name: "new-api", Protocol: "openai", BaseURL: server.URL, APIKey: "sk-admin", Enabled: true, Weight: 1},
	}
	if _, err := repository.SaveSettings(settings, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	catalog, err := ModelCatalogForUser("g1")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Platform.Name != "紫紫 AI" || catalog.Platform.Defaults.Image != "gpt-image-1" || catalog.UserGroup != "vip" || catalog.CreditsPerUnit != 10 || catalog.QuotaPerUnit != 500000 {
		t.Fatalf("catalog header: %#v", catalog)
	}
	// Sorted by ratio: the discounted group first; descriptions come from usable_group.
	if len(catalog.Groups) != 2 || catalog.Groups[0] != (ModelCatalogGroup{Name: "vip", Desc: "VIP group", Ratio: 0.65}) || catalog.Groups[1].Name != "default" {
		t.Fatalf("groups: %#v", catalog.Groups)
	}
	if len(catalog.Vendors) != 1 || catalog.Vendors[0].Name != "OpenAI" {
		t.Fatalf("vendors: %#v", catalog.Vendors)
	}
	models := map[string]ModelCatalogModel{}
	for _, item := range catalog.Models {
		models[item.Name] = item
	}
	if _, ok := models["text-embedding-3-small"]; ok {
		t.Fatal("embedding models must not be listed")
	}
	if image := models["gpt-image-1"]; image.Kind != "image" || image.VendorID != 1 || !slices.Equal(image.Groups, []string{"vip", "default"}) || image.QuotaType != 1 || image.ModelPrice != 0.2 {
		t.Fatalf("gpt-image-1: %#v", image)
	}
	if chat := models["gpt-4o"]; chat.Kind != "" || !slices.Equal(chat.Groups, []string{"default"}) || chat.ModelRatio != 2.5 || chat.CompletionRatio != 4 {
		t.Fatalf("gpt-4o: %#v", chat)
	}
	if tiered := models["tiered"]; tiered.QuotaType != 0 || tiered.ModelPrice != 0 {
		t.Fatalf("expression-billed model must be usage-based: %#v", tiered)
	}
	before := calls.count()
	if _, err := ModelCatalogForUser("g1"); err != nil || calls.count() != before {
		t.Fatalf("catalog should be cached (calls %d -> %d, err %v)", before, calls.count(), err)
	}

	keyCalls := func() int {
		calls.mu.Lock()
		defer calls.mu.Unlock()
		count := 0
		for _, call := range calls.list {
			if call == "POST /api/sso/key" {
				count++
			}
		}
		return count
	}

	// The account group bills with the login key; no group key is requested.
	channel, err := SelectModelChannelForUser("g1", "gpt-image-1", "newapi@vip", true)
	if err != nil || channel.ID != "newapi@vip" || channel.Protocol != "openai" || channel.APIKey != "sk-login" || channel.BaseURL != server.URL || keyCalls() != 0 {
		t.Fatalf("account group channel: %#v %v (key calls %d)", channel, err, keyCalls())
	}
	// Another group: one key from new-api, stored encrypted and reused.
	channel, err = SelectModelChannelForUser("g1", "gpt-4o", "newapi@default", true)
	if err != nil || channel.ID != "newapi@default" || !strings.HasPrefix(channel.APIKey, "sk-default-") {
		t.Fatalf("group channel: %#v %v", channel, err)
	}
	first := channel.APIKey
	if again, err := SelectModelChannelForUser("g1", "gpt-4o", "newapi@default", false); err != nil || again.APIKey != first || keyCalls() != 1 {
		t.Fatalf("stored key must be reused: %#v %v (key calls %d)", again, err, keyCalls())
	}
	var stored string
	if err := db.Raw("SELECT api_key FROM newapi_group_keys WHERE user_id = ? AND group_name = ?", "g1", "default").Scan(&stored).Error; err != nil || !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("group key must be encrypted at rest, got %q %v", stored, err)
	}
	// Model and group checks use the catalog.
	if _, err := SelectModelChannelForUser("g1", "tiered", "newapi@default", true); err == nil || err.Error() != "该模型在当前分组不可用" {
		t.Fatalf("model outside the group: %v", err)
	}
	if _, err := SelectModelChannelForUser("g1", "gpt-4o", "newapi@svip", true); err == nil || err.Error() != "该分组不可用，请换一个分组" {
		t.Fatalf("group outside the catalog: %v", err)
	}
	// A rejected group key is replaced once; the login key is never renewed here.
	renewed, ok, err := RenewNewapiGroupChannel("g1", "gpt-4o", channel)
	if err != nil || !ok || renewed.APIKey == first || keyCalls() != 2 {
		t.Fatalf("renew: %#v %v %v (key calls %d)", renewed, ok, err, keyCalls())
	}
	if _, ok, err := RenewNewapiGroupChannel("g1", "gpt-image-1", model.ModelChannel{ID: "newapi@vip", BaseURL: server.URL}); ok || err != nil {
		t.Fatalf("account group must not be renewed: %v %v", ok, err)
	}
	if _, ok, err := RenewNewapiGroupChannel("g1", "gpt-4o", model.ModelChannel{ID: "na", BaseURL: server.URL}); ok || err != nil {
		t.Fatalf("ordinary channels are never renewed: %v %v", ok, err)
	}
	// A user's own channel that merely reuses the id never gets the group key.
	if _, ok, err := RenewNewapiGroupChannel("g1", "gpt-4o", model.ModelChannel{ID: "newapi@default", BaseURL: "https://third-party.example.com"}); ok || err != nil || keyCalls() != 2 {
		t.Fatalf("non-new-api channels are never renewed: %v %v (key calls %d)", ok, err, keyCalls())
	}
	// Nor one on the new-api origin that is not the platform channel.
	if _, ok, err := RenewNewapiGroupChannel("g1", "gpt-4o", model.ModelChannel{ID: "newapi@default", BaseURL: server.URL + "/user"}); ok || err != nil || keyCalls() != 2 {
		t.Fatalf("non-platform channels are never renewed: %v %v (key calls %d)", ok, err, keyCalls())
	}
	// Reads of created tasks skip the catalog: a model since removed from the group still polls with the stored key...
	if read, err := SelectModelChannelForUser("g1", "tiered", "newapi@default", false); err != nil || read.APIKey != renewed.APIKey {
		t.Fatalf("read after model left the group: %#v %v", read, err)
	}
	// ...and so does a user whose catalog cannot be fetched (login key for groups without a stored key).
	if err := db.Create(&model.User{ID: "g2", Username: "g2", AffCode: "G2", Role: model.UserRoleUser, Status: model.UserStatusActive, NewapiID: "51",
		NewapiAccessToken: "nap_rate", NewapiAPIKey: "sk-login-2", NewapiTokenExpiresAt: future}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := SelectModelChannelForUser("g2", "gpt-4o", "newapi@vip", true); err == nil {
		t.Fatal("creating needs the catalog")
	}
	if read, err := SelectModelChannelForUser("g2", "gpt-4o", "newapi@vip", false); err != nil || read.APIKey != "sk-login-2" || read.BaseURL != server.URL {
		t.Fatalf("read without catalog: %#v %v", read, err)
	}
}
