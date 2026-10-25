package bank

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type statementSource struct{ response contracts.StatementsResponse }

func (s statementSource) Statements(context.Context, string, []int) (contracts.StatementsResponse, error) {
	return s.response, nil
}

// This exercises the real domain components together; only external B and MS3
// are replaced. HTTP orchestration belongs to the later integration stage.
func TestRealScoringSelectionAndBankAcrossAllDecisions(t *testing.T) {
	files, _ := filepath.Glob("../../testdata/decisions/*.json")
	p := policy(t)
	selector, err := optimizer.New(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var f struct {
				Request    contracts.DecisionRequest    `json:"request"`
				Statements contracts.StatementsResponse `json:"service_b_response"`
				Expected   struct {
					Response     contracts.DecisionResponse   `json:"response"`
					BankResponse *contracts.BankCheckResponse `json:"bank_response"`
					Before       contracts.BankState          `json:"bank_state_before"`
					Saved        contracts.BankState          `json:"bank_state_saved"`
					Called       bool                         `json:"ms2_called"`
				} `json:"expected"`
			}
			read(t, "decisions/"+filepath.Base(path), &f)
			evaluator, err := scoring.NewEvaluator(statementSource{f.Statements}, fixed)
			if err != nil {
				t.Fatal(err)
			}
			assessment, err := evaluator.Evaluate(context.Background(), f.Request)
			if err != nil {
				t.Fatal(err)
			}
			if assessment.Route == scoring.RouteNeedInfo || assessment.Route == scoring.RouteRejected {
				if f.Expected.Called {
					t.Fatal("unexpected early completion")
				}
				return
			}
			calls := 0
			s := service(t, p, f.Expected.Before, notifierFunc(func(ctx context.Context, e contracts.AdminEvent) (contracts.EventAck, error) {
				calls++
				return delivered(ctx, e)
			}))
			manualReasons := assessment.Reasons
			if assessment.Route == scoring.RouteSelection {
				selected, err := selector.Select(optimizer.Input{RequestedTerms: *f.Request.CreditRequest, Need: *f.Request.Need, AnnualCFADSRUB: assessment.CashFlow.CFADS, FutureDebtPaymentsRUB: assessment.CashFlow.FutureDebt})
				if err != nil {
					t.Fatal(err)
				}
				if selected.Terms != nil {
					r, err := s.Check(context.Background(), contracts.BankCheckRequest{PolicyID: p.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: *f.Request.CreditRequest, Terms: *selected.Terms, Score: *assessment.Score, Selection: selected.Audit})
					if err != nil {
						t.Fatal(err)
					}
					if f.Expected.BankResponse == nil || r.Verdict != f.Expected.BankResponse.Verdict {
						t.Fatalf("bank verdict %s", r.Verdict)
					}
					compareChecks(t, r.Checks, f.Expected.BankResponse.Checks)
					if f.Expected.Response.Terms == nil || *selected.Terms != *f.Expected.Response.Terms {
						t.Fatal("final terms differ")
					}
					compareState(t, s.Snapshot().State, f.Expected.Saved)
					if calls != 1 {
						t.Fatal("wrong notification count")
					}
					return
				}
				manualReasons = selected.Reasons
			}
			_, err = s.ManualReview(context.Background(), contracts.ManualReviewRequest{PolicyID: p.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: *f.Request.CreditRequest, Score: *assessment.Score, Reasons: manualReasons})
			if err != nil {
				t.Fatal(err)
			}
			if f.Expected.Response.Status != contracts.DecisionStatusUnderReview || f.Expected.Response.ReviewReason == nil || *f.Expected.Response.ReviewReason != contracts.ReviewReasonBorrower || calls != 1 {
				t.Fatal("manual route differs")
			}
			compareState(t, s.Snapshot().State, f.Expected.Saved)
		})
	}
}
