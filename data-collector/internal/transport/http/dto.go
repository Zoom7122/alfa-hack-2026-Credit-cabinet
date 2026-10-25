package httptransport

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// registerRequest — тело POST /applicants: кто оформляет заявку и за какую компанию.
type registerRequest struct {
	Representative *representativeRequest `json:"representative" binding:"required"`
	Company        *companyRequest        `json:"company" binding:"required"`
} //	@name	RegisterApplicantRequest

// representativeRequest — человек, который оформляет заявку от имени компании.
type representativeRequest struct {
	FullName *string `json:"full_name" binding:"required" example:"Литвинова Марина Олеговна"`
	// ИНН физлица (12 цифр), необязательно. По нему находится уже зарегистрированный пользователь.
	INN string `json:"inn" example:"779900010333"`
	// OWNER — собственник бизнеса, DIRECTOR — генеральный директор, CFO / ACCOUNTANT — финансовый директор
	// или бухгалтер, FINANCE_EMPLOYEE — сотрудник, отвечающий за финансирование, BANKING_EMPLOYEE — сотрудник,
	// работающий с банковскими продуктами, AUTHORIZED_EMPLOYEE — иной уполномоченный сотрудник.
	Role *string `json:"role" binding:"required" enums:"OWNER,DIRECTOR,CFO,ACCOUNTANT,FINANCE_EMPLOYEE,BANKING_EMPLOYEE,AUTHORIZED_EMPLOYEE" example:"ACCOUNTANT"`
} //	@name	RepresentativeInput

// companyRequest — компания или ИП, за которую оформляется заявка.
type companyRequest struct {
	INN *string `json:"inn" binding:"required" example:"7799000012"` // 10 цифр — ЮЛ, 12 — ИП
	// Название, если известно. После сбора данных заменится названием из ЕГРЮЛ.
	Name string `json:"name" example:"ООО «Северный ветер»"`
} //	@name	CompanyInput

func (r registerRequest) toRegisterInput() service.RegisterInput {
	return service.RegisterInput{
		FullName:    *r.Representative.FullName,
		PersonINN:   r.Representative.INN,
		Role:        domain.RepresentativeRole(*r.Representative.Role),
		CompanyINN:  *r.Company.INN,
		CompanyName: r.Company.Name,
	}
}

// questionnaireRequest — анкета заёмщика, данные заявки и всё, что нужно для сбора данных
// о компании из внешних систем. ИНН компании берётся из заявки.
// Указатели нужны, чтобы явный 0 отличался от отсутствующего поля.
type questionnaireRequest struct {
	Age           *int    `json:"age" binding:"required" example:"42" minimum:"18" maximum:"100"`
	MaritalStatus *string `json:"marital_status" binding:"required" enums:"SINGLE,MARRIED,DIVORCED,DIVORCED_MULTIPLE" example:"MARRIED"`
	ChildrenCount *int    `json:"children_count" binding:"required" example:"2" minimum:"0" maximum:"20"`

	// Продукт, сумма и срок, цель кредита — пишутся в заявку.
	ProductCode string          `json:"product_code" example:"WORKING_CAPITAL"`
	Requested   *requestedTerms `json:"requested"`
	Purpose     *purpose        `json:"purpose"`
	// Тип документа по имени файла из files. Не указан — OTHER (позже тип определит распознавание).
	Documents []documentSpec `json:"documents"`

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
} //	@name	QuestionnaireRequest

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

// requestedTerms — запрошенные сумма (₽) и срок.
type requestedTerms struct {
	Amount     int64 `json:"amount" example:"5000000"`
	TermMonths int   `json:"term_months" example:"24"`
} //	@name	RequestedTerms

// purpose — цель кредита.
type purpose struct {
	Code        string `json:"code" enums:"WORKING_CAPITAL,EQUIPMENT,VEHICLE,REAL_ESTATE,REFINANCING,BUSINESS_EXPANSION,TENDER,OTHER" example:"WORKING_CAPITAL"`
	Description string `json:"description,omitempty" example:"Закупка товара под сезон"`
} //	@name	Purpose

