package dispatch

import (
	"context"
	"errors"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeQueue struct {
	jobs              []Job
	current           Job
	failed, completed int
	failSave          bool
	terminal          bool
}

func (f *fakeQueue) EnqueueNext(context.Context, time.Time) (bool, error) { return false, nil }
func (f *fakeQueue) Claim(context.Context, time.Duration) (Job, bool, error) {
	if len(f.jobs) == 0 {
		return Job{}, false, nil
	}
	j := f.jobs[0]
	f.jobs = f.jobs[1:]
	f.current = j
	return j, true, nil
}
func (f *fakeQueue) Renew(context.Context, Job, time.Duration) error { return nil }
func (f *fakeQueue) StoreResponse(_ context.Context, j Job, r decision.Response) error {
	f.current.Response = &r
	return nil
}
func (f *fakeQueue) Complete(context.Context, Job, decision.Outcome, any, *time.Time, time.Time) error {
	if f.failSave {
		f.failSave = false
		return errors.New("database unavailable")
	}
	f.completed++
	return nil
}
func (f *fakeQueue) Fail(_ context.Context, j Job, terminal bool, _ time.Duration, _ string) error {
	f.failed++
	f.terminal = terminal
	if !terminal {
		f.current.Attempts++
		f.jobs = append(f.jobs, f.current)
	}
	return nil
}
func (f *fakeQueue) Stats(context.Context) (QueueStats, error) { return QueueStats{}, nil }

type fakeEngine struct {
	calls int
	fail  bool
}
type permanentFailure struct{}

func (permanentFailure) Error() string             { return "invalid input" }
func (permanentFailure) Retryable() bool           { return false }
func (permanentFailure) RetryAfter() time.Duration { return 0 }
func (e *fakeEngine) DecideWithKey(_ context.Context, r decision.Request, key string) (decision.Response, error) {
	e.calls++
	if e.fail {
		e.fail = false
		return decision.Response{}, permanentFailure{}
	}
	return decision.Response{ApplicationID: r.ApplicationID, Status: "NEED_INFO"}, nil
}
func testJob(id string) Job {
	return Job{ID: id, ApplicationID: id, Attempts: 1, Payload: Payload{Request: decision.Request{ApplicationID: id, InputSnapshot: &decision.InputSnapshot{ID: id, EvaluationDate: "2026-10-23"}}}}
}
func testWorker(q *fakeQueue, e *fakeEngine) *Worker {
	return &Worker{Repo: q, Engine: e, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: Config{Lease: time.Minute, Heartbeat: time.Second, MaxAttempts: 8, ReviewSLA: time.Hour, OfferValidity: 24 * time.Hour}}
}
func TestCachedResponseSurvivesOutcomeSaveFailure(t *testing.T) {
	q := &fakeQueue{jobs: []Job{testJob("one")}, failSave: true}
	e := &fakeEngine{}
	w := testWorker(q, e)
	for range 2 {
		if ok, err := w.ProcessNext(context.Background()); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	if e.calls != 1 || q.completed != 1 || q.failed != 1 || q.terminal {
		t.Fatal(e.calls, q)
	}
}
func TestPermanentFailureDoesNotBlockNextJob(t *testing.T) {
	q := &fakeQueue{jobs: []Job{testJob("bad"), testJob("good")}}
	e := &fakeEngine{fail: true}
	w := testWorker(q, e)
	for range 2 {
		if ok, err := w.ProcessNext(context.Background()); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	if !q.terminal || q.failed != 1 || q.completed != 1 {
		t.Fatal(q)
	}
}
