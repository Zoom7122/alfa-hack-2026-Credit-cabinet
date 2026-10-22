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

// QuestionnaireService — то, что хендлеру нужно от бизнес-слоя.
type QuestionnaireService interface {
	Create(ctx context.Context, in service.CreateInput) (domain.Questionnaire, error)
}

// QuestionnaireHandler принимает анкеты заёмщика. Маршруты — в router.go.
type QuestionnaireHandler struct {
	svc QuestionnaireService
	log *slog.Logger
}

func NewQuestionnaireHandler(svc QuestionnaireService, log *slog.Logger) *QuestionnaireHandler {
	return &QuestionnaireHandler{svc: svc, log: log}
}

// create — POST /api/v1/questionnaires.
//
//	@Summary		Принять анкету заёмщика
//	@Description	Проверяет правила и сохраняет анкету. Возраст — полных лет, бакеты считает скоринг.
//	@Tags			questionnaires
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createQuestionnaireRequest	true	"Анкета"
//	@Success		201		{object}	questionnaireResponse
//	@Failure		400		{object}	problem	"битый JSON, нет поля или неверный тип"
//	@Failure		422		{object}	problem	"нарушено правило: возраст 18–100, статус из перечня, детей 0–20"
//	@Failure		500		{object}	problem
//	@Router			/questionnaires [post]
func (h *QuestionnaireHandler) create(c *gin.Context) {
	var req createQuestionnaireRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}

	q, err := h.svc.Create(c.Request.Context(), service.CreateInput{
		Age:           *req.Age,
		MaritalStatus: domain.MaritalStatus(*req.MaritalStatus),
		ChildrenCount: *req.ChildrenCount,
	})
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			writeProblem(c, fromValidationError(verr, "анкета не прошла проверку"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "create questionnaire", "err", err)
		writeProblem(c, internalProblem())
		return
	}

	c.JSON(http.StatusCreated, toQuestionnaireResponse(q))
}
