package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// OutcomeReader — итог оценки по заявке (repository/postgres/backoffice.go).
type OutcomeReader interface {
	LatestDecision(ctx context.Context, appID string) (*domain.DecisionRecord, error)
	Offers(ctx context.Context, appID string) ([]domain.OfferRecord, error)
	InfoRequests(ctx context.Context, appID string) ([]domain.InfoRequestRecord, error)
}

// AnswerWriter — приём ответа клиента (repository/postgres/application.go).
type AnswerWriter interface {
	Get(ctx context.Context, id string) (domain.Application, bool, error)
	AnswerInfoRequest(ctx context.Context, a domain.InfoAnswer) (domain.ApplicationStatus, bool, error)
}

// ClientDecision — итог для клиента: без баллов и внутренних причин.
type ClientDecision struct {
	Outcome      string // APPROVED, APPROVED_WITH_CHANGES, REJECTED, MANUAL_REVIEW, NEED_INFO
	Reasons      []domain.ClientReason
	ReapplyAfter string // YYYY-MM-DD; пусто — не ограничено
	DecidedAt    time.Time
	ByEmployee   bool
}

// AnswerInput — ответ на вопрос банка.
type AnswerInput struct {
	DocumentIDs []string
	Value       json.RawMessage
	Comment     string
	Files       []storage.Staged
}

// OutcomeService — итог для клиента: решение, предложения, вопросы банка и ответы на них.
type OutcomeService struct {
	reader    OutcomeReader
	apps      AnswerWriter
	files     FileStorage
	reviewSLA time.Duration
	now       func() time.Time
	newDocID  func() string
}

func NewOutcomeService(reader OutcomeReader, apps AnswerWriter, files FileStorage, reviewSLA time.Duration) *OutcomeService {
	return &OutcomeService{reader: reader, apps: apps, files: files, reviewSLA: reviewSLA, now: time.Now,
		newDocID: func() string { return "doc_" + ulid.MustNew(ulid.Now(), rand.Reader).String() }}
}

func (s *OutcomeService) application(ctx context.Context, id string) (domain.Application, error) {
	app, ok, err := s.apps.Get(ctx, id)
	if err != nil {
		return domain.Application{}, fmt.Errorf("get application: %w", err)
	}
	if !ok {
		return domain.Application{}, ErrNotFound
	}
	return app, nil
}

// Decision — итог по заявке. ErrNotFound — заявки нет или её ещё не оценили.
func (s *OutcomeService) Decision(ctx context.Context, appID string) (ClientDecision, error) {
	if _, err := s.application(ctx, appID); err != nil {
		return ClientDecision{}, err
	}
	d, err := s.reader.LatestDecision(ctx, appID)
	if err != nil {
		return ClientDecision{}, err
	}
	if d == nil {
		return ClientDecision{}, ErrNotFound
	}
	out := ClientDecision{Outcome: d.AutoOutcome, DecidedAt: d.CreatedAt}
	var details struct {
		Underwriter *struct {
			DecidedAt    *time.Time `json:"decided_at"`
			ReapplyAfter *string    `json:"reapply_after"`
		} `json:"underwriter"`
	}
	_ = json.Unmarshal(d.Details, &details)
	if d.Outcome != "" {
		out.Outcome, out.ByEmployee = d.Outcome, true
		if u := details.Underwriter; u != nil && u.DecidedAt != nil {
			out.DecidedAt = *u.DecidedAt
		}
	}
	if len(d.ClientReasons) > 0 {
		_ = json.Unmarshal(d.ClientReasons, &out.Reasons)
	}
	if out.Outcome == "REJECTED" {
		out.ReapplyAfter = out.DecidedAt.AddDate(0, 0, reapplyAfterDays).Format(time.DateOnly)
		if u := details.Underwriter; u != nil && u.ReapplyAfter != nil {
			out.ReapplyAfter = *u.ReapplyAfter
		}
	}
	return out, nil
}

// Offers — предложения по заявке. ErrNotFound — заявки нет.
func (s *OutcomeService) Offers(ctx context.Context, appID string) ([]domain.OfferRecord, error) {
	if _, err := s.application(ctx, appID); err != nil {
		return nil, err
	}
	return s.reader.Offers(ctx, appID)
}

// InfoRequests — вопросы банка. ErrNotFound — заявки нет.
func (s *OutcomeService) InfoRequests(ctx context.Context, appID string) ([]domain.InfoRequestRecord, error) {
	if _, err := s.application(ctx, appID); err != nil {
		return nil, err
	}
	return s.reader.InfoRequests(ctx, appID)
}

// StageFile принимает файл ответа во временную папку. ErrFileTooLarge — больше лимита.
func (s *OutcomeService) StageFile(name string, r io.Reader) (storage.Staged, error) {
	st, err := s.files.Stage(name, r)
	if errors.Is(err, storage.ErrTooLarge) {
		return storage.Staged{}, ErrFileTooLarge
	}
	return st, err
}

// Discard удаляет принятые файлы, если до ответа дело не дошло.
func (s *OutcomeService) Discard(files []storage.Staged) { s.files.Discard(files...) }

type option struct {
	Value string `json:"value"`
}

// question — как отвечать на вопрос (info_requests.question).
type question struct {
	Input struct {
		Type         string   `json:"type"`
		ScoringField string   `json:"scoring_field"`
		Options      []option `json:"options"`
	} `json:"input"`
	Documents *struct {
		Required      bool     `json:"required"`
		AcceptedTypes []string `json:"accepted_types"`
	} `json:"documents"`
}

