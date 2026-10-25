package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/migrations/admin"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

const MigrationTable = Schema + ".goose_db_version"
const MigrationLockID int64 = 0x435245444954

type Migrator struct {
	db       *sql.DB
	provider *goose.Provider
}

func NewMigrator(db *sql.DB) (*Migrator, error) {
	if db == nil {
		return nil, fmt.Errorf("admin migrations: database is required")
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(MigrationLockID), lock.WithLockTimeout(1, 30), lock.WithUnlockTimeout(1, 5))
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, adminmigrations.FS, goose.WithTableName(MigrationTable), goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, err
	}
	return &Migrator{db: db, provider: provider}, nil
}

func (m *Migrator) Up(ctx context.Context) ([]*goose.MigrationResult, error) {
	// Serialize only this schema's bootstrap; the Provider subsequently holds
	// the same namespaced session lock while applying its own migrations.
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, databaseError("migration bootstrap", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", MigrationLockID); err != nil {
		return nil, databaseError("migration bootstrap", err)
	}
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+Schema); err != nil {
		return nil, databaseError("migration bootstrap", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, databaseError("migration bootstrap", err)
	}
	results, err := m.provider.Up(ctx)
	return results, databaseError("migration up", err)
}
func (m *Migrator) Down(ctx context.Context) (*goose.MigrationResult, error) {
	exists, err := m.initialized(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	result, err := m.provider.Down(ctx)
	if errors.Is(err, goose.ErrNoNextVersion) {
		return nil, nil
	}
	return result, databaseError("migration down", err)
}
func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	exists, err := m.initialized(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		result := []*goose.MigrationStatus{}
		for _, source := range m.provider.ListSources() {
			result = append(result, &goose.MigrationStatus{Source: source, State: goose.StatePending})
		}
		return result, nil
	}
	result, err := m.provider.Status(ctx)
	return result, databaseError("migration status", err)
}
func (m *Migrator) initialized(ctx context.Context) (bool, error) {
	var table sql.NullString
	err := m.db.QueryRowContext(ctx, "SELECT to_regclass($1)::text", MigrationTable).Scan(&table)
	return table.Valid, databaseError("migration status", err)
}
