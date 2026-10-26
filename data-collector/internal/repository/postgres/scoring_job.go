package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/dispatch"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

type ScoringJobRepository struct{ Pool *pgxpool.Pool }

const readyApplicationSQL = `SELECT a.id,a.status FROM applications a
 WHERE (a.status IN ('VERIFYING','ENRICHING') OR (a.status='SCORING' AND NOT EXISTS(SELECT 1 FROM scoring_jobs j WHERE j.application_id=a.id)))
 AND NOT EXISTS(SELECT 1 FROM scoring_jobs j WHERE j.application_id=a.id AND j.status IN ('PENDING','RUNNING','RETRY_WAIT'))
 AND EXISTS(SELECT 1 FROM collection_tasks t WHERE t.application_id=a.id)
 AND (SELECT status FROM collection_tasks t WHERE t.application_id=a.id ORDER BY t.created_at DESC,t.id DESC LIMIT 1) IN ('COMPLETED','FAILED')
 AND NOT EXISTS(SELECT 1 FROM documents d WHERE d.application_id=a.id AND d.deleted_at IS NULL AND (d.signature_state='PENDING'
  OR (d.financial_generation>0 AND NOT EXISTS(SELECT 1 FROM financial_document_jobs f WHERE f.document_id=d.id AND f.generation=d.financial_generation AND f.status IN ('COMPLETED','FAILED')))))
 ORDER BY a.updated_at,a.id FOR UPDATE OF a SKIP LOCKED LIMIT 1`

