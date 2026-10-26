//go:build integration

package postgres

import (
	"context"
	"errors"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/dispatch"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
	"testing"
	"time"
)

func seedScoringApp(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	_, err := p.Exec(context.Background(), `INSERT INTO users(id,full_name,role) VALUES('usr','Test','CLIENT');
 INSERT INTO companies(id,inn,type,name) VALUES('co','1111111111','LEGAL_ENTITY','Test');
 INSERT INTO applications(id,number,company_id,created_by,status,form) VALUES('app','A-test','co','usr','VERIFYING','{}');
 INSERT INTO collection_tasks(id,questionnaire_id,inn,status,request,application_id,finished_at) VALUES('task','q','1111111111','FAILED','{}','app',now());`)
	if err != nil {
		t.Fatal(err)
	}
}
func TestScoringJobsReadinessFencingAndAtomicOutcome(t *testing.T) {
	p := scoringTestDB(t)
	seedScoringApp(t, p)
	ctx := context.Background()
	r := ScoringJobRepository{Pool: p}
	exec := func(sql string) {
		t.Helper()
		if _, err := p.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO documents(id,application_id,type,file_name,mime_type,size_bytes,sha256,storage_key,signature_state,financial_generation)
 VALUES('doc','app','FINANCIAL_STATEMENTS','report.pdf','application/pdf',10,'hash','test','PENDING',2);
 INSERT INTO financial_document_jobs(id,document_id,generation,applicant,signature_ids,status) VALUES('old','doc',1,'{}','[]','COMPLETED'),('current','doc',2,'{}','[]','RUNNING');`)
	for _, sql := range []string{"SELECT 1", "UPDATE documents SET signature_state='DONE' WHERE id='doc'"} {
		exec(sql)
		if ok, err := r.EnqueueNext(ctx, time.Now()); err != nil || ok {
			t.Fatalf("admitted unfinished generation: %v %v", ok, err)
		}
	}
	exec(`UPDATE financial_document_jobs SET status='FAILED' WHERE id='current'; UPDATE documents SET signature_state='FAILED' WHERE id='doc'`)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if _, err := r.EnqueueNext(ctx, time.Now()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	if err := p.QueryRow(ctx, "SELECT count(*) FROM scoring_jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("jobs %d %v", count, err)
	}
	first, ok, err := r.Claim(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if first.Payload.Request.InputSnapshot.CollectionID != "" || len(first.Payload.Request.InputSnapshot.Statements.AnnualReports) != 0 {
		t.Fatal("failed collection reused older data")
	}
	exec("UPDATE scoring_jobs SET lease_until=now()-interval '1 second'")
	second, ok, err := r.Claim(ctx, time.Minute)
	if err != nil || !ok || second.Token == first.Token {
		t.Fatal(ok, err)
	}
	if err = r.Renew(ctx, first, time.Minute); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatalf("old renewal: %v", err)
	}
	outcome := decision.Outcome{Status: domain.ApplicationWaitingForInfo, AutoOutcome: "NEED_INFO", Questions: []decision.Question{{Code: "DATA", Title: "Data", Required: true, Answer: decision.Answer{Type: "TEXT"}}}}
	if err = r.Complete(ctx, first, outcome, map[string]any{}, nil, time.Now()); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatalf("old completion: %v", err)
	}
	resp := decision.Response{ApplicationID: "app", Status: "NEED_INFO"}
	if err = r.StoreResponse(ctx, second, resp); err != nil {
		t.Fatal(err)
	}
	if err = r.Fail(ctx, second, false, 0, "SAVE_FAILED"); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE applications SET form='{"scoring":{"experience_years":99}}' WHERE id='app'`)
	third, ok, err := r.Claim(ctx, time.Minute)
	if err != nil || !ok || third.Response == nil {
		t.Fatal("response cache lost", ok, err)
	}
	if third.Payload.Source.Scoring != nil {
		t.Fatal("immutable source was reread")
	}
	if err = r.Complete(ctx, third, outcome, map[string]any{"test": true}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = r.Complete(ctx, third, outcome, nil, nil, time.Now()); !errors.Is(err, dispatch.ErrLeaseLost) {
		t.Fatal("duplicate completion", err)
	}
	if err = p.QueryRow(ctx, "SELECT count(*) FROM decisions WHERE scoring_job_id=$1", third.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	var status string
	p.QueryRow(ctx, "SELECT status FROM applications WHERE id='app'").Scan(&status)
	if status != "WAITING_FOR_INFO" {
		t.Fatal(status)
	}
	exec(`UPDATE applications SET status='VERIFYING' WHERE id='app'`)
	if ok, err = r.EnqueueNext(ctx, time.Now()); err != nil || !ok {
		t.Fatal("new round", ok, err)
	}
	var round int
	p.QueryRow(ctx, "SELECT max(round) FROM scoring_jobs").Scan(&round)
	if round != 2 {
		t.Fatal(round)
	}
	stats, err := r.Stats(ctx)
	if err != nil || stats.Pending != 1 {
		t.Fatal(stats, err)
	}
}
func TestScoringCollectionExhaustionAndEventInbox(t *testing.T) {
	p := scoringTestDB(t)
	seedScoringApp(t, p)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `UPDATE applications SET status='ENRICHING';UPDATE collection_tasks SET status='RUNNING',attempts=3`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewCollectionTaskRepository(p).Claim(ctx, "task", 3); err != nil {
		t.Fatal(err)
	}
	var status string
	p.QueryRow(ctx, "SELECT status FROM applications").Scan(&status)
	if status != "VERIFYING" {
		t.Fatal("exhausted application stuck", status)
	}
	r := NewDecisionRepository(p)
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			if _, err := r.SaveCreditEvent(ctx, decision.CreditEvent{EventID: "event", ApplicationID: "app", Kind: "LOAN_ISSUED"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	p.QueryRow(ctx, "SELECT count(*) FROM application_events WHERE type='credit_engine.loan_issued'").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate inbox event", count)
	}
}

func TestScoringJobPersistsModelSelectionMode(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-v2", true: "active-model"}[active], func(t *testing.T) {
			p := scoringTestDB(t)
			seedScoringApp(t, p)
			ctx := context.Background()
			r := ScoringJobRepository{Pool: p, UseActiveModels: active}
			if ok, err := r.EnqueueNext(ctx, time.Now()); err != nil || !ok {
				t.Fatal(ok, err)
			}
			// A restart with a different configuration cannot mutate an existing snapshot.
			r.UseActiveModels = !active
			job, ok, err := r.Claim(ctx, time.Minute)
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			if active && job.Payload.Request.ModelVersion != nil {
				t.Fatal("active selection overridden by static version")
			}
			if !active && (job.Payload.Request.ModelVersion == nil || *job.Payload.Request.ModelVersion != "legal-v2") {
				t.Fatal("explicit version not frozen")
			}
		})
	}
}
