package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

func TestBooleanCriterionThroughMS1ClientMS2AndAdminEvent(t *testing.T) {
	for _, state := range []string{"false", "true", "unknown"} {
		t.Run(state, func(t *testing.T) {
			h := newHTTPPipeline(t, "../../config/bank.default.json")
			var f httpFixture
			restFixture(t, "decisions/uc04_ip_approved.json", &f)
			assessment := booleanWireAssessment(t, f.Request, state)
			if assessment.Route != scoring.RouteSelection || assessment.Score == nil {
				t.Fatal(assessment)
			}
			selected, err := h.ms1.Optimizer.Select(optimizer.Input{RequestedTerms: *f.Request.CreditRequest, Need: *f.Request.Need, AnnualCFADSRUB: assessment.CashFlow.CFADS, FutureDebtPaymentsRUB: assessment.CashFlow.FutureDebt})
			if err != nil || selected.Terms == nil {
				t.Fatal(selected, err)
			}
			request := contracts.BankCheckRequest{PolicyID: h.ms1.Policy.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: *f.Request.CreditRequest, Terms: *selected.Terms, Score: *assessment.Score, Selection: selected.Audit}
			first, err := h.ms1.BankClient.Check(context.Background(), request)
			if err != nil || first.Verdict != contracts.BankVerdictIssued {
				t.Fatal(first, err)
			}
			repeated, err := h.ms1.BankClient.Check(context.Background(), request)
			if err != nil || repeated.LoanID == nil || first.LoanID == nil || *repeated.LoanID != *first.LoanID {
				t.Fatal(repeated, err)
			}
			if loanCount(t, h) != 1 || len(h.events) != 1 {
				t.Fatal("replay produced duplicate issue/event")
			}
			criteria := h.events[0].Score.Economics.Criteria
			criterion := criteria[len(criteria)-1]
			if criterion.ID != "WIRE_FLAG" || criterion.Value != nil {
				t.Fatal(criterion)
			}
			if state == "unknown" {
				if criterion.BooleanValue != nil {
					t.Fatal("unknown became false")
				}
			} else if criterion.BooleanValue == nil || *criterion.BooleanValue != (state == "true") {
				t.Fatal("boolean lost", criterion)
			}
			var body struct {
				Score struct {
					Economics struct {
						Criteria []struct {
							Value json.RawMessage `json:"value"`
						} `json:"criteria"`
					} `json:"economics"`
				} `json:"score"`
			}
			if err = json.Unmarshal(h.bankInputs[0], &body); err != nil {
				t.Fatal(err)
			}
			wire := string(body.Score.Economics.Criteria[len(body.Score.Economics.Criteria)-1].Value)
			expected := state
			if state == "unknown" {
				expected = "null"
			}
			if wire != expected {
				t.Fatal("MS1 wire type lost", wire, expected)
			}
			// Same application with a changed typed value must conflict.
			changed := request
			changed.Score.Economics.Criteria = append([]contracts.EconomicCriterion{}, request.Score.Economics.Criteria...)
			n := len(changed.Score.Economics.Criteria) - 1
			other := state != "true"
			changed.Score.Economics.Criteria[n].BooleanValue = &other
			if _, err = h.ms1.BankClient.Check(context.Background(), changed); err == nil {
				t.Fatal("changed value replay accepted")
			} else {
				var failure *clients.Error
				if !errors.As(err, &failure) || failure.Status != 409 {
					t.Fatal("wrong conflict", err)
				}
			}
			if loanCount(t, h) != 1 || len(h.events) != 1 {
				t.Fatal("conflict caused side effect")
			}
		})
	}
}

