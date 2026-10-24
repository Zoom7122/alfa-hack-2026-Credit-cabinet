package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func problem(t *testing.T, w *httptest.ResponseRecorder, status int) contracts.Problem {
	t.Helper()
	var p contracts.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("non-JSON error: %d %s", w.Code, w.Body.String())
	}
	if w.Code != status || p.Status != status || w.Header().Get("Content-Type") != "application/problem+json" || p.Type != "about:blank" || p.Title == "" || p.Code == "" || p.Detail == "" || p.Fields == nil {
		t.Fatalf("invalid Problem: %d %+v", w.Code, p)
	}
	if strings.Contains(w.Body.String(), "confidential") {
		t.Fatal("private error details leaked")
	}
	return p
}

func TestDecisionHTTPInvalidInputAndRates(t *testing.T) {
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	for _, body := range []string{"", "{", "null", "[]", `{} {}`, `{"application_id":1}`, `{"application_id":"a","borrower":null}`, `{"application_id":"a","borrower":{"id":"b","type":"SOLE_PROPRIETOR"},"credit_request":{"amount_rub":1.5}}`} {
		problem(t, requestJSON(h.ms1.Server.Handler, "POST", "/api/v1/decisions", body), 400)
	}
	for _, rate := range []float64{-1, 100.01, 0.001, 1.234} {
		var f httpFixture
		restFixture(t, "decisions/uc04_ip_approved.json", &f)
		f.Request.CreditRequest.MaxAnnualRate = &rate
		p := problem(t, postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request), 422)
		if !reflect.DeepEqual(p.Fields, []string{"credit_request.max_annual_rate"}) {
			t.Fatal(p.Fields)
		}
	}
	for _, rate := range []float64{0, 1, 23.75, 100} {
		var f httpFixture
		restFixture(t, "decisions/uc04_ip_approved.json", &f)
		f.Request.ApplicationID += "-rate-" + strings.ReplaceAll(jsonNumber(t, rate), ".", "_")
		f.Request.CreditRequest.MaxAnnualRate = &rate
		w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
		if w.Code != 200 {
			t.Fatalf("rate %g rejected: %d %s", rate, w.Code, w.Body.String())
		}
		var result contracts.DecisionResponse
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		if result.Terms != nil && result.Terms.AnnualRate > rate {
			t.Fatal("rate was interpreted as fraction")
		}
	}
}
func jsonNumber(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDependencyHTTPFailuresRemainTechnical(t *testing.T) {
	for _, route := range []string{"check", "manual", "statements"} {
		for _, mode := range []string{"unavailable", "timeout", "bad JSON", "foreign ID", "unexpected status", "conflict"} {
			if route == "statements" && mode == "conflict" {
				continue
			}
			t.Run(route+"/"+mode, func(t *testing.T) {
				h := newHTTPPipeline(t, "../../config/bank.default.json")
				var f httpFixture
				file := "decisions/uc04_ip_approved.json"
				if route == "manual" {
					file = "decisions/uc03_ip_manual.json"
				}
				if route == "statements" {
					file = "decisions/uc04_le_approved.json"
				}
				restFixture(t, file, &f)
				fake := restRoundTripper(func(r *http.Request) (*http.Response, error) {
					switch mode {
					case "unavailable":
						return nil, errors.New("confidential dependency URL")
					case "timeout":
						return nil, context.DeadlineExceeded
					case "bad JSON":
						return &http.Response{StatusCode: expectedHTTPStatus(route), Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{"))}, nil
					case "foreign ID":
						if route == "statements" {
							f.Statements.BorrowerID = "other"
							return restResponse(t, 200, f.Statements), nil
						}
						if route == "manual" {
							return restResponse(t, 202, contracts.ManualReviewResponse{ApplicationID: "other", EventID: "other:BORROWER_REVIEW", NotificationStatus: contracts.NotificationStatusDelivered}), nil
						}
						return restResponse(t, 200, map[string]any{"application_id": "other"}), nil
					case "unexpected status":
						return restResponse(t, 503, map[string]string{"error": "confidential failure"}), nil
					default:
						return restResponse(t, 409, contracts.Problem{Status: 409, Code: "POLICY_MISMATCH", Fields: []string{"policy_id"}}), nil
					}
				})
				if route == "statements" {
					h.ms1.StatementsHTTP.Transport = fake
				} else {
					h.ms1.BankHTTP.Transport = fake
				}
				want := 502
				if mode == "timeout" {
					want = 504
				}
				if mode == "conflict" {
					want = 409
				}
				p := problem(t, postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request), want)
				if mode == "conflict" && (p.Code != "POLICY_MISMATCH" || !reflect.DeepEqual(p.Fields, []string{"policy_id"})) {
					t.Fatal(p)
				}
				if len(h.events) != 0 || h.ms2.Operations.Snapshot().State.Cash != 10000000 {
					t.Fatal("dependency failure caused bank side effects")
				}
			})
		}
	}
}
func expectedHTTPStatus(route string) int {
	if route == "manual" {
		return 202
	}
	return 200
}

func TestHTTPRetriesPendingConflictsAndLostResponse(t *testing.T) {
	for _, file := range []string{"uc04_ip_approved.json", "uc03_ip_manual.json", "uc05_ip_bank_review.json"} {
		t.Run(file, func(t *testing.T) {
			profile := "default"
			if strings.Contains(file, "uc05") {
				profile = "capital_boundary"
			}
			h := newHTTPPipeline(t, "../../config/bank."+profile+".json")
			var f httpFixture
			restFixture(t, "decisions/"+file, &f)
			h.adminFailure = true
			first := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			if first.Code != 200 || pendingCount(t, h) != 1 {
				t.Fatalf("%d %s", first.Code, first.Body.String())
			}
			state := h.ms2.Operations.Snapshot()
			h.adminFailure = false
			second := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			third := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			if second.Code != 200 || third.Code != 200 || second.Body.String() != first.Body.String() || third.Body.String() != first.Body.String() || len(h.events) != 2 || pendingCount(t, h) != 0 || !reflect.DeepEqual(h.events[0], h.events[1]) {
				t.Fatal("HTTP replay changed result/event or duplicated delivery")
			}
			compareWire(t, h.ms2.Operations.Snapshot(), state)
			f.Request.CreditRequest.TermMonths = intPointer(11)
			problem(t, postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request), 409)
			if len(h.events) != 2 {
				t.Fatal("conflict triggered delivery")
			}
		})
	}
	// MS2 commits successfully, but MS1 loses the HTTP response. A new request
	// runs scoring/selection again; only MS2 owns replay protection.
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	var f httpFixture
	restFixture(t, "decisions/uc04_ip_approved.json", &f)
	original := h.ms1.BankHTTP.Transport
	h.ms1.BankHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		resp, err := original.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
		return nil, errors.New("lost response")
	})
	problem(t, postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request), 502)
	h.ms1.BankHTTP.Transport = original
	w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
	if w.Code != 200 || loanCount(t, h) != 1 || len(h.events) != 1 || h.ms2.Operations.Snapshot().State.Cash != 7000000 {
		t.Fatalf("lost response replay failed: %d %s", w.Code, w.Body.String())
	}
}
func intPointer(v int) *int { return &v }

