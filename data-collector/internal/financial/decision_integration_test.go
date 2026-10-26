//go:build integration

package financial

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
)

func TestDocumentResultReachesScoringInput(t *testing.T) {
	pool := financialDB(t)
	ctx := context.Background()
	repo := Repository{Pool: pool, Topic: "financial-test"}
	doc, event := enqueueTestDocument(t, repo)
	job, ok, err := repo.Claim(ctx, event.TaskID, 3, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	report := validReport(VerifyInput{DocumentSHA256: doc.SHA256, Applicant: job.Applicant, Policy: policy(job.Applicant)})
	report.ID, report.VerifiedAt = newID("sigr_"), time.Now().UTC()
	result := Result{ID: newID("fin_"), DocumentID: doc.ID, Generation: job.Generation, SignatureReportID: report.ID, Signature: report,
		Extraction:        Extraction{DocumentSHA256: doc.SHA256, ParserVersion: "synthetic-test", Extracted: json.RawMessage(`{}`), Validation: json.RawMessage(`[]`)},
		ScoringIndicators: []Indicator{{Name: "usn_taxable_income", Period: "2025", PeriodKind: "YEAR", Amount: "6355000.00", Eligible: true, DataEligible: true}},
	}
	if err := repo.Complete(ctx, job, result); err != nil {
		t.Fatal(err)
	}
	decisions := postgres.NewDecisionRepository(pool)
	input := func(appID string) decision.Input {
		t.Helper()
		in, err := decisions.Input(ctx, appID, time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	in := input("app-ip")
	_, facts := decision.Build(in)
	if len(in.DocumentIndicators) != 1 || facts.DeclaredRevenue != 6_355_000 || facts.DeclaredSource != "DOCUMENT_KUDIR" ||
		facts.DocumentIndicators[0].ResultID != result.ID {
		t.Fatalf("persisted numbers not used: %+v %+v", in.DocumentIndicators, facts)
	}
	if _, err := pool.Exec(ctx, `UPDATE applications SET status='VERIFYING', updated_at=now()-interval '1 day' WHERE id='app-ip'`); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := decisions.ClaimNext(ctx, 0, 0); err != nil || !ok || id != "app-ip" {
		t.Fatalf("completed document blocked scoring: %s %v %v", id, ok, err)
	}
	// Another application of the same borrower cannot inherit these documents.
	if _, err := pool.Exec(ctx, `INSERT INTO applications(id,number,company_id,created_by,form) VALUES('app-other','OTHER','ip','user','{}')`); err != nil {
		t.Fatal(err)
	}
	if len(input("app-other").DocumentIndicators) != 0 {
		t.Fatal("documents crossed application boundary")
	}

	// A stored flag alone is insufficient when verification is unavailable or TEST.
	for _, mode := range []string{"MOCK", "LIVE"} {
		result.Signature.TrustMode = mode
		result.Signature.PolicyResult.Satisfied = mode != "LIVE"
		raw, _ := json.Marshal(result)
		if _, err := pool.Exec(ctx, `UPDATE financial_document_results SET result=$2 WHERE id=$1`, result.ID, raw); err != nil {
			t.Fatal(err)
		}
		if len(input("app-ip").DocumentIndicators) != 0 {
			t.Fatal("unverified result reached scoring")
		}
	}
	result.Signature = report
	approved := result.ScoringIndicators[0]
	for _, bad := range []Indicator{
		{Name: approved.Name, Period: approved.Period, PeriodKind: approved.PeriodKind, Amount: approved.Amount, DataEligible: true},
		{Name: approved.Name, Period: approved.Period, PeriodKind: approved.PeriodKind, Amount: approved.Amount, Eligible: true},
		{Name: approved.Name, Period: approved.Period, PeriodKind: approved.PeriodKind, Amount: "6355000.50", Eligible: true, DataEligible: true},
	} {
		result.ScoringIndicators[0] = bad
		raw, _ := json.Marshal(result)
		if _, err := pool.Exec(ctx, `UPDATE financial_document_results SET result=$2 WHERE id=$1`, result.ID, raw); err != nil {
			t.Fatal(err)
		}
		if len(input("app-ip").DocumentIndicators) != 0 {
			t.Fatal("ineligible/fractional indicator used")
		}
	}
	result.ScoringIndicators[0] = approved
	raw, _ := json.Marshal(result)
	if _, err := pool.Exec(ctx, `UPDATE financial_document_results SET result=$2 WHERE id=$1`, result.ID, raw); err != nil {
		t.Fatal(err)
	}
	if len(input("app-ip").DocumentIndicators) != 1 {
		t.Fatal("approved generation not restored")
	}
	// A new verification invalidates the old result before it finishes.
	if err := repo.Retry(ctx, "app-ip", doc.ID); err != nil {
		t.Fatal(err)
	}
	if len(input("app-ip").DocumentIndicators) != 0 {
		t.Fatal("old generation reused during verification")
	}
	if _, err := pool.Exec(ctx, `UPDATE applications SET status='VERIFYING',updated_at=now()-interval '1 day' WHERE id='app-ip'`); err != nil {
		t.Fatal(err)
	}
	if id, ok, err := decisions.ClaimNext(ctx, 0, 0); err != nil || ok {
		t.Fatalf("pending financial task bypassed: %s %v %v", id, ok, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE documents SET financial_generation=$2,deleted_at=now() WHERE id=$1`, doc.ID, job.Generation); err != nil {
		t.Fatal(err)
	}
	if len(input("app-ip").DocumentIndicators) != 0 {
		t.Fatal("deleted document reused")
	}
}
