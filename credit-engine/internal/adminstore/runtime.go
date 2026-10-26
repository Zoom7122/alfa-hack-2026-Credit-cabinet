package adminstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
)

type RuntimeModels struct {
	repository *Repository
	cache      sync.Map
}

func NewRuntimeModels(r *Repository) *RuntimeModels { return &RuntimeModels{repository: r} }

// Resolve always reads the active pointer from PostgreSQL. Only immutable rule
// engines are cached; an outage cannot silently select a stale active version.
func (r *RuntimeModels) Resolve(ctx context.Context, version string, borrower contracts.BorrowerType) (*modelbindings.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	query := publishedSelect + " WHERE model_version=$1"
	args := []any{version}
	if version == "" {
		query = publishedSelect + " WHERE model_version=(SELECT model_version FROM credit_engine_admin.active_models WHERE borrower_type=$1)"
		args = []any{borrower}
	}
	p, err := scanPublished(r.repository.db.QueryRowContext(ctx, query, args...))
	if err != nil {
		if version != "" && errors.Is(err, ErrNotFound) {
			return nil, modelbindings.ErrUnknownVersion
		}
		return nil, err
	}
	if p.BorrowerType != borrower {
		return nil, modelbindings.ErrBorrowerType
	}
	key := p.Version + ":" + p.DefinitionSHA256
	if entry, ok := r.cache.Load(key); ok {
		return entry.(*modelbindings.Entry), nil
	}
	entry, err := modelbindings.FromDefinition(p.Definition)
	if err != nil {
		return nil, err
	}
	actual, _ := r.cache.LoadOrStore(key, entry)
	return actual.(*modelbindings.Entry), nil
}
func (r *RuntimeModels) Models(ctx context.Context) (modelbindings.RuntimeMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := modelbindings.RuntimeMetadata{FormatVersion: 1, Models: []modelbindings.Metadata{}}
	// One SQL snapshot prevents mixing pointer changes with the catalog listing.
	rows, err := r.repository.db.QueryContext(ctx, `SELECT m.model_version,m.model_id,m.borrower_type,m.definition_json,m.definition_sha256,m.uses_facts,m.facts_schema_version,m.facts_schema_sha256,m.created_by,m.created_at,EXISTS(SELECT 1 FROM credit_engine_admin.active_models a WHERE a.model_version=m.model_version) FROM credit_engine_admin.model_versions m ORDER BY m.model_version`)
	if err != nil {
		return result, databaseError("runtime model metadata", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PublishedModel
		var data string
		var active bool
		if err := rows.Scan(&p.Version, &p.ID, &p.BorrowerType, &data, &p.DefinitionSHA256, &p.UsesFacts, &p.FactsSchemaVersion, &p.FactsSchemaSHA256, &p.CreatedBy, &p.CreatedAt, &active); err != nil {
			return result, databaseError("runtime metadata row", err)
		}
		d, err := storedDefinition(data, p.DefinitionSHA256)
		if err != nil {
			return result, err
		}
		m, err := modelbindings.DefinitionMetadata(d)
		if err != nil {
			return result, err
		}
		if m.Version != p.Version || m.ID != p.ID || m.BorrowerType != p.BorrowerType || m.FactsSchemaVersion != p.FactsSchemaVersion || m.FactsSchemaSHA256 != p.FactsSchemaSHA256 || m.UsesFacts != p.UsesFacts {
			return result, ErrCorrupt
		}
		m.Active = active
		result.Models = append(result.Models, m)
	}
	return result, databaseError("runtime model metadata", rows.Err())
}
