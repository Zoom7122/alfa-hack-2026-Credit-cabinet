package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	maxFullNameRunes    = 200
	maxCompanyNameRunes = 500
)

// ApplicationRepository — заявки вместе с компанией, представителем и документами
// (applications, companies, users, company_members, documents). Объявлено у потребителя.
type ApplicationRepository interface {
	// Register одной транзакцией пишет или находит пользователя (по ИНН) и компанию (по ИНН),
	// их связь и черновик заявки (черновик этого пользователя по этой компании переиспользуется).
	Register(ctx context.Context, reg domain.Registration) (domain.Application, error)
	// Get возвращает заявку с компанией, представителем и документами. false — заявки нет.
	Get(ctx context.Context, id string) (domain.Application, bool, error)
	// Submit одной транзакцией отправляет заявку: документы, анкета, задача на сбор, события.
	// domain.ErrApplicationNotDraft — заявка уже не черновик.
	Submit(ctx context.Context, sub domain.Submission) error
}

// RegisterInput — кто оформляет заявку и за какую компанию.
type RegisterInput struct {
	FullName    string
	PersonINN   string // ИНН физлица; необязательно
	Role        domain.RepresentativeRole
	CompanyINN  string
	CompanyName string // необязательно: после сбора имя заменится данными ЕГРЮЛ
}

// ApplicationService регистрирует заявителя (пользователь, компания, черновик заявки) и отдаёт заявки.
type ApplicationService struct {
	apps  ApplicationRepository
	now   func() time.Time
	newID func(prefix string) string
}

// ApplicationOption настраивает ApplicationService (используется в тестах).
type ApplicationOption func(*ApplicationService)

// WithApplicationClock подменяет источник времени.
func WithApplicationClock(now func() time.Time) ApplicationOption {
	return func(s *ApplicationService) { s.now = now }
}

// WithApplicationIDGenerator подменяет генератор идентификаторов: prefix — usr_, apl_, app_.
func WithApplicationIDGenerator(newID func(prefix string) string) ApplicationOption {
	return func(s *ApplicationService) { s.newID = newID }
}

func NewApplicationService(apps ApplicationRepository, opts ...ApplicationOption) *ApplicationService {
	s := &ApplicationService{
		apps:  apps,
		now:   time.Now,
		newID: func(prefix string) string { return prefix + ulid.MustNew(ulid.Now(), rand.Reader).String() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register проверяет данные и одной транзакцией создаёт (или находит) пользователя, компанию,
// их связь и черновик заявки. Нарушения возвращаются как *ValidationError — все сразу.
func (s *ApplicationService) Register(ctx context.Context, in RegisterInput) (domain.Application, error) {
	in = normalizeRegister(in)
	if fields := validateRegister(in); len(fields) > 0 {
		return domain.Application{}, &ValidationError{Fields: fields}
	}

	companyType := domain.CompanyTypeByINN(in.CompanyINN)
	name := in.CompanyName
	if name == "" {
		name = "ИНН " + in.CompanyINN // до сбора: потом имя заменится данными ЕГРЮЛ
	}
	app, err := s.apps.Register(ctx, domain.Registration{
		UserID:        s.newID("usr_"),
		CompanyID:     s.newID("apl_"),
		ApplicationID: s.newID("app_"),
		Representative: domain.Representative{FullName: in.FullName, INN: in.PersonINN, Role: in.Role,
			CanSign: domain.CanSign(in.Role, companyType)},
		Company:   domain.Company{INN: in.CompanyINN, Type: companyType, Name: name},
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return domain.Application{}, fmt.Errorf("register applicant: %w", err)
	}
	return app, nil
}

// Get возвращает заявку. ErrNotFound — заявки с таким id нет.
func (s *ApplicationService) Get(ctx context.Context, id string) (domain.Application, error) {
	app, ok, err := s.apps.Get(ctx, id)
	if err != nil {
		return domain.Application{}, fmt.Errorf("get application: %w", err)
	}
	if !ok {
		return domain.Application{}, ErrNotFound
	}
	return app, nil
}

func normalizeRegister(in RegisterInput) RegisterInput {
	in.FullName = strings.Join(strings.Fields(in.FullName), " ")
	in.PersonINN = strings.TrimSpace(in.PersonINN)
	in.Role = domain.RepresentativeRole(strings.ToUpper(strings.TrimSpace(string(in.Role))))
	in.CompanyINN = strings.TrimSpace(in.CompanyINN)
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	return in
}

func validateRegister(in RegisterInput) []FieldError {
	var fields []FieldError
	add := func(field, code, msg string) {
		fields = append(fields, FieldError{Field: field, Code: code, Message: msg})
	}

	switch {
	case in.FullName == "":
		add("representative.full_name", "required", "обязательное поле")
	case utf8.RuneCountInString(in.FullName) > maxFullNameRunes:
		add("representative.full_name", "too_long", fmt.Sprintf("не длиннее %d символов", maxFullNameRunes))
	}
	if in.PersonINN != "" && !personINNRe.MatchString(in.PersonINN) {
		add("representative.inn", "invalid_format", "ИНН физлица — 12 цифр")
	}
	if !in.Role.Valid() {
		add("representative.role", "invalid_value", "допустимые значения: OWNER, DIRECTOR, CFO, ACCOUNTANT, "+
			"FINANCE_EMPLOYEE, BANKING_EMPLOYEE, AUTHORIZED_EMPLOYEE")
	}
	if !innRe.MatchString(in.CompanyINN) {
		add("company.inn", "invalid_format", "ИНН должен состоять из 10 (ЮЛ) или 12 (ИП) цифр")
	}
	if utf8.RuneCountInString(in.CompanyName) > maxCompanyNameRunes {
		add("company.name", "too_long", fmt.Sprintf("не длиннее %d символов", maxCompanyNameRunes))
	}
	return fields
}
