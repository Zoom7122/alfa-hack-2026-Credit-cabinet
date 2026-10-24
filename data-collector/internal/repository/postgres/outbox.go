package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

const (
	selectUnpublished = `
SELECT id, topic, key, payload FROM outbox
WHERE topic = $1 AND published_at IS NULL
ORDER BY id
LIMIT $2
FOR UPDATE SKIP LOCKED`

	markPublished = `UPDATE outbox SET published_at = now() WHERE id = ANY($1)`
	markAttempt   = `UPDATE outbox SET attempts = attempts + 1, last_error = $2 WHERE id = ANY($1)`
)

// OutboxRepository — таблица outbox: события, которые надо опубликовать в Kafka.
type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

// PublishBatch берёт до limit неопубликованных событий топика, отдаёт их publish и помечает
// опубликованными — в одной транзакции. До её конца строки заблокированы (FOR UPDATE SKIP LOCKED):
// параллельные publisher'ы берут разные события. Ошибка publish — события остаются
// неопубликованными, у них растёт attempts и пишется last_error. Возвращает, сколько опубликовано.
func (r *OutboxRepository) PublishBatch(ctx context.Context, topic string, limit int,
	publish func(context.Context, []events.Message) error) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // после Commit ничего не делает

	rows, err := tx.Query(ctx, selectUnpublished, topic, limit)
	if err != nil {
		return 0, fmt.Errorf("select outbox: %w", err)
	}
	msgs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (events.Message, error) {
		var m events.Message
		err := row.Scan(&m.ID, &m.Topic, &m.Key, &m.Payload)
		return m, err
	})
	if err != nil {
		return 0, fmt.Errorf("read outbox: %w", err)
	}
	if len(msgs) == 0 {
		return 0, nil
	}
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}

	if perr := publish(ctx, msgs); perr != nil {
		if _, err := tx.Exec(ctx, markAttempt, ids, perr.Error()); err != nil {
			return 0, errors.Join(perr, fmt.Errorf("mark outbox attempt: %w", err))
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, errors.Join(perr, fmt.Errorf("commit: %w", err))
		}
		return 0, perr
	}
	if _, err := tx.Exec(ctx, markPublished, ids); err != nil {
		return 0, fmt.Errorf("mark outbox published: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return len(msgs), nil
}
