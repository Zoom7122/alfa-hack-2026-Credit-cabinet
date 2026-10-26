package financial

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/generated/financial/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/generated/financial/table"
)

var ErrNotFound = errors.New("document/application not found")
var ErrUnsupported = errors.New("unsupported financial document context")
var ErrConflict = errors.New("document cannot be changed in current application state")

type Repository struct {
	Pool           *pgxpool.Pool
	Topic          string
	DocumentsTopic string
}

// Use the connection's search_path (public in production, isolated schemas in tests).
var (
	documents    = table.Documents.FromSchema("")
	applications = table.Applications.FromSchema("")
	companies    = table.Companies.FromSchema("")
	jobs         = table.FinancialDocumentJobs.FromSchema("")
	results      = table.FinancialDocumentResults.FromSchema("")
	reports      = table.SignatureReports.FromSchema("")
	outboxTable  = table.Outbox.FromSchema("")
)

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type executer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Jet builds SQL and arguments; pgx maps columns positionally to the generated model.
// AllColumns and the model are generated together, so no handwritten column/Scan lists.
func queryOne[T any](ctx context.Context, q queryer, stmt jet.Statement) (T, error) {
	sql, args := stmt.Sql()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[T])
}
func queryMany[T any](ctx context.Context, q queryer, stmt jet.Statement) ([]T, error) {
	sql, args := stmt.Sql()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[T])
}
func execute(ctx context.Context, q executer, stmt jet.Statement) (pgconn.CommandTag, error) {
	sql, args := stmt.Sql()
	return q.Exec(ctx, sql, args...)
}
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func document(m model.Documents) Document {
	return Document{ID: m.ID, ApplicationID: m.ApplicationID, Type: m.Type, FileName: m.FileName, MIME: m.MimeType,
		Size: m.SizeBytes, SHA256: m.Sha256, StorageKey: m.StorageKey, SignedDocumentID: value(m.SignedDocumentID),
		Generation: int(m.FinancialGeneration), UploadedAt: m.UploadedAt, Origin: "CLIENT_UPLOAD",
		InfoRequestID: value(m.InfoRequestID), Recognition: json.RawMessage(value(m.Recognition)),
		Signatures: SignatureSummary{State: m.SignatureState, Verdict: value(m.SignatureVerdict)}}
}
func (r Repository) Get(ctx context.Context, app, id string) (Document, error) {
	m, err := queryOne[model.Documents](ctx, r.Pool, documents.SELECT(documents.AllColumns).WHERE(documents.ID.EQ(jet.String(id)).AND(documents.ApplicationID.EQ(jet.String(app))).AND(documents.DeletedAt.IS_NULL())))
	return document(m), notFound(err)
}
func (r Repository) List(ctx context.Context, app string) ([]Document, error) {
	rows, err := queryMany[model.Documents](ctx, r.Pool, documents.SELECT(documents.AllColumns).WHERE(documents.ApplicationID.EQ(jet.String(app)).AND(documents.DeletedAt.IS_NULL())).ORDER_BY(documents.UploadedAt, documents.ID))
	if err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(rows))
	for _, m := range rows {
		out = append(out, document(m))
	}
	return out, nil
}
func applicantContext(typ, inn, form string, registry ...string) (Applicant, error) {
	var fields struct {
		Context struct {
			TaxRegime string `json:"tax_regime"`
			LegalForm string `json:"legal_form"`
		} `json:"financial_document_context"`
	}
	if err := json.Unmarshal([]byte(form), &fields); err != nil {
		return Applicant{}, err
	}
	// Cabinet applications have a collected registry card; use its legal name when the parser context was not set explicitly.
	if typ == "LEGAL_ENTITY" && fields.Context.LegalForm == "" && len(registry) > 0 && registry[0] != "" {
		var card struct {
			FullName string `json:"full_name"`
		}
		if err := json.Unmarshal([]byte(registry[0]), &card); err != nil {
			return Applicant{}, err
		}
		name := strings.ToUpper(strings.TrimSpace(card.FullName))
		if strings.HasPrefix(name, "ООО ") || strings.HasPrefix(name, "ООО«") || strings.HasPrefix(name, "ООО\"") || strings.HasPrefix(name, "ОБЩЕСТВО С ОГРАНИЧЕННОЙ ОТВЕТСТВЕННОСТЬЮ ") {
			fields.Context.LegalForm = "OOO"
		}
	}
	return Applicant{Type: typ, INN: inn, TaxRegime: fields.Context.TaxRegime, LegalForm: fields.Context.LegalForm}, nil
}
func applicant(ctx context.Context, tx pgx.Tx, app string, editable bool) (Applicant, error) {
	type row struct {
		Type, INN, Form, Status string
		Registry                *string
	}
	m, err := queryOne[row](ctx, tx, jet.SELECT(companies.Type, companies.Inn, applications.Form, applications.Status, companies.Registry).
		FROM(applications.INNER_JOIN(companies, companies.ID.EQ(applications.CompanyID))).WHERE(applications.ID.EQ(jet.String(app))).FOR(jet.SHARE().OF(applications, companies)))
	if err != nil {
		return Applicant{}, notFound(err)
	}
	a, err := applicantContext(m.Type, m.INN, m.Form, value(m.Registry))
	if err != nil {
		return a, err
	}
	if editable && m.Status != "DRAFT" && m.Status != "WAITING_FOR_INFO" {
		return a, ErrConflict
	}
	return a, nil
}
func isFinancial(typ string) bool { return typ == "FINANCIAL_STATEMENTS" || typ == "TAX_RETURN" }
func supportsFinancial(a Applicant, typ string) bool {
	return (a.Type == "SOLE_PROPRIETOR" && a.TaxRegime == "USN_INCOME" && typ == "TAX_RETURN") ||
		(a.Type == "LEGAL_ENTITY" && a.LegalForm == "OOO" && typ == "FINANCIAL_STATEMENTS")
}
func insertDocument(ctx context.Context, tx pgx.Tx, d Document) error {
	state := d.Signatures.State
	if state == "" {
		state = "NOT_SIGNED"
	}
	m := model.Documents{ID: d.ID, ApplicationID: d.ApplicationID, Type: d.Type, FileName: d.FileName, MimeType: d.MIME, SizeBytes: d.Size, Sha256: d.SHA256, StorageKey: d.StorageKey, SignedDocumentID: nullable(d.SignedDocumentID), InfoRequestID: nullable(d.InfoRequestID), Recognition: nullable(`{"state":"NOT_APPLICABLE"}`), SignatureState: state}
	_, err := execute(ctx, tx, documents.INSERT(documents.ID, documents.ApplicationID, documents.Type, documents.FileName, documents.MimeType, documents.SizeBytes, documents.Sha256, documents.StorageKey, documents.SignedDocumentID, documents.InfoRequestID, documents.Recognition, documents.SignatureState).MODEL(m))
	return err
}
func (r Repository) enqueue(ctx context.Context, tx pgx.Tx, d Document, a Applicant) error {
	type generation struct{ Value int32 }
	g, err := queryOne[generation](ctx, tx, documents.UPDATE().SET(documents.FinancialGeneration.SET(documents.FinancialGeneration.ADD(jet.Int(1))), documents.SignatureState.SET(jet.String("PENDING")), documents.SignatureVerdict.SET(jet.StringExp(jet.NULL))).
		WHERE(documents.ID.EQ(jet.String(d.ID)).AND(documents.DeletedAt.IS_NULL())).RETURNING(documents.FinancialGeneration))
	if err != nil {
		return err
	}
	type idRow struct{ ID string }
	sigs, err := queryMany[idRow](ctx, tx, documents.SELECT(documents.ID).WHERE(documents.SignedDocumentID.EQ(jet.String(d.ID)).AND(documents.ApplicationID.EQ(jet.String(d.ApplicationID))).AND(documents.DeletedAt.IS_NULL())).ORDER_BY(documents.UploadedAt, documents.ID))
	if err != nil {
		return err
	}
	ids := []string{}
	for _, s := range sigs {
		ids = append(ids, s.ID)
	}
	if _, err := execute(ctx, tx, documents.UPDATE().SET(documents.SignatureState.SET(jet.String("PENDING")), documents.SignatureVerdict.SET(jet.StringExp(jet.NULL))).WHERE(documents.SignedDocumentID.EQ(jet.String(d.ID)).AND(documents.DeletedAt.IS_NULL()))); err != nil {
		return err
	}
	ab, _ := json.Marshal(a)
	sb, _ := json.Marshal(ids)
	id := newID("fjob_")
	_, err = execute(ctx, tx, jobs.INSERT(jobs.ID, jobs.DocumentID, jobs.Generation, jobs.Applicant, jobs.SignatureIds).MODEL(model.FinancialDocumentJobs{ID: id, DocumentID: d.ID, Generation: g.Value, Applicant: string(ab), SignatureIds: string(sb)}))
	if err != nil {
		return err
	}
	ev, _ := json.Marshal(Event{Event: EventRequested, TaskID: id})
	_, err = execute(ctx, tx, outboxTable.INSERT(outboxTable.Topic, outboxTable.Key, outboxTable.Payload).MODEL(model.Outbox{Topic: r.Topic, Key: d.ID, Payload: string(ev)}))
	return err
}
func lockedDocument(ctx context.Context, tx pgx.Tx, app, id string) (Document, error) {
	m, err := queryOne[model.Documents](ctx, tx, documents.SELECT(documents.AllColumns).WHERE(documents.ID.EQ(jet.String(id)).AND(documents.ApplicationID.EQ(jet.String(app))).AND(documents.DeletedAt.IS_NULL())).FOR(jet.UPDATE()))
	return document(m), notFound(err)
}
func (r Repository) Upload(ctx context.Context, d Document, sigs []Document) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		a, err := applicant(ctx, tx, d.ApplicationID, true)
		if err != nil {
			return err
		}
		target := d
		if d.Type == "SIGNATURE" {
			target, err = lockedDocument(ctx, tx, d.ApplicationID, d.SignedDocumentID)
			if err != nil {
				return err
			}
			if target.Type == "SIGNATURE" || target.InfoRequestID != d.InfoRequestID {
				return ErrUnsupported
			}
		}
		if isFinancial(target.Type) && !supportsFinancial(a, target.Type) {
			return ErrUnsupported
		}
		if d.InfoRequestID != "" {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM info_requests WHERE id=$1 AND application_id=$2 AND status='OPEN'`, d.InfoRequestID, d.ApplicationID).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				return ErrConflict
			}
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
		if isFinancial(target.Type) {
			return r.enqueue(ctx, tx, target, a)
		}
		return r.enqueueSignature(ctx, tx, target, a, newID("upload_"))
	})
}
func (r Repository) Retry(ctx context.Context, app, id string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		a, err := applicant(ctx, tx, app, false)
		if err != nil {
			return err
		}
		d, err := lockedDocument(ctx, tx, app, id)
		if err != nil {
			return err
		}
		if d.Type == "SIGNATURE" {
			d, err = lockedDocument(ctx, tx, app, d.SignedDocumentID)
			if err != nil {
				return err
			}
		}
		if isFinancial(d.Type) {
			if !supportsFinancial(a, d.Type) {
				return ErrUnsupported
			}
			return r.enqueue(ctx, tx, d, a)
		}
		return r.enqueueSignature(ctx, tx, d, a, newID("verify_"))
	})
}

// Generic attachments (including powers of attorney) use the signature worker, without financial parsing.
func (r Repository) enqueueSignature(ctx context.Context, tx pgx.Tx, d Document, a Applicant, requestID string) error {
	rows, err := queryMany[model.Documents](ctx, tx, documents.SELECT(documents.AllColumns).WHERE(
		documents.ApplicationID.EQ(jet.String(d.ApplicationID)).AND(documents.DeletedAt.IS_NULL()).AND(
			documents.ID.EQ(jet.String(d.ID)).OR(documents.SignedDocumentID.EQ(jet.String(d.ID))))))
	if err != nil {
		return err
	}
	docs := make([]domain.Document, 0, len(rows))
	hasSignature := len(rows) > 1 || (d.Signatures.State != "" && d.Signatures.State != "NOT_SIGNED") || strings.EqualFold(d.MIME, "application/pkcs7-signature")
	for _, row := range rows {
		docs = append(docs, domain.Document{ID: row.ID, ApplicationID: row.ApplicationID, Type: domain.DocumentType(row.Type),
			FileName: row.FileName, MimeType: row.MimeType, SizeBytes: row.SizeBytes, SHA256: row.Sha256,
			StorageKey: row.StorageKey, SignedDocumentID: value(row.SignedDocumentID)})
	}
	if hasSignature {
		if _, err := execute(ctx, tx, documents.UPDATE().SET(documents.SignatureState.SET(jet.String("PENDING")), documents.SignatureVerdict.SET(jet.StringExp(jet.NULL))).WHERE(
			documents.ApplicationID.EQ(jet.String(d.ApplicationID)).AND(documents.DeletedAt.IS_NULL()).AND(documents.ID.EQ(jet.String(d.ID)).OR(documents.SignedDocumentID.EQ(jet.String(d.ID)))))); err != nil {
			return err
		}
	}
	ev := events.NewDocumentsSubmitted(d.ApplicationID, a.INN, docs, time.Now().UTC())
	ev.VerificationID = requestID
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	topic := r.DocumentsTopic
	if topic == "" {
		topic = events.DocumentsTopic
	}
	_, err = execute(ctx, tx, outboxTable.INSERT(outboxTable.Topic, outboxTable.Key, outboxTable.Payload).MODEL(model.Outbox{Topic: topic, Key: d.ApplicationID, Payload: string(raw)}))
	return err
}

func (r Repository) Delete(ctx context.Context, app, id string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		a, err := applicant(ctx, tx, app, true)
		if err != nil {
			return err
		}
		d, err := lockedDocument(ctx, tx, app, id)
		if err != nil {
			return err
		}
		if d.InfoRequestID != "" {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM info_requests WHERE id=$1 AND application_id=$2`, d.InfoRequestID, app).Scan(&status); err != nil {
				return err
			}
			if status != "OPEN" {
				return ErrConflict
			}
		} else {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM applications WHERE id=$1`, app).Scan(&status); err != nil {
				return err
			}
			if status != "DRAFT" {
				return ErrConflict
			}
		}
		_, err = execute(ctx, tx, documents.UPDATE().SET(documents.DeletedAt.SET(jet.NOW())).WHERE(
			documents.ApplicationID.EQ(jet.String(app)).AND(documents.ID.EQ(jet.String(id)).OR(documents.SignedDocumentID.EQ(jet.String(id))))))
		if err != nil || d.Type != "SIGNATURE" {
			return err
		}
		target, err := lockedDocument(ctx, tx, app, d.SignedDocumentID)
		if err != nil {
			return err
		}
		if isFinancial(target.Type) {
			return r.enqueue(ctx, tx, target, a)
		}
		if _, err = execute(ctx, tx, documents.UPDATE().SET(documents.SignatureState.SET(jet.String("NOT_SIGNED")), documents.SignatureVerdict.SET(jet.StringExp(jet.NULL))).WHERE(documents.ID.EQ(jet.String(target.ID)))); err != nil {
			return err
		}
		target.Signatures = SignatureSummary{State: "NOT_SIGNED"}
		return r.enqueueSignature(ctx, tx, target, a, newID("verify_"))
	})
}
func (r Repository) Result(ctx context.Context, app, id string) (json.RawMessage, error) {
	type row struct {
		Result                            *string
		Snapshot, Type, INN, Form, Status string
		Registry                          *string
	}
	m, err := queryOne[row](ctx, r.Pool, jet.SELECT(results.Result, jobs.Applicant, companies.Type, companies.Inn, applications.Form, jobs.Status, companies.Registry).
		FROM(documents.INNER_JOIN(jobs, jobs.DocumentID.EQ(documents.ID).AND(jobs.Generation.EQ(documents.FinancialGeneration))).
			LEFT_JOIN(results, results.JobID.EQ(jobs.ID)).INNER_JOIN(applications, applications.ID.EQ(documents.ApplicationID)).INNER_JOIN(companies, companies.ID.EQ(applications.CompanyID))).
		WHERE(documents.ApplicationID.EQ(jet.String(app)).AND(documents.ID.EQ(jet.String(id))).AND(documents.DeletedAt.IS_NULL())))
	if err != nil {
		return nil, notFound(err)
	}
	current, err := applicantContext(m.Type, m.INN, m.Form, value(m.Registry))
	if err != nil {
		return nil, err
	}
	var snapshot Applicant
	if err = json.Unmarshal([]byte(m.Snapshot), &snapshot); err != nil {
		return nil, err
	}
	if current != snapshot {
		return nil, ErrConflict
	}
	if m.Status == "FAILED" {
		return nil, ErrProcessingFailed
	}
	if m.Result == nil {
		return nil, ErrNotFound
	}
	return json.RawMessage(*m.Result), nil
}
func job(m model.FinancialDocumentJobs) (Job, error) {
	j := Job{ID: m.ID, DocumentID: m.DocumentID, Generation: int(m.Generation), Status: m.Status, Attempts: int(m.Attempts)}
	if err := json.Unmarshal([]byte(m.Applicant), &j.Applicant); err != nil {
		return j, err
	}
	err := json.Unmarshal([]byte(m.SignatureIds), &j.SignatureIDs)
	return j, err
}

// Claim is a short transaction. Processing never holds a connection/row lock.
// The attempt number fences stale workers, while the lease prevents concurrent duplicates.
func (r Repository) Claim(ctx context.Context, id string, maxAttempts int, lease time.Duration) (j Job, ok bool, err error) {
	err = pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		m, e := queryOne[model.FinancialDocumentJobs](ctx, tx, jobs.SELECT(jobs.AllColumns).WHERE(jobs.ID.EQ(jet.String(id))).FOR(jet.UPDATE()))
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if m.Status == "COMPLETED" || m.Status == "FAILED" {
			return nil
		}
		now := time.Now().UTC()
		if m.Status == "RUNNING" && m.LeaseUntil != nil && m.LeaseUntil.After(now) {
			return ErrJobBusy
		}
		if int(m.Attempts) >= maxAttempts {
			m.Status = "FAILED"
			m.Error = nullable("ATTEMPTS_EXHAUSTED")
			m.FinishedAt = &now
			m.LeaseUntil = nil
		} else {
			m.Status = "RUNNING"
			m.Attempts++
			m.Error = nil
			m.StartedAt = &now
			until := now.Add(lease)
			m.LeaseUntil = &until
			m.FinishedAt = nil
		}
		_, e = execute(ctx, tx, jobs.UPDATE(jobs.Status, jobs.Attempts, jobs.Error, jobs.StartedAt, jobs.FinishedAt, jobs.LeaseUntil).MODEL(m).WHERE(jobs.ID.EQ(jet.String(id))))
		if e != nil {
			return e
		}
		if m.Status == "FAILED" {
			e = setProcessedState(ctx, tx, m.DocumentID, int(m.Generation), "FAILED", "")
			if e != nil {
				return e
			}
		}
		j, e = job(m)
		ok = e == nil
		return e
	})
	return
}
func (r Repository) Load(ctx context.Context, j Job) (Document, []Document, error) {
	m, err := queryOne[model.Documents](ctx, r.Pool, documents.SELECT(documents.AllColumns).WHERE(documents.ID.EQ(jet.String(j.DocumentID))))
	if err != nil {
		return Document{}, nil, err
	}
	sigs := []Document{}
	for _, id := range j.SignatureIDs {
		s, e := queryOne[model.Documents](ctx, r.Pool, documents.SELECT(documents.AllColumns).WHERE(documents.ID.EQ(jet.String(id)).AND(documents.SignedDocumentID.EQ(jet.String(m.ID))).AND(documents.ApplicationID.EQ(jet.String(m.ApplicationID)))))
		if e != nil {
			return Document{}, nil, e
		}
		sigs = append(sigs, document(s))
	}
	return document(m), sigs, nil
}
func attemptCondition(j Job) jet.BoolExpression {
	return jobs.ID.EQ(jet.String(j.ID)).AND(jobs.Status.EQ(jet.String("RUNNING"))).AND(jobs.Attempts.EQ(jet.Int(int64(j.Attempts))))
}
func (r Repository) Complete(ctx context.Context, j Job, result Result) error {
	if result.DocumentID != j.DocumentID || result.Generation != j.Generation {
		return errors.New("result does not belong to claimed task")
	}
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		tag, err := execute(ctx, tx, jobs.UPDATE().SET(jobs.Status.SET(jet.String("COMPLETED")), jobs.FinishedAt.SET(jet.NOW()), jobs.LeaseUntil.SET(jet.TimestampzExp(jet.NULL))).WHERE(attemptCondition(j)))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrJobNotRunning
		}
		report, err := json.Marshal(result.Signature)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = execute(ctx, tx, reports.INSERT(reports.ID, reports.DocumentID, reports.Verdict, reports.Policy, reports.PolicySatisfied, reports.TrustMode, reports.Report, reports.VerifiedAt).
			MODEL(model.SignatureReports{ID: result.SignatureReportID, DocumentID: &j.DocumentID, Verdict: result.Signature.Verdict, Policy: result.Signature.Policy, PolicySatisfied: result.Signature.PolicyResult.Satisfied, TrustMode: result.Signature.TrustMode, Report: string(report), VerifiedAt: result.Signature.VerifiedAt}))
		if err != nil {
			return err
		}
		_, err = execute(ctx, tx, results.INSERT(results.ID, results.JobID, results.DocumentID, results.Generation, results.SignatureReportID, results.OriginalSha256, results.ContentStorageKey, results.Result).
			MODEL(model.FinancialDocumentResults{ID: result.ID, JobID: j.ID, DocumentID: j.DocumentID, Generation: int32(j.Generation), SignatureReportID: result.SignatureReportID, OriginalSha256: result.DocumentSHA256, ContentStorageKey: nullable(result.ContentStorageKey), Result: string(raw)}))
		if err != nil {
			return err
		}
		return setProcessedState(ctx, tx, j.DocumentID, j.Generation, "DONE", result.Signature.Verdict)
	})
}
func (r Repository) Fail(ctx context.Context, j Job, reason string) error {
	return pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		tag, err := execute(ctx, tx, jobs.UPDATE().SET(jobs.Status.SET(jet.String("FAILED")), jobs.Error.SET(jet.String(reason)), jobs.FinishedAt.SET(jet.NOW()), jobs.LeaseUntil.SET(jet.TimestampzExp(jet.NULL))).WHERE(attemptCondition(j)))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrJobNotRunning
		}
		return setProcessedState(ctx, tx, j.DocumentID, j.Generation, "FAILED", "")
	})
}

func setProcessedState(ctx context.Context, tx pgx.Tx, documentID string, generation int, state, verdict string) error {
	parent := table.Documents.FromSchema("").AS("current_document")
	current := jet.EXISTS(parent.SELECT(parent.ID).WHERE(parent.ID.EQ(jet.String(documentID)).AND(parent.FinancialGeneration.EQ(jet.Int(int64(generation)))).AND(parent.DeletedAt.IS_NULL())))
	v := jet.StringExp(jet.NULL)
	if verdict != "" {
		v = jet.String(verdict)
	}
	_, err := execute(ctx, tx, documents.UPDATE().SET(documents.SignatureState.SET(jet.String(state)), documents.SignatureVerdict.SET(v)).WHERE(
		documents.DeletedAt.IS_NULL().AND(documents.ID.EQ(jet.String(documentID)).OR(documents.SignedDocumentID.EQ(jet.String(documentID)))).AND(current)))
	return err
}
