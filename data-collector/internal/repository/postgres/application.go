package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/events"
)

const (
	// Клиент с тем же ИНН физлица — тот же пользователь (users_client_inn_uidx); без ИНН — новый.
	upsertUser = `
INSERT INTO users (id, full_name, role, inn) VALUES ($1, $2, 'CLIENT', $3)
ON CONFLICT (inn) WHERE role = 'CLIENT' AND inn IS NOT NULL DO UPDATE SET full_name = EXCLUDED.full_name
RETURNING id`

	// Компания с тем же ИНН переиспользуется вместе с собранными данными; xmax = 0 — строку вставили.
	upsertCompany = `
INSERT INTO companies (id, inn, type, name) VALUES ($1, $2, $3, $4)
ON CONFLICT (inn) DO UPDATE SET inn = EXCLUDED.inn
RETURNING id, xmax = 0`

	upsertMember = `
INSERT INTO company_members (user_id, company_id, role, can_sign) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, company_id) DO UPDATE SET role = EXCLUDED.role, can_sign = EXCLUDED.can_sign`

	// Вошедший по токену: роль в компании, если она уже есть, не переписывается — иначе одна регистрация
	// задним числом поменяла бы роль и can_sign во всех его заявках по компании.
	insertMemberKeepRole = `
INSERT INTO company_members (user_id, company_id, role, can_sign) VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, company_id) DO NOTHING`

	selectDraft = `
SELECT id FROM applications WHERE company_id = $1 AND created_by = $2 AND status = 'DRAFT'
ORDER BY created_at DESC LIMIT 1`

	insertApplication = `
INSERT INTO applications (id, number, company_id, created_by, status, created_at, updated_at)
VALUES ($1, 'A-' || to_char($4::timestamptz, 'YYYY') || '-' || lpad(nextval('application_number_seq')::text, 6, '0'),
        $2, $3, 'DRAFT', $4, $4)`

	applicationColumns = `
SELECT a.id, a.number, a.status, coalesce(a.product_code, ''), a.amount, a.term_months, a.form -> 'purpose',
       a.created_at, a.submitted_at, a.updated_at,
       c.id, c.inn, c.type, c.name, coalesce(c.ogrn, ''),
       u.id, u.full_name, coalesce(u.inn, ''), coalesce(m.role, ''), coalesce(m.can_sign, false), u.persona IS NOT NULL,
       coalesce((SELECT t.id FROM collection_tasks t WHERE t.application_id = a.id ORDER BY t.created_at DESC LIMIT 1), '')
FROM applications a
JOIN companies c ON c.id = a.company_id
JOIN users u ON u.id = a.created_by
LEFT JOIN company_members m ON m.user_id = a.created_by AND m.company_id = a.company_id`

	selectApplication = applicationColumns + `
WHERE a.id = $1`
	selectApplications = applicationColumns + `
WHERE a.id = ANY($1)`

	documentColumns = `
SELECT application_id, id, type, file_name, mime_type, size_bytes, sha256, storage_key, coalesce(uploaded_by, ''),
       coalesce(signed_document_id, ''), signature_state, coalesce(signature_verdict, ''),
       coalesce(recognition ->> 'state', ''), uploaded_at
FROM documents`

	selectDocuments = documentColumns + `
WHERE application_id = $1 AND deleted_at IS NULL
ORDER BY id`
	selectDocumentsOf = documentColumns + `
WHERE application_id = ANY($1) AND deleted_at IS NULL
ORDER BY application_id, id`

	lockApplication = `SELECT status FROM applications WHERE id = $1 FOR UPDATE`

	insertDocument = `
INSERT INTO documents (id, application_id, type, file_name, mime_type, size_bytes, sha256, storage_key, uploaded_by,
                       signed_document_id, recognition, signature_state, uploaded_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

	submitApplication = `
UPDATE applications SET status = 'SUBMITTED', version = version + 1, form = $2, product_code = $3, amount = $4,
       term_months = $5, submitted_at = $6, updated_at = $6
WHERE id = $1`

	moveApplicationSQL = `
UPDATE applications SET status = $3, version = version + 1, updated_at = now() WHERE id = $1 AND status = $2`

	insertEventSQL = `
INSERT INTO application_events (id, application_id, type, from_status, to_status, actor_id, message, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
)

// ApplicationRepository — заявки и всё, что пишется вместе с ними: пользователи, компании,
// их связь, документы, задачи на сбор и события в outbox.
type ApplicationRepository struct {
	pool           *pgxpool.Pool
	tasksTopic     string // куда publisher отправит событие о задаче на сбор
	documentsTopic string // куда — событие о документах для worker'ов подписи и OCR
}

func NewApplicationRepository(pool *pgxpool.Pool, tasksTopic, documentsTopic string) *ApplicationRepository {
	return &ApplicationRepository{pool: pool, tasksTopic: tasksTopic, documentsTopic: documentsTopic}
}

// Register одной транзакцией пишет или находит пользователя и компанию, их связь и черновик заявки.
func (r *ApplicationRepository) Register(ctx context.Context, reg domain.Registration) (domain.Application, error) {
	var (
		appID        string
		alreadyKnown bool
	)
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var userID, companyID string
		if reg.ExistingUser {
			// вошёл по токену: пользователь уже есть, по ИНН его не ищем
			userID = reg.UserID
		} else if err := tx.QueryRow(ctx, upsertUser, reg.UserID, reg.Representative.FullName,
			nullString(reg.Representative.INN)).Scan(&userID); err != nil {
			return fmt.Errorf("upsert user: %w", err)
		}
		var inserted bool
		if err := tx.QueryRow(ctx, upsertCompany, reg.CompanyID, reg.Company.INN, string(reg.Company.Type),
			reg.Company.Name).Scan(&companyID, &inserted); err != nil {
			return fmt.Errorf("upsert company: %w", err)
		}
		alreadyKnown = !inserted
		member := upsertMember
		if reg.ExistingUser {
			member = insertMemberKeepRole
		}
		if _, err := tx.Exec(ctx, member, userID, companyID, string(reg.Representative.Role),
			reg.Representative.CanSign); err != nil {
			return fmt.Errorf("upsert company member: %w", err)
		}

		err := tx.QueryRow(ctx, selectDraft, companyID, userID).Scan(&appID)
		if !errors.Is(err, pgx.ErrNoRows) {
			return err // черновик уже есть — он и будет заявкой
		}
		appID = reg.ApplicationID
		if _, err := tx.Exec(ctx, insertApplication, appID, companyID, userID, reg.CreatedAt); err != nil {
			return fmt.Errorf("insert application: %w", err)
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "application.created",
			To: domain.ApplicationDraft, ActorID: userID, Message: "Заявка создана"})
	})
	if err != nil {
		return domain.Application{}, err
	}
	app, ok, err := r.Get(ctx, appID)
	if err != nil {
		return domain.Application{}, err
	}
	if !ok {
		return domain.Application{}, fmt.Errorf("application %s is not found after registration", appID)
	}
	app.Company.AlreadyKnown = alreadyKnown
	return app, nil
}

