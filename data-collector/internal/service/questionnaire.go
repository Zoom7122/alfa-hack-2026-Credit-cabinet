// Package service — слой бизнес-логики модуля сбора данных.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

const (
	minAge      = 18
	maxAge      = 100
	maxChildren = 20

	maxProductCode   = 64
	maxTermMonths    = 600
	maxPurposeLength = 2000
)

// saveTimeout — на запись заявки, когда клиент уже отключился.
const saveTimeout = 5 * time.Second

// FileStorage — где лежат файлы документов. Объявлено у потребителя.
type FileStorage interface {
	Stage(name string, r io.Reader) (storage.Staged, error)
	Commit(st storage.Staged, key string) error
	Discard(staged ...storage.Staged)
	Remove(keys ...string)
}

// Collector — проверка данных для сбора и задача на сбор (CollectionService).
type Collector interface {
	Validate(in domain.CollectInput) []FieldError
	NewTask(questionnaireID string, in domain.CollectInput) domain.CollectionTask
}

// SubmitInput — анкета с формы, данные заявки и принятые файлы.
type SubmitInput struct {
	// ActorID — вошедший по токену пользователь; пусто — без токена. Анкету отправляет только автор
	// заявки, а заявку тестового пользователя — только с его токеном.
	ActorID       string
	Age           int
	MaritalStatus domain.MaritalStatus
	ChildrenCount int
	Company       domain.CollectInput // по чему собирать данные; ИНН берётся из компании заявки
	ProductCode   string
	Requested     *domain.RequestedTerms
	Purpose       *domain.Purpose
	Documents     []DocumentSpec   // типы документов по именам файлов
	Files         []storage.Staged // принятые через StageFile
}

// Submitted — отправленная заявка, анкета, задача на сбор и документы.
type Submitted struct {
	Application   domain.Application
	Questionnaire domain.Questionnaire
	Task          domain.CollectionTask
	Documents     []domain.Document
}

// QuestionnaireService принимает анкету по заявке вместе с файлами, отправляет заявку
// и ставит сбор данных о компании в очередь.
type QuestionnaireService struct {
	apps      ApplicationRepository
	files     FileStorage
	collector Collector
	now       func() time.Time
	newID     func() string // анкеты: qst_…
	newDocID  func() string // документы: doc_…
}

// Option настраивает QuestionnaireService (используется в тестах).
type Option func(*QuestionnaireService)

// WithClock подменяет источник времени.
func WithClock(now func() time.Time) Option { return func(s *QuestionnaireService) { s.now = now } }

// WithIDGenerator подменяет генератор идентификаторов анкет.
func WithIDGenerator(newID func() string) Option {
	return func(s *QuestionnaireService) { s.newID = newID }
}

// WithDocumentIDGenerator подменяет генератор идентификаторов документов.
func WithDocumentIDGenerator(newID func() string) Option {
	return func(s *QuestionnaireService) { s.newDocID = newID }
}

