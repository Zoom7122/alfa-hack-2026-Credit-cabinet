//go:build integration

package financial_test

import (
	"context"
	"errors"
	. "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/financial"
	"os"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/outbox"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
)

type interruptedSave struct {
	Repository
	err error
}

func (s interruptedSave) Complete(context.Context, Job, Result) error { return s.err }

// Real broker + PostgreSQL: transactional outbox, uncommitted failed delivery,
// restart in the same group, reclaim after lease expiry, and duplicate suppression.
// The processor is synthetic here; real-file parsing is covered by the API test.
func TestFinancialKafkaRedelivery(t *testing.T) {
	if os.Getenv("KAFKA_BROKERS") == "" {
		t.Skip("set KAFKA_BROKERS for live Kafka tests")
	}
	pool := IntegrationDB(t)
	brokers := kafka.Brokers(os.Getenv("KAFKA_BROKERS"))
	topic := "it.financial." + IntegrationNewID("")
	group := topic + ".worker"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := kafka.EnsureTopic(brokers, topic, 1); err != nil {
		t.Fatal(err)
	}
	admin, err := sarama.NewClusterAdmin(brokers, kafka.NewConfig("financial-test"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	defer func() { _ = admin.DeleteConsumerGroup(group); _ = admin.DeleteTopic(topic) }()
	p, err := kafka.NewProducer(brokers, "financial-test")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := Repository{Pool: pool, Topic: topic}
	d, ev := IntegrationEnqueueTestDocument(t, r)
	pub := outbox.New(postgres.NewOutboxRepository(pool), p, outbox.Config{Topics: []string{topic}, BatchSize: 10, PublishTimeout: 10 * time.Second}, IntegrationLog)
	if n, err := pub.PublishOnce(ctx); err != nil || n != 1 {
		t.Fatalf("outbox: %d %v", n, err)
	}
	var published bool
	if err = pool.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM outbox WHERE key=$1`, d.ID).Scan(&published); err != nil || !published {
		t.Fatal("outbox not persisted", err)
	}
	processor := IntegrationProcessorFunc(func(_ context.Context, j Job, d Document, _ []Document) (Result, error) {
		return IntegrationResult(j, d), nil
	})
	failure := errors.New("simulated database outage during result persistence")
	first := NewWorker(interruptedSave{Repository: r, err: failure}, processor, WorkerConfig{}, IntegrationLog)
	c, err := kafka.NewConsumer(brokers, "financial-test", group, topic, 1, IntegrationLog)
	if err != nil {
		t.Fatal(err)
	}
	err = c.Run(ctx, first.Handle)
	_ = c.Close()
	if !errors.Is(err, failure) {
		t.Fatalf("worker did not stop on save failure: %v", err)
	}
	offset := func() int64 {
		t.Helper()
		response, err := admin.ListConsumerGroupOffsets(group, map[string][]int32{topic: {0}})
		if err != nil {
			t.Fatal(err)
		}
		block := response.GetBlock(topic, 0)
		if block == nil || block.Err != sarama.ErrNoError {
			t.Fatal("offset unavailable", block)
		}
		return block.Offset
	}
	if got := offset(); got > 0 {
		t.Fatalf("failed message acknowledged at %d", got)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM financial_document_results`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed save leaked results: %d %v", count, err)
	}
	IntegrationExpireLease(t, r, ev.TaskID)
	// Outbox delivery may repeat, even after a successful broker acknowledgement.
	if err = p.Publish(ctx, []events.Message{{Topic: topic, Key: d.ID, Payload: IntegrationRequested(ev.TaskID)}}); err != nil {
		t.Fatal(err)
	}
	second := NewWorker(r, processor, WorkerConfig{}, IntegrationLog)
	c, err = kafka.NewConsumer(brokers, "financial-test", group, topic, 1, IntegrationLog)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- c.Run(runCtx, second.Handle) }()
	defer func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("restarted consumer: %v", err)
		}
		_ = c.Close()
	}()
	for offset() < 2 {
		select {
		case <-ctx.Done():
			t.Fatal("redelivery was not committed", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	var status string
	var attempts, reports int
	if err = pool.QueryRow(ctx, `SELECT status,attempts,(SELECT count(*) FROM financial_document_results),(SELECT count(*) FROM signature_reports) FROM financial_document_jobs WHERE id=$1`, ev.TaskID).Scan(&status, &attempts, &count, &reports); err != nil {
		t.Fatal(err)
	}
	if status != "COMPLETED" || attempts != 2 || count != 1 || reports != 1 {
		t.Fatalf("redelivery: status=%s attempts=%d results=%d reports=%d", status, attempts, count, reports)
	}
}
