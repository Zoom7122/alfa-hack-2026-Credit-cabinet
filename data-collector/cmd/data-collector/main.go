// Сервис сбора данных о заёмщике для передачи в скоринг.
//
//	@title			data-collector API
//	@version		0.2
//	@description	Сбор данных о заёмщике для скоринга: анкета клиента и сырые ответы всех внешних систем (эмулятор mock-api).
//	@description	Сбор асинхронный: анкета ставит задачу в очередь (outbox → Kafka), системы опрашивает worker.
//	@description	Ошибки — RFC 9457 (`application/problem+json`).
//	@BasePath		/api/v1
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("data-collector stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := env.String("HTTP_ADDR", ":8081")
	bankURL := env.String("BANK_API_URL", "http://localhost:8090/bank/v1")
	bankTimeout, err := env.Duration("BANK_API_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	topic := env.String("KAFKA_TOPIC", events.DefaultTopic)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set: задачи на сбор и outbox хранятся в Postgres")
	}

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// сбор данных: задача + событие в outbox одной транзакцией; системы опрашивает worker
	collectionSvc := service.NewCollectionService(postgres.NewCollectionTaskRepository(pool, topic))

	// анкеты: хендлер → сервис → хранилище; после сохранения анкеты — задача на сбор
	questionnaireSvc := service.NewQuestionnaireService(memory.NewQuestionnaireRepository(), collectionSvc)

	// внутренние данные банка: хендлер → сервис → клиент банка (без хранилища)
	internalDataSvc := service.NewInternalDataService(bank.New(bankURL, bankTimeout))

	router := httptransport.NewRouter(
		httptransport.NewQuestionnaireHandler(questionnaireSvc, log),
		httptransport.NewCollectionHandler(collectionSvc, log),
		httptransport.NewInternalDataHandler(internalDataSvc, log),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      bankTimeout + 10*time.Second, // /check-mock ждёт системы банка
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("data-collector listening", "addr", addr, "bank_api", bankURL, "topic", topic)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
