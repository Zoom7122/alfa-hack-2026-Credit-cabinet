package app_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func TestHTTPModelVersionSelectionAndHeader(t *testing.T) {
	for _, test := range []struct {
		name, version string
		status        int
		header        string
	}{{"legacy", "", 200, "ip-v2"}, {"explicit", `"ip-v2"`, 200, "ip-v2"}, {"null", "null", 400, ""}, {"number", "1", 400, ""}, {"empty", `""`, 422, ""}, {"space", `" "`, 422, ""}, {"unknown", `"ip-missing"`, 422, ""}, {"other_borrower", `"legal-v2"`, 422, ""}} {
		t.Run(test.name, func(t *testing.T) {
			h := newHTTPPipeline(t, "../../config/bank.default.json")
			var f httpFixture
			restFixture(t, "decisions/uc04_ip_approved.json", &f)
			body := jsonNumber(t, f.Request)
			if test.version != "" {
				body = strings.Replace(body, `"application_id":`, `"model_version":`+test.version+`,"application_id":`, 1)
			}
			w := requestJSON(h.ms1.Server.Handler, "POST", "/api/v1/decisions", string(body))
			if w.Code != test.status || w.Header().Get("X-Scoring-Model-Version") != test.header {
				t.Fatalf("%d header %q body %s", w.Code, w.Header().Get("X-Scoring-Model-Version"), w.Body.String())
			}
			if test.status != 200 && (len(h.bankCalls) != 0 || h.bCalls != 0) {
				t.Fatal("invalid version called dependencies")
			}
			if test.status == 200 {
				var out contracts.DecisionResponse
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Score == nil || out.Score.Model != test.header {
					t.Fatal(out, err)
				}
			}
		})
	}
}
func TestHTTPVersionHeaderOnEarlyOutcomesAndDependencyError(t *testing.T) {
	for _, mode := range []string{"stop", "need_info", "dependency"} {
		t.Run(mode, func(t *testing.T) {
			h := newHTTPPipeline(t, "../../config/bank.default.json")
			var f httpFixture
			file := "decisions/uc04_ip_approved.json"
			want := "ip-v2"
			if mode == "dependency" {
				file = "decisions/uc04_le_approved.json"
				want = "legal-v2"
			}
			restFixture(t, file, &f)
			h.statements = f.Statements
			if mode == "stop" {
				v := true
				f.Request.Scoring.IP.Bankrupt = &v
			}
			if mode == "need_info" {
				f.Request.Borrower.Name = nil
			}
			if mode == "dependency" {
				h.ms1.StatementsHTTP.Transport = restRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("private") })
			}
			w := postJSON(t, h.ms1.Server.Handler, "/api/v1/decisions", f.Request)
			if w.Header().Get("X-Scoring-Model-Version") != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if mode == "dependency" && w.Code != 502 {
				t.Fatal(w.Code)
			}
			if mode != "dependency" && w.Code != 200 {
				t.Fatal(w.Code)
			}
		})
	}
}
