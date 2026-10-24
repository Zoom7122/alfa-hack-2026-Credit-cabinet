//go:build integration

package financial

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func financialDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("FINANCIAL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set FINANCIAL_TEST_DATABASE_URL to run isolated PostgreSQL tests")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(newID("test_"))
	if _, err = base.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		base.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); base.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`); base.Close() })
	paths, err := filepath.Glob("../../../db/migrations/0*.sql")
	if err != nil || len(paths) < 4 {
		t.Fatal("migration files missing", err)
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		up := strings.Split(string(raw), "-- +goose Down")[0]
		if _, e = pool.Exec(ctx, up); e != nil {
			t.Fatalf("migration %s: %v", path, e)
		}
	}
	_, err = pool.Exec(ctx, `INSERT INTO users(id,full_name,role) VALUES('user','Test','CLIENT');
 INSERT INTO companies(id,inn,type,name) VALUES('ip','123456789012','SOLE_PROPRIETOR','Test IP'),('ooo','1234567890','LEGAL_ENTITY','Test OOO');
 INSERT INTO applications(id,number,company_id,created_by,form) VALUES
 ('app-ip','IP','ip','user','{"financial_document_context":{"tax_regime":"USN_INCOME"}}'),
 ('app-ooo','OOO','ooo','user','{"financial_document_context":{"legal_form":"OOO"}}');`)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestFinancialAPIWithActualDocuments(t *testing.T) {
	pool := financialDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	files := Files{Root: t.TempDir()}
	repo := Repository{Pool: pool, Topic: "test-topic"}
	script, _ := filepath.Abs("../../financial_parser/parser.py")
	processor := Processor{Files: files, Verifier: HTTPVerifier{}, Parser: PythonParser{Script: script}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router, repo, files)
	upload := func(app, kind, parent, name string, raw []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("type", kind)
		if parent != "" {
			writer.WriteField("signed_document_id", parent)
		}
		part, e := writer.CreateFormFile("file", name)
		if e != nil {
			t.Fatal(e)
		}
		part.Write(raw)
		writer.Close()
		req := httptest.NewRequest("POST", "/api/v1/applications/"+app+"/documents", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	get := func(app, id, suffix string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/applications/"+app+"/documents/"+id+suffix, nil))
		return w
	}
	process := func(id string) {
		var ev []byte
		if err := pool.QueryRow(ctx, `SELECT payload FROM outbox WHERE key=$1 ORDER BY id DESC LIMIT 1`, id).Scan(&ev); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := repo.Handle(ctx, processor, ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tt := range []struct{ app, kind, file string }{{"app-ip", "TAX_RETURN", "kudir-2026.xls"}, {"app-ooo", "FINANCIAL_STATEMENTS", "girbo-2025.pdf"}} {
		t.Run(tt.file, func(t *testing.T) {
			tc := privateCase(t, tt.file)
			if _, err := pool.Exec(ctx, `UPDATE companies SET inn=$2 WHERE id=(SELECT company_id FROM applications WHERE id=$1)`, tt.app, tc.Applicant.INN); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(privateFixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			w := upload(tt.app, tt.kind, "", tt.file, raw)
			if w.Code != 201 {
				t.Fatalf("upload %d: %s", w.Code, w.Body.String())
			}
			var d Document
			if err = json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
			if d.Generation != 1 || d.SHA256 != hash(raw) || d.Origin != "CLIENT_UPLOAD" {
				t.Fatalf("bad document %+v", d)
			}
			if got := get(tt.app, d.ID, "/financial-result"); got.Code != 202 {
				t.Fatal("must initially be pending", got.Code)
			}
			if got := get(tt.app, d.ID, "/content"); got.Code != 200 || !bytes.Equal(got.Body.Bytes(), raw) {
				t.Fatal("original bytes changed")
			}
			process(d.ID)
			got := get(tt.app, d.ID, "/financial-result")
			if got.Code != 200 {
				t.Fatal(got.Code, got.Body.String())
			}
			var result Result
			if err = json.Unmarshal(got.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Signature.Verdict != "INDETERMINATE" || len(result.ScoringIndicators) != 0 || len(result.Indicators) == 0 {
				t.Fatalf("unexpected result %+v", result)
			}
			var previousForm []byte
			if err = pool.QueryRow(ctx, `SELECT form FROM applications WHERE id=$1`, tt.app).Scan(&previousForm); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `UPDATE applications SET form=jsonb_set(form,'{financial_document_context,tax_regime}','"CHANGED"') WHERE id=$1`, tt.app); err != nil {
				t.Fatal(err)
			}
			if get(tt.app, d.ID, "/financial-result").Code != 409 {
				t.Fatal("stale applicant context still exposes old result")
			}
			if _, err = pool.Exec(ctx, `UPDATE applications SET form=$2 WHERE id=$1`, tt.app, previousForm); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM financial_document_results WHERE document_id=$1`, d.ID).Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate delivery created multiple results", count, err)
			}
			other := "app-ip"
			if tt.app == other {
				other = "app-ooo"
			}
			if get(other, d.ID, "/content").Code != 404 {
				t.Fatal("cross-application document access")
			}
			wrong := upload(other, "SIGNATURE", d.ID, "wrong.sig", []byte("signature"))
			if wrong.Code != 404 {
				t.Fatal("cross-application signature accepted", wrong.Code)
			}
			late := upload(tt.app, "SIGNATURE", d.ID, "detached.sig", []byte("signature"))
			if late.Code != 201 {
				t.Fatal(late.Code, late.Body.String())
			}
			if get(tt.app, d.ID, "/financial-result").Code != 202 {
				t.Fatal("previous generation remains eligible during recheck")
			}
			process(d.ID)
			stored, err := repo.Get(ctx, tt.app, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			path, _ := files.Path(stored.StorageKey)
			if err = os.WriteFile(path, append(raw, []byte("tampered")...), 0600); err != nil {
				t.Fatal(err)
			}
			if err = repo.Retry(ctx, tt.app, d.ID); err != nil {
				t.Fatal(err)
			}
			process(d.ID)
			updated, err := repo.Result(ctx, tt.app, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			json.Unmarshal(updated, &result)
			if result.Signature.Verdict != "INVALID" || len(result.ScoringIndicators) != 0 {
				t.Fatal("tampered document not blocked")
			}
			if err = pool.QueryRow(ctx, `SELECT count(*) FROM signature_reports WHERE document_id=$1`, d.ID).Scan(&count); err != nil || count != 3 {
				t.Fatal(fmt.Sprint("audit reports: ", count, err))
			}
		})
	}
}
