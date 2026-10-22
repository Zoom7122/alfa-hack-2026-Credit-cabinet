// Package migrations встраивает SQL-миграции Goose в бинарник.
package migrations

import "embed"

// FS — все миграции схемы, по одному файлу на версию (00001_init.sql, ...).
//
//go:embed *.sql
var FS embed.FS
