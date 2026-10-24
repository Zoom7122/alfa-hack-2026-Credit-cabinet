package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/worker"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeSystems отвечает DONE по каждой системе, кроме пропущенных во входных данных.
// started закрывается при первом вызове; если release задан, ответ ждёт его закрытия.
type fakeSystems struct {
	started chan struct{}
	release chan struct{}
	panics  bool

	mu           sync.Mutex
	calls        int
	gotRequestID string
	gotInput     domain.CollectInput
	gotDeadline  bool
	gotCanceled  bool
}

func (f *fakeSystems) CollectAll(ctx context.Context, requestID string, in domain.CollectInput) []domain.SystemResult {
	f.mu.Lock()
	f.calls++
	f.gotRequestID, f.gotInput = requestID, in
	_, f.gotDeadline = ctx.Deadline()
	f.gotCanceled = ctx.Err() != nil
	if f.started != nil && f.calls == 1 {
		close(f.started)
	}
	f.mu.Unlock()
	if f.release != nil {
		<-f.release
	}
	if f.panics {
		panic("boom")
	}
	skipped := domain.SkippedSystems(in)
	out := make([]domain.SystemResult, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		if reason, ok := skipped[sys]; ok {
			out = append(out, domain.SystemResult{System: sys, State: domain.CollectSkipped, Reason: reason})
			continue
		}
		out = append(out, domain.SystemResult{System: sys, State: domain.CollectDone, Data: []byte(`{}`),
			Calls: []domain.Call{{Key: "k", Status: 200}}})
	}
	return out
}

func (f *fakeSystems) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// failingComplete — хранилище, у которого не получается записать результат.
type failingComplete struct {
	*memory.CollectionTaskRepository
	err error
}

func (r failingComplete) Complete(context.Context, domain.RawCollection) error { return r.err }

func input() domain.CollectInput {
	return domain.CollectInput{
		INN:               "7799000012",
		Director:          &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"},
		MarketplaceTokens: map[string]string{"wildberries": "secret-wb"},
		OFDToken:          "secret-ofd",
	}
}

