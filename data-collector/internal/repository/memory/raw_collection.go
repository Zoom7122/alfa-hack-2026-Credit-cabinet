package memory

import (
	"context"
	"sync"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// RawCollectionRepository хранит сборы данных в памяти процесса.
// Используется, когда DATABASE_URL не задан, и в тестах.
type RawCollectionRepository struct {
	mu    sync.RWMutex
	items map[string]domain.RawCollection
}

func NewRawCollectionRepository() *RawCollectionRepository {
	return &RawCollectionRepository{items: make(map[string]domain.RawCollection)}
}

func (r *RawCollectionRepository) Save(_ context.Context, rc domain.RawCollection) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[rc.ID] = rc
	return nil
}

// Get возвращает сбор по id.
func (r *RawCollectionRepository) Get(_ context.Context, id string) (domain.RawCollection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rc, ok := r.items[id]
	return rc, ok
}
