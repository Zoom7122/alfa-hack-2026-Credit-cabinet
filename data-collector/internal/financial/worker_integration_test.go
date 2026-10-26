//go:build integration

package financial

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func enqueueTestDocument(t *testing.T, r Repository) (Document, Event) {
	t.Helper()
	d := Document{ID: newID("doc_"), ApplicationID: "app-ip", Type: "TAX_RETURN", FileName: "synthetic.xls", MIME: "application/vnd.ms-excel", Size: 4, SHA256: hash([]byte("test")), StorageKey: newID("blob_")}
	if err := r.Upload(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := r.Pool.QueryRow(context.Background(), `SELECT payload FROM outbox WHERE key=$1`, d.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	return d, ev
}

func testResult(j Job, d Document) Result {
	return Result{
		ID: newID("fin_"), DocumentID: d.ID, Generation: j.Generation, SignatureReportID: newID("sig_"),
		Extraction: Extraction{DocumentSHA256: d.SHA256, Extracted: json.RawMessage(`{}`), Validation: json.RawMessage(`{}`)},
		Signature:  SignatureReport{Verdict: "INDETERMINATE", Policy: "KUDIR_IP", TrustMode: "LIVE", VerifiedAt: time.Now().UTC()},
	}
}

func expireLease(t *testing.T, r Repository, id string) {
	t.Helper()
	if _, err := r.Pool.Exec(context.Background(), `UPDATE financial_document_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestFinancialJobLeaseFencesStaleWorkers(t *testing.T) {
	r := Repository{Pool: financialDB(t), Topic: "test"}
	ctx := context.Background()
	d, ev := enqueueTestDocument(t, r)
	first, ok, err := r.Claim(ctx, ev.TaskID, 3, time.Minute)
	if err != nil || !ok || first.Attempts != 1 {
		t.Fatalf("first claim: %+v %v %v", first, ok, err)
	}
	if _, _, err = r.Claim(ctx, ev.TaskID, 3, time.Minute); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("live lease: %v", err)
	}
	expireLease(t, r, ev.TaskID)
	second, ok, err := r.Claim(ctx, ev.TaskID, 3, time.Minute)
	if err != nil || !ok || second.Attempts != 2 {
		t.Fatalf("reclaim: %+v %v %v", second, ok, err)
	}
	if err = r.Complete(ctx, first, testResult(first, d)); !errors.Is(err, ErrJobNotRunning) {
		t.Fatalf("stale completion: %v", err)
	}
	if err = r.Fail(ctx, first, "STALE"); !errors.Is(err, ErrJobNotRunning) {
		t.Fatalf("stale failure: %v", err)
	}
	// A failed report insert must roll back the status change and every audit write.
	invalid := testResult(second, d)
	invalid.Signature.Policy = "UNKNOWN"
	if err = r.Complete(ctx, second, invalid); err == nil {
		t.Fatal("invalid report persisted")
	}
	if err = r.Complete(ctx, second, testResult(second, d)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = r.Claim(ctx, ev.TaskID, 3, time.Minute); err != nil || ok {
		t.Fatalf("completed task reclaimed: %v %v", ok, err)
	}
	var reports, results int
	if err = r.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM signature_reports), (SELECT count(*) FROM financial_document_results)`).Scan(&reports, &results); err != nil || reports != 1 || results != 1 {
		t.Fatalf("atomic result: %d %d %v", reports, results, err)
	}
}

func TestFinancialExhaustedAttemptsAndGenerationIsolation(t *testing.T) {
	r := Repository{Pool: financialDB(t), Topic: "test"}
	ctx := context.Background()
	d, ev := enqueueTestDocument(t, r)
	first, _, err := r.Claim(ctx, ev.TaskID, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expireLease(t, r, ev.TaskID)
	j, ok, err := r.Claim(ctx, ev.TaskID, 1, time.Minute)
	if err != nil || !ok || j.Status != "FAILED" || j.Attempts != 1 {
		t.Fatalf("exhaustion: %+v %v %v", j, ok, err)
	}
	if _, err = r.Result(ctx, d.ApplicationID, d.ID); !errors.Is(err, ErrProcessingFailed) {
		t.Fatalf("failed result: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, r, Files{Root: t.TempDir()})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/applications/app-ip/documents/"+d.ID+"/financial-result", nil))
	if w.Code != 422 {
		t.Fatalf("failed task keeps polling: %d", w.Code)
	}
	if err = r.Retry(ctx, d.ApplicationID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err = r.Complete(ctx, first, testResult(first, d)); !errors.Is(err, ErrJobNotRunning) {
		t.Fatal("failed attempt completed", err)
	}
	// Complete an older running generation after a newer retry was queued.
	var nextID string
	if err = r.Pool.QueryRow(ctx, `SELECT id FROM financial_document_jobs WHERE document_id=$1 AND generation=2`, d.ID).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	second, _, err := r.Claim(ctx, nextID, 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Retry(ctx, d.ApplicationID, d.ID); err != nil {
		t.Fatal(err)
	}
	if err = r.Complete(ctx, second, testResult(second, d)); err != nil {
		t.Fatal(err)
	}
	stored, err := r.Get(ctx, d.ApplicationID, d.ID)
	if err != nil || stored.Generation != 3 || stored.Signatures.State != "PENDING" {
		t.Fatalf("old generation overwrote current: %+v %v", stored, err)
	}
	if _, err = r.Result(ctx, d.ApplicationID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old result exposed", err)
	}
}

func TestFinancialWorkerMigrationPreservesCompletedResults(t *testing.T) {
	r := Repository{Pool: financialDB(t), Topic: "test"}
	ctx := context.Background()
	d, ev := enqueueTestDocument(t, r)
	j, _, err := r.Claim(ctx, ev.TaskID, 3, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Complete(ctx, j, testResult(j, d)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../db/migrations/00008_financial_worker.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(raw), "-- +goose Down")
	if _, err = r.Pool.Exec(ctx, parts[1]); err != nil {
		t.Fatal("downgrade", err)
	}
	var status string
	if err = r.Pool.QueryRow(ctx, `SELECT status FROM financial_document_jobs WHERE id=$1`, j.ID).Scan(&status); err != nil || status != "DONE" {
		t.Fatalf("legacy state: %s %v", status, err)
	}
	if _, err = r.Pool.Exec(ctx, parts[0]); err != nil {
		t.Fatal("upgrade", err)
	}
	if _, ok, err := r.Claim(ctx, j.ID, 3, time.Minute); err != nil || ok {
		t.Fatalf("migrated completed task reclaimed: %v %v", ok, err)
	}
	if result, err := r.Result(ctx, d.ApplicationID, d.ID); err != nil || len(result) == 0 {
		t.Fatal("audit result lost", err)
	}
}
