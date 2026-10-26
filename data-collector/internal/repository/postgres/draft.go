package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func (r *ApplicationRepository) Draft(ctx context.Context, id string) (domain.SavedDraft, bool, error) {
	var out domain.SavedDraft
	var form []byte
	err := r.pool.QueryRow(ctx, `SELECT form, version, updated_at FROM applications WHERE id = $1`, id).
		Scan(&form, &out.Version, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.Data, err = draftFromForm(form)
	return out, true, err
}

// SaveDraft не отправляет анкету и не создаёт задач сбора. Версия защищает правки другой вкладки.
func (r *ApplicationRepository) SaveDraft(ctx context.Context, id string, data json.RawMessage, version int) (domain.SavedDraft, error) {
	var out domain.SavedDraft
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var status string
		var current int
		if err := tx.QueryRow(ctx, `SELECT status, version FROM applications WHERE id = $1 FOR UPDATE`, id).Scan(&status, &current); err != nil {
			return err
		}
		if status != string(domain.ApplicationDraft) {
			return domain.ErrApplicationNotDraft
		}
		if current != version {
			return domain.ErrVersionConflict
		}
		// JSON черновика сохраняется целиком: удалённые значения не возвращаются из старой анкеты.
		return tx.QueryRow(ctx, `UPDATE applications SET
            form = form || jsonb_build_object('draft', $2::jsonb, 'product_code', $2::jsonb -> 'product_code',
                       'requested', $2::jsonb -> 'requested', 'purpose', $2::jsonb -> 'purpose'),
            product_code = nullif($2::jsonb ->> 'product_code', ''),
            amount = CASE WHEN ($2::jsonb #>> '{requested,amount}')::bigint > 0 THEN ($2::jsonb #>> '{requested,amount}')::bigint END,
            term_months = CASE WHEN ($2::jsonb #>> '{requested,term_months}')::int > 0 THEN ($2::jsonb #>> '{requested,term_months}')::int END,
            version = version + 1, updated_at = now()
            WHERE id = $1 RETURNING version, updated_at`, id, data).Scan(&out.Version, &out.UpdatedAt)
	})
	out.Data = data
	return out, err
}

// Совместимость с уже существующими formRecord: читаем только реально записанные значения.
func draftFromForm(raw []byte) (json.RawMessage, error) {
	var form map[string]json.RawMessage
	if err := json.Unmarshal(raw, &form); err != nil {
		return nil, err
	}
	if data, ok := form["draft"]; ok && string(data) != "null" {
		return data, nil
	}
	out := map[string]json.RawMessage{}
	copyFields := func(src map[string]json.RawMessage, keys ...string) {
		for _, key := range keys {
			if value, ok := src[key]; ok && string(value) != "null" {
				out[key] = value
			}
		}
	}
	copyFields(form, "product_code", "requested", "purpose", "scoring")
	var q map[string]json.RawMessage
	if len(form["questionnaire"]) > 0 {
		if err := json.Unmarshal(form["questionnaire"], &q); err != nil {
			return nil, fmt.Errorf("decode questionnaire: %w", err)
		}
	}
	copyFields(q, "age", "marital_status", "children_count")
	var collect map[string]json.RawMessage
	if len(q["collect"]) > 0 {
		if err := json.Unmarshal(q["collect"], &collect); err != nil {
			return nil, err
		}
	}
	copyFields(collect, "director", "other_bank_biks", "collateral", "poa_number", "representative_inn")
	if value, ok := out["director"]; ok && string(value) != "null" {
		out["with_director"] = json.RawMessage(`true`)
	}
	var consents map[string]consentRecord
	if len(collect["consents"]) > 0 && string(collect["consents"]) != "null" {
		if err := json.Unmarshal(collect["consents"], &consents); err != nil {
			return nil, err
		}
	}
	signed := map[string]string{}
	for key, consent := range consents {
		if consent.SignedAt != "" {
			signed[key] = consent.SignedAt
		}
		if key == "tax" && consent.Scope != nil {
			out["tax_scope"], _ = json.Marshal(consent.Scope)
		}
	}
	if len(signed) > 0 {
		out["consents"], _ = json.Marshal(signed)
	}
	return json.Marshal(out)
}
