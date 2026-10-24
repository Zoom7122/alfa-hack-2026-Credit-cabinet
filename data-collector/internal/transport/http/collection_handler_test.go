package httptransport_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func getCollection(t *testing.T, r http.Handler, id string) (int, collectionBody, string) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/collections/"+id, nil))
	var body collectionBody
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, body, w.Body.String()
}

// panicSystems — сбор, который падает.
type panicSystems struct{}

func (panicSystems) CollectAll(context.Context, string, domain.CollectInput) []domain.SystemResult {
	panic("collector bug")
}

// Анкета принята → PENDING; worker выполнил задачу → COMPLETED с итогом по каждой системе.
func TestGetCollection(t *testing.T) {
	r, _, tasks := newRouter(t)
	if w := post(r, fullRequest); w.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, body = %s", w.Code, w.Body)
	}

	code, c, _ := getCollection(t, r, "tsk_test")
	if code != http.StatusOK || c.Status != "PENDING" || c.QuestionnaireID != "qst_test" || c.Sources != nil || c.StartedAt != nil {
		t.Fatalf("pending: %d %+v", code, c)
	}

	runWorker(t, stubSystems{}, tasks)

	code, c, raw := getCollection(t, r, "tsk_test")
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
}

// Сбор упал — FAILED с причиной, итога по системам нет.
func TestGetCollectionFailed(t *testing.T) {
	r, _, tasks := newRouter(t)
	post(r, fullRequest)

	runWorker(t, panicSystems{}, tasks)

	code, c, _ := getCollection(t, r, "tsk_test")
	if code != http.StatusOK || c.Status != "FAILED" || !strings.Contains(c.Error, "collector bug") || c.FinishedAt == nil ||
		c.Sources != nil {
		t.Fatalf("failed: %d %+v", code, c)
	}
}

func TestGetCollectionNotFound(t *testing.T) {
	r, _, _ := newRouter(t)

	code, _, raw := getCollection(t, r, "tsk_unknown")

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
	collections := newCollectionService(memory.NewCollectionTaskRepository())
	r := httptransport.NewRouter(
		httptransport.NewQuestionnaireHandler(service.NewQuestionnaireService(memory.NewQuestionnaireRepository(), collections), discardLog),
		httptransport.NewCollectionHandler(stubCollections{err: errors.New("db down")}, discardLog),
		httptransport.NewInternalDataHandler(stubInternalData{}, discardLog),
	)

	code, _, raw := getCollection(t, r, "tsk_test")
	if code != http.StatusInternalServerError || strings.Contains(raw, "db down") {
		t.Fatalf("%d %s", code, raw)
	}
}
