// Сервис сбора данных о заёмщике для передачи в скоринг.
//
//	@title						data-collector API
//	@version					0.2
//	@description				Сбор данных о заёмщике для скоринга: заявитель и компания, анкета с документами и сырые ответы
//	@description				всех внешних систем (эмулятор mock-api). Сначала регистрация (POST /applicants → черновик заявки),
//	@description				затем анкета с файлами (POST /applications/{id}/questionnaire). Сбор асинхронный: анкета ставит
//	@description				задачу в очередь (outbox → Kafka), системы опрашивает worker.
//	@description				Вход — мок-логином, как в api/openapi.yaml: GET /auth/test-users → POST /auth/mock-login → токен
//	@description				`mock.<user_id>` в `Authorization: Bearer …`. С токеном регистрация идёт от имени пользователя; без него — анонимно.
//	@description				Ошибки — RFC 9457 (`application/problem+json`).
//	@BasePath					/api/v1
//
//	@securityDefinitions.apikey	bearer
//	@in							header
//	@name						Authorization
//	@description				Bearer mock.<user_id> из POST /auth/mock-login
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/env"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
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
	// mock-api: синхронная подсказка реквизитов по ИНН (ЕГРЮЛ) для кабинета
	mockAPIURL := env.String("MOCK_API_URL", "http://localhost:8090")
	lookupTimeout, err := env.Duration("LOOKUP_TIMEOUT", 3*time.Second)
	if err != nil {
		return err
	}
	bankTimeout, err := env.Duration("BANK_API_TIMEOUT", 5*time.Second)
	if err != nil {
		return err
	}
	topic := env.String("KAFKA_TOPIC", events.DefaultTopic)
	documentsTopic := env.String("KAFKA_DOCUMENTS_TOPIC", events.DocumentsTopic)
	documentsDir := env.String("DOCUMENTS_DIR", "data/documents")
	maxFileMB, err := env.PositiveInt("MAX_FILE_SIZE_MB", 20)
	if err != nil {
		return err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is not set: заявки, задачи на сбор и outbox хранятся в Postgres")
	}
	maxFileSize := int64(maxFileMB) << 20
	files, err := storage.NewLocal(documentsDir, maxFileSize)
	if err != nil {
		return err
	}

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// заявки: регистрация (пользователь, компания, черновик) и отправка анкеты с документами;
	// при отправке задача на сбор и события пишутся в outbox той же транзакцией, системы опрашивает worker
	applications := postgres.NewApplicationRepository(pool, topic, documentsTopic)
	collectionSvc := service.NewCollectionService(postgres.NewCollectionTaskRepository(pool))
	// ЕГРЮЛ эмулятора: подсказка по ИНН и проверка на ликвидацию при регистрации
	registry := mockapi.New(mockAPIURL, lookupTimeout, "")
	applicationSvc := service.NewApplicationService(applications, service.WithRegistryCheck(registry))

	// мок-вход: тестовые пользователи заводятся при старте (их компании — со своими id, если уже есть)
	authSvc := service.NewAuthService(postgres.NewUserRepository(pool))
	if env.String("SEED_TEST_USERS", "true") != "false" {
		if err := authSvc.Seed(ctx, service.TestUsers); err != nil {
			return err
		}
		log.Info("test users seeded", "count", len(service.TestUsers))
	}
	questionnaireSvc := service.NewQuestionnaireService(applications, files, collectionSvc)

	// кабинет клиента: компании и заявки пользователя, журнал, документы, отчёты о подписи, подсказка по ИНН
	cabinetSvc := service.NewCabinetService(postgres.NewCabinetRepository(pool), applications,
		registry,
		service.FileOpenerFunc(func(key string) (io.ReadCloser, error) { return files.Open(key) }))

	// внутренние данные банка: хендлер → сервис → клиент банка (без хранилища)
	internalDataSvc := service.NewInternalDataService(bank.New(bankURL, bankTimeout))

	router := httptransport.NewRouter(
		httptransport.NewApplicationHandler(applicationSvc, log),
		// тело анкеты: все файлы и JSON с запасом на заголовки частей
		httptransport.NewQuestionnaireHandler(questionnaireSvc, service.MaxFiles*maxFileSize+2<<20, log),
		httptransport.NewCollectionHandler(collectionSvc, log),
		httptransport.NewInternalDataHandler(internalDataSvc, log),
		httptransport.WithAuth(httptransport.NewAuthHandler(authSvc, log)),
		httptransport.WithCabinet(httptransport.NewCabinetHandler(cabinetSvc, log)),
		// итог для клиента: решение, предложения, вопросы банка и ответы
		httptransport.WithOutcome(httptransport.NewOutcomeHandler(service.NewOutcomeService(postgres.NewBackofficeRepository(pool),
			applications, files, 4*time.Hour), service.MaxFiles*maxFileSize+2<<20, log)),
		// рабочее место андеррайтера
		httptransport.WithBackoffice(httptransport.NewBackofficeHandler(
			service.NewBackofficeService(postgres.NewBackofficeRepository(pool)), log)),
		// поставщик Б и МС3 для credit-engine
		httptransport.WithEngineHooks(httptransport.NewEngineHandler(
			service.NewDecisionService(postgres.NewDecisionRepository(pool), nil, service.DecisionConfig{}), log)),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute, // анкета с файлами до 200 МБ
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("data-collector listening", "addr", addr, "bank_api", bankURL, "topics", []string{topic, documentsTopic},
			"documents_dir", documentsDir)
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
