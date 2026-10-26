package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

var (
	// ErrRegistryUnavailable — ЕГРЮЛ не ответил: подсказку по ИНН дать нельзя, но заявку оформить можно.
	ErrRegistryUnavailable = errors.New("registry unavailable")
	// ErrReportNotReady — документ есть, но подпись по нему ещё не проверяли.
	ErrReportNotReady = errors.New("signature report is not ready")
)

// CabinetRepository — что кабинет клиента читает о компаниях и заявках пользователя. Объявлено у потребителя.
type CabinetRepository interface {
	// CompaniesOf — компании пользователя с ролью и реквизитами.
	CompaniesOf(ctx context.Context, userID string) ([]domain.CompanyProfile, error)
	// ApplicationIDs — заявки компаний пользователя по фильтру, новые сверху; черновики — только свои.
	ApplicationIDs(ctx context.Context, f domain.ApplicationFilter) ([]string, error)
	// Events — журнал заявки по времени.
	Events(ctx context.Context, applicationID string) ([]domain.ApplicationEvent, error)
	// LatestSignatureReport — последний отчёт проверки подписи документа (или его открепленной подписи).
	LatestSignatureReport(ctx context.Context, applicationID, documentID string) (json.RawMessage, bool, error)
	// CompanyKnown — компания с этим ИНН уже есть в системе.
	CompanyKnown(ctx context.Context, inn string) (bool, error)
}

// ApplicationReader — заявки с документами (ApplicationRepository.Get и List).
type ApplicationReader interface {
	Get(ctx context.Context, id string) (domain.Application, bool, error)
	// List — заявки по id в том же порядке; пропавшие пропускаются.
	List(ctx context.Context, ids []string) ([]domain.Application, error)
}

// Registry — ЕГРЮЛ/ЕГРИП для подсказки по ИНН (клиент mock-api).
type Registry interface {
	LookupCompany(ctx context.Context, inn string) (domain.CompanyCard, bool, error)
}

// FileOpener — файлы документов в хранилище.
type FileOpener interface {
	Open(key string) (io.ReadCloser, error)
}

// FileOpenerFunc — функция как FileOpener (storage.Local.Open возвращает *os.File).
type FileOpenerFunc func(key string) (io.ReadCloser, error)

func (f FileOpenerFunc) Open(key string) (io.ReadCloser, error) { return f(key) }

// CabinetService — кабинет клиента: его компании, список и журнал заявок, документы и отчёты о подписи,
// подсказка реквизитов по ИНН.
type CabinetService struct {
	repo     CabinetRepository
	apps     ApplicationReader
	registry Registry
	files    FileOpener
}

func NewCabinetService(repo CabinetRepository, apps ApplicationReader, registry Registry, files FileOpener) *CabinetService {
	return &CabinetService{repo: repo, apps: apps, registry: registry, files: files}
}

// Companies — компании пользователя.
func (s *CabinetService) Companies(ctx context.Context, userID string) ([]domain.CompanyProfile, error) {
	companies, err := s.repo.CompaniesOf(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	return companies, nil
}

// maxApplications — сколько заявок отдаёт список (пагинации пока нет).
const maxApplications = 100

// Applications — заявки компаний пользователя (свои и коллег по компании, черновики — только свои), новые сверху.
func (s *CabinetService) Applications(ctx context.Context, f domain.ApplicationFilter) ([]domain.Application, error) {
	f.Limit = maxApplications
	ids, err := s.repo.ApplicationIDs(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	apps, err := s.apps.List(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get applications: %w", err)
	}
	return apps, nil
}

// Timeline — журнал заявки. ErrNotFound — заявки нет.
func (s *CabinetService) Timeline(ctx context.Context, applicationID string) ([]domain.ApplicationEvent, error) {
	if _, err := s.application(ctx, applicationID); err != nil {
		return nil, err
	}
	events, err := s.repo.Events(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}

// Document — документ заявки и его содержимое. ErrNotFound — заявки или документа нет.
func (s *CabinetService) Document(ctx context.Context, applicationID, documentID string) (domain.Document, io.ReadCloser, error) {
	app, err := s.application(ctx, applicationID)
	if err != nil {
		return domain.Document{}, nil, err
	}
	i := slices.IndexFunc(app.Documents, func(d domain.Document) bool { return d.ID == documentID })
	if i < 0 {
		return domain.Document{}, nil, ErrNotFound
	}
	doc := app.Documents[i]
	f, err := s.files.Open(doc.StorageKey)
	if errors.Is(err, storage.ErrNotFound) {
		return domain.Document{}, nil, fmt.Errorf("%w: file of document %s is missing", ErrNotFound, doc.ID)
	}
	if err != nil {
		return domain.Document{}, nil, fmt.Errorf("open document %s: %w", doc.ID, err)
	}
	return doc, f, nil
}

// SignatureReport — последний отчёт проверки подписи документа или открепленной подписи (по id .sig — отчёт
// именно этой подписи). ErrNotFound — заявки или документа нет, ErrReportNotReady — подпись ещё не проверяли.
func (s *CabinetService) SignatureReport(ctx context.Context, applicationID, documentID string) (json.RawMessage, error) {
	app, err := s.application(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(app.Documents, func(d domain.Document) bool { return d.ID == documentID }) {
		return nil, ErrNotFound
	}
	report, ok, err := s.repo.LatestSignatureReport(ctx, applicationID, documentID)
	if err != nil {
		return nil, fmt.Errorf("get signature report: %w", err)
	}
	if !ok {
		return nil, ErrReportNotReady
	}
	return report, nil
}

// Lookup — реквизиты по ИНН из ЕГРЮЛ и знает ли компанию банк. ErrNotFound — в реестре нет,
// ErrRegistryUnavailable — реестр не ответил, *ValidationError — неверный ИНН.
func (s *CabinetService) Lookup(ctx context.Context, inn string) (domain.CompanyCard, bool, error) {
	if !innRe.MatchString(inn) {
		return domain.CompanyCard{}, false, &ValidationError{Fields: []FieldError{
			{Field: "inn", Code: "invalid_format", Message: "ИНН — 10 цифр для юрлица или 12 для ИП"}}}
	}
	card, found, err := s.registry.LookupCompany(ctx, inn)
	if err != nil {
		if ctx.Err() != nil {
			return domain.CompanyCard{}, false, ctx.Err() // клиент ушёл — реестр ни при чём
		}
		return domain.CompanyCard{}, false, fmt.Errorf("%w: %w", ErrRegistryUnavailable, err)
	}
	if !found {
		return domain.CompanyCard{}, false, ErrNotFound
	}
	known, err := s.repo.CompanyKnown(ctx, inn)
	if err != nil {
		return domain.CompanyCard{}, false, fmt.Errorf("company known: %w", err)
	}
	return card, known, nil
}

func (s *CabinetService) application(ctx context.Context, id string) (domain.Application, error) {
	app, ok, err := s.apps.Get(ctx, id)
	if err != nil {
		return domain.Application{}, fmt.Errorf("get application: %w", err)
	}
	if !ok {
		return domain.Application{}, ErrNotFound
	}
	return app, nil
}
