package middleware

import (
	"cmp"
	"net/http"
	"strings"

	"github.com/tigerowo/infinite-canvas/handler"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/service"
	"github.com/gin-gonic/gin"
)

// AdminAuth admits local admins and SSO users holding any canvas.* code (refreshed from new-api);
// RequirePermission then checks the code of each route group.
func AdminAuth(c *gin.Context) {
	user, ok := authUser(c)
	if !ok {
		handler.FailWithStatus(c.Writer, http.StatusUnauthorized, "未登录")
		c.Abort()
		return
	}
	if !user.IsLocalAdmin {
		var err error
		user.Permissions, err = service.UserPermissions(user.ID)
		ok = err == nil
	}
	if !ok || !service.CanEnterAdmin(user) {
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
	user, ok := authUser(c)
	if !ok || user.Role == model.UserRoleGuest {
		handler.FailWithStatus(c.Writer, http.StatusUnauthorized, "未登录或权限不足")
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), user))
	c.Next()
}

func OptionalAuth(c *gin.Context) {
	if user, ok := authUser(c); ok {
		c.Request = c.Request.WithContext(service.WithUser(c.Request.Context(), user))
	}
	c.Next()
}

func NotFoundJSON(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": 1, "data": nil, "msg": "接口不存在"})
}

func authUser(c *gin.Context) (model.AuthUser, bool) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if strings.TrimSpace(token) == "" {
		return model.AuthUser{}, false
	}
	return service.CurrentAuthUser(token)
}
