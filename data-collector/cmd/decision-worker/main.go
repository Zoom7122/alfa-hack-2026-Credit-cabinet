// Worker оценки: берёт заявки после сбора данных (VERIFYING), отправляет их в credit-engine и
// записывает итог — предложение, отказ, вопросы клиенту или ручное рассмотрение.
// Заявки берёт из Postgres (FOR UPDATE SKIP LOCKED): несколько реплик не мешают друг другу.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/dispatch"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
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

	concurrency, err := env.PositiveInt("DECISION_CONCURRENCY", 4)
	if err != nil {
		return err
	}
	attempts, err := env.PositiveInt("DECISION_MAX_ATTEMPTS", 8)
	if err != nil {
		return err
	}
	lease, err := env.Duration("DECISION_LEASE", 60*time.Second)
	if err != nil {
		return err
	}
	heartbeat, err := env.Duration("DECISION_HEARTBEAT", 10*time.Second)
	if err != nil {
		return err
	}
	if poll <= 0 || timeout <= 0 || heartbeat <= 0 || lease <= heartbeat*2 || lease <= timeout+10*time.Second {
		return fmt.Errorf("decision durations must be positive; lease must exceed 2 heartbeats and HTTP timeout + save timeout")
	}
	activeModels, err := strconv.ParseBool(env.String("DECISION_ACTIVE_MODELS", "false"))
	if err != nil {
		return fmt.Errorf("DECISION_ACTIVE_MODELS: %w", err)
	}
	repo := postgres.ScoringJobRepository{Pool: pool, UseActiveModels: activeModels}
	w := &dispatch.Worker{Repo: repo, Engine: scoring.New(scoringURL, timeout), Config: dispatch.Config{Lease: lease, Heartbeat: heartbeat, MaxAttempts: attempts, ReviewSLA: cfg.ReviewSLA, OfferValidity: cfg.OfferValidity, EvaluationDate: cfg.EvaluationDate}, Log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(200) })
	mux.HandleFunc("/readyz", func(rw http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if pool.Ping(c) != nil {
			rw.WriteHeader(503)
		} else {
			rw.WriteHeader(200)
		}
	})
	mux.HandleFunc("/metrics", func(rw http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		s, err := repo.Stats(c)
		if err != nil {
			rw.WriteHeader(503)
			return
		}
		rw.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(rw, "scoring_queue_pending %d\nscoring_queue_running %d\nscoring_queue_retry_wait %d\nscoring_queue_failed %d\nscoring_queue_oldest_seconds %g\nscoring_processed_total %d\nscoring_errors_total %d\nscoring_retries_total %d\nscoring_processing_seconds_sum %g\n", s.Pending, s.Running, s.Retries, s.Failed, s.OldestSeconds, w.Processed.Load(), w.Errors.Load(), w.Retried.Load(), float64(w.DurationNanos.Load())/float64(time.Second))
	})
	server := &http.Server{Addr: env.String("DECISION_HTTP_ADDR", ":9090"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(c)
	}()
	log.Info("decision worker started", "scoring_url", scoringURL, "concurrency", concurrency)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			ticker := time.NewTicker(poll)
			defer ticker.Stop()
			for {
				ok, err := w.ProcessNext(ctx)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					log.Error("process scoring job", "err", err)
				}
				if ok && err == nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}
	select {
	case <-ctx.Done():
		wg.Wait()
		return nil
	case err := <-serverErr:
		stop()
		wg.Wait()
		return err
	}
}
