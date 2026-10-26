package httpapi

import (
	"bytes"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/fieldcatalog"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/gin-gonic/gin"
)

func RegisterAdminFields(router *gin.Engine, access *adminauth.Access) {
	g := router.Group("/api/admin/v1", access.Browser())
	g.GET("/field-catalog", func(c *gin.Context) {
		result, err := fieldcatalog.Read()
		if err != nil {
			writeProblem(c, 500, "FIELD_CATALOG_UNAVAILABLE", nil)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/field-catalog/usage", func(c *gin.Context) {
		fields, ok := adminBody(c, "definition")
		if !ok {
			return
		}
		d, err := model.Decode(bytes.NewReader(fields["definition"]))
		if err != nil {
			writeAdminError(c, err)
			return
		}
		result, err := fieldcatalog.Analyze(d)
		if err != nil {
			writeProblem(c, 422, "INVALID_ADMIN_INPUT", nil)
			return
		}
		c.JSON(200, result)
	})
}
