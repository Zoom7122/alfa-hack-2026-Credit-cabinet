// Package httptransport — слой приёма HTTP-запросов (gin).
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

type Handler struct {
	svc QuestionnaireService
	log *slog.Logger
}

func NewHandler(svc QuestionnaireService, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// Register вешает маршруты модуля на группу (обычно /api/v1).
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.POST("/questionnaires", h.createQuestionnaire)
}

func (h *Handler) createQuestionnaire(c *gin.Context) {
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
			fields := make([]problemField, 0, len(verr.Fields))
			for _, f := range verr.Fields {
				fields = append(fields, problemField{Field: f.Field, Message: f.Message, Code: f.Code})
			}
			writeProblem(c, validationProblem(http.StatusUnprocessableEntity, "анкета не прошла проверку", fields))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "create questionnaire", "err", err)
		writeProblem(c, internalProblem())
		return
	}

	c.JSON(http.StatusCreated, toQuestionnaireResponse(q))
}
