package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

type fakeBackofficeSvc struct {
	err     error
	decided *service.DecisionInput
	filter  domain.QueueFilter
}

func (f *fakeBackofficeSvc) Queue(_ context.Context, _ string, q domain.QueueFilter) ([]domain.QueueItem, domain.QueueCounters, int, error) {
	f.filter = q
	return []domain.QueueItem{{ApplicationID: "app_1", Number: "A-1", Status: domain.ApplicationManualReview,
		ReviewReasons: []string{"Много обращений"}, DegradedSources: []domain.System{domain.SystemKAD},
		Assignee: &domain.UserRef{ID: "usr_underwriter_orlov", FullName: "Орлов"}}}, domain.QueueCounters{ManualReview: 1, Mine: 1}, 50, f.err
}

func (f *fakeBackofficeSvc) Dossier(_ context.Context, id string) (domain.Dossier, error) {
	if f.err != nil {
		return domain.Dossier{}, f.err
	}
	return domain.Dossier{
		Application: domain.Application{ID: id, Status: domain.ApplicationManualReview, Company: domain.Company{ID: "apl_1", INN: "7799000012", Name: "ООО"}},
		Version:     4,
		Decision: &domain.DecisionRecord{AutoOutcome: "MANUAL_REVIEW", Details: json.RawMessage(`{
			"engine":{"status":"UNDER_REVIEW","review_reason":"BORROWER","reasons":[{"code":"MANY_CREDIT_APPLICATIONS","message":"Много","fields":["x"]}],
			          "score":{"model":"legal-v2","total":738,"grade":"B","questionnaire":{"criteria":[{"id":"LE_Q01","answer":"business_age_months=89","points":60,"max_points":60}]},
			                   "economics":{"criteria":[{"id":"LE_E01","value":1.6,"points":15,"max_points":20}]}}},
			"findings":[{"code":"UNDECLARED_DEBT","message":"m","material":true}],
			"proposed_offer":{"amount_rub":2030000,"term_months":36,"annual_rate":20.75,"monthly_payment":80000}}`)},
		Events: []domain.ApplicationEvent{
			{ID: "evt_1", Type: "uw.comment", ActorID: "usr_uw", ActorRole: domain.UserUnderwriter, Payload: json.RawMessage(`{"comment":"проверить"}`)},
			{ID: "evt_2", Type: "uw.decision", ActorID: "usr_uw", ActorRole: domain.UserUnderwriter, Message: "Банк одобрил кредит"},
		},
		Staff: map[string]domain.UserRef{"usr_uw": {ID: "usr_uw", FullName: "Соколова"}},
	}, nil
}

func (f *fakeBackofficeSvc) Assign(context.Context, string, string) (domain.QueueItem, error) {
	return domain.QueueItem{ApplicationID: "app_1"}, f.err
}

func (f *fakeBackofficeSvc) Decide(ctx context.Context, _ string, id string, in service.DecisionInput) (domain.Dossier, error) {
	f.decided = &in
	if f.err != nil {
		return domain.Dossier{}, f.err
	}
	return f.Dossier(ctx, id)
}

