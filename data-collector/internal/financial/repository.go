package financial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("document/application not found")
var ErrUnsupported = errors.New("unsupported financial document context")
var ErrConflict = errors.New("document cannot be changed in current application state")

type Repository struct {
	Pool  *pgxpool.Pool
	Topic string
}

const documentColumns = `id, application_id, type, file_name, mime_type, size_bytes, sha256, storage_key, COALESCE(signed_document_id,''), financial_generation, uploaded_at, signature_state, COALESCE(signature_verdict,'')`

func scanDocument(row pgx.Row) (Document, error) {
	var d Document
	d.Origin = "CLIENT_UPLOAD"
	err := row.Scan(&d.ID, &d.ApplicationID, &d.Type, &d.FileName, &d.MIME, &d.Size, &d.SHA256, &d.StorageKey, &d.SignedDocumentID, &d.Generation, &d.UploadedAt, &d.Signatures.State, &d.Signatures.Verdict)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}
func (r Repository) Get(ctx context.Context, app, id string) (Document, error) {
	return scanDocument(r.Pool.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=$1 AND application_id=$2 AND deleted_at IS NULL`, id, app))
}
func (r Repository) List(ctx context.Context, app string) ([]Document, error) {
	rows, err := r.Pool.Query(ctx, `SELECT `+documentColumns+` FROM documents WHERE application_id=$1 AND deleted_at IS NULL ORDER BY uploaded_at,id`, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, e := scanDocument(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func applicant(ctx context.Context, tx pgx.Tx, app string, editable bool) (Applicant, error) {
	var a Applicant
	var state string
	// Financial context belongs to the application, never to untrusted multipart fields.
	err := tx.QueryRow(ctx, `SELECT c.type,c.inn,
 COALESCE(a.form #>> '{financial_document_context,tax_regime}',''),
 COALESCE(a.form #>> '{financial_document_context,legal_form}',''),a.status
 FROM applications a JOIN companies c ON c.id=a.company_id WHERE a.id=$1 FOR SHARE OF a,c`, app).Scan(&a.Type, &a.INN, &a.TaxRegime, &a.LegalForm, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if editable && state != "DRAFT" && state != "WAITING_FOR_INFO" {
		return a, ErrConflict
	}
	if (a.Type == "SOLE_PROPRIETOR" && a.TaxRegime == "USN_INCOME") || (a.Type == "LEGAL_ENTITY" && a.LegalForm == "OOO") {
		return a, nil
	}
	return a, ErrUnsupported
}
func insertDocument(ctx context.Context, tx pgx.Tx, d Document) error {
	_, err := tx.Exec(ctx, `INSERT INTO documents(id,application_id,type,file_name,mime_type,size_bytes,sha256,storage_key,signed_document_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''))`, d.ID, d.ApplicationID, d.Type, d.FileName, d.MIME, d.Size, d.SHA256, d.StorageKey, d.SignedDocumentID)
	return err
}
func (r Repository) enqueue(ctx context.Context, tx pgx.Tx, d Document, a Applicant) error {
	var generation int
	err := tx.QueryRow(ctx, `UPDATE documents SET financial_generation=financial_generation+1,signature_state='PENDING',signature_verdict=NULL
 WHERE id=$1 AND deleted_at IS NULL RETURNING financial_generation`, d.ID).Scan(&generation)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM documents WHERE signed_document_id=$1 AND application_id=$2 AND deleted_at IS NULL ORDER BY uploaded_at,id`, d.ID, d.ApplicationID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ab, _ := json.Marshal(a)
	sb, _ := json.Marshal(ids)
	id := newID("fjob_")
	_, err = tx.Exec(ctx, `INSERT INTO financial_document_jobs(id,document_id,generation,applicant,signature_ids) VALUES($1,$2,$3,$4,$5)`, id, d.ID, generation, ab, sb)
	if err != nil {
		return err
	}
	ev, _ := json.Marshal(Event{Event: EventRequested, TaskID: id})
	_, err = tx.Exec(ctx, `INSERT INTO outbox(topic,key,payload) VALUES($1,$2,$3)`, r.Topic, d.ID, ev)
	return err
}

// Upload commits the original, detached signatures, and work notification atomically.
func (r Repository) Upload(ctx context.Context, d Document, sigs []Document) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		a, err := applicant(ctx, tx, d.ApplicationID, true)
		if err != nil {
			return err
		}
		target := d
		if d.Type == "SIGNATURE" {
			target, err = scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=$1 AND application_id=$2 AND deleted_at IS NULL FOR UPDATE`, d.SignedDocumentID, d.ApplicationID))
			if err != nil {
				return err
			}
			if target.Type != "FINANCIAL_STATEMENTS" && target.Type != "TAX_RETURN" {
				return ErrUnsupported
			}
		} else if (a.Type == "SOLE_PROPRIETOR" && d.Type != "TAX_RETURN") || (a.Type == "LEGAL_ENTITY" && d.Type != "FINANCIAL_STATEMENTS") {
			return ErrUnsupported
		}
		if err = insertDocument(ctx, tx, d); err != nil {
			return err
		}
		for _, s := range sigs {
			if s.ApplicationID != d.ApplicationID || s.SignedDocumentID != d.ID {
				return ErrConflict
			}
			if err = insertDocument(ctx, tx, s); err != nil {
				return err
			}
		}
		return r.enqueue(ctx, tx, target, a)
	})
}
func (r Repository) Retry(ctx context.Context, app, id string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		a, err := applicant(ctx, tx, app, false)
		if err != nil {
			return err
		}
		d, err := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=$1 AND application_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, app))
		if err != nil {
			return err
		}
		if d.Type != "FINANCIAL_STATEMENTS" && d.Type != "TAX_RETURN" {
			return ErrUnsupported
		}
		return r.enqueue(ctx, tx, d, a)
	})
}
func (r Repository) Result(ctx context.Context, app, id string) (json.RawMessage, error) {
	var raw []byte
	var sameApplicant bool
	err := r.Pool.QueryRow(ctx, `SELECT r.result,
    (j.applicant->>'applicant_type'=c.type AND j.applicant->>'inn'=c.inn
    AND j.applicant->>'tax_regime'=COALESCE(a.form #>> '{financial_document_context,tax_regime}','')
    AND j.applicant->>'legal_form'=COALESCE(a.form #>> '{financial_document_context,legal_form}',''))
    FROM financial_document_results r JOIN documents d ON d.id=r.document_id
    JOIN financial_document_jobs j ON j.id=r.job_id
    JOIN applications a ON a.id=d.application_id JOIN companies c ON c.id=a.company_id
 WHERE d.application_id=$1 AND d.id=$2 AND d.deleted_at IS NULL AND r.generation=d.financial_generation`, app, id).Scan(&raw, &sameApplicant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err == nil && !sameApplicant {
		return nil, ErrConflict
	}
	return raw, err
}

// Handle holds a job row lock through processing. A crash rolls back; redelivery can
// reclaim it. Duplicate deliveries serialize and a completed job is never processed twice.
func (r Repository) Handle(ctx context.Context, p Processor, value []byte) error {
	var ev Event
	if json.Unmarshal(value, &ev) != nil || ev.Event != EventRequested || ev.TaskID == "" {
		return nil
	}
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		var j Job
		var ab, sb []byte
		var status string
		err := tx.QueryRow(ctx, `SELECT id,document_id,generation,applicant,signature_ids,status FROM financial_document_jobs WHERE id=$1 FOR UPDATE`, ev.TaskID).Scan(&j.ID, &j.DocumentID, &j.Generation, &ab, &sb, &status)
		if errors.Is(err, pgx.ErrNoRows) || status == "DONE" {
			return nil
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal(ab, &j.Applicant); err != nil {
			return err
		}
		d, err := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=$1`, j.DocumentID))
		if err != nil {
			return err
		}
		var ids []string
		if err = json.Unmarshal(sb, &ids); err != nil {
			return err
		}
		signatures := []Document{}
		for _, id := range ids {
			s, e := scanDocument(tx.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=$1 AND signed_document_id=$2 AND application_id=$3`, id, d.ID, d.ApplicationID))
			if e != nil {
				return e
			}
			signatures = append(signatures, s)
		}
		result, err := p.Process(ctx, j, d, signatures)
		if err != nil {
			return fmt.Errorf("process financial document: %w", err)
		}
		report, _ := json.Marshal(result.Signature)
		raw, _ := json.Marshal(result)
		_, err = tx.Exec(ctx, `INSERT INTO signature_reports(id,document_id,verdict,policy,policy_satisfied,trust_mode,report,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, result.SignatureReportID, d.ID, result.Signature.Verdict, result.Signature.Policy, result.Signature.PolicyResult.Satisfied, result.Signature.TrustMode, report, result.Signature.VerifiedAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO financial_document_results(id,job_id,document_id,generation,signature_report_id,original_sha256,content_storage_key,result) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8)`, result.ID, j.ID, d.ID, j.Generation, result.SignatureReportID, d.SHA256, result.ContentStorageKey, raw)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE documents SET signature_state='DONE',signature_verdict=$2 WHERE id=$1 AND financial_generation=$3`, d.ID, result.Signature.Verdict, j.Generation)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE financial_document_jobs SET status='DONE' WHERE id=$1`, j.ID)
		return err
	})
}
