// Package service — слой бизнес-логики модуля сбора данных.
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	minAge      = 18
	maxAge      = 100
	maxChildren = 20
)

// Repository — хранилище анкет. Объявлено здесь, у потребителя:
// сервису всё равно, память это или Postgres.
type Repository interface {
	Save(ctx context.Context, q domain.Questionnaire) error
}

// Collector — сбор данных о компании по анкете (CollectionService).
type Collector interface {
	Validate(in domain.CollectInput) []FieldError
	Collect(ctx context.Context, questionnaireID string, in domain.CollectInput) (domain.RawCollection, error)
}

// CreateInput — данные анкеты, пришедшие с формы.
type CreateInput struct {
	Age           int
	MaritalStatus domain.MaritalStatus
	ChildrenCount int
	Company       domain.CollectInput // по чему собирать данные: ИНН, руководитель, согласия, токены, залог
}

// Created — сохранённая анкета и итог сбора данных по ней.
type Created struct {
	Questionnaire domain.Questionnaire
	Collection    domain.RawCollection
}

// QuestionnaireService принимает анкеты, сохраняет их и запускает сбор данных о компании.
type QuestionnaireService struct {
	repo      Repository
	collector Collector
	now       func() time.Time
	newID     func() string
}

// Option настраивает QuestionnaireService (используется в тестах).
type Option func(*QuestionnaireService)

// WithClock подменяет источник времени.
func WithClock(now func() time.Time) Option { return func(s *QuestionnaireService) { s.now = now } }

// WithIDGenerator подменяет генератор идентификаторов.
func WithIDGenerator(newID func() string) Option {
	return func(s *QuestionnaireService) { s.newID = newID }
}

func NewQuestionnaireService(repo Repository, collector Collector, opts ...Option) *QuestionnaireService {
	s := &QuestionnaireService{
		repo:      repo,
		collector: collector,
		now:       time.Now,
		newID:     func() string { return "qst_" + ulid.MustNew(ulid.Now(), rand.Reader).String() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create проверяет анкету и данные для сбора, сохраняет анкету и собирает данные о компании.
// Нарушения возвращаются как *ValidationError — все сразу, и ничего не сохраняется.
// Отказ отдельной внешней системы ошибкой не считается: он виден в Collection.
func (s *QuestionnaireService) Create(ctx context.Context, in CreateInput) (Created, error) {
	fields := validate(in)
	fields = append(fields, s.collector.Validate(in.Company)...)
	if len(fields) > 0 {
		return Created{}, &ValidationError{Fields: fields}
	}

	q := domain.Questionnaire{
		ID:            s.newID(),
		Age:           in.Age,
		MaritalStatus: in.MaritalStatus,
		ChildrenCount: in.ChildrenCount,
		CreatedAt:     s.now().UTC(),
	}
	if err := s.repo.Save(ctx, q); err != nil {
		return Created{}, fmt.Errorf("save questionnaire: %w", err)
	}

	rc, err := s.collector.Collect(ctx, q.ID, in.Company)
	if err != nil {
		return Created{Questionnaire: q}, fmt.Errorf("collect company data: %w", err)
	}
	return Created{Questionnaire: q, Collection: rc}, nil
}

func validate(in CreateInput) []FieldError {
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
