package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type restRoundTripper func(*http.Request) (*http.Response, error)

func (f restRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func restResponse(t *testing.T, code int, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}
}
func restFixture(t *testing.T, name string, out any) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

func TestScoringStartupConnectsRESTClients(t *testing.T) {
	cfg, err := config.LoadScoring(environment(map[string]string{"STATEMENTS_URL": "http://b.invalid", "BANK_URL": "http://bank.invalid", "POLICY_FILE": "../../config/policy.json", "DEMO_EVALUATION_DATE": "2026-10-23"}))
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewScoring(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Evaluator == nil || a.BankClient == nil || a.StatementsClient == nil {
		t.Fatal("REST dependencies missing")
	}
	var f struct {
		Request    contracts.DecisionRequest    `json:"request"`
		Statements contracts.StatementsResponse `json:"service_b_response"`
		Expected   struct {
			BankRequest  contracts.BankCheckRequest  `json:"bank_check_request"`
			BankResponse contracts.BankCheckResponse `json:"bank_response"`
			Response     contracts.DecisionResponse  `json:"response"`
		} `json:"expected"`
	}
	restFixture(t, "decisions/uc04_le_approved.json", &f)
	bCalls := 0
	a.StatementsHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		bCalls++
		if r.URL.Host != "b.invalid" || r.URL.Query().Get("years") != "2024,2025" {
			t.Fatal(r.URL)
		}
		return restResponse(t, 200, f.Statements), nil
	})
	assessment, err := a.Evaluator.Evaluate(context.Background(), f.Request)
	if err != nil || assessment.Route != scoring.RouteSelection || assessment.Score.Total != f.Expected.Response.Score.Total || bCalls != 1 {
		t.Fatalf("%+v %v", assessment, err)
	}
	bankCalls := 0
	a.BankHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		bankCalls++
		if r.URL.Host != "bank.invalid" {
			t.Fatal(r.URL)
		}
		return restResponse(t, 200, f.Expected.BankResponse), nil
	})
	r, err := a.BankClient.Check(context.Background(), f.Expected.BankRequest)
	if err != nil || !reflect.DeepEqual(r, f.Expected.BankResponse) || bankCalls != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	var ip struct {
		Request contracts.DecisionRequest `json:"request"`
	}
	restFixture(t, "decisions/uc04_ip_approved.json", &ip)
	assessment, err = a.Evaluator.Evaluate(context.Background(), ip.Request)
	if err != nil || assessment.Route != scoring.RouteSelection || bCalls != 1 {
		t.Fatal("IP contacted B")
	}
}

func TestBankStartupConnectsAdminDelivery(t *testing.T) {
	cfg, err := config.LoadBank(environment(map[string]string{"ADMIN_URL": "http://admin.invalid", "POLICY_FILE": "../../config/policy.json", "BANK_STATE_FILE": "../../config/bank.default.json"}))
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewBank(cfg, clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if a.AdminClient == nil {
		t.Fatal("admin client missing")
	}
	var f struct {
		Request contracts.BankCheckRequest `json:"request"`
	}
	restFixture(t, "bank/bank_fixed_deal.json", &f)
	calls := 0
	var events []contracts.AdminEvent
	a.AdminHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "admin.invalid" || r.URL.Path != "/api/v1/credit-events" {
			t.Fatal(r.URL)
		}
		var e contracts.AdminEvent
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
		if calls == 1 {
			return restResponse(t, 503, map[string]string{"error": "unavailable"}), nil
		}
		return restResponse(t, 202, contracts.EventAck{EventID: e.EventID, Accepted: true}), nil
	})
	first, err := a.Operations.Check(context.Background(), f.Request)
	if err != nil || first.Verdict != contracts.BankVerdictIssued || first.NotificationStatus != contracts.NotificationStatusPending {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := a.Operations.Check(context.Background(), f.Request)
	if err != nil || second.NotificationStatus != contracts.NotificationStatusDelivered {
		t.Fatalf("%+v %v", second, err)
	}
	_, err = a.Operations.Check(context.Background(), f.Request)
	if err != nil || calls != 2 || !reflect.DeepEqual(events[0], events[1]) || a.Operations.Snapshot().State.Cash != 7000000 {
		t.Fatal("startup notifier not connected correctly")
	}
}

func TestStatementsRESTClassifiesMissingDataAndTechnicalFailures(t *testing.T) {
	cfg, err := config.LoadScoring(environment(map[string]string{"STATEMENTS_URL": "http://b.invalid", "BANK_URL": "http://bank.invalid", "POLICY_FILE": "../../config/policy.json", "DEMO_EVALUATION_DATE": "2026-10-23"}))
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewScoring(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		code   int
		change func(*contracts.StatementsResponse)
		want   int
	}{
		{"not found", 404, nil, 0},
		{"partial forms", 200, func(s *contracts.StatementsResponse) { s.AnnualReports = nil }, 0},
		{"contradictory balance", 200, func(s *contracts.StatementsResponse) { *s.AnnualReports[0].Balance.TotalAssets += 1000000 }, 0},
		{"wrong unit", 200, func(s *contracts.StatementsResponse) { *s.AnnualReports[0].Balance.Unit = "USD" }, 502},
		{"foreign borrower", 200, func(s *contracts.StatementsResponse) { s.BorrowerID = "other" }, 502},
		{"unavailable", 503, nil, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f struct {
				Request    contracts.DecisionRequest    `json:"request"`
				Statements contracts.StatementsResponse `json:"service_b_response"`
			}
			restFixture(t, "decisions/uc04_le_approved.json", &f)
			if tc.change != nil {
				tc.change(&f.Statements)
			}
			calls := 0
			a.StatementsHTTP.Transport = restRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return restResponse(t, tc.code, f.Statements), nil
			})
			result, err := a.Evaluator.Evaluate(context.Background(), f.Request)
			if calls != 1 {
				t.Fatalf("B calls = %d, want 1", calls)
			}
			if tc.want == 0 {
				if err != nil || result.Route != scoring.RouteNeedInfo || result.Score != nil {
					t.Fatalf("missing data became a technical failure or credit decision: %+v %v", result, err)
				}
			} else {
				var failure *scoring.Error
				if !errors.As(err, &failure) || failure.Status != tc.want {
					t.Fatalf("technical failure became a credit decision: %+v %v", result, err)
				}
			}
		})
	}
}
