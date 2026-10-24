//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

func itTask(id string) domain.CollectionTask {
	return domain.CollectionTask{
		ID: id, QuestionnaireID: "qst_it", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Input: domain.CollectInput{
			INN:               "7799000012",
			Director:          &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"},
			MarketplaceTokens: map[string]string{"wildberries": "secret-wb"},
			OFDToken:          "secret-ofd",
		},
		Skipped: []domain.System{domain.SystemBKI, domain.SystemEGRN},
	}
}

// PENDING (+ событие в outbox) → RUNNING → COMPLETED; повторы ничего не меняют.
func TestIntegrationTaskLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	taskID, topic := "tsk_"+suffix, "it.collection-tasks."+suffix
	repo := NewCollectionTaskRepository(pool)
	cleanupTasks(t, pool, topic, taskID)

	createTask(t, pool, topic, itTask(taskID))

	// задача: токены — только в secrets, пропущенные системы — списком
	var (
		status, request, secrets string
		skipped                  []string
	)
	if err := pool.QueryRow(ctx, `SELECT status, request::text, secrets::text, skipped_sources FROM collection_tasks WHERE id = $1`,
		taskID).Scan(&status, &request, &secrets, &skipped); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" || strings.Contains(request, "secret") || !strings.Contains(secrets, "secret-ofd") ||
		!slices.Equal(skipped, []string{"BKI", "EGRN"}) {
		t.Fatalf("task row: status=%s request=%s secrets=%s skipped=%v", status, request, secrets, skipped)
	}
	// событие: в топик задачи, ключ — id задачи, без входных данных
	var key, payload string
	if err := pool.QueryRow(ctx, `SELECT key, payload::text FROM outbox WHERE topic = $1 AND published_at IS NULL`, topic).
		Scan(&key, &payload); err != nil {
		t.Fatal(err)
	}
	var ev events.CollectionRequestedEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		t.Fatal(err)
	}
	if key != taskID || ev.Event != events.CollectionRequested || ev.TaskID != taskID || strings.Contains(payload, "secret") {
		t.Fatalf("outbox: key=%s payload=%s", key, payload)
	}

	task, ok, err := repo.Claim(ctx, taskID, 3)
	if err != nil || !ok || task.Status != domain.TaskRunning || task.Attempts != 1 || task.StartedAt.IsZero() ||
		task.Input.MarketplaceTokens["wildberries"] != "secret-wb" || task.Input.Director.LastName != "Ковалёв" ||
		!slices.Equal(task.Skipped, []domain.System{domain.SystemBKI, domain.SystemEGRN}) {
		t.Fatalf("claim: %+v (ok %v, err %v)", task, ok, err)
	}

	rc := domain.RawCollection{ID: "col_" + suffix, TaskID: taskID, QuestionnaireID: "qst_it", Input: task.Input,
		Systems: []domain.SystemResult{{System: domain.SystemFNS, State: domain.CollectPartial, Reason: "charter: 404",
			Calls: []domain.Call{{Key: "egrul", Status: 200}, {Key: "charter", Status: 404, Error: "status 404"}},
			Data:  json.RawMessage(`{"egrul":{}}`)}},
		StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC().Add(time.Second)}
	if err := repo.Complete(ctx, rc, nil); err != nil {
		t.Fatal(err)
	}

	got, ok, err := repo.Get(ctx, taskID)
	if err != nil || !ok || got.Status != domain.TaskCompleted || got.FinishedAt.IsZero() || got.Input.OFDToken != "" ||
		got.Result == nil || got.Result.ID != rc.ID || len(got.Result.Systems) != 1 || got.Result.Systems[0].FailedCalls() != 1 {
		t.Fatalf("get: %+v (ok %v, err %v)", got, ok, err)
	}
	var secretsNull bool
	if err := pool.QueryRow(ctx, `SELECT secrets IS NULL FROM collection_tasks WHERE id = $1`, taskID).Scan(&secretsNull); err != nil || !secretsNull {
		t.Fatalf("secrets must be wiped: %v %v", secretsNull, err)
	}

	// повторная доставка: результат второй раз не пишется, задачу снова не взять
	rc.ID = "col_" + suffix + "_2"
	if err := repo.Complete(ctx, rc, nil); !errors.Is(err, domain.ErrTaskNotRunning) {
		t.Fatalf("second complete: %v", err)
	}
	if _, ok, err := repo.Claim(ctx, taskID, 3); err != nil || ok {
		t.Fatalf("claim completed task: ok %v, err %v", ok, err)
	}
	if _, ok, err := repo.Get(ctx, "tsk_"+suffix+"_unknown"); err != nil || ok {
		t.Fatalf("unknown task: ok %v, err %v", ok, err)
	}
}

