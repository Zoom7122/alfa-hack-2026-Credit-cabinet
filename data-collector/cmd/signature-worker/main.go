// Worker проверки электронных подписей: читает события о документах заявок из Kafka
// (data-collector.documents), проверяет подписи и пишет отчёты в signature_reports.
// Файлы читает из того же хранилища, что и API (volume documents, только на чтение).
// Профиль один — TEST: тестовые корни и CRL, моки ЕГРЮЛ и реестра МЧД; это не проверка УКЭП.
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

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/sigworker"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("signature worker stopped", "err", err)
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
	if profile := env.String("SIGNATURE_PROFILE", signature.ProfileTest); profile != signature.ProfileTest {
		return fmt.Errorf("SIGNATURE_PROFILE=%s не поддерживается: есть только TEST; проверка УКЭП требует "+
			"сертифицированного СКЗИ и в MVP не реализована", profile)
	}
	brokers := kafka.Brokers(env.String("KAFKA_BROKERS", "localhost:9094"))
	topic := env.String("KAFKA_DOCUMENTS_TOPIC", events.DocumentsTopic)
	group := env.String("KAFKA_GROUP", "data-collector-signature-worker")
	partitions, err := env.PositiveInt("KAFKA_TOPIC_PARTITIONS", 3)
	if err != nil {
		return err
	}
	concurrency, err := env.PositiveInt("WORKER_CONCURRENCY", 4)
	if err != nil {
		return err
	}
	documentsDir := env.String("DOCUMENTS_DIR", "data/documents")
	maxFileMB, err := env.PositiveInt("MAX_FILE_SIZE_MB", 20)
	if err != nil {
		return err
	}
	authorityWait, err := env.Duration("SIGNATURE_AUTHORITY_WAIT", 20*time.Second)
	if err != nil {
		return err
	}
	attempts, err := env.PositiveInt("SIGNATURE_RETRY_ATTEMPTS", 3)
	if err != nil {
		return err
	}
	trustDir := os.Getenv("SIGNATURE_TRUST_DIR")

	trust, err := signature.LoadTrustStore(trustDir)
	if err != nil {
		return err
	}
	rules, err := signature.LoadRules(os.Getenv("SIGNATURE_RULES_FILE"))
	if err != nil {
		return err
	}
	files, err := storage.NewReader(documentsDir, int64(maxFileMB)<<20)
	if err != nil {
		return err
	}
	defer files.Close()

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := kafka.EnsureTopic(brokers, topic, int32(partitions)); err != nil {
		return err
	}
	consumer, err := kafka.NewConsumer(brokers, "data-collector-signature-worker", group, topic, concurrency, log)
	if err != nil {
		return err
	}
	defer consumer.Close()

	w := sigworker.New(postgres.NewSignatureRepository(pool), files, signature.NewVerifier(trust, rules),
		sigworker.Config{AuthorityWait: authorityWait, Attempts: attempts}, log)
	roots, intermediates, crls := trust.Summary()
	if roots == 0 {
		log.Warn("no trusted roots: certificates will be INDETERMINATE", "trust_dir", trustDir)
	}
	log.Info("signature worker started", "profile", signature.ProfileTest, "brokers", brokers, "topic", topic, "group", group,
		"concurrency", concurrency, "documents_dir", documentsDir, "trust_dir", trustDir, "roots", roots,
		"intermediates", intermediates, "crls", crls, "rules", rules.Version, "authority_wait", authorityWait)
	if err := consumer.Run(ctx, w.Handle); err != nil {
		return err
	}
	log.Info("shutting down")
	return nil
}
