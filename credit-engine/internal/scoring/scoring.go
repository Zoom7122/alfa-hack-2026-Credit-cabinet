// Package scoring prepares source facts and executes compiled scoring models.
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
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
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
	source  StatementsSource
	clock   clock.Clock
	catalog *modelbindings.Catalog
	models  modelbindings.Provider
	facts   FactsSource
}

func NewEvaluator(source StatementsSource, c clock.Clock) (*Evaluator, error) {
	if c == nil {
		return nil, errors.New("scoring: clock is required")
	}
	catalog, err := modelbindings.Default()
	if err != nil {
		return nil, err
	}
	return NewEvaluatorWithCatalog(source, nil, c, catalog)
}

func (e *Evaluator) Evaluate(ctx context.Context, r contracts.DecisionRequest) (Assessment, error) {
	return e.evaluateCompiled(ctx, r)
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
