//go:build integration

package financial

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

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