func backofficeApp(t *testing.T, svc *fakeBackofficeSvc) func(method, path, token, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	a := newTestApp(t).seedTestUsers(t)
	r := gin.New()
	api := r.Group("/api/v1")
	httptransport.WithAuth(httptransport.NewAuthHandler(a.auth, discardLog))(r, api)
	httptransport.WithBackoffice(httptransport.NewBackofficeHandler(svc, discardLog))(r, api)
	return func(method, path, token, body string, headers ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
}

const uw = "mock.usr_underwriter_sokolova"

func TestBackofficeAccess(t *testing.T) {
	do := backofficeApp(t, &fakeBackofficeSvc{})
	for token, want := range map[string]int{"": http.StatusUnauthorized, "mock.usr_director_north": http.StatusForbidden, uw: http.StatusOK} {
		if w := do(http.MethodGet, "/api/v1/backoffice/applications", token, ""); w.Code != want {
			t.Errorf("token %q: %d, want %d", token, w.Code, want)
		}
	}
}

func TestBackofficeQueueAndDossier(t *testing.T) {
	svc := &fakeBackofficeSvc{}
	do := backofficeApp(t, svc)
	w := do(http.MethodGet, "/api/v1/backoffice/applications?status=manual_review,WAITING_FOR_INFO&assignee=me&limit=10&cursor=20", uw, "")
	if w.Code != http.StatusOK {
		t.Fatalf("queue: %d %s", w.Code, w.Body)
	}
	q := decode[struct {
		Items []struct {
			ManualReviewReasons []string `json:"manual_review_reasons"`
			DegradedSources     []string `json:"degraded_sources"`
			Assignee            *struct {
				FullName string `json:"full_name"`
			} `json:"assignee"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
		Counters   struct {
			Mine int `json:"mine"`
		} `json:"counters"`
	}](t, w.Body.Bytes())
	if len(q.Items) != 1 || q.Items[0].DegradedSources[0] != "KAD" || q.Items[0].Assignee.FullName != "Орлов" ||
		q.NextCursor == nil || *q.NextCursor != "50" || q.Counters.Mine != 1 {
		t.Errorf("queue body: %s", w.Body)
	}
	if f := svc.filter; len(f.Statuses) != 2 || f.Assignee != "me" || f.Limit != 10 || f.Offset != 20 {
		t.Errorf("filter: %+v", f)
	}
	if w := do(http.MethodGet, "/api/v1/backoffice/applications?status=NOPE", uw, ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad status: %d", w.Code)
	}

	w = do(http.MethodGet, "/api/v1/backoffice/applications/app_1", uw, "")
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"4"` {
		t.Fatalf("dossier: %d %s", w.Code, w.Header())
	}
	body := w.Body.String()
	for _, want := range []string{`"version":4`, `"model_version":"legal-v2"`, `"title":"Сколько месяцев работает действующий бизнес организации"`,
		`"part":"ECONOMICS"`, `"engine_status":"UNDER_REVIEW"`, `"proposed_terms":{"amount":2030000`, `"severity":"WARN"`,
		`"message":"Внутренний комментарий: проверить"`, `"internal":true`, `"actor_kind":"EMPLOYEE"`, `"full_name":"Соколова"`} {
		if !strings.Contains(body, want) {
			t.Errorf("dossier misses %s", want)
		}
	}
}

func TestBackofficeDecisionErrors(t *testing.T) {
	svc := &fakeBackofficeSvc{}
	do := backofficeApp(t, svc)
	path := "/api/v1/backoffice/applications/app_1/decision"
	w := do(http.MethodPost, path, uw, `{"action":"approve","offers":[{"amount":1000000,"term_months":12,"rate_annual":22.5,"valid_days":7}],
		"expert_adjustments":[{"code":"X","points":10}],"internal_comment":"ok"}`, "If-Match", `W/"4"`, "Idempotency-Key", "key-1")
	if w.Code != http.StatusOK {
		t.Fatalf("decide: %d %s", w.Code, w.Body)
	}
	if d := svc.decided; d.Action != "APPROVE" || *d.IfMatch != 4 || d.IdempotencyKey != "key-1" || d.Offers[0].Rate != 22.5 || d.Adjustments[0].Points != 10 {
		t.Errorf("decision input: %+v", d)
	}
	if w := do(http.MethodPost, path, uw, `{"action":"APPROVE"}`, "If-Match", "abc"); w.Code != http.StatusBadRequest {
		t.Errorf("bad If-Match: %d", w.Code)
	}
	if w := do(http.MethodPost, path, uw, `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("no action: %d", w.Code)
	}
	for err, want := range map[error]int{
		service.ErrNotFound:       http.StatusNotFound,
		domain.ErrNotInReview:     http.StatusConflict,
		domain.ErrAssignedToOther: http.StatusConflict,
		domain.ErrVersionConflict: http.StatusPreconditionFailed,
		&service.ValidationError{Fields: []service.FieldError{{Field: "offers", Code: "required"}}}: http.StatusUnprocessableEntity,
	} {
		svc.err = err
		if w := do(http.MethodPost, path, uw, `{"action":"APPROVE"}`); w.Code != want {
			t.Errorf("%v: %d, want %d", err, w.Code, want)
		}
	}
	svc.err = domain.ErrAssignedToOther
	if w := do(http.MethodPost, "/api/v1/backoffice/applications/app_1/assign", uw, ""); w.Code != http.StatusConflict ||
		!strings.Contains(w.Body.String(), `"code":"CONFLICT"`) {
		t.Errorf("assign conflict: %d %s", w.Code, w.Body)
	}
}
