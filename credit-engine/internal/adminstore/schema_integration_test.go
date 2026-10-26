//go:build integration

package adminstore_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func isolatedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CREDIT_ENGINE_TEST_DSN")
	config, err := pgx.ParseConfig(dsn)
	if err != nil || dsn == "" || os.Getenv("CREDIT_ENGINE_TEST_ISOLATED") != "1" || config.Database != "credit_engine_admin_test" {
		t.Fatal("integration tests require an explicitly isolated Docker database credit_engine_admin_test")
	}
	rootDB, err := adminstore.OpenDatabase(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rootDB.PingContext(ctx); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("credit_engine_admin_test_%x", nonce)
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := rootDB.ExecContext(ctx, "CREATE DATABASE "+identifier); err != nil {
		rootDB.Close()
		t.Fatal(err)
	}
	childConfig := config.Copy()
	childConfig.Database = name
	childConfig.ConnectTimeout = 5 * time.Second
	db := stdlib.OpenDB(*childConfig)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	t.Cleanup(func() {
		db.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := rootDB.ExecContext(cleanup, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
		rootDB.Close()
	})
	return db
}
func dbExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(query, err)
	}
}
func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func migrationOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("migration error: %v; cause: %v", err, errors.Unwrap(err))
	}
}

func TestAdminSchemaIntegration(t *testing.T) {
	db := isolatedDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// These are fixtures owned by this disposable database, not project tables.
	dbExec(t, db, "CREATE TABLE public.root_sentinel(id integer PRIMARY KEY, value text NOT NULL)")
	dbExec(t, db, "INSERT INTO public.root_sentinel VALUES (1,'unchanged')")
	dbExec(t, db, "CREATE SCHEMA neighbor_service")
	dbExec(t, db, "CREATE TABLE neighbor_service.records(id integer PRIMARY KEY, value text NOT NULL)")
	dbExec(t, db, "INSERT INTO neighbor_service.records VALUES (1,'neighbor unchanged')")
	dbExec(t, db, "CREATE TABLE public.goose_db_version(id bigint PRIMARY KEY,version_id bigint,is_applied boolean,tstamp timestamp)")
	dbExec(t, db, "INSERT INTO public.goose_db_version VALUES (1,4242,true,'2026-01-01')")
	migrator, err := adminstore.NewMigrator(db)
	migrationOK(t, err)
	status, err := migrator.Status(ctx)
	migrationOK(t, err)
	if len(status) != 1 || status[0].Source.Version != 1 || string(status[0].State) != "pending" {
		t.Fatal(status)
	}
	if n := countRows(t, db, "SELECT count(*) FROM pg_namespace WHERE nspname='credit_engine_admin'"); n != 0 {
		t.Fatal("status created schema")
	}
	if result, err := migrator.Down(ctx); err != nil || result != nil {
		t.Fatal("down changed an uninitialized database", err)
	}
	t.Run("concurrent_up_and_repeat", func(t *testing.T) {
		var wg sync.WaitGroup
		counts := [2]int{}
		failures := [2]error{}
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m, err := adminstore.NewMigrator(db)
				if err != nil {
					failures[i] = err
					return
				}
				results, err := m.Up(ctx)
				counts[i], failures[i] = len(results), err
			}()
		}
		wg.Wait()
		for _, err := range failures {
			migrationOK(t, err)
		}
		if counts[0]+counts[1] != 1 {
			t.Fatal("migration applied twice", counts)
		}
		results, err := migrator.Up(ctx)
		migrationOK(t, err)
		if len(results) != 0 {
			t.Fatal("repeated up changed schema")
		}
	})
	if n := countRows(t, db, "SELECT count(*) FROM information_schema.tables WHERE table_schema='credit_engine_admin'"); n != 8 {
		t.Fatal("expected seven data tables plus private Goose table", n)
	}
	t.Run("documents_revisions_and_immutability", func(t *testing.T) {
		catalog, err := modelbindings.Default()
		if err != nil {
			t.Fatal(err)
		}
		entry, err := catalog.Lookup("ip-v2", contracts.BorrowerTypeSoleProprietor)
		if err != nil {
			t.Fatal(err)
		}
		definition, err := entry.Definition()
		if err != nil {
			t.Fatal(err)
		}
		definition.Version = "schema-storage-v1"
		definition.Parameters[0].Number.Lower.Value = "0e0"
		document, err := model.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		hash := release.Hash(document)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		draft := "00000000-0000-4000-8000-000000000001"
		if _, err := tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.model_drafts(draft_id,model_id,borrower_type,title,current_revision,created_by) VALUES($1,'ip','SOLE_PROPRIETOR','Test draft',1,'test')", draft); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.model_revisions(draft_id,revision,definition_json,definition_sha256,author,reason,changes) VALUES($1,1,$2,$3,'test','initial','{}')", draft, string(document), hash); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var saved string
		if err := db.QueryRowContext(ctx, "SELECT definition_json FROM credit_engine_admin.model_revisions WHERE draft_id=$1 AND revision=1", draft).Scan(&saved); err != nil || saved != string(document) || !strings.Contains(saved, "0e0") {
			t.Fatal("canonical numeric token changed", err)
		}
		columns := countRows(t, db, "SELECT count(*) FROM information_schema.columns WHERE table_schema='credit_engine_admin'")
		definition.FactsSchemaVersion = "schema-test-facts"
		definition.Parameters = append(definition.Parameters, model.Parameter{ID: "new_flag", Enabled: true, Title: "New flag", Source: model.Source{Kind: model.Supplier, Path: "new_flag"}, JSONName: "new_flag", Type: model.Boolean, Unit: model.Flag})
		updated, err := model.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.model_revisions(draft_id,revision,definition_json,definition_sha256,author,reason,changes) VALUES($1,2,$2,$3,'test','add flag','{}')", draft, string(updated), release.Hash(updated)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE credit_engine_admin.model_drafts SET current_revision=2 WHERE draft_id=$1", draft); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if columns != countRows(t, db, "SELECT count(*) FROM information_schema.columns WHERE table_schema='credit_engine_admin'") {
			t.Fatal("new fact required columns")
		}
		if _, err := db.ExecContext(ctx, "UPDATE credit_engine_admin.model_drafts SET current_revision=99 WHERE draft_id=$1", draft); err == nil {
			t.Fatal("missing current revision accepted")
		}
		dbExec(t, db, "INSERT INTO credit_engine_admin.model_versions(model_version,model_id,borrower_type,definition_json,definition_sha256,uses_facts,facts_schema_version,facts_schema_sha256,created_by) VALUES('schema-storage-v1','ip','SOLE_PROPRIETOR',$1,$2,false,'','','test')", string(document), hash)
		dbExec(t, db, "INSERT INTO credit_engine_admin.prepared_releases(release_version,manifest_json,manifest_sha256,archive,archive_sha256,changes_markdown,notes,created_by) VALUES('schema-r1',$1,$2,$3,$4,'changes','','test')", `{"version":"schema-r1"}`, strings.Repeat("a", 64), []byte("test archive"), strings.Repeat("b", 64))
		dbExec(t, db, "INSERT INTO credit_engine_admin.change_history(entity_kind,entity_id,action,author,reason,details) VALUES('DRAFT',$1,'CREATED','test','initial','{}')", draft)
		payload := `{"event_id":"event-1","application_id":"app-1","kind":"LOAN_ISSUED","borrower":{"type":"SOLE_PROPRIETOR"},"flag":false,"unknown":null}`
		dbExec(t, db, "INSERT INTO credit_engine_admin.credit_events(event_id,application_id,kind,borrower_type,occurred_at,payload_json,payload_sha256) VALUES('event-1','app-1','LOAN_ISSUED','SOLE_PROPRIETOR',now(),$1,$2)", payload, release.Hash([]byte(payload)))
		for _, query := range []string{"UPDATE credit_engine_admin.model_revisions SET author='changed'", "DELETE FROM credit_engine_admin.model_versions", "UPDATE credit_engine_admin.prepared_releases SET notes='changed'", "DELETE FROM credit_engine_admin.change_history", "UPDATE credit_engine_admin.credit_events SET payload_sha256='changed'"} {
			_, err := db.ExecContext(ctx, query)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "55000" {
				t.Fatal("immutable record mutated", query, err)
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO credit_engine_admin.admin_sessions(token_sha256,username,csrf_sha256,created_at,expires_at) VALUES($1,'admin',$2,now(),now()-interval '1 minute')", strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
			t.Fatal("invalid session lifetime accepted")
		}
		dbExec(t, db, "INSERT INTO credit_engine_admin.admin_sessions(token_sha256,username,csrf_sha256,expires_at) VALUES($1,'admin',$2,now()+interval '1 hour')", strings.Repeat("a", 64), strings.Repeat("b", 64))
		var eventJSON string
		if err := db.QueryRowContext(ctx, "SELECT payload_json FROM credit_engine_admin.credit_events WHERE event_id='event-1'").Scan(&eventJSON); err != nil || eventJSON != payload {
			t.Fatal("event changed", err)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(eventJSON), &value); err != nil || value["flag"] != false || value["unknown"] != nil {
			t.Fatal("false/null changed", err)
		}
	})
	t.Run("down_reapply_and_neighbor_isolation", func(t *testing.T) {
		result, err := migrator.Down(ctx)
		migrationOK(t, err)
		if result == nil || result.Source.Version != 1 {
			t.Fatal(result)
		}
		if n := countRows(t, db, "SELECT count(*) FROM information_schema.tables WHERE table_schema='credit_engine_admin'"); n != 1 {
			t.Fatal("down left data tables or removed version table", n)
		}
		if result, err := migrator.Down(ctx); err != nil || result != nil {
			t.Fatal("empty down is not idempotent", err)
		}
		results, err := migrator.Up(ctx)
		migrationOK(t, err)
		if len(results) != 1 {
			t.Fatal(results)
		}
		if n := countRows(t, db, "SELECT count(*) FROM public.root_sentinel WHERE value='unchanged'"); n != 1 {
			t.Fatal("public table affected")
		}
		if n := countRows(t, db, "SELECT count(*) FROM neighbor_service.records WHERE value='neighbor unchanged'"); n != 1 {
			t.Fatal("neighbor table affected")
		}
		if n := countRows(t, db, "SELECT count(*) FROM public.goose_db_version WHERE version_id=4242 AND is_applied"); n != 1 {
			t.Fatal("shared Goose state changed")
		}
	})
}
