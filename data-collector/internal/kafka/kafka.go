// Package kafka — обвязка sarama для data-collector: настройки клиента, создание топика,
// продюсер событий outbox и цикл чтения в составе consumer group.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

// retryBackoff — пауза перед новой попыткой войти в группу, если Kafka недоступна.
const retryBackoff = 2 * time.Second

// Brokers разбирает список брокеров через запятую: "kafka:9092,kafka-2:9092".
func Brokers(s string) []string {
	var out []string
	for _, b := range strings.Split(s, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// NewConfig — общие настройки клиентов. Версия протокола — минимальная версия брокера,
// которую мы поддерживаем (Kafka 4.x из compose с ней совместима). Сетевые таймауты
// короткие: недоступная Kafka должна быстро давать ошибку, а не держать вызов.
func NewConfig(clientID string) *sarama.Config {
	cfg := sarama.NewConfig()
	cfg.ClientID = clientID
	cfg.Version = sarama.V3_0_0_0
	cfg.Net.DialTimeout = 5 * time.Second
	cfg.Net.ReadTimeout = 10 * time.Second
	cfg.Net.WriteTimeout = 10 * time.Second
	cfg.Metadata.Retry.Max = 3
	cfg.Metadata.Retry.Backoff = 250 * time.Millisecond
	return cfg
}

// EnsureTopic создаёт топик, если его ещё нет. Фактор репликации — по умолчанию брокера.
func EnsureTopic(brokers []string, topic string, partitions int32) error {
	admin, err := sarama.NewClusterAdmin(brokers, NewConfig("data-collector-admin"))
	if err != nil {
		return fmt.Errorf("kafka admin: %w", err)
	}
	defer admin.Close()
	err = admin.CreateTopic(topic, &sarama.TopicDetail{NumPartitions: partitions, ReplicationFactor: -1}, false)
	if err != nil && !errors.Is(err, sarama.ErrTopicAlreadyExists) {
		return fmt.Errorf("create topic %s: %w", topic, err)
	}
	return nil
}

// Producer публикует события outbox в Kafka. Ключ сообщения — ключ события (id задачи).
type Producer struct {
	sp sarama.SyncProducer
}

// NewProducer — синхронный продюсер: запись подтверждают все синхронные реплики.
// Повторы ограничены, чтобы при недоступной Kafka отправка быстро заканчивалась ошибкой:
// события останутся в outbox и уйдут на следующем круге. Повтор может дать дубль
// сообщения — worker к дублям готов.
func NewProducer(brokers []string, clientID string) (*Producer, error) {
	cfg := NewConfig(clientID)
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	cfg.Producer.Retry.Max = 3
	cfg.Producer.Retry.Backoff = 250 * time.Millisecond
	sp, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	return &Producer{sp: sp}, nil
}

// Publish отправляет события и ждёт, пока брокер запишет все. Ошибка — хотя бы одно не записано.
// Отмена ctx прерывает ожидание; если отправка всё же завершится, события уйдут ещё раз.
func (p *Producer) Publish(ctx context.Context, msgs []events.Message) error {
	records := make([]*sarama.ProducerMessage, 0, len(msgs))
	for _, m := range msgs {
		records = append(records, &sarama.ProducerMessage{Topic: m.Topic, Key: sarama.StringEncoder(m.Key),
			Value: sarama.ByteEncoder(m.Payload)})
	}
	done := make(chan error, 1)
	go func() { done <- p.sp.SendMessages(records) }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("send to kafka: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close дожидается отправки и закрывает соединения.
func (p *Producer) Close() error {
	return p.sp.Close()
}

// Handler обрабатывает одно сообщение. Ошибка — сообщение не подтверждается.
type Handler func(ctx context.Context, value []byte) error

// Consumer читает топик в составе consumer group.
type Consumer struct {
	group       sarama.ConsumerGroup
	topic       string
	concurrency int
	log         *slog.Logger
}

// NewConsumer — участник consumer group. Оффсеты коммитятся вручную после обработки пачки;
// новая группа читает топик с начала, так что задачи, созданные до первого запуска
// worker'а, не теряются. concurrency — сколько сообщений обрабатывается одновременно
// на все партиции.
func NewConsumer(brokers []string, clientID, group, topic string, concurrency int, log *slog.Logger) (*Consumer, error) {
	cfg := NewConfig(clientID)
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Offsets.AutoCommit.Enable = false
	cfg.Consumer.Return.Errors = true
	g, err := sarama.NewConsumerGroup(brokers, group, cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer group: %w", err)
	}
	go func() {
		for err := range g.Errors() {
			log.Warn("kafka consumer", "group", group, "err", err)
		}
	}()
	return &Consumer{group: g, topic: topic, concurrency: concurrency, log: log}, nil
}

// Run читает сообщения, пока не отменён ctx. Сообщения партиции обрабатываются пачками:
// всё, что уже пришло, но не больше concurrency; оффсет коммитится, когда обработана вся
// пачка (at-least-once). После сбоя сообщения придут снова, поэтому обработчик должен
// быть идемпотентным. Ошибка обработчика останавливает чтение без коммита: Run возвращает
// её, процесс завершается и после перезапуска получит сообщение снова. Если Kafka
// недоступна, Run ждёт её и пробует снова. Отмена ctx — штатная остановка.
func (c *Consumer) Run(ctx context.Context, handle Handler) error {
	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	h := &claimHandler{ctx: runCtx, stop: stop, handle: handle, slots: make(chan struct{}, c.concurrency), batch: c.concurrency}

	for runCtx.Err() == nil {
		err := c.group.Consume(runCtx, []string{c.topic}, h)
		if errors.Is(err, sarama.ErrClosedConsumerGroup) {
			break
		}
		if err != nil && runCtx.Err() == nil {
			c.log.WarnContext(ctx, "kafka consume", "topic", c.topic, "err", err)
			select {
			case <-runCtx.Done():
			case <-time.After(retryBackoff):
			}
		}
	}
	return h.failure()
}

// Close выходит из группы и закрывает соединения.
func (c *Consumer) Close() error {
	return c.group.Close()
}

// claimHandler обрабатывает партиции, выданные группой. Обработчик получает контекст Run,
// а не сессии: ребаланс не прерывает начатую пачку, sarama ждёт её до Rebalance.Timeout.
type claimHandler struct {
	ctx    context.Context
	stop   context.CancelCauseFunc
	handle Handler
	slots  chan struct{} // одновременно обрабатываемые сообщения, на все партиции
	batch  int

	mu  sync.Mutex
	err error // первая ошибка обработчика
}

func (h *claimHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *claimHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *claimHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		batch, ok := h.next(session, claim)
		if !ok {
			return nil
		}
		if err := h.process(batch); err != nil {
			// при штатной остановке ошибка ожидаема: пачка просто придёт снова
			if h.ctx.Err() == nil {
				h.fail(err)
			}
			return nil
		}
		session.MarkMessage(batch[len(batch)-1], "")
		session.Commit()
	}
}

// next ждёт сообщение и добирает к нему уже пришедшие, но не больше batch.
// false — партицию забрали или сессия закончилась.
func (h *claimHandler) next(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) ([]*sarama.ConsumerMessage, bool) {
	var batch []*sarama.ConsumerMessage
	select {
	case <-session.Context().Done():
		return nil, false
	case msg, ok := <-claim.Messages():
		if !ok {
			return nil, false
		}
		batch = append(batch, msg)
	}
	for len(batch) < h.batch {
		select {
		case msg, ok := <-claim.Messages():
			if !ok {
				return batch, true
			}
			batch = append(batch, msg)
		default:
			return batch, true
		}
	}
	return batch, true
}

// process обрабатывает пачку параллельно и возвращает все ошибки.
func (h *claimHandler) process(batch []*sarama.ConsumerMessage) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, msg := range batch {
		h.slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-h.slots }()
			if err := h.handle(h.ctx, msg.Value); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s[%d]@%d: %w", msg.Topic, msg.Partition, msg.Offset, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// fail запоминает первую ошибку обработчика и останавливает Run.
func (h *claimHandler) fail(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
	h.stop(err)
}

func (h *claimHandler) failure() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}
