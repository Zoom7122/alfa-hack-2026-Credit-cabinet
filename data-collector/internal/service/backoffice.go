package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// BackofficeRepository — данные рабочего места (repository/postgres/backoffice.go).
type BackofficeRepository interface {
	Queue(ctx context.Context, f domain.QueueFilter) ([]domain.QueueItem, error)
	Counters(ctx context.Context, userID string) (domain.QueueCounters, error)
	QueueItem(ctx context.Context, appID string) (domain.QueueItem, bool, error)
	Dossier(ctx context.Context, appID string) (domain.Dossier, bool, error)
	Assign(ctx context.Context, appID, userID string) (bool, error)
	Decide(ctx context.Context, cmd domain.UnderwriterDecision) (bool, error)
	// DecisionApplied — по какой заявке уже применено решение с этим ключом; false — ключ новый.
	DecisionApplied(ctx context.Context, idempotencyKey string) (string, bool, error)
}

const (
	maxQueue          = 100
	defaultQueue      = 50
	defaultOfferDays  = 14
	maxOfferDays      = 30
	infoAnswerDays    = 5
	reapplyAfterDays  = 30
	maxCommentRunes   = 2000
	maxAdjustment     = 100
	maxDecisionOffers = 3
)

// BackofficeService — рабочее место андеррайтера: очередь, досье, взять в работу, решение.
type BackofficeService struct {
	repo BackofficeRepository
	now  func() time.Time
}

func NewBackofficeService(repo BackofficeRepository) *BackofficeService {
	return &BackofficeService{repo: repo, now: time.Now}
}

// Queue — очередь и счётчики; next — смещение следующей страницы, 0 — страниц больше нет.
func (s *BackofficeService) Queue(ctx context.Context, userID string, f domain.QueueFilter) ([]domain.QueueItem, domain.QueueCounters, int, error) {
	if f.Limit <= 0 {
		f.Limit = defaultQueue
	}
	f.Limit = min(f.Limit, maxQueue)
	if f.Assignee == "me" {
		f.Assignee = userID
	}
	// на страницу берём на одну больше: так видно, есть ли следующая
	page := f
	page.Limit++
	items, err := s.repo.Queue(ctx, page)
	if err != nil {
		return nil, domain.QueueCounters{}, 0, fmt.Errorf("queue: %w", err)
	}
	next := 0
	if len(items) > f.Limit {
		items, next = items[:f.Limit], f.Offset+f.Limit
	}
	counters, err := s.repo.Counters(ctx, userID)
	if err != nil {
		return nil, domain.QueueCounters{}, 0, fmt.Errorf("counters: %w", err)
	}
	return items, counters, next, nil
}

// Dossier — досье заявки. ErrNotFound — заявки нет.
func (s *BackofficeService) Dossier(ctx context.Context, appID string) (domain.Dossier, error) {
	d, ok, err := s.repo.Dossier(ctx, appID)
	if err != nil {
		return domain.Dossier{}, fmt.Errorf("dossier: %w", err)
	}
	if !ok {
		return domain.Dossier{}, ErrNotFound
	}
	return d, nil
}

// Assign — взять заявку в работу. ErrNotFound; domain.ErrNotInReview, domain.ErrAssignedToOther — нельзя.
func (s *BackofficeService) Assign(ctx context.Context, userID, appID string) (domain.QueueItem, error) {
	ok, err := s.repo.Assign(ctx, appID, userID)
	if err != nil {
		return domain.QueueItem{}, err
	}
	if !ok {
		return domain.QueueItem{}, ErrNotFound
	}
	item, ok, err := s.repo.QueueItem(ctx, appID)
	if err != nil {
		return domain.QueueItem{}, err
	}
	if !ok {
		return domain.QueueItem{}, ErrNotFound
	}
	return item, nil
}

