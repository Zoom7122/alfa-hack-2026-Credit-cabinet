//go:build integration

// Весь путь задачи против живых Postgres, Kafka и mock-api:
// Enqueue (задача + событие в outbox) → outbox-publisher → Kafka → worker → raw_collections.
//
//	make up && make test-integration
//
// Нужны DATABASE_URL и KAFKA_BROKERS, без них тест пропускается. Топик и группа — свои
// для теста, поэтому запущенные в compose publisher и worker его не трогают.
package worker_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/outbox"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
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
	topic, group := "it.pipeline."+suffix, "it-pipeline-"+suffix

	pool, err := postgres.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := kafka.EnsureTopic(brokers, topic, 2); err != nil {
		t.Fatal(err)
	}
	admin, err := sarama.NewClusterAdmin(brokers, kafka.NewConfig("it-admin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.DeleteConsumerGroup(group); err != nil {
			t.Errorf("delete group: %v", err)
		}
		if err := admin.DeleteTopic(topic); err != nil {
			t.Errorf("delete topic: %v", err)
		}
		_ = admin.Close()
	})
	producer, err := kafka.NewProducer(brokers, "it-publisher")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })

	// API: анкета → задача + событие в outbox
	tasks := postgres.NewCollectionTaskRepository(pool, topic)
	task, err := service.NewCollectionService(tasks).Enqueue(ctx, "qst_it_"+suffix, domain.CollectInput{
		INN:               "7799000012",
		Director:          &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", MiddleName: "Сергеевич", BirthDate: "1984-07-19"},
		BKICompanyConsent: &domain.Consent{ID: "CNS-IT", ValidUntil: "2027-04-21"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			"DELETE FROM raw_collections WHERE task_id = $1",
			"DELETE FROM collection_tasks WHERE id = $1",
		} {
			if _, err := pool.Exec(ctx, q, task.ID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, "DELETE FROM outbox WHERE topic = $1", topic); err != nil {
			t.Errorf("cleanup outbox: %v", err)
		}
	})

	// worker и publisher — как в своих контейнерах
	consumer, err := kafka.NewConsumer(brokers, "it-worker", group, topic, 2, discardLog)
	if err != nil {
		t.Fatal(err)
	}
	w := worker.New(mockapi.New(mockURL, 5*time.Second, "044525999"), tasks,
		worker.Config{CollectTimeout: 15 * time.Second, MaxAttempts: 3}, discardLog)
	pub := outbox.New(postgres.NewOutboxRepository(pool), producer,
		outbox.Config{Topic: topic, BatchSize: 10, PollInterval: 100 * time.Millisecond, PublishTimeout: 10 * time.Second}, discardLog)

	runCtx, stop := context.WithCancel(ctx)
	workerDone, publisherDone := make(chan error, 1), make(chan error, 1)
	go func() { workerDone <- consumer.Run(runCtx, w.Handle) }()
	go func() { publisherDone <- pub.Run(runCtx) }()

	var got domain.CollectionTask
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		var ok bool
		if got, ok, err = tasks.Get(ctx, task.ID); err != nil || !ok {
			t.Fatalf("get task: ok %v, err %v", ok, err)
		}
		if got.Status == domain.TaskCompleted || got.Status == domain.TaskFailed || time.Now().After(deadline) {
			break
		}
	}
	stop()
	if err := <-workerDone; err != nil {
		t.Fatalf("worker: %v", err)
	}
	if err := <-publisherDone; err != nil {
		t.Fatalf("publisher: %v", err)
	}
	if err := consumer.Close(); err != nil {
		t.Errorf("close consumer: %v", err)
	}

	if got.Status != domain.TaskCompleted || got.Attempts != 1 || got.Result == nil || len(got.Result.Systems) != len(domain.Systems) {
		t.Fatalf("task = %+v", got)
	}
	skipped := map[domain.System]bool{}
	for _, sys := range got.Skipped {
		skipped[sys] = true
	}
	for _, sys := range []domain.System{domain.SystemTaxReports, domain.SystemEGRN, domain.SystemOpenBanking,
		domain.SystemMarketplaces, domain.SystemOFD} {
		if !skipped[sys] {
			t.Errorf("%s must be skipped: %v", sys, got.Skipped)
		}
	}
	for _, s := range got.Result.Systems {
		want := domain.CollectDone
		if skipped[s.System] {
			want = domain.CollectSkipped
		}
		if s.State != want {
			t.Errorf("%s: state %s, want %s (%s)", s.System, s.State, want, s.Reason)
		}
	}

	// в raw_collections — строка задачи, у пропущенных систем колонка NULL; событие опубликовано
	var (
		egrnNull, bkiNull, published bool
	)
	if err := pool.QueryRow(ctx, `SELECT egrn IS NULL, bki IS NULL FROM raw_collections WHERE task_id = $1`, task.ID).
		Scan(&egrnNull, &bkiNull); err != nil || !egrnNull || bkiNull {
		t.Fatalf("raw_collections: egrn null %v, bki null %v, err %v", egrnNull, bkiNull, err)
	}
	if err := pool.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM outbox WHERE topic = $1 AND key = $2`, topic, task.ID).
		Scan(&published); err != nil || !published {
		t.Fatalf("outbox: published %v, err %v", published, err)
	}
}
