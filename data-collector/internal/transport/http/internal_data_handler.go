package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// InternalDataService — то, что хендлеру нужно от сервиса внутренних данных.
type InternalDataService interface {
	Collect(ctx context.Context, inn string) (domain.InternalBankData, error)
}

// InternalDataHandler отдаёт данные из внутренних систем банка. Маршруты — в router.go.
type InternalDataHandler struct {
	svc InternalDataService
	log *slog.Logger
}

func NewInternalDataHandler(svc InternalDataService, log *slog.Logger) *InternalDataHandler {
	return &InternalDataHandler{svc: svc, log: log}
}

// checkMock — GET /api/v1/check-mock?inn=...
// Опрашивает все системы банка и возвращает их ответы как есть.
// Отказ отдельной системы — не ошибка запроса: он виден в sources, ответ всё равно 200.
//
//	@Summary		Проверить данные внутренних систем банка
//	@Description	Параллельно опрашивает 5 систем банка (эмулятор mock-api) и возвращает их ответы как есть.
//	@Description	Секция системы, которая не ответила, — null; причина — в sources.
//	@Tags			internal-data
//	@Produce		json
//	@Param			inn	query		string	true	"ИНН: 10 цифр (ЮЛ) или 12 (ИП)"	example(7701234567)
//	@Success		200	{object}	internalDataResponse
//	@Failure		400	{object}	problem	"не передан inn"
//	@Failure		422	{object}	problem	"inn не из 10 или 12 цифр"
//	@Failure		500	{object}	problem
//	@Router			/check-mock [get]
func (h *InternalDataHandler) checkMock(c *gin.Context) {
	inn := c.Query("inn")
	if inn == "" {
		writeProblem(c, validationProblem(http.StatusBadRequest, "не передан ИНН", []problemField{{
			Field:   "inn",
			Message: "обязательный query-параметр",
			Code:    "required",
		}}))
		return
	}

	data, err := h.svc.Collect(c.Request.Context(), inn)
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			writeProblem(c, fromValidationError(verr, "некорректный ИНН"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "collect internal data", "err", err)
		writeProblem(c, internalProblem())
		return
	}

	for _, s := range data.Sources {
		if s.State == domain.SourceFailed {
			h.log.WarnContext(c.Request.Context(), "bank source failed", "source", s.Source, "err", s.Error)
		}
	}

	c.JSON(http.StatusOK, toInternalDataResponse(data))
}
