package model

import (
	"encoding/json"
	"time"
)

const (
	GenerationKindImage  = "image"
	GenerationKindVision = "vision"
	GenerationKindText   = "text"
	// Video jobs submit once, then wait in "polling" (no lease held) until the upstream finishes.
	GenerationKindVideo   = "video"
	GenerationKindAudio   = "audio"
	GenerationKindCompose = "compose"

	GenerationStatusQueued    = "queued"
	GenerationStatusRunning   = "running"
	GenerationStatusPolling   = "polling"
	GenerationStatusSucceeded = "succeeded"
	GenerationStatusFailed    = "failed"
	GenerationStatusCancelled = "cancelled"

	// Sources group works into the "我的作品" type tabs; the ecommerce ones are written by P3-B.
	GenerationSourceImage         = "image"
	GenerationSourceEcommerceSet  = "ecommerce-set"
	GenerationSourceEcommerceTool = "ecommerce-tool"
	GenerationSourceTemplate      = "template"
	// GenerationSourceTemplateExample marks admin runs that make a template's cover and examples (never charged the template fee).
	GenerationSourceTemplateExample = "template-example"
	GenerationSourceProductVideo    = "product-video"
)

// GenerationBatch groups jobs submitted together. Ecommerce sets keep their workflow state in
// ContextJSON and bump Version on every write so concurrent requests cannot queue the same job twice.
type GenerationBatch struct {
	ID          string    `json:"id" gorm:"primaryKey;size:64"`
	UserID      string    `json:"userId" gorm:"size:64;index"`
	SpaceID     string    `json:"spaceId" gorm:"size:32;not null;default:'';index"` // "" personal, else "team:<id>"
	Source      string    `json:"source" gorm:"size:32"`
	Title       string    `json:"title" gorm:"size:200"`
	ContextJSON string    `json:"-" gorm:"size:134217728"`
	Version     int       `json:"-" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"createdAt"`
}

// GenerationJob is one queued model call. Image results are stored files and show up as works.
// RequestJSON never holds image bytes: inputs are storage object IDs in InputFileIDs.
type GenerationJob struct {
	ID             string     `json:"id" gorm:"primaryKey;size:64"`
	UserID         string     `json:"userId" gorm:"size:64;index:idx_generation_jobs_user_created,priority:1"`
	SpaceID        string     `json:"spaceId" gorm:"size:32;not null;default:'';index"` // "" personal, else "team:<id>"
	BatchID        string     `json:"batchId" gorm:"size:64;index"`
	Source         string     `json:"source" gorm:"size:32"`
	Kind           string     `json:"kind" gorm:"size:16"`
	Endpoint       string     `json:"endpoint" gorm:"size:64"`
	Model          string     `json:"model" gorm:"size:128"`
	ChannelID      string     `json:"-" gorm:"size:128"`
	RequestJSON    string     `json:"-" gorm:"size:134217728"`
	InputFileIDs   []string   `json:"inputFileIds" gorm:"serializer:json"`
	Status         string     `json:"status" gorm:"size:16;index:idx_generation_jobs_claim,priority:1"`
	Member         bool       `json:"-"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"maxAttempts"`
	LeaseToken     string     `json:"-" gorm:"size:64"`
	LeaseExpiresAt *time.Time `json:"-"`
	NextRunAt      time.Time  `json:"nextRunAt" gorm:"index:idx_generation_jobs_claim,priority:2"`
	// UpstreamTaskID is the video_tasks row a submitted video job polls; NextRunAt is its next poll.
	UpstreamTaskID string     `json:"-" gorm:"size:128"`
	PollDeadline   *time.Time `json:"-"`
	Slow           bool       `json:"slow"` // a video still polling after 6 hours, re-checked every 10 minutes until the deadline
	Error          string     `json:"error" gorm:"type:text"`
	OutputFileID   string     `json:"outputFileId" gorm:"size:64"`
	OutputURL      string     `json:"outputUrl" gorm:"size:2048"`
	OutputText     string     `json:"outputText" gorm:"size:134217728"`
	Prompt         string     `json:"prompt" gorm:"size:134217728"`
	SlotKind       string     `json:"slotKind" gorm:"size:32"`
	Stage          string     `json:"stage" gorm:"size:16"`
	TemplateID     string     `json:"templateId,omitempty" gorm:"size:64;index"`
	FeeStatus      string     `json:"feeStatus,omitempty" gorm:"size:16;index"`
	FeeQuota       int64      `json:"feeQuota,omitempty"`
	FeeAttempts    int        `json:"-"`
	FeeNextAt      *time.Time `json:"-"`
	FeeError       string     `json:"-" gorm:"size:500"`
	Width          int        `json:"width"`
	Height         int        `json:"height"`
	CreatedAt      time.Time  `json:"createdAt" gorm:"index:idx_generation_jobs_user_created,priority:2"`
	FinishedAt     *time.Time `json:"finishedAt"`
	ExpiresAt      time.Time  `json:"expiresAt" gorm:"index"`
	DeletedAt      *time.Time `json:"-"`
}

// MarshalJSON adds inputUrls, the content URLs of InputFileIDs (signed per response like outputUrl).
func (job GenerationJob) MarshalJSON() ([]byte, error) {
	type plain GenerationJob
	urls := make([]string, len(job.InputFileIDs))
	for i, id := range job.InputFileIDs {
		urls[i] = "/api/files/" + id + "/content"
	}
	return json.Marshal(struct {
		plain
		InputURLs []string `json:"inputUrls"`
	}{plain(job), urls})
}
