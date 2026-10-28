package middleware

import (
	"cmp"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tigerowo/infinite-canvas/handler"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
)

// AdminAuth admits local admins and SSO users holding any canvas.* code (refreshed from new-api);
// RequirePermission then checks the code of each route group.
func AdminAuth(c *gin.Context) {
	user, err := authUser(c)
	if err != nil {
		unauthorized(c, err, "未登录")
		return
	}
	if !user.IsLocalAdmin {
		user.Permissions, err = service.UserPermissions(user.ID)
	}
	if err != nil || !service.CanEnterAdmin(user) {
		handler.FailWithStatus(c.Writer, http.StatusForbidden, "权限不足")
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), user))
	c.Next()
}

// RequirePermission lets through users of AdminAuth who hold code.
func RequirePermission(code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, _ := service.UserFromContext(c.Request.Context())
		if !service.HasPermission(user, code) {
			handler.FailWithStatus(c.Writer, http.StatusForbidden, "权限不足")
			c.Abort()
			return
		}
		c.Next()
	}
}

// SpaceGuard refuses a request a page made for another space than the account works in now (switched in
// another tab or device, or the team membership ended), so nothing is read from or written to the wrong space.
// Pages send X-Space-Id ("personal" or "team:<id>"); requests without it are not checked.
func SpaceGuard(c *gin.Context) {
	if sent := c.GetHeader("X-Space-Id"); sent != "" {
		user, _ := service.UserFromContext(c.Request.Context())
		if current := cmp.Or(user.SpaceID, "personal"); sent != current {
			handler.FailWithStatus(c.Writer, http.StatusConflict, "当前空间已切换，请刷新页面")
			c.Abort()
			return
		}
	}
	c.Next()
}

func UserAuth(c *gin.Context) {
	user, err := authUser(c)
	if err == nil && user.Role == model.UserRoleGuest {
		err = errors.New("guest")
	}
	if err != nil {
		unauthorized(c, err, "未登录或权限不足")
		return
	}
	c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), user))
	c.Next()
}

// OptionalAuth lets anonymous requests through, but tells a client whose new-api session ended to sign in again.
func OptionalAuth(c *gin.Context) {
	user, err := authUser(c)
	if errors.Is(err, service.ErrSessionExpired) {
		unauthorized(c, err, "")
		return
	}
	if err == nil {
		c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), user))
	}
	c.Next()
}

// unauthorized answers 401 with fallback, or the session-expired message the web signs in again on.
func unauthorized(c *gin.Context, err error, fallback string) {
	message := fallback
	if errors.Is(err, service.ErrSessionExpired) {
		message = err.Error()
	}
	handler.FailWithStatus(c.Writer, http.StatusUnauthorized, message)
	c.Abort()
}

func NotFoundJSON(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": 1, "data": nil, "msg": "接口不存在"})
}

func authUser(c *gin.Context) (model.AuthUser, error) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if strings.TrimSpace(token) == "" {
		return model.AuthUser{}, errors.New("未登录")
	}
	return service.CurrentAuthUser(token)
}
