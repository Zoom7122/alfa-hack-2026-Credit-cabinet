package adminstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func changeDetails(before, after []byte, beforeSHA, afterSHA string) []byte {
	decode := func(data []byte) any {
		if len(data) == 0 {
			return nil
		}
		var value any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		_ = d.Decode(&value)
		return value
	}
	paths := []string{}
	var walk func(any, any, string)
	walk = func(left, right any, path string) {
		if reflect.DeepEqual(left, right) {
			return
		}
		if a, ok := left.(map[string]any); ok {
			if b, ok := right.(map[string]any); ok {
				keys := map[string]bool{}
				for key := range a {
					keys[key] = true
				}
				for key := range b {
					keys[key] = true
				}
				for key := range keys {
					escaped := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
					old, oldOK := a[key]
					next, nextOK := b[key]
					if oldOK != nextOK {
						paths = append(paths, path+"/"+escaped)
					} else {
						walk(old, next, path+"/"+escaped)
					}
				}
				return
			}
		}
		if a, ok := left.([]any); ok {
			if b, ok := right.([]any); ok {
				count := max(len(a), len(b))
				for i := range count {
					child := path + "/" + strconv.Itoa(i)
					if i >= len(a) || i >= len(b) {
						paths = append(paths, child)
					} else {
						walk(a[i], b[i], child)
					}
				}
				return
			}
		}
		if path == "" {
			path = "/"
		}
		paths = append(paths, path)
	}
	walk(decode(before), decode(after), "")
	sort.Strings(paths)
	data, _ := json.Marshal(struct {
		BeforeSHA256 string   `json:"before_sha256"`
		AfterSHA256  string   `json:"after_sha256"`
		ChangedPaths []string `json:"changed_paths"`
	}{beforeSHA, afterSHA, paths})
	return data
}

func (r *Repository) GetRevision(ctx context.Context, id string, number int64) (Revision, error) {
	if !uuidPattern.MatchString(id) || number < 1 {
		return Revision{}, ErrInvalid
	}
	var revision Revision
	var data string
	var changes []byte
	err := r.db.QueryRowContext(ctx, "SELECT draft_id::text,revision,definition_json,definition_sha256,author,reason,changes,created_at FROM credit_engine_admin.model_revisions WHERE draft_id=$1 AND revision=$2", id, number).Scan(&revision.DraftID, &revision.Revision, &data, &revision.DefinitionSHA256, &revision.Author, &revision.Reason, &changes, &revision.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, ErrNotFound
	}
	if err != nil {
		return Revision{}, databaseError("read revision", err)
	}
	definition, err := storedDefinition(data, revision.DefinitionSHA256)
	if err != nil {
		return Revision{}, err
	}
	revision.Definition = definition
	revision.Changes = append(json.RawMessage{}, changes...)
	return revision, nil
}
func (r *Repository) ListRevisions(ctx context.Context, id string, page Page) ([]Revision, error) {
	if !uuidPattern.MatchString(id) {
		return nil, ErrInvalid
	}
	page, err := page.normalized()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT draft_id::text,revision,definition_json,definition_sha256,author,reason,changes,created_at FROM credit_engine_admin.model_revisions WHERE draft_id=$1 ORDER BY revision DESC LIMIT $2 OFFSET $3", id, page.Limit, page.Offset)
	if err != nil {
		return nil, databaseError("list revisions", err)
	}
	defer rows.Close()
	result := []Revision{}
	for rows.Next() {
		var revision Revision
		var data string
		var changes []byte
		if err := rows.Scan(&revision.DraftID, &revision.Revision, &data, &revision.DefinitionSHA256, &revision.Author, &revision.Reason, &changes, &revision.CreatedAt); err != nil {
			return nil, databaseError("list revisions", err)
		}
		definition, err := storedDefinition(data, revision.DefinitionSHA256)
		if err != nil {
			return nil, err
		}
		revision.Definition = definition
		revision.Changes = append(json.RawMessage{}, changes...)
		result = append(result, revision)
	}
	return result, databaseError("list revisions", rows.Err())
}

func (r *Repository) History(ctx context.Context, kind, id string, page Page) ([]HistoryEntry, error) {
	if kind != "DRAFT" && kind != "MODEL" && kind != "RELEASE" || strings.TrimSpace(id) == "" {
		return nil, ErrInvalid
	}
	page, err := page.normalized()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, "SELECT history_id,entity_kind,entity_id,action,revision,author,reason,details,occurred_at FROM credit_engine_admin.change_history WHERE entity_kind=$1 AND entity_id=$2 ORDER BY history_id DESC LIMIT $3 OFFSET $4", kind, id, page.Limit, page.Offset)
	if err != nil {
		return nil, databaseError("read history", err)
	}
	defer rows.Close()
	result := []HistoryEntry{}
	for rows.Next() {
		var entry HistoryEntry
		var details []byte
		if err := rows.Scan(&entry.ID, &entry.EntityKind, &entry.EntityID, &entry.Action, &entry.Revision, &entry.Author, &entry.Reason, &details, &entry.OccurredAt); err != nil {
			return nil, databaseError("read history", err)
		}
		entry.Details = append(json.RawMessage{}, details...)
		result = append(result, entry)
	}
	return result, databaseError("read history", rows.Err())
}
