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
	// очередь: ручное рассмотрение — по сроку, остальное — свежие сверху; черновики не показываем
	selectQueueSQL = `
SELECT a.id, a.number, a.status, a.version, coalesce(a.product_code, ''), a.amount, a.term_months,
       c.id, c.inn, c.type, c.name,
       coalesce(d.grade, ''), d.score, coalesce(d.details -> 'engine' -> 'reasons', '[]'::jsonb),
       coalesce(rc.sources, '[]'::jsonb),
       coalesce(u.id, ''), coalesce(u.full_name, ''),
       coalesce(a.submitted_at, a.created_at), a.updated_at, a.deadline_at
FROM applications a
JOIN companies c ON c.id = a.company_id
LEFT JOIN users u ON u.id = a.assignee_id
LEFT JOIN LATERAL (SELECT grade, score, details FROM decisions WHERE application_id = a.id
                   ORDER BY created_at DESC, id DESC LIMIT 1) d ON true
LEFT JOIN LATERAL (SELECT r.sources FROM collection_tasks t JOIN raw_collections r ON r.task_id = t.id
                   WHERE t.application_id = a.id ORDER BY t.created_at DESC LIMIT 1) rc ON true
WHERE a.status <> 'DRAFT'
  AND (cardinality($1::text[]) = 0 OR a.status = ANY($1))
  AND ($2 = '' OR ($2 = 'none' AND a.assignee_id IS NULL) OR a.assignee_id = $2)
  AND ($5 = '' OR a.company_id = $5)
  AND ($6 = '' OR a.id = $6)
ORDER BY (a.status = 'MANUAL_REVIEW') DESC, a.deadline_at ASC NULLS LAST, a.updated_at DESC, a.id
LIMIT $3 OFFSET $4`

	selectQueueCountersSQL = `
SELECT count(*) FILTER (WHERE status = 'MANUAL_REVIEW'),
       count(*) FILTER (WHERE status = 'MANUAL_REVIEW' AND assignee_id = $1),
       count(*) FILTER (WHERE status = 'MANUAL_REVIEW' AND deadline_at < date_trunc('day', now()) + interval '1 day'),
       count(*) FILTER (WHERE status = 'WAITING_FOR_INFO')
FROM applications`

	selectDossierHeadSQL = `
SELECT a.version, coalesce(a.assignee_id, ''), coalesce(u.full_name, ''), a.deadline_at, c.registry
FROM applications a JOIN companies c ON c.id = a.company_id LEFT JOIN users u ON u.id = a.assignee_id
WHERE a.id = $1`

	selectLatestRawSQL = `
SELECT r.id, r.sources, r.finished_at FROM collection_tasks t JOIN raw_collections r ON r.task_id = t.id
WHERE t.application_id = $1 ORDER BY t.created_at DESC LIMIT 1`

	selectLatestDecisionSQL = `
SELECT id, coalesce(grade, ''), score, max_amount, coalesce(auto_outcome, ''), coalesce(outcome, ''),
       coalesce(decided_by, ''), details, client_reasons, coalesce(comment, ''), created_at
FROM decisions WHERE application_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`

	selectOffersSQL = `
SELECT id, kind, status, amount, term_months, rate, monthly_payment, terms, valid_until
FROM offers WHERE application_id = $1 ORDER BY id`

	selectInfoRequestsSQL = `
SELECT id, status, required, question, answer, created_at, answered_at, due_at
FROM info_requests WHERE application_id = $1 ORDER BY created_at, id`

	selectStaffSQL = `SELECT id, full_name FROM users WHERE id = ANY($1) AND role = 'UNDERWRITER'`

	lockReviewSQL = `SELECT status, version, coalesce(assignee_id, '') FROM applications WHERE id = $1 FOR UPDATE`

	assignSQL = `UPDATE applications SET assignee_id = $2, version = version + 1, updated_at = now() WHERE id = $1`

	selectDecisionRequestSQL = `SELECT application_id FROM decision_requests WHERE idempotency_key = $1`
	insertDecisionRequestSQL = `INSERT INTO decision_requests (idempotency_key, application_id, user_id, action) VALUES ($1, $2, $3, $4)`

	updateUnderwriterDecisionSQL = `
UPDATE decisions SET outcome = $2, decided_by = $3, comment = $4, client_reasons = coalesce($5, client_reasons),
       details = details || jsonb_build_object('underwriter', $6::jsonb)
WHERE id = $1`

	insertUnderwriterDecisionSQL = `
INSERT INTO decisions (id, application_id, outcome, decided_by, comment, client_reasons, details)
VALUES ($1, $2, $3, $4, $5, $6, jsonb_build_object('underwriter', $7::jsonb))`

	withdrawOffersSQL = `UPDATE offers SET status = 'WITHDRAWN' WHERE application_id = $1 AND status = 'ACTIVE'`

	insertUnderwriterOfferSQL = `
INSERT INTO offers (id, application_id, kind, amount, term_months, rate, monthly_payment, terms, valid_until)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	insertQuestionSQL = `
INSERT INTO info_requests (id, application_id, required, question, due_at) VALUES ($1, $2, $3, $4, $5)`

	finishReviewSQL = `
