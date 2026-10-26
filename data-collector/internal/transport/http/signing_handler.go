package httptransport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

type SigningService interface {
	Accept(context.Context, string, string, string) (domain.SigningSession, error)
	Signing(context.Context, string, string) (domain.SigningSession, error)
	Confirm(context.Context, string, string, string) (domain.SigningSession, error)
}

type SigningHandler struct {
	svc SigningService
	log *slog.Logger
}

func NewSigningHandler(svc SigningService, log *slog.Logger) *SigningHandler {
	return &SigningHandler{svc: svc, log: log}
}

func WithSigning(h *SigningHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		g := api.Group("/applications/:id", func(c *gin.Context) {
			u, ok := currentUser(c)
			if !ok {
				writeProblem(c, unauthorizedProblem("войдите, чтобы подписать договор"))
				return
			}
			if u.Role != domain.UserClient {
				writeProblem(c, forbiddenProblem("договор подписывает представитель компании"))
				return
			}
			c.Next()
		})
		g.POST("/offers/:offerId/accept", h.accept)
		g.GET("/signing", h.get)
		g.POST("/signing/confirm", h.confirm)
	}
}

type signingSignerResponse struct {
	ID       string `json:"id" validate:"required"`
	FullName string `json:"full_name" validate:"required"`
}

// signingResponse — только MOCK/SIMPLE_CODE; криптографической ЭП этот сценарий не создаёт.
type signingResponse struct {
	ID             string                   `json:"id" validate:"required"`
	State          string                   `json:"state" validate:"required" enums:"AWAITING_SIGNER,SIGNED,FAILED,CANCELLED"`
	Profile        string                   `json:"profile" validate:"required" enums:"MOCK"`
	Method         string                   `json:"method" validate:"required" enums:"SIMPLE_CODE"`
	OfferID        string                   `json:"offer_id" validate:"required"`
	RequiredSigner signingSignerResponse    `json:"required_signer" validate:"required"`
	AwaitingMe     bool                     `json:"awaiting_me" validate:"required"`
	Documents      []documentResponse       `json:"documents" validate:"required"`
	DocumentSHA256 string                   `json:"document_sha256" validate:"required"`
	DemoCode       string                   `json:"demo_code,omitempty"`
	CodeExpiresAt  time.Time                `json:"code_expires_at" validate:"required"`
	AttemptsLeft   int                      `json:"attempts_left" validate:"required"`
	SignedAt       *time.Time               `json:"signed_at,omitempty"`
	Disbursement   *domain.MockDisbursement `json:"disbursement,omitempty"`
} //	@name	SigningSession

type confirmCodeRequest struct {
	Method string `json:"method,omitempty" enums:"SIMPLE_CODE"`
	Code   string `json:"code" binding:"required" example:"123456"`
} //	@name	ConfirmSigningRequest

func toSigningResponse(s domain.SigningSession, actorID string) signingResponse {
	code := ""
	awaitingMe := s.State == domain.SigningAwaiting && s.Signer.ID == actorID
	if awaitingMe {
		code = s.Code
	}
	return signingResponse{ID: s.ID, State: s.State, Profile: "MOCK", Method: "SIMPLE_CODE", OfferID: s.OfferID,
		RequiredSigner: signingSignerResponse{ID: s.Signer.ID, FullName: s.Signer.FullName}, AwaitingMe: awaitingMe,
		Documents: toDocumentResponses([]domain.Document{s.Document}), DocumentSHA256: s.DocumentSHA256,
		DemoCode: code, CodeExpiresAt: s.CodeExpiresAt, AttemptsLeft: s.AttemptsLeft, SignedAt: s.SignedAt, Disbursement: s.Disbursement}
}

// accept godoc
//
//	@Summary		Выбрать предложение и получить договор с демо-кодом
//	@Description	OFFERED → SIGNING. Повтор выбора того же предложения возвращает прежнюю сессию. Код виден только подписанту, действует 10 минут; 3 попытки.
//	@Tags			signing
//	@Produce		json
//	@Security		bearer
//	@Param			id						path		string	true	"id заявки"
//	@Param			offerId					path		string	true	"id предложения"
//	@Success		200						{object}	signingResponse
//	@Failure		401,403,404,409,410,500	{object}	problem
//	@Router			/applications/{id}/offers/{offerId}/accept [post]
func (h *SigningHandler) accept(c *gin.Context) {
	u, _ := currentUser(c)
	s, err := h.svc.Accept(c.Request.Context(), u.ID, c.Param("id"), c.Param("offerId"))
	h.respond(c, u.ID, s, err)
}

