package app_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
)

// Extend the isolated scoring requirements through the complete HTTP process:
// no stop or unconfirmed cash flow may accidentally reach the bank or admin.
func TestAcceptanceSingleFactorsHaveNoBankSideEffects(t *testing.T) {
	type example struct {
		Base   string                     `json:"base_case"`
		Set    map[string]json.RawMessage `json:"set"`
		Status contracts.DecisionStatus   `json:"expected_status"`
		Reason string                     `json:"expected_reason"`
	}
	var cases struct {
		Stops       []example `json:"single_factor_stops"`
		Unconfirmed []example `json:"cash_stop_confirmation"`
	}
	restFixture(t, "unit_cases.json", &cases)
	if len(cases.Stops) != 17 || len(cases.Unconfirmed) != 2 {
		t.Fatal("incomplete acceptance source")
	}
	for i, tc := range append(cases.Stops, cases.Unconfirmed...) {
		t.Run(fmt.Sprintf("%02d/%s/%s", i, tc.Base, tc.Reason), func(t *testing.T) {
			h := newHTTPPipeline(t, "../../config/bank.default.json")
			var f httpFixture
			restFixture(t, tc.Base, &f)
			h.statements = f.Statements
			data := json.RawMessage(jsonNumber(t, f.Request))
			for path, value := range tc.Set {
				data = replaceAcceptanceField(t, data, strings.Split(path, "."), value)
			}
			before := h.ms2.Operations.Snapshot()
			w := requestJSON(h.ms1.Server.Handler, "POST", "/api/v1/decisions", string(data))
			var result contracts.DecisionResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || result.Status != tc.Status || result.Score != nil || result.Terms != nil || result.LoanID != nil || result.TermsConfirmed || result.ReviewReason != nil || result.MonthlyPaymentRUB != nil || result.OverpaymentRUB != nil {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tc.Reason != "" {
				found := false
				for _, reason := range result.Reasons {
					found = found || reason.Code == tc.Reason
				}
				if !found {
					t.Fatalf("missing reason %s", tc.Reason)
				}
			}
			if len(h.bankCalls) != 0 || len(h.events) != 0 || loanCount(t, h) != 0 || pendingCount(t, h) != 0 {
				t.Fatal("terminal decision caused bank/admin side effects")
			}
			wantB := 0
			if tc.Status == contracts.DecisionStatusNeedInfo && f.Request.Borrower.Type == contracts.BorrowerTypeLegalEntity {
				wantB = 1
			}
			if h.bCalls != wantB {
				t.Fatalf("B calls=%d want %d", h.bCalls, wantB)
			}
			compareWire(t, h.ms2.Operations.Snapshot(), before)
		})
	}
}

// RawMessage preserves int64 money and null while applying test-only paths.
func replaceAcceptanceField(t *testing.T, data json.RawMessage, path []string, value json.RawMessage) json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	key := path[0]
	if strings.HasSuffix(key, "[*]") {
		key = strings.TrimSuffix(key, "[*]")
		var elements []json.RawMessage
		if err := json.Unmarshal(object[key], &elements); err != nil {
			t.Fatal(err)
		}
		for i := range elements {
			elements[i] = replaceAcceptanceField(t, elements[i], path[1:], value)
		}
		object[key] = json.RawMessage(jsonNumber(t, elements))
	} else if len(path) == 1 {
		object[key] = value
	} else {
		object[key] = replaceAcceptanceField(t, object[key], path[1:], value)
	}
	return json.RawMessage(jsonNumber(t, object))
}

func TestAcceptanceBankRestartResetsStateAndReplayRegistry(t *testing.T) {
	h := newHTTPPipeline(t, "../../config/bank.default.json")
	var f httpFixture
	restFixture(t, "decisions/uc04_ip_approved.json", &f)
	initial := h.ms2.Operations.Snapshot()
	w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
	if w.Code != 200 || h.ms2.Operations.Snapshot().State.Cash != 7000000 || loanCount(t, h) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	previous := h.ms2
	fresh, err := app.NewBank(previous.Settings, previous.Clock)
	if err != nil {
		t.Fatal(err)
	}
	fresh.AdminHTTP.Transport = previous.AdminHTTP.Transport
	h.ms2 = fresh
	compareWire(t, h.ms2.Operations.Snapshot(), initial)
	if loanCount(t, h) != 0 || pendingCount(t, h) != 0 {
		t.Fatal("restart retained operation registry")
	}
	w = postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
	if w.Code != 200 || h.ms2.Operations.Snapshot().State.Cash != 7000000 || loanCount(t, h) != 1 || len(h.events) != 2 {
		t.Fatalf("old MS1 kept a cache across bank restart: %d %s", w.Code, w.Body.String())
	}
}
