//go:build integration

package adminstore_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func TestPersistentSessionsConcurrencyExpiryAndRollback(t *testing.T) {
	db := isolatedDatabase(t)
	ctx := context.Background()
	repo, _ := adminstore.NewRepository(db)
	a := &app.Admin{Settings: config.Admin{DatabaseTimeout: 10 * time.Second}, Database: db, Repository: repo}
	if err := a.Initialize(ctx); err == nil {
		t.Fatal("startup must reject pending migrations")
	}
	if countRows(t, db, "SELECT count(*) FROM information_schema.tables WHERE table_schema=$1", adminstore.Schema) != 0 {
		t.Fatal("startup applied DDL")
	}
	migrator, _ := adminstore.NewMigrator(db)
	_, err := migrator.Up(ctx)
	migrationOK(t, err)
	if err := a.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Initialize(ctx); err != nil {
		t.Fatal("restart seed not idempotent", err)
	}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	newSession := func(index int) adminstore.Session {
		return adminstore.Session{TokenSHA256: fmt.Sprintf("%064x", index), CSRFSHA256: fmt.Sprintf("%064x", index+100), Username: "editor", CreatedAt: now.Add(time.Duration(index) * time.Second), ExpiresAt: now.Add(time.Hour)}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 1; i <= 16; i++ {
		wg.Add(1)
		go func(index int) { defer wg.Done(); errs <- repo.CreateSession(ctx, newSession(index), 3) }(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.admin_sessions"); n != 3 {
		t.Fatal("concurrent logins exceeded limit", n)
	}
	latest := newSession(40)
	if err := repo.CreateSession(ctx, latest, 3); err != nil {
		t.Fatal(err)
	}
	restarted, _ := adminstore.NewRepository(db)
	got, err := restarted.GetSession(ctx, latest.TokenSHA256, now)
	if err != nil || got.CSRFSHA256 != latest.CSRFSHA256 {
		t.Fatal("session did not survive repository restart", err)
	}
	if _, err := repo.GetSession(ctx, latest.TokenSHA256, latest.ExpiresAt); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("expired session accepted", err)
	}
	if err := repo.DeleteSession(ctx, latest.TokenSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSession(ctx, latest.TokenSHA256, now); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("logout failed", err)
	}
	if err := repo.DeleteSession(ctx, latest.TokenSHA256); err != nil {
		t.Fatal("repeat delete failed", err)
	}
	for _, s := range []adminstore.Session{{}, {TokenSHA256: "raw-token", Username: "editor"}, {TokenSHA256: newSession(1).TokenSHA256, CSRFSHA256: newSession(1).CSRFSHA256, Username: "editor", CreatedAt: now, ExpiresAt: now}} {
		if err := repo.CreateSession(ctx, s, 3); !errors.Is(err, adminstore.ErrInvalid) {
			t.Fatal("invalid session accepted", err)
		}
	}
	before := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.admin_sessions")
	dbExec(t, db, `CREATE FUNCTION credit_engine_admin.reject_session_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fictional secret'; END; $$`)
	dbExec(t, db, `CREATE TRIGGER reject_session_test BEFORE INSERT ON credit_engine_admin.admin_sessions FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_session_test()`)
	if err := repo.CreateSession(ctx, newSession(50), 1); err == nil || err.Error() != "admin database create session failed" {
		t.Fatal("DB failure not hidden", err)
	}
	if after := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.admin_sessions"); after != before {
		t.Fatal("failed insert evicted existing sessions")
	}
}