// DecisionInput — решение андеррайтера из запроса.
type DecisionInput struct {
	Action          string
	IfMatch         *int
	IdempotencyKey  string
	Offers          []domain.OfferInput
	ClientReasons   []domain.ClientReason
	InfoRequests    []domain.InfoRequestInput
	Adjustments     []domain.Adjustment
	InternalComment string
}

// Decide проверяет и применяет решение; возвращает досье после него. *ValidationError — ошибки полей;
// ErrNotFound; domain.ErrNotInReview, domain.ErrAssignedToOther, domain.ErrVersionConflict.
func (s *BackofficeService) Decide(ctx context.Context, userID, appID string, in DecisionInput) (domain.Dossier, error) {
	key := strings.TrimSpace(in.IdempotencyKey)
	if key != "" {
		prev, done, err := s.repo.DecisionApplied(ctx, key)
		if err != nil {
			return domain.Dossier{}, err
		}
		if done && prev == appID {
			return s.Dossier(ctx, appID) // повтор: решение уже применено
		}
		if done {
			return domain.Dossier{}, &ValidationError{Fields: []FieldError{{Field: "Idempotency-Key", Code: "reused",
				Message: "ключ уже использован для другой заявки"}}}
		}
	}
	d, err := s.Dossier(ctx, appID)
	if err != nil {
		return domain.Dossier{}, err
	}
	// статус, исполнитель и версия — до проверки полей: их ошибка важнее (repo проверит ещё раз под блокировкой)
	switch {
	case d.Application.Status != domain.ApplicationManualReview:
		return domain.Dossier{}, domain.ErrNotInReview
	case d.Assignee != nil && d.Assignee.ID != userID:
		return domain.Dossier{}, domain.ErrAssignedToOther
	case in.IfMatch != nil && *in.IfMatch != d.Version:
		return domain.Dossier{}, domain.ErrVersionConflict
	}
	now := s.now().UTC()
	cmd := domain.UnderwriterDecision{ApplicationID: appID, UserID: userID, Action: in.Action, IfMatch: in.IfMatch,
		IdempotencyKey: key, ClientReasons: in.ClientReasons, InfoRequests: in.InfoRequests,
		Adjustments: in.Adjustments, InternalComment: strings.TrimSpace(in.InternalComment), Now: now}
	if fields := s.prepare(&cmd, in, d); len(fields) > 0 {
		return domain.Dossier{}, &ValidationError{Fields: fields}
	}
	ok, err := s.repo.Decide(ctx, cmd)
	if err != nil {
		return domain.Dossier{}, err
	}
	if !ok {
		return domain.Dossier{}, ErrNotFound
	}
	return s.Dossier(ctx, appID)
}

