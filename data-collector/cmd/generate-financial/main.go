// Generate the financial module's Jet models and table builders from migrated PostgreSQL.
package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/go-jet/jet/v2/generator/metadata"
	gen "github.com/go-jet/jet/v2/generator/postgres"
	"github.com/go-jet/jet/v2/generator/template"
	"github.com/go-jet/jet/v2/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required; apply db migrations first")
	}
	schema := os.Getenv("JET_SCHEMA")
	if schema == "" {
		schema = "public"
	}
	output := os.Getenv("JET_OUTPUT")
	if output == "" {
		output = "internal/generated/financial"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	// No data is read: only schema metadata. Keep independent of the database name.
	allow := map[string]bool{"applications": true, "companies": true, "documents": true, "signature_reports": true,
		"financial_document_jobs": true, "financial_document_results": true, "outbox": true}
	tmpl := template.Default(postgres.Dialect).UseSchema(func(s metadata.Schema) template.Schema {
		return template.DefaultSchema(s).UsePath("").UseModel(template.DefaultModel().UseTable(func(t metadata.Table) template.TableModel {
			m := template.DefaultTableModel(t)
			m.Skip = !allow[t.Name]
			return m
		})).UseSQLBuilder(template.DefaultSQLBuilder().UseTable(func(t metadata.Table) template.TableSQLBuilder {
			b := template.DefaultTableSQLBuilder(t)
			b.Skip = !allow[t.Name]
			return b
		}))
	})
	return gen.GenerateDB(db, schema, output, tmpl)
}