func booleanWireAssessment(t *testing.T, request contracts.DecisionRequest, state string) scoring.Assessment {
	t.Helper()
	raw, err := os.ReadFile("../../models/ip-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	d.Version = "ip-boolean-wire-test"
	d.FactsSchemaVersion = "boolean-wire-test"
	d.Parameters = append(d.Parameters, model.Parameter{ID: "wire_flag", Enabled: true, Title: "Test boolean wire", Source: model.Source{Kind: model.Supplier, Path: "wire_flag"}, JSONName: "wire_flag", Type: model.Boolean, Unit: model.Flag, Required: false})
	flag := func(v bool) model.Condition {
		return model.Condition{Op: model.EQ, Parameter: "wire_flag", Value: &model.Literal{Boolean: &v}}
	}
	d.Criteria = append(d.Criteria, model.Criterion{ID: "WIRE_FLAG", Enabled: true, Title: "Test boolean wire", Block: model.Additional, ValueParameter: "wire_flag", MaxPoints: 0, UnknownAction: model.Zero, SpecialCases: []model.ScoreCase{}, Cases: []model.ScoreCase{{ID: "yes", When: flag(true), Points: 0}, {ID: "no", When: flag(false), Points: 0}}})
	catalog, err := sourcecatalog.Bindings(d.BorrowerType)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rules.Compile(d, catalog)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := scoring.PrepareApplication(request, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	value := rules.Unknown()
	if state != "unknown" {
		value = rules.Boolean(state == "true")
	}
	prepared = scoring.WithAdditionalFacts(prepared, map[string]rules.Value{"wire_flag": value})
	evaluated, err := scoring.EvaluatePrepared(engine, prepared)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := scoring.ToAssessment(evaluated)
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func TestBooleanCriterionThroughReviewEvents(t *testing.T) {
	for _, state := range []string{"true", "false", "unknown"} {
		for _, mode := range []string{"bank", "borrower"} {
			t.Run(state+"/"+mode, func(t *testing.T) {
				stateFile, fixture := "../../config/bank.capital_boundary.json", "decisions/uc04_ip_approved.json"
				if mode == "borrower" {
					stateFile, fixture = "../../config/bank.default.json", "decisions/uc03_ip_manual.json"
				}
				h := newHTTPPipeline(t, stateFile)
				var f httpFixture
				restFixture(t, fixture, &f)
				assessment := booleanWireAssessment(t, f.Request, state)
				if mode == "borrower" {
					if assessment.Route != scoring.RouteManual {
						t.Fatal(assessment)
					}
					request := contracts.ManualReviewRequest{PolicyID: h.ms1.Policy.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: *f.Request.CreditRequest, Score: *assessment.Score, Reasons: assessment.Reasons}
					for i := 0; i < 2; i++ {
						if _, err := h.ms1.BankClient.ManualReview(context.Background(), request); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					selected, err := h.ms1.Optimizer.Select(optimizer.Input{RequestedTerms: *f.Request.CreditRequest, Need: *f.Request.Need, AnnualCFADSRUB: assessment.CashFlow.CFADS, FutureDebtPaymentsRUB: assessment.CashFlow.FutureDebt})
					if err != nil || selected.Terms == nil {
						t.Fatal(selected, err)
					}
					request := contracts.BankCheckRequest{PolicyID: h.ms1.Policy.ID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: *f.Request.CreditRequest, Terms: *selected.Terms, Score: *assessment.Score, Selection: selected.Audit}
					for i := 0; i < 2; i++ {
						result, err := h.ms1.BankClient.Check(context.Background(), request)
						if err != nil || result.Verdict != contracts.BankVerdictBankReview {
							t.Fatal(result, err)
						}
					}
				}
				if loanCount(t, h) != 0 || len(h.events) != 1 {
					t.Fatal("review caused issuance/duplicate event")
				}
				event := h.events[0]
				want := contracts.EventTypeBankReview
				if mode == "borrower" {
					want = contracts.EventTypeBorrowerReview
				}
				if event.Kind != want {
					t.Fatal(event.Kind, want)
				}
				criterion := event.Score.Economics.Criteria[len(event.Score.Economics.Criteria)-1]
				if criterion.Value != nil {
					t.Fatal("boolean converted to number")
				}
				if state == "unknown" {
					if criterion.BooleanValue != nil {
						t.Fatal("unknown converted to false")
					}
				} else if criterion.BooleanValue == nil || *criterion.BooleanValue != (state == "true") {
					t.Fatal("review lost boolean")
				}
			})
		}
	}
}
