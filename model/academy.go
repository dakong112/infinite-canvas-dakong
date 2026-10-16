package model

import "time"

const (
	AcademyKindCamp   = "camp"
	AcademyKindCourse = "course"

	AcademyStatusDraft     = "draft"
	AcademyStatusPublished = "published"

	AcademyFeeFree    = "free"
	AcademyFeeCharged = "charged"

	// Derived enrollment states of a published course (never stored).
	AcademyStateOpen  = "open"
	AcademyStateFull  = "full"
	AcademyStateEnded = "ended"
)

type AcademyLesson struct {
	Title    string `json:"title"`
	VideoURL string `json:"videoUrl"`
}

// AcademyCourse is a 学院 camp or course; EnrolledCount is only changed by the atomic seat updates.
type AcademyCourse struct {
	ID            uint            `json:"id" gorm:"primaryKey"`
	Slug          string          `json:"slug" gorm:"size:64;uniqueIndex"`
	Kind          string          `json:"kind" gorm:"size:16"`
	Category      string          `json:"category" gorm:"size:32;index"`
	Title         string          `json:"title" gorm:"size:100"`
	Summary       string          `json:"summary" gorm:"size:300"`
	CoverURL      string          `json:"coverUrl" gorm:"size:2048"`
	Body          string          `json:"body" gorm:"type:text"`
	Lessons       []AcademyLesson `json:"lessons" gorm:"serializer:json"`
	StartsAt      *time.Time      `json:"startsAt"`
	EndsAt        *time.Time      `json:"endsAt"`
	EnrollEndsAt  *time.Time      `json:"enrollEndsAt"`
	Capacity      int             `json:"capacity"`
	EnrolledCount int             `json:"enrolledCount"`
	MemberOnly    bool            `json:"memberOnly"`
	FeeCredits    float64         `json:"feeCredits"`
	MemberFree    bool            `json:"memberFree"`
	Status        string          `json:"status" gorm:"size:16;index"`
	Sort          int             `json:"sort"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	// Per-request fields of the public API.
	State    string `json:"state,omitempty" gorm:"-"`
	Enrolled bool   `json:"enrolled" gorm:"-"`
	// Locked: lesson videos are hidden until the user enrolls.
	Locked bool `json:"locked" gorm:"-"`
}

// AcademyEnrollment is one user's seat in a course; enrollments are personal, never team-scoped.
type AcademyEnrollment struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	CourseID  uint      `json:"courseId" gorm:"uniqueIndex:idx_academy_enrollments_course_user"`
	UserID    string    `json:"userId" gorm:"size:64;uniqueIndex:idx_academy_enrollments_course_user;index"`
	FeeQuota  int64     `json:"feeQuota"`
	FeeStatus string    `json:"feeStatus" gorm:"size:16"`
	CreatedAt time.Time `json:"createdAt"`
}

// AcademyEnrollmentRow is an enrollment in the admin list, with the user's name.
type AcademyEnrollmentRow struct {
	AcademyEnrollment
	Username string `json:"username"`
}