// get godoc
//
//	@Summary		Состояние демо-подписания договора
//	@Description	Сессия сохраняется в БД; после перезагрузки возвращается тот же код. У других представителей компании код скрыт.
//	@Tags			signing
//	@Produce		json
//	@Security		bearer
//	@Param			id				path		string	true	"id заявки"
//	@Success		200				{object}	signingResponse
//	@Failure		401,403,404,500	{object}	problem
//	@Router			/applications/{id}/signing [get]
func (h *SigningHandler) get(c *gin.Context) {
	u, _ := currentUser(c)
	s, err := h.svc.Signing(c.Request.Context(), u.ID, c.Param("id"))
	h.respond(c, u.ID, s, err)
}

// confirm godoc
//
//	@Summary		Подтвердить договор кодом и завершить учебную выдачу
//	@Description	Только директор компании или сам ИП. SIGNING → COMPLETED, код стирается. Повтор успешного подтверждения безопасен. Неверный код расходует попытку; 3 ошибки или истечение кода возвращают заявку в OFFERED.
//	@Tags			signing
//	@Accept			json
//	@Produce		json
//	@Security		bearer
//	@Param			id								path		string				true	"id заявки"
//	@Param			body							body		confirmCodeRequest	true	"Код из интерфейса"
//	@Success		200								{object}	signingResponse
//	@Failure		400,401,403,404,409,410,422,500	{object}	problem
//	@Router			/applications/{id}/signing/confirm [post]
func (h *SigningHandler) confirm(c *gin.Context) {
	var in confirmCodeRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}
	if in.Method != "" && in.Method != "SIMPLE_CODE" {
		writeProblem(c, validationProblem(422, "поддерживается демо-подписание кодом", []problemField{{Field: "method", Code: "invalid_value", Message: "используйте SIMPLE_CODE"}}))
		return
	}
	u, _ := currentUser(c)
	s, err := h.svc.Confirm(c.Request.Context(), u.ID, c.Param("id"), in.Code)
	h.respond(c, u.ID, s, err)
}

func (h *SigningHandler) respond(c *gin.Context, actorID string, s domain.SigningSession, err error) {
	if err == nil {
		c.JSON(http.StatusOK, toSigningResponse(s, actorID))
		return
	}
	var codeErr *domain.SigningCodeError
	var validation *service.ValidationError
	switch {
	case errors.As(err, &codeErr):
		p := validationProblem(422, fmt.Sprintf("Неверный код. Осталось попыток: %d", codeErr.AttemptsLeft), nil)
		p.Code = "SIGNATURE_INVALID"
		if codeErr.Expired {
			p.Status, p.Code, p.Detail = 410, "CODE_EXPIRED", "Срок действия кода истёк. Выберите предложение заново, чтобы получить новый код"
		} else if codeErr.AttemptsLeft == 0 {
			p.Detail = "Попытки закончились. Выберите предложение заново, чтобы получить новый код"
		}
		writeProblem(c, p)
	case errors.As(err, &validation):
		writeProblem(c, fromValidationError(validation, "проверьте код"))
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("заявка, предложение или сессия подписания не найдены"))
	case errors.Is(err, service.ErrUnauthorized):
		writeProblem(c, unauthorizedProblem("нужен вход в кабинет"))
	case errors.Is(err, domain.ErrNotAuthorizedSigner):
		p := forbiddenProblem("договор подписывает директор компании или сам ИП; подписант должен быть зарегистрирован в кабинете")
		p.Code = "NOT_AUTHORIZED_SIGNER"
		writeProblem(c, p)
	case errors.Is(err, service.ErrForbidden):
		writeProblem(c, forbiddenProblem("заявка другой компании"))
	case errors.Is(err, domain.ErrOfferExpired):
		p := conflictProblem("предложение больше не действует")
		p.Status, p.Code = 410, "OFFER_EXPIRED"
		writeProblem(c, p)
	case errors.Is(err, service.ErrInvalidTransition):
		writeProblem(c, conflictProblem("статус заявки изменился — обновите страницу"))
	default:
		h.log.ErrorContext(c.Request.Context(), "mock signing", "err", err)
		writeProblem(c, internalProblem())
	}
}
