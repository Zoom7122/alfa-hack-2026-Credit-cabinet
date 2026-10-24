package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// uniqueViolation — SQLSTATE нарушения уникальности.
const uniqueViolation = "23505"

// taskColumns — колонки задачи в порядке scanTask.
const taskColumns = `t.id, t.questionnaire_id, t.status, t.request, t.secrets, t.skipped_sources, t.attempts,
       t.error, t.created_at, t.started_at, t.finished_at`

const (
	insertTask = `
INSERT INTO collection_tasks (id, questionnaire_id, inn, status, request, secrets, skipped_sources, created_at, updated_at)
VALUES ($1, $2, $3, 'PENDING', $4, $5, $6, $7, $7)`

	insertOutbox = `INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`

	// claimTask берёт задачу в работу. RUNNING тоже подходит: прошлый worker упал, не дойдя
	// до коммита оффсета, и сообщение пришло снова. Кончились попытки — FAILED.
	// Выражения в SET видят строку до изменения.
	claimTask = `
UPDATE collection_tasks AS t SET
    status      = CASE WHEN t.attempts < $2::int THEN 'RUNNING' ELSE 'FAILED' END,
    attempts    = CASE WHEN t.attempts < $2::int THEN t.attempts + 1 ELSE t.attempts END,
    error       = CASE WHEN t.attempts < $2::int THEN NULL ELSE $3::text END,
    secrets     = CASE WHEN t.attempts < $2::int THEN t.secrets END,
    started_at  = CASE WHEN t.attempts < $2::int THEN now() ELSE t.started_at END,
    finished_at = CASE WHEN t.attempts < $2::int THEN NULL ELSE now() END,
    updated_at  = now()
WHERE t.id = $1 AND t.status IN ('PENDING', 'RUNNING')
RETURNING ` + taskColumns

	completeTask = `
UPDATE collection_tasks SET status = 'COMPLETED', error = NULL, secrets = NULL, finished_at = $2, updated_at = now()
WHERE id = $1 AND status = 'RUNNING'`

	failTask = `
UPDATE collection_tasks SET status = 'FAILED', error = $2, secrets = NULL, finished_at = now(), updated_at = now()
WHERE id = $1 AND status = 'RUNNING'`

	selectTask = `
SELECT ` + taskColumns + `, rc.id, rc.sources, rc.started_at, rc.finished_at
FROM collection_tasks t LEFT JOIN raw_collections rc ON rc.task_id = t.id
WHERE t.id = $1`
)

// CollectionTaskRepository — задачи на сбор (collection_tasks) и их результаты (raw_collections).
// Задача создаётся одной транзакцией с событием в outbox, результат пишется одной транзакцией
// со сменой статуса задачи.
type CollectionTaskRepository struct {
	pool  *pgxpool.Pool
	topic string // топик Kafka для события о новой задаче
}

func NewCollectionTaskRepository(pool *pgxpool.Pool, topic string) *CollectionTaskRepository {
	return &CollectionTaskRepository{pool: pool, topic: topic}
}

// Create сохраняет задачу в статусе PENDING вместе с событием о ней в outbox.
func (r *CollectionTaskRepository) Create(ctx context.Context, task domain.CollectionTask) error {
	request, err := json.Marshal(toRequestRecord(task.Input))
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	secrets, err := encodeSecrets(task.Input)
	if err != nil {
		return err
	}
	event, err := json.Marshal(events.NewCollectionRequested(task))
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	skipped := make([]string, 0, len(task.Skipped))
	for _, sys := range task.Skipped {
		skipped = append(skipped, string(sys))
	}

	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, insertTask, task.ID, task.QuestionnaireID, task.Input.INN, string(request), secrets,
			skipped, task.CreatedAt); err != nil {
			return fmt.Errorf("insert collection_tasks: %w", err)
		}
		if _, err := tx.Exec(ctx, insertOutbox, r.topic, task.ID, string(event)); err != nil {
			return fmt.Errorf("insert outbox: %w", err)
		}
		return nil
	})
}

