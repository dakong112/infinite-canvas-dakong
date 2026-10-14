package model

import "time"

const (
	GenerationKindImage  = "image"
	GenerationKindVision = "vision"
	GenerationKindText   = "text"

	GenerationStatusQueued    = "queued"
	GenerationStatusRunning   = "running"
	GenerationStatusSucceeded = "succeeded"
	GenerationStatusFailed    = "failed"
	GenerationStatusCancelled = "cancelled"

	// Sources group works into the "我的作品" type tabs; the ecommerce ones are written by P3-B.
	GenerationSourceImage         = "image"
	GenerationSourceEcommerceSet  = "ecommerce-set"
	GenerationSourceEcommerceTool = "ecommerce-tool"
)

// GenerationBatch groups jobs submitted together. Ecommerce sets keep their workflow state in
// ContextJSON and bump Version on every write so concurrent requests cannot queue the same job twice.
type GenerationBatch struct {
	ID          string    `json:"id" gorm:"primaryKey;size:64"`
	UserID      string    `json:"userId" gorm:"size:64;index"`
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
	Error          string     `json:"error" gorm:"type:text"`
	OutputFileID   string     `json:"outputFileId" gorm:"size:64"`
	OutputURL      string     `json:"outputUrl" gorm:"size:2048"`
	OutputText     string     `json:"outputText" gorm:"size:134217728"`
	Prompt         string     `json:"prompt" gorm:"size:134217728"`
	SlotKind       string     `json:"slotKind" gorm:"size:32"`
	Stage          string     `json:"stage" gorm:"size:16"`
	Width          int        `json:"width"`
	Height         int        `json:"height"`
	CreatedAt      time.Time  `json:"createdAt" gorm:"index:idx_generation_jobs_user_created,priority:2"`
	FinishedAt     *time.Time `json:"finishedAt"`
	ExpiresAt      time.Time  `json:"expiresAt" gorm:"index"`
	DeletedAt      *time.Time `json:"-"`
}
