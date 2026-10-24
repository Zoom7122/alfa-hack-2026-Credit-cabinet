// Package memory — хранилища в памяти процесса: анкеты (пока их нет в Postgres)
// и задачи на сбор для тестов.
package memory

import (
	"context"
	"sync"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

type QuestionnaireRepository struct {
	mu    sync.RWMutex
	items map[string]domain.Questionnaire
}

func NewQuestionnaireRepository() *QuestionnaireRepository {
	return &QuestionnaireRepository{items: make(map[string]domain.Questionnaire)}
}

func (r *QuestionnaireRepository) Save(_ context.Context, q domain.Questionnaire) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[q.ID] = q
	return nil
}

// Get возвращает анкету по id.
func (r *QuestionnaireRepository) Get(_ context.Context, id string) (domain.Questionnaire, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q, ok := r.items[id]
	return q, ok
}
