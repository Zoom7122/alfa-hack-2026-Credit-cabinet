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
	Create(ctx context.Context, in service.CreateInput) (service.Created, error)
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
//	@Summary		Принять анкету и собрать данные о компании
//	@Description	Проверяет анкету и данные для сбора, сохраняет анкету и опрашивает все внешние системы (эмулятор mock-api).
//	@Description	Сырые ответы систем сохраняются строкой в таблицу raw_collections: одна система — одна колонка.
//	@Description	В ответе — только состояние по каждой системе. Отказ системы не ошибка запроса: ответ всё равно 201.
//	@Description	Системы без нужных данных (нет согласия, токена, залога) пропускаются со state=SKIPPED.
//	@Tags			questionnaires
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createQuestionnaireRequest	true	"Анкета и данные для сбора"
//	@Success		201		{object}	questionnaireResponse
//	@Failure		400		{object}	problem	"битый JSON, нет обязательного поля (age, marital_status, children_count, inn) или неверный тип"
//	@Failure		422		{object}	problem	"нарушено правило анкеты или неверный формат данных для сбора (ИНН, БИК, VIN, даты)"
//	@Failure		500		{object}	problem
//	@Router			/questionnaires [post]
func (h *QuestionnaireHandler) create(c *gin.Context) {
	var req createQuestionnaireRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}

	res, err := h.svc.Create(c.Request.Context(), service.CreateInput{
		Age:           *req.Age,
		MaritalStatus: domain.MaritalStatus(*req.MaritalStatus),
		ChildrenCount: *req.ChildrenCount,
		Company:       req.toCollectInput(),
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

	for _, s := range res.Collection.Systems {
		if s.State == domain.CollectFailed || s.State == domain.CollectPartial {
			h.log.WarnContext(c.Request.Context(), "external system failed", "collection", res.Collection.ID,
				"source", s.System, "state", s.State, "reason", s.Reason)
		}
	}
	c.JSON(http.StatusCreated, toQuestionnaireResponse(res))
}
