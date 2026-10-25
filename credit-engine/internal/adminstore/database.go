// Package adminstore owns persistent administrative data and local migrations.
// Opening a database never applies migrations or contacts another microservice.
package adminstore

import (
	"database/sql"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const Schema = "credit_engine_admin"

// DatabaseError hides connection strings and SQL parameters from normal logs.
// Its original cause remains available for errors.Is/As and controlled handling.
type DatabaseError struct {
	Operation string
	cause     error
}

func (e *DatabaseError) Error() string { return "admin database " + e.Operation + " failed" }
func (e *DatabaseError) Unwrap() error { return e.cause }
func databaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &DatabaseError{Operation: operation, cause: err}
}

func OpenDatabase(dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, &DatabaseError{Operation: "configuration"}
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, databaseError("configuration", err)
	}
	if config.ConnectTimeout <= 0 {
		config.ConnectTimeout = 5 * time.Second
	}
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)
	return db, nil
}
