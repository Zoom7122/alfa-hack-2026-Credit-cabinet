package postgres

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

type SigningRepository struct{ pool *pgxpool.Pool }

func NewSigningRepository(pool *pgxpool.Pool) *SigningRepository {
	return &SigningRepository{pool: pool}
}

// Все изменения подписания сериализуются блокировкой строки заявки.
func lockSigningApplication(ctx context.Context, tx pgx.Tx, actorID, appID string) (domain.ApplicationStatus, error) {
	if actorID == "" {
		return "", service.ErrUnauthorized
	}
	var status domain.ApplicationStatus
	var member bool
	err := tx.QueryRow(ctx, `SELECT a.status, EXISTS (
        SELECT 1 FROM company_members m JOIN users u ON u.id = m.user_id
        WHERE m.company_id = a.company_id AND m.user_id = $2 AND u.role = 'CLIENT')
        FROM applications a WHERE a.id = $1 FOR UPDATE`, appID, actorID).Scan(&status, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", service.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !member {
		return "", service.ErrForbidden
	}
	return status, nil
}

const selectSigningSQL = `
SELECT s.id, s.application_id, s.offer_id, s.status, u.id, u.full_name,
       coalesce(s.code, ''), s.code_expires_at, s.attempts_left, s.signed_at, s.disbursement,
       s.document_sha256, d.id, d.file_name, d.mime_type, d.size_bytes, d.sha256, d.storage_key, d.uploaded_at
FROM signings s JOIN users u ON u.id = s.signer_id JOIN documents d ON d.id = s.document_id
WHERE s.application_id = $1 AND d.deleted_at IS NULL ORDER BY s.created_at DESC, s.id DESC LIMIT 1`

func readSigning(ctx context.Context, tx pgx.Tx, appID string) (domain.SigningSession, error) {
	var s domain.SigningSession
	var payout []byte
	err := tx.QueryRow(ctx, selectSigningSQL, appID).Scan(&s.ID, &s.ApplicationID, &s.OfferID, &s.State,
		&s.Signer.ID, &s.Signer.FullName, &s.Code, &s.CodeExpiresAt, &s.AttemptsLeft, &s.SignedAt, &payout,
		&s.DocumentSHA256, &s.Document.ID, &s.Document.FileName, &s.Document.MimeType, &s.Document.SizeBytes,
		&s.Document.SHA256, &s.Document.StorageKey, &s.Document.UploadedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, service.ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Document.ApplicationID, s.Document.Type = appID, domain.DocLoanAgreementDraft
	s.Document.SignatureState, s.Document.RecognitionState = domain.SignatureNotSigned, domain.RecognitionNotApplicable
	if len(payout) > 0 {
		if err := json.Unmarshal(payout, &s.Disbursement); err != nil {
			return s, err
		}
	}
	return s, nil
}

func signingFor(s domain.SigningSession, actorID string) domain.SigningSession {
	if s.Signer.ID != actorID || s.State != domain.SigningAwaiting {
		s.Code = ""
	}
	return s
}

func (r *SigningRepository) Signing(ctx context.Context, actorID, appID string) (domain.SigningSession, error) {
	var s domain.SigningSession
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockSigningApplication(ctx, tx, actorID, appID); err != nil {
			return err
		}
		var err error
		s, err = readSigning(ctx, tx, appID)
		return err
	})
	return signingFor(s, actorID), err
}

