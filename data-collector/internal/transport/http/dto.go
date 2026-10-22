package httptransport

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// createQuestionnaireRequest — тело POST /questionnaires.
// Указатели нужны, чтобы явный 0 отличался от отсутствующего поля.
type createQuestionnaireRequest struct {
	Age           *int    `json:"age" binding:"required"`
	MaritalStatus *string `json:"marital_status" binding:"required"`
	ChildrenCount *int    `json:"children_count" binding:"required"`
}

type questionnaireResponse struct {
	ID            string    `json:"id"`
	Age           int       `json:"age"`
	MaritalStatus string    `json:"marital_status"`
	ChildrenCount int       `json:"children_count"`
	CreatedAt     time.Time `json:"created_at"`
}

func toQuestionnaireResponse(q domain.Questionnaire) questionnaireResponse {
	return questionnaireResponse{
		ID:            q.ID,
		Age:           q.Age,
		MaritalStatus: string(q.MaritalStatus),
		ChildrenCount: q.ChildrenCount,
		CreatedAt:     q.CreatedAt,
	}
}
