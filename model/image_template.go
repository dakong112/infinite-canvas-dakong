package model

import "time"

const (
	ImageTemplateStatusDraft     = "draft"
	ImageTemplateStatusPublished = "published"

	ImageTemplateKindImage = "image"
	ImageTemplateKindVideo = "video"

	// Template fee states on generation_jobs.fee_status; non-template jobs keep "".
	TemplateFeePending = "pending"
	TemplateFeeCharged = "charged"
	TemplateFeeFailed  = "failed"
	TemplateFeeWaived  = "waived"
)

// ImageTemplateInput is one upload area of a template; files are numbered @图片1… in input order.
type ImageTemplateInput struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Max      int    `json:"max"`
	Required bool   `json:"required"`
}

type ImageTemplateExample struct {
	Inputs []string `json:"inputs"`
	Output string   `json:"output"`
	Prompt string   `json:"prompt"`
}

// VideoTemplateShot is one storyboard row a video template starts the wizard with.
type VideoTemplateShot struct {
	Seconds   int    `json:"seconds"`
	Scene     string `json:"scene"`
	Camera    string `json:"camera"`
	Voiceover string `json:"voiceover"`
	Subtitle  string `json:"subtitle"`
}

// VideoTemplateDefaults prefill the product video wizard (form, settings and optional storyboard).
type VideoTemplateDefaults struct {
	Duration      int                 `json:"duration"`
	Ratio         string              `json:"ratio"`
	Style         string              `json:"style"`
	Name          string              `json:"name"`
	Intro         string              `json:"intro"`
	SellingPoints string              `json:"sellingPoints"`
	Shots         []VideoTemplateShot `json:"shots"`
}

// ImageTemplate is an admin-authored image app ("模板"): a system prompt plus the inputs users upload.
type ImageTemplate struct {
	ID   string `json:"id" gorm:"primaryKey;size:64"`
	Slug string `json:"slug" gorm:"size:64;uniqueIndex"`
	// Kind "video" templates only prefill the product video wizard (VideoDefaults); image fields stay empty.
	Kind              string                 `json:"kind" gorm:"size:16;default:image;index"`
	Title             string                 `json:"title" gorm:"size:100"`
	Description       string                 `json:"description" gorm:"size:300"`
	Category          string                 `json:"category" gorm:"size:16;index"`
	CoverURL          string                 `json:"coverUrl" gorm:"size:2048"`
	Inputs            []ImageTemplateInput   `json:"inputs" gorm:"serializer:json"`
	Prompt            string                 `json:"prompt,omitempty" gorm:"type:text"`
	PromptRequired    bool                   `json:"promptRequired"`
	PromptPlaceholder string                 `json:"promptPlaceholder" gorm:"size:300"`
	Ratios            []string               `json:"ratios" gorm:"serializer:json"`
	Resolutions       []string               `json:"resolutions" gorm:"serializer:json"`
	Model             string                 `json:"model" gorm:"size:128"`
	FeeCredits        float64                `json:"feeCredits"`
	MemberFree        bool                   `json:"memberFree"`
	Examples          []ImageTemplateExample `json:"examples" gorm:"serializer:json"`
	Credit            string                 `json:"credit" gorm:"size:300"`
	VideoDefaults     *VideoTemplateDefaults `json:"videoDefaults,omitempty" gorm:"serializer:json"`
	Status            string                 `json:"status" gorm:"size:16;index"`
	Sort              int                    `json:"sort"`
	CreatedAt         time.Time              `json:"createdAt"`
	UpdatedAt         time.Time              `json:"updatedAt"`
}
