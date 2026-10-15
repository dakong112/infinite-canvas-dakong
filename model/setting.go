package model

import "encoding/json"

type SettingKey string

const (
	SettingKeyPublic                 SettingKey = "public"
	SettingKeyPrivate                SettingKey = "private"
	SettingKeyAgentSkillsInitialized SettingKey = "agent-skills-initialized"

	StorageProviderTypeS3     = "s3"
	StorageProviderTypeWebDAV = "webdav"
	StorageProviderTypeLocal  = "local"
)

// ModelChannel 模型渠道配置。
type ModelChannel struct {
	ID           string          `json:"id"`
	Protocol     string          `json:"protocol"`
	Name         string          `json:"name"`
	BaseURL      string          `json:"baseUrl"`
	APIKey       string          `json:"apiKey"`
	Models       []string        `json:"models"`
	Weight       int             `json:"weight"`
	Timeout      int             `json:"timeout"`
	Enabled      bool            `json:"enabled"`
	Remark       string          `json:"remark"`
	UploadAPIKey string          `json:"uploadApiKey,omitempty"`
	BridgeID     string          `json:"bridgeId,omitempty"`
	ComfyURL     string          `json:"comfyUrl,omitempty"`
	WorkflowDir  string          `json:"workflowDir,omitempty"`
	Workflows    []WorkflowEntry `json:"workflows,omitempty"`

	ModelCapabilities    map[string]string `json:"modelCapabilities,omitempty"`
	ParameterTranslation string            `json:"parameterTranslation,omitempty"`
}

type WorkflowFieldMapping struct {
	ID                 string `json:"id,omitempty"`
	NodeID             string `json:"nodeId"`
	ClassType          string `json:"classType,omitempty"`
	FieldName          string `json:"fieldName"`
	FieldType          string `json:"fieldType,omitempty"`
	Label              string `json:"label,omitempty"`
	Role               string `json:"role,omitempty"`
	SafeToOverride     *bool  `json:"safeToOverride,omitempty"`
	OptionsSource      string `json:"optionsSource,omitempty"`
	Source             string `json:"source,omitempty"`
	SourceIndex        int    `json:"sourceIndex,omitempty"`
	ImageOrder         int    `json:"imageOrder,omitempty"`
	FieldValue         any    `json:"fieldValue,omitempty"`
	Value              any    `json:"value,omitempty"`
	Default            any    `json:"default,omitempty"`
	DefaultValue       any    `json:"defaultValue,omitempty"`
	Enabled            *bool  `json:"enabled,omitempty"`
	Required           bool   `json:"required,omitempty"`
	RandomEnabled      bool   `json:"randomEnabled,omitempty"`
	BindPrompt         bool   `json:"bindPrompt,omitempty"`
	SourceFromUpstream bool   `json:"sourceFromUpstream,omitempty"`
	SourceAutomatic    *bool  `json:"sourceAutomatic,omitempty"`
	Options            []any  `json:"options,omitempty"`
	Min                any    `json:"min,omitempty"`
	Max                any    `json:"max,omitempty"`
	Step               any    `json:"step,omitempty"`
}

type WorkflowEntry struct {
	Provider      string                 `json:"provider"`
	Kind          string                 `json:"kind"`
	WorkflowID    string                 `json:"workflowId"`
	Title         string                 `json:"title"`
	Capability    string                 `json:"capability"`
	Enabled       bool                   `json:"enabled"`
	Fields        []WorkflowFieldMapping `json:"fields"`
	WorkflowJSON  map[string]any         `json:"workflowJson,omitempty"`
	WorkflowGraph map[string]any         `json:"workflowGraph,omitempty"`
}

type WorkflowSummary struct {
	Provider   string `json:"provider"`
	Kind       string `json:"kind"`
	WorkflowID string `json:"workflowId"`
	Title      string `json:"title"`
	Capability string `json:"capability"`
	Enabled    bool   `json:"enabled"`
}

// ModelCost 模型算力点配置。
type ModelCost struct {
	Model   string  `json:"model"`
	Credits float64 `json:"credits"`
}

