package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/outbox"
)

var discardLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeStore — таблица outbox в памяти с семантикой PublishBatch: успех — события опубликованы,
// ошибка — остаются, attempts растёт.
type fakeStore struct {
	mu        sync.Mutex
	msgs      []events.Message
	published map[int64]bool
	attempts  map[int64]int
	lastError map[int64]string
}

func newStore(msgs ...events.Message) *fakeStore {
	return &fakeStore{msgs: msgs, published: map[int64]bool{}, attempts: map[int64]int{}, lastError: map[int64]string{}}
}

func (s *fakeStore) PublishBatch(ctx context.Context, topics []string, limit int,
	publish func(context.Context, []events.Message) error) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var batch []events.Message
	for _, m := range s.msgs {
		if slices.Contains(topics, m.Topic) && !s.published[m.ID] && len(batch) < limit {
			batch = append(batch, m)
		}
	}
	if len(batch) == 0 {
		return 0, nil
	}
	if err := publish(ctx, batch); err != nil {
		for _, m := range batch {
			s.attempts[m.ID]++
			s.lastError[m.ID] = err.Error()
		}
		return 0, err
	}
	for _, m := range batch {
		s.published[m.ID] = true
	}
	return len(batch), nil
}

func (s *fakeStore) attemptsOf(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[id]
}

func (s *fakeStore) publishedIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int64
	for _, m := range s.msgs {
		if s.published[m.ID] {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// fakeProducer запоминает отправленное; err — ошибка брокера, block — ждать отмены контекста.
type fakeProducer struct {
	mu    sync.Mutex
	err   error
	block bool
	got   [][]events.Message
}

func (p *fakeProducer) Publish(ctx context.Context, msgs []events.Message) error {
	if p.block {
		<-ctx.Done()
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.got = append(p.got, msgs)
	return nil
}

func (p *fakeProducer) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *fakeProducer) batches() [][]events.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func msg(id int64, topic string) events.Message {
	return events.Message{ID: id, Topic: topic, Key: "tsk_" + string(rune('a'+id)), Payload: []byte(`{}`)}
}

func config(batch int) outbox.Config {
	return outbox.Config{Topics: []string{"tasks", "documents"}, BatchSize: batch, PollInterval: time.Hour, PublishTimeout: time.Second}
}

func TestPublishOnce(t *testing.T) {
	store := newStore(msg(1, "tasks"), msg(2, "other"), msg(3, "documents"))
	producer := &fakeProducer{}

	n, err := outbox.New(store, producer, config(10), discardLog).PublishOnce(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("published %d, err %v", n, err)
	}
	// только события своих топиков, одной пачкой
	if b := producer.batches(); len(b) != 1 || len(b[0]) != 2 || b[0][0].ID != 1 || b[0][1].ID != 3 {
		t.Fatalf("batches = %+v", b)
	}
	if ids := store.publishedIDs(); !slices.Equal(ids, []int64{1, 3}) {
		t.Fatalf("published = %v", ids)
	}
}

// Брокер недоступен — события остаются в outbox, у них растёт attempts.
func TestPublishOnceProducerError(t *testing.T) {
	store := newStore(msg(1, "tasks"))
	boom := errors.New("kafka is down")

	n, err := outbox.New(store, &fakeProducer{err: boom}, config(10), discardLog).PublishOnce(context.Background())
	if !errors.Is(err, boom) || n != 0 {
		t.Fatalf("published %d, err %v", n, err)
	}
	if len(store.publishedIDs()) != 0 || store.attempts[1] != 1 || store.lastError[1] != boom.Error() {
		t.Fatalf("published %v, attempts %v, last error %v", store.publishedIDs(), store.attempts, store.lastError)
	}
}

// Брокер завис — отправка прерывается по PublishTimeout, события остаются в outbox.
func TestPublishOnceTimeout(t *testing.T) {
	store := newStore(msg(1, "tasks"))
	cfg := config(10)
	cfg.PublishTimeout = 20 * time.Millisecond

	_, err := outbox.New(store, &fakeProducer{block: true}, cfg, discardLog).PublishOnce(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || len(store.publishedIDs()) != 0 {
		t.Fatalf("err %v, published %v", err, store.publishedIDs())
	}
}

// За полной пачкой следующая идёт сразу, без паузы PollInterval; отмена контекста останавливает Run.
func TestRun(t *testing.T) {
	store := newStore(msg(1, "tasks"), msg(2, "tasks"), msg(3, "tasks"))
	producer := &fakeProducer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- outbox.New(store, producer, config(1), discardLog).Run(ctx) }()

	deadline := time.After(5 * time.Second)
	for len(store.publishedIDs()) < 3 {
		select {
		case <-deadline:
			t.Fatalf("published %v", store.publishedIDs())
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if b := producer.batches(); len(b) != 3 {
		t.Fatalf("batches = %+v", b)
	}
}

// Брокер недоступен — паузы между попытками растут; после восстановления событие уходит.
func TestRunBacksOffOnErrors(t *testing.T) {
	store := newStore(msg(1, "tasks"))
	producer := &fakeProducer{err: errors.New("kafka is down")}
	cfg := config(10)
	cfg.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- outbox.New(store, producer, cfg, discardLog).Run(ctx) }()

	time.Sleep(300 * time.Millisecond)
	// с постоянной паузой за 300 мс было бы ~30 попыток, с удвоением 10+20+40+80+160 мс — около пяти
	if attempts := store.attemptsOf(1); attempts < 2 || attempts > 8 {
		t.Fatalf("attempts = %d", attempts)
	}

	producer.setErr(nil)
	deadline := time.After(5 * time.Second)
	for len(store.publishedIDs()) == 0 {
		select {
		case <-deadline:
			t.Fatal("event is not published after the broker recovered")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
