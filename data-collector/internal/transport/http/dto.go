package httptransport

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// createQuestionnaireRequest — тело POST /questionnaires: анкета заёмщика и всё,
// что нужно для сбора данных о компании из внешних систем.
// Указатели нужны, чтобы явный 0 отличался от отсутствующего поля.
type createQuestionnaireRequest struct {
	Age           *int    `json:"age" binding:"required" example:"42" minimum:"18" maximum:"100"`
	MaritalStatus *string `json:"marital_status" binding:"required" enums:"SINGLE,MARRIED,DIVORCED,DIVORCED_MULTIPLE" example:"MARRIED"`
	ChildrenCount *int    `json:"children_count" binding:"required" example:"2" minimum:"0" maximum:"20"`

	// ИНН компании (10 цифр) или ИП (12 цифр) — ключ ко всем системам.
	INN *string `json:"inn" binding:"required" example:"7799000012"`
	// Руководитель; у ИП — сам предприниматель. Без него не запрашиваются данные физлица:
	// БКИ, ФССП, Федресурс, Госуслуги, дисквалификация, санкции.
	Director *personRequest  `json:"director"`
	Consents consentsRequest `json:"consents"`
	// БИК банков, где у клиента ещё есть счета: мультибанкинг и блокировки ФНС.
	OtherBankBIKs []string `json:"other_bank_biks" example:"044525225"`
	// Токены продавца: {"wildberries": "…", "ozon": "…"}. Мок принимает любой непустой токен (кроме начинающегося с invalid).
	MarketplaceTokens map[string]string `json:"marketplace_tokens" swaggertype:"object"`
	// Токен оператора фискальных данных (онлайн-кассы).
	OFDToken   string            `json:"ofd_token" example:"demo-ofd-token"`
	Collateral collateralRequest `json:"collateral"`
	// Номер МЧД, если заявку подаёт представитель, и его ИНН.
	POANumber         string `json:"poa_number" example:"0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37"`
	RepresentativeINN string `json:"representative_inn" example:"779900010333"`
} //	@name	CreateQuestionnaireRequest

type personRequest struct {
	INN        string `json:"inn" example:"779900010139"`
	LastName   string `json:"last_name" example:"Ковалёв"`
	FirstName  string `json:"first_name" example:"Игорь"`
	MiddleName string `json:"middle_name" example:"Сергеевич"`
	BirthDate  string `json:"birth_date" example:"1984-07-19"`
} //	@name	Person

type consentRequest struct {
	ID         string   `json:"id" example:"CNS-2026-000781"`
	SignedAt   string   `json:"signed_at" example:"2026-10-21T14:02:11Z"`
	ValidUntil string   `json:"valid_until" example:"2027-04-21"`
	Scope      []string `json:"scope" enums:"DECLARATIONS,ENS_BALANCE"` // только у consents.tax
} //	@name	Consent

// consentsRequest — согласия субъектов. Без согласия запрос в систему не отправляется.
type consentsRequest struct {
	BKICompany *consentRequest `json:"bki_company"` // компании на запрос в БКИ
	BKIPerson  *consentRequest `json:"bki_person"`  // руководителя на запрос в БКИ
	Tax        *consentRequest `json:"tax"`         // на налоговую отчётность
} //	@name	Consents

// collateralRequest — предлагаемый залог.
type collateralRequest struct {
	CadastralNumbers []string `json:"cadastral_numbers" example:"50:21:0110203:845"` // недвижимость → ЕГРН
	VINs             []string `json:"vins" example:"XTC652005N1498213"`              // транспорт → реестр залогов
	Serials          []string `json:"serials" example:"JCB3CX2023118457"`            // техника → реестр залогов
} //	@name	Collateral

func (r createQuestionnaireRequest) toCollectInput() domain.CollectInput {
	in := domain.CollectInput{
		INN:               *r.INN,
		BKICompanyConsent: r.Consents.BKICompany.toDomain(),
		BKIPersonConsent:  r.Consents.BKIPerson.toDomain(),
		TaxConsent:        r.Consents.Tax.toDomain(),
		OtherBankBIKs:     r.OtherBankBIKs,
		MarketplaceTokens: r.MarketplaceTokens,
		OFDToken:          r.OFDToken,
		Collateral: domain.Collateral{
			CadastralNumbers: r.Collateral.CadastralNumbers,
			VINs:             r.Collateral.VINs,
			Serials:          r.Collateral.Serials,
		},
		POANumber:         r.POANumber,
		RepresentativeINN: r.RepresentativeINN,
	}
	if d := r.Director; d != nil {
		in.Director = &domain.Person{INN: d.INN, LastName: d.LastName, FirstName: d.FirstName,
			MiddleName: d.MiddleName, BirthDate: d.BirthDate}
	}
	return in
}

func (c *consentRequest) toDomain() *domain.Consent {
	if c == nil {
		return nil
	}
	return &domain.Consent{ID: c.ID, SignedAt: c.SignedAt, ValidUntil: c.ValidUntil, Scope: c.Scope}
}

type questionnaireResponse struct {
	ID            string             `json:"id"`
	Age           int                `json:"age"`
	MaritalStatus string             `json:"marital_status"`
	ChildrenCount int                `json:"children_count"`
	CreatedAt     time.Time          `json:"created_at"`
	Collection    collectionResponse `json:"collection"`
} //	@name	Questionnaire

// collectionResponse — итог сбора данных по анкете. Сами ответы систем лежат
// в таблице raw_collections (строка с этим id), здесь — только состояние.
type collectionResponse struct {
	ID         string                 `json:"id" example:"col_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	INN        string                 `json:"inn" example:"7799000012"`
	StartedAt  time.Time              `json:"started_at"`
	FinishedAt time.Time              `json:"finished_at"`
	Sources    []systemResultResponse `json:"sources"`
} //	@name	Collection

type systemResultResponse struct {
	Source      string `json:"source" enums:"CORE_BANKING,FNS,BKI,COMPLIANCE,FSSP,KAD,FEDRESURS,M4D,TAX_REPORTS,PLEDGE_REGISTRY,EGRN,GOSUSLUGI,OPEN_BANKING,MARKETPLACES,OFD,ZAKUPKI,SIGNALS,CBR,HH"`
	State       string `json:"state" enums:"DONE,PARTIAL,FAILED,SKIPPED"`
	Reason      string `json:"reason,omitempty"`
	Calls       int    `json:"calls"`
	FailedCalls int    `json:"failed_calls"`
	DurationMs  int64  `json:"duration_ms"`
} //	@name	CollectionSource

func toQuestionnaireResponse(res service.Created) questionnaireResponse {
	q, rc := res.Questionnaire, res.Collection
	sources := make([]systemResultResponse, 0, len(rc.Systems))
	for _, s := range rc.Systems {
		sources = append(sources, systemResultResponse{
			Source:      string(s.System),
			State:       string(s.State),
			Reason:      s.Reason,
			Calls:       len(s.Calls),
			FailedCalls: s.FailedCalls(),
			DurationMs:  s.Duration.Milliseconds(),
		})
	}
	return questionnaireResponse{
		ID:            q.ID,
		Age:           q.Age,
		MaritalStatus: string(q.MaritalStatus),
		ChildrenCount: q.ChildrenCount,
		CreatedAt:     q.CreatedAt,
		Collection: collectionResponse{
			ID:         rc.ID,
			INN:        rc.Input.INN,
			StartedAt:  rc.StartedAt,
			FinishedAt: rc.FinishedAt,
			Sources:    sources,
		},
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
