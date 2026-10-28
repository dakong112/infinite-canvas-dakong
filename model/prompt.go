package model

// Prompt 提示词记录。
type Prompt struct {
	ID        string   `json:"id" gorm:"primaryKey"`
	Title     string   `json:"title"`
	CoverURL  string   `json:"coverUrl"`
	Prompt    string   `json:"prompt"`
	Tags      []string `json:"tags" gorm:"serializer:json"`
	Category  string   `json:"category" gorm:"index"`
	GithubURL string   `json:"githubUrl" gorm:"-"`
	Preview   string   `json:"preview"`
	VideoURL  string   `json:"videoUrl"`       // preview clip of a video prompt
	Media     string   `json:"media" gorm:"-"` // from the category: "video" or "" (image)
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// PromptList 提示词分页结果。
type PromptList struct {
	Items      []Prompt `json:"items"`
	Tags       []string `json:"tags"`
	Categories []string `json:"categories"`
	Total      int      `json:"total"`
}

// PromptCategory 提示词分类。
type PromptCategory struct {
	Category    string `json:"category" gorm:"primaryKey"`
	Name        string `json:"name"`
	Description string `json:"description"`
	GithubURL   string `json:"githubUrl"`
	Remote      bool   `json:"remote"`
	Media       string `json:"media"` // "video" for video-generation prompt categories, "" for image
	UpdatedAt   string `json:"updatedAt"`
}
