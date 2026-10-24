package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// CollectionTaskRepository хранит задачи на сбор, события outbox и результаты сборов
// в памяти процесса. Семантика как у Postgres: задача и событие появляются вместе,
// Claim берёт только PENDING и RUNNING, результат записывается один раз, после
// завершения токенов нет. Используется в тестах.
type CollectionTaskRepository struct {
	mu      sync.Mutex
	tasks   map[string]domain.CollectionTask
	results map[string]domain.RawCollection // по id задачи
	outbox  []events.Message
}

func NewCollectionTaskRepository() *CollectionTaskRepository {
	return &CollectionTaskRepository{
		tasks:   make(map[string]domain.CollectionTask),
		results: make(map[string]domain.RawCollection),
	}
}

// Create сохраняет задачу в статусе PENDING и событие о ней в outbox.
func (r *CollectionTaskRepository) Create(_ context.Context, task domain.CollectionTask) error {
	payload, err := json.Marshal(events.NewCollectionRequested(task))
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[task.ID]; ok {
		return fmt.Errorf("collection task %s already exists", task.ID)
	}
	task.Status = domain.TaskPending
	r.tasks[task.ID] = task
	r.outbox = append(r.outbox, events.Message{ID: int64(len(r.outbox) + 1), Topic: events.DefaultTopic, Key: task.ID,
		Payload: payload})
	return nil
}

// Get возвращает задачу; у выполненной — с сохранённым сбором.
func (r *CollectionTaskRepository) Get(_ context.Context, id string) (domain.CollectionTask, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok {
		return domain.CollectionTask{}, false, nil
	}
	if rc, done := r.results[id]; done {
		task.Result = &rc
	}
	return task, true, nil
}

// Claim переводит задачу в RUNNING и увеличивает attempts; у задачи, которую брали уже
// maxAttempts раз, — в FAILED. false — задачи нет или она уже завершена.
func (r *CollectionTaskRepository) Claim(_ context.Context, id string, maxAttempts int) (domain.CollectionTask, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || (task.Status != domain.TaskPending && task.Status != domain.TaskRunning) {
		return domain.CollectionTask{}, false, nil
	}
	now := time.Now().UTC()
	if task.Attempts >= maxAttempts {
		task.Status, task.Error, task.FinishedAt = domain.TaskFailed, domain.ErrAttemptsExhausted.Error(), now
		task.Input = withoutTokens(task.Input)
	} else {
		task.Status, task.Error, task.StartedAt, task.FinishedAt = domain.TaskRunning, "", now, time.Time{}
		task.Attempts++
	}
	r.tasks[id] = task
	return task, true, nil
}

// Complete сохраняет сбор и переводит задачу в COMPLETED.
// domain.ErrTaskNotRunning — задача не RUNNING или результат уже есть.
func (r *CollectionTaskRepository) Complete(_ context.Context, rc domain.RawCollection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[rc.TaskID]
	if _, done := r.results[rc.TaskID]; !ok || done || task.Status != domain.TaskRunning {
		return domain.ErrTaskNotRunning
	}
	rc.Input = withoutTokens(rc.Input)
	r.results[rc.TaskID] = rc
	task.Status, task.Error, task.FinishedAt = domain.TaskCompleted, "", rc.FinishedAt
	task.Input = withoutTokens(task.Input)
	r.tasks[rc.TaskID] = task
	return nil
}

// Fail переводит выполняющуюся задачу в FAILED с причиной.
func (r *CollectionTaskRepository) Fail(_ context.Context, id, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[id]
	if !ok || task.Status != domain.TaskRunning {
		return nil
	}
	task.Status, task.Error, task.FinishedAt = domain.TaskFailed, reason, time.Now().UTC()
	task.Input = withoutTokens(task.Input)
	r.tasks[id] = task
	return nil
}

// Outbox — события, записанные вместе с задачами, по порядку.
func (r *CollectionTaskRepository) Outbox() []events.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Message(nil), r.outbox...)
}

// Result — сохранённый сбор по задаче, с сырыми данными систем.
func (r *CollectionTaskRepository) Result(taskID string) (domain.RawCollection, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rc, ok := r.results[taskID]
	return rc, ok
}

// withoutTokens — входные данные без токенов: такими они остаются после завершения задачи.
func withoutTokens(in domain.CollectInput) domain.CollectInput {
	in.MarketplaceTokens = nil
	in.OFDToken = ""
	return in
}
