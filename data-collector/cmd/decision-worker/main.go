// Worker оценки: берёт заявки после сбора данных (VERIFYING), отправляет их в credit-engine и
// записывает итог — предложение, отказ, вопросы клиенту или ручное рассмотрение.
// Заявки берёт из Postgres (FOR UPDATE SKIP LOCKED): несколько реплик не мешают друг другу.
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

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("decision worker stopped", "err", err)
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
	scoringURL := env.String("SCORING_URL", "http://localhost:8082")
	timeout, err := env.Duration("SCORING_TIMEOUT", 20*time.Second)
	if err != nil {
		return err
	}
	poll, err := env.Duration("DECISION_POLL_INTERVAL", 2*time.Second)
	if err != nil {
		return err
	}
	cfg := service.DecisionConfig{}
	if cfg.SignatureWait, err = env.Duration("DECISION_SIGNATURE_WAIT", 2*time.Minute); err != nil {
		return err
	}
	if cfg.Stale, err = env.Duration("DECISION_STALE", 2*time.Minute); err != nil {
		return err
	}
	if cfg.ReviewSLA, err = env.Duration("REVIEW_SLA", 4*time.Hour); err != nil {
		return err
	}
	if cfg.OfferValidity, err = env.Duration("OFFER_VALIDITY", 14*24*time.Hour); err != nil {
		return err
	}
	if d := os.Getenv("DEMO_EVALUATION_DATE"); d != "" {
		if cfg.EvaluationDate, err = time.Parse(time.DateOnly, d); err != nil {
			return fmt.Errorf("DEMO_EVALUATION_DATE: %w", err)
		}
	}

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	svc := service.NewDecisionService(postgres.NewDecisionRepository(pool), scoring.New(scoringURL, timeout), cfg)
	log.Info("decision worker started", "scoring_url", scoringURL)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		// всё, что накопилось, — подряд; потом ждём следующего тика
		for {
			id, ok, err := svc.ProcessNext(ctx)
			if err != nil {
				log.Error("score application", "application_id", id, "err", err)
				break
			}
			if !ok {
				break
			}
			log.Info("application scored", "application_id", id)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
