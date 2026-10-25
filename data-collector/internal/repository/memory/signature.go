package memory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// SignatureRepository — документы, контекст заявок и отчёты проверки ЭП в памяти. Семантика как у
// Postgres: отчёт с тем же ключом идемпотентности второй раз не пишется, документ и подпись
// переходят в DONE/FAILED с вердиктом вместе с записью отчёта. Используется в тестах.
type SignatureRepository struct {
	mu       sync.Mutex
	docs     map[string][]domain.Document // по заявке, в порядке id
	contexts map[string]domain.SignatureContext
	reports  []domain.SignatureReport
	verdicts map[string]string // signature_verdict по id документа
	events   map[string][]string
}

func NewSignatureRepository() *SignatureRepository {
	return &SignatureRepository{docs: map[string][]domain.Document{}, contexts: map[string]domain.SignatureContext{},
		verdicts: map[string]string{}, events: map[string][]string{}}
}

// AddDocuments добавляет документы заявки.
func (r *SignatureRepository) AddDocuments(docs ...domain.Document) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range docs {
		r.docs[d.ApplicationID] = append(r.docs[d.ApplicationID], d)
		slices.SortFunc(r.docs[d.ApplicationID], func(a, b domain.Document) int { return strings.Compare(a.ID, b.ID) })
	}
}

// SetContext задаёт заявителя и сбор по заявке.
func (r *SignatureRepository) SetContext(sc domain.SignatureContext) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contexts[sc.ApplicationID] = sc
}

func (r *SignatureRepository) Documents(_ context.Context, applicationID string) ([]domain.Document, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.docs[applicationID]), nil
}

func (r *SignatureRepository) Context(_ context.Context, applicationID string) (domain.SignatureContext, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sc, ok := r.contexts[applicationID]
	return sc, ok, nil
}

func (r *SignatureRepository) ReportExists(_ context.Context, key string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.reports, func(rep domain.SignatureReport) bool { return rep.IdempotencyKey == key }), nil
}

func (r *SignatureRepository) SaveReport(_ context.Context, rep domain.SignatureReport) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slices.ContainsFunc(r.reports, func(x domain.SignatureReport) bool { return x.IdempotencyKey == rep.IdempotencyKey }) {
		return false, nil
	}
	r.reports = append(r.reports, rep)
	docs := r.docs[rep.ApplicationID]
	for i := range docs {
		if docs[i].ID == rep.DocumentID || docs[i].ID == rep.SignatureDocumentID {
			docs[i].SignatureState = rep.DocumentState
			r.verdicts[docs[i].ID] = rep.Verdict
		}
	}
	r.events[rep.ApplicationID] = append(r.events[rep.ApplicationID], "document.signatures_verified")
	return true, nil
}

// Reports — записанные отчёты в порядке записи.
func (r *SignatureRepository) Reports() []domain.SignatureReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reports)
}

// Signature — signature_state и signature_verdict документа.
func (r *SignatureRepository) Signature(applicationID, documentID string) (domain.SignatureState, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.docs[applicationID] {
		if d.ID == documentID {
			return d.SignatureState, r.verdicts[documentID]
		}
	}
	return "", ""
}

// Events — события журнала заявки, записанные вместе с отчётами.
func (r *SignatureRepository) Events(applicationID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events[applicationID])
}
