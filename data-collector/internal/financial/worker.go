package financial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var ErrJobBusy = errors.New("financial task is already running")
var ErrJobNotRunning = errors.New("financial task attempt is no longer current")
var ErrProcessingFailed = errors.New("financial processing failed; request a new verification")

// Jobs separates persistence from the Kafka handler, like the collection worker's Tasks.
type Jobs interface {
	Claim(context.Context, string, int, time.Duration) (Job, bool, error)
	Load(context.Context, Job) (Document, []Document, error)
	Complete(context.Context, Job, Result) error
	Fail(context.Context, Job, string) error
}
type DocumentProcessor interface {
	Process(context.Context, Job, Document, []Document) (Result, error)
}
type WorkerConfig struct {
	ProcessTimeout time.Duration
	MaxAttempts    int
}
type Worker struct {
	jobs      Jobs
	processor DocumentProcessor
	cfg       WorkerConfig
	log       *slog.Logger
}

func NewWorker(jobs Jobs, processor DocumentProcessor, cfg WorkerConfig, log *slog.Logger) *Worker {
	if cfg.ProcessTimeout <= 0 {
		cfg.ProcessTimeout = 90 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	return &Worker{jobs: jobs, processor: processor, cfg: cfg, log: log}
}

// Handle acknowledges only persisted terminal outcomes. Infrastructure errors return
// to the shared Kafka consumer, which leaves the offset uncommitted for redelivery.
func (w *Worker) Handle(ctx context.Context, value []byte) error {
	var ev Event
	if json.Unmarshal(value, &ev) != nil || ev.Event != EventRequested || ev.TaskID == "" {
		return nil
	}
	task, ok, err := w.jobs.Claim(ctx, ev.TaskID, w.cfg.MaxAttempts, w.cfg.ProcessTimeout+30*time.Second)
	if err != nil {
		return fmt.Errorf("claim financial task: %w", err)
	}
	if !ok || task.Status == "FAILED" {
		return nil
	}
	log := w.log.With("task", task.ID, "document", task.DocumentID, "attempt", task.Attempts)
	log.InfoContext(ctx, "financial processing started")
	// Finish a claimed task on shutdown, but bound all work by explicit timeouts.
	base := context.WithoutCancel(ctx)
	workCtx, cancel := context.WithTimeout(base, w.cfg.ProcessTimeout)
	defer cancel()
	d, sigs, err := w.jobs.Load(workCtx, task)
	if err != nil {
		return fmt.Errorf("load financial task: %w", err)
	}
	result, processErr := w.process(workCtx, task, d, sigs)
	saveCtx, saveCancel := context.WithTimeout(base, 10*time.Second)
	defer saveCancel()
	if processErr != nil {
		if err = w.jobs.Fail(saveCtx, task, "DOCUMENT_PROCESSING_FAILED"); errors.Is(err, ErrJobNotRunning) {
			return nil
		} else if err != nil {
			return fmt.Errorf("persist financial failure: %w", err)
		}
		log.WarnContext(base, "financial processing failed")
		return nil
	}
	if err = w.jobs.Complete(saveCtx, task, result); errors.Is(err, ErrJobNotRunning) {
		return nil
	} else if err != nil {
		return fmt.Errorf("complete financial task: %w", err)
	}
	log.InfoContext(base, "financial processing completed", "result", result.ID)
	return nil
}
func (w *Worker) process(ctx context.Context, j Job, d Document, s []Document) (r Result, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("financial processor panic")
		}
	}()
	return w.processor.Process(ctx, j, d, s)
}
