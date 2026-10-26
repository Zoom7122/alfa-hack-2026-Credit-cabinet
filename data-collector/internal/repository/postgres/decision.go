package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// заявка на оценку: VERIFYING без подписей в проверке (или ждали подписи дольше $1),
	// либо SCORING, брошенная упавшим worker'ом (дольше $2)
	claimScoringSQL = `
SELECT a.id, a.status FROM applications a
WHERE (a.status = 'VERIFYING' AND (a.updated_at < now() - $1::interval OR NOT EXISTS (
          SELECT 1 FROM documents d WHERE d.application_id = a.id AND d.deleted_at IS NULL AND d.signature_state = 'PENDING')))
   OR (a.status = 'SCORING' AND a.updated_at < now() - $2::interval)
ORDER BY a.updated_at
LIMIT 1
FOR UPDATE SKIP LOCKED`

	touchScoringSQL = `UPDATE applications SET status = 'SCORING', version = version + 1, updated_at = now() WHERE id = $1`

	selectDecisionInputSQL = `
SELECT a.form, coalesce(t.raw_collection_id, ''), coalesce(t.finished_at, a.updated_at),
       EXISTS (SELECT 1 FROM signature_reports r JOIN documents d ON d.id = r.document_id
               WHERE d.application_id = a.id AND r.integrity_status = 'INVALID')
FROM applications a
LEFT JOIN LATERAL (SELECT rc.id AS raw_collection_id, rc.finished_at FROM collection_tasks ct
                   JOIN raw_collections rc ON rc.task_id = ct.id
                   WHERE ct.application_id = a.id ORDER BY ct.created_at DESC LIMIT 1) t ON true
WHERE a.id = $1`

	// бухотчётность для поставщика Б: последний сбор по ИНН компании
	selectCompanyFNSSQL = `
SELECT r.fns FROM raw_collections r JOIN companies c ON c.inn = r.inn
WHERE c.id = $1 ORDER BY r.created_at DESC LIMIT 1`

	insertDecisionSQL = `
INSERT INTO decisions (id, application_id, grade, score, max_amount, auto_outcome, details, client_reasons)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	insertOfferSQL = `
INSERT INTO offers (id, application_id, kind, amount, term_months, rate, monthly_payment, terms, valid_until)
VALUES ($1, $2, 'REQUESTED', $3, $4, $5, $6, $7, $8)`

	insertInfoRequestSQL = `
INSERT INTO info_requests (id, application_id, required, question, due_at) VALUES ($1, $2, $3, $4, $5)`

	// вопросы прошлого раунда, которые остались без ответа, — больше не нужны
	cancelOpenQuestionsSQL = `UPDATE info_requests SET status = 'CANCELLED' WHERE application_id = $1 AND status = 'OPEN'`

	selectAnsweredCodesSQL = `
SELECT DISTINCT question ->> 'code' FROM info_requests
WHERE application_id = $1 AND status = 'ANSWERED' AND question ->> 'code' IS NOT NULL`

	finishScoringSQL = `
