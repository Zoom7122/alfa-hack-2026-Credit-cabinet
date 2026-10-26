//go:build integration

package financial

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
	"github.com/gin-gonic/gin"
)

func TestTestPDFAcceptedWithoutContextAndNeverQueued(t *testing.T) {
	pool := financialDB(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE applications SET form='{}' WHERE id='app-ip'`); err != nil {
		t.Fatal(err)
	}
	repo := Repository{Pool: pool, Topic: "financial", DocumentsTopic: "signatures"}
	probe := Document{ID: "probe", ApplicationID: "app-ip", Type: "TAX_RETURN", FileName: "test.pdf", MIME: "application/pdf", SHA256: testdocument.SHA256, Size: int64(len(testdocument.PDF())), StorageKey: "probe"}
	if err := repo.Upload(ctx, probe, nil); err != nil {
		t.Fatalf("repository rejected sample: %v", err)
	}
	if err := repo.Delete(ctx, probe.ApplicationID, probe.ID); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	Register(router, repo, Files{Root: t.TempDir()})
	upload := func(typ, name, parent string, raw []byte) (Document, *httptest.ResponseRecorder) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("type", typ)
		writer.WriteField("is_test", "true") // Never trusted by the server.
		if parent != "" {
			writer.WriteField("signed_document_id", parent)
		}
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(raw)
		writer.Close()
		request := httptest.NewRequest("POST", "/api/v1/applications/app-ip/documents", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var doc Document
		if response.Code == 201 {
			if err := json.Unmarshal(response.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
		}
		return doc, response
	}
	assertNoWork := func() {
		t.Helper()
		var jobs, outbox, pending int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM financial_document_jobs),
            (SELECT count(*) FROM outbox), (SELECT count(*) FROM documents WHERE signature_state='PENDING')`).Scan(&jobs, &outbox, &pending); err != nil {
			t.Fatal(err)
		}
		if jobs != 0 || outbox != 0 || pending != 0 {
			t.Fatalf("sample blocks scoring or queued processing: jobs=%d outbox=%d pending=%d", jobs, outbox, pending)
		}
	}
	for _, typ := range []string{"FINANCIAL_STATEMENTS", "TAX_RETURN", "BANK_STATEMENT", "OTHER"} {
		doc, response := upload(typ, "renamed-"+typ+".pdf", "", testdocument.PDF())
		if response.Code != 201 || !doc.IsTest || doc.Generation != 0 || doc.Signatures.State != "NOT_SIGNED" || doc.SHA256 != testdocument.SHA256 {
			t.Fatalf("sample not accepted: %d %s", response.Code, response.Body.String())
		}
		if err := repo.Retry(ctx, "app-ip", doc.ID); err != nil {
			t.Fatal(err)
		}
		// An attached signature and its deletion must not start processing the sample.
		sig, response := upload("SIGNATURE", doc.FileName+".sig", doc.ID, []byte("synthetic signature"))
		if response.Code != 201 || sig.Signatures.State != "NOT_SIGNED" {
			t.Fatalf("sample signature not accepted: %d %s", response.Code, response.Body.String())
		}
		if err := repo.Delete(ctx, "app-ip", sig.ID); err != nil {
			t.Fatal(err)
		}
		assertNoWork()
	}
	modified := append(testdocument.PDF(), []byte("\nmodified")...)
	_, response := upload("TAX_RETURN", "test-financial-document.pdf", "", modified)
	if response.Code != 422 {
		t.Fatalf("filename or is_test request flag bypassed real validation: %d %s", response.Code, response.Body.String())
	}
	assertNoWork()
}

func TestPersistedTestPDFCannotAffectDecision(t *testing.T) {
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
		Extraction:        Extraction{DocumentSHA256: doc.SHA256},
		ScoringIndicators: []Indicator{{Name: "usn_taxable_income", Period: "2025", PeriodKind: "YEAR", Amount: "999999999.00", Eligible: true, DataEligible: true}}}
	if err := repo.Complete(ctx, job, result); err != nil {
		t.Fatal(err)
	}
	decisions := postgres.NewDecisionRepository(pool)
	before, err := decisions.Input(ctx, doc.ApplicationID, time.Now().UTC())
	if err != nil || len(before.DocumentIndicators) != 1 {
		t.Fatalf("positive control did not reach scoring: %+v %v", before.DocumentIndicators, err)
	}
	// Simulate an old/incorrect processor persisting an approved sample result and
	// an invalid-integrity report. Neither values nor the fraud flag may be used.
	result.DocumentSHA256, result.Signature.DocumentSHA256 = testdocument.SHA256, testdocument.SHA256
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE documents SET sha256=$2 WHERE id=$1`, doc.ID, testdocument.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE financial_document_results SET original_sha256=$2,result=$3 WHERE id=$1`, result.ID, testdocument.SHA256, raw); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE signature_reports SET document_sha256=$2,integrity_status='INVALID' WHERE id=$1`, report.ID, testdocument.SHA256); err != nil {
		t.Fatal(err)
	}
	after, err := decisions.Input(ctx, doc.ApplicationID, time.Now().UTC())
	if err != nil || len(after.DocumentIndicators) != 0 || after.ForgedDocument {
		t.Fatalf("sample changed scoring input: indicators=%+v forged=%v err=%v", after.DocumentIndicators, after.ForgedDocument, err)
	}
}
