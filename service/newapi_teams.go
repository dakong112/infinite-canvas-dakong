package service

import (
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/repository"
)

const (
	// permissionsFreshFor is how long fetched codes are used without asking new-api again.
	permissionsFreshFor = time.Minute
	// permissionsStaleMax is how long the last codes still count while new-api cannot be reached.
	permissionsStaleMax = 10 * time.Minute
	teamsCacheTTL       = time.Minute
)

// NewapiTeam is one team of the user as new-api lists it (quotas in new-api units).
type NewapiTeam struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	LimitQuota     int64  `json:"limit_quota"`
	Balance        int64  `json:"balance"`
	KeyRemainQuota int64  `json:"key_remain_quota"`
}

// Space is one workspace of the switcher: the personal space (ID "") or a team ("team:<id>").
type Space struct {
	ID     string `json:"id"`
	TeamID int    `json:"teamId"`
	Name   string `json:"name"`
	Role   string `json:"role"`
}

type SpaceList struct {
	Current string  `json:"current"`
	Spaces  []Space `json:"spaces"`
}

var teamsCache ttlCache[[]NewapiTeam]

// HasPermission reports whether the user holds code; local admins hold every code.
func HasPermission(user model.AuthUser, code string) bool {
	return user.IsLocalAdmin || slices.Contains(user.Permissions, code)
}

// CanEnterAdmin reports whether the user may open the admin console at all.
func CanEnterAdmin(user model.AuthUser) bool {
	return user.IsLocalAdmin || slices.ContainsFunc(user.Permissions, func(code string) bool { return strings.HasPrefix(code, "canvas.") })
}

// UserPermissions returns the user's permission codes, asking new-api again when they are older than a
// minute. While new-api cannot be reached the last codes keep working for ten minutes, then nothing is granted.
func UserPermissions(userID string) ([]string, error) {
	user, ok, err := repository.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, safeMessageError{message: "用户不存在"}
	}
	if user.NewapiID == "" || !NewapiSSOEnabled() {
		return user.Permissions, nil
	}
	age := time.Since(time.Unix(user.PermissionsAt, 0))
	if age < permissionsFreshFor {
		return user.Permissions, nil
	}
	var data struct {
		Permissions []string `json:"permissions"`
	}
	err = newapiCall(user, http.MethodPost, "/api/sso/permissions", newapiClientBody(nil), &data)
	if err == nil {
		codes := data.Permissions
		if codes == nil {
			codes = []string{}
		}
		if err := repository.SaveUserPermissions(user.ID, codes, time.Now().Unix()); err != nil {
			log.Printf("save permissions failed: user=%s err=%v", user.ID, err)
		}
		return codes, nil
	}
	log.Printf("refresh permissions failed: user=%s err=%v", user.ID, err)
	if age < permissionsStaleMax {
		return user.Permissions, nil
	}
	return nil, err
}

// newapiClientBody adds the application credentials new-api's /api/sso/* user endpoints require.
func newapiClientBody(extra map[string]any) map[string]any {
	body := map[string]any{"client_id": config.Cfg.NewapiClientID, "client_secret": config.Cfg.NewapiClientSecret}
	maps.Copy(body, extra)
	return body
}

// newapiTeams lists the user's teams, cached for a minute per user.
func newapiTeams(user model.User) ([]NewapiTeam, error) {
	if cached, ok := teamsCache.get(user.ID); ok {
		return cached, nil
	}
	teams := []NewapiTeam{}
	if err := newapiCall(user, http.MethodPost, "/api/sso/teams", newapiClientBody(nil), &teams); err != nil {
		return nil, err
	}
	teamsCache.set(user.ID, teams, teamsCacheTTL)
	return teams, nil
}

func findTeam(teams []NewapiTeam, id int) (NewapiTeam, bool) {
	index := slices.IndexFunc(teams, func(team NewapiTeam) bool { return team.ID == id })
	if index < 0 {
		return NewapiTeam{}, false
	}
	return teams[index], true
}

// resolveUserSpace returns the space the user works in now. A team counts only while new-api lists the user
// as its member: a member who left goes back to the personal space, and an unreachable new-api denies the
// team for this request (the stored choice is kept).
func resolveUserSpace(user model.User) string {
	if user.CurrentTeamID <= 0 {
		return ""
	}
	if !NewapiSSOEnabled() || user.NewapiID == "" {
		return ""
	}
	teams, err := newapiTeams(user)
	if err != nil {
		log.Printf("team membership check failed: user=%s err=%v", user.ID, err)
		return ""
	}
	if _, ok := findTeam(teams, user.CurrentTeamID); !ok {
		if err := repository.SetUserCurrentTeam(user.ID, 0); err != nil {
			log.Printf("reset space failed: user=%s err=%v", user.ID, err)
		}
		summaryCache.delete(user.ID)
		return ""
	}
	return model.TeamSpaceID(user.CurrentTeamID)
}

// ListSpaces returns the personal space and the user's teams.
func ListSpaces(userID string) (SpaceList, error) {
	user, ok, err := repository.GetUserByID(userID)
	if err != nil {
		return SpaceList{}, err
	}
	if !ok {
		return SpaceList{}, safeMessageError{message: "用户不存在"}
	}
	list := SpaceList{Spaces: []Space{{Name: "个人空间"}}}
	if !NewapiSSOEnabled() || user.NewapiID == "" {
		return list, nil
	}
	linked, err := newapiLinkedUser(userID)
	if err != nil {
		return SpaceList{}, err
	}
	teams, err := newapiTeams(linked)
	if err != nil {
		return SpaceList{}, err
	}
	for _, team := range teams {
		list.Spaces = append(list.Spaces, Space{ID: model.TeamSpaceID(team.ID), TeamID: team.ID, Name: team.Name, Role: team.Role})
	}
	list.Current = resolveUserSpace(user)
	return list, nil
}

// SwitchSpace moves the user to a team's space (teamID > 0, the user must be a member) or back to the personal space (0).
func SwitchSpace(userID string, teamID int) (SpaceList, error) {
	if teamID < 0 {
		return SpaceList{}, safeMessageError{message: "空间无效"}
	}
	if teamID > 0 {
		user, err := newapiLinkedUser(userID)
		if err != nil {
			return SpaceList{}, err
		}
		// Ask new-api again so a team joined a moment ago can be entered.
		teamsCache.delete(user.ID)
		teams, err := newapiTeams(user)
		if err != nil {
			return SpaceList{}, err
		}
		if _, ok := findTeam(teams, teamID); !ok {
			return SpaceList{}, safeMessageError{message: "你不是该团队的成员"}
		}
	}
	if err := repository.SetUserCurrentTeam(userID, teamID); err != nil {
		return SpaceList{}, err
	}
	summaryCache.delete(userID)
	return ListSpaces(userID)
}

// SpaceOwner is the owner key of the user's space rows: the team space id, or the user id for the personal space.
func SpaceOwner(user model.AuthUser) string {
	if user.SpaceID != "" {
		return user.SpaceID
	}
	return user.ID
}
