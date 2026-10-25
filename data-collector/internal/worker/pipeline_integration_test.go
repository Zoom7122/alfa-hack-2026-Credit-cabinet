//go:build integration

// Весь путь заявки против живых Postgres, Kafka и mock-api:
// регистрация → анкета с документом и подписью (заявка, задача, события в outbox одной транзакцией) →
// outbox-publisher → Kafka → worker → raw_collections, заявка VERIFYING с реквизитами из ЕГРЮЛ;
// событие о документах доходит до своего топика.
//
//	make up && make test-integration
//
// Нужны DATABASE_URL и KAFKA_BROKERS, без них тест пропускается. Топики, группа и компания — свои
// для теста, поэтому запущенные в compose publisher и worker их не трогают.
package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/outbox"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/worker"
)

func TestIntegrationPipeline(t *testing.T) {
	dbURL, brokersEnv := os.Getenv("DATABASE_URL"), os.Getenv("KAFKA_BROKERS")
	if dbURL == "" || brokersEnv == "" {
		t.Skip("DATABASE_URL or KAFKA_BROKERS is not set")
	}
	mockURL := os.Getenv("MOCK_API_URL")
	if mockURL == "" {
		mockURL = "http://localhost:8090"
	}
	brokers := kafka.Brokers(brokersEnv)
	ctx := context.Background()
	suffix := time.Now().UTC().Format("20060102150405.000000000")
	tasksTopic, docsTopic := "it.pipeline.tasks."+suffix, "it.pipeline.documents."+suffix
	group, docsGroup := "it-pipeline-"+suffix, "it-pipeline-docs-"+suffix

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, topic := range []string{tasksTopic, docsTopic} {
		if err := kafka.EnsureTopic(brokers, topic, 2); err != nil {
			t.Fatal(err)
		}
	}
	admin, err := sarama.NewClusterAdmin(brokers, kafka.NewConfig("it-admin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, g := range []string{group, docsGroup} {
			if err := admin.DeleteConsumerGroup(g); err != nil {
				t.Errorf("delete group %s: %v", g, err)
			}
		}
		for _, topic := range []string{tasksTopic, docsTopic} {
			if err := admin.DeleteTopic(topic); err != nil {
				t.Errorf("delete topic %s: %v", topic, err)
			}
		}
		_ = admin.Close()
	})
	producer, err := kafka.NewProducer(brokers, "it-publisher")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })

	// API: регистрация «Северного ветра» под новым ИНН теста и анкета с документом и подписью
	files, err := storage.NewLocal(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	apps := postgres.NewApplicationRepository(pool, tasksTopic, docsTopic)
	tasks := postgres.NewCollectionTaskRepository(pool)
	collections := service.NewCollectionService(tasks)
	questionnaires := service.NewQuestionnaireService(apps, files, collections)
	// у теста свой человек (ИНН физлица), поэтому пользователь и черновик — новые; компания —
	// с ИНН мока: если она уже есть в БД, тест её переиспользует и не удаляет
	app, err := service.NewApplicationService(apps).Register(ctx, service.RegisterInput{FullName: "Тестов Тест Тестович",
		PersonINN: fmt.Sprintf("88%010d", time.Now().UnixNano()%1e10), Role: domain.RoleDirector, CompanyINN: "7799000012"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupApplication(t, pool, app, tasksTopic, docsTopic) })
	var staged []storage.Staged
	for name, body := range map[string]string{"balance.pdf": "%PDF-1.4 balance", "balance.pdf.sig": "signature"} {
		st, err := questionnaires.StageFile(name, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		staged = append(staged, st)
	}
	submitted, err := questionnaires.Submit(ctx, app.ID, service.SubmitInput{Age: 42, MaritalStatus: domain.MaritalMarried,
		ChildrenCount: 2, Files: staged,
		Company: domain.CollectInput{Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь",
			MiddleName: "Сергеевич", BirthDate: "1984-07-19"}, BKICompanyConsent: &domain.Consent{ID: "CNS-IT", ValidUntil: "2027-04-21"}},
		Documents: []service.DocumentSpec{{FileName: "balance.pdf", Type: domain.DocFinancialStatements}}})
	if err != nil {
		t.Fatal(err)
	}

	// worker, publisher и потребитель топика документов (будущий worker подписи и OCR)
	consumer, err := kafka.NewConsumer(brokers, "it-worker", group, tasksTopic, 2, discardLog)
	if err != nil {
		t.Fatal(err)
	}
	docsConsumer, err := kafka.NewConsumer(brokers, "it-documents", docsGroup, docsTopic, 1, discardLog)
	if err != nil {
		t.Fatal(err)
	}
	w := worker.New(mockapi.New(mockURL, 5*time.Second, "044525999"), tasks,
		worker.Config{CollectTimeout: 15 * time.Second, MaxAttempts: 3}, discardLog)
	pub := outbox.New(postgres.NewOutboxRepository(pool), producer, outbox.Config{Topics: []string{tasksTopic, docsTopic},
		BatchSize: 10, PollInterval: 100 * time.Millisecond, PublishTimeout: 10 * time.Second}, discardLog)

	var (
		mu       sync.Mutex
		docEvent events.DocumentsSubmittedEvent
	)
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 3)
	go func() { done <- consumer.Run(runCtx, w.Handle) }()
	go func() { done <- pub.Run(runCtx) }()
	go func() {
		done <- docsConsumer.Run(runCtx, func(_ context.Context, value []byte) error {
			mu.Lock()
			defer mu.Unlock()
			return json.Unmarshal(value, &docEvent)
		})
	}()

	var got domain.Application
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		var ok bool
		if got, ok, err = apps.Get(ctx, app.ID); err != nil || !ok {
			t.Fatalf("get application: ok %v, err %v", ok, err)
		}
		mu.Lock()
		docsReceived := docEvent.ApplicationID != ""
		mu.Unlock()
		if (got.Status == domain.ApplicationVerifying && docsReceived) || time.Now().After(deadline) {
			break
		}
	}
	stop()
	for range 3 {
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	for _, c := range []*kafka.Consumer{consumer, docsConsumer} {
		if err := c.Close(); err != nil {
			t.Errorf("close consumer: %v", err)
		}
	}

	if got.Status != domain.ApplicationVerifying || got.Company.Name != `ООО "СЕВЕРНЫЙ ВЕТЕР"` || got.Company.OGRN != "1217700412345" {
		t.Fatalf("application = %+v", got)
	}
	task, ok, err := tasks.Get(ctx, submitted.Task.ID)
	if err != nil || !ok || task.Status != domain.TaskCompleted || task.Result == nil || len(task.Result.Systems) != len(domain.Systems) ||
		len(task.DocumentIDs) != 2 {
		t.Fatalf("task = %+v (ok %v, err %v)", task, ok, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if docEvent.ApplicationID != app.ID || len(docEvent.Documents) != 2 {
		t.Fatalf("documents event = %+v", docEvent)
	}
}

// cleanupApplication удаляет строки, созданные тестом: заявку со всем, что к ней относится,
// пользователя и компанию, если её создал тест.
func cleanupApplication(t *testing.T, pool *pgxpool.Pool, app domain.Application, topics ...string) {
	t.Helper()
	steps := []struct {
		sql string
		arg any
	}{
		{"DELETE FROM raw_collections WHERE task_id IN (SELECT id FROM collection_tasks WHERE application_id = $1)", app.ID},
		{"DELETE FROM collection_tasks WHERE application_id = $1", app.ID},
		{"DELETE FROM outbox WHERE topic = ANY($1)", topics},
		{"DELETE FROM documents WHERE application_id = $1 AND signed_document_id IS NOT NULL", app.ID},
		{"DELETE FROM documents WHERE application_id = $1", app.ID},
		{"DELETE FROM application_events WHERE application_id = $1", app.ID},
		{"DELETE FROM applications WHERE id = $1", app.ID},
		{"DELETE FROM company_members WHERE user_id = $1", app.Representative.UserID},
		{"DELETE FROM users WHERE id = $1", app.Representative.UserID},
	}
	if !app.Company.AlreadyKnown {
		steps = append(steps, struct {
			sql string
			arg any
		}{"DELETE FROM companies WHERE id = $1", app.Company.ID})
	}
	for _, st := range steps {
		if _, err := pool.Exec(context.Background(), st.sql, st.arg); err != nil {
			t.Errorf("cleanup %q: %v", st.sql, err)
		}
	}
}
