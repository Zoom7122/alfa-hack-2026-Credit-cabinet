package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func TestGeneratedFactsPipeline(t *testing.T) {
	for _, mode := range []string{"ip", "legal", "integer", "null_optional", "missing_required", "legal_missing_required", "early_stop", "legal_early_stop", "supplier_stop", "supplier_stop_and_null", "unavailable", "timeout", "wrong_id", "wrong_schema", "wrong_type", "missing_key", "future_date", "legacy", "explicit_v2"} {
		t.Run(mode, func(t *testing.T) {
			h := newHTTPPipelineWithSettings(t, "../../config/bank.default.json", map[string]string{"FACTS_URL": "http://facts.invalid"})
			version, fixture := "ip-demo-v1", "decisions/uc04_ip_approved.json"
			if strings.HasPrefix(mode, "legal") || strings.HasPrefix(mode, "supplier_stop") || mode == "unavailable" || mode == "timeout" || strings.HasPrefix(mode, "wrong_") || mode == "missing_key" || mode == "future_date" {
				version, fixture = "legal-demo-v1", "decisions/uc04_le_approved.json"
			}
			if strings.HasPrefix(mode, "supplier_stop") {
				version = "legal-facts-stop-v1"
			}
			if mode == "integer" {
				version = "ip-demo-v2"
			}
			if mode == "legacy" || mode == "explicit_v2" {
				version = "ip-v2"
			}
			var f httpFixture
			restFixture(t, fixture, &f)
			h.statements = f.Statements
			if mode != "legacy" {
				f.Request.ModelVersion = &version
			}
			if mode == "early_stop" || mode == "legal_early_stop" {
				flag := true
				if f.Request.Scoring.IP != nil {
					f.Request.Scoring.IP.Bankrupt = &flag
				} else {
					f.Request.Scoring.LegalEntity.Bankrupt = &flag
				}
			}
			calls := 0
			var requestBody []byte
			order := []string{}
			oldB := h.ms1.StatementsHTTP.Transport
			h.ms1.StatementsHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
				order = append(order, "B")
				return oldB.RoundTrip(r)
			})
			h.ms1.FactsHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				order = append(order, "facts")
				body, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				if calls > 1 && !bytes.Equal(requestBody, body) {
					t.Fatal("repeat changed aggregate request")
				}
				requestBody = body
				var request contracts.ScoringFactsRequest
				if err := json.Unmarshal(body, &request); err != nil {
					t.Fatal(err)
				}
				if request.ModelVersion != version || request.ApplicationID != f.Request.ApplicationID || !reflect.DeepEqual(request.Borrower, contracts.ScoringFactsBorrower{ID: f.Request.Borrower.ID, Type: f.Request.Borrower.Type, INN: f.Request.Borrower.INN}) {
					t.Fatal(request)
				}
				if r.Context() == nil || r.Method != "POST" || r.URL.Path != "/api/v1/scoring-facts" {
					t.Fatal(r)
				}
				if mode == "unavailable" {
					return nil, errors.New("private")
				}
				if mode == "timeout" {
					return nil, context.DeadlineExceeded
				}
				var number any = 1000
				if mode == "integer" {
					number = json.Number("9007199254740993")
				}
				if mode == "missing_required" || mode == "legal_missing_required" || mode == "supplier_stop_and_null" {
					number = nil
				}
				var flag any = false
				if mode == "null_optional" {
					flag = nil
				}
				facts := map[string]any{"annual_contract_value_rub": number, "has_confirmed_contracts": flag}
				response := map[string]any{"application_id": request.ApplicationID, "borrower_id": request.Borrower.ID, "borrower_type": request.Borrower.Type, "model_version": request.ModelVersion, "facts_schema_version": request.FactsSchemaVersion, "facts_date": "2026-10-20", "additional_facts": facts}
				switch mode {
				case "wrong_id":
					response["application_id"] = "foreign"
				case "wrong_schema":
					response["facts_schema_version"] = "foreign"
				case "wrong_type":
					facts["has_confirmed_contracts"] = "false"
				case "missing_key":
					delete(facts, "annual_contract_value_rub")
				case "future_date":
					response["facts_date"] = "2026-10-24"
				}
				return restResponse(t, 200, response), nil
			})
			w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			wantHTTP := 200
			if mode == "timeout" {
				wantHTTP = 504
			}
			if mode == "unavailable" || strings.HasPrefix(mode, "wrong_") || mode == "missing_key" || mode == "future_date" {
				wantHTTP = 502
			}
			if w.Code != wantHTTP || w.Header().Get("X-Scoring-Model-Version") != version {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			wantCalls := 1
			if mode == "early_stop" || mode == "legal_early_stop" || mode == "legacy" || mode == "explicit_v2" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("facts calls %d want %d", calls, wantCalls)
			}
			wantB := 0
			if mode == "legal" || mode == "legal_missing_required" {
				wantB = 1
			}
			if h.bCalls != wantB {
				t.Fatal("unexpected B", h.bCalls)
			}
			if wantB == 1 && !reflect.DeepEqual(order, []string{"facts", "B"}) {
				t.Fatal(order)
			}
			if wantHTTP != 200 {
				if len(h.bankCalls) != 0 || len(h.events) != 0 || loanCount(t, h) != 0 {
					t.Fatal("technical error had side effects")
				}
				return
			}
			var result contracts.DecisionResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := contracts.DecisionStatusApproved
			if mode == "early_stop" || mode == "legal_early_stop" || strings.HasPrefix(mode, "supplier_stop") {
				want = contracts.DecisionStatusRejected
			}
			if mode == "missing_required" || mode == "legal_missing_required" {
				want = contracts.DecisionStatusNeedInfo
			}
			if result.Status != want {
				t.Fatalf("want %s: %s", want, w.Body.String())
			}
			if want != contracts.DecisionStatusApproved {
				if result.Score != nil || len(h.bankCalls) != 0 || len(h.events) != 0 || loanCount(t, h) != 0 {
					t.Fatal("early outcome had side effects")
				}
				if want == contracts.DecisionStatusNeedInfo && !strings.Contains(w.Body.String(), "additional_facts.annual_contract_value_rub") {
					t.Fatal(w.Body.String())
				}
				if strings.HasPrefix(mode, "supplier_stop") && !strings.Contains(w.Body.String(), "SUPPLIER_STOP") {
					t.Fatal(w.Body.String())
				}
				return
			}
			if result.Score == nil || result.Score.Model != version || len(h.events) != 1 || len(h.bankCalls) != 1 || loanCount(t, h) != 1 {
				t.Fatal(result)
			}
			if wantCalls == 1 {
				checkFlag := func(score *contracts.ScoreResult) {
					t.Helper()
					if score == nil {
						t.Fatal("missing score")
					}
					found := false
					for _, criterion := range score.Economics.Criteria {
						if criterion.ID != "TEST_has_confirmed_contracts" {
							continue
						}
						found = true
						if criterion.Value != nil || (mode == "null_optional") != (criterion.BooleanValue == nil) || criterion.BooleanValue != nil && *criterion.BooleanValue {
							t.Fatal(criterion)
						}
					}
					if !found {
						t.Fatal("named supplier value not evaluated")
					}
				}
				checkFlag(result.Score)
				checkFlag(&h.events[0].Score)
			}
			// Re-evaluation uses the same explicit version and aggregate body, while
			// the real MS2 registry prevents a second issue or notification.
			loan := *result.LoanID
			w = postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.LoanID == nil || *result.LoanID != loan || result.Score.Model != version || loanCount(t, h) != 1 || len(h.events) != 1 || calls != 2*wantCalls {
				t.Fatal("repeat changed version or duplicated issue", result, calls)
			}
		})
	}
}

func TestGeneratedFactsPipelineStartup(t *testing.T) {
	cfg, err := config.LoadScoring(environment(map[string]string{"STATEMENTS_URL": "http://b.invalid", "POLICY_FILE": "../../config/policy.json"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.NewScoring(cfg, nil); err == nil || !strings.Contains(err.Error(), "FACTS_URL") {
		t.Fatal("missing provider URL accepted", err)
	}
	cfg.FactsURL = "http://facts.invalid"
	cfg.WriteTimeout = cfg.FactsTimeout + cfg.StatementsTimeout + cfg.BankTimeout
	if _, err := app.NewScoring(cfg, nil); err == nil {
		t.Fatal("no time budget for complete pipeline")
	}
}
