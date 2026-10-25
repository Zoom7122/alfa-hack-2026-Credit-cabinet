package bank

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

// Notifier's REST adapter must reject every HTTP status except 202 before
// returning an acknowledgement. Service additionally verifies ID and accepted.
type Notifier interface {
	Notify(context.Context, contracts.AdminEvent) (contracts.EventAck, error)
}

type entry struct {
	check        *contracts.BankCheckRequest
	manual       *contracts.ManualReviewRequest
	checkResult  contracts.BankCheckResponse
	manualResult contracts.ManualReviewResponse
	event        contracts.AdminEvent
	delivered    bool
}
type Loan struct {
	ApplicationID, LoanID string
	Borrower              contracts.Borrower
	Terms                 contracts.LoanTerms
}
type Service struct {
	mu        sync.Mutex
	state     contracts.BankState
	projector *Projector
	clock     clock.Clock
	notifier  Notifier
	entries   map[string]*entry
}

func NewService(p config.Policy, state contracts.BankState, c clock.Clock, n Notifier) (*Service, error) {
	if err := config.ValidateBankState(state); err != nil {
		return nil, err
	}
	projector, err := NewProjector(p)
	if err != nil {
		return nil, err
	}
	for code, value := range norms(state, p.LargeExposureThreshold) {
		if value == nil {
			return nil, fmt.Errorf("invalid initial bank norm %s", code)
		}
	}
	if c == nil {
		return nil, fmt.Errorf("bank clock is required")
	}
	return &Service{state: copyState(state), projector: projector, clock: c, notifier: n, entries: make(map[string]*entry)}, nil
}

func (s *Service) Check(ctx context.Context, request contracts.BankCheckRequest) (contracts.BankCheckResponse, error) {
	if err := envelope(request.PolicyID, request.ApplicationID, request.Borrower); err != nil {
		return contracts.BankCheckResponse{}, err
	}
	if request.PolicyID != s.projector.policy.ID {
		return contracts.BankCheckResponse{}, conflict("POLICY_MISMATCH")
	}
	r, err := copyJSON(request)
	if err != nil {
		return contracts.BankCheckResponse{}, &Error{Status: 422, Code: "INVALID_VALUE"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return contracts.BankCheckResponse{}, err
	}
	if old, exists := s.entries[r.ApplicationID]; exists {
		if old.check == nil || !reflect.DeepEqual(*old.check, r) {
			return contracts.BankCheckResponse{}, conflict("APPLICATION_CONFLICT")
		}
		s.deliver(ctx, old)
		return copyJSON(old.checkResult)
	}
	if slices.Contains(s.state.IssuedApplicationIds, r.ApplicationID) {
		return contracts.BankCheckResponse{}, conflict("APPLICATION_CONFLICT")
	}
	if err := validatePayload(r.Borrower, r.RequestedTerms, r.Score); err != nil {
		return contracts.BankCheckResponse{}, err
	}
	if err := validateTerms(r.Terms); err != nil {
		return contracts.BankCheckResponse{}, err
	}
	projection, err := s.projector.Project(s.state, r.ApplicationID, r.Borrower.ID, r.Terms)
	if err != nil {
		return contracts.BankCheckResponse{}, err
	}
	kind, verdict := contracts.EventTypeBankReview, contracts.BankVerdictBankReview
	var loanID *string
	problems := []contracts.CheckResult{}
	reasons := []contracts.Reason{}
	for _, check := range projection.Checks {
		if !check.Passed {
			problems = append(problems, check)
			reasons = append(reasons, contracts.Reason{Code: check.Code, Message: check.Detail, Fields: []string{}})
		}
	}
	if projection.Issued {
		kind = contracts.EventTypeLoanIssued
		verdict = contracts.BankVerdictIssued
		loanID = ptr("loan-" + r.ApplicationID)
	}
	event := s.event(r.ApplicationID, kind, r.Borrower, r.Score, r.RequestedTerms)
	event.Terms = &r.Terms
	event.LoanID = loanID
	event.Problems = problems
	event.Reasons = reasons
	event.Selection = &r.Selection
	e := &entry{check: &r, event: event, checkResult: contracts.BankCheckResponse{ApplicationID: r.ApplicationID, Verdict: verdict, LoanID: loanID, Checks: projection.Checks, EventID: event.EventID, NotificationStatus: contracts.NotificationStatusPending}}
	// State, loan/replay record and event are committed before notification.
	if projection.Issued {
		s.state = projection.State
	}
	s.entries[r.ApplicationID] = e
	s.deliver(ctx, e)
	return copyJSON(e.checkResult)
}

func (s *Service) ManualReview(ctx context.Context, request contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error) {
	if err := envelope(request.PolicyID, request.ApplicationID, request.Borrower); err != nil {
		return contracts.ManualReviewResponse{}, err
	}
	if request.PolicyID != s.projector.policy.ID {
		return contracts.ManualReviewResponse{}, conflict("POLICY_MISMATCH")
	}
	r, err := copyJSON(request)
	if err != nil {
		return contracts.ManualReviewResponse{}, &Error{Status: 422, Code: "INVALID_VALUE"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return contracts.ManualReviewResponse{}, err
	}
	if old, exists := s.entries[r.ApplicationID]; exists {
		if old.manual == nil || !reflect.DeepEqual(*old.manual, r) {
			return contracts.ManualReviewResponse{}, conflict("APPLICATION_CONFLICT")
		}
		s.deliver(ctx, old)
		return copyJSON(old.manualResult)
	}
	if slices.Contains(s.state.IssuedApplicationIds, r.ApplicationID) {
		return contracts.ManualReviewResponse{}, conflict("APPLICATION_CONFLICT")
	}
	if err := validatePayload(r.Borrower, r.RequestedTerms, r.Score); err != nil {
		return contracts.ManualReviewResponse{}, err
	}
	if len(r.Reasons) == 0 {
		return contracts.ManualReviewResponse{}, &Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{"reasons"}}
	}
	for _, reason := range r.Reasons {
		if strings.TrimSpace(reason.Code) == "" || strings.TrimSpace(reason.Message) == "" {
			return contracts.ManualReviewResponse{}, &Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{"reasons"}}
		}
	}
	event := s.event(r.ApplicationID, contracts.EventTypeBorrowerReview, r.Borrower, r.Score, r.RequestedTerms)
	event.Reasons = r.Reasons
	e := &entry{manual: &r, event: event, manualResult: contracts.ManualReviewResponse{ApplicationID: r.ApplicationID, EventID: event.EventID, NotificationStatus: contracts.NotificationStatusPending}}
	s.entries[r.ApplicationID] = e
	s.deliver(ctx, e)
	return copyJSON(e.manualResult)
}

