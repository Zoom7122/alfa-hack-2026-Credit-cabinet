// Package dispatch executes durable scoring rounds. It owns no replica-local queue.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
)

var ErrLeaseLost = errors.New("scoring job lease lost")
var ErrApplicationChanged = errors.New("application left scoring")

type Payload struct {
	Request  decision.Request `json:"request"`
	Facts    decision.Facts   `json:"facts"`
	Source   decision.Input   `json:"source"`
	Answered map[string]bool  `json:"answered"`
}
type Job struct {
	ID, ApplicationID, Token string
	Round, Attempts          int
	Payload                  Payload
	Response                 *decision.Response
}
type QueueStats struct {
	Pending, Running, Retries, Failed int64
	OldestSeconds                     float64
}
type Repository interface {
	EnqueueNext(context.Context, time.Time) (bool, error)
	Claim(context.Context, time.Duration) (Job, bool, error)
	Renew(context.Context, Job, time.Duration) error
	StoreResponse(context.Context, Job, decision.Response) error
	Complete(context.Context, Job, decision.Outcome, any, *time.Time, time.Time) error
	Fail(context.Context, Job, bool, time.Duration, string) error
	Stats(context.Context) (QueueStats, error)
}
type Engine interface {
	DecideWithKey(context.Context, decision.Request, string) (decision.Response, error)
}
type Config struct {
	Lease, Heartbeat, ReviewSLA, OfferValidity time.Duration
	MaxAttempts                                int
	EvaluationDate                             time.Time
}
type Worker struct {
	Repo                       Repository
	Engine                     Engine
	Config                     Config
	Log                        *slog.Logger
	Processed, Errors, Retried atomic.Int64
	DurationNanos              atomic.Int64
}

func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	date := w.Config.EvaluationDate
	if date.IsZero() {
		date = time.Now().UTC()
	}
	enqueueCtx, enqueueCancel := context.WithTimeout(ctx, 10*time.Second)
	_, enqueueErr := w.Repo.EnqueueNext(enqueueCtx, date)
	enqueueCancel()
	if err := enqueueErr; err != nil {
		return false, err
	}
	claimCtx, claimCancel := context.WithTimeout(ctx, 5*time.Second)
	job, ok, err := w.Repo.Claim(claimCtx, w.Config.Lease)
	claimCancel()
	if err != nil || !ok {
		return ok, err
	}
	started := time.Now()
	defer func() { w.DurationNanos.Add(time.Since(started).Nanoseconds()) }()
	log := w.Log.With("application_id", job.ApplicationID, "job_id", job.ID, "round", job.Round, "attempt", job.Attempts)
	// Crash loops are bounded too. A persisted response only needs local finalization.
	if job.Attempts > w.Config.MaxAttempts && job.Response == nil {
		return true, w.Repo.Fail(ctx, job, true, 0, "ATTEMPTS_EXHAUSTED")
	}
	work, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(w.Config.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-work.Done():
				return
			case <-ticker.C:
				if err := w.Repo.Renew(work, job, w.Config.Lease); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	resp := job.Response
	if resp == nil {
		out, callErr := w.Engine.DecideWithKey(work, job.Payload.Request, job.ID)
		err = callErr
		if err == nil {
			resp = &out
			err = w.Repo.StoreResponse(work, job, out)
		}
	}
	close(done)
	<-stopped
	cancel()
	save, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	if err != nil {
		if errors.Is(err, ErrLeaseLost) {
			return true, nil
		}
		w.Errors.Add(1)
		permanent := false
		delay := backoff(job.Attempts)
		var failure interface {
			Retryable() bool
			RetryAfter() time.Duration
		}
		if errors.As(err, &failure) {
			permanent = !failure.Retryable()
			if after := failure.RetryAfter(); after > delay {
				delay = after
			}
		}
		terminal := permanent || job.Attempts >= w.Config.MaxAttempts
		if !terminal {
			w.Retried.Add(1)
		}
		log.Warn("scoring attempt failed", "error", err, "terminal", terminal)
		return true, w.Repo.Fail(save, job, terminal, delay, errorCode(err))
	}
	outcome := decision.Interpret(*resp, job.Payload.Facts)
	outcome = decision.WithoutAnswered(outcome, job.Payload.Answered)
	var deadline *time.Time
	if outcome.Status == "MANUAL_REVIEW" {
		d := time.Now().Add(w.Config.ReviewSLA)
		deadline = &d
	}
	details := map[string]any{"engine": resp, "request": job.Payload.Request, "findings": outcome.Findings, "facts": job.Payload.Facts, "proposed_offer": outcome.Offer, "evaluated_at": job.Payload.Request.InputSnapshot.EvaluationDate, "scoring_job_id": job.ID}
	if err = w.Repo.Complete(save, job, outcome, details, deadline, time.Now().Add(w.Config.OfferValidity)); err != nil {
		log.Warn("save scoring outcome failed", "error", err)
		if errors.Is(err, ErrLeaseLost) {
			return true, nil
		}
		w.Errors.Add(1)
		terminal := job.Attempts >= w.Config.MaxAttempts || errors.Is(err, ErrApplicationChanged)
		return true, w.Repo.Fail(save, job, terminal, backoff(job.Attempts), "OUTCOME_SAVE_FAILED")
	}
	w.Processed.Add(1)
	log.Info("scoring round completed", "status", outcome.Status)
	return true, nil
}
func backoff(attempt int) time.Duration {
	n := min(max(attempt-1, 0), 6)
	base := min(2*time.Second*time.Duration(1<<n), 120*time.Second)
	return min(base+time.Duration(rand.Int64N(int64(base/4)+1)), 120*time.Second)
}
func errorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return coded.ErrorCode()
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	return "SCORING_TRANSPORT_FAILED"
}
func EncodePayload(p Payload) ([]byte, error) { return json.Marshal(p) }
