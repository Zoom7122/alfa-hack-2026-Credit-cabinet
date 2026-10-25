package httpapi

import (
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminui"
	"github.com/gin-gonic/gin"
)

func RegisterAdminUI(router *gin.Engine) {
	serve := func(c *gin.Context, name string) {
		data, contentType, ok := adminui.Asset(name)
		if !ok {
			writeProblem(c, 404, "NOT_FOUND", nil)
			return
		}
		c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "no-store")
		c.Data(200, contentType, data)
	}
	router.GET("/", func(c *gin.Context) { serve(c, "index.html") })
	router.GET("/assets/:file", func(c *gin.Context) {
		if c.Param("file") == "index.html" {
			writeProblem(c, 404, "NOT_FOUND", nil)
			return
		}
		serve(c, c.Param("file"))
	})
}
