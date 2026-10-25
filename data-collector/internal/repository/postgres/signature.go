package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// заявитель, кто подал заявку, последняя задача на сбор и её результат по ФНС и МЧД
	selectSignatureContext = `
SELECT c.inn, c.type, c.name, coalesce(c.ogrn, ''), u.id, coalesce(u.inn, ''), coalesce(m.role, ''),
       t.id, t.status, t.request, rc.id, rc.fns, rc.m4d, rc.sources
FROM applications a
JOIN companies c ON c.id = a.company_id
JOIN users u ON u.id = a.created_by
LEFT JOIN company_members m ON m.user_id = a.created_by AND m.company_id = a.company_id
LEFT JOIN LATERAL (SELECT id, status, request FROM collection_tasks
                   WHERE application_id = a.id ORDER BY created_at DESC LIMIT 1) t ON true
LEFT JOIN raw_collections rc ON rc.task_id = t.id
WHERE a.id = $1`

	selectReportExists = `SELECT EXISTS (SELECT 1 FROM signature_reports WHERE idempotency_key = $1)`

	insertSignatureReport = `
INSERT INTO signature_reports (id, document_id, verdict, policy, policy_satisfied, trust_mode, report, verified_at,
                               signature_document_id, document_sha256, signature_sha256, profile,
                               integrity_status, format_status, certificate_status, identity_status, authority_status,
                               decision, failure_reason, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
ON CONFLICT (idempotency_key) DO NOTHING`

	updateDocumentSignature = `
UPDATE documents SET signature_state = $3, signature_verdict = $4
WHERE application_id = $1 AND id = ANY($2)`
)

// SignatureRepository — проверка ЭП документов заявки: документы и контекст для worker'а подписи,
// отчёты в signature_reports. Отчёт пишется одной транзакцией со сменой signature_state документов
// и событием в журнале заявки; второй отчёт с тем же ключом идемпотентности не пишется.
type SignatureRepository struct {
	pool *pgxpool.Pool
}

func NewSignatureRepository(pool *pgxpool.Pool) *SignatureRepository {
	return &SignatureRepository{pool: pool}
}

// Documents — документы заявки (без удалённых) в порядке id: путь к файлу берётся отсюда, а не из события.
func (r *SignatureRepository) Documents(ctx context.Context, applicationID string) ([]domain.Document, error) {
	return queryDocuments(ctx, r.pool, applicationID)
}

// Context — заявитель, представитель и последний сбор по заявке. false — заявки нет.
func (r *SignatureRepository) Context(ctx context.Context, applicationID string) (domain.SignatureContext, bool, error) {
	sc := domain.SignatureContext{ApplicationID: applicationID}
	var (
		companyType, role                string
		taskID, taskStatus, collectionID *string
		request, fns, m4d, sources       []byte
	)
	err := r.pool.QueryRow(ctx, selectSignatureContext, applicationID).Scan(&sc.Company.INN, &companyType, &sc.Company.Name,
		&sc.Company.OGRN, &sc.Representative.UserID, &sc.Representative.INN, &role, &taskID, &taskStatus, &request,
		&collectionID, &fns, &m4d, &sources)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc, false, nil
	}
	if err != nil {
		return sc, false, fmt.Errorf("select signature context: %w", err)
	}
	sc.Company.Type, sc.Representative.Role = domain.CompanyType(companyType), domain.RepresentativeRole(role)
	if taskID == nil || taskStatus == nil {
		return sc, true, nil
	}
	var rec requestRecord
	if err := json.Unmarshal(request, &rec); err != nil {
		return sc, false, fmt.Errorf("decode request: %w", err)
	}
	c := &domain.SignatureCollection{TaskID: *taskID, Status: domain.TaskStatus(*taskStatus), Input: fromRequestRecord(rec, nil)}
	if collectionID != nil {
		c.CollectionID, c.FNS, c.M4D = *collectionID, nullJSON(fns), nullJSON(m4d)
		var records []sourceRecord
		if err := json.Unmarshal(sources, &records); err != nil {
			return sc, false, fmt.Errorf("decode sources: %w", err)
		}
		for _, s := range records {
			switch domain.System(s.Source) {
			case domain.SystemFNS:
				c.FNSState = domain.CollectState(s.State)
			case domain.SystemM4D:
				c.M4DState = domain.CollectState(s.State)
			}
		}
	}
	sc.Collection = c
	return sc, true, nil
}

// ReportExists сообщает, есть ли уже отчёт с этим ключом идемпотентности.
func (r *SignatureRepository) ReportExists(ctx context.Context, key string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, selectReportExists, key).Scan(&exists); err != nil {
		return false, fmt.Errorf("select signature report: %w", err)
	}
	return exists, nil
}

// SaveReport одной транзакцией пишет отчёт, переводит документ и файл подписи в DONE или FAILED
// с вердиктом и пишет document.signatures_verified в журнал заявки. false — отчёт с этим ключом
// уже есть (повторная доставка события): ничего не меняется.
func (r *SignatureRepository) SaveReport(ctx context.Context, rep domain.SignatureReport) (bool, error) {
	saved := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, insertSignatureReport, rep.ID, rep.DocumentID, rep.Verdict, rep.Policy,
			rep.Decision == domain.VerificationVerified, rep.TrustMode, string(rep.Report), rep.VerifiedAt,
			nullString(rep.SignatureDocumentID), rep.DocumentSHA256, nullString(rep.SignatureSHA256), rep.Profile,
			string(rep.Integrity), string(rep.Format), string(rep.Certificate), string(rep.Identity), string(rep.Authority),
			string(rep.Decision), nullString(rep.FailureReason), rep.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("insert signature report: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		saved = true
		ids := []string{rep.DocumentID}
		if rep.SignatureDocumentID != "" && rep.SignatureDocumentID != rep.DocumentID {
			ids = append(ids, rep.SignatureDocumentID)
		}
		if _, err := tx.Exec(ctx, updateDocumentSignature, rep.ApplicationID, ids, string(rep.DocumentState), rep.Verdict); err != nil {
			return fmt.Errorf("update document signature state: %w", err)
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: rep.ApplicationID, Type: "document.signatures_verified",
			Payload: map[string]any{"document_id": rep.DocumentID, "signature_document_id": rep.SignatureDocumentID,
				"report_id": rep.ID, "decision": rep.Decision, "verdict": rep.Verdict, "profile": rep.Profile}})
	})
	if err != nil {
		return false, err
	}
	return saved, nil
}

// nullJSON — nil вместо JSON null.
func nullJSON(b []byte) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return b
}
