//go:build integration

package financial

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/postgres"
	"github.com/gin-gonic/gin"
)

func TestCabinetPowerOfAttorneyAttachments(t *testing.T) {
	pool := financialDB(t)
	ctx := context.Background()
	// A power of attorney does not depend on the financial parser's tax/legal-form context.
	if _, err := pool.Exec(ctx, `UPDATE applications SET status='WAITING_FOR_INFO', form='{}' WHERE id='app-ooo';
        INSERT INTO info_requests(id,application_id,required,question) VALUES('ir-poa','app-ooo',true,'{"input":{"type":"NONE"},"documents":{"required":true,"accepted_types":["POWER_OF_ATTORNEY"]}}');`); err != nil {
		t.Fatal(err)
	}
	repo := Repository{Pool: pool, Topic: "finance", DocumentsTopic: "signatures"}
	router := gin.New()
	Register(router, repo, Files{Root: t.TempDir()})
	upload := func(typ, name, target string) (Document, int) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("type", typ)
		writer.WriteField("info_request_id", "ir-poa")
		if target != "" {
			writer.WriteField("signed_document_id", target)
		}
		part, _ := writer.CreateFormFile("file", name)
		part.Write([]byte("test document bytes"))
		writer.Close()
		request := httptest.NewRequest("POST", "/api/v1/applications/app-ooo/documents", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		var doc Document
		if response.Code == 201 {
			if err := json.Unmarshal(response.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
		}
		return doc, response.Code
	}
	poa, code := upload("POWER_OF_ATTORNEY", "poa.xml", "")
	if code != 201 || poa.InfoRequestID != "ir-poa" || poa.Generation != 0 || poa.MIME != "application/xml" {
		t.Fatalf("upload: %d %+v", code, poa)
	}
	sig, code := upload("SIGNATURE", "poa.xml.sig", poa.ID)
	if code != 201 || sig.SignedDocumentID != poa.ID || sig.MIME != "application/pkcs7-signature" {
		t.Fatalf("signature: %d %+v", code, sig)
	}
	current, err := repo.Get(ctx, "app-ooo", poa.ID)
	if err != nil || current.Signatures.State != "PENDING" {
		t.Fatalf("state: %+v %v", current, err)
	}
	var jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_document_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("POA sent to financial parser: %d %v", jobs, err)
	}
	for range 2 {
		if err := repo.Retry(ctx, "app-ooo", poa.ID); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := pool.Query(ctx, `SELECT payload FROM outbox WHERE topic='signatures' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var retries []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var event events.DocumentsSubmittedEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(event.VerificationID, "verify_") {
			retries = append(retries, event.VerificationID)
		}
	}
	rows.Close()
	if len(retries) != 2 || retries[0] == retries[1] {
		t.Fatalf("retry keys: %v", retries)
	}
	if err := repo.Delete(ctx, "app-ooo", poa.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := repo.List(ctx, "app-ooo")
	if err != nil || len(remaining) != 0 {
		t.Fatalf("delete should hide original and signature: %+v %v", remaining, err)
	}
	apps := postgres.NewApplicationRepository(pool, "collection", "signatures")
	answer := domain.InfoAnswer{ApplicationID: "app-ooo", RequestID: "ir-poa", ActorID: "user", CompanyINN: "1234567890", DocumentIDs: []string{poa.ID}, Now: time.Now().UTC(), ReviewSLA: time.Hour}
	if _, _, err := apps.AnswerInfoRequest(ctx, answer); !errors.Is(err, domain.ErrInvalidAnswerDocuments) {
		t.Fatalf("deleted document accepted: %v", err)
	}
	poa, code = upload("POWER_OF_ATTORNEY", "poa.xml", "")
	if code != 201 {
		t.Fatal(code)
	}
	sig, code = upload("SIGNATURE", "poa.xml.sig", poa.ID)
	if code != 201 {
		t.Fatal(code)
	}
	answer.DocumentIDs = []string{poa.ID, sig.ID}
	if _, found, err := apps.AnswerInfoRequest(ctx, answer); err != nil || !found {
		t.Fatalf("answer: %v, found=%v", err, found)
	}
	app, found, err := apps.Get(ctx, "app-ooo")
	if err != nil || !found || len(app.Documents) != 2 || app.Documents[0].InfoRequestID != "ir-poa" {
		t.Fatalf("answer duplicated/lost attachments: %+v %v", app.Documents, err)
	}
	if err := repo.Delete(ctx, "app-ooo", poa.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("answered attachment can be removed: %v", err)
	}
}

func TestFinancialAttachmentGenerationStates(t *testing.T) {
	pool := financialDB(t)
	ctx := context.Background()
	repo := Repository{Pool: pool, Topic: "finance"}
	doc := Document{ID: "doc-fin", ApplicationID: "app-ooo", Type: "FINANCIAL_STATEMENTS", FileName: "balance.pdf", MIME: "application/pdf", SHA256: hash([]byte("original")), StorageKey: "original", Size: 8}
	sig := Document{ID: "doc-sig", ApplicationID: "app-ooo", Type: "SIGNATURE", FileName: "balance.pdf.sig", MIME: "application/pkcs7-signature", SignedDocumentID: doc.ID, SHA256: hash([]byte("signature")), StorageKey: "signature", Size: 9}
	if err := repo.Upload(ctx, doc, []Document{sig}); err != nil {
		t.Fatal(err)
	}
	claim := func() Job {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM financial_document_jobs WHERE document_id=$1 ORDER BY generation DESC LIMIT 1`, doc.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		job, ok, err := repo.Claim(ctx, id, 3, time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim: %v %v", ok, err)
		}
		return job
	}
	first := claim()
	if err := repo.Retry(ctx, doc.ApplicationID, doc.ID); err != nil {
		t.Fatal(err)
	}
	complete := func(job Job) {
		t.Helper()
		report := SignatureReport{ID: newID("sigr_"), Verdict: "INDETERMINATE", Policy: "FINANCIAL_STATEMENTS", TrustMode: "MOCK", DocumentSHA256: doc.SHA256, VerifiedAt: time.Now().UTC()}
		if err := repo.Complete(ctx, job, Result{ID: newID("fin_"), DocumentID: doc.ID, Generation: job.Generation, SignatureReportID: report.ID, Signature: report, ProcessedAt: time.Now().UTC(), Extraction: Extraction{DocumentSHA256: doc.SHA256}}); err != nil {
			t.Fatal(err)
		}
	}
	complete(first)
	current, err := repo.Get(ctx, doc.ApplicationID, sig.ID)
	if err != nil || current.Signatures.State != "PENDING" {
		t.Fatalf("stale generation completed signature: %+v %v", current, err)
	}
	complete(claim())
	for _, id := range []string{doc.ID, sig.ID} {
		current, err := repo.Get(ctx, doc.ApplicationID, id)
		if err != nil || current.Signatures.State != "DONE" {
			t.Fatalf("signature never finishes: %+v %v", current, err)
		}
	}
}
