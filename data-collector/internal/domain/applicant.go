package domain

import (
	"errors"
	"slices"
	"time"
)

// Кто оформляет кредит (users + company_members), за какую компанию (companies)
// и сама заявка (applications).

// RepresentativeRole — кем представитель приходится компании.
type RepresentativeRole string

const (
	RoleOwner              RepresentativeRole = "OWNER"               // собственник бизнеса
	RoleDirector           RepresentativeRole = "DIRECTOR"            // генеральный директор
	RoleCFO                RepresentativeRole = "CFO"                 // финансовый директор
	RoleAccountant         RepresentativeRole = "ACCOUNTANT"          // бухгалтер
	RoleFinanceEmployee    RepresentativeRole = "FINANCE_EMPLOYEE"    // сотрудник, отвечающий за финансирование
	RoleBankingEmployee    RepresentativeRole = "BANKING_EMPLOYEE"    // сотрудник, работающий с банковскими продуктами
	RoleAuthorizedEmployee RepresentativeRole = "AUTHORIZED_EMPLOYEE" // иной сотрудник, уполномоченный оформлять заявку
)

// RepresentativeRoles — все роли в порядке пунктов формы.
var RepresentativeRoles = []RepresentativeRole{RoleOwner, RoleDirector, RoleCFO, RoleAccountant, RoleFinanceEmployee,
	RoleBankingEmployee, RoleAuthorizedEmployee}

// Valid сообщает, входит ли значение в допустимый перечень.
func (r RepresentativeRole) Valid() bool { return slices.Contains(RepresentativeRoles, r) }

// CompanyType — юрлицо или ИП.
type CompanyType string

const (
	CompanyLegalEntity    CompanyType = "LEGAL_ENTITY"
	CompanySoleProprietor CompanyType = "SOLE_PROPRIETOR"
)

// CompanyTypeByINN — тип по длине ИНН: 12 цифр — ИП, иначе юрлицо.
func CompanyTypeByINN(inn string) CompanyType {
	if len(inn) == 12 {
		return CompanySoleProprietor
	}
	return CompanyLegalEntity
}

// CanSign — может ли представитель подписывать договор без доверенности:
// руководитель, а у ИП — и сам предприниматель. Остальным нужна МЧД.
func CanSign(role RepresentativeRole, t CompanyType) bool {
	return role == RoleDirector || (t == CompanySoleProprietor && role == RoleOwner)
}

// Representative — человек, который оформляет заявку от имени компании.
type Representative struct {
	UserID   string // usr_<ULID>
	FullName string
	INN      string // ИНН физлица; может быть пустым
	Role     RepresentativeRole
	CanSign  bool
	HasLogin bool // тестовый пользователь (persona): входит по мок-входу, анкету без его токена не отправить
}

// Company — компания или ИП; в API — Applicant.
type Company struct {
	ID           string // apl_<ULID>
	INN          string
	Type         CompanyType
	Name         string
	OGRN         string
	AlreadyKnown bool // при регистрации: компания уже была в системе
}

// CompanyCard — реквизиты компании из ЕГРЮЛ/ЕГРИП: дописываются в companies после сбора.
type CompanyCard struct {
	INN              string
	Type             CompanyType
	OGRN             string
	KPP              string
	FullName         string
	ShortName        string
	RegistrationDate string // YYYY-MM-DD
	DirectorName     string
	Region           string
	TerminatedOn     string // YYYY-MM-DD: компания ликвидирована (в выписке есть дата прекращения)
}

// Name — как называть компанию: короткое имя, если есть.
func (c CompanyCard) Name() string {
	if c.ShortName != "" {
		return c.ShortName
	}
	return c.FullName
}

// ApplicationStatus — статус заявки по FSM (docs/fsm.md).
type ApplicationStatus string

const (
	ApplicationDraft          ApplicationStatus = "DRAFT"            // создана при регистрации
	ApplicationSubmitted      ApplicationStatus = "SUBMITTED"        // анкета отправлена, сбор в очереди
	ApplicationEnriching      ApplicationStatus = "ENRICHING"        // worker опрашивает системы
	ApplicationVerifying      ApplicationStatus = "VERIFYING"        // данные собраны, ждём проверку подписей
	ApplicationScoring        ApplicationStatus = "SCORING"          // заявку оценивает credit-engine
	ApplicationManualReview   ApplicationStatus = "MANUAL_REVIEW"    // решение за андеррайтером
	ApplicationWaitingForInfo ApplicationStatus = "WAITING_FOR_INFO" // банк ждёт ответы клиента
	ApplicationOffered        ApplicationStatus = "OFFERED"          // клиенту выданы предложения
	ApplicationRejected       ApplicationStatus = "REJECTED"         // отказ банка
)

// ApplicationStatuses — статусы, которые ставит модуль, в порядке конвейера.
var ApplicationStatuses = []ApplicationStatus{ApplicationDraft, ApplicationSubmitted, ApplicationEnriching,
	ApplicationVerifying, ApplicationScoring, ApplicationManualReview, ApplicationWaitingForInfo, ApplicationOffered,
	ApplicationRejected}

// Valid — статус из тех, что ставит модуль.
func (s ApplicationStatus) Valid() bool { return slices.Contains(ApplicationStatuses, s) }

// Цели кредита (PurposeCode в api/openapi.yaml).
var PurposeCodes = []string{"WORKING_CAPITAL", "EQUIPMENT", "VEHICLE", "REAL_ESTATE", "REFINANCING",
	"BUSINESS_EXPANSION", "TENDER", "OTHER"}

// RequestedTerms — запрошенные сумма (₽) и срок.
type RequestedTerms struct {
	Amount     int64
	TermMonths int
}

// Purpose — цель кредита.
type Purpose struct {
	Code        string
	Description string
}

// Application — заявка на кредит.
type Application struct {
	ID               string // app_<ULID>
	Number           string // A-2026-000123
	Status           ApplicationStatus
	Company          Company
	Representative   Representative
	ProductCode      string
	Requested        *RequestedTerms
	Purpose          *Purpose
	Documents        []Document
	CollectionTaskID string // последняя задача на сбор; пусто — анкету ещё не отправляли
	CreatedAt        time.Time
	SubmittedAt      time.Time // нулевое — не отправлена
	UpdatedAt        time.Time
}

// Registration — что пишется одной транзакцией при регистрации: пользователь, компания,
// их связь и черновик заявки. Id новых строк; существующие пользователь (по ИНН),
// компания (по ИНН) и черновик переиспользуются.
type Registration struct {
	UserID string
	// ExistingUser — UserID — уже заведённый пользователь (вошёл по токену): его не ищут по ИНН и не создают.
	ExistingUser   bool
	CompanyID      string
	ApplicationID  string
	Representative Representative
	Company        Company
	CreatedAt      time.Time
}

// Submission — что пишется одной транзакцией при отправке анкеты: документы, заявка
// (SUBMITTED, анкета, сумма и срок), задача на сбор и события в outbox.
type Submission struct {
	ApplicationID string
	ActorID       string // кто отправил: usr_…
	Questionnaire Questionnaire
	ProductCode   string
	Requested     *RequestedTerms
	Purpose       *Purpose
	Scoring       *ScoringAnswers
	Documents     []Document
	Task          CollectionTask
	SubmittedAt   time.Time
}

// ErrApplicationNotDraft — заявка уже отправлена: анкету второй раз не принять.
var ErrApplicationNotDraft = errors.New("application is not a draft")
