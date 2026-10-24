// Package scoring validates source facts and implements the fixed MVP v2 model.
// It has no bank, optimizer or notification side effects.
package scoring

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

// Error describes a transport/validation failure, never a credit decision.
type Error struct {
	Status int
	Code   string
	Fields []string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Code, e.Fields) }

var ErrStatementsNotFound = errors.New("statements not found")

// StatementsSource is implemented by the REST adapter in the integration stage.
// It returns ErrStatementsNotFound for 404 and *Error for dependency failures.
type StatementsSource interface {
	Statements(context.Context, string, []int) (contracts.StatementsResponse, error)
}

type Route string

const (
	RouteNeedInfo  Route = "NEED_INFO"
	RouteRejected  Route = "REJECTED"
	RouteManual    Route = "MANUAL_REVIEW"
	RouteSelection Route = "SELECT_OFFER"
)

// CashFlow is ordered by calendar month. Monetary values are unrounded rubles.
// FutureDebt starts in the month following the evaluation month.
type CashFlow struct {
	Receipts, CFADS, HistoricalDebt, Variable, Fixed, MonthlyRequired float64
	PositiveMonths                                                    int
	MonthlyReceipts                                                   [12]float64
	MonthlyCFADS                                                      [12]float64
	FutureDebt                                                        [36]float64
}

// Assessment is an internal routing result. SELECT_OFFER is not an approval.
type Assessment struct {
	Route    Route
	Score    *contracts.ScoreResult
	Reasons  []contracts.Reason
	CashFlow CashFlow
}

type Evaluator struct {
	source StatementsSource
	clock  clock.Clock
}

func NewEvaluator(source StatementsSource, c clock.Clock) (*Evaluator, error) {
	if c == nil {
		return nil, errors.New("scoring: clock is required")
	}
	return &Evaluator{source: source, clock: c}, nil
}

func (e *Evaluator) Evaluate(ctx context.Context, r contracts.DecisionRequest) (Assessment, error) {
	if err := Validate(r); err != nil {
		return Assessment{}, err
	}
	if reasons := knownStops(r); len(reasons) > 0 {
		return Assessment{Route: RouteRejected, Reasons: reasons}, nil
	}
	now := dateOnly(e.clock.Now())
	data := inspectInput(r, now)
	if data.cashConfirmed && data.flow.CFADS <= 0 {
		return Assessment{Route: RouteRejected, Reasons: []contracts.Reason{reason("NONPOSITIVE_VERIFIED_CFADS", "Подтверждённый годовой CFADS неположителен.", "monthly_history")}}, nil
	}
	var statements reports
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		if e.source == nil {
			return Assessment{}, &Error{Status: 502, Code: "STATEMENTS_UNAVAILABLE"}
		}
		years := []int{now.Year() - 2, now.Year() - 1}
		response, err := e.source.Statements(ctx, r.Borrower.ID, years)
		if errors.Is(err, ErrStatementsNotFound) {
			data.missing = append(data.missing, "service_b.annual_reports")
		} else if err != nil {
			var failure *Error
			if errors.As(err, &failure) {
				return Assessment{}, failure
			}
			status := 502
			var timeout interface{ Timeout() bool }
			if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
				status = 504
			}
			return Assessment{}, &Error{Status: status, Code: "STATEMENTS_UNAVAILABLE"}
		} else {
			var missing []string
			statements, missing, err = inspectStatements(response, r.Borrower.ID, now)
			if err != nil {
				return Assessment{}, err
			}
			data.missing = append(data.missing, missing...)
		}
	}
	if len(data.missing) > 0 {
		return Assessment{Route: RouteNeedInfo, Reasons: []contracts.Reason{reason("MISSING_DATA", "Нужны полные и достоверные сведения.", unique(data.missing)...)}}, nil
	}
	var q contracts.QuestionnaireResult
	var econ contracts.EconomicResult
	manual := data.manual
	model := "ip-v2"
	if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
		q = ipQuestionnaire(r)
		econ, manual = ipEconomics(r, data.flow, manual)
	} else {
		model = "legal-v2"
		q = legalQuestionnaire(r)
		econ, manual = legalEconomics(statements, manual)
	}
	raw := float64(q.Points) + econ.Points
	total, grade := totalAndGrade(raw)
	score := &contracts.ScoreResult{Model: model, Questionnaire: q, Economics: econ, RawTotal: raw, Total: total, Grade: grade}
	if grade == contracts.GradeD {
		return Assessment{Route: RouteRejected, Score: score, Reasons: []contracts.Reason{reason("SCORE_CLASS_D", "Итоговый класс D.")}, CashFlow: data.flow}, nil
	}
	if grade == contracts.GradeC {
		manual = append([]contracts.Reason{reason("SCORE_CLASS_C", "Итоговый класс C.")}, manual...)
	}
	if len(manual) > 0 {
		return Assessment{Route: RouteManual, Score: score, Reasons: manual, CashFlow: data.flow}, nil
	}
	return Assessment{Route: RouteSelection, Score: score, Reasons: []contracts.Reason{}, CashFlow: data.flow}, nil
}

func totalAndGrade(raw float64) (int, contracts.Grade) {
	total := int(math.Floor(raw + 0.5))
	switch {
	case total < 550:
		return total, contracts.GradeD
	case total < 650:
		return total, contracts.GradeC
	case total < 800:
		return total, contracts.GradeB
	default:
		return total, contracts.GradeA
	}
}
func reason(code, message string, fields ...string) contracts.Reason {
	if fields == nil {
		fields = []string{}
	}
	return contracts.Reason{Code: code, Message: message, Fields: fields}
}
func unique(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		found := false
		for _, v := range out {
			if v == s {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s)
		}
	}
	return out
}
func dateOnly(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
func yes(p *bool) bool { return p != nil && *p }
func val[T any](p *T) T {
	if p != nil {
		return *p
	}
	var z T
	return z
}
