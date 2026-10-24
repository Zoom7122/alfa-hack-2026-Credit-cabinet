package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func newRouter(t *testing.T) (*gin.Engine, *memory.QuestionnaireRepository, *memory.CollectionTaskRepository) {
	t.Helper()

	repo := memory.NewQuestionnaireRepository()
	tasks := memory.NewCollectionTaskRepository()
	collections := newCollectionService(tasks)
	svc := service.NewQuestionnaireService(repo, collections,
		service.WithClock(func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }),
		service.WithIDGenerator(func() string { return "qst_test" }),
	)
	r := httptransport.NewRouter(
		httptransport.NewQuestionnaireHandler(svc, discardLog),
		httptransport.NewCollectionHandler(collections, discardLog),
		httptransport.NewInternalDataHandler(stubInternalData{}, discardLog),
	)
	return r, repo, tasks
}

func post(r http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/questionnaires", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type problemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Errors []struct {
		Field string `json:"field"`
		Code  string `json:"code"`
	} `json:"errors"`
}

const fullRequest = `{
	"age": 42, "marital_status": "MARRIED", "children_count": 2,
	"inn": "7799000012",
	"director": {"inn": "779900010139", "last_name": "Ковалёв", "first_name": "Игорь", "middle_name": "Сергеевич",
		"birth_date": "1984-07-19"},
	"consents": {
		"bki_company": {"id": "CNS-1", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
		"bki_person": {"id": "CNS-2", "valid_until": "2027-04-21"},
		"tax": {"id": "TAX-1", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
	},
	"other_bank_biks": ["044525225"],
	"marketplace_tokens": {"wildberries": "secret-wb"},
	"ofd_token": "secret-ofd",
	"collateral": {"cadastral_numbers": ["50:21:0110203:845"], "vins": ["xtc652005n1498213"], "serials": []},
	"poa_number": "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
	"representative_inn": "779900010333"
}`

// collectionBody — задача на сбор в ответах POST /questionnaires и GET /collections/{id}.
type collectionBody struct {
	ID              string     `json:"id"`
	QuestionnaireID string     `json:"questionnaire_id"`
	INN             string     `json:"inn"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	Error           string     `json:"error"`
	SkippedSources  []string   `json:"skipped_sources"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	RawCollectionID string     `json:"raw_collection_id"`
	Sources         []struct {
		Source      string `json:"source"`
		State       string `json:"state"`
		Reason      string `json:"reason"`
		Calls       int    `json:"calls"`
		FailedCalls int    `json:"failed_calls"`
		DurationMs  int64  `json:"duration_ms"`
	} `json:"sources"`
}

type createdBody struct {
	ID            string         `json:"id"`
	Age           int            `json:"age"`
	MaritalStatus string         `json:"marital_status"`
	ChildrenCount int            `json:"children_count"`
	CreatedAt     time.Time      `json:"created_at"`
	Collection    collectionBody `json:"collection"`
}

func TestCreateQuestionnaire(t *testing.T) {
	r, repo, tasks := newRouter(t)

	w := post(r, fullRequest)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if loc := w.Header().Get("Location"); loc != "/api/v1/collections/tsk_test" {
		t.Fatalf("Location = %q", loc)
	}
	var body createdBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "qst_test" || body.Age != 42 || body.MaritalStatus != "MARRIED" || body.ChildrenCount != 2 ||
		!body.CreatedAt.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("questionnaire = %+v", body)
	}
	// задача поставлена в очередь: системы ещё не опрашивались, для полной анкеты пропусков нет
	c := body.Collection
	if c.ID != "tsk_test" || c.QuestionnaireID != "qst_test" || c.INN != "7799000012" || c.Status != "PENDING" ||
		c.Attempts != 0 || c.SkippedSources == nil || len(c.SkippedSources) != 0 || c.StartedAt != nil || c.FinishedAt != nil ||
		c.RawCollectionID != "" || c.Sources != nil {
		t.Fatalf("collection = %+v", c)
	}
	// в ответе токенов нет
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response leaks tokens: %s", w.Body)
	}

	if _, ok := repo.Get(context.Background(), "qst_test"); !ok {
		t.Fatal("questionnaire is not saved")
	}
	task, ok, err := tasks.Get(context.Background(), "tsk_test")
	if err != nil || !ok || task.QuestionnaireID != "qst_test" || task.Status != domain.TaskPending {
		t.Fatalf("task = %+v (found %v, err %v)", task, ok, err)
	}
	// worker'у достанутся нормализованные данные вместе с токенами
	in := task.Input
	if in.Director == nil || in.Director.LastName != "Ковалёв" || in.BKIPersonConsent == nil || in.TaxConsent.Scope[1] != "ENS_BALANCE" ||
		in.MarketplaceTokens["wildberries"] != "secret-wb" || in.OFDToken != "secret-ofd" ||
		in.Collateral.VINs[0] != "XTC652005N1498213" || in.RepresentativeINN != "779900010333" {
		t.Fatalf("collect input = %+v", in)
	}
	if out := tasks.Outbox(); len(out) != 1 || out[0].Key != "tsk_test" || !strings.Contains(string(out[0].Payload), `"task_id":"tsk_test"`) {
		t.Fatalf("outbox = %+v", out)
	}
}