// documentSpec — тип документа для файла из files.
type documentSpec struct {
	FileName string `json:"file_name" example:"balance.pdf"`
	Type     string `json:"type" enums:"FINANCIAL_STATEMENTS,TAX_RETURN,BANK_STATEMENT,MANAGEMENT_REPORT,CHARTER,POWER_OF_ATTORNEY,DIRECTOR_PASSPORT,COLLATERAL_TITLE,EGRN_EXTRACT,LEASE_AGREEMENT,CONTRACT_WITH_CUSTOMER,LOAN_AGREEMENT_DRAFT,OTHER" example:"FINANCIAL_STATEMENTS"`
} //	@name	DocumentSpec

func (r questionnaireRequest) toSubmitInput(files []storage.Staged) service.SubmitInput {
	in := service.SubmitInput{
		Age:           *r.Age,
		MaritalStatus: domain.MaritalStatus(*r.MaritalStatus),
		ChildrenCount: *r.ChildrenCount,
		Company:       r.toCollectInput(),
		ProductCode:   r.ProductCode,
		Files:         files,
	}
	if q := r.Requested; q != nil {
		in.Requested = &domain.RequestedTerms{Amount: q.Amount, TermMonths: q.TermMonths}
	}
	if p := r.Purpose; p != nil {
		in.Purpose = &domain.Purpose{Code: p.Code, Description: p.Description}
	}
	for _, d := range r.Documents {
		in.Documents = append(in.Documents, service.DocumentSpec{FileName: d.FileName, Type: domain.DocumentType(d.Type)})
	}
	return in
}

