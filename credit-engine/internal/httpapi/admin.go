package httpapi

import (
	"context"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/gin-gonic/gin"
)

// NewAdminRouter registers authentication. Subsequent model and event routes
// reuse Access.Browser and Access.Service respectively.
func NewAdminRouter(access *adminauth.Access, timeout time.Duration) *gin.Engine {
	r := NewRouter()
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.POST("/api/admin/v1/auth/login", access.Login)
	group := r.Group("/api/admin/v1", access.Browser())
	group.GET("/auth/session", access.Session)
	group.POST("/auth/logout", access.Logout)
	return r
}
