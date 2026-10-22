// Сервис сбора данных о заёмщике для передачи в скоринг.
//
//	@title			data-collector API
//	@version		0.1
//	@description	Сбор данных о заёмщике для скоринга: анкета клиента и данные внутренних систем банка.
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
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
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

	// анкеты: хендлер → сервис → хранилище
	questionnaireRepo := memory.NewQuestionnaireRepository()
	questionnaireSvc := service.NewQuestionnaireService(questionnaireRepo)

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
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("data-collector listening", "addr", addr, "bank_api", bankURL)
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
