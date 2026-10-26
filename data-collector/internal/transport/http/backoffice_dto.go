package httptransport

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

type userRefResponse struct {
	ID       string `json:"id" example:"usr_underwriter_sokolova"`
	FullName string `json:"full_name" example:"Соколова Мария Игоревна"`
} //	@name	UserRef

type boApplicantRef struct {
	ID        string `json:"id"`
	ShortName string `json:"short_name"`
	INN       string `json:"inn"`
	Type      string `json:"type" enums:"LEGAL_ENTITY,SOLE_PROPRIETOR"`
} //	@name	BoApplicantRef

// boQueueItemResponse — строка очереди андеррайтера.
type boQueueItemResponse struct {
	ID          string          `json:"id"`
	Number      string          `json:"number"`
	Status      string          `json:"status"`
	Version     int             `json:"version"`
	Applicant   boApplicantRef  `json:"applicant"`
	ProductCode string          `json:"product_code,omitempty"`
	Requested   *requestedTerms `json:"requested,omitempty"`
	Grade       string          `json:"grade,omitempty" enums:"A,B,C,D"`
	Score       *int            `json:"score,omitempty"`
	// Причины ручного рассмотрения от credit-engine — только у MANUAL_REVIEW.
	ManualReviewReasons []string         `json:"manual_review_reasons"`
	DegradedSources     []string         `json:"degraded_sources"` // системы, ответившие с ошибкой или частично
	Assignee            *userRefResponse `json:"assignee"`
	SubmittedAt         time.Time        `json:"submitted_at"`
	SLADueAt            *time.Time       `json:"sla_due_at"`
} //	@name	BoApplicationListItem

type boCountersResponse struct {
	ManualReview   int `json:"manual_review"`
	Mine           int `json:"mine"`
	DueToday       int `json:"due_today"`
	WaitingForInfo int `json:"waiting_for_info"`
} //	@name	BoQueueCounters

type boQueueResponse struct {
	Items []boQueueItemResponse `json:"items"`
	// Курсор следующей страницы; null — страниц больше нет.
	NextCursor *string            `json:"next_cursor"`
	Counters   boCountersResponse `json:"counters"`
} //	@name	BoQueue