func (s *Service) event(id string, kind contracts.EventType, b contracts.Borrower, score contracts.ScoreResult, requested contracts.CreditRequest) contracts.AdminEvent {
	return contracts.AdminEvent{EventID: id + ":" + string(kind), OccurredAt: s.clock.Now().UTC().Format(time.RFC3339), Kind: kind, ApplicationID: id, Borrower: b, Score: score, RequestedTerms: requested, Reasons: []contracts.Reason{}, Problems: []contracts.CheckResult{}}
}
func (s *Service) deliver(ctx context.Context, e *entry) {
	if e.delivered || s.notifier == nil {
		return
	}
	event, err := copyJSON(e.event)
	if err != nil {
		return
	} // Only finite validated DTOs are stored.
	ack, err := s.notifier.Notify(ctx, event)
	if err != nil || !ack.Accepted || ack.EventID != e.event.EventID {
		return
	}
	e.delivered = true
	e.checkResult.NotificationStatus = contracts.NotificationStatusDelivered
	e.manualResult.NotificationStatus = contracts.NotificationStatusDelivered
}

func (s *Service) Snapshot() contracts.BankStateResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string]float64, 10)
	for code, value := range norms(s.state, s.projector.policy.LargeExposureThreshold) {
		values[code] = *value
	}
	return contracts.BankStateResponse{PolicyID: s.projector.policy.ID, State: copyState(s.state), Norms: values}
}
func (s *Service) Loans() ([]Loan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	loans := []Loan{}
	for _, id := range slices.Sorted(maps.Keys(s.entries)) {
		e := s.entries[id]
		if e.check != nil && e.checkResult.Verdict == contracts.BankVerdictIssued {
			loans = append(loans, Loan{ApplicationID: id, LoanID: *e.checkResult.LoanID, Borrower: e.check.Borrower, Terms: e.check.Terms})
		}
	}
	return copyJSON(loans)
}
func (s *Service) PendingEvents() ([]contracts.AdminEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := []contracts.AdminEvent{}
	for _, id := range slices.Sorted(maps.Keys(s.entries)) {
		if e := s.entries[id]; !e.delivered {
			events = append(events, e.event)
		}
	}
	return copyJSON(events)
}
func conflict(code string) error { return &Error{Status: 409, Code: code} }