func (r ScoringJobRepository) EnqueueNext(ctx context.Context, date time.Time) (bool, error) {
	created := false
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		var app, status string
		if err := tx.QueryRow(ctx, readyApplicationSQL).Scan(&app, &status); err != nil {
			return err
		}
		// Recheck after locking: a READ COMMITTED locking SELECT can re-evaluate
		// an updated parent while its subqueries still use the earlier statement snapshot.
		var anyJob, activeJob bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scoring_jobs WHERE application_id=$1),EXISTS(SELECT 1 FROM scoring_jobs WHERE application_id=$1 AND status IN ('PENDING','RUNNING','RETRY_WAIT'))`, app).Scan(&anyJob, &activeJob); err != nil {
			return err
		}
		if activeJob || status == "SCORING" && anyJob {
			return pgx.ErrNoRows
		}
		repo := &DecisionRepository{pool: tx, apps: &ApplicationRepository{pool: tx}}
		in, err := repo.Input(ctx, app, date)
		if err != nil {
			var syntax *json.SyntaxError
			var wrongType *json.UnmarshalTypeError
			if errors.As(err, &syntax) || errors.As(err, &wrongType) {
				id := "score_" + ulid.Make().String()
				if _, saveErr := tx.Exec(ctx, `INSERT INTO scoring_jobs(id,application_id,round,status,snapshot,last_error,finished_at)
     SELECT $1,$2,coalesce((SELECT max(round) FROM scoring_jobs WHERE application_id=$2),0)+1,'FAILED',jsonb_build_object('form',form),'INVALID_FORM_DATA',now() FROM applications WHERE id=$2`, id, app); saveErr != nil {
					return saveErr
				}
				if _, saveErr := tx.Exec(ctx, `UPDATE applications SET status='MANUAL_REVIEW',deadline_at=now()+interval '4 hours',version=version+1,updated_at=now() WHERE id=$1`, app); saveErr != nil {
					return saveErr
				}
				created = true
				return insertEvent(ctx, tx, appEvent{ApplicationID: app, Type: "scoring.failed", From: domain.ApplicationStatus(status), To: domain.ApplicationManualReview, Payload: map[string]any{"job_id": id, "reason": "INVALID_FORM_DATA", "technical": true}})
			}
			return err
		}
		if in.FactsDate.After(date) {
			in.FactsDate = date
		}
		var collection string
		if err = tx.QueryRow(ctx, `SELECT coalesce(rc.id,'') FROM collection_tasks t LEFT JOIN raw_collections rc ON rc.task_id=t.id WHERE t.application_id=$1 ORDER BY t.created_at DESC,t.id DESC LIMIT 1`, app).Scan(&collection); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT d.id,d.sha256,d.financial_generation,d.signature_state,d.signature_verdict,r.report,r.integrity_status,f.status,fr.result
   FROM documents d LEFT JOIN financial_document_jobs f ON f.document_id=d.id AND f.generation=d.financial_generation
   LEFT JOIN financial_document_results fr ON fr.job_id=f.id
   LEFT JOIN LATERAL(SELECT sr.report,sr.integrity_status FROM signature_reports sr WHERE sr.document_id=d.id AND (d.financial_generation=0 OR sr.id=fr.signature_report_id) ORDER BY sr.verified_at DESC,sr.id DESC LIMIT 1) r ON true
   WHERE d.application_id=$1 AND d.deleted_at IS NULL ORDER BY d.id`, app)
		if err != nil {
			return err
		}
		docs := []decision.SnapshotDocument{}
		for rows.Next() {
			var d decision.SnapshotDocument
			var result []byte
			if err = rows.Scan(&d.ID, &d.SHA256, &d.Generation, &d.SignatureState, &d.SignatureVerdict, &d.SignatureReport, &d.IntegrityStatus, &d.FinancialStatus, &result); err != nil {
				rows.Close()
				return err
			}
			if result != nil {
				if err = json.Unmarshal(result, &d.FinancialResult); err != nil {
					rows.Close()
					return err
				}
			}
			docs = append(docs, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		id := "score_" + ulid.Make().String()
		req, facts, buildErr := decision.BuildSnapshot(in, id, collection, docs)
		if buildErr == nil {
			err = tx.QueryRow(ctx, `SELECT coalesce(rc.sources,jsonb_build_object('state',t.status,'error',t.error)) FROM collection_tasks t LEFT JOIN raw_collections rc ON rc.task_id=t.id WHERE t.application_id=$1 ORDER BY t.created_at DESC,t.id DESC LIMIT 1`, app).Scan(&req.InputSnapshot.SourceStates)
			if err != nil {
				return err
			}
		}
		answered, err := repo.AnsweredCodes(ctx, app)
		if err != nil {
			return err
		}
		payload, err := dispatch.EncodePayload(dispatch.Payload{Request: req, Facts: facts, Source: in, Answered: answered})
		if err != nil {
			return err
		}
		jobStatus := "PENDING"
		if buildErr != nil {
			jobStatus = "FAILED"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO scoring_jobs(id,application_id,round,snapshot,status,last_error,finished_at)
   SELECT $1,$2,coalesce(max(round),0)+1,$3,$4,CASE WHEN $4='FAILED' THEN 'INVALID_SNAPSHOT_DATA' END,CASE WHEN $4='FAILED' THEN now() END FROM scoring_jobs WHERE application_id=$2`, id, app, payload, jobStatus); err != nil {
			return err
		}
		next := domain.ApplicationScoring
		if buildErr != nil {
			next = domain.ApplicationManualReview
		}
		if _, err = tx.Exec(ctx, `UPDATE applications SET status=$2,version=version+1,updated_at=now(),deadline_at=CASE WHEN $2='MANUAL_REVIEW' THEN now()+interval '4 hours' END WHERE id=$1`, app, string(next)); err != nil {
			return err
		}
		created = true
		eventType := "scoring.started"
		if buildErr != nil {
			eventType = "scoring.failed"
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: app, Type: eventType, From: domain.ApplicationStatus(status), To: next, Payload: map[string]any{"job_id": id, "preparation_failed": buildErr != nil}})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return created, err
}
func (r ScoringJobRepository) Claim(ctx context.Context, lease time.Duration) (dispatch.Job, bool, error) {
	var j dispatch.Job
	j.Token = ulid.Make().String()
	var payload, response []byte
	err := r.Pool.QueryRow(ctx, `WITH next AS (SELECT id FROM scoring_jobs WHERE (status IN ('PENDING','RETRY_WAIT') AND next_attempt_at<=now()) OR (status='RUNNING' AND lease_until<now()) ORDER BY next_attempt_at,created_at FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE scoring_jobs j SET status='RUNNING',attempts=attempts+1,owner_token=$1,lease_until=now()+$2::interval FROM next WHERE j.id=next.id
 RETURNING j.id,j.application_id,j.round,j.attempts,j.snapshot,j.response`, j.Token, lease.String()).Scan(&j.ID, &j.ApplicationID, &j.Round, &j.Attempts, &payload, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	if err = json.Unmarshal(payload, &j.Payload); err != nil {
		return j, true, err
	}
	if response != nil {
		if err = json.Unmarshal(response, &j.Response); err != nil {
			return j, true, err
		}
	}
	return j, true, nil
}
func (r ScoringJobRepository) Renew(ctx context.Context, j dispatch.Job, lease time.Duration) error {
	tag, err := r.Pool.Exec(ctx, `UPDATE scoring_jobs SET lease_until=now()+$3::interval WHERE id=$1 AND owner_token=$2 AND status='RUNNING' AND lease_until>now()`, j.ID, j.Token, lease.String())
	if err == nil && tag.RowsAffected() != 1 {
		return dispatch.ErrLeaseLost
	}
	return err
}
func (r ScoringJobRepository) StoreResponse(ctx context.Context, j dispatch.Job, resp decision.Response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	tag, err := r.Pool.Exec(ctx, `UPDATE scoring_jobs SET response=$3 WHERE id=$1 AND owner_token=$2 AND status='RUNNING' AND lease_until>now()`, j.ID, j.Token, raw)
	if err == nil && tag.RowsAffected() != 1 {
		return dispatch.ErrLeaseLost
	}
	return err
}
func (r ScoringJobRepository) Complete(ctx context.Context, j dispatch.Job, o decision.Outcome, details any, deadline *time.Time, valid time.Time) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		var owned bool
		err := tx.QueryRow(ctx, `SELECT owner_token=$2 AND status='RUNNING' AND lease_until>now() FROM scoring_jobs WHERE id=$1 FOR UPDATE`, j.ID, j.Token).Scan(&owned)
		if err != nil {
			return err
		}
		if !owned {
			return dispatch.ErrLeaseLost
		}
		var status string
		if err = tx.QueryRow(ctx, `SELECT status FROM applications WHERE id=$1 FOR UPDATE`, j.ApplicationID).Scan(&status); err != nil {
			return err
		}
		if status != "SCORING" {
			return dispatch.ErrApplicationChanged
		}
		if err = saveOutcome(ctx, tx, j.ApplicationID, j.ID, o, details, deadline, valid); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE scoring_jobs SET status='COMPLETED',finished_at=now(),lease_until=NULL,owner_token=NULL,last_error=NULL WHERE id=$1`, j.ID)
		return err
	})
}
func (r ScoringJobRepository) Fail(ctx context.Context, j dispatch.Job, terminal bool, delay time.Duration, reason string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		state := "RETRY_WAIT"
		if terminal {
			state = "FAILED"
		}
		tag, err := tx.Exec(ctx, `UPDATE scoring_jobs SET status=$3,last_error=$4,next_attempt_at=now()+$5::interval,lease_until=NULL,owner_token=NULL,finished_at=CASE WHEN $3='FAILED' THEN now() END WHERE id=$1 AND owner_token=$2 AND status='RUNNING' AND lease_until>now()`, j.ID, j.Token, state, reason, delay.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return dispatch.ErrLeaseLost
		}
		if terminal {
			tag, err = tx.Exec(ctx, `UPDATE applications SET status='MANUAL_REVIEW',deadline_at=now()+interval '4 hours',version=version+1,updated_at=now() WHERE id=$1 AND status='SCORING'`, j.ApplicationID)
			if err != nil {
				return err
			}

		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: j.ApplicationID, Type: "scoring." + map[bool]string{true: "failed", false: "retry_scheduled"}[terminal], Payload: map[string]any{"job_id": j.ID, "reason": reason, "attempt": j.Attempts, "technical": true}})
	})
}
func (r ScoringJobRepository) Stats(ctx context.Context) (dispatch.QueueStats, error) {
	var s dispatch.QueueStats
	err := r.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status IN ('PENDING','RETRY_WAIT')),count(*) FILTER(WHERE status='RUNNING'),count(*) FILTER(WHERE status='RETRY_WAIT'),count(*) FILTER(WHERE status='FAILED'),coalesce(extract(epoch FROM now()-min(created_at) FILTER(WHERE status IN ('PENDING','RETRY_WAIT'))),0)::float8 FROM scoring_jobs`).Scan(&s.Pending, &s.Running, &s.Retries, &s.Failed, &s.OldestSeconds)
	return s, err
}