// prepare проверяет поля решения и дополняет команду: итог, статус, сроки, условия по умолчанию.
func (s *BackofficeService) prepare(cmd *domain.UnderwriterDecision, in DecisionInput, d domain.Dossier) []FieldError {
	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}
	if utf8.RuneCountInString(cmd.InternalComment) > maxCommentRunes {
		add("internal_comment", "too_long", fmt.Sprintf("не длиннее %d символов", maxCommentRunes))
	}
	for i, a := range cmd.Adjustments {
		if strings.TrimSpace(a.Code) == "" {
			add(fmt.Sprintf("expert_adjustments[%d].code", i), "required", "обязательное поле")
		}
		if a.Points == 0 || a.Points > maxAdjustment || a.Points < -maxAdjustment {
			add(fmt.Sprintf("expert_adjustments[%d].points", i), "out_of_range", fmt.Sprintf("от −%d до %d, не ноль", maxAdjustment, maxAdjustment))
		}
	}
	switch cmd.Action {
	case "APPROVE":
		offers := in.Offers
		if len(offers) == 0 {
			if o := proposedOffer(d); o != nil {
				offers = []domain.OfferInput{*o}
			}
		}
		if len(offers) == 0 {
			add("offers", "required", "у движка нет предложенных условий — укажите их")
		}
		if len(offers) > maxDecisionOffers {
			add("offers", "too_many", fmt.Sprintf("не больше %d предложений", maxDecisionOffers))
		}
		for i := range offers {
			o := &offers[i]
			f := fmt.Sprintf("offers[%d].", i)
			if o.Kind == "" {
				o.Kind = "REQUESTED"
			}
			if o.ProductCode == "" {
				o.ProductCode = d.Application.ProductCode
			}
			if o.ValidDays == 0 {
				o.ValidDays = defaultOfferDays
			}
			if o.Kind != "REQUESTED" && o.Kind != "ALTERNATIVE" {
				add(f+"kind", "invalid_value", "допустимые значения: REQUESTED, ALTERNATIVE")
			}
			if o.Amount <= 0 {
				add(f+"amount", "out_of_range", "сумма больше нуля")
			}
			if o.TermMonths <= 0 || o.TermMonths > maxTermMonths {
				add(f+"term_months", "out_of_range", fmt.Sprintf("срок от 1 до %d месяцев", maxTermMonths))
			}
			if o.Rate <= 0 || o.Rate > 100 {
				add(f+"rate_annual", "out_of_range", "ставка от 0 до 100 % годовых")
			}
			if o.ValidDays < 1 || o.ValidDays > maxOfferDays {
				add(f+"valid_days", "out_of_range", fmt.Sprintf("от 1 до %d дней", maxOfferDays))
			}
		}
		cmd.Offers = offers
		cmd.Status, cmd.Outcome, cmd.Message = domain.ApplicationOffered, "APPROVED", "Банк одобрил кредит — посмотрите предложение"
		if r := d.Application.Requested; len(offers) > 0 && (r == nil || offers[0].Amount != r.Amount || offers[0].TermMonths != r.TermMonths) {
			cmd.Outcome = "APPROVED_WITH_CHANGES"
		}
	case "REJECT":
		if len(cmd.ClientReasons) == 0 {
			add("client_reasons", "required", "укажите причину отказа для клиента")
		}
		for i, r := range cmd.ClientReasons {
			if strings.TrimSpace(r.Title) == "" {
				add(fmt.Sprintf("client_reasons[%d].title", i), "required", "обязательное поле")
			}
		}
		cmd.Status, cmd.Outcome, cmd.Message = domain.ApplicationRejected, "REJECTED", "Банк отказал в кредите"
		cmd.ReapplyAfter = cmd.Now.AddDate(0, 0, reapplyAfterDays)
	case "REQUEST_INFO":
		if len(cmd.InfoRequests) == 0 {
			add("info_requests", "required", "задайте хотя бы один вопрос")
		}
		for i, q := range cmd.InfoRequests {
			if strings.TrimSpace(q.Title) == "" {
				add(fmt.Sprintf("info_requests[%d].title", i), "required", "обязательное поле")
			}
		}
		cmd.Status, cmd.Outcome, cmd.Message = domain.ApplicationWaitingForInfo, "NEED_INFO", "Банку нужны дополнительные сведения"
		cmd.InfoDueAt = cmd.Now.AddDate(0, 0, infoAnswerDays)
	default:
		add("action", "invalid_value", "допустимые значения: APPROVE, REJECT, REQUEST_INFO")
	}
	return fields
}

// proposedOffer — условия, которые предложил движок (decisions.details.proposed_offer).
func proposedOffer(d domain.Dossier) *domain.OfferInput {
	if d.Decision == nil || len(d.Decision.Details) == 0 {
		return nil
	}
	var details struct {
		Proposed *decision.Offer `json:"proposed_offer"`
	}
	if json.Unmarshal(d.Decision.Details, &details) != nil || details.Proposed == nil || details.Proposed.AmountRUB <= 0 {
		return nil
	}
	p := details.Proposed
	return &domain.OfferInput{Kind: "REQUESTED", ProductCode: d.Application.ProductCode, Amount: p.AmountRUB,
		TermMonths: p.TermMonths, Rate: p.AnnualRate, ValidDays: defaultOfferDays}
}