UPDATE applications SET status = $2, assignee_id = $3, deadline_at = NULL, version = version + 1, updated_at = now()
WHERE id = $1`
)

// BackofficeRepository — данные рабочего места андеррайтера.
type BackofficeRepository struct {
	pool    *pgxpool.Pool
	apps    *ApplicationRepository
	cabinet *CabinetRepository
}

func NewBackofficeRepository(pool *pgxpool.Pool) *BackofficeRepository {
	return &BackofficeRepository{pool: pool, apps: &ApplicationRepository{pool: pool}, cabinet: NewCabinetRepository(pool)}
}

type queueArgs struct {
	statuses         []string
	assignee         string
	limit, offset    int
	companyID, appID string
}

func (r *BackofficeRepository) queue(ctx context.Context, a queueArgs) ([]domain.QueueItem, error) {
	if a.statuses == nil {
		a.statuses = []string{}
	}
	rows, err := r.pool.Query(ctx, selectQueueSQL, a.statuses, a.assignee, a.limit, a.offset, a.companyID, a.appID)
	if err != nil {
		return nil, fmt.Errorf("select queue: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.QueueItem, error) {
		var (
			it                       domain.QueueItem
			status, companyType      string
			amount                   *int64
			term                     *int32
			score                    *float64
			reasons, sources         []byte
			assigneeID, assigneeName string
			deadline                 *time.Time
		)
		if err := row.Scan(&it.ApplicationID, &it.Number, &status, &it.Version, &it.ProductCode, &amount, &term,
			&it.Company.ID, &it.Company.INN, &companyType, &it.Company.Name, &it.Grade, &score, &reasons, &sources,
			&assigneeID, &assigneeName, &it.SubmittedAt, &it.UpdatedAt, &deadline); err != nil {
			return it, err
		}
		it.Status, it.Company.Type = domain.ApplicationStatus(status), domain.CompanyType(companyType)
		if amount != nil && term != nil {
			it.Requested = &domain.RequestedTerms{Amount: *amount, TermMonths: int(*term)}
		}
		if score != nil {
			v := int(*score)
			it.Score = &v
		}
		if assigneeID != "" {
			it.Assignee = &domain.UserRef{ID: assigneeID, FullName: assigneeName}
		}
		if deadline != nil {
			d := deadline.UTC()
			it.DeadlineAt = &d
		}
		it.SubmittedAt, it.UpdatedAt = it.SubmittedAt.UTC(), it.UpdatedAt.UTC()
		// причины ручного рассмотрения — только у заявок у андеррайтера
		if it.Status == domain.ApplicationManualReview {
			var rs []struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(reasons, &rs)
			for _, x := range rs {
				if x.Message != "" {
					it.ReviewReasons = append(it.ReviewReasons, x.Message)
				}
			}
		}
		var recs []sourceRecord
		_ = json.Unmarshal(sources, &recs)
		for _, s := range recs {
			if s.State == string(domain.CollectFailed) || s.State == string(domain.CollectPartial) {
				it.DegradedSources = append(it.DegradedSources, domain.System(s.Source))
			}
		}
		return it, nil
	})
}

// Queue — очередь по фильтру; assignee "me" сервис заменяет на id пользователя.
func (r *BackofficeRepository) Queue(ctx context.Context, f domain.QueueFilter) ([]domain.QueueItem, error) {
	st := make([]string, len(f.Statuses))
	for i, s := range f.Statuses {
		st[i] = string(s)
	}
	return r.queue(ctx, queueArgs{statuses: st, assignee: f.Assignee, limit: f.Limit, offset: f.Offset})
}

// Counters — счётчики очереди для пользователя.
func (r *BackofficeRepository) Counters(ctx context.Context, userID string) (domain.QueueCounters, error) {
	var c domain.QueueCounters
	if err := r.pool.QueryRow(ctx, selectQueueCountersSQL, userID).Scan(&c.ManualReview, &c.Mine, &c.DueToday, &c.WaitingForInfo); err != nil {
		return c, fmt.Errorf("select queue counters: %w", err)
	}
	return c, nil
}

// QueueItem — строка очереди одной заявки; false — заявки нет или это черновик.
func (r *BackofficeRepository) QueueItem(ctx context.Context, appID string) (domain.QueueItem, bool, error) {
	items, err := r.queue(ctx, queueArgs{limit: 1, appID: appID})
	if err != nil || len(items) == 0 {
		return domain.QueueItem{}, false, err
	}
	return items[0], true, nil
}

// Dossier — всё по заявке для андеррайтера; false — заявки нет.
func (r *BackofficeRepository) Dossier(ctx context.Context, appID string) (domain.Dossier, bool, error) {
	app, ok, err := r.apps.Get(ctx, appID)
	if err != nil || !ok {
		return domain.Dossier{}, ok, err
	}
	d := domain.Dossier{Application: app, Systems: map[domain.System]json.RawMessage{}, Staff: map[string]domain.UserRef{}}
	var (
		assigneeID, assigneeName string
		deadline                 *time.Time
		registry                 []byte
	)
	if err := r.pool.QueryRow(ctx, selectDossierHeadSQL, appID).Scan(&d.Version, &assigneeID, &assigneeName, &deadline, &registry); err != nil {
		return d, false, fmt.Errorf("select dossier head: %w", err)
	}
	if assigneeID != "" {
		d.Assignee = &domain.UserRef{ID: assigneeID, FullName: assigneeName}
	}
	if deadline != nil {
		t := deadline.UTC()
		d.DeadlineAt = &t
	}
	if len(registry) > 0 {
		var rec registryRecord
		if err := json.Unmarshal(registry, &rec); err == nil {
			d.Card = &domain.CompanyCard{INN: rec.INN, Type: domain.CompanyType(rec.ApplicantType), OGRN: rec.OGRN, KPP: rec.KPP,
				FullName: rec.FullName, ShortName: rec.ShortName, RegistrationDate: rec.RegistrationDate,
				DirectorName: rec.DirectorName, Region: rec.Region, TerminatedOn: rec.TerminatedOn}
		}
	}

	// сбор: итог по системам и сырые ответы
	var (
		rawID    string
		sources  []byte
		finished time.Time
	)
	err = r.pool.QueryRow(ctx, selectLatestRawSQL, appID).Scan(&rawID, &sources, &finished)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return d, false, fmt.Errorf("select raw collection: %w", err)
	default:
		var recs []sourceRecord
		if err := json.Unmarshal(sources, &recs); err != nil {
			return d, false, fmt.Errorf("decode sources: %w", err)
		}
		d.Sources = fromSourceRecords(recs)
		t := finished.UTC()
		d.SourcesAt = &t
		if d.Systems, err = r.systems(ctx, rawID); err != nil {
			return d, false, err
		}
	}

	if d.Decision, err = r.latestDecision(ctx, r.pool, appID); err != nil {
		return d, false, err
	}
	if d.Offers, err = r.offers(ctx, appID); err != nil {
		return d, false, err
	}
	if d.InfoRequests, err = r.infoRequests(ctx, appID); err != nil {
		return d, false, err
	}
	if others, err := r.queue(ctx, queueArgs{limit: 20, companyID: app.Company.ID}); err != nil {
		return d, false, err
	} else {
		for _, o := range others {
			if o.ApplicationID != appID {
				d.Others = append(d.Others, o)
			}
		}
	}
	if d.Events, err = r.cabinet.Events(ctx, appID); err != nil {
		return d, false, err
	}
	var actors []string
	for _, e := range d.Events {
		if e.ActorRole == domain.UserUnderwriter {
			actors = append(actors, e.ActorID)
		}
	}
	if len(actors) > 0 {
		rows, err := r.pool.Query(ctx, selectStaffSQL, actors)
		if err != nil {
			return d, false, fmt.Errorf("select staff: %w", err)
		}
		staff, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.UserRef, error) {
			var u domain.UserRef
			return u, row.Scan(&u.ID, &u.FullName)
		})
		if err != nil {
			return d, false, err
		}
		for _, u := range staff {
			d.Staff[u.ID] = u
		}
	}
	return d, true, nil
}

func (r *BackofficeRepository) systems(ctx context.Context, rawID string) (map[domain.System]json.RawMessage, error) {
	cols := make([]string, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		cols = append(cols, SystemColumns[sys])
	}
	values := make([][]byte, len(cols))
	dest := make([]any, len(values))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := r.pool.QueryRow(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM raw_collections WHERE id = $1", rawID).Scan(dest...); err != nil {
		return nil, fmt.Errorf("select raw collection systems: %w", err)
	}
	out := map[domain.System]json.RawMessage{}
	for i, sys := range domain.Systems {
		if values[i] != nil {
			out[sys] = values[i]
		}
	}
	return out, nil
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *BackofficeRepository) latestDecision(ctx context.Context, q queryer, appID string) (*domain.DecisionRecord, error) {
	var (
		d       domain.DecisionRecord
		created time.Time
	)
	err := q.QueryRow(ctx, selectLatestDecisionSQL, appID).Scan(&d.ID, &d.Grade, &d.Score, &d.MaxAmount, &d.AutoOutcome,
		&d.Outcome, &d.DecidedBy, &d.Details, &d.ClientReasons, &d.Comment, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select decision: %w", err)
	}
	d.CreatedAt = created.UTC()
	return &d, nil
}

func (r *BackofficeRepository) offers(ctx context.Context, appID string) ([]domain.OfferRecord, error) {
	rows, err := r.pool.Query(ctx, selectOffersSQL, appID)
	if err != nil {
		return nil, fmt.Errorf("select offers: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.OfferRecord, error) {
		var o domain.OfferRecord
		err := row.Scan(&o.ID, &o.Kind, &o.Status, &o.Amount, &o.TermMonths, &o.Rate, &o.MonthlyPayment, &o.Terms, &o.ValidUntil)
		o.ValidUntil = o.ValidUntil.UTC()
		return o, err
	})
}

func (r *BackofficeRepository) infoRequests(ctx context.Context, appID string) ([]domain.InfoRequestRecord, error) {
	rows, err := r.pool.Query(ctx, selectInfoRequestsSQL, appID)
	if err != nil {
		return nil, fmt.Errorf("select info requests: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.InfoRequestRecord, error) {
		var q domain.InfoRequestRecord
		err := row.Scan(&q.ID, &q.Status, &q.Required, &q.Question, &q.Answer, &q.CreatedAt, &q.AnsweredAt, &q.DueAt)
		q.CreatedAt = q.CreatedAt.UTC()
		return q, err
	})
}

// Assign — андеррайтер берёт заявку в работу. false — заявки нет; domain.ErrNotInReview,
// domain.ErrAssignedToOther — взять нельзя. Повтор своим же — без изменений.
func (r *BackofficeRepository) Assign(ctx context.Context, appID, userID string) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var (
			status, assignee string
			version          int
		)
		err := tx.QueryRow(ctx, lockReviewSQL, appID).Scan(&status, &version, &assignee)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		switch {
		case status != string(domain.ApplicationManualReview):
			return domain.ErrNotInReview
		case assignee == userID:
			return nil
		case assignee != "":
			return domain.ErrAssignedToOther
		}
		if _, err := tx.Exec(ctx, assignSQL, appID, userID); err != nil {
			return fmt.Errorf("assign: %w", err)
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "uw.assigned", ActorID: userID})
	})
	return found, err
}

// Decide применяет решение андеррайтера одной транзакцией: проверки статуса, исполнителя и версии,
// решение в decisions, предложения или вопросы, новый статус и события. Повтор с тем же
// Idempotency-Key ничего не меняет. false — заявки нет.
func (r *BackofficeRepository) Decide(ctx context.Context, cmd domain.UnderwriterDecision) (bool, error) {
	found := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if cmd.IdempotencyKey != "" {
			var prev string
			err := tx.QueryRow(ctx, selectDecisionRequestSQL, cmd.IdempotencyKey).Scan(&prev)
			if err == nil {
				found = prev == cmd.ApplicationID
				if !found {
					return fmt.Errorf("idempotency key %q is used for another application", cmd.IdempotencyKey)
				}
				return nil // решение уже применено
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		var (
			status, assignee string
			version          int
		)
		err := tx.QueryRow(ctx, lockReviewSQL, cmd.ApplicationID).Scan(&status, &version, &assignee)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		switch {
		case status != string(domain.ApplicationManualReview):
			return domain.ErrNotInReview
		case assignee != "" && assignee != cmd.UserID:
			return domain.ErrAssignedToOther
		case cmd.IfMatch != nil && *cmd.IfMatch != version:
			return domain.ErrVersionConflict
		}

		uw, _ := json.Marshal(map[string]any{"action": cmd.Action, "user_id": cmd.UserID, "decided_at": cmd.Now,
			"adjustments": cmd.Adjustments, "reapply_after": reapply(cmd)})
		var reasons any
		if len(cmd.ClientReasons) > 0 {
			b, _ := json.Marshal(cmd.ClientReasons)
			reasons = string(b)
		}
		latest, err := r.latestDecision(ctx, tx, cmd.ApplicationID)
		if err != nil {
			return err
		}
		if latest != nil {
			_, err = tx.Exec(ctx, updateUnderwriterDecisionSQL, latest.ID, cmd.Outcome, cmd.UserID, nullString(cmd.InternalComment), reasons, string(uw))
		} else {
			_, err = tx.Exec(ctx, insertUnderwriterDecisionSQL, "dcs_"+ulid.Make().String(), cmd.ApplicationID, cmd.Outcome, cmd.UserID,
				nullString(cmd.InternalComment), reasons, string(uw))
		}
		if err != nil {
			return fmt.Errorf("save underwriter decision: %w", err)
		}

		switch cmd.Action {
		case "APPROVE":
			if _, err := tx.Exec(ctx, withdrawOffersSQL, cmd.ApplicationID); err != nil {
				return fmt.Errorf("withdraw offers: %w", err)
			}
			for i, o := range cmd.Offers {
				terms, _ := json.Marshal(map[string]any{"product_code": o.ProductCode, "collateral_required": o.CollateralRequired,
					"guarantee_required": o.GuaranteeRequired, "conditions": o.Conditions, "recommended": i == 0, "source": "underwriter"})
				if _, err := tx.Exec(ctx, insertUnderwriterOfferSQL, "ofr_"+ulid.Make().String(), cmd.ApplicationID, o.Kind, o.Amount,
					o.TermMonths, o.Rate, decision.Annuity(o.Amount, o.Rate, o.TermMonths), string(terms),
					cmd.Now.Add(time.Duration(o.ValidDays)*24*time.Hour)); err != nil {
					return fmt.Errorf("insert offer: %w", err)
				}
			}
		case "REQUEST_INFO":
			for _, q := range cmd.InfoRequests {
				question := map[string]any{"title": q.Title, "description": q.Description, "target_field": q.TargetField, "source": "underwriter"}
				if len(q.Input) > 0 {
					question["input"] = q.Input
				}
				if len(q.Documents) > 0 {
					question["documents"] = q.Documents
				}
				b, _ := json.Marshal(question)
				if _, err := tx.Exec(ctx, insertQuestionSQL, "ir_"+ulid.Make().String(), cmd.ApplicationID, q.Required, string(b), cmd.InfoDueAt); err != nil {
					return fmt.Errorf("insert info request: %w", err)
				}
			}
		}
		if _, err := tx.Exec(ctx, finishReviewSQL, cmd.ApplicationID, string(cmd.Status), cmd.UserID); err != nil {
			return fmt.Errorf("finish review: %w", err)
		}
		// служебное — без текста для клиента
		for _, a := range cmd.Adjustments {
			if err := insertEvent(ctx, tx, appEvent{ApplicationID: cmd.ApplicationID, Type: "uw.adjustment", ActorID: cmd.UserID, Payload: a}); err != nil {
				return err
			}
		}
		if c := strings.TrimSpace(cmd.InternalComment); c != "" {
			if err := insertEvent(ctx, tx, appEvent{ApplicationID: cmd.ApplicationID, Type: "uw.comment", ActorID: cmd.UserID,
				Payload: map[string]string{"comment": c}}); err != nil {
				return err
			}
		}
		if err := insertEvent(ctx, tx, appEvent{ApplicationID: cmd.ApplicationID, Type: "uw.decision", From: domain.ApplicationManualReview,
			To: cmd.Status, ActorID: cmd.UserID, Message: cmd.Message, Payload: map[string]string{"action": cmd.Action, "outcome": cmd.Outcome}}); err != nil {
			return err
		}
		if cmd.IdempotencyKey != "" {
			if _, err := tx.Exec(ctx, insertDecisionRequestSQL, cmd.IdempotencyKey, cmd.ApplicationID, cmd.UserID, cmd.Action); err != nil {
				return fmt.Errorf("save idempotency key: %w", err)
			}
		}
		return nil
	})
	return found, err
}

func reapply(cmd domain.UnderwriterDecision) any {
	if cmd.Action != "REJECT" || cmd.ReapplyAfter.IsZero() {
		return nil
	}
	return cmd.ReapplyAfter.Format(time.DateOnly)
}

// DecisionApplied — заявка, по которой уже применено решение с этим ключом; false — ключ новый.
func (r *BackofficeRepository) DecisionApplied(ctx context.Context, key string) (string, bool, error) {
	var appID string
	err := r.pool.QueryRow(ctx, selectDecisionRequestSQL, key).Scan(&appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("select decision request: %w", err)
	}
	return appID, true, nil
}

// LatestDecision — последняя оценка заявки; nil — оценки ещё не было.
func (r *BackofficeRepository) LatestDecision(ctx context.Context, appID string) (*domain.DecisionRecord, error) {
	return r.latestDecision(ctx, r.pool, appID)
}

// Offers — предложения по заявке.
func (r *BackofficeRepository) Offers(ctx context.Context, appID string) ([]domain.OfferRecord, error) {
	return r.offers(ctx, appID)
}

// InfoRequests — вопросы банка по заявке.
func (r *BackofficeRepository) InfoRequests(ctx context.Context, appID string) ([]domain.InfoRequestRecord, error) {
	return r.infoRequests(ctx, appID)
}
