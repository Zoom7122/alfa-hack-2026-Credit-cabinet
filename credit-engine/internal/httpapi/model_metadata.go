package httpapi

import (
	"context"
	"errors"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/gin-gonic/gin"
)

func RegisterScoringModels(router *gin.Engine, catalog *modelbindings.Catalog) {
	RegisterScoringModelProvider(router, catalog)
}
func RegisterScoringModelProvider(router *gin.Engine, provider modelbindings.Provider) {
	router.GET("/api/v1/models", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		result, err := provider.Models(c.Request.Context())
		if err != nil {
			writeProblem(c, 503, "SCORING_MODELS_UNAVAILABLE", nil)
			return
		}
		c.JSON(200, result)
	})
}

type ScoringMetadataSource interface {
	Models(context.Context) (modelbindings.RuntimeMetadata, error)
}

func RegisterAdminRuntime(router *gin.Engine, access *adminauth.Access, source ScoringMetadataSource) {
	router.GET("/api/admin/v1/runtime/models", access.Browser(), func(c *gin.Context) {
		if source == nil {
			writeProblem(c, 503, "SCORING_METADATA_NOT_CONFIGURED", nil)
			return
		}
		result, err := source.Models(c.Request.Context())
		if err != nil {
			var dependency *clients.Error
			if errors.As(err, &dependency) {
				writeProblem(c, dependency.Status, dependency.Code, nil)
			} else {
				writeProblem(c, 502, "SCORING_METADATA_UNAVAILABLE", nil)
			}
			return
		}
		c.JSON(200, result)
	})
}
