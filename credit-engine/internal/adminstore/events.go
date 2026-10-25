package adminstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

type EventSummary struct {
	EventID       string                 `json:"event_id"`
	ApplicationID string                 `json:"application_id"`
	Kind          contracts.EventType    `json:"kind"`
	BorrowerType  contracts.BorrowerType `json:"borrower_type"`
	BorrowerID    string                 `json:"borrower_id"`
	BorrowerName  string                 `json:"borrower_name"`
	ModelVersion  string                 `json:"model_version"`
	Grade         contracts.Grade        `json:"grade"`
	ScoreTotal    int                    `json:"score_total"`
	OccurredAt    time.Time              `json:"occurred_at"`
	ReceivedAt    time.Time              `json:"received_at"`
	PayloadSHA256 string                 `json:"payload_sha256"`
}
type StoredEvent struct {
	EventSummary `json:"summary"`
	Payload      contracts.AdminEvent `json:"payload"`
}
type EventFilter struct {
	Page          Page
	Kind          contracts.EventType
	BorrowerType  contracts.BorrowerType
	ApplicationID string
	ModelVersion  string
	BorrowerID    string
	From          *time.Time
	To            *time.Time
}

const eventSelect = "SELECT event_id,application_id,kind,borrower_type,occurred_at,received_at,payload_sha256,payload_json FROM credit_engine_admin.credit_events"

func scanEvent(row rowScanner) (StoredEvent, error) {
	var value StoredEvent
	var data string
	err := row.Scan(&value.EventID, &value.ApplicationID, &value.Kind, &value.BorrowerType, &value.OccurredAt, &value.ReceivedAt, &value.PayloadSHA256, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredEvent{}, ErrNotFound
	}
	if err != nil {
		return StoredEvent{}, databaseError("read event", err)
	}
	if release.Hash([]byte(data)) != value.PayloadSHA256 {
		return StoredEvent{}, ErrCorrupt
	}
	payload, err := events.Decode(strings.NewReader(data))
	if err != nil {
		return StoredEvent{}, ErrCorrupt
	}
	stamp, err := time.Parse(time.RFC3339, payload.OccurredAt)
	if err != nil || payload.EventID != value.EventID || payload.ApplicationID != value.ApplicationID || payload.Kind != value.Kind || payload.Borrower.Type != value.BorrowerType || !stamp.Truncate(time.Microsecond).Equal(value.OccurredAt) {
		return StoredEvent{}, ErrCorrupt
	}
	value.Payload = payload
	value.BorrowerID = payload.Borrower.ID
	value.BorrowerName = *payload.Borrower.Name
	value.ModelVersion = payload.Score.Model
	value.Grade = payload.Score.Grade
	value.ScoreTotal = payload.Score.Total
	return value, nil
}

// SaveEvent commits before returning. ON CONFLICT never mutates an immutable
// event; the second statement sees a concurrent winner's committed row.
func (r *Repository) SaveEvent(ctx context.Context, event contracts.AdminEvent) (StoredEvent, error) {
	data, err := events.Canonical(event)
	if err != nil {
		return StoredEvent{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	stamp, _ := time.Parse(time.RFC3339, event.OccurredAt)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return StoredEvent{}, databaseError("save event", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.credit_events(event_id,application_id,kind,borrower_type,occurred_at,payload_json,payload_sha256) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(event_id) DO NOTHING", event.EventID, event.ApplicationID, event.Kind, event.Borrower.Type, stamp, string(data), release.Hash(data))
	if err != nil {
		return StoredEvent{}, databaseError("save event", err)
	}
	stored, err := scanEvent(tx.QueryRowContext(ctx, eventSelect+" WHERE event_id=$1", event.EventID))
	if err != nil {
		return StoredEvent{}, err
	}
	if stored.PayloadSHA256 != release.Hash(data) {
		return StoredEvent{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return StoredEvent{}, databaseError("save event", err)
	}
	return stored, nil
}
func (r *Repository) GetEvent(ctx context.Context, id string) (StoredEvent, error) {
	if strings.TrimSpace(id) == "" || strings.ContainsRune(id, 0) {
		return StoredEvent{}, ErrInvalid
	}
	return scanEvent(r.db.QueryRowContext(ctx, eventSelect+" WHERE event_id=$1", id))
}
func (r *Repository) ListEvents(ctx context.Context, filter EventFilter) ([]EventSummary, error) {
	page, err := filter.Page.normalized()
	if err != nil {
		return nil, err
	}
	if filter.Kind != "" && filter.Kind != contracts.EventTypeLoanIssued && filter.Kind != contracts.EventTypeBankReview && filter.Kind != contracts.EventTypeBorrowerReview {
		return nil, ErrInvalid
	}
	if filter.BorrowerType != "" && filter.BorrowerType != contracts.BorrowerTypeSoleProprietor && filter.BorrowerType != contracts.BorrowerTypeLegalEntity {
		return nil, ErrInvalid
	}
	if filter.From != nil && filter.To != nil && filter.From.After(*filter.To) {
		return nil, ErrInvalid
	}
	if strings.ContainsRune(filter.ApplicationID, 0) || strings.ContainsRune(filter.ModelVersion, 0) || strings.ContainsRune(filter.BorrowerID, 0) {
		return nil, ErrInvalid
	}
	query := eventSelect + " WHERE true"
	args := []any{}
	for _, item := range []struct {
		expr    string
		value   any
		present bool
	}{{"kind", filter.Kind, filter.Kind != ""}, {"borrower_type", filter.BorrowerType, filter.BorrowerType != ""}, {"application_id", filter.ApplicationID, filter.ApplicationID != ""}, {"payload_json::jsonb #>> '{score,model}'", filter.ModelVersion, filter.ModelVersion != ""}, {"payload_json::jsonb #>> '{borrower,id}'", filter.BorrowerID, filter.BorrowerID != ""}} {
		if item.present {
			args = append(args, item.value)
			query += fmt.Sprintf(" AND %s=$%d", item.expr, len(args))
		}
	}
	if filter.From != nil {
		args = append(args, *filter.From)
		query += fmt.Sprintf(" AND occurred_at >= $%d", len(args))
	}
	if filter.To != nil {
		args = append(args, *filter.To)
		query += fmt.Sprintf(" AND occurred_at <= $%d", len(args))
	}
	args = append(args, page.Limit, page.Offset)
	query += fmt.Sprintf(" ORDER BY occurred_at DESC,event_id LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, databaseError("list events", err)
	}
	defer rows.Close()
	result := []EventSummary{}
	for rows.Next() {
		value, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value.EventSummary)
	}
	return result, databaseError("list events", rows.Err())
}
