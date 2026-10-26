package adminstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

type ActiveModel struct {
	BorrowerType     contracts.BorrowerType `json:"borrower_type"`
	Version          string                 `json:"version"`
	DefinitionSHA256 string                 `json:"definition_sha256"`
	Revision         int64                  `json:"revision"`
	ChangedBy        string                 `json:"changed_by"`
	Reason           string                 `json:"reason"`
	ChangedAt        time.Time              `json:"changed_at"`
}

const activeSelect = `SELECT a.borrower_type,a.model_version,m.definition_sha256,a.revision,a.changed_by,a.reason,a.changed_at FROM credit_engine_admin.active_models a JOIN credit_engine_admin.model_versions m ON m.model_version=a.model_version`

func scanActive(row rowScanner) (ActiveModel, error) {
	var a ActiveModel
	err := row.Scan(&a.BorrowerType, &a.Version, &a.DefinitionSHA256, &a.Revision, &a.ChangedBy, &a.Reason, &a.ChangedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, databaseError("read active model", err)
}
func (r *Repository) ActiveModels(ctx context.Context) ([]ActiveModel, error) {
	rows, err := r.db.QueryContext(ctx, activeSelect+" ORDER BY a.borrower_type")
	if err != nil {
		return nil, databaseError("list active models", err)
	}
	defer rows.Close()
	result := []ActiveModel{}
	for rows.Next() {
		a, err := scanActive(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, databaseError("list active models", rows.Err())
}

// SeedActiveModels never replaces an administrator's previous choice.
func (r *Repository) SeedActiveModels(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO credit_engine_admin.active_models(borrower_type,model_version,revision,changed_by,reason)
 SELECT borrower_type,model_version,1,'system','Initial v2 model'
 FROM credit_engine_admin.model_versions WHERE model_version IN ('ip-v2','legal-v2')
 ON CONFLICT (borrower_type) DO NOTHING`)
	return databaseError("seed active models", err)
}

func validateActivation(d model.ModelDefinition) error {
	bindings, err := sourcecatalog.Bindings(d.BorrowerType)
	if err != nil {
		return ErrInvalid
	}
	result := model.Validate(d, bindings)
	if !result.Valid {
		return &modelgen.ValidationError{Version: d.Version, Result: result}
	}
	return nil
}

func (r *Repository) ApplyDraft(ctx context.Context, id string, expected, activeRevision int64, author, reason string) (ActiveModel, error) {
	if !uuidPattern.MatchString(id) || expected < 1 || activeRevision < 1 || !validActor(author, reason) {
		return ActiveModel{}, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ActiveModel{}, databaseError("apply draft", err)
	}
	defer tx.Rollback()
	// Match the lock order of release preparation: global publication lock, draft, active pointer.
	if err := lockPreparation(ctx, tx); err != nil {
		return ActiveModel{}, err
	}
	draft, err := lockedDraft(ctx, tx, id)
	if err != nil {
		return ActiveModel{}, err
	}
	if draft.Revision != expected {
		return ActiveModel{}, &ConflictError{CurrentRevision: draft.Revision}
	}
	if err := validateActivation(draft.Definition); err != nil {
		return ActiveModel{}, err
	}
	if err := insertPublished(ctx, tx, draft.Definition, author, reason); err != nil {
		return ActiveModel{}, err
	}
	result, err := activate(ctx, tx, draft.Definition.BorrowerType, draft.Definition.Version, activeRevision, author, reason)
	if err != nil {
		return ActiveModel{}, err
	}
	if err := tx.Commit(); err != nil {
		return ActiveModel{}, databaseError("apply draft", err)
	}
	return result, nil
}

func (r *Repository) ActivateVersion(ctx context.Context, borrower contracts.BorrowerType, version string, expected int64, author, reason string) (ActiveModel, error) {
	if expected < 1 || !validActor(author, reason) {
		return ActiveModel{}, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ActiveModel{}, databaseError("activate version", err)
	}
	defer tx.Rollback()
	if err := lockPreparation(ctx, tx); err != nil {
		return ActiveModel{}, err
	}
	published, err := scanPublished(tx.QueryRowContext(ctx, publishedSelect+" WHERE model_version=$1", version))
	if err != nil {
		return ActiveModel{}, err
	}
	if published.BorrowerType != borrower {
		return ActiveModel{}, ErrInvalid
	}
	if err := validateActivation(published.Definition); err != nil {
		return ActiveModel{}, err
	}
	result, err := activate(ctx, tx, borrower, version, expected, author, reason)
	if err != nil {
		return ActiveModel{}, err
	}
	if err := tx.Commit(); err != nil {
		return ActiveModel{}, databaseError("activate version", err)
	}
	return result, nil
}

func activate(ctx context.Context, tx *sql.Tx, borrower contracts.BorrowerType, version string, expected int64, author, reason string) (ActiveModel, error) {
	old, err := scanActive(tx.QueryRowContext(ctx, activeSelect+" WHERE a.borrower_type=$1 FOR UPDATE OF a", borrower))
	if err != nil {
		return ActiveModel{}, err
	}
	// An identical retry after a lost response is successful without another history entry.
	if old.Version == version {
		return old, nil
	}
	if old.Revision != expected {
		return ActiveModel{}, &ConflictError{CurrentRevision: old.Revision}
	}
	_, err = tx.ExecContext(ctx, `UPDATE credit_engine_admin.active_models SET model_version=$2,revision=revision+1,changed_by=$3,reason=$4,changed_at=now() WHERE borrower_type=$1`, borrower, version, author, reason)
	if err != nil {
		return ActiveModel{}, databaseError("activate model", err)
	}
	current, err := scanActive(tx.QueryRowContext(ctx, activeSelect+" WHERE a.borrower_type=$1", borrower))
	if err != nil {
		return ActiveModel{}, err
	}
	details, _ := json.Marshal(map[string]any{"previous_version": old.Version, "version": version, "definition_sha256": current.DefinitionSHA256, "activation_revision": current.Revision})
	if err := insertHistory(ctx, tx, "MODEL", version, "MODEL_ACTIVATED", nil, author, reason, details); err != nil {
		return ActiveModel{}, err
	}
	return current, nil
}
