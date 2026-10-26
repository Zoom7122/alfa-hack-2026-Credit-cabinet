package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// EngineHooks — то, что credit-engine вызывает у data-collector.
type EngineHooks interface {
	Statements(ctx context.Context, borrowerID string, years []int) (decision.Statements, error)
	CreditEvent(ctx context.Context, ev decision.CreditEvent) error
}

// EngineHandler — ручки для credit-engine: поставщик Б (бухотчётность) и МС3 (события).
// Внутренние: в nginx кабинета не пропускаются.
type EngineHandler struct {
	svc EngineHooks
	log *slog.Logger
}

func NewEngineHandler(svc EngineHooks, log *slog.Logger) *EngineHandler {
	return &EngineHandler{svc: svc, log: log}
}

// WithEngineHooks — ручки, которые вызывает credit-engine.
func WithEngineHooks(h *EngineHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		api.GET("/borrowers/:id/statements", h.statements)
		api.POST("/credit-events", h.creditEvent)
	}
}

// statements — GET /api/v1/borrowers/{id}/statements?years=2024,2025.
//
//	@Summary		Бухотчётность компании для credit-engine (поставщик Б)
//	@Description	Два годовых комплекта из ГИР БО последнего сбора: баланс и ОФР в тыс. руб. id — компания (apl_…).
//	@Tags			credit-engine
//	@Produce		json
//	@Param			id		path		string	true	"id компании (apl_…)"
//	@Param			years	query		string	true	"годы через запятую"
//	@Success		200		{object}	object	"StatementsResponse credit-engine"
//	@Failure		404		{object}	problem	"по компании не было сбора"
//	@Router			/borrowers/{id}/statements [get]
func (h *EngineHandler) statements(c *gin.Context) {
	var years []int
	for _, y := range strings.Split(c.Query("years"), ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(y)); err == nil {
			years = append(years, v)
		}
	}
	st, err := h.svc.Statements(c.Request.Context(), c.Param("id"), years)
	switch {
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("по компании не было сбора данных"))
	case err != nil:
		h.log.ErrorContext(c.Request.Context(), "statements", "err", err)
		writeProblem(c, internalProblem())
	default:
		c.JSON(http.StatusOK, st)
	}
}

// creditEvent — POST /api/v1/credit-events.
//
//	@Summary	Событие credit-engine (МС3): выдача, ручное рассмотрение, проблема банка
//	@Tags		credit-engine
//	@Accept		json
//	@Produce	json
//	@Param		body	body		object	true	"AdminEvent credit-engine"
//	@Success	202		{object}	object	"EventAck"
//	@Failure	400		{object}	problem
//	@Router		/credit-events [post]
func (h *EngineHandler) creditEvent(c *gin.Context) {
	var ev decision.CreditEvent
	if err := c.ShouldBindJSON(&ev); err != nil || ev.EventID == "" {
		writeProblem(c, validationProblem(http.StatusBadRequest, "нужен event_id", nil))
		return
	}
	if err := h.svc.CreditEvent(c.Request.Context(), ev); err != nil {
		h.log.ErrorContext(c.Request.Context(), "credit event", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"event_id": ev.EventID, "accepted": true})
}