func (r questionnaireRequest) toCollectInput() domain.CollectInput {
	in := domain.CollectInput{
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

// applicationResponse — заявка: компания, представитель, документы, последняя задача на сбор.
type applicationResponse struct {
	ID     string `json:"id" example:"app_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	Number string `json:"number" example:"A-2026-000001"`
	// DRAFT — создана при регистрации, SUBMITTED — анкета отправлена, ENRICHING — идёт сбор данных,
	// VERIFYING — данные собраны.
	Status         string                 `json:"status" enums:"DRAFT,SUBMITTED,ENRICHING,VERIFYING" example:"DRAFT"`
	Applicant      applicantResponse      `json:"applicant"`
	Representative representativeResponse `json:"representative"`
	ProductCode    string                 `json:"product_code,omitempty" example:"WORKING_CAPITAL"`
	Requested      *requestedTerms        `json:"requested,omitempty"`
	Purpose        *purpose               `json:"purpose,omitempty"`
	Documents      []documentResponse     `json:"documents"`
	// Последняя задача на сбор: GET /collections/{id}.
	CollectionTaskID string     `json:"collection_task_id,omitempty" example:"tsk_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	CreatedAt        time.Time  `json:"created_at"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
} //	@name	Application

// applicantResponse — компания или ИП (в API — Applicant).
type applicantResponse struct {
	ID   string `json:"id" example:"apl_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	INN  string `json:"inn" example:"7799000012"`
	Type string `json:"type" enums:"LEGAL_ENTITY,SOLE_PROPRIETOR"`
	// До сбора — из регистрации (или «ИНН …»), после — из ЕГРЮЛ.
	Name string `json:"name" example:"ООО \"СЕВЕРНЫЙ ВЕТЕР\""`
	OGRN string `json:"ogrn,omitempty" example:"1217700412345"`
	// Только в ответе на регистрацию: компания уже была в системе и переиспользована.
	AlreadyKnown *bool `json:"already_known,omitempty"`
} //	@name	Applicant

// representativeResponse — кто оформляет заявку.
type representativeResponse struct {
	ID       string `json:"id" example:"usr_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	FullName string `json:"full_name" example:"Литвинова Марина Олеговна"`
	INN      string `json:"inn,omitempty" example:"779900010333"`
	Role     string `json:"role" enums:"OWNER,DIRECTOR,CFO,ACCOUNTANT,FINANCE_EMPLOYEE,BANKING_EMPLOYEE,AUTHORIZED_EMPLOYEE"`
	// Может подписать договор без доверенности: директор, у ИП — сам предприниматель.
	CanSign bool `json:"can_sign"`
} //	@name	Representative

// documentResponse — файл заявки. Сам файл лежит в хранилище, проверку подписи и распознавание
// сделают отдельные worker'ы.
type documentResponse struct {
	ID        string `json:"id" example:"doc_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	Type      string `json:"type" example:"FINANCIAL_STATEMENTS"`
	FileName  string `json:"file_name" example:"balance.pdf"`
	MimeType  string `json:"mime_type" example:"application/pdf"`
	SizeBytes int64  `json:"size_bytes" example:"48213"`
	SHA256    string `json:"sha256"`
	// У открепленной подписи (type=SIGNATURE) — какой документ она подписывает.
	SignedDocumentID string    `json:"signed_document_id,omitempty"`
	SignatureState   string    `json:"signature_state" enums:"NOT_SIGNED,PENDING,DONE,FAILED"`
	RecognitionState string    `json:"recognition_state" enums:"NOT_APPLICABLE,PENDING,PROCESSING,DONE,FAILED"`
	UploadedAt       time.Time `json:"uploaded_at"`
} //	@name	Document

// submittedResponse — ответ POST /applications/{id}/questionnaire.
type submittedResponse struct {
	Application   applicationResponse    `json:"application"`
	Questionnaire questionnaireResponse  `json:"questionnaire"`
	Collection    collectionTaskResponse `json:"collection"`
} //	@name	QuestionnaireSubmitted

// questionnaireResponse — сохранённая анкета заёмщика.
type questionnaireResponse struct {
	ID            string    `json:"id" example:"qst_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	Age           int       `json:"age"`
	MaritalStatus string    `json:"marital_status"`
	ChildrenCount int       `json:"children_count"`
	CreatedAt     time.Time `json:"created_at"`
} //	@name	Questionnaire

// collectionTaskResponse — задача на сбор данных о компании. Сами ответы систем лежат
// в таблице raw_collections (строка raw_collection_id), здесь — только состояние.
type collectionTaskResponse struct {
	ID              string `json:"id" example:"tsk_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	ApplicationID   string `json:"application_id,omitempty" example:"app_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	QuestionnaireID string `json:"questionnaire_id" example:"qst_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	INN             string `json:"inn" example:"7799000012"`
	// PENDING — ждёт worker, RUNNING — системы опрашиваются, COMPLETED — сбор сохранён, FAILED — не выполнена.
	Status   string `json:"status" enums:"PENDING,RUNNING,COMPLETED,FAILED" example:"PENDING"`
	Attempts int    `json:"attempts" example:"0"` // сколько раз worker брал задачу
	Error    string `json:"error,omitempty"`      // почему FAILED
	// Системы, для которых в анкете нет данных (согласия, токена, залога, банков): они не опрашиваются.
	SkippedSources []string `json:"skipped_sources" example:"EGRN,MARKETPLACES"`
	// Документы, пришедшие с анкетой.
	DocumentIDs     []string               `json:"document_ids"`
	CreatedAt       time.Time              `json:"created_at"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	FinishedAt      *time.Time             `json:"finished_at,omitempty"`
	RawCollectionID string                 `json:"raw_collection_id,omitempty" example:"col_01JB8Z5X0G6Q9T4V2W8Y3K7M1N"`
	Sources         []systemResultResponse `json:"sources,omitempty"` // только у COMPLETED: состояние по каждой системе
} //	@name	CollectionTask

type systemResultResponse struct {
	Source      string `json:"source" enums:"CORE_BANKING,FNS,BKI,COMPLIANCE,FSSP,KAD,FEDRESURS,M4D,TAX_REPORTS,PLEDGE_REGISTRY,EGRN,GOSUSLUGI,OPEN_BANKING,MARKETPLACES,OFD,ZAKUPKI,SIGNALS,CBR,HH"`
	State       string `json:"state" enums:"DONE,PARTIAL,FAILED,SKIPPED"`
	Reason      string `json:"reason,omitempty"`
	Calls       int    `json:"calls"`
	FailedCalls int    `json:"failed_calls"`
	DurationMs  int64  `json:"duration_ms"`
} //	@name	CollectionSource

func toApplicationResponse(a domain.Application) applicationResponse {
	resp := applicationResponse{
		ID:     a.ID,
		Number: a.Number,
		Status: string(a.Status),
		Applicant: applicantResponse{ID: a.Company.ID, INN: a.Company.INN, Type: string(a.Company.Type),
			Name: a.Company.Name, OGRN: a.Company.OGRN},
		Representative: representativeResponse{ID: a.Representative.UserID, FullName: a.Representative.FullName,
			INN: a.Representative.INN, Role: string(a.Representative.Role), CanSign: a.Representative.CanSign},
		ProductCode:      a.ProductCode,
		Documents:        toDocumentResponses(a.Documents),
		CollectionTaskID: a.CollectionTaskID,
		CreatedAt:        a.CreatedAt,
		SubmittedAt:      optionalTime(a.SubmittedAt),
		UpdatedAt:        a.UpdatedAt,
	}
	if r := a.Requested; r != nil {
		resp.Requested = &requestedTerms{Amount: r.Amount, TermMonths: r.TermMonths}
	}
	if p := a.Purpose; p != nil {
		resp.Purpose = &purpose{Code: p.Code, Description: p.Description}
	}
	return resp
}

func toDocumentResponses(docs []domain.Document) []documentResponse {
	out := make([]documentResponse, 0, len(docs))
	for _, d := range docs {
		out = append(out, documentResponse{ID: d.ID, Type: string(d.Type), FileName: d.FileName, MimeType: d.MimeType,
			SizeBytes: d.SizeBytes, SHA256: d.SHA256, SignedDocumentID: d.SignedDocumentID,
			SignatureState: string(d.SignatureState), RecognitionState: string(d.RecognitionState), UploadedAt: d.UploadedAt})
	}
	return out
}

func toSubmittedResponse(res service.Submitted) submittedResponse {
	q := res.Questionnaire
	return submittedResponse{
		Application: toApplicationResponse(res.Application),
		Questionnaire: questionnaireResponse{ID: q.ID, Age: q.Age, MaritalStatus: string(q.MaritalStatus),
			ChildrenCount: q.ChildrenCount, CreatedAt: q.CreatedAt},
		Collection: toCollectionTaskResponse(res.Task),
	}
}

func toCollectionTaskResponse(t domain.CollectionTask) collectionTaskResponse {
	resp := collectionTaskResponse{
		ID:              t.ID,
		ApplicationID:   t.ApplicationID,
		QuestionnaireID: t.QuestionnaireID,
		INN:             t.Input.INN,
		Status:          string(t.Status),
		Attempts:        t.Attempts,
		Error:           t.Error,
		SkippedSources:  make([]string, 0, len(t.Skipped)),
		DocumentIDs:     append([]string{}, t.DocumentIDs...),
		CreatedAt:       t.CreatedAt,
		StartedAt:       optionalTime(t.StartedAt),
		FinishedAt:      optionalTime(t.FinishedAt),
	}
	for _, sys := range t.Skipped {
		resp.SkippedSources = append(resp.SkippedSources, string(sys))
	}
	if rc := t.Result; rc != nil {
		resp.RawCollectionID = rc.ID
		resp.Sources = make([]systemResultResponse, 0, len(rc.Systems))
		for _, s := range rc.Systems {
			resp.Sources = append(resp.Sources, systemResultResponse{
				Source:      string(s.System),
				State:       string(s.State),
				Reason:      s.Reason,
				Calls:       len(s.Calls),
				FailedCalls: s.FailedCalls(),
				DurationMs:  s.Duration.Milliseconds(),
			})
		}
	}
	return resp
}

// optionalTime — nil для нулевого времени: поле не попадает в ответ.
func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
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

// loginRequest — тело POST /auth/mock-login.
type loginRequest struct {
	UserID *string `json:"user_id" binding:"required" example:"usr_director_north"`
} //	@name	MockLoginRequest

// companyAccessResponse — компания пользователя и его роль в ней.
type companyAccessResponse struct {
	ApplicantID string `json:"applicant_id" validate:"required" example:"apl_north_wind"`
	ShortName   string `json:"short_name" validate:"required" example:"ООО «Северный ветер»"`
	INN         string `json:"inn" validate:"required" example:"7799000012"`
	Role        string `json:"role" validate:"required" enums:"OWNER,DIRECTOR,CFO,ACCOUNTANT,FINANCE_EMPLOYEE,BANKING_EMPLOYEE,AUTHORIZED_EMPLOYEE" example:"DIRECTOR"`
	// Может подписать договор без доверенности: директор, у ИП — сам предприниматель.
	CanSign bool `json:"can_sign" validate:"required"`
} //	@name	CompanyAccess

// meResponse — текущий пользователь (Me в api/openapi.yaml).
type meResponse struct {
	ID        string                  `json:"id" validate:"required" example:"usr_director_north"`
	FullName  string                  `json:"full_name" validate:"required" example:"Ковалёв Игорь Сергеевич"`
	Roles     []string                `json:"roles" validate:"required" enums:"CLIENT,UNDERWRITER"`
	Companies []companyAccessResponse `json:"companies" validate:"required"`
	// Заявки, ждущие подписи пользователя как директора. Подписания в data-collector пока нет — всегда пусто.
	PendingSignatures []string `json:"pending_signatures" validate:"required"`
} //	@name	Me

// testUserResponse — тестовый пользователь для экрана входа (TestUser в api/openapi.yaml).
type testUserResponse struct {
	ID        string                  `json:"id" validate:"required" example:"usr_director_north"`
	FullName  string                  `json:"full_name" validate:"required" example:"Ковалёв Игорь Сергеевич"`
	Roles     []string                `json:"roles" validate:"required" enums:"CLIENT,UNDERWRITER"`
	Persona   string                  `json:"persona" validate:"required" example:"Сценарий A: директор ООО «Северный ветер»"`
	Companies []companyAccessResponse `json:"companies" validate:"required"`
} //	@name	TestUser

type testUsersResponse struct {
	Items []testUserResponse `json:"items" validate:"required"`
} //	@name	TestUsers

// loginResponse — ответ POST /auth/mock-login.
type loginResponse struct {
	// Токен для Authorization: Bearer …
	Token string     `json:"token" validate:"required" example:"mock.usr_director_north"`
	User  meResponse `json:"user" validate:"required"`
} //	@name	MockLoginResponse

func toCompanyAccess(cs []domain.CompanyAccess) []companyAccessResponse {
	out := make([]companyAccessResponse, 0, len(cs))
	for _, c := range cs {
		out = append(out, companyAccessResponse{ApplicantID: c.CompanyID, ShortName: c.Name, INN: c.INN, Role: string(c.Role), CanSign: c.CanSign})
	}
	return out
}

func toMeResponse(u domain.User) meResponse {
	return meResponse{ID: u.ID, FullName: u.FullName, Roles: []string{string(u.Role)}, Companies: toCompanyAccess(u.Companies),
		PendingSignatures: []string{}}
}

func toTestUserResponse(u domain.User) testUserResponse {
	return testUserResponse{ID: u.ID, FullName: u.FullName, Roles: []string{string(u.Role)}, Persona: u.Persona,
		Companies: toCompanyAccess(u.Companies)}
}