// newTask создаёт задачу и возвращает сообщение о ней — то, что publisher отправит в Kafka.
func newTask(t *testing.T, tasks *memory.CollectionTaskRepository, id string) []byte {
	t.Helper()
	task := domain.CollectionTask{ID: id, QuestionnaireID: "qst_test", Input: input(),
		Skipped:   []domain.System{domain.SystemBKI, domain.SystemTaxReports, domain.SystemEGRN, domain.SystemOpenBanking},
		CreatedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	for _, m := range tasks.Outbox() {
		if m.Key == id {
			return m.Payload
		}
	}
	t.Fatalf("no outbox event for %s", id)
	return nil
}

func newWorker(systems worker.Systems, tasks worker.Tasks, maxAttempts int) *worker.Worker {
	clock := []time.Time{time.Date(2026, 10, 22, 12, 0, 1, 0, time.UTC), time.Date(2026, 10, 22, 12, 0, 3, 0, time.UTC)}
	return worker.New(systems, tasks, worker.Config{CollectTimeout: time.Second, MaxAttempts: maxAttempts}, discardLog,
		worker.WithClock(func() time.Time { t := clock[0]; clock = clock[1:]; return t }),
		worker.WithIDGenerator(func() string { return "col_test" }),
	)
}

func getTask(t *testing.T, tasks *memory.CollectionTaskRepository, id string) domain.CollectionTask {
	t.Helper()
	task, ok, err := tasks.Get(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("task %s: found %v, err %v", id, ok, err)
	}
	return task
}

// Пока системы опрашиваются, задача RUNNING; после — COMPLETED, сбор сохранён, токенов нет.
func TestHandle(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	systems := &fakeSystems{started: make(chan struct{}), release: make(chan struct{})}
	w := newWorker(systems, tasks, 3)

	done := make(chan error, 1)
	go func() { done <- w.Handle(context.Background(), msg) }()

	<-systems.started
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskRunning || task.Attempts != 1 || task.StartedAt.IsZero() {
		t.Fatalf("while collecting: %+v", task)
	}
	close(systems.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	task := getTask(t, tasks, "tsk_test")
	if task.Status != domain.TaskCompleted || task.Error != "" || task.Input.MarketplaceTokens != nil || task.Input.OFDToken != "" {
		t.Fatalf("task = %+v", task)
	}
	// системам ушли данные задачи с токенами и id сбора (для X-Request-Id), сбор ограничен по времени
	if systems.gotRequestID != "col_test" || systems.gotInput.MarketplaceTokens["wildberries"] != "secret-wb" ||
		systems.gotInput.OFDToken != "secret-ofd" || !systems.gotDeadline {
		t.Fatalf("systems got request %q, input %+v, deadline %v", systems.gotRequestID, systems.gotInput, systems.gotDeadline)
	}
	rc, ok := tasks.Result("tsk_test")
	if !ok || rc.ID != "col_test" || rc.TaskID != "tsk_test" || rc.QuestionnaireID != "qst_test" ||
		len(rc.Systems) != len(domain.Systems) || !rc.FinishedAt.After(rc.StartedAt) || !task.FinishedAt.Equal(rc.FinishedAt) {
		t.Fatalf("result = %+v (found %v)", rc, ok)
	}
	if strings.Contains(string(mustJSON(t, rc.Input)), "secret") {
		t.Fatalf("tokens are kept in the result: %+v", rc.Input)
	}
}

// Повторная доставка сообщения о выполненной задаче не запускает сбор снова.
func TestHandleDuplicate(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	systems := &fakeSystems{}
	w := newWorker(systems, tasks, 3)

	for range 2 {
		if err := w.Handle(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
	}
	if systems.callCount() != 1 {
		t.Fatalf("systems called %d times", systems.callCount())
	}
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskCompleted || task.Attempts != 1 {
		t.Fatalf("task = %+v", task)
	}
}

// Прошлый worker взял задачу и упал до коммита оффсета: сообщение пришло снова — задача выполняется.
func TestHandleRetriesAbandonedTask(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	if _, _, err := tasks.Claim(context.Background(), "tsk_test", 3); err != nil {
		t.Fatal(err)
	}

	if err := newWorker(&fakeSystems{}, tasks, 3).Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskCompleted || task.Attempts != 2 {
		t.Fatalf("task = %+v", task)
	}
}

// Задачу брали уже MaxAttempts раз (worker падал посреди сбора) — FAILED без нового сбора.
func TestHandleAttemptsExhausted(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	for range 2 {
		if _, _, err := tasks.Claim(context.Background(), "tsk_test", 2); err != nil {
			t.Fatal(err)
		}
	}
	systems := &fakeSystems{}

	if err := newWorker(systems, tasks, 2).Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	task := getTask(t, tasks, "tsk_test")
	if task.Status != domain.TaskFailed || task.Error != domain.ErrAttemptsExhausted.Error() || task.Input.OFDToken != "" ||
		systems.callCount() != 0 {
		t.Fatalf("task = %+v, systems called %d times", task, systems.callCount())
	}
}

// Битое или чужое сообщение и сообщение о несуществующей задаче пропускаются без ошибки.
func TestHandleSkipsUnexpectedMessages(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	systems := &fakeSystems{}
	w := newWorker(systems, tasks, 3)

	for _, msg := range []string{
		`{"event":`,
		`{"event":"questionnaire.deleted","task_id":"tsk_test"}`,
		`{"event":"collection.requested"}`,
		`{"event":"collection.requested","task_id":"tsk_unknown"}`,
	} {
		if err := w.Handle(context.Background(), []byte(msg)); err != nil {
			t.Fatalf("%s: %v", msg, err)
		}
	}
	if systems.callCount() != 0 {
		t.Fatalf("systems called %d times", systems.callCount())
	}
}

// Результат не записался — ошибка наружу: сообщение не подтвердится и придёт снова.
func TestHandleCompleteError(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	boom := errors.New("db down")

	err := newWorker(&fakeSystems{}, failingComplete{tasks, boom}, 3).Handle(context.Background(), msg)
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped complete error, got %v", err)
	}
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskRunning {
		t.Fatalf("task = %+v", task)
	}
}

// Результат уже записан другим worker'ом — второй результат отбрасывается без ошибки.
func TestHandleAlreadyCompleted(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")

	err := newWorker(&fakeSystems{}, failingComplete{tasks, domain.ErrTaskNotRunning}, 3).Handle(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
}

// Сбор упал — задача FAILED с причиной, повтор не нужен.
func TestHandlePanic(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")

	if err := newWorker(&fakeSystems{panics: true}, tasks, 3).Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskFailed || !strings.Contains(task.Error, "panic: boom") {
		t.Fatalf("task = %+v", task)
	}
}

// Остановка worker'а не обрывает начатый сбор: он доводится до конца и сохраняется.
func TestHandleIgnoresCancel(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")
	systems := &fakeSystems{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := newWorker(systems, tasks, 3).Handle(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if systems.gotCanceled {
		t.Fatal("collection context must not inherit the worker cancellation")
	}
	if task := getTask(t, tasks, "tsk_test"); task.Status != domain.TaskCompleted {
		t.Fatalf("task = %+v", task)
	}
}

// Сообщение о задаче — событие collection.requested без входных данных и токенов.
func TestEventHasNoInput(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	msg := newTask(t, tasks, "tsk_test")

	var ev events.CollectionRequestedEvent
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Event != events.CollectionRequested || ev.TaskID != "tsk_test" || ev.QuestionnaireID != "qst_test" ||
		ev.INN != "7799000012" || strings.Contains(string(msg), "secret") || strings.Contains(string(msg), "Ковалёв") {
		t.Fatalf("event = %s", msg)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
