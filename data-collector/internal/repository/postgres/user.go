package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// Общая блокировка стартового заполнения пользователей и заявок.
	seedTestDataLockSQL = `SELECT pg_advisory_xact_lock(20261026, 1)`

	// Тестовый пользователь: id фиксирован, поэтому повторный старт обновляет строку, а не дублирует.
	// created_at — порядок в списке входа (как в service.TestUsers): обновляется и у уже заведённых.
	seedUser = `
INSERT INTO users (id, full_name, role, persona, created_at) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET full_name = EXCLUDED.full_name, role = EXCLUDED.role, persona = EXCLUDED.persona,
                               created_at = EXCLUDED.created_at`

	// Убранные из кода тестовые пользователи пропадают из списка входа (строки остаются: на них ссылаются заявки).
	retireTestUsers = `UPDATE users SET persona = NULL WHERE persona IS NOT NULL AND NOT (id = ANY($1))`

	// Компания с тем же ИНН (по ней уже подавали заявку) остаётся со своим id и собранными данными.
	// Любой конфликт — не ошибка: id ищем по ИНН ниже.
	seedCompany = `INSERT INTO companies (id, inn, type, name) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`

	selectCompanyByINN = `SELECT id, type FROM companies WHERE inn = $1`

	selectTestUsers = `
SELECT id, full_name, role, coalesce(persona, '') FROM users WHERE persona IS NOT NULL ORDER BY created_at, id`

	selectUser = `SELECT id, full_name, role, coalesce(persona, '') FROM users WHERE id = $1`

	selectUserCompanies = `
SELECT m.user_id, c.id, c.inn, c.name, m.role, m.can_sign
FROM company_members m JOIN companies c ON c.id = m.company_id
WHERE m.user_id = ANY($1)
ORDER BY c.name, c.id`
)

// UserRepository — пользователи кабинета и их доступ к компаниям (users, companies, company_members).
type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// SeedTestUsers одной транзакцией заводит тестовых пользователей, их компании и роли.
func (r *UserRepository) SeedTestUsers(ctx context.Context, users []domain.TestUserSeed) error {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, seedTestDataLockSQL); err != nil {
			return fmt.Errorf("lock test users: %w", err)
		}
		ids := make([]string, len(users))
		for i, u := range users {
			ids[i] = u.ID
		}
		if _, err := tx.Exec(ctx, retireTestUsers, ids); err != nil {
			return fmt.Errorf("retire test users: %w", err)
		}
		for i, u := range users {
			if _, err := tx.Exec(ctx, seedUser, u.ID, u.FullName, string(u.Role), u.Persona,
				base.Add(time.Duration(i)*time.Millisecond)); err != nil {
				return fmt.Errorf("seed user %s: %w", u.ID, err)
			}
			for _, c := range u.Companies {
				companyID, companyType, err := seedCompanyID(ctx, tx, c)
				if err != nil {
					return err
				}
				canSign := domain.CanSign(c.Role, domain.CompanyType(companyType))
				if _, err := tx.Exec(ctx, upsertMember, u.ID, companyID, string(c.Role), canSign); err != nil {
					return fmt.Errorf("seed member %s/%s: %w", u.ID, c.INN, err)
				}
			}
		}
		return nil
	})
}

// seedCompanyID — id компании тестового пользователя: заведённой сейчас или уже существующей с этим ИНН.
// Если предпочтительный id занят другой компанией (в коде сменили ИНН), заводит компанию с id по ИНН.
func seedCompanyID(ctx context.Context, tx pgx.Tx, c domain.TestCompanySeed) (string, string, error) {
	companyType := string(domain.CompanyTypeByINN(c.INN))
	for _, id := range []string{c.ID, "apl_inn_" + c.INN} {
		if _, err := tx.Exec(ctx, seedCompany, id, c.INN, companyType, c.Name); err != nil {
			return "", "", fmt.Errorf("seed company %s: %w", c.INN, err)
		}
		var gotID, gotType string
		err := tx.QueryRow(ctx, selectCompanyByINN, c.INN).Scan(&gotID, &gotType)
		if err == nil {
			return gotID, gotType, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("select company %s: %w", c.INN, err)
		}
	}
	return "", "", fmt.Errorf("seed company %s: id %s is taken by another company", c.INN, c.ID)
}

func (r *UserRepository) TestUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, selectTestUsers)
	if err != nil {
		return nil, fmt.Errorf("select test users: %w", err)
	}
	users, err := pgx.CollectRows(rows, scanUser)
	if err != nil {
		return nil, fmt.Errorf("scan test users: %w", err)
	}
	return users, r.withCompanies(ctx, users)
}

func (r *UserRepository) User(ctx context.Context, id string) (domain.User, bool, error) {
	rows, err := r.pool.Query(ctx, selectUser, id)
	if err != nil {
		return domain.User{}, false, fmt.Errorf("select user: %w", err)
	}
	user, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, fmt.Errorf("scan user: %w", err)
	}
	users := []domain.User{user}
	if err := r.withCompanies(ctx, users); err != nil {
		return domain.User{}, false, err
	}
	pending, err := r.pool.Query(ctx, `SELECT s.application_id FROM signings s
        JOIN applications a ON a.id = s.application_id
        JOIN company_members m ON m.company_id = a.company_id AND m.user_id = s.signer_id
        WHERE s.signer_id = $1 AND s.status = 'AWAITING_SIGNER' AND a.status = 'SIGNING' AND m.can_sign
        ORDER BY s.created_at, s.id`, id)
	if err != nil {
		return domain.User{}, false, err
	}
	users[0].PendingSignatures, err = pgx.CollectRows(pending, pgx.RowTo[string])
	if err != nil {
		return domain.User{}, false, err
	}
	return users[0], true, nil
}

func scanUser(row pgx.CollectableRow) (domain.User, error) {
	var (
		u    domain.User
		role string
	)
	err := row.Scan(&u.ID, &u.FullName, &role, &u.Persona)
	u.Role = domain.UserRole(role)
	return u, err
}

// withCompanies дописывает пользователям их компании одним запросом.
func (r *UserRepository) withCompanies(ctx context.Context, users []domain.User) error {
	if len(users) == 0 {
		return nil
	}
	ids := make([]string, len(users))
	index := make(map[string]int, len(users))
	for i, u := range users {
		ids[i] = u.ID
		index[u.ID] = i
	}
	rows, err := r.pool.Query(ctx, selectUserCompanies, ids)
	if err != nil {
		return fmt.Errorf("select user companies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			userID, role string
			c            domain.CompanyAccess
		)
		if err := rows.Scan(&userID, &c.CompanyID, &c.INN, &c.Name, &role, &c.CanSign); err != nil {
			return fmt.Errorf("scan user company: %w", err)
		}
		c.Role = domain.RepresentativeRole(role)
		if i, ok := index[userID]; ok {
			users[i].Companies = append(users[i].Companies, c)
		}
	}
	return rows.Err()
}
