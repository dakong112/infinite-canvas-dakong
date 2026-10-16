package model

import "strconv"

type UserRole string

const (
	UserRoleGuest UserRole = "guest"
	UserRoleUser  UserRole = "user"
	UserRoleAdmin UserRole = "admin"
)

type UserStatus string

const (
	UserStatusActive UserStatus = "active"
	UserStatusBan    UserStatus = "ban"
)

// User 系统用户。
type User struct {
	ID                   string     `json:"id" gorm:"primaryKey"`
	Username             string     `json:"username" gorm:"uniqueIndex"`
	Password             string     `json:"password,omitempty"`
	Email                string     `json:"email"`
	DisplayName          string     `json:"displayName"`
	AvatarURL            string     `json:"avatarUrl"`
	Role                 UserRole   `json:"role"`
	Credits              float64    `json:"credits" gorm:"type:decimal(20,2)"`
	AffCode              string     `json:"affCode" gorm:"uniqueIndex"`
	AffCount             int        `json:"affCount"`
	InviterID            string     `json:"inviterId"`
	GithubID             string     `json:"githubId"`
	LinuxDoID            string     `json:"linuxDoId" gorm:"index"`
	WechatID             string     `json:"wechatId"`
	NewapiID             string     `json:"newapiId" gorm:"index"`
	// Write via struct Save/Updates only: map-based Update/Updates bypass serializer:secret and would store plaintext.
	NewapiAPIKey         string     `json:"-" gorm:"serializer:secret"`
	NewapiAccessToken    string     `json:"-" gorm:"serializer:secret"`
	NewapiTokenExpiresAt int64      `json:"-"`
	Status               UserStatus `json:"status"`
	LastLoginAt          string     `json:"lastLoginAt"`
	Extra                string     `json:"extra" gorm:"type:text"`
	CreatedAt            string     `json:"createdAt"`
	UpdatedAt            string     `json:"updatedAt"`
	Permissions          []string   `json:"permissions" gorm:"type:text;serializer:json"` // canvas.* codes from new-api (SSO users)
	PermissionsAt        int64      `json:"-"`                                            // unix time Permissions were fetched
	CurrentTeamID        int        `json:"-" gorm:"not null;default:0"`                  // team whose space the user works in; 0 = personal
}

// UserList 用户分页结果。
type UserList struct {
	Items []User `json:"items"`
	Total int    `json:"total"`
}

// AuthUser 用户公开信息。
type AuthUser struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	AvatarURL   string   `json:"avatarUrl"`
	Role        UserRole `json:"role"`
	Credits     float64  `json:"credits"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
	Permissions []string `json:"permissions"`
	// IsLocalAdmin is an admin account of this site (not linked to new-api): every permission, no codes needed.
	IsLocalAdmin bool `json:"isLocalAdmin"`
	// SpaceID is "" for the personal space or "team:<id>" for a team space.
	SpaceID string `json:"spaceId"`
}

// AuthSession 登录会话信息。
type AuthSession struct {
	Token string   `json:"token"`
	User  AuthUser `json:"user"`
}

func PublicUser(user User) AuthUser {
	return AuthUser{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		AvatarURL:   user.AvatarURL,
		Role:        user.Role,
		Credits:     user.Credits,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Permissions: append([]string{}, user.Permissions...),
		// An admin row linked to new-api gets nothing from the role: its codes decide.
		IsLocalAdmin: user.Role == UserRoleAdmin && user.NewapiID == "",
		SpaceID:      TeamSpaceID(user.CurrentTeamID),
	}
}

// TeamSpacePrefix starts the space id of a team ("team:<id>"); the personal space id is "".
const TeamSpacePrefix = "team:"

func TeamSpaceID(teamID int) string {
	if teamID <= 0 {
		return ""
	}
	return TeamSpacePrefix + strconv.Itoa(teamID)
}

// Permission codes new-api grants for this application (resource canvas.*).
const (
	PermTemplateRead    = "canvas.template.read"
	PermTemplateWrite   = "canvas.template.write"
	PermTemplatePublish = "canvas.template.publish"
	PermUserRead        = "canvas.user.read"
	PermOrderRead       = "canvas.order.read"
	PermSettingWrite    = "canvas.setting.write"
	PermPromptWrite     = "canvas.prompt.write"
	PermAssetWrite      = "canvas.asset.write"
	PermAcademyRead     = "canvas.academy.read"
	PermAcademyWrite    = "canvas.academy.write"
)

type CreditLogType string

const (
	CreditLogTypeAdminAdjust CreditLogType = "admin_adjust"
	CreditLogTypeAIConsume   CreditLogType = "ai_consume"
	CreditLogTypeAIRefund    CreditLogType = "ai_refund"
)

// CreditLog 用户算力点变更流水。
type CreditLog struct {
	ID        string        `json:"id" gorm:"primaryKey"`
	UserID    string        `json:"userId" gorm:"index"`
	Type      CreditLogType `json:"type"`
	Amount    float64       `json:"amount" gorm:"type:decimal(20,2)"`
	Balance   float64       `json:"balance" gorm:"type:decimal(20,2)"`
	RelatedID string        `json:"relatedId"`
	Remark    string        `json:"remark"`
	Extra     string        `json:"extra" gorm:"type:text"`
	CreatedAt string        `json:"createdAt"`

	UserDisplayName string `json:"userDisplayName" gorm:"->;-:migration"`
}

type CreditLogList struct {
	Items []CreditLog `json:"items"`
	Total int         `json:"total"`
}

// NewapiGroupKey is the user's new-api API key for one group other than the
// account group, obtained through POST /api/sso/key on first use.
// Write via struct Create/Save only: map updates bypass serializer:secret.
type NewapiGroupKey struct {
	ID        uint   `json:"-" gorm:"primaryKey"`
	UserID    string `json:"-" gorm:"size:64;uniqueIndex:idx_newapi_group_keys_user_group,priority:1"`
	GroupName string `json:"-" gorm:"size:64;uniqueIndex:idx_newapi_group_keys_user_group,priority:2"`
	APIKey    string `json:"-" gorm:"serializer:secret"`
	CreatedAt string `json:"-"`
}
