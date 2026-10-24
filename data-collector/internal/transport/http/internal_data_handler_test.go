package httptransport_test

import (
	"encoding/json"
	"errors"
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

func newCheckMockRouter(svc httptransport.InternalDataService) *gin.Engine {
	tasks := memory.NewCollectionTaskRepository()
	apps := memory.NewApplicationRepository(tasks)
	collections := service.NewCollectionService(tasks)
	return httptransport.NewRouter(
		httptransport.NewApplicationHandler(service.NewApplicationService(apps), discardLog),
		httptransport.NewQuestionnaireHandler(service.NewQuestionnaireService(apps, nil, collections), 1<<20, discardLog),
		httptransport.NewCollectionHandler(collections, discardLog),
		httptransport.NewInternalDataHandler(svc, discardLog),
	)
}

func getCheckMock(r http.Handler, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/check-mock"+query, nil))
	return w
}

func TestCheckMockPartialData(t *testing.T) {
	r := newCheckMockRouter(stubInternalData{data: domain.InternalBankData{
		Credits: &domain.CreditReport{Bureau: "НБКИ"},
		Sources: []domain.SourceResult{
			{Source: domain.SourceCredits, State: domain.SourceDone, Duration: 12 * time.Millisecond},
			{Source: domain.SourceAcquiring, State: domain.SourceFailed, Error: "bank acquiring: status 503"},
		},
	}})

	w := getCheckMock(r, "?inn=7701234567")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var body struct {
		INN     string `json:"inn"`
		Sources []struct {
			Source     string `json:"source"`
			State      string `json:"state"`
			Error      string `json:"error"`
			DurationMs int64  `json:"duration_ms"`
		} `json:"sources"`
		Credits   *struct{ Bureau string } `json:"credits"`
		Acquiring json.RawMessage          `json:"acquiring"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.INN != "7701234567" || body.Credits == nil || body.Credits.Bureau != "НБКИ" {
		t.Fatalf("body = %s", w.Body)
	}
	if string(body.Acquiring) != "null" {
		t.Fatalf("failed source must be null, got %s", body.Acquiring)
	}
	if len(body.Sources) != 2 || body.Sources[0].DurationMs != 12 ||
		body.Sources[1].State != "FAILED" || body.Sources[1].Error == "" {
		t.Fatalf("sources = %+v", body.Sources)
	}
}

func TestCheckMockErrors(t *testing.T) {
	tests := []struct {
		name       string
		svc        stubInternalData
		query      string
		wantStatus int
		wantCode   string
	}{
		{name: "no inn", query: "", wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED"},
		{
			name:       "invalid inn",
			svc:        stubInternalData{err: &service.ValidationError{Fields: []service.FieldError{{Field: "inn", Code: "invalid_format"}}}},
			query:      "?inn=123",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "VALIDATION_FAILED",
		},
		{
			name:       "unexpected error",
			svc:        stubInternalData{err: errors.New("boom")},
			query:      "?inn=7701234567",
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := getCheckMock(newCheckMockRouter(tt.svc), tt.query)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body)
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Fatalf("Content-Type = %q", ct)
			}
			var p problemBody
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.Code != tt.wantCode {
				t.Fatalf("problem = %+v, err = %v", p, err)
			}
		})
	}
}
