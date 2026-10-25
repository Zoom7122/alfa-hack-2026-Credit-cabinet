package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// UserRepository — пользователи и их компании (users, companies, company_members). Объявлено у потребителя.
type UserRepository interface {
	// SeedTestUsers заводит тестовых пользователей, их компании и роли; повторный запуск ничего не дублирует.
	SeedTestUsers(ctx context.Context, users []domain.TestUserSeed) error
	// TestUsers — пользователи с persona, в порядке заведения.
	TestUsers(ctx context.Context) ([]domain.User, error)
	// User — пользователь с компаниями. false — такого нет.
	User(ctx context.Context, id string) (domain.User, bool, error)
}

// AuthService — мок-вход по контракту api/openapi.yaml: список тестовых пользователей, вход по id,
// текущий пользователь по токену `mock.<user_id>`. Паролей нет — это стенд, поэтому войти можно только
// под тестовым пользователем (с persona): под пользователем, заведённым анонимной регистрацией, нельзя.
type AuthService struct {
	users UserRepository
	// заведённые компании тестовых пользователей (ИНН) — первыми в профиле
	primary map[string][]string
}

func NewAuthService(users UserRepository) *AuthService {
	s := &AuthService{users: users, primary: map[string][]string{}}
	s.remember(TestUsers) // и без заведения при старте (SEED_TEST_USERS=false) порядок тот же
	return s
}

// Seed заводит тестовых пользователей (TestUsers).
func (s *AuthService) Seed(ctx context.Context, users []domain.TestUserSeed) error {
	if err := s.users.SeedTestUsers(ctx, users); err != nil {
		return fmt.Errorf("seed test users: %w", err)
	}
	s.remember(users)
	return nil
}

func (s *AuthService) remember(users []domain.TestUserSeed) {
	for _, u := range users {
		inns := make([]string, 0, len(u.Companies))
		for _, c := range u.Companies {
			inns = append(inns, c.INN)
		}
		s.primary[u.ID] = inns
	}
}

// TestUsers — кого можно выбрать на экране входа.
func (s *AuthService) TestUsers(ctx context.Context) ([]domain.User, error) {
	users, err := s.users.TestUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list test users: %w", err)
	}
	for i := range users {
		s.order(&users[i])
	}
	return users, nil
}

// Login — токен и пользователь. ErrNotFound — пользователя с таким id нет.
func (s *AuthService) Login(ctx context.Context, userID string) (string, domain.User, error) {
	user, err := s.find(ctx, userID)
	if err != nil {
		return "", domain.User{}, err
	}
	return domain.MockToken(user.ID), user, nil
}

// Me — пользователь по токену. ErrUnauthorized — токен не мок-входа или пользователя нет.
func (s *AuthService) Me(ctx context.Context, token string) (domain.User, error) {
	id, ok := domain.UserIDFromToken(token)
	if !ok {
		return domain.User{}, ErrUnauthorized
	}
	user, err := s.find(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return domain.User{}, ErrUnauthorized
		}
		return domain.User{}, err
	}
	return user, nil
}

func (s *AuthService) find(ctx context.Context, id string) (domain.User, error) {
	user, ok, err := s.users.User(ctx, id)
	if err != nil {
		return domain.User{}, fmt.Errorf("get user: %w", err)
	}
	if !ok || user.Persona == "" {
		return domain.User{}, ErrNotFound
	}
	s.order(&user)
	return user, nil
}

// order — сначала компании, заведённые тестовому пользователю (в их порядке), потом добавленные регистрацией
// по имени: кабинет берёт первую компанию как основную, и она не должна меняться от регистрации на чужой ИНН.
func (s *AuthService) order(u *domain.User) {
	primary := s.primary[u.ID]
	rank := func(c domain.CompanyAccess) int {
		if i := slices.Index(primary, c.INN); i >= 0 {
			return i
		}
		return len(primary)
	}
	slices.SortStableFunc(u.Companies, func(a, b domain.CompanyAccess) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.CompanyID, b.CompanyID))
	})
}
