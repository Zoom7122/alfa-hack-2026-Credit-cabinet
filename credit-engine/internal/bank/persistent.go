package bank

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/runtimehash"
	"log/slog"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Operations keeps the in-memory implementation usable in isolated unit tests.
// Executable services use PersistentService and never silently fall back to memory.
type Operations interface {
	Check(context.Context, contracts.BankCheckRequest) (contracts.BankCheckResponse, error)
	ManualReview(context.Context, contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error)
	Snapshot() contracts.BankStateResponse
	Loans() ([]Loan, error)
	PendingEvents() ([]contracts.AdminEvent, error)
}
type PersistentService struct {
	pool     *pgxpool.Pool
	policy   config.Policy
	clock    clock.Clock
	notifier Notifier
}

func NewPersistent(ctx context.Context, pool *pgxpool.Pool, p config.Policy, state contracts.BankState, c clock.Clock, n Notifier) (*PersistentService, error) {
	if _, err := NewService(p, state, c, nil); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if _, err = pool.Exec(ctx, `INSERT INTO credit_engine_runtime.bank_state(id,policy_id,state) VALUES('default',$1,$2) ON CONFLICT DO NOTHING`, p.ID, raw); err != nil {
		return nil, err
	}
	var policy string
	if err = pool.QueryRow(ctx, `SELECT policy_id FROM credit_engine_runtime.bank_state WHERE id='default'`).Scan(&policy); err != nil {
		return nil, err
	}
	if policy != p.ID {
		return nil, fmt.Errorf("bank persistent policy mismatch")
	}
	return &PersistentService{pool: pool, policy: p, clock: c, notifier: n}, nil
}
func (s *PersistentService) Check(ctx context.Context, r contracts.BankCheckRequest) (contracts.BankCheckResponse, error) {
	var out contracts.BankCheckResponse
	err := s.apply(ctx, r.ApplicationID, r.OperationID, "CHECK", r, &out)
	return out, err
}
func (s *PersistentService) ManualReview(ctx context.Context, r contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error) {
	var out contracts.ManualReviewResponse
	err := s.apply(ctx, r.ApplicationID, r.OperationID, "MANUAL", r, &out)
	return out, err
}
func (s *PersistentService) apply(ctx context.Context, app string, op *string, kind string, request, out any) error {
	key := "legacy:" + app
	if op != nil {
		if *op == "" || len(*op) > 200 {
			return &Error{Status: 422, Code: "INVALID_OPERATION_ID"}
		}
		key = *op
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return &Error{Status: 422, Code: "INVALID_VALUE"}
	}
	hash, err := runtimehash.Digest(raw)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var stateRaw []byte
		var policy string
		if err := tx.QueryRow(ctx, `SELECT policy_id,state FROM credit_engine_runtime.bank_state WHERE id='default' FOR UPDATE`).Scan(&policy, &stateRaw); err != nil {
			return err
		}
		if policy != s.policy.ID {
			return conflict("POLICY_MISMATCH")
		}
		var oldHash, oldKind string
		var response []byte
		err := tx.QueryRow(ctx, `SELECT request_hash,kind,response FROM credit_engine_runtime.bank_operations WHERE operation_id=$1`, key).Scan(&oldHash, &oldKind, &response)
		if err == nil {
			if oldHash != hash || oldKind != kind {
				return conflict("APPLICATION_CONFLICT")
			}
			return json.Unmarshal(response, out)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var state contracts.BankState
		if err = json.Unmarshal(stateRaw, &state); err != nil {
			return err
		}
		// Reuse the validated projection logic without network I/O or replica-local state.
		core, err := NewService(s.policy, state, s.clock, nil)
		if err != nil {
			return err
		}
		if kind == "CHECK" {
			_, err = core.Check(ctx, request.(contracts.BankCheckRequest))
		} else {
			_, err = core.ManualReview(ctx, request.(contracts.ManualReviewRequest))
		}
		if err != nil {
			return err
		}
		e := core.entries[app]
		prefix := app
		if op != nil {
			prefix = *op
		}
		e.event.EventID = prefix + ":" + string(e.event.Kind)
		if kind == "CHECK" {
			e.checkResult.EventID = e.event.EventID
			response, err = json.Marshal(e.checkResult)
		} else {
			e.manualResult.EventID = e.event.EventID
			response, err = json.Marshal(e.manualResult)
		}
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credit_engine_runtime.bank_operations(operation_id,application_id,kind,request_hash,request,response) VALUES($1,$2,$3,$4,$5,$6)`, key, app, kind, hash, raw, response); err != nil {
			return err
		}
		if kind == "CHECK" && e.checkResult.Verdict == contracts.BankVerdictIssued {
			loan, _ := json.Marshal(Loan{ApplicationID: app, LoanID: *e.checkResult.LoanID, Borrower: e.check.Borrower, Terms: e.check.Terms})
			if _, err = tx.Exec(ctx, `INSERT INTO credit_engine_runtime.bank_loans(application_id,operation_id,loan) VALUES($1,$2,$3)`, app, key, loan); err != nil {
				return err
			}
			stateRaw, err = json.Marshal(core.state)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE credit_engine_runtime.bank_state SET state=$1 WHERE id='default'`, stateRaw); err != nil {
				return err
			}
		}
		event, err := json.Marshal(e.event)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credit_engine_runtime.bank_outbox(event_id,operation_id,payload) VALUES($1,$2,$3)`, e.event.EventID, key, event); err != nil {
			return err
		}
		return json.Unmarshal(response, out)
	})
}
func (s *PersistentService) SnapshotContext(ctx context.Context) (contracts.BankStateResponse, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT state FROM credit_engine_runtime.bank_state WHERE id='default'`).Scan(&raw); err != nil {
		return contracts.BankStateResponse{}, err
	}
	var state contracts.BankState
	if err := json.Unmarshal(raw, &state); err != nil {
		return contracts.BankStateResponse{}, err
	}
	core, err := NewService(s.policy, state, s.clock, nil)
	if err != nil {
		return contracts.BankStateResponse{}, err
	}
	return core.Snapshot(), nil
}
func (s *PersistentService) Snapshot() contracts.BankStateResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := s.SnapshotContext(ctx)
	return out
}
func (s *PersistentService) Loans() ([]Loan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT loan FROM credit_engine_runtime.bank_loans ORDER BY application_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Loan{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var l Loan
		if err = json.Unmarshal(raw, &l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *PersistentService) PendingEvents() ([]contracts.AdminEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT payload FROM credit_engine_runtime.bank_outbox WHERE delivered_at IS NULL ORDER BY event_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.AdminEvent{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var e contracts.AdminEvent
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PublishNext claims in a short transaction, delivers outside it, then fences the acknowledgement.
func (s *PersistentService) PublishNext(ctx context.Context) (bool, error) {
	if s.notifier == nil {
		return false, nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return false, err
	}
	token := hex.EncodeToString(b[:])
	var id, operation string
	var raw []byte
	err := s.pool.QueryRow(ctx, `WITH next AS (SELECT event_id FROM credit_engine_runtime.bank_outbox WHERE delivered_at IS NULL AND next_attempt_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_attempt_at,event_id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE credit_engine_runtime.bank_outbox o SET owner_token=$1,lease_until=now()+interval '30 seconds',attempts=attempts+1 FROM next WHERE o.event_id=next.event_id RETURNING o.event_id,o.operation_id,o.payload`, token).Scan(&id, &operation, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var ev contracts.AdminEvent
	if err = json.Unmarshal(raw, &ev); err != nil {
		return true, err
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	ack, sendErr := s.notifier.Notify(call, ev)
	cancel()
	delivered := sendErr == nil && ack.Accepted && ack.EventID == id
	if !delivered {
		slog.WarnContext(ctx, "bank notification pending", "event_id", id, "err", sendErr)
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = pgx.BeginFunc(save, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(save, `UPDATE credit_engine_runtime.bank_outbox SET delivered_at=CASE WHEN $3 THEN now() ELSE NULL END,next_attempt_at=now()+interval '2 seconds',lease_until=NULL,owner_token=NULL WHERE event_id=$1 AND owner_token=$2 AND lease_until>now()`, id, token, delivered)
		if err != nil {
			return err
		}
		if delivered && tag.RowsAffected() == 1 {
			_, err = tx.Exec(save, `UPDATE credit_engine_runtime.bank_operations SET response=jsonb_set(response,'{notification_status}','"DELIVERED"'::jsonb) WHERE operation_id=$1`, operation)
		}
		return err
	})
	return true, err
}
func (s *PersistentService) RunOutbox(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for {
			ok, err := s.PublishNext(ctx)
			if err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "bank outbox poll failed", "err", err)
			}
			if !ok || err != nil || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