// PublicModelChannelSetting 公开模型渠道配置。
type PublicModelChannelSetting struct {
	AvailableModels        []string                 `json:"availableModels"`
	AvailableWorkflows     []string                 `json:"availableWorkflows"`
	ModelCosts             []ModelCost              `json:"modelCosts"`
	Channels               []PublicModelChannelInfo `json:"channels"`
	DefaultModel           string                   `json:"defaultModel"`
	DefaultImageModel      string                   `json:"defaultImageModel"`
	DefaultVideoModel      string                   `json:"defaultVideoModel"`
	DefaultTextModel       string                   `json:"defaultTextModel"`
	SystemPrompt           string                   `json:"systemPrompt"`
	SystemPrompts          SystemPromptSetting      `json:"systemPrompts"`
	AllowCustomChannel     *bool                    `json:"allowCustomChannel"`
	AllowUserRemoteChannel *bool                    `json:"allowUserRemoteChannel"`
}

type SystemPromptSetting struct {
	Image         string `json:"image"`
	Video         string `json:"video"`
	Text          string `json:"text"`
	Workflow      string `json:"workflow"`
	WorkflowAgent string `json:"workflowAgent"`
}

type PublicModelChannelInfo struct {
	ParameterTranslationModels []string          `json:"parameterTranslationModels,omitempty"`
	ID                         string            `json:"id"`
	Protocol                   string            `json:"protocol"`
	Name                       string            `json:"name"`
	BaseURL                    string            `json:"baseUrl"`
	Models                     []string          `json:"models"`
	Weight                     int               `json:"weight"`
	Timeout                    int               `json:"timeout"`
	Enabled                    bool              `json:"enabled"`
	Remark                     string            `json:"remark"`
	Workflows                  []WorkflowSummary `json:"workflows,omitempty"`

	ModelCapabilities map[string]string `json:"modelCapabilities,omitempty"`
}

// PublicSetting 公开配置。
type PublicSetting struct {
	ModelChannel PublicModelChannelSetting `json:"modelChannel"`
	Auth         PublicAuthSetting         `json:"auth"`
	Storage      PublicStorageSetting      `json:"storage"`
	Billing      PublicBillingSetting      `json:"billing"`
	Announcement PublicAnnouncementSetting `json:"announcement"`
	Works        PublicWorksSetting        `json:"works"`
	Ecommerce    PublicEcommerceSetting    `json:"ecommerce"`
	Video        PublicVideoSetting        `json:"video"`
	// ImageExamples are the example images of each image tool, keyed by tool id (set, detail, replicate, remove-bg, tryon, free).
	ImageExamples map[string]ImageToolExamples `json:"imageExamples"`
}

type ImageToolExamples struct {
	Cover string             `json:"cover"`
	Items []ImageExampleItem `json:"items"`
}

// ImageExampleItem is one example image; Before is the input of before/after tools (remove-bg, tryon, replicate).
type ImageExampleItem struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	Before string `json:"before,omitempty"`
}

// PublicEcommerceSetting picks the models and image quality used by the ecommerce image tools.
type PublicEcommerceSetting struct {
	VisionModel    string `json:"visionModel"`
	ImageModel     string `json:"imageModel"`
	PreviewQuality string `json:"previewQuality"`
	FinalQuality   string `json:"finalQuality"`
}

// PublicVideoSetting picks the models of the product video (带货视频) wizard. An empty KeyframeModel
// (no image model configured) makes shots go straight from the product images to video.
type PublicVideoSetting struct {
	Model          string `json:"model"`
	KeyframeModel  string `json:"keyframeModel"`
	ScriptModel    string `json:"scriptModel"`
	TTSModel       string `json:"ttsModel"`
	TTSVoice       string `json:"ttsVoice"`
	MaxShotSeconds int    `json:"maxShotSeconds"`
}

// PublicWorksSetting controls server-side works retention and the generation queue limits.
type PublicWorksSetting struct {
	RetentionDays       int `json:"retentionDays"`
	MemberRetentionDays int `json:"memberRetentionDays"`
	UserConcurrency     int `json:"userConcurrency"`
	MemberConcurrency   int `json:"memberConcurrency"`
	GlobalConcurrency   int `json:"globalConcurrency"`
}

// PublicBillingSetting controls how new-api quota is shown to users.
type PublicBillingSetting struct {
	CreditsPerUnit float64 `json:"creditsPerUnit"`
	CreditName     string  `json:"creditName"`
}

// PublicAnnouncementSetting is the operator-configured banner above the portal.
type PublicAnnouncementSetting struct {
	Enabled bool   `json:"enabled"`
	Text    string `json:"text"`
	Link    string `json:"link"`
	EndAt   string `json:"endAt"`
}

type PublicStorageSetting struct {
	Mode                    string `json:"mode"`
	AllowUserProvider       bool   `json:"allowUserProvider"`
	AllowUserGlobalProvider bool   `json:"allowUserGlobalProvider"`
}

