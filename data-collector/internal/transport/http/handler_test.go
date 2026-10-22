package httptransport_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func newRouter(t *testing.T) (*gin.Engine, *memory.QuestionnaireRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := memory.NewQuestionnaireRepository()
	svc := service.New(repo,
		service.WithClock(func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }),
		service.WithIDGenerator(func() string { return "qst_test" }),
	)
	r := gin.New()
	httptransport.NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))).Register(r.Group("/api/v1"))
	return r, repo
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

func TestCreateQuestionnaire(t *testing.T) {
	r, repo := newRouter(t)

	w := post(r, `{"age":34,"marital_status":"MARRIED","children_count":0}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	want := `{"id":"qst_test","age":34,"marital_status":"MARRIED","children_count":0,"created_at":"2026-10-22T12:00:00Z"}`
	if got := w.Body.String(); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	saved, ok := repo.Get(context.Background(), "qst_test")
	if !ok || saved.MaritalStatus != domain.MaritalMarried || saved.ChildrenCount != 0 {
		t.Fatalf("saved = %+v (found %v)", saved, ok)
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
		{name: "missing children_count", body: `{"age":34,"marital_status":"SINGLE"}`, wantStatus: http.StatusBadRequest, wantFields: []string{"children_count"}},
		{name: "wrong type", body: `{"age":"34","marital_status":"SINGLE","children_count":1}`, wantStatus: http.StatusBadRequest, wantFields: []string{"age"}},
		{name: "unknown marital status", body: `{"age":34,"marital_status":"UNKNOWN","children_count":1}`, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"marital_status"}},
		{name: "age out of range", body: `{"age":12,"marital_status":"SINGLE","children_count":1}`, wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"age"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, repo := newRouter(t)

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
			for i, f := range tt.wantFields {
				if i >= len(p.Errors) || p.Errors[i].Field != f {
					t.Fatalf("errors = %+v, want fields %v", p.Errors, tt.wantFields)
				}
			}
			if _, ok := repo.Get(context.Background(), "qst_test"); ok {
				t.Fatal("invalid request must not be saved")
			}
		})
	}
}
