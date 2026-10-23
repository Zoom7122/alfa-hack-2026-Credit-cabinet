// Сервис сбора данных о заёмщике для передачи в скоринг.
//
//	@title			data-collector API
//	@version		0.1
//	@description	Сбор данных о заёмщике для скоринга: анкета клиента и сырые ответы всех внешних систем (эмулятор mock-api).
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
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
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

	addr := envOr("HTTP_ADDR", ":8081")
	bankURL := envOr("BANK_API_URL", "http://localhost:8090/bank/v1")
	bankTimeout, err := time.ParseDuration(envOr("BANK_API_TIMEOUT", "5s"))
	if err != nil {
		return fmt.Errorf("BANK_API_TIMEOUT: %w", err)
	}
	mockURL := envOr("MOCK_API_URL", "http://localhost:8090")
	mockTimeout, err := time.ParseDuration(envOr("MOCK_API_TIMEOUT", "5s"))
	if err != nil {
		return fmt.Errorf("MOCK_API_TIMEOUT: %w", err)
	}
	collectTimeout, err := time.ParseDuration(envOr("COLLECT_TIMEOUT", "8s"))
	if err != nil {
		return fmt.Errorf("COLLECT_TIMEOUT: %w", err)
	}

	// сырые ответы систем: Postgres, если задан DATABASE_URL, иначе память процесса
	var rawRepo service.RawCollectionRepository
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		pool, err := postgres.Connect(ctx, dbURL)
		if err != nil {
			return fmt.Errorf("connect postgres: %w", err)
		}
		defer pool.Close()
		rawRepo = postgres.NewRawCollectionRepository(pool)
	} else {
		log.Warn("DATABASE_URL is not set: raw collections are kept in memory")
		rawRepo = memory.NewRawCollectionRepository()
	}

	// сбор данных: сервис → клиент внешних систем (mock-api) + хранилище raw_collections
	mockClient := mockapi.New(mockURL, mockTimeout, envOr("OWN_BANK_BIK", "044525999"))
	collectionSvc := service.NewCollectionService(mockClient, rawRepo, collectTimeout)

	// анкеты: хендлер → сервис → хранилище; после сохранения анкеты — сбор данных
	questionnaireRepo := memory.NewQuestionnaireRepository()
	questionnaireSvc := service.NewQuestionnaireService(questionnaireRepo, collectionSvc)

	// внутренние данные банка: хендлер → сервис → клиент банка (без хранилища)
	bankClient := bank.New(bankURL, bankTimeout)
	internalDataSvc := service.NewInternalDataService(bankClient)

	router := httptransport.NewRouter(
		httptransport.NewQuestionnaireHandler(questionnaireSvc, log),
		httptransport.NewInternalDataHandler(internalDataSvc, log),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      collectTimeout + 10*time.Second, // POST /questionnaires ждёт весь сбор
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("data-collector listening", "addr", addr, "bank_api", bankURL, "mock_api", mockURL)
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

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