// Get возвращает заявку с компанией, представителем и документами.
func (r *ApplicationRepository) Get(ctx context.Context, id string) (domain.Application, bool, error) {
	app, err := scanApplication(r.pool.QueryRow(ctx, selectApplication, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Application{}, false, nil
	}
	if err != nil {
		return domain.Application{}, false, err
	}
	if app.Documents, err = queryDocuments(ctx, r.pool, id); err != nil {
		return domain.Application{}, false, err
	}
	return app, true, nil
}

// List — заявки по id в том же порядке, двумя запросами (заявки и их документы); пропавшие пропускаются.
func (r *ApplicationRepository) List(ctx context.Context, ids []string) ([]domain.Application, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, selectApplications, ids)
	if err != nil {
		return nil, fmt.Errorf("select applications: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Application, error) { return scanApplication(row) })
	if err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, selectDocumentsOf, ids)
	if err != nil {
		return nil, fmt.Errorf("select documents: %w", err)
	}
	docs, err := pgx.CollectRows(rows, scanDocument)
	if err != nil {
		return nil, fmt.Errorf("read documents: %w", err)
	}
	byApp := make(map[string][]domain.Document, len(found))
	for _, d := range docs {
		byApp[d.ApplicationID] = append(byApp[d.ApplicationID], d)
	}
	byID := make(map[string]domain.Application, len(found))
	for _, a := range found {
		a.Documents = byApp[a.ID]
		byID[a.ID] = a
	}
	out := make([]domain.Application, 0, len(found))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

func scanApplication(row pgx.Row) (domain.Application, error) {
	var (
		app                       domain.Application
		status, companyType, role string
		amount                    *int64
		termMonths                *int32
		purpose                   []byte
		submittedAt               *time.Time
	)
	err := row.Scan(
		&app.ID, &app.Number, &status, &app.ProductCode, &amount, &termMonths, &purpose,
		&app.CreatedAt, &submittedAt, &app.UpdatedAt,
		&app.Company.ID, &app.Company.INN, &companyType, &app.Company.Name, &app.Company.OGRN,
		&app.Representative.UserID, &app.Representative.FullName, &app.Representative.INN, &role,
		&app.Representative.CanSign, &app.Representative.HasLogin, &app.CollectionTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Application{}, err
	}
	if err != nil {
		return domain.Application{}, fmt.Errorf("select application: %w", err)
	}
	app.Status = domain.ApplicationStatus(status)
	app.Company.Type = domain.CompanyType(companyType)
	app.Representative.Role = domain.RepresentativeRole(role)
	if amount != nil && termMonths != nil {
		app.Requested = &domain.RequestedTerms{Amount: *amount, TermMonths: int(*termMonths)}
	}
	if len(purpose) > 0 {
		var p purposeRecord
		if err := json.Unmarshal(purpose, &p); err != nil {
			return domain.Application{}, fmt.Errorf("decode purpose: %w", err)
		}
		app.Purpose = &domain.Purpose{Code: p.Code, Description: p.Description}
	}
	app.CreatedAt, app.UpdatedAt = app.CreatedAt.UTC(), app.UpdatedAt.UTC()
	if submittedAt != nil {
		app.SubmittedAt = submittedAt.UTC()
	}
	return app, nil
}

// queryDocuments — документы заявки без удалённых, в порядке id.
func queryDocuments(ctx context.Context, pool *pgxpool.Pool, applicationID string) ([]domain.Document, error) {
	rows, err := pool.Query(ctx, selectDocuments, applicationID)
	if err != nil {
		return nil, fmt.Errorf("select documents: %w", err)
	}
	docs, err := pgx.CollectRows(rows, scanDocument)
	if err != nil {
		return nil, fmt.Errorf("read documents: %w", err)
	}
	return docs, nil
}

func scanDocument(row pgx.CollectableRow) (domain.Document, error) {
	var (
		d                                         domain.Document
		docType, signatureState, recognitionState string
	)
	err := row.Scan(&d.ApplicationID, &d.ID, &docType, &d.FileName, &d.MimeType, &d.SizeBytes, &d.SHA256, &d.StorageKey,
		&d.UploadedBy, &d.SignedDocumentID, &signatureState, &d.SignatureVerdict, &recognitionState, &d.UploadedAt)
	d.Type, d.SignatureState = domain.DocumentType(docType), domain.SignatureState(signatureState)
	d.RecognitionState, d.UploadedAt = domain.RecognitionState(recognitionState), d.UploadedAt.UTC()
	return d, err
}

// Submit одной транзакцией отправляет заявку: документы, анкета и сумма со сроком в applications
// (SUBMITTED), событие в журнале, задача на сбор, события в outbox — о задаче и о документах.
// domain.ErrApplicationNotDraft — заявка уже не черновик.
func (r *ApplicationRepository) Submit(ctx context.Context, sub domain.Submission) error {
	form, err := json.Marshal(toFormRecord(sub))
	if err != nil {
		return fmt.Errorf("encode form: %w", err)
	}
	var amount, termMonths any
	if req := sub.Requested; req != nil {
		amount, termMonths = req.Amount, req.TermMonths
	}
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, lockApplication, sub.ApplicationID).Scan(&status); err != nil {
			return fmt.Errorf("lock application: %w", err)
		}
		if status != string(domain.ApplicationDraft) {
			return domain.ErrApplicationNotDraft
		}
		// сначала документы, потом их открепленные подписи: подпись ссылается на документ
		for _, signatures := range []bool{false, true} {
			for _, d := range sub.Documents {
				if (d.SignedDocumentID != "") != signatures {
					continue
				}
				recognition, err := json.Marshal(map[string]string{"state": string(d.RecognitionState)})
				if err != nil {
					return fmt.Errorf("encode recognition: %w", err)
				}
				if _, err := tx.Exec(ctx, insertDocument, d.ID, d.ApplicationID, string(d.Type), d.FileName, d.MimeType,
					d.SizeBytes, d.SHA256, d.StorageKey, nullString(d.UploadedBy), nullString(d.SignedDocumentID),
					string(recognition), string(d.SignatureState), d.UploadedAt); err != nil {
					return fmt.Errorf("insert document %s: %w", d.FileName, err)
				}
			}
		}
		if _, err := tx.Exec(ctx, submitApplication, sub.ApplicationID, string(form), nullString(sub.ProductCode),
			amount, termMonths, sub.SubmittedAt); err != nil {
			return fmt.Errorf("submit application: %w", err)
		}
		if err := insertEvent(ctx, tx, appEvent{ApplicationID: sub.ApplicationID, Type: "application.submitted",
			From: domain.ApplicationDraft, To: domain.ApplicationSubmitted, ActorID: sub.ActorID,
			Message: "Заявка отправлена на рассмотрение",
			Payload: map[string]any{"collection_task_id": sub.Task.ID, "documents": len(sub.Documents)}}); err != nil {
			return err
		}
		if err := insertTask(ctx, tx, sub.Task, r.tasksTopic); err != nil {
			return err
		}
		if len(sub.Documents) == 0 {
			return nil
		}
		return insertOutbox(ctx, tx, r.documentsTopic, sub.ApplicationID,
			events.NewDocumentsSubmitted(sub.ApplicationID, sub.Task.Input.INN, sub.Documents, sub.SubmittedAt))
	})
}