// Задачу, которую брали maxAttempts раз, Claim переводит в FAILED; Fail — сбой сбора.
func TestIntegrationTaskFailures(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	exhausted, failed, topic := "tsk_"+suffix+"_a", "tsk_"+suffix+"_b", "it.collection-tasks."+suffix
	repo := NewCollectionTaskRepository(pool)
	cleanupTasks(t, pool, topic, exhausted, failed)

	for _, id := range []string{exhausted, failed} {
		createTask(t, pool, topic, itTask(id))
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if task, ok, err := repo.Claim(ctx, exhausted, 2); err != nil || !ok || task.Status != domain.TaskRunning || task.Attempts != attempt {
			t.Fatalf("claim %d: %+v (ok %v, err %v)", attempt, task, ok, err)
		}
	}
	task, ok, err := repo.Claim(ctx, exhausted, 2)
	if err != nil || !ok || task.Status != domain.TaskFailed || task.Error != domain.ErrAttemptsExhausted.Error() ||
		task.Attempts != 2 || task.FinishedAt.IsZero() || task.Input.OFDToken != "" {
		t.Fatalf("exhausted: %+v (ok %v, err %v)", task, ok, err)
	}
	if _, ok, err := repo.Claim(ctx, exhausted, 2); err != nil || ok {
		t.Fatalf("claim failed task: ok %v, err %v", ok, err)
	}

	if _, _, err := repo.Claim(ctx, failed, 3); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fail(ctx, failed, "panic: boom"); err != nil {
		t.Fatal(err)
	}
	got, _, err := repo.Get(ctx, failed)
	if err != nil || got.Status != domain.TaskFailed || got.Error != "panic: boom" || got.Input.MarketplaceTokens != nil || got.Result != nil {
		t.Fatalf("failed: %+v (err %v)", got, err)
	}
}

// Параллельные publisher'ы не берут одни и те же события; неудачная публикация оставляет их в outbox.
func TestIntegrationOutboxPublishBatch(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	suffix := testSuffix()
	topic := "it.collection-tasks." + suffix
	outbox := NewOutboxRepository(pool)
	ids := []string{"tsk_" + suffix + "_1", "tsk_" + suffix + "_2", "tsk_" + suffix + "_3", "tsk_" + suffix + "_4"}
	cleanupTasks(t, pool, topic, ids...)
	for _, id := range ids[:3] {
		createTask(t, pool, topic, itTask(id))
	}
	keys := func(msgs []events.Message) []string {
		var out []string
		for _, m := range msgs {
			out = append(out, m.Key)
		}
		return out
	}

	// A держит первые два события, пока публикует; B в это время получает только третье
	locked, release := make(chan struct{}), make(chan struct{})
	var gotA []events.Message
	doneA := make(chan error, 1)
	go func() {
		n, err := outbox.PublishBatch(ctx, []string{topic}, 2, func(_ context.Context, msgs []events.Message) error {
			gotA = msgs
			close(locked)
			<-release
			return nil
		})
		if err == nil && n != 2 {
			err = errors.New("A published wrong number of events")
		}
		doneA <- err
	}()
	<-locked
	var gotB []events.Message
	n, err := outbox.PublishBatch(ctx, []string{topic}, 10, func(_ context.Context, msgs []events.Message) error {
		gotB = msgs
		return nil
	})
	close(release)
	if err := <-doneA; err != nil {
		t.Fatal(err)
	}
	if err != nil || n != 1 || !slices.Equal(keys(gotB), ids[2:3]) || !slices.Equal(keys(gotA), ids[:2]) {
		t.Fatalf("A got %v, B got %v (n %d, err %v)", keys(gotA), keys(gotB), n, err)
	}
	if string(gotA[0].Payload) == "" || gotA[0].Topic != topic {
		t.Fatalf("message = %+v", gotA[0])
	}

	// брокер недоступен: событие остаётся неопубликованным, попытка учтена
	createTask(t, pool, topic, itTask(ids[3]))
	boom := errors.New("kafka is down")
	if _, err := outbox.PublishBatch(ctx, []string{topic}, 10, func(context.Context, []events.Message) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("want producer error, got %v", err)
	}
	var (
		attempts  int
		lastError string
	)
	if err := pool.QueryRow(ctx, `SELECT attempts, last_error FROM outbox WHERE key = $1 AND published_at IS NULL`, ids[3]).
		Scan(&attempts, &lastError); err != nil || attempts != 1 || lastError != boom.Error() {
		t.Fatalf("attempts=%d last_error=%q err=%v", attempts, lastError, err)
	}
	if n, err := outbox.PublishBatch(ctx, []string{topic}, 10, func(context.Context, []events.Message) error { return nil }); err != nil || n != 1 {
		t.Fatalf("retry: n %d, err %v", n, err)
	}
	var unpublished int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE topic = $1 AND published_at IS NULL`, topic).Scan(&unpublished); err != nil || unpublished != 0 {
		t.Fatalf("unpublished = %d, err %v", unpublished, err)
	}
}
