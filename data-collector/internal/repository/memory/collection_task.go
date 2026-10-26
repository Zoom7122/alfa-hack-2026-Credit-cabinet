// Package memory — хранилища в памяти процесса с семантикой Postgres-репозиториев, для тестов.
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

// store — данные хранилищ в памяти под одной блокировкой, как одна БД: задачи, их результаты,
// outbox, заявки с компаниями и документами. Используется в тестах.
type store struct {
	mu sync.Mutex

	tasks   map[string]domain.CollectionTask
	results map[string]domain.RawCollection // по id задачи
	outbox  []events.Message

	users        map[string]domain.Representative // по id; Role и CanSign — в members
	userByINN    map[string]string
	companies    map[string]domain.Company // по id
	companyByINN map[string]string
	cards        map[string]domain.CompanyCard // реквизиты из ЕГРЮЛ по ИНН
	members      map[[2]string]domain.Representative
	applications map[string]*application
	events       map[string][]string // типы событий по заявке
	appSeq       int

	roles     map[string]domain.UserRole // роль пользователя в системе; нет — CLIENT
	personas  map[string]string          // persona тестовых пользователей
	testUsers []string                   // id тестовых пользователей в порядке заведения
}

// application — заявка в памяти.
type application struct {
	app        domain.Application // Company и Representative собираются при чтении
	companyID  string
	userID     string
	submission *domain.Submission
}

func newStore() *store {
	return &store{
		tasks:        make(map[string]domain.CollectionTask),
		results:      make(map[string]domain.RawCollection),
		users:        make(map[string]domain.Representative),
		userByINN:    make(map[string]string),
		companies:    make(map[string]domain.Company),
		companyByINN: make(map[string]string),
		cards:        make(map[string]domain.CompanyCard),
		members:      make(map[[2]string]domain.Representative),
		applications: make(map[string]*application),
		events:       make(map[string][]string),
		roles:        make(map[string]domain.UserRole),
		personas:     make(map[string]string),
	}
}

// CollectionTaskRepository хранит задачи на сбор, события outbox и результаты сборов в памяти.
// Семантика как у Postgres: задача и событие появляются вместе, Claim берёт только PENDING
// и RUNNING, результат записывается один раз, после завершения токенов нет, заявка задачи
// идёт SUBMITTED → ENRICHING → VERIFYING. Используется в тестах.
type CollectionTaskRepository struct {
	s *store
}

func NewCollectionTaskRepository() *CollectionTaskRepository {
	return &CollectionTaskRepository{s: newStore()}
}

// Create сохраняет задачу в статусе PENDING и событие о ней в outbox (топик по умолчанию).
func (r *CollectionTaskRepository) Create(_ context.Context, task domain.CollectionTask) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return r.s.createTask(task, events.DefaultTopic)
}

// createTask — Create под уже взятой блокировкой.
func (s *store) createTask(task domain.CollectionTask, topic string) error {
	if _, ok := s.tasks[task.ID]; ok {
		return fmt.Errorf("collection task %s already exists", task.ID)
	}
	task.Status = domain.TaskPending
	s.tasks[task.ID] = task
	return s.addOutbox(topic, task.ID, events.NewCollectionRequested(task))
}

func (s *store) addOutbox(topic, key string, event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	s.outbox = append(s.outbox, events.Message{ID: int64(len(s.outbox) + 1), Topic: topic, Key: key, Payload: payload})
	return nil
}

// Get возвращает задачу; у выполненной — с сохранённым сбором.
func (r *CollectionTaskRepository) Get(_ context.Context, id string) (domain.CollectionTask, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	task, ok := r.s.tasks[id]
	if !ok {
		return domain.CollectionTask{}, false, nil
	}
	if rc, done := r.s.results[id]; done {
		task.Result = &rc
	}
	return task, true, nil
}