// Get возвращает задачу; у выполненной — с итогом по системам из raw_collections (без сырых данных).
func (r *CollectionTaskRepository) Get(ctx context.Context, id string) (domain.CollectionTask, bool, error) {
	var (
		rcID                  *string
		sources               []byte
		rcStarted, rcFinished *time.Time
	)
	task, err := scanTask(r.pool.QueryRow(ctx, selectTask, id), &rcID, &sources, &rcStarted, &rcFinished)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionTask{}, false, nil
	}
	if err != nil {
		return domain.CollectionTask{}, false, fmt.Errorf("select collection task: %w", err)
	}
	if rcID != nil && rcStarted != nil && rcFinished != nil {
		var records []sourceRecord
		if err := json.Unmarshal(sources, &records); err != nil {
			return domain.CollectionTask{}, false, fmt.Errorf("decode sources: %w", err)
		}
		task.Result = &domain.RawCollection{ID: *rcID, TaskID: task.ID, QuestionnaireID: task.QuestionnaireID,
			Input: task.Input, Systems: fromSourceRecords(records), StartedAt: rcStarted.UTC(), FinishedAt: rcFinished.UTC()}
	}
	return task, true, nil
}

// Claim переводит задачу в RUNNING и увеличивает attempts; у задачи, которую брали уже
// maxAttempts раз, — в FAILED. false — задачи нет или она уже завершена.
func (r *CollectionTaskRepository) Claim(ctx context.Context, id string, maxAttempts int) (domain.CollectionTask, bool, error) {
	task, err := scanTask(r.pool.QueryRow(ctx, claimTask, id, maxAttempts, domain.ErrAttemptsExhausted.Error()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionTask{}, false, nil
	}
	if err != nil {
		return domain.CollectionTask{}, false, fmt.Errorf("claim collection task: %w", err)
	}
	return task, true, nil
}

// Complete записывает сбор в raw_collections и переводит задачу в COMPLETED (токены стираются).
// domain.ErrTaskNotRunning — задача уже не RUNNING или её результат уже записан.
func (r *CollectionTaskRepository) Complete(ctx context.Context, rc domain.RawCollection) error {
	args, err := insertArgs(rc)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, completeTask, rc.TaskID, rc.FinishedAt)
		if err != nil {
			return fmt.Errorf("complete collection task: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrTaskNotRunning
		}
		if _, err := tx.Exec(ctx, insertRawCollection, args...); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "raw_collections_task_uidx" {
				return domain.ErrTaskNotRunning
			}
			return fmt.Errorf("insert raw_collections: %w", err)
		}
		return nil
	})
}

// Fail переводит выполняющуюся задачу в FAILED с причиной; токены стираются.
func (r *CollectionTaskRepository) Fail(ctx context.Context, id, reason string) error {
	if _, err := r.pool.Exec(ctx, failTask, id, reason); err != nil {
		return fmt.Errorf("fail collection task: %w", err)
	}
	return nil
}

// scanTask читает колонки taskColumns; extra — колонки запроса после них.
func scanTask(row pgx.Row, extra ...any) (domain.CollectionTask, error) {
	var (
		task                  domain.CollectionTask
		status                string
		request, secrets      []byte
		skipped               []string
		attempts              int16
		errText               *string
		startedAt, finishedAt *time.Time
	)
	dest := append([]any{&task.ID, &task.QuestionnaireID, &status, &request, &secrets, &skipped, &attempts,
		&errText, &task.CreatedAt, &startedAt, &finishedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return domain.CollectionTask{}, err
	}

	var rec requestRecord
	if err := json.Unmarshal(request, &rec); err != nil {
		return domain.CollectionTask{}, fmt.Errorf("decode request: %w", err)
	}
	var sec *secretsRecord
	if secrets != nil {
		sec = &secretsRecord{}
		if err := json.Unmarshal(secrets, sec); err != nil {
			return domain.CollectionTask{}, fmt.Errorf("decode secrets: %w", err)
		}
	}
	task.Input = fromRequestRecord(rec, sec)
	task.Status = domain.TaskStatus(status)
	for _, s := range skipped {
		task.Skipped = append(task.Skipped, domain.System(s))
	}
	task.Attempts = int(attempts)
	if errText != nil {
		task.Error = *errText
	}
	task.CreatedAt = task.CreatedAt.UTC()
	if startedAt != nil {
		task.StartedAt = startedAt.UTC()
	}
	if finishedAt != nil {
		task.FinishedAt = finishedAt.UTC()
	}
	return task, nil
}