UPDATE applications SET status = $2, deadline_at = $3, version = version + 1, updated_at = now()
WHERE id = $1 AND status = 'SCORING'`

	creditEventExistsSQL = `SELECT EXISTS (SELECT 1 FROM application_events WHERE type LIKE 'credit_engine.%' AND payload ->> 'event_id' = $1)`
	applicationExistsSQL = `SELECT EXISTS (SELECT 1 FROM applications WHERE id = $1)`
)

// DecisionRepository — оценка заявки credit-engine: взять заявку, собрать вход, записать итог.
type DecisionRepository struct {
	pool *pgxpool.Pool
	apps *ApplicationRepository
}

func NewDecisionRepository(pool *pgxpool.Pool) *DecisionRepository {
	return &DecisionRepository{pool: pool, apps: &ApplicationRepository{pool: pool}}
}

// ClaimNext берёт заявку на оценку и переводит её в SCORING (событие scoring.started).
// false — оценивать нечего. signatureWait — сколько ждать проверку подписей, stale — когда считать
// оценку брошенной и начинать заново.
func (r *DecisionRepository) ClaimNext(ctx context.Context, signatureWait, stale time.Duration) (string, bool, error) {
	var id string
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, claimScoringSQL, signatureWait.String(), stale.String()).Scan(&id, &status)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, touchScoringSQL, id); err != nil {
			return fmt.Errorf("move to scoring: %w", err)
		}
		if status == string(domain.ApplicationScoring) {
			return nil // повтор брошенной оценки
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: id, Type: "scoring.started", From: domain.ApplicationVerifying,
			To: domain.ApplicationScoring})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("claim scoring: %w", err)
	}
	return id, true, nil
}

// Input — всё для запроса в движок: заявка, анкета, последний сбор, признак подделки документа.
func (r *DecisionRepository) Input(ctx context.Context, appID string, now time.Time) (decision.Input, error) {
	app, ok, err := r.apps.Get(ctx, appID)
	if err != nil {
		return decision.Input{}, err
	}
	if !ok {
		return decision.Input{}, fmt.Errorf("application %s is not found", appID)
	}
	var (
		form         []byte
		rawID        string
		finished     time.Time
		forged       bool
		formDecoded  formRecord
		systemsQuery = make([]string, 0, len(domain.Systems))
	)
	if err := r.pool.QueryRow(ctx, selectDecisionInputSQL, appID).Scan(&form, &rawID, &finished, &forged); err != nil {
		return decision.Input{}, fmt.Errorf("select decision input: %w", err)
	}
	if err := json.Unmarshal(form, &formDecoded); err != nil {
		return decision.Input{}, fmt.Errorf("decode form: %w", err)
	}
	q := formDecoded.Questionnaire
	in := decision.Input{Application: app, Age: q.Age, MaritalStatus: domain.MaritalStatus(q.MaritalStatus),
		Children: q.ChildrenCount, Scoring: formDecoded.Scoring.toDomain(), Collect: fromRequestRecord(q.Collect, nil),
		Systems: map[domain.System]json.RawMessage{}, FactsDate: finished.UTC(), Now: now, ForgedDocument: forged}
	if rawID == "" {
		return in, nil // сбора нет — движок попросит данные
	}
	for _, sys := range domain.Systems {
		systemsQuery = append(systemsQuery, SystemColumns[sys])
	}
	values := make([][]byte, len(domain.Systems))
	dest := make([]any, len(values))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := r.pool.QueryRow(ctx, "SELECT "+strings.Join(systemsQuery, ", ")+" FROM raw_collections WHERE id = $1", rawID).
		Scan(dest...); err != nil {
		return decision.Input{}, fmt.Errorf("select raw collection: %w", err)
	}
	for i, sys := range domain.Systems {
		if values[i] != nil {
			in.Systems[sys] = values[i]
		}
	}
	return in, nil
}

// SaveOutcome одной транзакцией записывает итог оценки: решение, предложение (если одобрено),
// вопросы клиенту, новый статус заявки (у ручного рассмотрения — срок) и событие в журнале.
// details — ответ движка и находки для досье.
func (r *DecisionRepository) SaveOutcome(ctx context.Context, appID string, o decision.Outcome, details any,
	deadline *time.Time, offerValidUntil time.Time) error {
	questionsDue := time.Now().AddDate(0, 0, 5) // срок ответа на вопросы — как у вопросов андеррайтера
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode decision details: %w", err)
	}
	var reasons any
	if len(o.ClientReasons) > 0 {
		b, err := json.Marshal(o.ClientReasons)
		if err != nil {
			return fmt.Errorf("encode client reasons: %w", err)
		}
		reasons = string(b)
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var maxAmount any
		if o.Offer != nil {
			maxAmount = o.Offer.AmountRUB
		}
		if _, err := tx.Exec(ctx, insertDecisionSQL, "dcs_"+ulid.Make().String(), appID, nullString(o.Grade), o.Score,
			maxAmount, o.AutoOutcome, string(detailsJSON), reasons); err != nil {
			return fmt.Errorf("insert decision: %w", err)
		}
		if o.Offer != nil && o.Status == domain.ApplicationOffered {
			terms, _ := json.Marshal(map[string]any{"confirmed": o.Offer.Confirmed, "source": "credit-engine"})
			if _, err := tx.Exec(ctx, insertOfferSQL, "ofr_"+ulid.Make().String(), appID, o.Offer.AmountRUB, o.Offer.TermMonths,
				o.Offer.AnnualRate, o.Offer.MonthlyPayment, string(terms), offerValidUntil); err != nil {
				return fmt.Errorf("insert offer: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, cancelOpenQuestionsSQL, appID); err != nil {
			return fmt.Errorf("cancel open questions: %w", err)
		}
		for _, q := range o.Questions {
			question := map[string]any{"code": q.Code, "title": q.Title, "description": q.Description,
				"fields": q.Fields, "source": "credit-engine", "input": q.Answer}
			if len(q.Answer.DocTypes) > 0 {
				question["documents"] = map[string]any{"required": true, "accepted_types": q.Answer.DocTypes}
			}
			b, _ := json.Marshal(question)
			if _, err := tx.Exec(ctx, insertInfoRequestSQL, "ir_"+ulid.Make().String(), appID, q.Required, string(b), questionsDue); err != nil {
				return fmt.Errorf("insert info request: %w", err)
			}
		}
		tag, err := tx.Exec(ctx, finishScoringSQL, appID, string(o.Status), deadline)
		if err != nil {
			return fmt.Errorf("finish scoring: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("application %s is not in SCORING", appID)
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "scoring.completed", From: domain.ApplicationScoring,
			To: o.Status, Message: o.Message, Payload: map[string]any{"auto_outcome": o.AutoOutcome, "grade": o.Grade}})
	})
}

// CompanyFNS — данные ФНС последнего сбора по компании; false — сборов нет.
func (r *DecisionRepository) CompanyFNS(ctx context.Context, companyID string) (json.RawMessage, bool, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, selectCompanyFNSSQL, companyID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("select company fns: %w", err)
	}
	return raw, true, nil
}

// SaveCreditEvent — событие МС2 → МС3 в журнал заявки (служебное), повтор с тем же event_id не пишется.
// false — заявки нет или событие уже было.
func (r *DecisionRepository) SaveCreditEvent(ctx context.Context, ev decision.CreditEvent) (bool, error) {
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var exists, appExists bool
		if err := tx.QueryRow(ctx, creditEventExistsSQL, ev.EventID).Scan(&exists); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, applicationExistsSQL, ev.ApplicationID).Scan(&appExists); err != nil {
			return err
		}
		if exists || !appExists {
			return nil
		}
		saved = true
		return insertEvent(ctx, tx, appEvent{ApplicationID: ev.ApplicationID, Type: "credit_engine." + strings.ToLower(ev.Kind),
			Payload: ev})
	})
	if err != nil {
		return false, fmt.Errorf("save credit event: %w", err)
	}
	return saved, nil
}

// AnsweredCodes — темы вопросов, на которые клиент уже ответил.
func (r *DecisionRepository) AnsweredCodes(ctx context.Context, appID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, selectAnsweredCodesSQL, appID)
	if err != nil {
		return nil, fmt.Errorf("select answered codes: %w", err)
	}
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(codes))
	for _, c := range codes {
		out[c] = true
	}
	return out, nil
}
