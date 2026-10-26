package scoring

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

func TestApplicationStatementsUseSameScoringRules(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	want, err, _ := assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	f.Request.FinancialStatements = &f.Statements
	raw, _ := json.Marshal(f.Request)
	request, err := Decode(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	// No supplier is available: application-scoped data must suffice.
	e, err := NewEvaluator(nil, clock.Fixed{Time: evaluationDate})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Evaluate(context.Background(), request)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("inline/supplier scoring differ: got=%+v want=%+v err=%v", got, want, err)
	}
	// Changing document profit must change an economic criterion.
	current := &request.FinancialStatements.AnnualReports[1]
	current.ProfitAndLoss.NetProfit = ptr(int64(0))
	current.ProfitAndLoss.ProfitBeforeTax = nil
	current.ProfitAndLoss.IncomeTax = nil
	changed, err := e.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Score != nil && reflect.DeepEqual(changed.Score.Economics, got.Score.Economics) {
		t.Fatal("document values had no effect on economic scoring")
	}
}

func TestApplicationStatementsStillValidateBorrowerAndCompleteness(t *testing.T) {
	t.Run("wrong borrower", func(t *testing.T) {
		f := loadFixture(t, "decisions/uc04_le_approved.json")
		f.Statements.BorrowerID = "another-company"
		f.Request.FinancialStatements = &f.Statements
		_, err, source := assess(t, f)
		if err == nil || source.calls != 0 {
			t.Fatalf("wrong borrower accepted or supplier queried: %v", err)
		}
	})
	t.Run("partial document", func(t *testing.T) {
		f := loadFixture(t, "decisions/uc04_le_approved.json")
		f.Request.FinancialStatements = &contracts.StatementsResponse{BorrowerID: f.Request.Borrower.ID}
		got, err, source := assess(t, f)
		if err != nil || got.Route != RouteNeedInfo || source.calls != 0 {
			t.Fatalf("partial statements bypassed validation: %+v %v", got, err)
		}
	})
}