func toQueueItem(it domain.QueueItem) boQueueItemResponse {
	out := boQueueItemResponse{ID: it.ApplicationID, Number: it.Number, Status: string(it.Status), Version: it.Version,
		Applicant:   boApplicantRef{ID: it.Company.ID, ShortName: it.Company.Name, INN: it.Company.INN, Type: string(it.Company.Type)},
		ProductCode: it.ProductCode, Grade: it.Grade, Score: it.Score, ManualReviewReasons: nonNilStrings(it.ReviewReasons),
		DegradedSources: []string{}, SubmittedAt: it.SubmittedAt, SLADueAt: it.DeadlineAt}
	if r := it.Requested; r != nil {
		out.Requested = &requestedTerms{Amount: r.Amount, TermMonths: r.TermMonths}
	}
	for _, s := range it.DegradedSources {
		out.DegradedSources = append(out.DegradedSources, string(s))
	}
	if a := it.Assignee; a != nil {
		out.Assignee = &userRefResponse{ID: a.ID, FullName: a.FullName}
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---------------------------------------------------------------- досье

type boApplicantResponse struct {
	ID               string `json:"id"`
	INN              string `json:"inn"`
	Type             string `json:"type"`
	ShortName        string `json:"short_name"`
	FullName         string `json:"full_name,omitempty"`
	OGRN             string `json:"ogrn,omitempty"`
	KPP              string `json:"kpp,omitempty"`
	RegistrationDate string `json:"registration_date,omitempty"`
	DirectorName     string `json:"director_name,omitempty"`
	Region           string `json:"region,omitempty"`
	TerminatedOn     string `json:"terminated_on,omitempty"`
	decision.CompanyFacts
} //	@name	BoApplicant

type boSourceResponse struct {
	Code        string     `json:"code"` // система data-collector: CORE_BANKING, FNS, BKI, …
	State       string     `json:"state" enums:"DONE,PARTIAL,FAILED,SKIPPED"`
	Criticality string     `json:"criticality" enums:"CRITICAL,NORMAL"`
	Reason      *string    `json:"reason"`
	Calls       int        `json:"calls"`
	FailedCalls int        `json:"failed_calls"`
	DurationMs  int64      `json:"duration_ms"`
	FetchedAt   *time.Time `json:"fetched_at"`
} //	@name	BoSource

type boFindingResponse struct {
	decision.Finding
	Severity string `json:"severity" enums:"INFO,WARN,STOP"`
} //	@name	BoFinding

type boFactorResponse struct {
	Code      string          `json:"code"`
	Title     string          `json:"title"`
	Part      string          `json:"part" enums:"QUESTIONNAIRE,ECONOMICS"`
	Answer    string          `json:"answer,omitempty"`
	Value     json.RawMessage `json:"value,omitempty" swaggertype:"number"`
	Points    float64         `json:"points"`
	MaxPoints int             `json:"max_points"`
} //	@name	BoScoreFactor

type boScoreResponse struct {
	ModelVersion string             `json:"model_version"`
	Engine       string             `json:"engine" enums:"SCORECARD"`
	Total        int                `json:"total"`
	RawTotal     float64            `json:"raw_total"`
	Grade        string             `json:"grade" enums:"A,B,C,D"`
	Factors      []boFactorResponse `json:"factors"`
	ComputedAt   time.Time          `json:"computed_at"`
} //	@name	BoScore

type boReasonResponse struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields"`
} //	@name	BoEngineReason

type boTermsResponse struct {
	Amount         int64   `json:"amount"`
	TermMonths     int     `json:"term_months"`
	RateAnnual     float64 `json:"rate_annual"`
	MonthlyPayment int64   `json:"monthly_payment"`
	Confirmed      bool    `json:"confirmed"` // условия подтверждены движком и банком (APPROVED)
} //	@name	BoTerms

type boEngineDecisionResponse struct {
	// Итог движка для заявки: APPROVED, REJECTED, MANUAL_REVIEW, NEED_INFO.
	Outcome       string             `json:"outcome"`
	EngineStatus  string             `json:"engine_status" enums:"APPROVED,REJECTED,UNDER_REVIEW,NEED_INFO"`
	ReviewReason  string             `json:"review_reason,omitempty" enums:"BORROWER,BANK"`
	Reasons       []boReasonResponse `json:"reasons"`
	ProposedTerms *boTermsResponse   `json:"proposed_terms"`
} //	@name	BoEngineDecision

type boUnderwriterDecisionResponse struct {
	Outcome   string           `json:"outcome" enums:"APPROVED,APPROVED_WITH_CHANGES,REJECTED,NEED_INFO"`
	DecidedBy *userRefResponse `json:"decided_by"`
	Comment   string           `json:"comment,omitempty"`
	DecidedAt *time.Time       `json:"decided_at,omitempty"`
} //	@name	BoUnderwriterDecision

type boOfferResponse struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind" enums:"REQUESTED,ALTERNATIVE,RETENTION"`
	State              string    `json:"state" enums:"ACTIVE,ACCEPTED,WITHDRAWN,EXPIRED"`
	Recommended        bool      `json:"recommended"`
	ProductCode        string    `json:"product_code,omitempty"`
	Amount             int64     `json:"amount"`
	TermMonths         int       `json:"term_months"`
	RateAnnual         float64   `json:"rate_annual"`
	MonthlyPayment     int64     `json:"monthly_payment"`
	CollateralRequired []string  `json:"collateral_required"`
	GuaranteeRequired  bool      `json:"guarantee_required"`
	Conditions         []string  `json:"conditions"`
	ValidUntil         time.Time `json:"valid_until"`
} //	@name	Offer

