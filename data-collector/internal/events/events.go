// Package events — сообщения, которыми части data-collector обмениваются через Kafka:
// API пишет их в таблицу outbox, outbox-publisher публикует, worker'ы читают.
package events

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// DefaultTopic — топик задач на сбор данных; читает worker сбора.
	DefaultTopic = "data-collector.collection-tasks"
	// DocumentsTopic — топик документов заявок; его читает worker проверки ЭП, позже — OCR.
	DocumentsTopic = "data-collector.documents"

	// CollectionRequested — создана задача на сбор данных по анкете.
	CollectionRequested = "collection.requested"
	// DocumentsSubmitted — с анкетой пришли документы: ждут проверки подписи и распознавания.
	DocumentsSubmitted = "documents.submitted"
)

// Message — событие из outbox: что и куда опубликовать.
type Message struct {
	ID      int64  // id строки outbox
	Topic   string // топик Kafka
	Key     string // ключ сообщения
	Payload []byte // JSON
}

// CollectionRequestedEvent — тело сообщения о новой задаче на сбор. Ключ — id задачи.
// Входных данных и токенов в нём нет: worker берёт их из collection_tasks по task_id.
type CollectionRequestedEvent struct {
	Event           string    `json:"event"` // CollectionRequested
	TaskID          string    `json:"task_id"`
	ApplicationID   string    `json:"application_id,omitempty"`
	QuestionnaireID string    `json:"questionnaire_id"`
	INN             string    `json:"inn"`
	CreatedAt       time.Time `json:"created_at"`
}

// NewCollectionRequested — событие о созданной задаче.
func NewCollectionRequested(t domain.CollectionTask) CollectionRequestedEvent {
	return CollectionRequestedEvent{
		Event:           CollectionRequested,
		TaskID:          t.ID,
		ApplicationID:   t.ApplicationID,
		QuestionnaireID: t.QuestionnaireID,
		INN:             t.Input.INN,
		CreatedAt:       t.CreatedAt,
	}
}

// DocumentsSubmittedEvent — тело сообщения о документах заявки. Ключ — id заявки.
// Файлы лежат в общем хранилище по storage_key. Worker подписи берёт из события только id
// и проверяет документы с подписью (signature_state = PENDING) и PDF/XML со встроенной;
// worker OCR — с recognition_state = PENDING.
type DocumentsSubmittedEvent struct {
	VerificationID string        `json:"verification_id,omitempty"`
	Event          string        `json:"event"` // DocumentsSubmitted
	ApplicationID  string        `json:"application_id"`
	CompanyINN     string        `json:"company_inn"`
	Documents      []DocumentRef `json:"documents"`
	CreatedAt      time.Time     `json:"created_at"`
}

// DocumentRef — документ в событии.
type DocumentRef struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	FileName         string `json:"file_name"`
	MimeType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
	SHA256           string `json:"sha256"`
	StorageKey       string `json:"storage_key"`
	SignedDocumentID string `json:"signed_document_id,omitempty"`
	SignatureState   string `json:"signature_state"`
	RecognitionState string `json:"recognition_state"`
}

// NewDocumentsSubmitted — событие о документах, пришедших с анкетой.
func NewDocumentsSubmitted(applicationID, companyINN string, docs []domain.Document, at time.Time) DocumentsSubmittedEvent {
	ev := DocumentsSubmittedEvent{Event: DocumentsSubmitted, ApplicationID: applicationID, CompanyINN: companyINN,
		Documents: make([]DocumentRef, 0, len(docs)), CreatedAt: at}
	for _, d := range docs {
		ev.Documents = append(ev.Documents, DocumentRef{ID: d.ID, Type: string(d.Type), FileName: d.FileName,
			MimeType: d.MimeType, SizeBytes: d.SizeBytes, SHA256: d.SHA256, StorageKey: d.StorageKey,
			SignedDocumentID: d.SignedDocumentID, SignatureState: string(d.SignatureState),
			RecognitionState: string(d.RecognitionState)})
	}
	return ev
}
