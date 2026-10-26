package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// Рабочее место андеррайтера: очередь, досье, решение по заявке на ручном рассмотрении.

var (
	// ErrAssignedToOther — заявку уже взял другой андеррайтер.
	ErrAssignedToOther = errors.New("application is assigned to another underwriter")
	// ErrNotInReview — заявка не на ручном рассмотрении: взять или решить её нельзя.
	ErrNotInReview = errors.New("application is not in manual review")
	// ErrVersionConflict — заявка изменилась после того, как андеррайтер её открыл (If-Match).
	ErrVersionConflict = errors.New("application version conflict")
)

// QueueFilter — какие заявки показать в очереди.
type QueueFilter struct {
	UserID   string // кто смотрит: для assignee=me и счётчика «мои»
	Statuses []ApplicationStatus
	Assignee string // "" — все, "me", "none" или id андеррайтера
	Limit    int
	Offset   int
}

// UserRef — пользователь в ответах: id и ФИО.
type UserRef struct {
	ID       string
	FullName string
}

// QueueItem — строка очереди.
type QueueItem struct {
	ApplicationID   string
	Number          string
	Status          ApplicationStatus
	Version         int
	Company         Company
	ProductCode     string
	Requested       *RequestedTerms
	Grade           string
	Score           *int
	ReviewReasons   []string // причины ручного рассмотрения от движка
	DegradedSources []System // системы, которые не ответили или ответили частично
	Assignee        *UserRef
	SubmittedAt     time.Time
	UpdatedAt       time.Time
	DeadlineAt      *time.Time
}

// QueueCounters — счётчики над очередью.
type QueueCounters struct {
	ManualReview   int // на ручном рассмотрении
	Mine           int // из них — мои
	DueToday       int // из них — срок сегодня или уже прошёл
	WaitingForInfo int // ждут ответа клиента
}

// DecisionRecord — последняя оценка заявки (decisions): автоматическая и решение андеррайтера.
type DecisionRecord struct {
	ID            string
	Grade         string
	Score         *float64
	MaxAmount     *int64
	AutoOutcome   string
	Outcome       string
	DecidedBy     string
	Details       json.RawMessage // ответ движка, запрос, находки, предложенные условия
	ClientReasons json.RawMessage
	Comment       string
	CreatedAt     time.Time
}

// OfferRecord — предложение клиенту (offers).
type OfferRecord struct {
	ID             string
	Kind           string // REQUESTED, ALTERNATIVE
	Status         string // ACTIVE, ACCEPTED, WITHDRAWN, EXPIRED
	Amount         int64
	TermMonths     int
	Rate           float64
	MonthlyPayment int64
	Terms          json.RawMessage // product_code, collateral_required, guarantee_required, conditions, recommended
	ValidUntil     time.Time
}

// InfoRequestRecord — вопрос клиенту (info_requests).
type InfoRequestRecord struct {
	ID         string
	Status     string // OPEN, ANSWERED, CANCELLED
	Required   bool
	Question   json.RawMessage // title, description, target_field, input, documents, source
	Answer     json.RawMessage
	CreatedAt  time.Time
	AnsweredAt *time.Time
	DueAt      *time.Time
}

// Dossier — всё по заявке для андеррайтера.
type Dossier struct {
	Application  Application
	Version      int
	Assignee     *UserRef
	DeadlineAt   *time.Time
	Card         *CompanyCard
	Sources      []SystemResult             // итог сбора по системам, без сырых данных
	SourcesAt    *time.Time                 // когда закончился сбор
	Systems      map[System]json.RawMessage // сырые ответы систем для вкладки «Компания»
	Decision     *DecisionRecord
	Offers       []OfferRecord
	InfoRequests []InfoRequestRecord
	Others       []QueueItem // другие заявки компании
	Events       []ApplicationEvent
	Staff        map[string]UserRef // сотрудники банка из журнала: actor_id → ФИО
}

// OfferInput — предложение андеррайтера при одобрении.
type OfferInput struct {
	Kind               string // REQUESTED, ALTERNATIVE
	ProductCode        string
	Amount             int64
	TermMonths         int
	Rate               float64
	CollateralRequired []string
	GuaranteeRequired  bool
	Conditions         []string
	ValidDays          int
}

// InfoRequestInput — вопрос андеррайтера клиенту.
type InfoRequestInput struct {
	Title       string
	Description string
	TargetField string
	Required    bool
	Input       json.RawMessage // {type, options}
	Documents   json.RawMessage // {required, accepted_types, hint}
}

// ClientReason — причина отказа для клиента.
type ClientReason struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// Adjustment — экспертная поправка к скорингу.
type Adjustment struct {
	Code    string `json:"code"`
	Points  int    `json:"points"`
	Comment string `json:"comment,omitempty"`
}

// UnderwriterDecision — решение андеррайтера по заявке на ручном рассмотрении.
type UnderwriterDecision struct {
	ApplicationID   string
	UserID          string
	Action          string // APPROVE, REJECT, REQUEST_INFO
	IfMatch         *int   // версия заявки, которую видел андеррайтер
	IdempotencyKey  string
	Offers          []OfferInput
	ClientReasons   []ClientReason
	InfoRequests    []InfoRequestInput
	Adjustments     []Adjustment
	InternalComment string
	Now             time.Time
	ReapplyAfter    time.Time // при отказе — когда можно подать снова
	InfoDueAt       time.Time // при запросе данных — срок ответа
	Outcome         string    // APPROVED, APPROVED_WITH_CHANGES, REJECTED, NEED_INFO — считает сервис
	Status          ApplicationStatus
	Message         string // текст события для клиента
}

var (
	// ErrNotWaitingForInfo — банк не ждёт ответов: заявка не в WAITING_FOR_INFO.
	ErrNotWaitingForInfo = errors.New("application is not waiting for info")
	// ErrQuestionClosed — на вопрос уже ответили или его отменили.
	ErrQuestionClosed         = errors.New("info request is not open")
	ErrInvalidAnswerDocuments = errors.New("answer documents are unavailable or belong to another question")
)

// InfoAnswer — ответ клиента на вопрос банка.
type InfoAnswer struct {
	ApplicationID string
	RequestID     string
	ActorID       string
	CompanyINN    string
	Value         json.RawMessage // значение по типу вопроса; nil — только документы
	Comment       string
	Documents     []Document     // приложенные файлы (info_request_id — этот вопрос)
	DocumentIDs   []string       // документы, загруженные и проверенные до отправки ответа
	ScoringPatch  map[string]any // поле анкеты scoring → значение, если вопрос его уточняет
	Now           time.Time
	ReviewSLA     time.Duration // если вопросы задал андеррайтер — срок его повторного рассмотрения
}
