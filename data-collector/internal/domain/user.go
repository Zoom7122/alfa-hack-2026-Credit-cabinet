package domain

import "strings"

// Пользователь кабинета (users) и его доступ к компаниям (company_members). Вход — мок-логином
// по контракту api/openapi.yaml: токен `mock.<user_id>`, паролей нет.

// UserRole — роль пользователя в системе: клиент или сотрудник банка.
type UserRole string

const (
	UserClient      UserRole = "CLIENT"
	UserUnderwriter UserRole = "UNDERWRITER"
)

// User — пользователь с компаниями, от имени которых он действует.
type User struct {
	ID                string // usr_…
	FullName          string
	Role              UserRole
	Persona           string // что демонстрирует тестовый пользователь; пусто — обычный пользователь
	Companies         []CompanyAccess
	PendingSignatures []string
}

// CompanyAccess — компания пользователя и его роль в ней (CompanyAccess в api/openapi.yaml).
type CompanyAccess struct {
	CompanyID string // apl_…
	INN       string
	Name      string
	Role      RepresentativeRole
	CanSign   bool
}

// MockTokenPrefix — токен мок-входа: `mock.<user_id>`, как в моках фронта и в api/openapi.yaml.
const MockTokenPrefix = "mock."

// MockToken — токен для пользователя.
func MockToken(userID string) string { return MockTokenPrefix + userID }

// UserIDFromToken — id пользователя из токена; false — токен не мок-входа.
func UserIDFromToken(token string) (string, bool) {
	id, ok := strings.CutPrefix(token, MockTokenPrefix)
	return id, ok && id != ""
}

// TestUserSeed — синтетический пользователь для /auth/test-users: заводится при старте API.
type TestUserSeed struct {
	ID        string
	FullName  string
	Role      UserRole
	Persona   string
	Companies []TestCompanySeed
}

// TestCompanySeed — компания тестового пользователя. ID — предпочтительный id: если компания
// с этим ИНН уже есть (заявку подавали раньше), остаётся её id.
type TestCompanySeed struct {
	ID   string
	INN  string
	Name string
	Role RepresentativeRole
}
