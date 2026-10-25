package events_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/events"
)

func fixture(t *testing.T, name string) contracts.AdminEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../../testdata/decisions", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Expected struct {
			Notification *contracts.AdminEvent `json:"notification"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(data, &f); err != nil || f.Expected.Notification == nil {
		t.Fatal("missing event fixture", err)
	}
	return *f.Expected.Notification
}
func clone(t *testing.T, event contracts.AdminEvent) contracts.AdminEvent {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var result contracts.AdminEvent
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func requireError(t *testing.T, err error, status int) {
	t.Helper()
	var event *events.Error
	if !errors.As(err, &event) || event.Status != status || len(event.Fields) == 0 {
		t.Fatal("unexpected event error", err)
	}
}

func TestEveryExistingNotificationPreservesContract(t *testing.T) {
	files, err := filepath.Glob("../../testdata/decisions/*.json")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	kinds := map[contracts.EventType]bool{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Expected struct {
				Notification json.RawMessage `json:"notification"`
			} `json:"expected"`
		}
		if json.Unmarshal(data, &f) != nil || len(f.Expected.Notification) == 0 || bytes.Equal(f.Expected.Notification, []byte("null")) {
			continue
		}
		count++
		t.Run(filepath.Base(path), func(t *testing.T) {
			event, err := events.Decode(bytes.NewReader(f.Expected.Notification))
			if err != nil {
				t.Fatal(err)
			}
			kinds[event.Kind] = true
			canonical, err := events.Canonical(event)
			if err != nil {
				t.Fatal(err)
			}
			again, err := events.Decode(bytes.NewReader(canonical))
			if err != nil || !reflect.DeepEqual(event, again) {
				t.Fatal("event roundtrip changed payload", err)
			}
		})
	}
	if count < 10 || len(kinds) != 3 {
		t.Fatal("all three event shapes must be covered", count, kinds)
	}
}
func TestEventFalseZeroAndNullRemainDistinct(t *testing.T) {
	base := fixture(t, "uc04_ip_approved")
	falseValue := false
	zero := 0.0
	for _, tc := range []struct {
		name    string
		number  *float64
		boolean *bool
		wire    string
	}{{"false", nil, &falseValue, `"value":false`}, {"zero", &zero, nil, `"value":0`}, {"unknown", nil, nil, `"value":null`}} {
		t.Run(tc.name, func(t *testing.T) {
			event := clone(t, base)
			event.Score.Economics.Criteria = []contracts.EconomicCriterion{{ID: "new_named_fact", Value: tc.number, BooleanValue: tc.boolean, Points: 0, MaxPoints: 0}}
			data, err := events.Canonical(event)
			if err != nil || !bytes.Contains(data, []byte(tc.wire)) {
				t.Fatal("wrong union", err)
			}
			decoded, err := events.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			criterion := decoded.Score.Economics.Criteria[0]
			if (criterion.Value == nil) != (tc.number == nil) || (criterion.BooleanValue == nil) != (tc.boolean == nil) {
				t.Fatal("lost type")
			}
		})
	}
}
func TestEventSemanticValidation(t *testing.T) {
	base := fixture(t, "uc04_ip_approved")
	for _, tc := range []struct {
		name   string
		change func(*contracts.AdminEvent)
	}{{"id", func(e *contracts.AdminEvent) { e.EventID = "different" }}, {"kind", func(e *contracts.AdminEvent) { e.Kind = "UNKNOWN" }}, {"time", func(e *contracts.AdminEvent) { e.OccurredAt = "2026-10-25" }}, {"UTC", func(e *contracts.AdminEvent) { e.OccurredAt = "2026-10-25T12:00:00+03:00" }}, {"text NUL", func(e *contracts.AdminEvent) { e.Score.Questionnaire.Criteria[0].Answer = "bad\x00text" }}, {"UTF8", func(e *contracts.AdminEvent) { e.Borrower.ID = string([]byte{0xff}) }}, {"rate percent", func(e *contracts.AdminEvent) { rate := 101.0; e.RequestedTerms.MaxAnnualRate = &rate }}, {"rate decimals", func(e *contracts.AdminEvent) { e.Terms.AnnualRate = 23.751 }}, {"terms", func(e *contracts.AdminEvent) { e.Terms.TermMonths = 37 }}, {"loan id", func(e *contracts.AdminEvent) { e.LoanID = nil }}, {"selection", func(e *contracts.AdminEvent) { e.Selection = nil }}, {"score", func(e *contracts.AdminEvent) { e.Score.Total = 1001 }}, {"nonfinite", func(e *contracts.AdminEvent) { e.Score.RawTotal = math.NaN() }}, {"ambiguous union", func(e *contracts.AdminEvent) {
		n, b := 0.0, false
		e.Score.Economics.Criteria[0].Value = &n
		e.Score.Economics.Criteria[0].BooleanValue = &b
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			event := clone(t, base)
			tc.change(&event)
			_, err := events.Canonical(event)
			requireError(t, err, 422)
		})
	}
	for _, name := range []string{"uc03_ip_manual", "uc05_ip_bank_review"} {
		event := fixture(t, name)
		loan := "unexpected-loan"
		event.LoanID = &loan
		_, err := events.Canonical(event)
		requireError(t, err, 422)
	}
}
func TestEventStrictNamedStructureAndBounds(t *testing.T) {
	data, err := events.Canonical(fixture(t, "uc04_ip_approved"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]json.RawMessage)
	}{{"missing", func(f map[string]json.RawMessage) { delete(f, "occurred_at") }}, {"alias", func(f map[string]json.RawMessage) { f["Event_ID"] = f["event_id"] }}, {"null array", func(f map[string]json.RawMessage) { f["reasons"] = []byte("null") }}, {"unknown", func(f map[string]json.RawMessage) { f["secret"] = []byte(`"not allowed"`) }}, {"nested alias", func(f map[string]json.RawMessage) {
		var borrower map[string]json.RawMessage
		json.Unmarshal(f["borrower"], &borrower)
		borrower["ID"] = borrower["id"]
		f["borrower"], _ = json.Marshal(borrower)
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			changed := map[string]json.RawMessage{}
			for key, value := range fields {
				changed[key] = value
			}
			tc.change(changed)
			raw, _ := json.Marshal(changed)
			_, err := events.Decode(bytes.NewReader(raw))
			requireError(t, err, 400)
		})
	}
	duplicate := bytes.Replace(data, []byte(`"event_id":`), []byte(`"event_id":"duplicate","event_id":`), 1)
	_, err = events.Decode(bytes.NewReader(duplicate))
	requireError(t, err, 400)
	_, err = events.Decode(strings.NewReader(strings.Repeat("x", 1<<20+1)))
	requireError(t, err, 413)
	_, err = events.Decode(bytes.NewReader(append(data, []byte(` {}`)...)))
	requireError(t, err, 400)
}
