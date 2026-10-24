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
//	@Summary		Принять анкету и поставить сбор данных о компании в очередь
//	@Description	Проверяет анкету и данные для сбора, сохраняет анкету и создаёт задачу на сбор (status=PENDING)
//	@Description	вместе с событием в outbox — одной транзакцией. Publisher отправляет событие в Kafka, worker
//	@Description	опрашивает все внешние системы (эмулятор mock-api) и пишет сырые ответы строкой в raw_collections.
//	@Description	Состояние сбора — GET /collections/{id} (ссылка в заголовке Location).
//	@Description	Системы без нужных данных (нет согласия, токена, залога, банков) не опрашиваются: они перечислены в skipped_sources.
//	@Tags			questionnaires
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createQuestionnaireRequest	true	"Анкета и данные для сбора"
//	@Success		202		{object}	questionnaireResponse
//	@Header			202		{string}	Location	"/api/v1/collections/{id} — состояние задачи на сбор"
//	@Failure		400		{object}	problem		"битый JSON, нет обязательного поля (age, marital_status, children_count, inn) или неверный тип"
//	@Failure		422		{object}	problem		"нарушено правило анкеты или неверный формат данных для сбора (ИНН, БИК, VIN, даты)"
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

	c.Header("Location", collectionsPath+"/"+res.Task.ID)
	c.JSON(http.StatusAccepted, toQuestionnaireResponse(res))
}
