package httptransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/worker"
)

var (
	discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))
	fixedNow   = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard // без строк access-лога в выводе тестов
	os.Exit(m.Run())
}

// stubInternalData — сервис внутренних данных с заранее заданным ответом.
type stubInternalData struct {
	data domain.InternalBankData
	err  error
}

func (s stubInternalData) Collect(_ context.Context, inn string) (domain.InternalBankData, error) {
	if s.err != nil {
		return domain.InternalBankData{}, s.err
	}
	d := s.data
	d.INN = inn
	return d, nil
}

// stubSystems — внешние системы: маркетплейсы без токена пропущены, ФССП не ответила, остальные DONE.
type stubSystems struct{}

func (stubSystems) CollectAll(_ context.Context, _ string, in domain.CollectInput) []domain.SystemResult {
	out := make([]domain.SystemResult, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		res := domain.SystemResult{System: sys, State: domain.CollectDone, Duration: 3 * time.Millisecond,
			Calls: []domain.Call{{Key: "k", Status: 200}}, Data: []byte(`{"inn":"` + in.INN + `"}`)}
		switch sys {
		case domain.SystemMarketplaces:
			res = domain.SystemResult{System: sys, State: domain.CollectSkipped, Reason: "нет токенов продавца marketplace_tokens"}
		case domain.SystemFSSP:
			res = domain.SystemResult{System: sys, State: domain.CollectFailed, Reason: "company: status 503",
				Calls: []domain.Call{{Key: "company", Status: 503, Error: "status 503"}}}
		}
		out = append(out, res)
	}
	return out
}

// CompanyCard — реквизиты из ЕГРЮЛ по любому ответу ФНС.
func (stubSystems) CompanyCard(fnsData json.RawMessage) (domain.CompanyCard, bool) {
	var d struct {
		INN string `json:"inn"`
	}
	if json.Unmarshal(fnsData, &d) != nil || d.INN == "" {
		return domain.CompanyCard{}, false
	}
	return domain.CompanyCard{INN: d.INN, Type: domain.CompanyLegalEntity, OGRN: "1217700412345", ShortName: `ООО "ТЕСТ"`}, true
}

// testApp — роутер со всеми хендлерами поверх хранилищ в памяти и временной папки для файлов.
type testApp struct {
	r     *gin.Engine
	tasks *memory.CollectionTaskRepository
	apps  *memory.ApplicationRepository
	root  string
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	tasks := memory.NewCollectionTaskRepository()
	apps := memory.NewApplicationRepository(tasks)
	root := t.TempDir()
	files, err := storage.NewLocal(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	collections := service.NewCollectionService(tasks,
		service.WithCollectionClock(func() time.Time { return fixedNow }),
		service.WithCollectionIDGenerator(func() string { return "tsk_test" }),
	)
	n := 0
	applications := service.NewApplicationService(apps,
		service.WithApplicationClock(func() time.Time { return fixedNow }),
		service.WithApplicationIDGenerator(func(prefix string) string { n++; return prefix + "test" + strings.Repeat("0", n) }),
	)
	questionnaires := service.NewQuestionnaireService(apps, files, collections,
		service.WithClock(func() time.Time { return fixedNow }),
		service.WithIDGenerator(func() string { return "qst_test" }),
	)
	r := httptransport.NewRouter(
		httptransport.NewApplicationHandler(applications, discardLog),
		httptransport.NewQuestionnaireHandler(questionnaires, 4<<20, discardLog),
		httptransport.NewCollectionHandler(collections, discardLog),
		httptransport.NewInternalDataHandler(stubInternalData{}, discardLog),
	)
	return &testApp{r: r, tasks: tasks, apps: apps, root: root}
}

func (a *testApp) do(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	a.r.ServeHTTP(w, req)
	return w
}

func (a *testApp) postJSON(path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return a.do(req)
}

func (a *testApp) get(path string) *httptest.ResponseRecorder {
	return a.do(httptest.NewRequest(http.MethodGet, path, nil))
}

// register регистрирует заявителя и возвращает id черновика заявки.
func (a *testApp) register(t *testing.T, body string) string {
	t.Helper()
	w := a.postJSON("/api/v1/applicants", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: status %d, body %s", w.Code, w.Body)
	}
	var app struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &app); err != nil {
		t.Fatal(err)
	}
	return app.ID
}

// readExample читает пример запроса из examples/.
func readExample(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile("../../../examples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// file — часть files в multipart.
type file struct {
	name, body string
}

// submitMultipart отправляет анкету по заявке: JSON в части questionnaire и файлы в частях files.
func (a *testApp) submitMultipart(t *testing.T, appID, questionnaire string, files ...file) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if questionnaire != "" {
		if err := mw.WriteField("questionnaire", questionnaire); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		part, err := mw.CreateFormFile("files", f.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(part, f.body)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+appID+"/questionnaire", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.do(req)
}

// runWorker выполняет задачи из outbox так, как это сделал бы worker по событиям из Kafka.
func runWorker(t *testing.T, systems worker.Systems, tasks *memory.CollectionTaskRepository) {
	t.Helper()
	w := worker.New(systems, tasks, worker.Config{CollectTimeout: 15 * time.Second, MaxAttempts: 3}, discardLog,
		worker.WithIDGenerator(func() string { return "col_test" }))
	for _, m := range tasks.Outbox() {
		if m.Topic != events.DefaultTopic {
			continue // события о документах — для worker'ов подписи и OCR
		}
		if err := w.Handle(context.Background(), m.Payload); err != nil {
			t.Fatalf("worker: %v", err)
		}
	}
}

// problemBody — ответ об ошибке.
type problemBody struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
	Errors []struct {
		Field string `json:"field"`
		Code  string `json:"code"`
	} `json:"errors"`
}

func decodeProblem(t *testing.T, w *httptest.ResponseRecorder) problemBody {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, body %s", ct, w.Body)
	}
	var p problemBody
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

func (p problemBody) fields() []string {
	var out []string
	for _, e := range p.Errors {
		out = append(out, e.Field)
	}
	return out
}
