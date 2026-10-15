package service

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tigerowo/infinite-canvas/model"
)

func TestFetchAdminChannelModelsParsesOpenAIModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"},{"id":""}]}`))
	}))
	defer server.Close()

	models, err := fetchAdminChannelModels(model.ModelChannel{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("fetchAdminChannelModels returned error: %v", err)
	}
	if want := []string{"a-model", "z-model"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

func TestFetchAdminChannelModelsReportsArkPlanModelsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plan/v3/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := fetchAdminChannelModels(model.ModelChannel{
		Protocol: "ark",
		BaseURL:  server.URL + "/api/plan/v3/contents/generations/tasks",
		APIKey:   "test-key",
	})
	if err == nil {
		t.Fatal("expected unsupported /models error")
	}
	if !strings.Contains(err.Error(), "Agent Plan 未提供 OpenAI /models") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildModelChannelURLNormalizesArkPlanTaskPath(t *testing.T) {
	got := BuildModelChannelURL(model.ModelChannel{BaseURL: "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks?debug=1"}, "/models")
	want := "https://ark.cn-beijing.volces.com/api/plan/v3/models"
	if got != want {
		t.Fatalf("BuildModelChannelURL = %q, want %q", got, want)
	}
}

func TestBuildModelChannelURLZhipuV4(t *testing.T) {
	tests := []struct {
		baseURL string
		path    string
		want    string
	}{
		{"https://open.bigmodel.cn/api/paas/v4", "/chat/completions", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4/", "/models", "https://open.bigmodel.cn/api/paas/v4/models"},
		{"https://open.bigmodel.cn/api/paas/v4", "/images/generations", "https://open.bigmodel.cn/api/paas/v4/images/generations"},
		{"https://ark.cn-beijing.volces.com/api/plan/v3", "/chat/completions", "https://ark.cn-beijing.volces.com/api/plan/v3/chat/completions"},
		{"https://api.openai.com", "/chat/completions", "https://api.openai.com/v1/chat/completions"},
	}
	for _, tt := range tests {
		got := BuildModelChannelURL(model.ModelChannel{BaseURL: tt.baseURL}, tt.path)
		if got != tt.want {
			t.Fatalf("BuildModelChannelURL(%q, %q) = %q, want %q", tt.baseURL, tt.path, got, tt.want)
		}
	}
}

func TestNormalizePublicBillingAndAnnouncement(t *testing.T) {
	setting := normalizePublicSettingWithChannels(model.PublicSetting{
		Announcement: model.PublicAnnouncementSetting{Enabled: true, Text: "  限时优惠 ", Link: "javascript:alert(1)", EndAt: "not-a-time"},
	}, nil)
	if setting.Billing.CreditsPerUnit != 1 || setting.Billing.CreditName != "积分" {
		t.Fatalf("billing defaults: %#v", setting.Billing)
	}
	if setting.Announcement.Text != "限时优惠" || setting.Announcement.Link != "" || setting.Announcement.EndAt != "" || !setting.Announcement.Enabled {
		t.Fatalf("announcement normalize: %#v", setting.Announcement)
	}
	for _, link := range []string{"//evil.com", "/\\evil.com"} {
		got := normalizePublicSettingWithChannels(model.PublicSetting{Announcement: model.PublicAnnouncementSetting{Text: "x", Link: link}}, nil)
		if got.Announcement.Link != "" {
			t.Fatalf("link %q must be rejected, got %q", link, got.Announcement.Link)
		}
	}
	empty := normalizePublicSettingWithChannels(model.PublicSetting{Announcement: model.PublicAnnouncementSetting{Enabled: true}}, nil)
	if empty.Announcement.Enabled {
		t.Fatal("announcement without text must be disabled")
	}
}

func TestNormalizePublicWorksSetting(t *testing.T) {
	defaults := normalizePublicSettingWithChannels(model.PublicSetting{}, nil).Works
	want := model.PublicWorksSetting{RetentionDays: 7, MemberRetentionDays: 90, UserConcurrency: 2, MemberConcurrency: 4, GlobalConcurrency: 8}
	if defaults != want {
		t.Fatalf("works defaults = %#v, want %#v", defaults, want)
	}
	capped := normalizePublicSettingWithChannels(model.PublicSetting{Works: model.PublicWorksSetting{
		RetentionDays: 99999, MemberRetentionDays: 30, UserConcurrency: 1, MemberConcurrency: 3, GlobalConcurrency: 1000,
	}}, nil).Works
	if capped.RetentionDays != 3650 || capped.MemberRetentionDays != 30 || capped.UserConcurrency != 1 || capped.MemberConcurrency != 3 || capped.GlobalConcurrency != 64 {
		t.Fatalf("works caps = %#v", capped)
	}
}

func TestWorksConcurrencyCappedByGlobal(t *testing.T) {
	works := normalizePublicSettingWithChannels(model.PublicSetting{Works: model.PublicWorksSetting{UserConcurrency: 10, MemberConcurrency: 20, GlobalConcurrency: 4}}, nil).Works
	if works.UserConcurrency != 4 || works.MemberConcurrency != 4 || works.GlobalConcurrency != 4 {
		t.Fatalf("concurrency not capped by global: %#v", works)
	}
}

func TestNormalizePublicEcommerceSetting(t *testing.T) {
	channels := []model.ModelChannel{{Name: "c", BaseURL: "https://api.example.com", APIKey: "k", Models: []string{"gpt-4o-mini", "gpt-image-1"}, Enabled: true}}
	setting := normalizePublicSettingWithChannels(model.PublicSetting{
		ModelChannel: model.PublicModelChannelSetting{AvailableModels: []string{"gpt-4o-mini", "gpt-image-1"}, DefaultTextModel: "gpt-4o-mini", DefaultImageModel: "gpt-image-1"},
		Ecommerce:    model.PublicEcommerceSetting{PreviewQuality: "ultra"},
	}, channels)
	want := model.PublicEcommerceSetting{VisionModel: "gpt-4o-mini", ImageModel: "gpt-image-1", PreviewQuality: "low", FinalQuality: "high"}
	if setting.Ecommerce != want {
		t.Fatalf("ecommerce defaults = %#v, want %#v", setting.Ecommerce, want)
	}
	explicit := normalizePublicSettingWithChannels(model.PublicSetting{Ecommerce: model.PublicEcommerceSetting{VisionModel: " v ", ImageModel: "i", PreviewQuality: "medium", FinalQuality: "auto"}}, nil)
	if explicit.Ecommerce != (model.PublicEcommerceSetting{VisionModel: "v", ImageModel: "i", PreviewQuality: "medium", FinalQuality: "auto"}) {
		t.Fatalf("explicit ecommerce = %#v", explicit.Ecommerce)
	}
}

func TestNormalizeImageExamples(t *testing.T) {
	items := []model.ImageExampleItem{
		{Label: " " + strings.Repeat("长", 45) + " ", URL: "/api/files/a", Before: "javascript:alert(1)"},
		{Label: "x", URL: "https://cdn.example.com/b.png", Before: "http://cdn.example.com/a.png"},
		{Label: "script", URL: "javascript:alert(1)"},
		{Label: "relative", URL: "/etc/passwd"},
		{Label: "hostless", URL: "https:///a.png"},
		{Label: "empty"},
	}
	for range 20 {
		items = append(items, model.ImageExampleItem{Label: "more", URL: "/api/files/m"})
	}
	got := normalizePublicSettingWithChannels(model.PublicSetting{ImageExamples: map[string]model.ImageToolExamples{
		"remove-bg": {Cover: "data:image/png;base64,AA", Items: items},
		"free":      {Cover: " /api/files/c "},
		"unknown":   {Cover: "/api/files/c"},
	}}, nil).ImageExamples
	if _, ok := got["unknown"]; ok || len(got) != 2 {
		t.Fatalf("tools = %#v", got)
	}
	if got["free"].Cover != "/api/files/c" || got["free"].Items == nil {
		t.Fatalf("free = %#v", got["free"])
	}
	removeBg := got["remove-bg"]
	if removeBg.Cover != "" || len(removeBg.Items) != 12 {
		t.Fatalf("remove-bg cover %q, %d items", removeBg.Cover, len(removeBg.Items))
	}
	if first := removeBg.Items[0]; first.Label != strings.Repeat("长", 40) || first.URL != "/api/files/a" || first.Before != "" {
		t.Fatalf("first item = %#v", first)
	}
	if second := removeBg.Items[1]; second.Before != "http://cdn.example.com/a.png" || removeBg.Items[2].Label != "more" {
		t.Fatalf("items = %#v", removeBg.Items[:3])
	}
}
