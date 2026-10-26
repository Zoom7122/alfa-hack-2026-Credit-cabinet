package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/gin-gonic/gin"
)

type DraftService interface {
	Get(context.Context, string, string) (domain.SavedDraft, error)
	Save(context.Context, string, string, json.RawMessage, int) (domain.SavedDraft, error)
}

type DraftHandler struct {
	svc DraftService
	log *slog.Logger
}

func NewDraftHandler(svc DraftService, log *slog.Logger) *DraftHandler {
	return &DraftHandler{svc, log}
}

func WithDraft(h *DraftHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		api.GET("/applications/:id/draft", h.get)
		api.PUT("/applications/:id/draft", h.put)
	}
}

type draftResponse struct {
	Data      json.RawMessage `json:"data" swaggertype:"object"`
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
} //	@name	ApplicationDraft

type draftRequest struct {
	Data    json.RawMessage `json:"data" binding:"required" swaggertype:"object"`
	Version int             `json:"version" binding:"required,min=1"`
} //	@name	SaveApplicationDraft

// Поля незавершённой анкеты допускают частичное заполнение. Файлы отправляются отдельно с анкетой,
// токены внешних систем по-прежнему живут только во вкладке до отправки.
type draftFields struct {
	ProductCode *string `json:"product_code"`
	Requested   *struct {
		Amount     *int64 `json:"amount"`
		TermMonths *int   `json:"term_months"`
	} `json:"requested"`
	Purpose           *purpose           `json:"purpose"`
	Age               *int               `json:"age"`
	MaritalStatus     *string            `json:"marital_status"`
	ChildrenCount     *int               `json:"children_count"`
	WithDirector      *bool              `json:"with_director"`
	Director          *personRequest     `json:"director"`
	OtherBankBIKs     []string           `json:"other_bank_biks"`
	Collateral        *collateralRequest `json:"collateral"`
	Consents          map[string]string  `json:"consents"`
	TaxScope          []string           `json:"tax_scope"`
	POANumber         *string            `json:"poa_number"`
	RepresentativeINN *string            `json:"representative_inn"`
	Scoring           *scoringRequest    `json:"scoring"`
}

func draftActor(c *gin.Context) string { u, _ := currentUser(c); return u.ID }

// @Summary	Полная сохранённая анкета черновика, без подстановки отсутствующих полей
// @Tags		applications
// @Produce	json
// @Security	bearer
// @Param		id					path		string	true	"id заявки"
// @Success	200					{object}	draftResponse
// @Failure	401,403,404,409,500	{object}	problem
// @Router		/applications/{id}/draft [get]
func (h *DraftHandler) get(c *gin.Context) {
	draft, err := h.svc.Get(c.Request.Context(), c.Param("id"), draftActor(c))
	h.respond(c, draft, err)
}

// @Summary	Сохранить незавершённую анкету в БД без отправки заявки
// @Tags		applications
// @Accept		json
// @Produce	json
// @Security	bearer
// @Param		id							path		string			true	"id заявки"
// @Param		body						body		draftRequest	true	"Поля и версия черновика"
// @Success	200							{object}	draftResponse
// @Failure	400,401,403,404,409,413,500	{object}	problem
// @Router		/applications/{id}/draft [put]
func (h *DraftHandler) put(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	var req draftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(c, validationProblem(http.StatusRequestEntityTooLarge, "черновик больше 128 КиБ", nil))
			return
		}
		writeProblem(c, bindingProblem(err))
		return
	}
	var fields draftFields
	decoder := json.NewDecoder(bytes.NewReader(req.Data))
	decoder.DisallowUnknownFields()
	if len(req.Data) == 0 || req.Data[0] != '{' {
		writeProblem(c, validationProblem(http.StatusBadRequest, "data — объект полей анкеты", nil))
		return
	}
	if err := decoder.Decode(&fields); err != nil {
		writeProblem(c, validationProblem(http.StatusBadRequest, "неверные поля черновика: "+err.Error(), nil))
		return
	}
	draft, err := h.svc.Save(c.Request.Context(), c.Param("id"), draftActor(c), req.Data, req.Version)
	h.respond(c, draft, err)
}

func (h *DraftHandler) respond(c *gin.Context, draft domain.SavedDraft, err error) {
	switch {
	case err == nil:
		c.JSON(http.StatusOK, draftResponse{draft.Data, draft.Version, draft.UpdatedAt})
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("заявка не найдена"))
	case errors.Is(err, service.ErrUnauthorized):
		writeProblem(c, unauthorizedProblem("войдите как автор заявки"))
	case errors.Is(err, service.ErrForbidden):
		writeProblem(c, forbiddenProblem("черновик доступен только автору заявки"))
	case errors.Is(err, domain.ErrApplicationNotDraft):
		writeProblem(c, conflictProblem("анкету уже отправили"))
	case errors.Is(err, domain.ErrVersionConflict):
		writeProblem(c, conflictProblem("черновик изменён в другой вкладке; обновите страницу"))
	default:
		h.log.Error("draft", "err", err)
		writeProblem(c, internalProblem())
	}
}
