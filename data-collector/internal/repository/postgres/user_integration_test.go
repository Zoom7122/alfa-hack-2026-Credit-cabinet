//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

func TestUserRepositorySeedAndRead(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewUserRepository(pool)

	// уникальные строки теста: ИНН и id не пересекаются с настоящими тестовыми пользователями
	suffix := time.Now().UnixNano() % 1_000_000
	inn := fmt.Sprintf("88%08d", suffix)
	director := fmt.Sprintf("usr_it_director_%d", suffix)
	accountant := fmt.Sprintf("usr_it_accountant_%d", suffix)
	companyID := fmt.Sprintf("apl_it_%d", suffix)
	t.Cleanup(func() {
		// заведение снимает persona с тех, кого нет в списке, — возвращаем настоящих тестовых пользователей
		if err := repo.SeedTestUsers(ctx, service.TestUsers); err != nil {
			t.Errorf("restore test users: %v", err)
		}
		for _, q := range []struct {
			sql string
			arg any
		}{
			{"DELETE FROM application_events WHERE application_id IN (SELECT id FROM applications WHERE company_id IN (SELECT id FROM companies WHERE inn = $1))", inn},
			{"DELETE FROM applications WHERE company_id IN (SELECT id FROM companies WHERE inn = $1)", inn},
			{"DELETE FROM company_members WHERE company_id IN (SELECT id FROM companies WHERE inn = $1)", inn},
			{"DELETE FROM companies WHERE inn = $1", inn},
			{"DELETE FROM users WHERE id = ANY($1)", []string{director, accountant}},
		} {
			if _, err := pool.Exec(ctx, q.sql, q.arg); err != nil {
				t.Errorf("cleanup %q: %v", q.sql, err)
			}
		}
	})

	seeds := []domain.TestUserSeed{
		{ID: director, FullName: "Тестов Директор", Role: domain.UserClient, Persona: "директор",
			Companies: []domain.TestCompanySeed{{ID: companyID, INN: inn, Name: "ООО «Тест»", Role: domain.RoleDirector}}},
		{ID: accountant, FullName: "Тестова Бухгалтер", Role: domain.UserClient, Persona: "бухгалтер",
			Companies: []domain.TestCompanySeed{{ID: "apl_ignored", INN: inn, Name: "ООО «Тест»", Role: domain.RoleAccountant}}},
	}
	// дважды — повторный старт API ничего не дублирует
	for range 2 {
		if err := repo.SeedTestUsers(ctx, seeds); err != nil {
			t.Fatal(err)
		}
	}

	user, ok, err := repo.User(ctx, director)
	if err != nil || !ok {
		t.Fatalf("user: ok=%v err=%v", ok, err)
	}
	if user.FullName != "Тестов Директор" || user.Role != domain.UserClient || user.Persona != "директор" {
		t.Errorf("user: %+v", user)
	}
	if len(user.Companies) != 1 || user.Companies[0].CompanyID != companyID || !user.Companies[0].CanSign {
		t.Errorf("director companies: %+v", user.Companies)
	}
	// компания с тем же ИНН одна: второй пользователь привязан к ней, а не к apl_ignored
	acc, _, err := repo.User(ctx, accountant)
	if err != nil {
		t.Fatal(err)
	}
	if len(acc.Companies) != 1 || acc.Companies[0].CompanyID != companyID || acc.Companies[0].CanSign {
		t.Errorf("accountant companies: %+v", acc.Companies)
	}

	all, err := repo.TestUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, u := range all {
		if u.ID == director || u.ID == accountant {
			found++
		}
	}
	if found != 2 {
		t.Errorf("test users: found %d of 2", found)
	}

	if _, ok, err := repo.User(ctx, "usr_it_nobody"); ok || err != nil {
		t.Errorf("unknown user: ok=%v err=%v", ok, err)
	}

	// регистрация от имени вошедшего: тот же пользователь, роль в компании не переписывается
	apps := NewApplicationRepository(pool, "it-tasks", "it-documents")
	register := func(role domain.RepresentativeRole) domain.Application {
		t.Helper()
		app, err := apps.Register(ctx, domain.Registration{
			UserID: accountant, ExistingUser: true, CompanyID: "apl_it_unused", ApplicationID: fmt.Sprintf("app_it_%d_%s", suffix, role),
			Representative: domain.Representative{FullName: "Тестова Бухгалтер", Role: role, CanSign: role == domain.RoleDirector},
			Company:        domain.Company{INN: inn, Type: domain.CompanyLegalEntity, Name: "ООО «Тест»"},
			CreatedAt:      time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	first := register(domain.RoleAccountant)
	again := register(domain.RoleDirector)
	if again.ID != first.ID {
		t.Errorf("draft is not reused: %s, then %s", first.ID, again.ID)
	}
	if again.Representative.UserID != accountant || again.Representative.Role != domain.RoleAccountant || again.Representative.CanSign {
		t.Errorf("role is overwritten: %+v", again.Representative)
	}
}