type boInfoRequestResponse struct {
	ID          string          `json:"id"`
	State       string          `json:"state" enums:"OPEN,ANSWERED,CANCELLED"`
	Required    bool            `json:"required"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	TargetField string          `json:"target_field,omitempty"`
	Input       json.RawMessage `json:"input" swaggertype:"object"`
	Documents   json.RawMessage `json:"documents,omitempty" swaggertype:"object"`
	Source      string          `json:"source" enums:"credit-engine,underwriter"`
	CreatedAt   time.Time       `json:"created_at"`
	DueAt       *time.Time      `json:"due_at"`
	Answer      json.RawMessage `json:"answer" swaggertype:"object"`
	AnsweredAt  *time.Time      `json:"answered_at"`
} //	@name	InfoRequest

type boTimelineEventResponse struct {
	timelineEventResponse
	Actor    *userRefResponse `json:"actor"`    // сотрудник банка
	Internal bool             `json:"internal"` // служебное: клиенту не показывается
} //	@name	BoTimelineEvent

// boDossierResponse — досье заявки для андеррайтера.
type boDossierResponse struct {
	Application         applicationResponse            `json:"application"`
	Version             int                            `json:"version"` // для If-Match решения
	Assignee            *userRefResponse               `json:"assignee"`
	SLADueAt            *time.Time                     `json:"sla_due_at"`
	Applicant           boApplicantResponse            `json:"applicant"`
	Sources             []boSourceResponse             `json:"sources"`
	Findings            []boFindingResponse            `json:"findings"`
	Score               *boScoreResponse               `json:"score"`
	EngineDecision      *boEngineDecisionResponse      `json:"engine_decision"`
	UnderwriterDecision *boUnderwriterDecisionResponse `json:"underwriter_decision"`
	Offers              []boOfferResponse              `json:"offers"`
	InfoRequests        []boInfoRequestResponse        `json:"info_requests"`
	OtherApplications   []boQueueItemResponse          `json:"other_applications"`
	Timeline            []boTimelineEventResponse      `json:"timeline"`
} //	@name	BoDossier

// критичные системы: без них решение надёжно не принять
var criticalSystems = map[domain.System]bool{domain.SystemCoreBanking: true, domain.SystemFNS: true, domain.SystemBKI: true,
	domain.SystemCompliance: true, domain.SystemFedresurs: true}

// staffMessages — тексты служебных событий для журнала андеррайтера.
var staffMessages = map[string]string{
	"uw.assigned":                   "Взял(а) заявку в работу",
	"credit_engine.loan_issued":     "credit-engine: выдача учтена в состоянии банка",
	"credit_engine.borrower_review": "credit-engine: заявка передана на ручное рассмотрение",
	"credit_engine.bank_review":     "credit-engine: сделка требует проверки банком",
	"scoring.started":               "Заявка передана в credit-engine",
}

// engineDetails — что лежит в decisions.details (service/decision.go).
type engineDetails struct {
	Engine        *decision.Response `json:"engine"`
	Findings      []decision.Finding `json:"findings"`
	ProposedOffer *decision.Offer    `json:"proposed_offer"`
	Underwriter   *struct {
		DecidedAt *time.Time `json:"decided_at"`
	} `json:"underwriter"`
}

func toDossier(d domain.Dossier, now time.Time) boDossierResponse {
	out := boDossierResponse{Application: toApplicationResponse(d.Application), Version: d.Version, SLADueAt: d.DeadlineAt,
		Sources: []boSourceResponse{}, Findings: []boFindingResponse{}, Offers: []boOfferResponse{},
		InfoRequests: []boInfoRequestResponse{}, OtherApplications: []boQueueItemResponse{}, Timeline: []boTimelineEventResponse{}}
	if a := d.Assignee; a != nil {
		out.Assignee = &userRefResponse{ID: a.ID, FullName: a.FullName}
	}

	c := d.Application.Company
	out.Applicant = boApplicantResponse{ID: c.ID, INN: c.INN, Type: string(c.Type), ShortName: c.Name, OGRN: c.OGRN,
		CompanyFacts: decision.Profile(d.Systems, c.ID, now)}
	if card := d.Card; card != nil {
		out.Applicant.FullName, out.Applicant.KPP, out.Applicant.RegistrationDate = card.FullName, card.KPP, card.RegistrationDate
		out.Applicant.DirectorName, out.Applicant.Region, out.Applicant.TerminatedOn = card.DirectorName, card.Region, card.TerminatedOn
		if card.OGRN != "" {
			out.Applicant.OGRN = card.OGRN
		}
	}

	for _, s := range d.Sources {
		crit := "NORMAL"
		if criticalSystems[s.System] {
			crit = "CRITICAL"
		}
		var reason *string
		if s.Reason != "" {
			reason = &s.Reason
		}
		out.Sources = append(out.Sources, boSourceResponse{Code: string(s.System), State: string(s.State), Criticality: crit,
			Reason: reason, Calls: len(s.Calls), FailedCalls: s.FailedCalls(), DurationMs: s.Duration.Milliseconds(), FetchedAt: d.SourcesAt})
	}

	if dec := d.Decision; dec != nil {
		var det engineDetails
		_ = json.Unmarshal(dec.Details, &det)
		for _, f := range det.Findings {
			sev := "INFO"
			if f.Material {
				sev = "WARN"
			}
			out.Findings = append(out.Findings, boFindingResponse{Finding: f, Severity: sev})
		}
		if e := det.Engine; e != nil {
			ed := &boEngineDecisionResponse{Outcome: dec.AutoOutcome, EngineStatus: e.Status, Reasons: []boReasonResponse{}}
			if e.ReviewReason != nil {
				ed.ReviewReason = *e.ReviewReason
			}
			for _, r := range e.Reasons {
				ed.Reasons = append(ed.Reasons, boReasonResponse{Code: r.Code, Message: r.Message, Fields: nonNilStrings(r.Fields)})
			}
			if p := det.ProposedOffer; p != nil && p.AmountRUB > 0 {
				ed.ProposedTerms = &boTermsResponse{Amount: p.AmountRUB, TermMonths: p.TermMonths, RateAnnual: p.AnnualRate,
					MonthlyPayment: p.MonthlyPayment, Confirmed: p.Confirmed}
			}
			out.EngineDecision = ed
			if s := e.Score; s != nil {
				sc := &boScoreResponse{ModelVersion: s.Model, Engine: "SCORECARD", Total: s.Total, RawTotal: s.RawTotal, Grade: s.Grade,
					Factors: []boFactorResponse{}, ComputedAt: dec.CreatedAt}
				for _, part := range []struct {
					name string
					p    decision.ScorePart
				}{{"QUESTIONNAIRE", s.Questionnaire}, {"ECONOMICS", s.Economics}} {
					for _, cr := range part.p.Criteria {
						sc.Factors = append(sc.Factors, boFactorResponse{Code: cr.ID, Title: decision.CriterionTitle(cr.ID), Part: part.name,
							Answer: cr.Answer, Value: cr.Value, Points: cr.Points, MaxPoints: cr.MaxPoints})
					}
				}
				out.Score = sc
			}
		}
		if dec.Outcome != "" {
			uw := &boUnderwriterDecisionResponse{Outcome: dec.Outcome, Comment: dec.Comment}
			if u, ok := d.Staff[dec.DecidedBy]; ok {
				uw.DecidedBy = &userRefResponse{ID: u.ID, FullName: u.FullName}
			} else if dec.DecidedBy != "" {
				uw.DecidedBy = &userRefResponse{ID: dec.DecidedBy}
			}
			if det.Underwriter != nil {
				uw.DecidedAt = det.Underwriter.DecidedAt
			}
			out.UnderwriterDecision = uw
		}
	}

	for _, o := range d.Offers {
		out.Offers = append(out.Offers, toOffer(o))
	}
	for _, q := range d.InfoRequests {
		out.InfoRequests = append(out.InfoRequests, toInfoRequest(q))
	}
	for _, o := range d.Others {
		out.OtherApplications = append(out.OtherApplications, toQueueItem(o))
	}
	for _, e := range d.Events {
		out.Timeline = append(out.Timeline, toStaffEvent(e, d.Staff))
	}
	return out
}

func toOffer(o domain.OfferRecord) boOfferResponse {
	var t struct {
		ProductCode        string   `json:"product_code"`
		CollateralRequired []string `json:"collateral_required"`
		GuaranteeRequired  bool     `json:"guarantee_required"`
		Conditions         []string `json:"conditions"`
		Recommended        *bool    `json:"recommended"`
	}
	_ = json.Unmarshal(o.Terms, &t)
	return boOfferResponse{ID: o.ID, Kind: o.Kind, State: o.Status, Recommended: t.Recommended == nil || *t.Recommended,
		ProductCode: t.ProductCode, Amount: o.Amount, TermMonths: o.TermMonths, RateAnnual: o.Rate, MonthlyPayment: o.MonthlyPayment,
		CollateralRequired: nonNilStrings(t.CollateralRequired), GuaranteeRequired: t.GuaranteeRequired,
		Conditions: nonNilStrings(t.Conditions), ValidUntil: o.ValidUntil}
}

func toInfoRequest(q domain.InfoRequestRecord) boInfoRequestResponse {
	var qq struct {
		Title       string          `json:"title"`
		Description string          `json:"description"`
		TargetField string          `json:"target_field"`
		Fields      []string        `json:"fields"`
		Input       json.RawMessage `json:"input"`
		Documents   json.RawMessage `json:"documents"`
		Source      string          `json:"source"`
	}
	_ = json.Unmarshal(q.Question, &qq)
	if qq.TargetField == "" && len(qq.Fields) > 0 {
		qq.TargetField = strings.Join(qq.Fields, ",")
	}
	if len(qq.Input) == 0 {
		qq.Input = json.RawMessage(`{"type":"TEXT"}`)
	}
	answer := q.Answer
	if len(answer) == 0 {
		answer = json.RawMessage("null")
	}
	return boInfoRequestResponse{ID: q.ID, State: q.Status, Required: q.Required, Title: qq.Title, Description: qq.Description,
		TargetField: qq.TargetField, Input: qq.Input, Documents: qq.Documents, Source: qq.Source, CreatedAt: q.CreatedAt,
		DueAt: q.DueAt, Answer: answer, AnsweredAt: q.AnsweredAt}
}

// toStaffEvent — событие журнала для андеррайтера: со служебными, с автором-сотрудником.
func toStaffEvent(e domain.ApplicationEvent, staff map[string]domain.UserRef) boTimelineEventResponse {
	ev, clientVisible := toTimelineEvent(e)
	if !clientVisible {
		ev = timelineEventResponse{ID: e.ID, At: e.At, Type: e.Type, FromStatus: string(e.From), ToStatus: string(e.To),
			ActorKind: actorKind(e), Message: staffMessage(e)}
	}
	out := boTimelineEventResponse{timelineEventResponse: ev, Internal: !clientVisible}
	if u, ok := staff[e.ActorID]; ok {
		out.Actor = &userRefResponse{ID: u.ID, FullName: u.FullName}
	}
	return out
}

func staffMessage(e domain.ApplicationEvent) string {
	switch e.Type {
	case "uw.comment":
		var p struct {
			Comment string `json:"comment"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		return "Внутренний комментарий: " + p.Comment
	case "uw.adjustment":
		var a domain.Adjustment
		_ = json.Unmarshal(e.Payload, &a)
		msg := "Поправка к скорингу: " + a.Code + " " + signed(a.Points)
		if a.Comment != "" {
			msg += " — " + a.Comment
		}
		return msg
	}
	if m, ok := staffMessages[e.Type]; ok {
		return m
	}
	return e.Type
}

