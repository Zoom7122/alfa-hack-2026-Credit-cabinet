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

// Пути для заголовка Location: регистрация ссылается на заявку, анкета — на задачу на сбор.
const (
	applicationsPath = "/api/v1/applications"
	collectionsPath  = "/api/v1/collections"
)

// RouterOption подключает необязательные части API.
type RouterOption func(r *gin.Engine, api *gin.RouterGroup)

// WithAuth — мок-вход (/auth/test-users, /auth/mock-login, /me) и проверка токена на всех ручках /api/v1:
// с токеном регистрация оформляется от имени вошедшего пользователя, без токена всё работает анонимно.
func WithAuth(auth *AuthHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		// вход — до проверки токена: по контракту ему токен не нужен, и старый токен не мешает перелогиниться
		api.GET("/auth/test-users", auth.testUsers)
		api.POST("/auth/mock-login", auth.mockLogin)
		api.Use(auth.authenticate)
		api.GET("/me", auth.me)
	}
}

// NewRouter — единственное место, где перечислены все эндпоинты data-collector.
// Новая ручка = метод в хендлере + строка здесь (или в RouterOption).
func NewRouter(applications *ApplicationHandler, questionnaires *QuestionnaireHandler, collections *CollectionHandler,
	internalData *InternalDataHandler, opts ...RouterOption) *gin.Engine {
	useJSONFieldNames()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", healthz)
	r.GET("/swagger/*any", swagger()) // UI: /swagger/ или /swagger/index.html

	api := r.Group("/api/v1")
	// middleware группы (проверка токена) — до ручек
	for _, opt := range opts {
		opt(r, api)
	}
	{
		api.POST("/applicants", applications.register)
		api.GET("/applications/:id", applications.get)
		api.POST("/applications/:id/questionnaire", questionnaires.submit)
		api.GET("/collections/:id", collections.get)
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
