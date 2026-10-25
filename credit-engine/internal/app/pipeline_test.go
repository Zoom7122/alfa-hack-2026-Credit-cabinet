package app_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

type httpPipeline struct {
	ms1          *app.Scoring
	ms2          *app.Bank
	mu           sync.Mutex
	bCalls       int
	bankCalls    []string
	bankInputs   []json.RawMessage
	events       []contracts.AdminEvent
	statements   contracts.StatementsResponse
	adminFailure bool
}

func newHTTPPipeline(t *testing.T, stateFile string) *httpPipeline {
	return newHTTPPipelineWithSettings(t, stateFile, nil)
}

func newHTTPPipelineWithSettings(t *testing.T, stateFile string, overrides map[string]string) *httpPipeline {
	t.Helper()
	values := map[string]string{"STATEMENTS_URL": "http://b.invalid", "BANK_URL": "http://bank.invalid", "POLICY_FILE": "../../config/policy.json", "DEMO_EVALUATION_DATE": "2026-10-23"}
	for key, value := range overrides {
		values[key] = value
	}
	sc, err := config.LoadScoring(environment(values))
	if err != nil {
		t.Fatal(err)
	}
	bc, err := config.LoadBank(environment(map[string]string{"ADMIN_URL": "http://admin.invalid", "POLICY_FILE": "../../config/policy.json", "BANK_STATE_FILE": stateFile}))
	if err != nil {
		t.Fatal(err)
	}
	ms1, err := app.NewScoring(sc, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms2, err := app.NewBank(bc, clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	h := &httpPipeline{ms1: ms1, ms2: ms2}
	ms1.StatementsHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		h.mu.Lock()
		h.bCalls++
		s := h.statements
		h.mu.Unlock()
		return restResponse(t, 200, s), nil
	})
	ms1.BankHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		h.bankCalls = append(h.bankCalls, r.URL.Path)
		h.bankInputs = append(h.bankInputs, append(json.RawMessage(nil), data...))
		h.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(data))
		w := httptest.NewRecorder()
		h.ms2.Server.Handler.ServeHTTP(w, r)
		return w.Result(), nil
	})
	ms2.AdminHTTP.Transport = restRoundTripper(func(r *http.Request) (*http.Response, error) {
		var e contracts.AdminEvent
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			return nil, err
		}
		h.mu.Lock()
		h.events = append(h.events, e)
		fail := h.adminFailure
		h.mu.Unlock()
		if fail {
			return restResponse(t, 503, contracts.Problem{Status: 503}), nil
		}
		return restResponse(t, 202, contracts.EventAck{EventID: e.EventID, Accepted: true}), nil
	})
	return h
}

