//go:build integration

// Kafka против живого брокера: публикация событий outbox и чтение в consumer group.
//
//	KAFKA_BROKERS=localhost:9094 make test-integration
//
// Без KAFKA_BROKERS тесты пропускаются. Каждый тест работает в своём топике и своей группе.
package kafka_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/kafka"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func testBrokers(t *testing.T) []string {
	t.Helper()
	v := os.Getenv("KAFKA_BROKERS")
	if v == "" {
		t.Skip("KAFKA_BROKERS is not set")
	}
	return kafka.Brokers(v)
}

func suffix() string { return time.Now().UTC().Format("20060102150405.000000000") }

func testAdmin(t *testing.T, brokers []string) sarama.ClusterAdmin {
	t.Helper()
	admin, err := sarama.NewClusterAdmin(brokers, kafka.NewConfig("it-admin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return admin
}

// testTopic создаёт топик из одной партиции и удаляет его вместе с группами после теста.
func testTopic(t *testing.T, brokers []string, groups ...string) string {
	t.Helper()
	topic := "it.kafka." + suffix()
	for range 2 { // второй раз — топик уже есть, это не ошибка
		if err := kafka.EnsureTopic(brokers, topic, 1); err != nil {
			t.Fatal(err)
		}
	}
	admin := testAdmin(t, brokers)
	t.Cleanup(func() {
		for _, g := range groups {
			if err := admin.DeleteConsumerGroup(g); err != nil {
				t.Errorf("delete group %s: %v", g, err)
			}
		}
		if err := admin.DeleteTopic(topic); err != nil {
			t.Errorf("delete topic: %v", err)
		}
	})
	return topic
}

func publish(t *testing.T, brokers []string, topic string, payloads ...string) {
	t.Helper()
	p, err := kafka.NewProducer(brokers, "it-producer")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var msgs []events.Message
	for i, payload := range payloads {
		msgs = append(msgs, events.Message{ID: int64(i + 1), Topic: topic, Key: "tsk_" + payload, Payload: []byte(payload)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Publish(ctx, msgs); err != nil {
		t.Fatal(err)
	}
}

// consume читает топик группой, пока handle не вернёт done или не выйдет время.
// Вызовы handle идут по одному, хотя Consumer обрабатывает пачку параллельно.
func consume(t *testing.T, brokers []string, topic, group string, handle func(value []byte) (done bool, err error)) error {
	t.Helper()
	c, err := kafka.NewConsumer(brokers, "it-consumer", group, topic, 2, discardLog)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var mu sync.Mutex
	err = c.Run(ctx, func(_ context.Context, value []byte) error {
		mu.Lock()
		defer mu.Unlock()
		done, err := handle(value)
		if done {
			cancel()
		}
		return err
	})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("timed out waiting for messages")
	}
	return err
}

// committed — оффсет, закоммиченный группой по партиции 0; -1 — коммитов нет.
func committed(t *testing.T, brokers []string, topic, group string) int64 {
	t.Helper()
	resp, err := testAdmin(t, brokers).ListConsumerGroupOffsets(group, map[string][]int32{topic: {0}})
	if err != nil {
		t.Fatal(err)
	}
	block := resp.GetBlock(topic, 0)
	if block == nil {
		return -1
	}
	return block.Offset
}

func TestIntegrationPublishConsume(t *testing.T) {
	brokers := testBrokers(t)
	group := "it-group-" + suffix()
	topic := testTopic(t, brokers, group)
	publish(t, brokers, topic, `{"n":1}`, `{"n":2}`, `{"n":3}`)

	var got []string
	err := consume(t, brokers, topic, group, func(value []byte) (bool, error) {
		got = append(got, string(value))
		return len(got) == 3, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{`{"n":1}`, `{"n":2}`, `{"n":3}`}) {
		t.Fatalf("got %v", got)
	}
	if at := committed(t, brokers, topic, group); at != 3 {
		t.Fatalf("committed offset = %d, want 3", at)
	}
}

// Ошибка обработчика останавливает чтение без коммита: следующий consumer группы получает сообщение снова.
func TestIntegrationRedeliveryOnError(t *testing.T) {
	brokers := testBrokers(t)
	group := "it-group-" + suffix()
	topic := testTopic(t, brokers, group)
	publish(t, brokers, topic, `{"n":1}`)

	boom := errors.New("db down")
	err := consume(t, brokers, topic, group, func([]byte) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("want handler error, got %v", err)
	}
	if at := committed(t, brokers, topic, group); at > 0 {
		t.Fatalf("offset committed after failure: %d", at)
	}

	var got []string
	if err := consume(t, brokers, topic, group, func(value []byte) (bool, error) {
		got = append(got, string(value))
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{`{"n":1}`}) {
		t.Fatalf("redelivered %v", got)
	}
	if at := committed(t, brokers, topic, group); at != 1 {
		t.Fatalf("committed offset = %d, want 1", at)
	}
}
