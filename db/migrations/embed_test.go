package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/db/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsCanBeLoaded(t *testing.T) {
	// Loading the provider checks migration versions without connecting to a database.
	db, err := sql.Open("pgx", "postgres://migration-test@localhost/migration-test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.ListSources()) == 0 {
		t.Fatal("no embedded migrations found")
	}
}