func postJSON(t *testing.T, handler http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return requestJSON(handler, http.MethodPost, path, string(data))
}
func requestJSON(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

type httpFixture struct {
	Request    contracts.DecisionRequest    `json:"request"`
	Statements contracts.StatementsResponse `json:"service_b_response"`
	Expected   struct {
		Response         contracts.DecisionResponse     `json:"response"`
		StatementsCalled bool                           `json:"statements_called"`
		BankCalled       bool                           `json:"ms2_called"`
		AdminCalled      bool                           `json:"admin_called"`
		BankRequest      *contracts.BankCheckRequest    `json:"bank_check_request"`
		ManualRequest    *contracts.ManualReviewRequest `json:"manual_review_request"`
		Event            *contracts.AdminEvent          `json:"notification"`
		Before           contracts.BankState            `json:"bank_state_before"`
		Saved            contracts.BankState            `json:"bank_state_saved"`
	} `json:"expected"`
}

// This runs both real Gin APIs and all real domain components. Only external
// B/MS3 and the network between the two apps are replaced; no sockets are bound.
func TestFullHTTPProcessAcrossScenarioManifest(t *testing.T) {
	var manifest struct {
		Scenarios []struct {
			ID      string   `json:"id"`
			Initial string   `json:"initial_bank"`
			Steps   []string `json:"steps"`
		} `json:"scenarios"`
	}
	restFixture(t, "scenarios.json", &manifest)
	if len(manifest.Scenarios) != 17 {
		t.Fatal("scenario manifest changed")
	}
	steps := 0
	for _, scenario := range manifest.Scenarios {
		t.Run(scenario.ID, func(t *testing.T) {
			h := newHTTPPipeline(t, filepath.Join("../../testdata", scenario.Initial))
			for _, step := range scenario.Steps {
				steps++
				var f httpFixture
				restFixture(t, step, &f)
				compareWire(t, h.ms2.Operations.Snapshot().State, f.Expected.Before)
				h.statements = f.Statements
				beforeB, beforeBank, beforeAdmin := h.bCalls, len(h.bankCalls), len(h.events)
				w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
				if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				var actual contracts.DecisionResponse
				if err := json.Unmarshal(w.Body.Bytes(), &actual); err != nil {
					t.Fatal(err)
				}
				compareWire(t, actual, f.Expected.Response)
				// Compare the raw JSON too: an accidental private response field must fail.
				compareWire(t, json.RawMessage(w.Body.Bytes()), f.Expected.Response)
				compareWire(t, h.ms2.Operations.Snapshot().State, f.Expected.Saved)
				if h.bCalls-beforeB != boolCount(f.Expected.StatementsCalled) || len(h.bankCalls)-beforeBank != boolCount(f.Expected.BankCalled) || len(h.events)-beforeAdmin != boolCount(f.Expected.AdminCalled) {
					t.Fatal("unexpected B/MS2/MS3 calls")
				}
				if f.Expected.BankRequest != nil {
					if h.bankCalls[beforeBank] != "/api/v1/bank/check-and-issue" {
						t.Fatal("wrong MS2 route")
					}
					compareWire(t, h.bankInputs[beforeBank], f.Expected.BankRequest)
				}
				if f.Expected.ManualRequest != nil {
					if h.bankCalls[beforeBank] != "/api/v1/bank/manual-reviews" {
						t.Fatal("wrong manual route")
					}
					compareWire(t, h.bankInputs[beforeBank], f.Expected.ManualRequest)
				}
				if f.Expected.Event != nil {
					compareWire(t, h.events[beforeAdmin], f.Expected.Event)
				}
				state := requestJSON(h.ms2.Server.Handler, "GET", "/api/v1/bank/state", "")
				if state.Code != 200 {
					t.Fatal(state.Code)
				}
				compareWire(t, json.RawMessage(state.Body.Bytes()), h.ms2.Operations.Snapshot())
			}
		})
	}
	if steps != 18 {
		t.Fatalf("steps=%d", steps)
	}
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Only reason wording/check details may differ from fixtures. Numeric values,
// codes, field sets, public messages, payload shape and side effects are checked.
func compareWire(t *testing.T, actual, expected any) {
	t.Helper()
	decode := func(v any) any {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var out any
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	var compare func(any, any, string)
	compare = func(a, b any, path string) {
		switch want := b.(type) {
		case map[string]any:
			got, ok := a.(map[string]any)
			if !ok || len(got) != len(want) {
				t.Fatalf("%s: shape %v want %v", path, a, b)
			}
			for key, v := range want {
				if key == "detail" || key == "message" && strings.Contains(path, "reasons[") {
					if s, ok := got[key].(string); !ok || s == "" {
						t.Fatalf("%s.%s is empty", path, key)
					}
					continue
				}
				compare(got[key], v, path+"."+key)
			}
		case []any:
			got, ok := a.([]any)
			// Existing scoring adds source fields to several manual reasons whose
			// fixtures leave fields empty. Preserve these useful explanations;
			// every explicitly required fixture field must still be present.
			if ok && strings.HasSuffix(path, ".fields") && strings.Contains(path, "reasons[") && len(want) == 0 {
				return
			}
			if !ok || len(got) != len(want) {
				t.Fatalf("%s: array %v want %v", path, a, b)
			}
			if strings.HasSuffix(path, ".fields") {
				sort.Slice(got, func(i, j int) bool { return fmt.Sprint(got[i]) < fmt.Sprint(got[j]) })
				sort.Slice(want, func(i, j int) bool { return fmt.Sprint(want[i]) < fmt.Sprint(want[j]) })
			}
			for i, v := range want {
				compare(got[i], v, fmt.Sprintf("%s[%d]", path, i))
			}
		case float64:
			got, ok := a.(float64)
			tol := 1e-9
			if strings.Contains(strings.ToLower(path), "rub") {
				tol = 0.01
			}
			if !ok || math.Abs(got-want) > tol {
				t.Fatalf("%s: %.15g want %.15g", path, got, want)
			}
		default:
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("%s: %v want %v", path, a, b)
			}
		}
	}
	compare(decode(actual), decode(expected), "response")
}
