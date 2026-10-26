package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// SeedTestApplications заполняет кабинет и очередь банка перед запуском HTTP API.
// Постоянные ULID-совместимые id позволяют дополнять набор, не меняя существующие
// заявки, ответы, назначения, предложения и даты при повторном старте.
func (r *ApplicationRepository) SeedTestApplications(ctx context.Context) (int, error) {
	return r.seedTestApplications(ctx, testApplications, time.Now().UTC())
}

func testApplicationID(id int) string { return fmt.Sprintf("app_%026d", id) }

func (r *ApplicationRepository) seedTestApplications(ctx context.Context, seeds []testApplication, now time.Time) (int, error) {
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
				continue
			}
			if err := r.seedTestApplication(ctx, tx, seed, id, now); err != nil {
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

func (r *ApplicationRepository) seedTestApplication(ctx context.Context, tx pgx.Tx, seed testApplication, id string, now time.Time) error {
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
	product, ok := domain.ProductByCode(seed.Product)
	if !ok {
		return fmt.Errorf("unknown test product %q", seed.Product)
	}
	created := now.Add(-24 * time.Hour)
	var submitted *time.Time
	if seed.Status != domain.ApplicationDraft {
		at := created.Add(time.Minute)
		submitted = &at
	}
	var deadline any
	if seed.Status == domain.ApplicationManualReview {
		deadline = now.Add(4 * time.Hour)
	}
	form, err := json.Marshal(map[string]any{
		"product_code": seed.Product,
		"requested":    map[string]any{"amount": seed.Amount, "term_months": seed.Term},
		"purpose":      map[string]any{"code": product.Purposes[0], "description": "Тестовая заявка для демонстрации кабинета"},
		"questionnaire": map[string]any{"id": strings.Replace(id, "app_", "qst_", 1), "age": 42,
			"marital_status": "MARRIED", "children_count": 2, "created_at": created,
			"collect": map[string]any{"inn": c.INN}},
	})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO applications
        (id, number, company_id, created_by, status, product_code, amount, term_months, form, assignee_id, deadline_at, submitted_at, created_at, updated_at)
        VALUES ($1, 'A-' || to_char($12::timestamptz, 'YYYY') || '-' || lpad(nextval('application_number_seq')::text, 6, '0'),
                $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, companyID, userID, string(seed.Status), seed.Product, seed.Amount, seed.Term, form,
		nullString(seed.AssigneeID), deadline, submitted, created, now); err != nil {
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
	if seed.Status == domain.ApplicationSubmitted {
		task := domain.CollectionTask{ID: strings.Replace(id, "app_", "tsk_", 1), ApplicationID: id,
			QuestionnaireID: strings.Replace(id, "app_", "qst_", 1), Input: domain.CollectInput{INN: c.INN}, CreatedAt: now}
		skipped := domain.SkippedSystems(task.Input)
		for _, system := range domain.Systems {
			if _, ok := skipped[system]; ok {
				task.Skipped = append(task.Skipped, system)
			}
		}
		return insertTask(ctx, tx, task, r.tasksTopic)
	}
	return seedTestOutcome(ctx, tx, seed, id, now)
}

func seedTestEvent(ctx context.Context, tx pgx.Tx, appID, kind, status, actor, message string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO application_events (id, application_id, type, to_status, actor_id, message, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)`, "evt_"+ulid.MustNew(ulid.Timestamp(at), rand.Reader).String(),
		appID, kind, status, nullString(actor), message, at)
	return err
}

func seedTestOutcome(ctx context.Context, tx pgx.Tx, seed testApplication, id string, now time.Time) error {
	outcome, engineStatus, grade, score, message := "MANUAL_REVIEW", "UNDER_REVIEW", "B", 680, "Заявку рассматривает кредитный специалист"
	amount, term, rate := seed.Amount, seed.Term, 20.75
	switch seed.Status {
	case domain.ApplicationOffered:
		outcome, engineStatus, grade, score, message = "APPROVED", "APPROVED", "A", 820, "Банк одобрил кредит"
		if seed.Company == "north" {
			outcome, amount, term = "APPROVED_WITH_CHANGES", 2_030_000, 36
		}
	case domain.ApplicationRejected:
		outcome, engineStatus, grade, score, message = "REJECTED", "REJECTED", "D", 410, "Банк отказал в кредите"
	case domain.ApplicationWaitingForInfo:
		outcome, engineStatus, message = "NEED_INFO", "DATA_REQUIRED", "Банку нужны дополнительные сведения"
	}
	proposal := decision.Offer{AmountRUB: amount, TermMonths: term, AnnualRate: rate,
		MonthlyPayment: decision.Annuity(amount, rate, term), Confirmed: seed.Status == domain.ApplicationOffered}
	engineReasons := []map[string]string{}
	if seed.Reason != "" {
		engineReasons = append(engineReasons, map[string]string{"code": "DEMO_SCENARIO", "message": seed.Reason})
	}
	detail := map[string]any{"test_data": true, "engine": map[string]any{"status": engineStatus, "reasons": engineReasons}}
	var maxAmount *int64
	if seed.Status == domain.ApplicationOffered || seed.Status == domain.ApplicationManualReview {
		detail["proposed_offer"], maxAmount = proposal, &amount
	}
	details, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	reasons := []domain.ClientReason{}
	if seed.Status == domain.ApplicationRejected {
		reasons = append(reasons, domain.ClientReason{Code: "DEMO_REJECTION", Title: seed.Reason})
	}
	clientReasons, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO decisions (id, application_id, grade, score, max_amount, auto_outcome, details, client_reasons, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, strings.Replace(id, "app_", "dcs_", 1), id, grade, score, maxAmount,
		outcome, details, clientReasons, now); err != nil {
		return err
	}
	if seed.Status == domain.ApplicationOffered {
		product, _ := domain.ProductByCode(seed.Product)
		collateral := []string{}
		if product.CollateralAbove != nil && amount > *product.CollateralAbove {
			collateral = product.CollateralTypes
		}
		terms, err := json.Marshal(map[string]any{"product_code": seed.Product, "recommended": true,
			"collateral_required": collateral, "guarantee_required": product.GuaranteeRequired, "conditions": []string{}})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO offers (id, application_id, kind, amount, term_months, rate, monthly_payment, terms, valid_until)
            VALUES ($1, $2, 'REQUESTED', $3, $4, $5, $6, $7, $8)`, strings.Replace(id, "app_", "ofr_", 1), id,
			amount, term, rate, proposal.MonthlyPayment, terms, now.AddDate(0, 0, 14)); err != nil {
			return err
		}
	}
	if seed.Status == domain.ApplicationWaitingForInfo {
		questions := []map[string]any{{"title": seed.Reason, "input": map[string]string{"type": "TEXT"}}}
		if seed.Company == "smirnova" {
			questions = []map[string]any{
				{"title": "Свободные деньги", "description": "Уточните остаток свободных денег на счетах",
					"input": map[string]string{"type": "MONEY", "scoring_field": "free_cash"}},
				{"title": "Выписка по счетам за 12 месяцев", "input": map[string]string{"type": "NONE"},
					"documents": map[string]any{"required": true, "accepted_types": []string{"BANK_STATEMENT"}}},
			}
		}
		for i, question := range questions {
			// После ответа демонстрационная заявка вернётся специалисту, а не в скоринг без сырых данных.
			question["source"] = "underwriter"
			body, err := json.Marshal(question)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO info_requests (id, application_id, required, question, created_at, due_at)
                VALUES ($1, $2, true, $3, $4, $5)`, fmt.Sprintf("ir_%s_%d", strings.TrimPrefix(id, "app_"), i), id,
				body, now, now.AddDate(0, 0, 5)); err != nil {
				return err
			}
		}
	}
	return seedTestEvent(ctx, tx, id, "decision.ready", string(seed.Status), "", message, now)
}
