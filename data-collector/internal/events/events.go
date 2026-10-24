// Package events — сообщения, которыми части data-collector обмениваются через Kafka:
// API пишет их в таблицу outbox, outbox-publisher публикует, worker читает.
package events

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// DefaultTopic — топик задач на сбор данных.
	DefaultTopic = "data-collector.collection-tasks"
	// CollectionRequested — создана задача на сбор данных по анкете.
	CollectionRequested = "collection.requested"
)

// Message — событие из outbox: что и куда опубликовать.
type Message struct {
	ID      int64  // id строки outbox
	Topic   string // топик Kafka
	Key     string // ключ сообщения: id задачи
	Payload []byte // JSON
}

// CollectionRequestedEvent — тело сообщения о новой задаче на сбор. Входных данных
// и токенов в нём нет: worker берёт их из collection_tasks по task_id.
type CollectionRequestedEvent struct {
	Event           string    `json:"event"` // CollectionRequested
	TaskID          string    `json:"task_id"`
	QuestionnaireID string    `json:"questionnaire_id"`
	INN             string    `json:"inn"`
	CreatedAt       time.Time `json:"created_at"`
}

// NewCollectionRequested — событие о созданной задаче.
func NewCollectionRequested(t domain.CollectionTask) CollectionRequestedEvent {
	return CollectionRequestedEvent{
		Event:           CollectionRequested,
		TaskID:          t.ID,
		QuestionnaireID: t.QuestionnaireID,
		INN:             t.Input.INN,
		CreatedAt:       t.CreatedAt,
	}
}
