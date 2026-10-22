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

// CreateInput — данные анкеты, пришедшие с формы.
type CreateInput struct {
	Age           int
	MaritalStatus domain.MaritalStatus
	ChildrenCount int
}

// QuestionnaireService принимает анкеты и сохраняет их.
type QuestionnaireService struct {
	repo  Repository
	now   func() time.Time
	newID func() string
}

// Option настраивает QuestionnaireService (используется в тестах).
type Option func(*QuestionnaireService)

// WithClock подменяет источник времени.
func WithClock(now func() time.Time) Option { return func(s *QuestionnaireService) { s.now = now } }

// WithIDGenerator подменяет генератор идентификаторов.
func WithIDGenerator(newID func() string) Option {
	return func(s *QuestionnaireService) { s.newID = newID }
}

func NewQuestionnaireService(repo Repository, opts ...Option) *QuestionnaireService {
	s := &QuestionnaireService{
		repo:  repo,
		now:   time.Now,
		newID: func() string { return "qst_" + ulid.MustNew(ulid.Now(), rand.Reader).String() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create проверяет бизнес-правила и сохраняет анкету.
// Нарушения возвращаются как *ValidationError.
func (s *QuestionnaireService) Create(ctx context.Context, in CreateInput) (domain.Questionnaire, error) {
	if err := validate(in); err != nil {
		return domain.Questionnaire{}, err
	}

	q := domain.Questionnaire{
		ID:            s.newID(),
		Age:           in.Age,
		MaritalStatus: in.MaritalStatus,
		ChildrenCount: in.ChildrenCount,
		CreatedAt:     s.now().UTC(),
	}
	if err := s.repo.Save(ctx, q); err != nil {
		return domain.Questionnaire{}, fmt.Errorf("save questionnaire: %w", err)
	}
	return q, nil
}

func validate(in CreateInput) error {
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
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
