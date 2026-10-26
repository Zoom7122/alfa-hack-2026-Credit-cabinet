package httpapi

import (
	"context"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/gin-gonic/gin"
)

type AdminActivation interface {
	ActiveModels(context.Context) ([]adminstore.ActiveModel, error)
	ApplyDraft(context.Context, string, int64, int64, string, string) (adminstore.ActiveModel, error)
	ActivateVersion(context.Context, contracts.BorrowerType, string, int64, string, string) (adminstore.ActiveModel, error)
}

func RegisterAdminActivation(router *gin.Engine, access *adminauth.Access, store AdminActivation) {
	g := router.Group("/api/admin/v1", access.Browser())
	g.GET("/active-models", func(c *gin.Context) {
		result, err := store.ActiveModels(c.Request.Context())
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/drafts/:id/apply", func(c *gin.Context) {
		fields, ok := adminBody(c, "expected_revision", "expected_activation_revision", "reason")
		if !ok {
			return
		}
		revision, e1 := adminRevision(fields["expected_revision"])
		active, e2 := adminRevision(fields["expected_activation_revision"])
		reason, e3 := adminString(fields["reason"])
		if e1 != nil || e2 != nil || e3 != nil {
			writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
			return
		}
		result, err := store.ApplyDraft(c.Request.Context(), c.Param("id"), revision, active, adminActor(c), reason)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.PUT("/active-models/:borrower_type", func(c *gin.Context) {
		fields, ok := adminBody(c, "version", "expected_activation_revision", "reason")
		if !ok {
			return
		}
		version, e1 := adminString(fields["version"])
		active, e2 := adminRevision(fields["expected_activation_revision"])
		reason, e3 := adminString(fields["reason"])
		if e1 != nil || e2 != nil || e3 != nil {
			writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
			return
		}
		result, err := store.ActivateVersion(c.Request.Context(), contracts.BorrowerType(c.Param("borrower_type")), version, active, adminActor(c), reason)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
}
