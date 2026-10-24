package httptransport_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

// collectionBody — задача на сбор в ответах POST /applications/{id}/questionnaire и GET /collections/{id}.
type collectionBody struct {
	ID              string     `json:"id"`
	ApplicationID   string     `json:"application_id"`
	QuestionnaireID string     `json:"questionnaire_id"`
	INN             string     `json:"inn"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	Error           string     `json:"error"`
	SkippedSources  []string   `json:"skipped_sources"`
	DocumentIDs     []string   `json:"document_ids"`
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

func (a *testApp) getCollection(t *testing.T, id string) (int, collectionBody, string) {
	t.Helper()
	w := a.get("/api/v1/collections/" + id)
	var body collectionBody
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, body, w.Body.String()
}

// panicSystems — сбор, который падает.
type panicSystems struct{ stubSystems }

func (panicSystems) CollectAll(context.Context, string, domain.CollectInput) []domain.SystemResult {
	panic("collector bug")
}

// Анкета принята → PENDING; worker выполнил задачу → COMPLETED с итогом по каждой системе,
// заявка VERIFYING с реквизитами компании из ЕГРЮЛ.
func TestGetCollection(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)
	decodeSubmitted(t, a.submitMultipart(t, appID, fullQuestionnaire, file{"balance.pdf", "%PDF"}))

	code, c, _ := a.getCollection(t, "tsk_test")
	if code != http.StatusOK || c.Status != "PENDING" || c.ApplicationID != appID || c.Sources != nil || c.StartedAt != nil ||
		len(c.DocumentIDs) != 1 {
		t.Fatalf("pending: %d %+v", code, c)
	}

	runWorker(t, stubSystems{}, a.tasks)

	code, c, raw := a.getCollection(t, "tsk_test")
	if code != http.StatusOK || c.Status != "COMPLETED" || c.Attempts != 1 || c.RawCollectionID != "col_test" ||
		c.StartedAt == nil || c.FinishedAt == nil || len(c.Sources) != len(domain.Systems) {
		t.Fatalf("completed: %d %+v", code, c)
	}
	states := map[string]string{}
	for _, s := range c.Sources {
		states[s.Source] = s.State
		if s.Source == "FSSP" && (s.FailedCalls != 1 || s.Reason == "") {
			t.Errorf("fssp = %+v", s)
		}
	}
	if states["CORE_BANKING"] != "DONE" || states["MARKETPLACES"] != "SKIPPED" || states["FSSP"] != "FAILED" {
		t.Fatalf("states = %v", states)
	}
	// в ответе только состояние, сами данные и токены — нет
	if strings.Contains(raw, "secret") || strings.Contains(raw, `"data"`) {
		t.Fatalf("response leaks data or tokens: %s", raw)
	}

	app := decodeApplication(t, a.get("/api/v1/applications/"+appID).Body.Bytes())
	if app.Status != "VERIFYING" || app.Applicant.Name != `ООО "ТЕСТ"` || app.Applicant.OGRN != "1217700412345" {
		t.Fatalf("application after collection = %+v", app.Applicant)
	}
	want := []string{"application.created", "application.submitted", "enrich.started", "enrich.completed"}
	if ev := a.apps.Events(appID); !slices.Equal(ev, want) {
		t.Fatalf("events = %v, want %v", ev, want)
	}
}

// Сбор упал — FAILED с причиной, итога по системам нет, заявка остаётся в ENRICHING.
func TestGetCollectionFailed(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)
	decodeSubmitted(t, a.submitMultipart(t, appID, fullQuestionnaire, file{"balance.pdf", "%PDF"}))

	runWorker(t, panicSystems{}, a.tasks)

	code, c, _ := a.getCollection(t, "tsk_test")
	if code != http.StatusOK || c.Status != "FAILED" || !strings.Contains(c.Error, "collector bug") || c.FinishedAt == nil ||
		c.Sources != nil {
		t.Fatalf("failed: %d %+v", code, c)
	}
	if app := decodeApplication(t, a.get("/api/v1/applications/"+appID).Body.Bytes()); app.Status != "ENRICHING" {
		t.Fatalf("application = %s", app.Status)
	}
	if ev := a.apps.Events(appID); ev[len(ev)-1] != "enrich.failed" {
		t.Fatalf("events = %v", ev)
	}
}

func TestGetCollectionNotFound(t *testing.T) {
	code, _, raw := newTestApp(t).getCollection(t, "tsk_unknown")

	var p problemBody
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusNotFound || p.Code != "NOT_FOUND" || p.Status != http.StatusNotFound {
		t.Fatalf("%d %s", code, raw)
	}
}

// stubCollections — сервис задач, у которого не работает хранилище.
type stubCollections struct{ err error }

func (s stubCollections) Get(context.Context, string) (domain.CollectionTask, error) {
	return domain.CollectionTask{}, s.err
}

func TestGetCollectionInternalError(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	apps := memory.NewApplicationRepository(tasks)
	collections := service.NewCollectionService(tasks)
	r := httptransport.NewRouter(
		httptransport.NewApplicationHandler(service.NewApplicationService(apps), discardLog),
		httptransport.NewQuestionnaireHandler(service.NewQuestionnaireService(apps, nil, collections), 1<<20, discardLog),
		httptransport.NewCollectionHandler(stubCollections{err: errors.New("db down")}, discardLog),
		httptransport.NewInternalDataHandler(stubInternalData{}, discardLog),
	)
	a := &testApp{r: r}

	code, _, raw := a.getCollection(t, "tsk_test")
	if code != http.StatusInternalServerError || strings.Contains(raw, "db down") {
		t.Fatalf("%d %s", code, raw)
	}
}