func pendingCount(t *testing.T, h *httpPipeline) int {
	t.Helper()
	events, err := h.ms2.Operations.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}
func loanCount(t *testing.T, h *httpPipeline) int {
	t.Helper()
	loans, err := h.ms2.Operations.Loans()
	if err != nil {
		t.Fatal(err)
	}
	return len(loans)
}

func TestConcurrentHTTPDecisionsUseCurrentBank(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(jsonNumber(t, same), func(t *testing.T) {
			h := newHTTPPipeline(t, "../../config/bank.default.json")
			var f httpFixture
			restFixture(t, "decisions/uc04_ip_approved.json", &f)
			var wg sync.WaitGroup
			count := 2
			if same {
				count = 8
			}
			answers := make(chan *httptest.ResponseRecorder, count)
			start := make(chan struct{})
			for i := 0; i < count; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					r := f.Request
					if !same {
						r.ApplicationID += strings.Repeat("x", i)
					}
					data, _ := json.Marshal(r)
					<-start
					answers <- requestJSON(h.ms1.Server.Handler, "POST", "/api/v1/decisions", string(data))
				}(i)
			}
			close(start)
			wg.Wait()
			close(answers)
			issued, review := 0, 0
			for w := range answers {
				var result contracts.DecisionResponse
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				if result.Status == contracts.DecisionStatusApproved {
					issued++
				} else if result.Status == contracts.DecisionStatusUnderReview {
					review++
				}
			}
			if same && (issued != count || review != 0 || len(h.events) != 1) || !same && (issued != 1 || review != 1 || len(h.events) != 2) {
				t.Fatalf("issued=%d review=%d events=%d", issued, review, len(h.events))
			}
			if loanCount(t, h) != 1 || h.ms2.Operations.Snapshot().State.Cash != 7000000 {
				t.Fatal("duplicate issue")
			}
		})
	}
}

func TestBankHTTPFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/bank/*.json")
	if err != nil || len(files) != 7 {
		t.Fatal(files, err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var f struct {
				Initial  contracts.BankState        `json:"initial_bank"`
				Request  contracts.BankCheckRequest `json:"request"`
				Expected struct {
					Response contracts.BankCheckResponse `json:"response"`
					Event    contracts.AdminEvent        `json:"notification"`
					Saved    contracts.BankState         `json:"bank_state_saved"`
				} `json:"expected"`
			}
			restFixture(t, "bank/"+filepath.Base(file), &f)
			statePath := filepath.Join(t.TempDir(), "initial.json")
			data, _ := json.Marshal(f.Initial)
			if err := os.WriteFile(statePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			h := newHTTPPipeline(t, statePath)
			w := postJSON(t, h.ms2.Server.Handler, "/api/v1/bank/check-and-issue", f.Request)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			compareWire(t, json.RawMessage(w.Body.Bytes()), f.Expected.Response)
			compareWire(t, h.events[0], f.Expected.Event)
			compareWire(t, h.ms2.Operations.Snapshot().State, f.Expected.Saved)
		})
	}
}

func TestHTTPJSONRequiredBankFieldsAndValueValidation(t *testing.T) {
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	var f struct {
		Request contracts.BankCheckRequest `json:"request"`
	}
	restFixture(t, "bank/bank_fixed_deal.json", &f)
	for _, field := range []string{"terms.annual_rate", "terms", "score.total", "selection.monthly_payment_rub", "borrower.id", "requested_terms"} {
		for _, null := range []bool{false, true} {
			var raw map[string]any
			_ = json.Unmarshal([]byte(jsonNumber(t, f.Request)), &raw)
			parts := strings.Split(field, ".")
			obj := raw
			for _, part := range parts[:len(parts)-1] {
				obj = obj[part].(map[string]any)
			}
			last := parts[len(parts)-1]
			if null {
				obj[last] = nil
			} else {
				delete(obj, last)
			}
			problem(t, postJSON(t, h.ms2.Server.Handler, "/api/v1/bank/check-and-issue", raw), 400)
		}
	}
	for _, rate := range []float64{-1, 100.01, 1.234} {
		r := f.Request
		r.Terms.AnnualRate = rate
		problem(t, postJSON(t, h.ms2.Server.Handler, "/api/v1/bank/check-and-issue", r), 422)
	}
	if len(h.events) != 0 || loanCount(t, h) != 0 {
		t.Fatal("invalid JSON/value changed bank")
	}
	// Additional source fields are ignored, including alleged ready-made scores.
	var d httpFixture
	restFixture(t, "decisions/uc04_ip_approved.json", &d)
	var raw map[string]any
	_ = json.Unmarshal([]byte(jsonNumber(t, d.Request)), &raw)
	raw["score"] = 999
	raw["grade"] = "D"
	raw["ratio"] = math.MaxFloat64
	w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", raw)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	compareWire(t, json.RawMessage(w.Body.Bytes()), d.Expected.Response)
}

func TestHTTPCompleteNeedInfoAndFreshBankForSecondDynamicDeal(t *testing.T) {
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	w := requestJSON(h.ms1.Server.Handler, "POST", "/api/v1/decisions", `{"application_id":"application-001","borrower":{"id":"borrower-001","type":"SOLE_PROPRIETOR"}}`)
	var result contracts.DecisionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != contracts.DecisionStatusNeedInfo || result.Score != nil || len(h.bankCalls) != 0 || len(h.events) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var f httpFixture
	restFixture(t, "decisions/uc04_ip_approved.json", &f)
	f.Request.ApplicationID, f.Request.Borrower.ID = "application-001", "borrower-001"
	w = postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != contracts.DecisionStatusApproved || loanCount(t, h) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}

	h = newHTTPPipeline(t, "../../config/bank.default.json")
	restFixture(t, "decisions/uc06_02_le_bank_review.json", &f)
	h.statements = f.Statements
	w = postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != contracts.DecisionStatusApproved || h.ms2.Operations.Snapshot().State.Cash != 8000000 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestBankHTTPMalformedJSONAndPolicyMismatch(t *testing.T) {
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	for _, route := range []string{"/api/v1/bank/check-and-issue", "/api/v1/bank/manual-reviews"} {
		for _, body := range []string{"", "{", "[]", "null", "{} {}", `{"policy_id":false}`, `{"reasons":[null]}`} {
			problem(t, requestJSON(h.ms2.Server.Handler, "POST", route, body), 400)
		}
	}
	var f struct {
		Request contracts.BankCheckRequest `json:"request"`
	}
	restFixture(t, "bank/bank_fixed_deal.json", &f)
	f.Request.PolicyID = "other-policy"
	p := problem(t, postJSON(t, h.ms2.Server.Handler, "/api/v1/bank/check-and-issue", f.Request), 409)
	if p.Code != "POLICY_MISMATCH" || len(h.events) != 0 || loanCount(t, h) != 0 {
		t.Fatal(p)
	}
}
