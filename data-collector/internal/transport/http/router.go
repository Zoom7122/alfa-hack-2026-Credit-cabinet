// Package httptransport — слой приёма HTTP-запросов (gin).
package httptransport

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/docs" // спецификация, сгенерированная `make swagger`
)

// NewRouter — единственное место, где перечислены все эндпоинты data-collector.
// Новая ручка = метод в хендлере + строка здесь.
func NewRouter(questionnaires *QuestionnaireHandler, internalData *InternalDataHandler) *gin.Engine {
	useJSONFieldNames()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", healthz)
	r.GET("/swagger/*any", swagger()) // UI: /swagger/ или /swagger/index.html

	api := r.Group("/api/v1")
	{
		api.POST("/questionnaires", questionnaires.create)
		api.GET("/check-mock", internalData.checkMock)
	}

	return r
}

// swagger отдаёт Swagger UI. gin-swagger знает только конкретные файлы,
// поэтому голый /swagger/ перенаправляем на index.html.
func swagger() gin.HandlerFunc {
	ui := ginSwagger.WrapHandler(swaggerFiles.Handler)
	return func(c *gin.Context) {
		if c.Param("any") == "/" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		ui(c)
	}
}

// useJSONFieldNames: в ошибках валидации поле называется как в JSON (inn, а не INN).
func useJSONFieldNames() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			switch name {
			case "-":
				return ""
			case "":
				return f.Name
			}
			return name
		})
	}
}

func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
