package financial

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

var workerTestLog = slog.New(slog.NewTextHandler(io.Discard, nil))

type processorFunc func(context.Context, Job, Document, []Document) (Result, error)

func (f processorFunc) Process(ctx context.Context, j Job, d Document, s []Document) (Result, error) {
	return f(ctx, j, d, s)
}

type fakeJobs struct {
	claimErr, loadErr, completeErr, failErr error
	skip                                    bool
	claims, completed, failed               int
	afterClaim                              func()
}

func (s *fakeJobs) Claim(context.Context, string, int, time.Duration) (Job, bool, error) {
	s.claims++
	if s.afterClaim != nil {
		s.afterClaim()
	}
	return Job{ID: "job", Status: "RUNNING", Attempts: 1}, !s.skip, s.claimErr
}
func (s *fakeJobs) Load(context.Context, Job) (Document, []Document, error) {
	return Document{}, nil, s.loadErr
}
func (s *fakeJobs) Complete(ctx context.Context, _ Job, _ Result) error {
	s.completed++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.completeErr
}
func (s *fakeJobs) Fail(ctx context.Context, _ Job, reason string) error {
	s.failed++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if reason != "DOCUMENT_PROCESSING_FAILED" {
		return errors.New("unexpected failure reason")
	}
	return s.failErr
}
func requested(id string) []byte {
	b, _ := json.Marshal(Event{Event: EventRequested, TaskID: id})
	return b
}

func TestFinancialWorkerAcknowledgesOnlyDurableOutcome(t *testing.T) {
	storageErr := errors.New("storage unavailable")
	for _, tt := range []struct {
		name                         string
		store                        fakeJobs
		processErr                   error
		panic                        bool
		wantErr                      error
		processed, completed, failed int
	}{
		{name: "completed", processed: 1, completed: 1},
		{name: "duplicate terminal task", store: fakeJobs{skip: true}},
		{name: "active lease", store: fakeJobs{claimErr: ErrJobBusy}, wantErr: ErrJobBusy},
		{name: "claim unavailable", store: fakeJobs{claimErr: storageErr}, wantErr: storageErr},
		{name: "load unavailable", store: fakeJobs{loadErr: storageErr}, wantErr: storageErr},
		{name: "save unavailable", store: fakeJobs{completeErr: storageErr}, wantErr: storageErr, processed: 1, completed: 1},
		{name: "processing failed", processErr: errors.New("broken document"), processed: 1, failed: 1},
		{name: "panic", panic: true, processed: 1, failed: 1},
		{name: "failure save unavailable", processErr: errors.New("broken document"), store: fakeJobs{failErr: storageErr}, wantErr: storageErr, processed: 1, failed: 1},
		{name: "stale completion", store: fakeJobs{completeErr: ErrJobNotRunning}, processed: 1, completed: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			processed := 0
			p := processorFunc(func(context.Context, Job, Document, []Document) (Result, error) {
				processed++
				if tt.panic {
					panic("unexpected processor error")
				}
				return Result{}, tt.processErr
			})
			w := NewWorker(&tt.store, p, WorkerConfig{}, workerTestLog)
			if err := w.Handle(context.Background(), requested("job")); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error %v, want %v", err, tt.wantErr)
			}
			if processed != tt.processed || tt.store.completed != tt.completed || tt.store.failed != tt.failed {
				t.Fatalf("processed/completed/failed = %d/%d/%d", processed, tt.store.completed, tt.store.failed)
			}
		})
	}
}

func TestFinancialWorkerIgnoresOtherEvents(t *testing.T) {
	s := &fakeJobs{}
	w := NewWorker(s, nil, WorkerConfig{}, workerTestLog)
	for _, msg := range []string{`broken`, `{"event":"collection.requested","task_id":"job"}`, `{"event":"financial.document.requested"}`} {
		if err := w.Handle(context.Background(), []byte(msg)); err != nil {
			t.Fatal(err)
		}
	}
	if s.claims != 0 {
		t.Fatal("unrelated messages reached financial repository")
	}
}

func TestFinancialWorkerFinishesClaimOnShutdownWithBoundedTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &fakeJobs{afterClaim: cancel}
	p := processorFunc(func(ctx context.Context, _ Job, _ Document, _ []Document) (Result, error) {
		if ctx.Err() != nil {
			t.Fatal("shutdown canceled claimed work immediately")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("processing has no deadline")
		}
		<-ctx.Done()
		return Result{}, ctx.Err()
	})
	w := NewWorker(s, p, WorkerConfig{ProcessTimeout: 20 * time.Millisecond}, workerTestLog)
	if err := w.Handle(ctx, requested("job")); err != nil {
		t.Fatal(err)
	}
	if s.failed != 1 {
		t.Fatal("timeout failure not persisted after shutdown")
	}
}
