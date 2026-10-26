package scoring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
)

type FactsSource interface {
	Fetch(context.Context, *modelbindings.Entry, contracts.ScoringFactsRequest, time.Time) (modelbindings.Facts, error)
}

func NewEvaluatorWithCatalog(source StatementsSource, facts FactsSource, c clock.Clock, catalog *modelbindings.Catalog) (*Evaluator, error) {
	if c == nil || catalog == nil {
		return nil, fmt.Errorf("scoring: clock and compiled catalog are required")
	}
	for _, metadata := range catalog.List() {
		if metadata.UsesFacts && facts == nil {
			return nil, fmt.Errorf("scoring: facts source is required by compiled models")
		}
	}
	return &Evaluator{source: source, clock: c, catalog: catalog, facts: facts}, nil
}
func (e *Evaluator) resolve(r contracts.DecisionRequest) (*modelbindings.Entry, error) {
	version := ""
	if r.ModelVersion == nil {
		var err error
		version, err = modelbindings.LegacyVersion(r.Borrower.Type)
		if err != nil {
			return nil, &Error{Status: 422, Code: "UNSUPPORTED_MODEL_VERSION", Fields: []string{"model_version"}}
		}
	} else {
		version = *r.ModelVersion
	}
	entry, err := e.catalog.Lookup(version, r.Borrower.Type)
	if err != nil {
		return nil, &Error{Status: 422, Code: "UNSUPPORTED_MODEL_VERSION", Fields: []string{"model_version"}}
	}
	return entry, nil
}
func (e *Evaluator) ModelVersion(r contracts.DecisionRequest) (string, error) {
	entry, err := e.resolve(r)
	if err != nil {
		return "", err
	}
	return entry.Metadata().Version, nil
}

func (e *Evaluator) evaluateCompiled(ctx context.Context, r contracts.DecisionRequest) (Assessment, error) {
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	if err := Validate(r); err != nil {
		return Assessment{}, err
	}
	entry, err := e.resolve(r)
	if err != nil {
		return Assessment{}, err
	}
	date := dateOnly(e.clock.Now())
	if r.InputSnapshot != nil {
		date, err = time.Parse(time.DateOnly, r.InputSnapshot.EvaluationDate)
		if err != nil || date.After(dateOnly(e.clock.Now())) || r.InputSnapshot.ID == "" {
			return Assessment{}, &Error{Status: 422, Code: "INVALID_INPUT_SNAPSHOT", Fields: []string{"input_snapshot"}}
		}
	}
	p, err := PrepareApplication(r, date)
	if err != nil {
		return Assessment{}, err
	}
	stops, err := entry.Engine().Stops(p.Input)
	if err != nil {
		return Assessment{}, err
	}
	finish := func() (Assessment, error) {
		result, err := EvaluatePrepared(entry.Engine(), p)
		if err != nil {
			return Assessment{}, err
		}
		return ToAssessment(result)
	}
	if len(stops) > 0 {
		return finish()
	}
	if r.InputSnapshot != nil {
		for _, issue := range r.InputSnapshot.Diagnostics {
			if issue.Conflict {
				return Assessment{Route: RouteNeedInfo, Reasons: []contracts.Reason{{Code: issue.Code, Message: issue.Message, Fields: issue.Fields}}}, nil
			}
		}
	}
	if entry.Metadata().UsesFacts {
		// These models require their own frozen supplier facts contract, not live data in a snapshot round.
		if r.InputSnapshot != nil {
			return Assessment{}, &Error{Status: 422, Code: "SNAPSHOT_MODEL_UNSUPPORTED"}
		}
		if e.facts == nil {
			return Assessment{}, &Error{Status: 502, Code: "FACTS_UNAVAILABLE"}
		}
		request := contracts.ScoringFactsRequest{ApplicationID: r.ApplicationID, Borrower: contracts.ScoringFactsBorrower{ID: r.Borrower.ID, Type: r.Borrower.Type, INN: r.Borrower.INN}, ModelVersion: entry.Metadata().Version, FactsSchemaVersion: entry.Metadata().FactsSchemaVersion}
		facts, err := e.facts.Fetch(ctx, entry, request, p.Date)
		if err != nil {
			return Assessment{}, err
		}
		p, err = WithSupplierFacts(p, facts.Values, facts.Metadata.FactsDate)
		if err != nil {
			return Assessment{}, &Error{Status: 502, Code: "INVALID_FACTS_RESPONSE", Fields: []string{"facts_date"}}
		}
		if err := ctx.Err(); err != nil {
			return Assessment{}, err
		}
		stops, err = entry.Engine().Stops(p.Input)
		if err != nil {
			return Assessment{}, err
		}
		if len(stops) > 0 {
			return finish()
		}
	}
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		if e.source == nil && r.InputSnapshot == nil {
			return Assessment{}, &Error{Status: 502, Code: "STATEMENTS_UNAVAILABLE"}
		}
		var response contracts.StatementsResponse
		var sourceErr error
		if r.InputSnapshot != nil {
			if r.InputSnapshot.Statements == nil {
				sourceErr = ErrStatementsNotFound
			} else {
				response = *r.InputSnapshot.Statements
			}
		} else {
			response, sourceErr = e.source.Statements(ctx, r.Borrower.ID, []int{p.Date.Year() - 2, p.Date.Year() - 1})
		}
		if errors.Is(sourceErr, ErrStatementsNotFound) {
		} else if sourceErr != nil {
			var failure *Error
			if errors.As(sourceErr, &failure) {
				return Assessment{}, failure
			}
			status := 502
			var timeout interface{ Timeout() bool }
			if errors.Is(sourceErr, context.DeadlineExceeded) || errors.As(sourceErr, &timeout) && timeout.Timeout() {
				status = 504
			}
			return Assessment{}, &Error{Status: status, Code: "STATEMENTS_UNAVAILABLE"}
		} else {
			p, err = WithStatements(p, response)
			if err != nil {
				return Assessment{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Assessment{}, err
	}
	return finish()
}