type PublicStorageConfig struct {
	PublicStorageSetting
	AutoSyncAllAssets bool `json:"autoSyncAllAssets"`
}

type PublicAuthSetting struct {
	AllowRegister *bool                    `json:"allowRegister"`
	LinuxDo       PublicLinuxDoAuthSetting `json:"linuxDo"`
	NewapiSSO     bool                     `json:"newapiSso"`
}

type PublicLinuxDoAuthSetting struct {
	Enabled bool `json:"enabled"`
}

// PrivateSetting 私有配置。
type PrivateSetting struct {
	Channels   []ModelChannel        `json:"channels"`
	PromptSync PromptSyncSetting     `json:"promptSync"`
	AILog      AILogSetting          `json:"aiLog"`
	Auth       PrivateAuthSetting    `json:"auth"`
	Storage    PrivateStorageSetting `json:"storage"`
	// TemplateSamples are the shared sample images (/api/files/<id>/content) admins run template examples with.
	TemplateSamples TemplateSamples `json:"templateSamples"`
	// EcommercePrompts overrides the built-in ecommerce AI templates; empty fields use the defaults.
	EcommercePrompts EcommercePrompts `json:"ecommercePrompts"`
}

// EcommercePrompts are the ecommerce set templates (analyze, plan, selling points) and the slot
// prompt variants per kind (main, white, detail, sku).
type EcommercePrompts struct {
	Analyze             string              `json:"analyze"`
	Plan                string              `json:"plan"`
	SellingPoints       string              `json:"sellingPoints"`
	SellingPointRewrite string              `json:"sellingPointRewrite"`
	Variants            map[string][]string `json:"variants"`
}

// TemplateSamples groups sample inputs by subject: product (商品组), portrait (人像组) and model (模特组).
type TemplateSamples struct {
	Product  []string `json:"product"`
	Portrait []string `json:"portrait"`
	Model    []string `json:"model"`
}

type AILogSetting struct {
	LocalDirectReportEnabled *bool               `json:"localDirectReportEnabled"`
	Cleanup                  AILogCleanupSetting `json:"cleanup"`
}

type AILogCleanupSetting struct {
	Enabled       *bool  `json:"enabled"`
	RetentionDays int    `json:"retentionDays"`
	Cron          string `json:"cron"`
}

type PrivateStorageSetting struct {
	Mode                    string                      `json:"mode"`
	AllowUserProvider       bool                        `json:"allowUserProvider"`
	AllowUserGlobalProvider bool                        `json:"allowUserGlobalProvider"`
	AutoSyncAllAssets       bool                        `json:"autoSyncAllAssets"`
	Providers               []StorageProvider           `json:"providers"`
	RoundRobinCursor        int                         `json:"roundRobinCursor"`
	CapacityCheck           StorageCapacityCheckSetting `json:"capacityCheck"`
	CapacityLimitBytes      int64                       `json:"capacityLimitBytes"`
}

type StorageProvider struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	AccessKeyID       string `json:"accessKeyId"`
	SecretAccessKey   string `json:"secretAccessKey"`
	PublicBaseURL     string `json:"publicBaseUrl"`
	PathPrefix        string `json:"pathPrefix"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	Weight            int    `json:"weight"`
	Enabled           bool   `json:"enabled"`
	OwnerUserID       string `json:"ownerUserId"`
	CapacityBytes     int64  `json:"capacityBytes"`
	CapacityCheckedAt string `json:"capacityCheckedAt"`
	CapacityExceeded  bool   `json:"capacityExceeded"`
}

type StorageCapacityCheckSetting struct {
	Enabled *bool  `json:"enabled"`
	Cron    string `json:"cron"`
}

// PromptSyncSetting 提示词定时同步配置。
type PromptSyncSetting struct {
	Enabled *bool  `json:"enabled"`
	Cron    string `json:"cron"`
}

type PrivateAuthSetting struct {
	LinuxDo PrivateLinuxDoAuthSetting `json:"linuxDo"`
}

type PrivateLinuxDoAuthSetting struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// Setting 系统配置。
type Setting struct {
	Key       SettingKey      `json:"key" gorm:"primaryKey"`
	Value     json.RawMessage `json:"value" gorm:"serializer:json"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}

// Settings 系统公开和私有配置。
type Settings struct {
	Public  PublicSetting  `json:"public"`
	Private PrivateSetting `json:"private"`
}
