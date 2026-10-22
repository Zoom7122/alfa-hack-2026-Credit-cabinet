package httptransport

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// createQuestionnaireRequest — тело POST /questionnaires.
// Указатели нужны, чтобы явный 0 отличался от отсутствующего поля.
type createQuestionnaireRequest struct {
	Age           *int    `json:"age" binding:"required" example:"34" minimum:"18" maximum:"100"`
	MaritalStatus *string `json:"marital_status" binding:"required" enums:"SINGLE,MARRIED,DIVORCED,DIVORCED_MULTIPLE" example:"MARRIED"`
	ChildrenCount *int    `json:"children_count" binding:"required" example:"2" minimum:"0" maximum:"20"`
} //	@name	CreateQuestionnaireRequest

type questionnaireResponse struct {
	ID            string    `json:"id"`
	Age           int       `json:"age"`
	MaritalStatus string    `json:"marital_status"`
	ChildrenCount int       `json:"children_count"`
	CreatedAt     time.Time `json:"created_at"`
} //	@name	Questionnaire

func toQuestionnaireResponse(q domain.Questionnaire) questionnaireResponse {
	return questionnaireResponse{
		ID:            q.ID,
		Age:           q.Age,
		MaritalStatus: string(q.MaritalStatus),
		ChildrenCount: q.ChildrenCount,
		CreatedAt:     q.CreatedAt,
	}
}

// internalDataResponse — ответ GET /check-mock. Верхний уровень в snake_case,
// как весь API; внутри секций — ответы систем банка в их собственном формате.
type internalDataResponse struct {
	INN           string                      `json:"inn"`
	Sources       []sourceResultResponse      `json:"sources"`
	Accounts      *domain.AccountsReport      `json:"accounts"`
	Credits       *domain.CreditReport        `json:"credits"`
	Acquiring     *domain.AcquiringReport     `json:"acquiring"`
	SalaryProject *domain.SalaryProjectReport `json:"salary_project"`
	CardIndex     *domain.CardIndexReport     `json:"card_index"`
} //	@name	InternalData

type sourceResultResponse struct {
	Source     string `json:"source" enums:"ACCOUNTS,CREDITS,ACQUIRING,SALARY_PROJECT,CARD_INDEX"`
	State      string `json:"state" enums:"DONE,FAILED"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
} //	@name	SourceResult

func toInternalDataResponse(d domain.InternalBankData) internalDataResponse {
	sources := make([]sourceResultResponse, 0, len(d.Sources))
	for _, s := range d.Sources {
		sources = append(sources, sourceResultResponse{
			Source:     string(s.Source),
			State:      string(s.State),
			Error:      s.Error,
			DurationMs: s.Duration.Milliseconds(),
		})
	}
	return internalDataResponse{
		INN:           d.INN,
		Sources:       sources,
		Accounts:      d.Accounts,
		Credits:       d.Credits,
		Acquiring:     d.Acquiring,
		SalaryProject: d.SalaryProject,
		CardIndex:     d.CardIndex,
	}
}
