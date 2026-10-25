package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// AuthService — то, что хендлеру входа нужно от бизнес-слоя.
type AuthService interface {
	TestUsers(ctx context.Context) ([]domain.User, error)
	Login(ctx context.Context, userID string) (string, domain.User, error)
	Me(ctx context.Context, token string) (domain.User, error)
}

// AuthHandler — мок-вход по контракту api/openapi.yaml. Маршруты — в router.go.
type AuthHandler struct {
	svc AuthService
	log *slog.Logger
}

func NewAuthHandler(svc AuthService, log *slog.Logger) *AuthHandler {
	return &AuthHandler{svc: svc, log: log}
}

const userKey = "user"

// authenticate — необязательная проверка `Authorization: Bearer mock.<user_id>`. Без заголовка запрос идёт
// анонимно (регистрация и анкета работают и так); с неверным токеном — 401.
func (h *AuthHandler) authenticate(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if header == "" {
		c.Next()
		return
	}
	// схема — без учёта регистра (RFC 9110, 11.1)
	scheme, token, _ := strings.Cut(strings.TrimSpace(header), " ")
	token = strings.TrimSpace(token)
	if !strings.EqualFold(scheme, "Bearer") || token == "" {
		writeProblem(c, unauthorizedProblem("ожидается Authorization: Bearer <token>"))
		return
	}
	user, err := h.svc.Me(c.Request.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrUnauthorized) {
			writeProblem(c, unauthorizedProblem("токен недействителен"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "authenticate", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	c.Set(userKey, user)
	c.Next()
}

// currentUser — пользователь, вошедший по токену; false — запрос анонимный.
func currentUser(c *gin.Context) (domain.User, bool) {
	v, ok := c.Get(userKey)
	if !ok {
		return domain.User{}, false
	}
	user, ok := v.(domain.User)
	return user, ok
}

// testUsers — GET /api/v1/auth/test-users.
//
//	@Summary		Список синтетических пользователей для входа
//	@Description	Тестовые пользователи стенда: директора, бухгалтеры и ИП компаний сценариев mock-api A–E, демо-персонажи
//	@Description	экранов кабинета и андеррайтер. Заводятся при старте API (SEED_TEST_USERS). Вход — POST /auth/mock-login,
//	@Description	только под ними: пользователи, заведённые анонимной регистрацией, войти не могут. Токен этой ручке не нужен.
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	testUsersResponse
//	@Failure		500	{object}	problem
//	@Router			/auth/test-users [get]
func (h *AuthHandler) testUsers(c *gin.Context) {
	users, err := h.svc.TestUsers(c.Request.Context())
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "list test users", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	items := make([]testUserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, toTestUserResponse(u))
	}
	c.JSON(http.StatusOK, testUsersResponse{Items: items})
}

// mockLogin — POST /api/v1/auth/mock-login.
//
//	@Summary		Войти под синтетическим пользователем
//	@Description	Возвращает токен `mock.<user_id>` и пользователя. Токен передаётся в `Authorization: Bearer …`:
//	@Description	с ним POST /applicants оформляет заявку от имени этого пользователя, а GET /me возвращает его компании.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		loginRequest	true	"id тестового пользователя"
//	@Success		200		{object}	loginResponse
//	@Failure		400		{object}	problem	"битый JSON, нет user_id"
//	@Failure		404		{object}	problem	"пользователя нет или он не тестовый"
//	@Failure		500		{object}	problem
//	@Router			/auth/mock-login [post]
func (h *AuthHandler) mockLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}
	userID := strings.TrimSpace(*req.UserID)
	if userID == "" {
		writeProblem(c, validationProblem(http.StatusBadRequest, "тело запроса не прошло проверку",
			[]problemField{{Field: "user_id", Message: "обязательное поле", Code: "required"}}))
		return
	}
	token, user, err := h.svc.Login(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeProblem(c, notFoundProblem("пользователь не найден"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "mock login", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	c.JSON(http.StatusOK, loginResponse{Token: token, User: toMeResponse(user)})
}

// me — GET /api/v1/me.
//
//	@Summary		Текущий пользователь и его компании
//	@Description	Пользователь по токену `Authorization: Bearer mock.<user_id>` и компании, от имени которых он действует.
//	@Tags			auth
//	@Produce		json
//	@Security		bearer
//	@Success		200	{object}	meResponse
//	@Failure		401	{object}	problem	"нет или неверный токен"
//	@Failure		500	{object}	problem
//	@Router			/me [get]
func (h *AuthHandler) me(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		writeProblem(c, unauthorizedProblem("нужен токен: Authorization: Bearer mock.<user_id>"))
		return
	}
	c.JSON(http.StatusOK, toMeResponse(user))
}
