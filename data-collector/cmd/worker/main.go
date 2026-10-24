// Worker сбора данных: читает задачи из Kafka и опрашивает по каждой все внешние системы (mock-api).
// Входные данные берёт из collection_tasks, результат пишет в raw_collections.
// Несколько реплик делят партиции топика в одной consumer group.
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

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("worker stopped", "err", err)
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
	group := env.String("KAFKA_GROUP", "data-collector-worker")
	partitions, err := env.PositiveInt("KAFKA_TOPIC_PARTITIONS", 3)
	if err != nil {
		return err
	}
	mockURL := env.String("MOCK_API_URL", "http://localhost:8090")
	mockTimeout, err := env.Duration("MOCK_API_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	collectTimeout, err := env.Duration("COLLECT_TIMEOUT", 8*time.Second)
	if err != nil {
		return err
	}
	concurrency, err := env.PositiveInt("WORKER_CONCURRENCY", 4)
	if err != nil {
		return err
	}
	maxAttempts, err := env.PositiveInt("WORKER_MAX_ATTEMPTS", 3)
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
	consumer, err := kafka.NewConsumer(brokers, "data-collector-worker", group, topic, concurrency, log)
	if err != nil {
		return err
	}
	defer consumer.Close()

	// сбор: клиент внешних систем (mock-api) + задачи и результаты в Postgres
	w := worker.New(
		mockapi.New(mockURL, mockTimeout, env.String("OWN_BANK_BIK", "044525999")),
		postgres.NewCollectionTaskRepository(pool, topic),
		worker.Config{CollectTimeout: collectTimeout, MaxAttempts: maxAttempts},
		log,
	)
	log.Info("worker started", "brokers", brokers, "topic", topic, "group", group, "concurrency", concurrency,
		"mock_api", mockURL)
	if err := consumer.Run(ctx, w.Handle); err != nil {
		return err
	}
	log.Info("shutting down")
	return nil
}