// Claim переводит задачу в RUNNING и увеличивает attempts; у задачи, которую брали уже
// maxAttempts раз, — в FAILED. false — задачи нет или она уже завершена.
func (r *CollectionTaskRepository) Claim(_ context.Context, id string, maxAttempts int) (domain.CollectionTask, bool, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	task, ok := r.s.tasks[id]
	if !ok || (task.Status != domain.TaskPending && task.Status != domain.TaskRunning) {
		return domain.CollectionTask{}, false, nil
	}
	now := time.Now().UTC()
	if task.Attempts >= maxAttempts {
		task.Status, task.Error, task.FinishedAt = domain.TaskFailed, domain.ErrAttemptsExhausted.Error(), now
		task.Input = withoutTokens(task.Input)
		r.s.addEvent(task.ApplicationID, "enrich.failed")
	} else {
		task.Status, task.Error, task.StartedAt, task.FinishedAt = domain.TaskRunning, "", now, time.Time{}
		task.Attempts++
		r.s.moveApplication(task.ApplicationID, domain.ApplicationSubmitted, domain.ApplicationEnriching, "enrich.started")
	}
	r.s.tasks[id] = task
	return task, true, nil
}

// Complete сохраняет сбор и переводит задачу в COMPLETED, заявку — в VERIFYING; card дописывает
// реквизиты компании. domain.ErrTaskNotRunning — задача не RUNNING или результат уже есть.
func (r *CollectionTaskRepository) Complete(_ context.Context, rc domain.RawCollection, card *domain.CompanyCard) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	task, ok := r.s.tasks[rc.TaskID]
	if _, done := r.s.results[rc.TaskID]; !ok || done || task.Status != domain.TaskRunning {
		return domain.ErrTaskNotRunning
	}
	rc.Input = withoutTokens(rc.Input)
	r.s.results[rc.TaskID] = rc
	task.Status, task.Error, task.FinishedAt = domain.TaskCompleted, "", rc.FinishedAt
	task.Input = withoutTokens(task.Input)
	r.s.tasks[rc.TaskID] = task
	if card != nil {
		r.s.cards[card.INN] = *card
		if id, ok := r.s.companyByINN[rc.Input.INN]; ok {
			c := r.s.companies[id]
			c.Name, c.OGRN = card.Name(), card.OGRN
			r.s.companies[id] = c
		}
	}
	r.s.moveApplication(task.ApplicationID, domain.ApplicationEnriching, domain.ApplicationVerifying, "enrich.completed")
	return nil
}

// Fail переводит выполняющуюся задачу в FAILED с причиной.
func (r *CollectionTaskRepository) Fail(_ context.Context, id, reason string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	task, ok := r.s.tasks[id]
	if !ok || task.Status != domain.TaskRunning {
		return nil
	}
	task.Status, task.Error, task.FinishedAt = domain.TaskFailed, reason, time.Now().UTC()
	task.Input = withoutTokens(task.Input)
	r.s.tasks[id] = task
	r.s.addEvent(task.ApplicationID, "enrich.failed")
	return nil
}

// Outbox — события, записанные вместе с задачами и заявками, по порядку.
func (r *CollectionTaskRepository) Outbox() []events.Message {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return append([]events.Message(nil), r.s.outbox...)
}

// Result — сохранённый сбор по задаче, с сырыми данными систем.
func (r *CollectionTaskRepository) Result(taskID string) (domain.RawCollection, bool) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rc, ok := r.s.results[taskID]
	return rc, ok
}

// CompanyCard — реквизиты, дописанные после сбора по компании с этим ИНН.
func (r *CollectionTaskRepository) CompanyCard(inn string) (domain.CompanyCard, bool) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	card, ok := r.s.cards[inn]
	return card, ok
}

// moveApplication переводит заявку из from в to и пишет событие; заявка не в from — ничего.
func (s *store) moveApplication(appID string, from, to domain.ApplicationStatus, eventType string) {
	a, ok := s.applications[appID]
	if !ok || a.app.Status != from {
		return
	}
	a.app.Status, a.app.UpdatedAt = to, time.Now().UTC()
	s.addEvent(appID, eventType)
}

func (s *store) addEvent(appID, eventType string) {
	if appID != "" {
		s.events[appID] = append(s.events[appID], eventType)
	}
}

// withoutTokens — входные данные без токенов: такими они остаются после завершения задачи.
func withoutTokens(in domain.CollectInput) domain.CollectInput {
	in.MarketplaceTokens = nil
	in.OFDToken = ""
	return in
}
