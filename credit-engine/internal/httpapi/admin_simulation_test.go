package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func simulationSampleFixture(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("../../testdata/decisions/uc04_ip_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(map[string]any{"evaluation_date": "2026-10-23", "application": fixture.Request, "statements": nil, "facts_date": nil, "additional_facts": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestSimulationSampleRejectsAliasesDuplicatesAndUndeclaredReportNames(t *testing.T) {
	base := simulationSampleFixture(t)
	if _, err := decodeSimulationSample(base); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"root alias", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"evaluation_date"`), []byte(`"Evaluation_Date"`), 1)
		}},
		{"duplicate", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"evaluation_date":`), []byte(`"evaluation_date":"2026-10-22","evaluation_date":`), 1)
		}},
		{"application alias", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"application_id"`), []byte(`"Application_ID"`), 1)
		}},
		{"reports alias", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"statements":null`), []byte(`"statements":{"Borrower_ID":"x","annual_reports":[]}`), 1)
		}},
		{"reports duplicate", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"statements":null`), []byte(`"statements":{"borrower_id":"x","borrower_id":"y","annual_reports":[]}`), 1)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeSimulationSample(tc.mutate(base)); err == nil {
				t.Fatal("invalid sample accepted")
			}
		})
	}
}
func TestSimulationRoutesRequireBrowserAccess(t *testing.T) {
	r := NewAdminRouter(adminTestAccess(t), time.Second)
	RegisterAdminSimulation(r, adminTestAccess(t), nil)
	for _, path := range []string{"/api/admin/v1/simulations", "/api/admin/v1/simulations/compare"} {
		request := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		request.Header.Set("Authorization", "Bearer fictional-ms2-service-token-1234567890")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, request)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}