// Answer принимает ответ на вопрос банка: значение по типу вопроса, комментарий, файлы.
// Возвращает статус заявки после ответа. *ValidationError — ошибки полей; ErrNotFound;
// ErrForbidden, ErrUnauthorized — отвечает не автор заявки; domain.ErrNotWaitingForInfo, domain.ErrQuestionClosed.
func (s *OutcomeService) Answer(ctx context.Context, actorID, appID, requestID string, in AnswerInput) (domain.ApplicationStatus, error) {
	defer s.files.Discard(in.Files...)
	app, err := s.application(ctx, appID)
	if err != nil {
		return "", err
	}
	switch {
	case actorID != "" && actorID != app.Representative.UserID:
		return "", ErrForbidden
	case actorID == "" && app.Representative.HasLogin:
		return "", ErrUnauthorized
	}
	requests, err := s.reader.InfoRequests(ctx, appID)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(requests, func(r domain.InfoRequestRecord) bool { return r.ID == requestID })
	if i < 0 {
		return "", ErrNotFound
	}
	var q question
	_ = json.Unmarshal(requests[i].Question, &q)

	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}
	comment := strings.TrimSpace(in.Comment)
	if utf8.RuneCountInString(comment) > maxCommentRunes {
		add("comment", "too_long", fmt.Sprintf("не длиннее %d символов", maxCommentRunes))
	}
	value, patch := in.Value, map[string]any{}
	hasValue := len(value) > 0 && string(value) != "null"
	switch q.Input.Type {
	case "", "NONE":
		value = nil
	case "NUMBER", "MONEY":
		var v float64
		if !hasValue || json.Unmarshal(value, &v) != nil || v < 0 {
			add("value", "invalid_value", "нужно неотрицательное число")
		} else if f := q.Input.ScoringField; f != "" {
			patch[f] = int64(v) // суммы, сроки, стаж — целые; ставка — дробная
			if f == "max_annual_rate" {
				patch[f] = v
			}
		}
	case "CHOICE":
		var v string
		if !hasValue || json.Unmarshal(value, &v) != nil || !slices.ContainsFunc(q.Input.Options, func(o option) bool { return o.Value == v }) {
			add("value", "invalid_value", "выберите один из вариантов")
		} else if q.Input.ScoringField != "" {
			patch[q.Input.ScoringField] = v
		}
	default: // TEXT, DATE, BOOLEAN — значение как есть
		if !hasValue && comment == "" {
			add("value", "required", "обязательное поле")
		}
	}
	seen := make(map[string]bool)
	hasDocument := false
	for _, id := range in.DocumentIDs {
		i := slices.IndexFunc(app.Documents, func(d domain.Document) bool { return d.ID == id && d.InfoRequestID == requestID })
		if seen[id] || i < 0 {
			add("document_ids", "invalid_document", "документ не найден или относится к другому вопросу")
			continue
		}
		seen[id] = true
		doc := app.Documents[i]
		if doc.Type != domain.DocSignature {
			hasDocument = true
			if q.Documents != nil && len(q.Documents.AcceptedTypes) > 0 && !slices.Contains(q.Documents.AcceptedTypes, string(doc.Type)) {
				add("document_ids", "invalid_type", "этот тип документа не подходит к вопросу")
			}
		}
	}
	if q.Documents != nil && q.Documents.Required && len(in.Files) == 0 && !hasDocument {
		add("files", "required", "приложите документ")
	}
	if len(fields) > 0 {
		return "", &ValidationError{Fields: fields}
	}

	docType := domain.DocOther
	if q.Documents != nil && len(q.Documents.AcceptedTypes) > 0 && domain.DocumentType(q.Documents.AcceptedTypes[0]).Valid() {
		docType = domain.DocumentType(q.Documents.AcceptedTypes[0])
	}
	var specs []DocumentSpec
	for _, f := range in.Files {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".sig") && !strings.HasSuffix(strings.ToLower(f.Name), ".p7s") {
			specs = append(specs, DocumentSpec{FileName: f.Name, Type: docType})
		}
	}
	now := s.now().UTC()
	planned, fileFields := planDocuments(app.ID, app.Representative.UserID, in.Files, specs, s.newDocID, now)
	if len(fileFields) > 0 {
		return "", &ValidationError{Fields: fileFields}
	}
	docs := make([]domain.Document, 0, len(planned))
	keys := make([]string, 0, len(planned))
	for _, p := range planned {
		if err := s.files.Commit(p.staged, p.doc.StorageKey); err != nil {
			s.files.Remove(keys...)
			return "", fmt.Errorf("save document %s: %w", p.doc.FileName, err)
		}
		keys = append(keys, p.doc.StorageKey)
		docs = append(docs, p.doc)
	}
	next, ok, err := s.apps.AnswerInfoRequest(ctx, domain.InfoAnswer{ApplicationID: app.ID, RequestID: requestID,
		ActorID: app.Representative.UserID, CompanyINN: app.Company.INN, Value: value, Comment: comment, Documents: docs,
		DocumentIDs:  in.DocumentIDs,
		ScoringPatch: patch, Now: now, ReviewSLA: s.reviewSLA})
	if err != nil || !ok {
		s.files.Remove(keys...)
	}
	if err != nil {
		if errors.Is(err, domain.ErrInvalidAnswerDocuments) {
			return "", &ValidationError{Fields: []FieldError{{Field: "document_ids", Code: "invalid_document", Message: "документ удалён или относится к другому вопросу"}}}
		}
		return "", err
	}
	if !ok {
		return "", ErrNotFound
	}
	return next, nil
}
