package adminstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

var ErrNotFound = errors.New("administrative object not found")
var ErrConflict = errors.New("administrative revision conflict")
var ErrImmutable = errors.New("published version is immutable")
var ErrInvalid = errors.New("invalid administrative input")
var ErrCorrupt = errors.New("stored administrative data failed integrity validation")

type ConflictError struct{ CurrentRevision int64 }

func (e *ConflictError) Error() string { return ErrConflict.Error() }
func (e *ConflictError) Unwrap() error { return ErrConflict }

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("admin repository: database is required")
	}
	return &Repository{db: db}, nil
}

type Page struct {
	Limit  int
	Offset int
}

func (page Page) normalized() (Page, error) {
	if page.Limit == 0 {
		page.Limit = 50
	}
	if page.Limit < 1 || page.Limit > 200 || page.Offset < 0 {
		return Page{}, ErrInvalid
	}
	return page, nil
}

type Draft struct {
	ID               string                `json:"id"`
	Revision         int64                 `json:"revision"`
	Definition       model.ModelDefinition `json:"definition"`
	DefinitionSHA256 string                `json:"definition_sha256"`
	CreatedBy        string                `json:"created_by"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
}
type Revision struct {
	DraftID          string                `json:"draft_id"`
	Revision         int64                 `json:"revision"`
	Definition       model.ModelDefinition `json:"definition"`
	DefinitionSHA256 string                `json:"definition_sha256"`
	Author           string                `json:"author"`
	Reason           string                `json:"reason"`
	Changes          json.RawMessage       `json:"changes"`
	CreatedAt        time.Time             `json:"created_at"`
}
type HistoryEntry struct {
	ID         int64           `json:"id"`
	EntityKind string          `json:"entity_kind"`
	EntityID   string          `json:"entity_id"`
	Action     string          `json:"action"`
	Revision   *int64          `json:"revision"`
	Author     string          `json:"author"`
	Reason     string          `json:"reason"`
	Details    json.RawMessage `json:"details"`
	OccurredAt time.Time       `json:"occurred_at"`
}

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func newDraftID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}
func validActor(author, reason string) bool {
	return strings.TrimSpace(author) != "" && len(author) <= 256 && strings.TrimSpace(reason) != "" && len(reason) <= 16<<10
}

// Drafts require a transport-valid definition, not valid business weights or
// conditions. Publishing uses the complete shared model/release validator.
func draftDocument(definition model.ModelDefinition) ([]byte, error) {
	if definition.BorrowerType != contracts.BorrowerTypeSoleProprietor && definition.BorrowerType != contracts.BorrowerTypeLegalEntity {
		return nil, ErrInvalid
	}
	data, err := model.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := model.Decode(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return data, nil
}
func storedDefinition(data, checksum string) (model.ModelDefinition, error) {
	if release.Hash([]byte(data)) != checksum {
		return model.ModelDefinition{}, ErrCorrupt
	}
	definition, err := model.Decode(strings.NewReader(data))
	if err != nil {
		return model.ModelDefinition{}, ErrCorrupt
	}
	return definition, nil
}

func (r *Repository) CreateDraft(ctx context.Context, definition model.ModelDefinition, author, reason string) (Draft, error) {
	if !validActor(author, reason) {
		return Draft{}, ErrInvalid
	}
	data, err := draftDocument(definition)
	if err != nil {
		return Draft{}, err
	}
	id, err := newDraftID()
	if err != nil {
		return Draft{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Draft{}, databaseError("create draft", err)
	}
	defer tx.Rollback()
	var created, updated time.Time
	err = tx.QueryRowContext(ctx, "INSERT INTO credit_engine_admin.model_drafts(draft_id,model_id,borrower_type,title,current_revision,created_by) VALUES($1,$2,$3,$4,1,$5) RETURNING created_at,updated_at", id, definition.ID, definition.BorrowerType, definition.Title, author).Scan(&created, &updated)
	if err != nil {
		return Draft{}, databaseError("create draft", err)
	}
	changes := changeDetails(nil, data, "", release.Hash(data))
	if err := insertRevision(ctx, tx, id, 1, data, author, reason, changes); err != nil {
		return Draft{}, err
	}
	if err := insertHistory(ctx, tx, "DRAFT", id, "DRAFT_CREATED", int64ptr(1), author, reason, changes); err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, databaseError("create draft", err)
	}
	owned, err := storedDefinition(string(data), release.Hash(data))
	if err != nil {
		return Draft{}, err
	}
	return Draft{ID: id, Revision: 1, Definition: owned, DefinitionSHA256: release.Hash(data), CreatedBy: author, CreatedAt: created, UpdatedAt: updated}, nil
}

const draftSelect = "SELECT d.draft_id::text,d.current_revision,v.definition_json,v.definition_sha256,d.created_by,d.created_at,d.updated_at FROM credit_engine_admin.model_drafts d JOIN credit_engine_admin.model_revisions v ON v.draft_id=d.draft_id AND v.revision=d.current_revision"

type rowScanner interface{ Scan(...any) error }

func scanDraft(row rowScanner) (Draft, error) {
	var d Draft
	var data string
	if err := row.Scan(&d.ID, &d.Revision, &data, &d.DefinitionSHA256, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Draft{}, ErrNotFound
		}
		return Draft{}, databaseError("read draft", err)
	}
	definition, err := storedDefinition(data, d.DefinitionSHA256)
	if err != nil {
		return Draft{}, err
	}
	d.Definition = definition
	return d, nil
}
func (r *Repository) GetDraft(ctx context.Context, id string) (Draft, error) {
	if !uuidPattern.MatchString(id) {
		return Draft{}, ErrInvalid
	}
	return scanDraft(r.db.QueryRowContext(ctx, draftSelect+" WHERE d.draft_id=$1 AND d.deleted_at IS NULL", id))
}
func (r *Repository) ListDrafts(ctx context.Context, page Page) ([]Draft, error) {
	page, err := page.normalized()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, draftSelect+" WHERE d.deleted_at IS NULL ORDER BY d.updated_at DESC,d.draft_id LIMIT $1 OFFSET $2", page.Limit, page.Offset)
	if err != nil {
		return nil, databaseError("list drafts", err)
	}
	defer rows.Close()
	drafts := []Draft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	return drafts, databaseError("list drafts", rows.Err())
}

func (r *Repository) UpdateDraft(ctx context.Context, id string, expected int64, definition model.ModelDefinition, author, reason string) (Draft, error) {
	if !uuidPattern.MatchString(id) || expected < 1 || expected == int64(^uint64(0)>>1) || !validActor(author, reason) {
		return Draft{}, ErrInvalid
	}
	data, err := draftDocument(definition)
	if err != nil {
		return Draft{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Draft{}, databaseError("update draft", err)
	}
	defer tx.Rollback()
	current, err := lockedDraft(ctx, tx, id)
	if err != nil {
		return Draft{}, err
	}
	if current.Revision != expected {
		return Draft{}, &ConflictError{CurrentRevision: current.Revision}
	}
	if current.Definition.BorrowerType != definition.BorrowerType {
		return Draft{}, ErrInvalid
	}
	previous, err := model.Marshal(current.Definition)
	if err != nil {
		return Draft{}, ErrCorrupt
	}
	changes := changeDetails(previous, data, current.DefinitionSHA256, release.Hash(data))
	result, err := tx.ExecContext(ctx, "UPDATE credit_engine_admin.model_drafts SET model_id=$1,title=$2,current_revision=$3,updated_at=now() WHERE draft_id=$4 AND current_revision=$5 AND deleted_at IS NULL", definition.ID, definition.Title, expected+1, id, expected)
	if err != nil {
		return Draft{}, databaseError("update draft", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return Draft{}, databaseError("update draft", err)
	} else if count != 1 {
		return Draft{}, &ConflictError{CurrentRevision: current.Revision}
	}
	if err := insertRevision(ctx, tx, id, expected+1, data, author, reason, changes); err != nil {
		return Draft{}, err
	}
	if err := insertHistory(ctx, tx, "DRAFT", id, "DRAFT_UPDATED", int64ptr(expected+1), author, reason, changes); err != nil {
		return Draft{}, err
	}
	updated, err := scanDraft(tx.QueryRowContext(ctx, draftSelect+" WHERE d.draft_id=$1", id))
	if err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, databaseError("update draft", err)
	}
	return updated, nil
}

func (r *Repository) DeleteDraft(ctx context.Context, id string, expected int64, author, reason string) error {
	if !uuidPattern.MatchString(id) || expected < 1 || !validActor(author, reason) {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return databaseError("delete draft", err)
	}
	defer tx.Rollback()
	current, err := lockedDraft(ctx, tx, id)
	if err != nil {
		return err
	}
	if current.Revision != expected {
		return &ConflictError{CurrentRevision: current.Revision}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE credit_engine_admin.model_drafts SET deleted_at=now(),updated_at=now() WHERE draft_id=$1 AND current_revision=$2", id, expected); err != nil {
		return databaseError("delete draft", err)
	}
	details, _ := json.Marshal(map[string]any{"definition_sha256": current.DefinitionSHA256, "deleted": true})
	if err := insertHistory(ctx, tx, "DRAFT", id, "DRAFT_DELETED", &expected, author, reason, details); err != nil {
		return err
	}
	return databaseError("delete draft", tx.Commit())
}
func lockedDraft(ctx context.Context, tx *sql.Tx, id string) (Draft, error) {
	// Lock the parent before joining its current revision. A joined FOR UPDATE
	// can lose the old join row while waiting for a concurrent writer, making
	// an existing draft look absent instead of reporting its newer revision.
	var revision int64
	err := tx.QueryRowContext(ctx, "SELECT current_revision FROM credit_engine_admin.model_drafts WHERE draft_id=$1 AND deleted_at IS NULL FOR UPDATE", id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, ErrNotFound
	}
	if err != nil {
		return Draft{}, databaseError("lock draft", err)
	}
	return scanDraft(tx.QueryRowContext(ctx, draftSelect+" WHERE d.draft_id=$1 AND d.deleted_at IS NULL", id))
}
func int64ptr(value int64) *int64 { return &value }
func insertRevision(ctx context.Context, tx *sql.Tx, id string, revision int64, data []byte, author, reason string, changes []byte) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.model_revisions(draft_id,revision,definition_json,definition_sha256,author,reason,changes) VALUES($1,$2,$3,$4,$5,$6,$7)", id, revision, string(data), release.Hash(data), author, reason, string(changes))
	return databaseError("save revision", err)
}
func insertHistory(ctx context.Context, tx *sql.Tx, kind, id, action string, revision *int64, author, reason string, details []byte) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.change_history(entity_kind,entity_id,action,revision,author,reason,details) VALUES($1,$2,$3,$4,$5,$6,$7)", kind, id, action, revision, author, reason, string(details))
	return databaseError("save history", err)
}
