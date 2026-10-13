package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
		case "Bearer nap_rate":
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.URL.Path != "/api/status" && r.Header.Get("Authorization") != "Bearer "+testPAT {
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
			_, _ = w.Write([]byte(`{"success":true,"data":{"subscriptions":[{"subscription":{"plan_id":3,"end_time":1900000000,"status":"active"}}]}}`))
		case "GET /api/subscription/plans":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"plan":{"id":3,"title":"年度会员","subtitle":"全年畅享","price_amount":299,"currency":"CNY","duration_unit":"year","duration_value":1,"allow_balance_pay":true}}]}`))
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
		case "GET /api/pricing":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"model_name":"gpt-image-1","quota_type":1,"model_price":0.2},{"model_name":"gpt-4o","quota_type":0,"model_ratio":2.5},{"model_name":"tiered","quota_type":1,"model_price":0.5,"billing_mode":"tiered_expr","billing_expr":"p*2"}],"group_ratio":{"default":1,"vip":0.65}}`))
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
		NewapiBaseURL: server.URL, NewapiClientID: "infinite", NewapiClientSecret: "x", NewapiRedirectURI: "https://www.example.com/sso/callback"}
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
	for id, pat := range map[string]string{"u3": "nap_scope", "u4": "nap_forbid", "u5": "nap_rate", "u6": "nap_bad"} {
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
	if err != nil || len(plans) != 1 || plans[0].Title != "年度会员" || !plans[0].AllowBalancePay {
		t.Fatalf("plans: %#v %v", plans, err)
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
