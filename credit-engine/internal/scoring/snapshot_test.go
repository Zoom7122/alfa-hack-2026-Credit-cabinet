package scoring

import (
	"context"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"testing"
	"time"
)

func TestSnapshotConflictAndConfirmedStopPriority(t *testing.T) {
	e, err := NewEvaluator(nil, clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	r := contracts.DecisionRequest{ApplicationID: "app", Borrower: contracts.Borrower{ID: "co", Type: contracts.BorrowerTypeLegalEntity}, InputSnapshot: &contracts.InputSnapshot{ID: "snapshot", EvaluationDate: "2026-10-23", Diagnostics: []contracts.InputDiagnostic{{Conflict: true, Code: "FINANCIAL_DATA_CONFLICT", Message: "clarify", Fields: []string{"service_b.annual_reports.2025"}}}}}
	a, err := e.Evaluate(context.Background(), r)
	if err != nil || a.Route != RouteNeedInfo || a.Reasons[0].Code != "FINANCIAL_DATA_CONFLICT" {
		t.Fatalf("%+v %v", a, err)
	}
	inactive := false
	r.Scoring = &contracts.ScoringInput{LegalEntity: &contracts.LegalEntityScoring{Active: &inactive}}
	a, err = e.Evaluate(context.Background(), r)
	if err != nil || a.Route != RouteRejected {
		t.Fatalf("confirmed stop overridden: %+v %v", a, err)
	}
	r.InputSnapshot.EvaluationDate = "2027-10-23"
	if _, err = e.Evaluate(context.Background(), r); err == nil {
		t.Fatal("future evaluation accepted")
	}
}