// Достаточно ИНН: остальное необязательно, системы без данных сразу перечислены в skipped_sources.
func TestCreateQuestionnaireOnlyINN(t *testing.T) {
	r, _, tasks := newRouter(t)

	w := post(r, `{"age":34,"marital_status":"SINGLE","children_count":0,"inn":"779900010202"}`)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var body createdBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"BKI", "TAX_REPORTS", "EGRN", "GOSUSLUGI", "OPEN_BANKING", "MARKETPLACES", "OFD"}
	if !slices.Equal(body.Collection.SkippedSources, want) {
		t.Fatalf("skipped_sources = %v, want %v", body.Collection.SkippedSources, want)
	}
	if task, ok, _ := tasks.Get(context.Background(), "tsk_test"); !ok || task.Input.INN != "779900010202" || task.Input.Director != nil {
		t.Fatalf("task = %+v (found %v)", task, ok)
	}
}

func TestCreateQuestionnaireErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantFields []string
	}{
		{name: "empty body", body: ``, wantStatus: http.StatusBadRequest},
		{name: "broken json", body: `{"age":`, wantStatus: http.StatusBadRequest},
		{name: "missing children_count", body: `{"age":34,"marital_status":"SINGLE","inn":"7799000012"}`, wantStatus: http.StatusBadRequest, wantFields: []string{"children_count"}},
		{name: "missing inn", body: `{"age":34,"marital_status":"SINGLE","children_count":1}`, wantStatus: http.StatusBadRequest, wantFields: []string{"inn"}},
		{name: "wrong type", body: `{"age":"34","marital_status":"SINGLE","children_count":1,"inn":"7799000012"}`, wantStatus: http.StatusBadRequest, wantFields: []string{"age"}},
		{name: "wrong nested type", body: `{"age":34,"marital_status":"SINGLE","children_count":1,"inn":"7799000012","director":{"inn":779900010139}}`, wantStatus: http.StatusBadRequest, wantFields: []string{"director.inn"}},
		{name: "unknown marital status", body: `{"age":34,"marital_status":"UNKNOWN","children_count":1,"inn":"7799000012"}`, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"marital_status"}},
		{name: "age out of range", body: `{"age":12,"marital_status":"SINGLE","children_count":1,"inn":"7799000012"}`, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"age"}},
		{name: "bad inn", body: `{"age":34,"marital_status":"SINGLE","children_count":1,"inn":"123"}`, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"inn"}},
		{
			name:       "questionnaire and collection errors together",
			body:       `{"age":12,"marital_status":"SINGLE","children_count":1,"inn":"7799000012","other_bank_biks":["1"],"collateral":{"vins":["short"]}}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantFields: []string{"age", "other_bank_biks[0]", "collateral.vins[0]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, repo, tasks := newRouter(t)

			w := post(r, tt.body)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body)
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Fatalf("Content-Type = %q", ct)
			}
			var p problemBody
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != "VALIDATION_FAILED" || p.Status != tt.wantStatus {
				t.Fatalf("problem = %+v", p)
			}
			if tt.wantFields != nil && len(p.Errors) != len(tt.wantFields) {
				t.Fatalf("errors = %+v, want fields %v", p.Errors, tt.wantFields)
			}
			for i, f := range tt.wantFields {
				if p.Errors[i].Field != f {
					t.Fatalf("errors = %+v, want fields %v", p.Errors, tt.wantFields)
				}
			}
			if _, ok := repo.Get(context.Background(), "qst_test"); ok {
				t.Fatal("invalid request must not be saved")
			}
			if _, ok, _ := tasks.Get(context.Background(), "tsk_test"); ok || len(tasks.Outbox()) != 0 {
				t.Fatal("invalid request must not enqueue a collection")
			}
		})
	}
}
