// Package outbox переносит события из таблицы outbox в Kafka (transactional outbox):
// сервис пишет событие в одной транзакции с данными, publisher доставляет его брокеру.
// Доставка at-least-once: событие, опубликованное перед сбоем коммита, уйдёт ещё раз.
package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// maxErrorBackoff — самая длинная пауза после неудачных публикаций подряд.
const maxErrorBackoff = 5 * time.Second

// Store — таблица outbox. Объявлено у потребителя.
type Store interface {
	// PublishBatch берёт до limit неопубликованных событий топика, вызывает publish и при
	// успехе помечает их опубликованными. Возвращает, сколько опубликовано.
	PublishBatch(ctx context.Context, topic string, limit int, publish func(context.Context, []events.Message) error) (int, error)
}

// Producer — брокер сообщений.
type Producer interface {
	Publish(ctx context.Context, msgs []events.Message) error
}

// Config — настройки публикации.
type Config struct {
	Topic          string        // какие события публиковать
	BatchSize      int           // событий за один круг
	PollInterval   time.Duration // пауза, когда публиковать нечего или публикация не удалась
	PublishTimeout time.Duration // на отправку пачки брокеру
}

// Publisher периодически переносит события из outbox в брокер.
type Publisher struct {
	store    Store
	producer Producer
	cfg      Config
	log      *slog.Logger
}

func New(store Store, producer Producer, cfg Config, log *slog.Logger) *Publisher {
	return &Publisher{store: store, producer: producer, cfg: cfg, log: log}
}

// Run публикует события, пока не отменён ctx. За полной пачкой сразу идёт следующая,
// иначе — пауза PollInterval. Ошибки БД и брокера не останавливают publisher:
// события остаются в outbox и уйдут на следующем круге. Пока ошибки идут подряд,
// пауза удваивается до maxErrorBackoff, чтобы не долбить недоступную Kafka и не засорять лог.
func (p *Publisher) Run(ctx context.Context) error {
	backoff := p.cfg.PollInterval
	for {
		n, err := p.PublishOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		pause := p.cfg.PollInterval
		switch {
		case err != nil:
			pause = backoff
			backoff = min(2*backoff, max(p.cfg.PollInterval, maxErrorBackoff))
			p.log.ErrorContext(ctx, "publish outbox", "topic", p.cfg.Topic, "err", err, "retry_in", pause)
		case n == p.cfg.BatchSize:
			backoff = p.cfg.PollInterval
			continue
		default:
			backoff = p.cfg.PollInterval
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(pause):
		}
	}
}

// PublishOnce публикует одну пачку событий и возвращает, сколько опубликовано.
func (p *Publisher) PublishOnce(ctx context.Context) (int, error) {
	n, err := p.store.PublishBatch(ctx, p.cfg.Topic, p.cfg.BatchSize, func(ctx context.Context, msgs []events.Message) error {
		ctx, cancel := context.WithTimeout(ctx, p.cfg.PublishTimeout)
		defer cancel()
		return p.producer.Publish(ctx, msgs)
	})
	if n > 0 {
		p.log.InfoContext(ctx, "outbox published", "topic", p.cfg.Topic, "events", n)
	}
	return n, err
}
