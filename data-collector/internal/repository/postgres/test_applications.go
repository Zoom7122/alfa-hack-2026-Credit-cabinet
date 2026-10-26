package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// SeedTestApplications заполняет кабинет и очередь банка перед запуском HTTP API.
// Постоянные ULID-совместимые id позволяют дополнять набор, не меняя существующие
// заявки, ответы, назначения, предложения и даты при повторном старте.
func (r *ApplicationRepository) SeedTestApplications(ctx context.Context, files ...*storage.Local) (int, error) {
	return r.seedTestApplications(ctx, testApplications, time.Now().UTC(), files...)
}

func testApplicationID(id int) string { return fmt.Sprintf("app_%026d", id) }

func (r *ApplicationRepository) seedTestApplications(ctx context.Context, seeds []testApplication, now time.Time, files ...*storage.Local) (int, error) {
	inserted := 0
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Два одновременно запускаемых API не должны частично заполнить один набор.
		if _, err := tx.Exec(ctx, seedTestDataLockSQL); err != nil {
			return fmt.Errorf("lock test applications: %w", err)
		}
		for _, seed := range seeds {
			id := testApplicationID(seed.ID)
			var exists bool
			if err := tx.QueryRow(ctx, applicationExistsSQL, id).Scan(&exists); err != nil {
				return err
			}
			if exists {
				if err := upgradeTestApplicationForm(ctx, tx, seed, id, now); err != nil {
					return err
				}
				if err := r.enqueueLegacyTestApplication(ctx, tx, seed, id, now, files...); err != nil {
					return err
				}
				continue
			}
			if err := r.seedTestApplication(ctx, tx, seed, id, now, files...); err != nil {
				return fmt.Errorf("seed test application %s: %w", id, err)
			}
			inserted++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *ApplicationRepository) seedTestApplication(ctx context.Context, tx pgx.Tx, seed testApplication, id string, now time.Time, files ...*storage.Local) error {
	c, ok := testCompanies[seed.Company]
	if !ok {
		return fmt.Errorf("unknown test company %q", seed.Company)
	}
	companyID, companyType, err := seedCompanyID(ctx, tx, c.TestCompanySeed)
	if err != nil {
		return err
	}
	card, err := json.Marshal(toRegistryRecord(domain.CompanyCard{INN: c.INN, Type: domain.CompanyType(companyType),
		OGRN: c.OGRN, FullName: c.Name, ShortName: c.Name, RegistrationDate: c.RegistrationDate, DirectorName: c.DirectorName, Region: c.INN[:2]}))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE companies SET registry = coalesce(registry, $2::jsonb),
        ogrn = coalesce(ogrn, $3), registry_status = coalesce(registry_status, 'ACTIVE') WHERE id = $1`,
		companyID, card, nullString(c.OGRN)); err != nil {
		return err
	}
	userID, name, role := c.DirectorID, c.DirectorName, c.Role
	if seed.CreatorID != "" {
		userID, name = seed.CreatorID, seed.CreatorName
		if userID != c.DirectorID {
			role = domain.RoleAccountant
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, full_name, role) VALUES ($1, $2, 'CLIENT') ON CONFLICT (id) DO NOTHING`, userID, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, insertMemberKeepRole, userID, companyID, string(role), domain.CanSign(role, domain.CompanyType(companyType))); err != nil {
		return err
	}
	_, ok = domain.ProductByCode(seed.Product)
	if !ok {
		return fmt.Errorf("unknown test product %q", seed.Product)
	}
	created := now.Add(-24 * time.Hour)
	var submitted *time.Time
	if seed.Status != domain.ApplicationDraft {
		at := created.Add(time.Minute)
		submitted = &at
	}
	status := domain.ApplicationSubmitted
	if seed.Status == domain.ApplicationDraft {
		status = domain.ApplicationDraft
	}
	form, err := testApplicationForm(seed, id, now)
	if err != nil {
		return err
	}
	var initial map[string]json.RawMessage
	if err := json.Unmarshal(form, &initial); err != nil {
		return err
	}
	initial["draft"], err = draftFromForm(form)
	if err != nil {
		return err
	}
	form, err = json.Marshal(initial)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO applications
        (id, number, company_id, created_by, status, product_code, amount, term_months, form, assignee_id, deadline_at, submitted_at, created_at, updated_at)
        VALUES ($1, 'A-' || to_char($12::timestamptz, 'YYYY') || '-' || lpad(nextval('application_number_seq')::text, 6, '0'),
                $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, companyID, userID, string(status), seed.Product, seed.Amount, seed.Term, form,
		nullString(seed.AssigneeID), nil, submitted, created, now); err != nil {
		return err
	}
	if err := seedTestEvent(ctx, tx, id, "application.created", "DRAFT", userID, "Создана тестовая заявка", created); err != nil {
		return err
	}
	if seed.Status == domain.ApplicationDraft {
		return nil
	}
	if err := seedTestEvent(ctx, tx, id, "application.submitted", "SUBMITTED", userID, "Анкета отправлена", *submitted); err != nil {
		return err
	}
	return r.enqueueTestApplication(ctx, tx, id, userID, form, now, files...)
}

func seedTestEvent(ctx context.Context, tx pgx.Tx, appID, kind, status, actor, message string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO application_events (id, application_id, type, to_status, actor_id, message, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)`, "evt_"+ulid.MustNew(ulid.Timestamp(at), rand.Reader).String(),
		appID, kind, status, nullString(actor), message, at)
	return err
}