func (r *SigningRepository) Accept(ctx context.Context, actorID, appID, offerID, code string, now time.Time,
	prepare service.PrepareContract) (domain.SigningSession, error) {
	var s domain.SigningSession
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		status, err := lockSigningApplication(ctx, tx, actorID, appID)
		if err != nil {
			return err
		}
		if status == domain.ApplicationSigning || status == domain.ApplicationCompleted {
			s, err = readSigning(ctx, tx, appID)
			if err == nil && s.OfferID != offerID {
				return service.ErrInvalidTransition
			}
			return err // повтор выбора того же предложения возвращает ту же сессию и код
		}
		if status != domain.ApplicationOffered {
			return service.ErrInvalidTransition
		}
		app, err := scanApplication(tx.QueryRow(ctx, selectApplication, appID))
		if err != nil {
			return err
		}
		var offer domain.OfferRecord
		err = tx.QueryRow(ctx, `SELECT id, amount, term_months, rate, monthly_payment, terms, status, valid_until
            FROM offers WHERE application_id = $1 AND id = $2 FOR UPDATE`, appID, offerID).
			Scan(&offer.ID, &offer.Amount, &offer.TermMonths, &offer.Rate, &offer.MonthlyPayment, &offer.Terms, &offer.Status, &offer.ValidUntil)
		if errors.Is(err, pgx.ErrNoRows) {
			return service.ErrNotFound
		}
		if err != nil {
			return err
		}
		if offer.Status != "ACTIVE" || !now.Before(offer.ValidUntil) {
			return domain.ErrOfferExpired
		}
		var signer domain.UserRef
		err = tx.QueryRow(ctx, `SELECT u.id, u.full_name FROM company_members m JOIN users u ON u.id = m.user_id
            WHERE m.company_id = $1 AND m.can_sign AND u.role = 'CLIENT'
              AND (m.role = 'DIRECTOR' OR ($2 = 'SOLE_PROPRIETOR' AND m.role = 'OWNER'))
            ORDER BY (u.id = $3) DESC, u.id LIMIT 1`, app.Company.ID, string(app.Company.Type), actorID).Scan(&signer.ID, &signer.FullName)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotAuthorizedSigner
		}
		if err != nil {
			return err
		}
		doc, err := prepare(app, offer)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO documents (id, application_id, type, file_name, mime_type, size_bytes,
            sha256, storage_key, recognition, signature_state, uploaded_at)
            VALUES ($1, $2, 'LOAN_AGREEMENT_DRAFT', $3, $4, $5, $6, $7, '{"state":"NOT_APPLICABLE"}', 'NOT_SIGNED', $8)`,
			doc.ID, appID, doc.FileName, doc.MimeType, doc.SizeBytes, doc.SHA256, doc.StorageKey, now)
		if err != nil {
			return err
		}
		s = domain.SigningSession{ID: "sgn_" + ulid.Make().String(), ApplicationID: appID, OfferID: offerID,
			State: domain.SigningAwaiting, Signer: signer, Document: doc, DocumentSHA256: doc.SHA256,
			Code: code, CodeExpiresAt: now.Add(domain.SigningCodeTTL), AttemptsLeft: domain.SigningAttempts}
		_, err = tx.Exec(ctx, `INSERT INTO signings (id, application_id, offer_id, signer_id, method, code,
            attempts_left, document_id, document_sha256, code_expires_at, created_at)
            VALUES ($1, $2, $3, $4, 'SIMPLE_CODE', $5, $6, $7, $8, $9, $10)`,
			s.ID, appID, offerID, signer.ID, code, s.AttemptsLeft, doc.ID, doc.SHA256, s.CodeExpiresAt, now)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE offers SET status = CASE WHEN id = $2 THEN 'ACCEPTED' ELSE 'WITHDRAWN' END
            WHERE application_id = $1 AND status = 'ACTIVE'`, appID, offerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'SIGNING', deadline_at = NULL,
            version = version + 1, updated_at = $2 WHERE id = $1`, appID, now); err != nil {
			return err
		}
		return insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "offer.accepted", ActorID: actorID,
			From: domain.ApplicationOffered, To: domain.ApplicationSigning, Message: "Предложение выбрано, договор готов к подписанию",
			Payload: map[string]any{"offer_id": offerID, "signing_id": s.ID, "document_id": doc.ID, "profile": "MOCK"}})
	})
	return signingFor(s, actorID), err
}

func (r *SigningRepository) Confirm(ctx context.Context, actorID, appID, signingID, code string, now time.Time) (domain.SigningSession, error) {
	var s domain.SigningSession
	var codeErr error
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		status, err := lockSigningApplication(ctx, tx, actorID, appID)
		if err != nil {
			return err
		}
		s, err = readSigning(ctx, tx, appID)
		if err != nil {
			return err
		}
		var canSign bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM company_members m
            JOIN applications a ON a.company_id = m.company_id JOIN companies c ON c.id = m.company_id
            WHERE a.id = $1 AND m.user_id = $2 AND m.can_sign
              AND (m.role = 'DIRECTOR' OR (c.type = 'SOLE_PROPRIETOR' AND m.role = 'OWNER')))`, appID, actorID).Scan(&canSign); err != nil {
			return err
		}
		if s.Signer.ID != actorID || !canSign {
			return domain.ErrNotAuthorizedSigner
		}
		if s.ID != signingID {
			return service.ErrInvalidTransition
		}
		if s.State == domain.SigningSigned && status == domain.ApplicationCompleted {
			return nil // повтор подтверждения не создаёт вторую выдачу и запись аудита
		}
		if status != domain.ApplicationSigning || s.State != domain.SigningAwaiting {
			return service.ErrInvalidTransition
		}
		expired := !now.Before(s.CodeExpiresAt)
		if expired || subtle.ConstantTimeCompare([]byte(code), []byte(s.Code)) != 1 {
			s.AttemptsLeft--
			if expired {
				s.AttemptsLeft = 0
			}
			if s.AttemptsLeft <= 0 {
				s.State = domain.SigningFailed
				if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'OFFERED', version = version + 1, updated_at = $2 WHERE id = $1`, appID, now); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE offers SET status = CASE WHEN valid_until > $2 THEN 'ACTIVE' ELSE 'EXPIRED' END
                    WHERE id = $1 AND status = 'ACCEPTED'`, s.OfferID, now); err != nil {
					return err
				}
				if err := insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "sign.failed", ActorID: actorID,
					From: domain.ApplicationSigning, To: domain.ApplicationOffered, Message: "Подписание не завершено — выберите предложение заново",
					Payload: map[string]any{"signing_id": s.ID, "expired": expired, "profile": "MOCK"}}); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE signings SET attempts_left = $2, status = $3,
                code = CASE WHEN $3 = 'FAILED' THEN NULL ELSE code END WHERE id = $1`, s.ID, s.AttemptsLeft, s.State); err != nil {
				return err
			}
			codeErr = &domain.SigningCodeError{Expired: expired, AttemptsLeft: s.AttemptsLeft}
			return nil // попытка должна сохраниться, несмотря на ошибку для клиента
		}
		var amount int64
		if err := tx.QueryRow(ctx, `SELECT amount FROM offers WHERE id = $1`, s.OfferID).Scan(&amount); err != nil {
			return err
		}
		s.Disbursement = &domain.MockDisbursement{Amount: amount, AccountMasked: "демонстрационный счёт", ExecutedAt: now}
		payout, err := json.Marshal(s.Disbursement)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE signings SET status = 'SIGNED', signed_at = $2, code = NULL,
            disbursement = $3 WHERE id = $1`, s.ID, now, payout); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE applications SET status = 'COMPLETED', version = version + 1, updated_at = $2 WHERE id = $1`, appID, now); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "sign.confirmed", ActorID: actorID,
			Message: "Договор подписан демонстрационным кодом", Payload: map[string]any{"signing_id": s.ID,
				"document_id": s.Document.ID, "document_sha256": s.DocumentSHA256, "method": "SIMPLE_CODE", "profile": "MOCK"}}); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, appEvent{ApplicationID: appID, Type: "disbursement.simulated",
			From: domain.ApplicationSigning, To: domain.ApplicationCompleted,
			Message: "Демонстрационная выдача кредита завершена", Payload: s.Disbursement}); err != nil {
			return err
		}
		s.State, s.SignedAt = domain.SigningSigned, &now
		return nil
	})
	if err == nil {
		err = codeErr
	}
	return signingFor(s, actorID), err
}
