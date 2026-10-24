// Publisher outbox: переносит события из таблицы outbox в Kafka.
// Событие о задаче на сбор пишет API (POST /questionnaires) одной транзакцией с задачей,
// читает его worker. Несколько реплик publisher'а безопасны: события блокируются FOR UPDATE SKIP LOCKED.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/outbox"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("outbox publisher stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	brokers := kafka.Brokers(env.String("KAFKA_BROKERS", "localhost:9094"))
	topic := env.String("KAFKA_TOPIC", events.DefaultTopic)
	partitions, err := env.PositiveInt("KAFKA_TOPIC_PARTITIONS", 3)
	if err != nil {
		return err
	}
	pollInterval, err := env.Duration("PUBLISHER_POLL_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return err
	}
	batchSize, err := env.PositiveInt("PUBLISHER_BATCH_SIZE", 100)
	if err != nil {
		return err
	}
	publishTimeout, err := env.Duration("PUBLISHER_PUBLISH_TIMEOUT", 10*time.Second)
	if err != nil {
		return err
	}

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := kafka.EnsureTopic(brokers, topic, int32(partitions)); err != nil {
		return err
	}
	producer, err := kafka.NewProducer(brokers, "data-collector-publisher")
	if err != nil {
		return err
	}
	defer producer.Close()

	p := outbox.New(postgres.NewOutboxRepository(pool), producer, outbox.Config{
		Topic:          topic,
		BatchSize:      batchSize,
		PollInterval:   pollInterval,
		PublishTimeout: publishTimeout,
	}, log)
	log.Info("outbox publisher started", "brokers", brokers, "topic", topic, "poll_interval", pollInterval)
	if err := p.Run(ctx); err != nil {
		return err
	}
	log.Info("shutting down")
	return nil
}
