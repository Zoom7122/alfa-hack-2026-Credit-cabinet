package httptransport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

type fakeHooks struct {
	years  []int
	events []decision.CreditEvent
}

func (f *fakeHooks) Statements(_ context.Context, id string, years []int) (decision.Statements, error) {
	if id != "apl_1" {
		return decision.Statements{}, service.ErrNotFound
	}
	f.years = years
	return decision.Statements{BorrowerID: id, AnnualReports: []decision.AnnualReport{}}, nil
}

func (f *fakeHooks) CreditEvent(_ context.Context, ev decision.CreditEvent) error {
	f.events = append(f.events, ev)
	return nil
}

func TestEngineHooks(t *testing.T) {
	hooks := &fakeHooks{}
	r := gin.New()
	httptransport.WithEngineHooks(httptransport.NewEngineHandler(hooks, discardLog))(r, r.Group("/api/v1"))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	w := do(http.MethodGet, "/api/v1/borrowers/apl_1/statements?years=2024,2025", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"borrower_id":"apl_1"`) || len(hooks.years) != 2 || hooks.years[1] != 2025 {
		t.Errorf("statements: %d %s %v", w.Code, w.Body, hooks.years)
	}
	if w := do(http.MethodGet, "/api/v1/borrowers/apl_nope/statements?years=2025", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown borrower: %d", w.Code)
	}

	w = do(http.MethodPost, "/api/v1/credit-events", `{"event_id":"app_1:LOAN_ISSUED","kind":"LOAN_ISSUED","application_id":"app_1"}`)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"accepted":true`) || len(hooks.events) != 1 {
		t.Errorf("credit event: %d %s", w.Code, w.Body)
	}
	if w := do(http.MethodPost, "/api/v1/credit-events", `{"kind":"LOAN_ISSUED"}`); w.Code != http.StatusBadRequest {
		t.Errorf("event without id: %d", w.Code)
	}
}

func TestQuestionnaireScoringAnswers(t *testing.T) {
	a := newTestApp(t)
	bad := strings.Replace(fullQuestionnaire, `"age": 42,`, `"age": 42, "scoring": {"car_class": "TANK", "max_annual_rate": 150, "need_cost": -1},`, 1)
	app := a.register(t, litvinova)
	w := a.submitMultipart(t, app, bad, file{"balance.pdf", "%PDF"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad scoring: %d %s", w.Code, w.Body)
	}
	for _, field := range []string{"scoring.car_class", "scoring.max_annual_rate", "scoring.need_cost"} {
		if !strings.Contains(w.Body.String(), field) {
			t.Errorf("no error for %s: %s", field, w.Body)
		}
	}
	good := strings.Replace(fullQuestionnaire, `"age": 42,`, `"age": 42, "scoring": {"max_annual_rate": 24.5, "need_evidence": "CONTRACT_AND_COST", "experience_years": 9},`, 1)
	if w := a.submitMultipart(t, app, good, file{"balance.pdf", "%PDF"}); w.Code != http.StatusAccepted {
		t.Fatalf("good scoring: %d %s", w.Code, w.Body)
	}
	sub, ok := a.apps.Submission(app)
	if !ok || sub.Scoring == nil || *sub.Scoring.MaxAnnualRate != 24.5 || sub.Scoring.NeedEvidence != "CONTRACT_AND_COST" {
		t.Errorf("stored scoring: %+v", sub.Scoring)
	}
}