// appEvent — запись журнала заявки (application_events).
type appEvent struct {
	ApplicationID string
	Type          string
	From, To      domain.ApplicationStatus // пусто — NULL
	ActorID       string                   // пусто — событие системы
	Message       string                   // пусто — не для клиента
	Payload       any
}

func insertEvent(ctx context.Context, tx pgx.Tx, ev appEvent) error {
	payload := []byte("{}")
	if ev.Payload != nil {
		var err error
		if payload, err = json.Marshal(ev.Payload); err != nil {
			return fmt.Errorf("encode event payload: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, insertEventSQL, newEventID(), ev.ApplicationID, ev.Type, nullString(string(ev.From)),
		nullString(string(ev.To)), nullString(ev.ActorID), nullString(ev.Message), string(payload)); err != nil {
		return fmt.Errorf("insert application event %s: %w", ev.Type, err)
	}
	return nil
}

// moveApplication переводит заявку из from в to (версия +1) и пишет событие.
// Заявка не в from — ничего не делает: переход уже был или заявка ушла дальше.
func moveApplication(ctx context.Context, tx pgx.Tx, appID string, from, to domain.ApplicationStatus,
	eventType string, payload any) error {
	tag, err := tx.Exec(ctx, moveApplicationSQL, appID, string(from), string(to))
	if err != nil {
		return fmt.Errorf("move application %s → %s: %w", from, to, err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: eventType, From: from, To: to, Payload: payload})
}

var (
	eventIDMu      sync.Mutex
	eventIDEntropy = ulid.Monotonic(rand.Reader, 0)
)

// newEventID — evt_<ULID>, возрастающий даже в пределах миллисекунды: журнал читается по id.
func newEventID() string {
	eventIDMu.Lock()
	defer eventIDMu.Unlock()
	return "evt_" + ulid.MustNew(ulid.Now(), eventIDEntropy).String()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// formRecord — анкета в applications.form: поля ApplicationInput и анкета заёмщика
// с данными для сбора (без токенов, как collection_tasks.request).
type formRecord struct {
	ProductCode   string              `json:"product_code,omitempty"`
	Requested     *requestedRecord    `json:"requested,omitempty"`
	Purpose       *purposeRecord      `json:"purpose,omitempty"`
	Questionnaire questionnaireRecord `json:"questionnaire"`
	Scoring       *scoringRecord      `json:"scoring,omitempty"`
}

// scoringRecord — ответы для credit-engine (domain.ScoringAnswers) в applications.form.
type scoringRecord struct {
	MaxAnnualRate           *float64 `json:"max_annual_rate,omitempty"`
	MinAcceptableAmount     *int64   `json:"min_acceptable_amount,omitempty"`
	MaxAcceptableTermMonths *int     `json:"max_acceptable_term_months,omitempty"`
	NeedCost                *int64   `json:"need_cost,omitempty"`
	OwnContribution         *int64   `json:"own_contribution,omitempty"`
	FreeCash                *int64   `json:"free_cash,omitempty"`
	MinimumCash             *int64   `json:"minimum_cash,omitempty"`
	NeedEvidence            string   `json:"need_evidence,omitempty"`
	ExperienceYears         *int     `json:"experience_years,omitempty"`
	PrimaryIncomeSource     string   `json:"primary_income_source,omitempty"`
	Qualification           string   `json:"qualification,omitempty"`
	TotalExperienceMonths   *int     `json:"total_experience_months,omitempty"`
	NetMonthlyIncome        *int64   `json:"net_monthly_income,omitempty"`
	CarClass                string   `json:"car_class,omitempty"`
	EducationLevel          string   `json:"education_level,omitempty"`
	RealEstateStatus        string   `json:"real_estate_status,omitempty"`
	BusinessLocation        string   `json:"business_location,omitempty"`
	LocationStabilityMonths *int     `json:"location_stability_months,omitempty"`
	BuyerContracts          string   `json:"buyer_contracts,omitempty"`
}

func toScoringRecord(sc *domain.ScoringAnswers) *scoringRecord {
	if sc == nil {
		return nil
	}
	r := scoringRecord(*sc)
	return &r
}

func (r *scoringRecord) toDomain() *domain.ScoringAnswers {
	if r == nil {
		return nil
	}
	sc := domain.ScoringAnswers(*r)
	return &sc
}

type requestedRecord struct {
	Amount     int64 `json:"amount"`
	TermMonths int   `json:"term_months"`
}

type purposeRecord struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
}

type questionnaireRecord struct {
	ID            string        `json:"id"`
	Age           int           `json:"age"`
	MaritalStatus string        `json:"marital_status"`
	ChildrenCount int           `json:"children_count"`
	CreatedAt     time.Time     `json:"created_at"`
	Collect       requestRecord `json:"collect"`
}

func toFormRecord(sub domain.Submission) formRecord {
	q := sub.Questionnaire
	rec := formRecord{
		ProductCode: sub.ProductCode,
		Questionnaire: questionnaireRecord{ID: q.ID, Age: q.Age, MaritalStatus: string(q.MaritalStatus),
			ChildrenCount: q.ChildrenCount, CreatedAt: q.CreatedAt, Collect: toRequestRecord(sub.Task.Input)},
	}
	if r := sub.Requested; r != nil {
		rec.Requested = &requestedRecord{Amount: r.Amount, TermMonths: r.TermMonths}
	}
	if p := sub.Purpose; p != nil {
		rec.Purpose = &purposeRecord{Code: p.Code, Description: p.Description}
	}
	rec.Scoring = toScoringRecord(sub.Scoring)
	return rec
}

// registryRecord — реквизиты из ЕГРЮЛ в companies.registry (поля CounterpartyPrefill в api/openapi.yaml).
type registryRecord struct {
	INN              string `json:"inn"`
	ApplicantType    string `json:"applicant_type"`
	OGRN             string `json:"ogrn,omitempty"`
	KPP              string `json:"kpp,omitempty"`
	FullName         string `json:"full_name"`
	ShortName        string `json:"short_name,omitempty"`
	RegistrationDate string `json:"registration_date,omitempty"`
	DirectorName     string `json:"director_name,omitempty"`
	Region           string `json:"region,omitempty"`
	TerminatedOn     string `json:"terminated_on,omitempty"`
}

func toRegistryRecord(c domain.CompanyCard) registryRecord {
	return registryRecord{INN: c.INN, ApplicantType: string(c.Type), OGRN: c.OGRN, KPP: c.KPP, FullName: c.FullName,
		ShortName: c.ShortName, RegistrationDate: c.RegistrationDate, DirectorName: c.DirectorName, Region: c.Region,
		TerminatedOn: c.TerminatedOn}
}

const (
	lockInfoRequestSQL = `
SELECT status, coalesce(question ->> 'source', '') FROM info_requests WHERE id = $1 AND application_id = $2 FOR UPDATE`

	insertAnswerDocumentSQL = `
INSERT INTO documents (id, application_id, type, file_name, mime_type, size_bytes, sha256, storage_key, uploaded_by,
                       signed_document_id, recognition, signature_state, uploaded_at, info_request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	answerInfoRequestSQL = `UPDATE info_requests SET status = 'ANSWERED', answer = $2, answered_at = $3 WHERE id = $1`

	patchScoringSQL = `
UPDATE applications SET form = jsonb_set(form, '{scoring}', coalesce(form -> 'scoring', '{}'::jsonb) || $2::jsonb, true),
       updated_at = now()
WHERE id = $1`

	openRequiredSQL = `SELECT count(*) FROM info_requests WHERE application_id = $1 AND status = 'OPEN' AND required`

	cancelOptionalSQL = `UPDATE info_requests SET status = 'CANCELLED' WHERE application_id = $1 AND status = 'OPEN'`

	infoProvidedSQL = `
UPDATE applications SET status = $2, deadline_at = $3, version = version + 1, updated_at = now() WHERE id = $1`
)

// AnswerInfoRequest одной транзакцией принимает ответ клиента: документы, ответ, уточнение анкеты.
// Ответили на все обязательные вопросы — заявка возвращается: к андеррайтеру (MANUAL_REVIEW со сроком),
// если вопросы задал он, иначе на повторную оценку (VERIFYING). Возвращает статус после ответа;
// false — заявки или вопроса нет; domain.ErrNotWaitingForInfo, domain.ErrQuestionClosed.
func (r *ApplicationRepository) AnswerInfoRequest(ctx context.Context, a domain.InfoAnswer) (domain.ApplicationStatus, bool, error) {
	var (
		next  domain.ApplicationStatus
		found bool
	)
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, lockApplication, a.ApplicationID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock application: %w", err)
		}
		var qStatus, source string
		err = tx.QueryRow(ctx, lockInfoRequestSQL, a.RequestID, a.ApplicationID).Scan(&qStatus, &source)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock info request: %w", err)
		}
		found = true
		switch {
		case status != string(domain.ApplicationWaitingForInfo):
			return domain.ErrNotWaitingForInfo
		case qStatus != "OPEN":
			return domain.ErrQuestionClosed
		}

		ids := make([]string, 0, len(a.Documents))
		for _, signatures := range []bool{false, true} {
			for _, d := range a.Documents {
				if (d.SignedDocumentID != "") != signatures {
					continue
				}
				recognition, _ := json.Marshal(map[string]string{"state": string(d.RecognitionState)})
				if _, err := tx.Exec(ctx, insertAnswerDocumentSQL, d.ID, d.ApplicationID, string(d.Type), d.FileName, d.MimeType,
					d.SizeBytes, d.SHA256, d.StorageKey, nullString(d.UploadedBy), nullString(d.SignedDocumentID),
					string(recognition), string(d.SignatureState), d.UploadedAt, a.RequestID); err != nil {
					return fmt.Errorf("insert document %s: %w", d.FileName, err)
				}
				ids = append(ids, d.ID)
			}
		}
		answer := map[string]any{"document_ids": ids}
		if len(a.Value) > 0 {
			answer["value"] = a.Value
		}
		if a.Comment != "" {
			answer["comment"] = a.Comment
		}
		b, _ := json.Marshal(answer)
		if _, err := tx.Exec(ctx, answerInfoRequestSQL, a.RequestID, string(b), a.Now); err != nil {
			return fmt.Errorf("answer info request: %w", err)
		}
		if len(a.ScoringPatch) > 0 {
			patch, _ := json.Marshal(a.ScoringPatch)
			if _, err := tx.Exec(ctx, patchScoringSQL, a.ApplicationID, string(patch)); err != nil {
				return fmt.Errorf("patch scoring answers: %w", err)
			}
		}

		var open int
		if err := tx.QueryRow(ctx, openRequiredSQL, a.ApplicationID).Scan(&open); err != nil {
			return fmt.Errorf("count open questions: %w", err)
		}
		next = domain.ApplicationWaitingForInfo
		if open == 0 {
			var deadline any
			next = domain.ApplicationVerifying
			if source == "underwriter" {
				next, deadline = domain.ApplicationManualReview, a.Now.Add(a.ReviewSLA)
			}
			if _, err := tx.Exec(ctx, cancelOptionalSQL, a.ApplicationID); err != nil {
				return fmt.Errorf("cancel optional questions: %w", err)
			}
			if _, err := tx.Exec(ctx, infoProvidedSQL, a.ApplicationID, string(next), deadline); err != nil {
				return fmt.Errorf("return application: %w", err)
			}
			if err := insertEvent(ctx, tx, appEvent{ApplicationID: a.ApplicationID, Type: "info.provided", From: domain.ApplicationWaitingForInfo,
				To: next, ActorID: a.ActorID, Message: "Ответы отправлены банку"}); err != nil {
				return err
			}
		} else if err := insertEvent(ctx, tx, appEvent{ApplicationID: a.ApplicationID, Type: "info.answered", ActorID: a.ActorID,
			Payload: map[string]string{"info_request_id": a.RequestID}}); err != nil {
			return err
		}
		if len(a.Documents) == 0 {
			return nil
		}
		return insertOutbox(ctx, tx, r.documentsTopic, a.ApplicationID,
			events.NewDocumentsSubmitted(a.ApplicationID, a.CompanyINN, a.Documents, a.Now))
	})
	return next, found, err
}
