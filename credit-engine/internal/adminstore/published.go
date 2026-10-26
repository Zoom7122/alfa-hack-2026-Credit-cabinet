package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

type PublishedModel struct {
	Version            string                 `json:"version"`
	ID                 string                 `json:"id"`
	BorrowerType       contracts.BorrowerType `json:"borrower_type"`
	Definition         model.ModelDefinition  `json:"definition"`
	DefinitionSHA256   string                 `json:"definition_sha256"`
	UsesFacts          bool                   `json:"uses_facts"`
	FactsSchemaVersion string                 `json:"facts_schema_version"`
	FactsSchemaSHA256  string                 `json:"facts_schema_sha256"`
	CreatedBy          string                 `json:"created_by"`
	CreatedAt          time.Time              `json:"created_at"`
}

const publishedSelect = "SELECT model_version,model_id,borrower_type,definition_json,definition_sha256,uses_facts,facts_schema_version,facts_schema_sha256,created_by,created_at FROM credit_engine_admin.model_versions"

func scanPublished(row rowScanner) (PublishedModel, error) {
	var value PublishedModel
	var data string
	err := row.Scan(&value.Version, &value.ID, &value.BorrowerType, &data, &value.DefinitionSHA256, &value.UsesFacts, &value.FactsSchemaVersion, &value.FactsSchemaSHA256, &value.CreatedBy, &value.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PublishedModel{}, ErrNotFound
	}
	if err != nil {
		return PublishedModel{}, databaseError("read model version", err)
	}
	definition, err := storedDefinition(data, value.DefinitionSHA256)
	if err != nil {
		return PublishedModel{}, err
	}
	schema, err := model.FactsSchemaSHA256(definition.Parameters)
	if err != nil {
		return PublishedModel{}, ErrCorrupt
	}
	if definition.ID != value.ID || definition.Version != value.Version || definition.BorrowerType != value.BorrowerType || definition.FactsSchemaVersion != value.FactsSchemaVersion || schema != value.FactsSchemaSHA256 || value.UsesFacts != (schema != "") {
		return PublishedModel{}, ErrCorrupt
	}
	value.Definition = definition
	return value, nil
}
func (r *Repository) GetVersion(ctx context.Context, version string) (PublishedModel, error) {
	if strings.TrimSpace(version) == "" {
		return PublishedModel{}, ErrInvalid
	}
	return scanPublished(r.db.QueryRowContext(ctx, publishedSelect+" WHERE model_version=$1", version))
}
func (r *Repository) ListVersions(ctx context.Context, page Page) ([]PublishedModel, error) {
	page, err := page.normalized()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, publishedSelect+" ORDER BY created_at DESC,model_version LIMIT $1 OFFSET $2", page.Limit, page.Offset)
	if err != nil {
		return nil, databaseError("list model versions", err)
	}
	defer rows.Close()
	result := []PublishedModel{}
	for rows.Next() {
		value, err := scanPublished(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, databaseError("list model versions", rows.Err())
}
func (r *Repository) CopyVersion(ctx context.Context, version, newVersion, author, reason string) (Draft, error) {
	if strings.TrimSpace(newVersion) == "" {
		return Draft{}, ErrInvalid
	}
	source, err := r.GetVersion(ctx, version)
	if err != nil {
		return Draft{}, err
	}
	source.Definition.Version = newVersion
	return r.CreateDraft(ctx, source.Definition, author, reason)
}

// Caller holds the release transaction and namespaced preparation lock. No
// ON CONFLICT update can mutate a published model or its supplier schema.
func insertPublished(ctx context.Context, tx *sql.Tx, definition model.ModelDefinition, author, reason string) error {
	data, err := model.Marshal(definition)
	if err != nil {
		return ErrInvalid
	}
	checksum := release.Hash(data)
	schema, err := model.FactsSchemaSHA256(definition.Parameters)
	if err != nil {
		return ErrInvalid
	}
	var oldHash string
	err = tx.QueryRowContext(ctx, "SELECT definition_sha256 FROM credit_engine_admin.model_versions WHERE lower(model_version)=lower($1)", definition.Version).Scan(&oldHash)
	if err == nil {
		var oldVersion string
		if err := tx.QueryRowContext(ctx, "SELECT model_version FROM credit_engine_admin.model_versions WHERE lower(model_version)=lower($1)", definition.Version).Scan(&oldVersion); err != nil {
			return databaseError("check published version", err)
		}
		if oldHash != checksum || oldVersion != definition.Version {
			return ErrImmutable
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return databaseError("check published version", err)
	}
	if schema != "" {
		var conflicting bool
		err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM credit_engine_admin.model_versions WHERE facts_schema_version=$1 AND facts_schema_sha256<>$2)", definition.FactsSchemaVersion, schema).Scan(&conflicting)
		if err != nil {
			return databaseError("check facts schema", err)
		}
		if conflicting {
			return ErrImmutable
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.model_versions(model_version,model_id,borrower_type,definition_json,definition_sha256,uses_facts,facts_schema_version,facts_schema_sha256,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", definition.Version, definition.ID, definition.BorrowerType, string(data), checksum, schema != "", definition.FactsSchemaVersion, schema, author)
	if err != nil {
		return databaseError("publish model version", err)
	}
	details := changeDetails(nil, data, "", checksum)
	return insertHistory(ctx, tx, "MODEL", definition.Version, "MODEL_PUBLISHED", nil, author, reason, details)
}
