// Package worker выполняет задачи на сбор данных: по событию из Kafka берёт задачу
// из collection_tasks, опрашивает все внешние системы и сохраняет результат в raw_collections.
package worker

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// saveTimeout — на запись результата, даже если worker останавливается.
const saveTimeout = 5 * time.Second

// Systems — опрос всех внешних систем (mockapi.Client). Объявлен у потребителя.
type Systems interface {
	CollectAll(ctx context.Context, requestID string, in domain.CollectInput) []domain.SystemResult
	// CompanyCard — реквизиты компании из ответа ЕГРЮЛ в данных ФНС; false — их нет.
	CompanyCard(fnsData json.RawMessage) (domain.CompanyCard, bool)
}

// Tasks — хранилище задач на сбор.
type Tasks interface {
	// Claim переводит задачу в RUNNING и увеличивает attempts; задачу, которую брали уже
	// maxAttempts раз, — в FAILED. false — задачи нет или она уже завершена.
	Claim(ctx context.Context, id string, maxAttempts int) (domain.CollectionTask, bool, error)
	// Complete сохраняет сбор и переводит задачу в COMPLETED одной транзакцией, заявку — в VERIFYING;
	// card дописывает реквизиты компании (nil — их нет). domain.ErrTaskNotRunning — результат
	// уже записан или задача снята.
	Complete(ctx context.Context, rc domain.RawCollection, card *domain.CompanyCard) error
	// Fail переводит выполняющуюся задачу в FAILED с причиной.
	Fail(ctx context.Context, id, reason string) error
}

// Config — настройки выполнения задач.
type Config struct {
	CollectTimeout time.Duration // на весь сбор по задаче, включая медленные системы
	MaxAttempts    int           // сколько раз брать задачу, прежде чем перевести её в FAILED
}

// Worker выполняет задачи на сбор. Безопасен для параллельного использования.
type Worker struct {
	systems Systems
	tasks   Tasks
	cfg     Config
	log     *slog.Logger
	now     func() time.Time
	newID   func() string
}

// Option настраивает Worker (используется в тестах).
type Option func(*Worker)

// WithClock подменяет источник времени.
func WithClock(now func() time.Time) Option { return func(w *Worker) { w.now = now } }

// WithIDGenerator подменяет генератор идентификаторов сбора.
func WithIDGenerator(newID func() string) Option { return func(w *Worker) { w.newID = newID } }

func New(systems Systems, tasks Tasks, cfg Config, log *slog.Logger, opts ...Option) *Worker {
	w := &Worker{
		systems: systems,
		tasks:   tasks,
		cfg:     cfg,
		log:     log,
		now:     time.Now,
		newID:   func() string { return "col_" + ulid.MustNew(ulid.Now(), rand.Reader).String() },
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Handle обрабатывает одно сообщение из топика задач: PENDING → RUNNING → сбор → COMPLETED
// (заявка: SUBMITTED → ENRICHING → VERIFYING).
// Ошибка — только сбой хранилища: сообщение не подтверждается и придёт снова.
// Битые сообщения, повторы и задачи без попыток пропускаются без ошибки.
func (w *Worker) Handle(ctx context.Context, value []byte) error {
	var ev events.CollectionRequestedEvent
	if err := json.Unmarshal(value, &ev); err != nil || ev.Event != events.CollectionRequested || ev.TaskID == "" {
		w.log.WarnContext(ctx, "skip unexpected message", "err", err, "event", ev.Event, "value", preview(value))
		return nil
	}
	log := w.log.With("task", ev.TaskID, "questionnaire", ev.QuestionnaireID, "inn", ev.INN)

	task, ok, err := w.tasks.Claim(ctx, ev.TaskID, w.cfg.MaxAttempts)
	if err != nil {
		return fmt.Errorf("claim task %s: %w", ev.TaskID, err)
	}
	if !ok {
		log.InfoContext(ctx, "task is not pending, skip")
		return nil
	}
	if task.Status == domain.TaskFailed {
		log.ErrorContext(ctx, "task failed", "err", task.Error, "attempts", task.Attempts)
		return nil
	}
	log.InfoContext(ctx, "collection started", "attempt", task.Attempts, "skipped", task.Skipped)

	// Начатый сбор доводим до конца и сохраняем, даже если worker останавливается.
	base := context.WithoutCancel(ctx)
	rc, err := w.collect(base, task)
	if err != nil {
		// сбор упал сам по себе — повтор не поможет
		log.ErrorContext(ctx, "collection failed", "err", err)
		failCtx, cancel := context.WithTimeout(base, saveTimeout)
		defer cancel()
		if err := w.tasks.Fail(failCtx, task.ID, err.Error()); err != nil {
			return fmt.Errorf("fail task %s: %w", task.ID, err)
		}
		return nil
	}

	saveCtx, cancel := context.WithTimeout(base, saveTimeout)
	defer cancel()
	if err := w.tasks.Complete(saveCtx, rc, w.companyCard(rc)); err != nil {
		if errors.Is(err, domain.ErrTaskNotRunning) {
			log.InfoContext(ctx, "task is already finished, result is dropped", "collection", rc.ID)
			return nil
		}
		return fmt.Errorf("complete task %s: %w", task.ID, err)
	}

	for _, s := range rc.Systems {
		if s.State == domain.CollectFailed || s.State == domain.CollectPartial {
			log.WarnContext(ctx, "external system failed", "collection", rc.ID, "source", s.System, "state", s.State,
				"reason", s.Reason)
		}
	}
	log.InfoContext(ctx, "collection completed", "collection", rc.ID, "duration", rc.FinishedAt.Sub(rc.StartedAt))
	return nil
}

// collect опрашивает все системы по задаче. Паника сбора возвращается ошибкой.
func (w *Worker) collect(ctx context.Context, task domain.CollectionTask) (rc domain.RawCollection, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	collectCtx, cancel := context.WithTimeout(ctx, w.cfg.CollectTimeout)
	defer cancel()

	rc = domain.RawCollection{ID: w.newID(), TaskID: task.ID, QuestionnaireID: task.QuestionnaireID, Input: task.Input,
		StartedAt: w.now().UTC()}
	rc.Systems = w.systems.CollectAll(collectCtx, rc.ID, task.Input)
	rc.FinishedAt = w.now().UTC()
	return rc, nil
}

// companyCard — реквизиты компании из собранных данных ФНС; nil — ЕГРЮЛ не ответил.
func (w *Worker) companyCard(rc domain.RawCollection) *domain.CompanyCard {
	for _, s := range rc.Systems {
		if s.System == domain.SystemFNS {
			if card, ok := w.systems.CompanyCard(s.Data); ok {
				return &card
			}
		}
	}
	return nil
}

// preview — начало сообщения для лога.
func preview(value []byte) string {
	const limit = 256
	if len(value) > limit {
		return string(value[:limit]) + "…"
	}
	return string(value)
}
