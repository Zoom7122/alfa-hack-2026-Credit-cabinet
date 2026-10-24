package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type decisionFunc func(context.Context, contracts.DecisionRequest) (contracts.DecisionResponse, error)

func (f decisionFunc) Decide(c context.Context, r contracts.DecisionRequest) (contracts.DecisionResponse, error) {
	return f(c, r)
}

const envelopeJSON = `{"application_id":"app","borrower":{"id":"borrower","type":"SOLE_PROPRIETOR"}}`

func TestProblemResponsesAndSafeSerialization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		nan    bool
	}{
		{"unexpected", errors.New("confidential"), 500, false},
		{"wrapped validation", fmt.Errorf("private: %w", &scoring.Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{"credit_request.max_annual_rate"}}), 422, false},
		{"bank conflict", &bank.Error{Status: 409, Code: "APPLICATION_CONFLICT"}, 409, false},
		{"client failure", &clients.Error{Status: 502, Code: "DEPENDENCY_UNAVAILABLE"}, 502, false},
		{"invalid error status", &scoring.Error{Status: 200, Code: "confidential"}, 500, false},
		{"deadline", context.DeadlineExceeded, 504, false},
		{"cancelled", context.Canceled, 502, false},
		{"nonfinite response", nil, 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httpapi.NewScoringRouter(decisionFunc(func(context.Context, contracts.DecisionRequest) (contracts.DecisionResponse, error) {
				if tc.nan {
					return contracts.DecisionResponse{MonthlyPaymentRUB: floatPointer(math.NaN())}, nil
				}
				return contracts.DecisionResponse{}, tc.err
			}))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/decisions", strings.NewReader(envelopeJSON)))
			if w.Code != tc.status || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "confidential") {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func floatPointer(v float64) *float64 { return &v }

func TestUnknownRoutesMethodsAndExternalEndpoints(t *testing.T) {
	r := httpapi.NewScoringRouter(decisionFunc(func(context.Context, contracts.DecisionRequest) (contracts.DecisionResponse, error) {
		t.Fatal("unexpected service call")
		return contracts.DecisionResponse{}, nil
	}))
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/api/v1/decisions", 405}, {"POST", "/api/v1/decisions/", 404}, {"GET", "/api/v1/bank/state", 404}, {"POST", "/api/v1/credit-events", 404}, {"GET", "/api/v1/borrowers/b/statements", 404}, {"POST", "/healthz", 405}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
