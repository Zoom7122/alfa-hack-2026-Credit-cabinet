package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	maxBanks      = 10
	maxCollateral = 20
)

var (
	personINNRe = regexp.MustCompile(`^\d{12}$`)
	bikRe       = regexp.MustCompile(`^\d{9}$`)
	cadastralRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{6,7}:\d+$`)
	vinRe       = regexp.MustCompile(`^[A-HJ-NPR-Z0-9]{17}$`) // в VIN нет I, O, Q
	uuidRe      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// saveTimeout — на запись задачи, когда клиент уже отключился.
const saveTimeout = 5 * time.Second

// TaskRepository — хранилище задач на сбор (таблица collection_tasks). Объявлено у потребителя.
type TaskRepository interface {
	// Create сохраняет задачу вместе с событием о ней в outbox — атомарно:
	// событие не теряется и не публикуется без задачи.
	Create(ctx context.Context, task domain.CollectionTask) error
	// Get возвращает задачу и, если она выполнена, итог сбора. false — задачи нет.
	Get(ctx context.Context, id string) (domain.CollectionTask, bool, error)
}

// CollectionService ставит сбор данных о компании в очередь и отдаёт его состояние.
// Сами системы опрашивает worker (internal/worker) по событию из Kafka.
type CollectionService struct {
	tasks TaskRepository
	now   func() time.Time
	newID func() string
}

// CollectionOption настраивает CollectionService (используется в тестах).
type CollectionOption func(*CollectionService)

// WithCollectionClock подменяет источник времени.
func WithCollectionClock(now func() time.Time) CollectionOption {
	return func(s *CollectionService) { s.now = now }
}

// WithCollectionIDGenerator подменяет генератор идентификаторов задач.
func WithCollectionIDGenerator(newID func() string) CollectionOption {
	return func(s *CollectionService) { s.newID = newID }
}

func NewCollectionService(tasks TaskRepository, opts ...CollectionOption) *CollectionService {
	s := &CollectionService{
		tasks: tasks,
		now:   time.Now,
		newID: func() string { return "tsk_" + ulid.MustNew(ulid.Now(), rand.Reader).String() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Validate проверяет входные данные сбора и возвращает все нарушения сразу.
func (s *CollectionService) Validate(in domain.CollectInput) []FieldError {
	return validateCollect(normalize(in))
}

// Enqueue создаёт задачу на сбор по анкете (PENDING) и событие о ней для worker.
// Системы без входных данных сразу попадают в Skipped: worker их не опрашивает.
func (s *CollectionService) Enqueue(ctx context.Context, questionnaireID string, in domain.CollectInput) (domain.CollectionTask, error) {
	// Задачу ставим, даже если клиент отключился: анкета уже сохранена.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()

	in = normalize(in)
	task := domain.CollectionTask{
		ID:              s.newID(),
		QuestionnaireID: questionnaireID,
		Input:           in,
		Status:          domain.TaskPending,
		CreatedAt:       s.now().UTC(),
	}
	skipped := domain.SkippedSystems(in)
	for _, sys := range domain.Systems {
		if _, ok := skipped[sys]; ok {
			task.Skipped = append(task.Skipped, sys)
		}
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return domain.CollectionTask{}, fmt.Errorf("create collection task: %w", err)
	}
	return task, nil
}

// Get возвращает задачу на сбор, у выполненной — с итогом по системам.
// ErrNotFound — задачи с таким id нет.
func (s *CollectionService) Get(ctx context.Context, id string) (domain.CollectionTask, error) {
	task, ok, err := s.tasks.Get(ctx, id)
	if err != nil {
		return domain.CollectionTask{}, fmt.Errorf("get collection task: %w", err)
	}
	if !ok {
		return domain.CollectionTask{}, ErrNotFound
	}
	return task, nil
}

// normalize убирает пробелы, повторы и регистр там, где он не важен.
func normalize(in domain.CollectInput) domain.CollectInput {
	trim := strings.TrimSpace
	in.INN = trim(in.INN)
	if in.Director != nil {
		d := *in.Director
		d.INN, d.LastName, d.FirstName, d.MiddleName, d.BirthDate =
			trim(d.INN), trim(d.LastName), trim(d.FirstName), trim(d.MiddleName), trim(d.BirthDate)
		in.Director = &d
	}
	in.OtherBankBIKs = dedupe(in.OtherBankBIKs, trim)
	if len(in.MarketplaceTokens) > 0 {
		tokens := make(map[string]string, len(in.MarketplaceTokens))
		for mp, tok := range in.MarketplaceTokens {
			tokens[strings.ToLower(trim(mp))] = trim(tok)
		}
		in.MarketplaceTokens = tokens
	}
	in.OFDToken = trim(in.OFDToken)
	in.Collateral = domain.Collateral{
		CadastralNumbers: dedupe(in.Collateral.CadastralNumbers, trim),
		VINs:             dedupe(in.Collateral.VINs, func(s string) string { return strings.ToUpper(trim(s)) }),
		Serials:          dedupe(in.Collateral.Serials, func(s string) string { return strings.ToUpper(trim(s)) }),
	}
	in.POANumber = strings.ToLower(trim(in.POANumber))
	in.RepresentativeINN = trim(in.RepresentativeINN)
	return in
}

func dedupe(values []string, norm func(string) string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		v = norm(v)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func validateCollect(in domain.CollectInput) []FieldError {
	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}

	if !innRe.MatchString(in.INN) {
		add("inn", "invalid_format", "ИНН должен состоять из 10 (ЮЛ) или 12 (ИП) цифр")
	}

	if d := in.Director; d != nil {
		if !personINNRe.MatchString(d.INN) {
			add("director.inn", "invalid_format", "ИНН физлица — 12 цифр")
		}
		if d.LastName == "" {
			add("director.last_name", "required", "обязательное поле")
		}
		if d.FirstName == "" {
			add("director.first_name", "required", "обязательное поле")
		}
		if !validDate(d.BirthDate) {
			add("director.birth_date", "invalid_format", "дата рождения в формате ГГГГ-ММ-ДД")
		}
	}

	for _, c := range []struct {
		field   string
		consent *domain.Consent
	}{{"consents.bki_company", in.BKICompanyConsent}, {"consents.bki_person", in.BKIPersonConsent}, {"consents.tax", in.TaxConsent}} {
		if c.consent == nil {
			continue
		}
		if strings.TrimSpace(c.consent.ID) == "" {
			add(c.field+".id", "required", "обязательное поле")
		}
		if !validDate(c.consent.ValidUntil) {
			add(c.field+".valid_until", "invalid_format", "дата в формате ГГГГ-ММ-ДД")
		}
		if c.consent.SignedAt != "" {
			if _, err := time.Parse(time.RFC3339, c.consent.SignedAt); err != nil {
				add(c.field+".signed_at", "invalid_format", "время в формате RFC 3339")
			}
		}
	}
	if in.BKIPersonConsent != nil && in.Director == nil {
		add("consents.bki_person", "invalid_value", "согласие руководителя без данных руководителя (director)")
	}
	if cs := in.TaxConsent; cs != nil {
		if len(cs.Scope) == 0 {
			add("consents.tax.scope", "required", "нужно хотя бы одно право: DECLARATIONS, ENS_BALANCE")
		}
		for i, sc := range cs.Scope {
			if sc != domain.TaxScopeDeclarations && sc != domain.TaxScopeENSBalance {
				add(fmt.Sprintf("consents.tax.scope[%d]", i), "invalid_value", "допустимые значения: DECLARATIONS, ENS_BALANCE")
			}
		}
	}

	if len(in.OtherBankBIKs) > maxBanks {
		add("other_bank_biks", "too_many", fmt.Sprintf("не больше %d банков", maxBanks))
	}
	for i, bik := range in.OtherBankBIKs {
		if !bikRe.MatchString(bik) {
			add(fmt.Sprintf("other_bank_biks[%d]", i), "invalid_format", "БИК — 9 цифр")
		}
	}

	for _, mp := range slices.Sorted(maps.Keys(in.MarketplaceTokens)) {
		tok := in.MarketplaceTokens[mp]
		if mp != domain.MarketplaceWildberries && mp != domain.MarketplaceOzon {
			add("marketplace_tokens."+mp, "invalid_value", "допустимые площадки: wildberries, ozon")
		} else if tok == "" {
			add("marketplace_tokens."+mp, "required", "пустой токен продавца")
		}
	}

	col := in.Collateral
	if len(col.CadastralNumbers)+len(col.VINs)+len(col.Serials) > maxCollateral {
		add("collateral", "too_many", fmt.Sprintf("не больше %d объектов залога", maxCollateral))
	}
	for i, n := range col.CadastralNumbers {
		if !cadastralRe.MatchString(n) {
			add(fmt.Sprintf("collateral.cadastral_numbers[%d]", i), "invalid_format", "кадастровый номер вида 50:21:0110203:845")
		}
	}
	for i, v := range col.VINs {
		if !vinRe.MatchString(v) {
			add(fmt.Sprintf("collateral.vins[%d]", i), "invalid_format", "VIN — 17 символов, без I, O, Q")
		}
	}
	for i, s := range col.Serials {
		if s == "" || len(s) > 64 {
			add(fmt.Sprintf("collateral.serials[%d]", i), "invalid_format", "заводской номер — от 1 до 64 символов")
		}
	}

	if in.POANumber != "" && !uuidRe.MatchString(in.POANumber) {
		add("poa_number", "invalid_format", "номер МЧД — UUID")
	}
	if in.RepresentativeINN != "" && !personINNRe.MatchString(in.RepresentativeINN) {
		add("representative_inn", "invalid_format", "ИНН физлица — 12 цифр")
	}
	return fields
}

func validDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}