func NewQuestionnaireService(apps ApplicationRepository, files FileStorage, collector Collector, opts ...Option) *QuestionnaireService {
	s := &QuestionnaireService{
		apps:      apps,
		files:     files,
		collector: collector,
		now:       time.Now,
		newID:     func() string { return "qst_" + ulid.MustNew(ulid.Now(), rand.Reader).String() },
		newDocID:  monotonicID("doc_"),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// monotonicID — генератор ULID с префиксом, возрастающих даже в пределах одной миллисекунды:
// документы одной анкеты отдаются в том порядке, в каком пришли.
func monotonicID(prefix string) func() string {
	var mu sync.Mutex
	entropy := ulid.Monotonic(rand.Reader, 0)
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return prefix + ulid.MustNew(ulid.Now(), entropy).String()
	}
}

// StageFile принимает файл во временную папку хранилища. ErrFileTooLarge — файл больше лимита.
func (s *QuestionnaireService) StageFile(name string, r io.Reader) (storage.Staged, error) {
	st, err := s.files.Stage(name, r)
	if errors.Is(err, storage.ErrTooLarge) {
		return storage.Staged{}, ErrFileTooLarge
	}
	return st, err
}

// Discard удаляет принятые файлы, если запрос не дошёл до Submit.
func (s *QuestionnaireService) Discard(files []storage.Staged) {
	s.files.Discard(files...)
}

// Submit проверяет анкету, данные для сбора и файлы, переносит файлы на постоянное место и одной
// транзакцией отправляет заявку (SUBMITTED) и ставит задачу на сбор. Нарушения — *ValidationError,
// все сразу, и ничего не сохраняется. ErrNotFound — заявки нет; ErrInvalidTransition — анкету
// по ней уже отправили. Принятые файлы во временной папке не остаются.
func (s *QuestionnaireService) Submit(ctx context.Context, applicationID string, in SubmitInput) (Submitted, error) {
	defer s.files.Discard(in.Files...)

	app, ok, err := s.apps.Get(ctx, applicationID)
	if err != nil {
		return Submitted{}, fmt.Errorf("get application: %w", err)
	}
	if !ok {
		return Submitted{}, ErrNotFound
	}
	switch {
	case in.ActorID != "" && in.ActorID != app.Representative.UserID:
		return Submitted{}, ErrForbidden
	case in.ActorID == "" && app.Representative.HasLogin:
		return Submitted{}, ErrUnauthorized
	}
	if app.Status != domain.ApplicationDraft {
		return Submitted{}, ErrInvalidTransition
	}

	in.Company.INN = app.Company.INN
	in.ProductCode = strings.TrimSpace(in.ProductCode)
	now := s.now().UTC()
	fields := validate(in)
	fields = append(fields, s.collector.Validate(in.Company)...)
	fields = append(fields, validateApplication(in)...)
	planned, fileFields := planDocuments(app.ID, app.Representative.UserID, in.Files, in.Documents, s.newDocID, now)
	fields = append(fields, fileFields...)
	if len(fields) > 0 {
		return Submitted{}, &ValidationError{Fields: fields}
	}

	q := domain.Questionnaire{
		ID:            s.newID(),
		Age:           in.Age,
		MaritalStatus: in.MaritalStatus,
		ChildrenCount: in.ChildrenCount,
		CreatedAt:     now,
	}
	task := s.collector.NewTask(q.ID, in.Company)
	task.ApplicationID = app.ID
	docs := make([]domain.Document, 0, len(planned))
	for _, p := range planned {
		docs = append(docs, p.doc)
		task.DocumentIDs = append(task.DocumentIDs, p.doc.ID)
	}

	// Файлы — на постоянное место, затем одна транзакция. Не вышло — файлы этого запроса удаляются.
	keys := make([]string, 0, len(planned))
	for _, p := range planned {
		if err := s.files.Commit(p.staged, p.doc.StorageKey); err != nil {
			s.files.Remove(keys...)
			return Submitted{}, fmt.Errorf("save document %s: %w", p.doc.FileName, err)
		}
		keys = append(keys, p.doc.StorageKey)
	}
	// Отправку доводим до конца, даже если клиент отключился: файлы уже на месте.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	sub := domain.Submission{ApplicationID: app.ID, ActorID: app.Representative.UserID, Questionnaire: q,
		ProductCode: in.ProductCode, Requested: in.Requested, Purpose: in.Purpose, Documents: docs, Task: task,
		SubmittedAt: now}
	if err := s.apps.Submit(saveCtx, sub); err != nil {
		s.files.Remove(keys...)
		if errors.Is(err, domain.ErrApplicationNotDraft) {
			return Submitted{}, ErrInvalidTransition
		}
		return Submitted{}, fmt.Errorf("submit application: %w", err)
	}

	app.Status, app.SubmittedAt, app.CollectionTaskID = domain.ApplicationSubmitted, now, task.ID
	app.ProductCode, app.Requested, app.Purpose, app.Documents = sub.ProductCode, sub.Requested, sub.Purpose, docs
	return Submitted{Application: app, Questionnaire: q, Task: task, Documents: docs}, nil
}

func validate(in SubmitInput) []FieldError {
	var fields []FieldError
	if in.Age < minAge || in.Age > maxAge {
		fields = append(fields, FieldError{
			Field:   "age",
			Code:    "out_of_range",
			Message: fmt.Sprintf("возраст должен быть от %d до %d лет", minAge, maxAge),
		})
	}
	if !in.MaritalStatus.Valid() {
		fields = append(fields, FieldError{
			Field:   "marital_status",
			Code:    "invalid_value",
			Message: "допустимые значения: SINGLE, MARRIED, DIVORCED, DIVORCED_MULTIPLE",
		})
	}
	if in.ChildrenCount < 0 || in.ChildrenCount > maxChildren {
		fields = append(fields, FieldError{
			Field:   "children_count",
			Code:    "out_of_range",
			Message: fmt.Sprintf("количество детей и иждивенцев должно быть от 0 до %d", maxChildren),
		})
	}
	return fields
}

// validateApplication проверяет данные заявки: продукт, сумму и срок, цель.
func validateApplication(in SubmitInput) []FieldError {
	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}

	if len(in.ProductCode) > maxProductCode {
		add("product_code", "too_long", fmt.Sprintf("не длиннее %d символов", maxProductCode))
	}
	if r := in.Requested; r != nil {
		if r.Amount <= 0 {
			add("requested.amount", "out_of_range", "сумма должна быть больше нуля")
		}
		if r.TermMonths <= 0 || r.TermMonths > maxTermMonths {
			add("requested.term_months", "out_of_range", fmt.Sprintf("срок от 1 до %d месяцев", maxTermMonths))
		}
	}
	if p := in.Purpose; p != nil {
		if !slices.Contains(domain.PurposeCodes, p.Code) {
			add("purpose.code", "invalid_value", "допустимые значения: "+strings.Join(domain.PurposeCodes, ", "))
		}
		if utf8.RuneCountInString(p.Description) > maxPurposeLength {
			add("purpose.description", "too_long", fmt.Sprintf("не длиннее %d символов", maxPurposeLength))
		}
	}
	return fields
}