func signed(v int) string {
	if v > 0 {
		return "+" + strconv.Itoa(v)
	}
	return strconv.Itoa(v)
}

// decisionRequest — решение андеррайтера (BoDecisionInput).
type decisionRequest struct {
	Action          string                `json:"action" binding:"required" enums:"APPROVE,REJECT,REQUEST_INFO"`
	Offers          []offerInputRequest   `json:"offers"`
	ClientReasons   []clientReasonRequest `json:"client_reasons"`
	InfoRequests    []infoRequestInput    `json:"info_requests"`
	Adjustments     []adjustmentRequest   `json:"expert_adjustments"`
	InternalComment string                `json:"internal_comment"`
} //	@name	BoDecisionInput

type offerInputRequest struct {
	Kind               string   `json:"kind" enums:"REQUESTED,ALTERNATIVE"`
	ProductCode        string   `json:"product_code"`
	Amount             int64    `json:"amount"`
	TermMonths         int      `json:"term_months"`
	RateAnnual         float64  `json:"rate_annual"`
	CollateralRequired []string `json:"collateral_required"`
	GuaranteeRequired  bool     `json:"guarantee_required"`
	Conditions         []string `json:"conditions"`
	ValidDays          int      `json:"valid_days"`
} //	@name	OfferInput

type clientReasonRequest struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
} //	@name	ClientReason

type infoRequestInput struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	TargetField string          `json:"target_field"`
	Required    bool            `json:"required"`
	Input       json.RawMessage `json:"input" swaggertype:"object"`
	Documents   json.RawMessage `json:"documents" swaggertype:"object"`
} //	@name	InfoRequestInput

type adjustmentRequest struct {
	Code    string `json:"code"`
	Points  int    `json:"points"`
	Comment string `json:"comment"`
} //	@name	ExpertAdjustment
