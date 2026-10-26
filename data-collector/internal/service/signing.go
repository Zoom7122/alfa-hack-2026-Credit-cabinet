package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// PrepareContract вызывается внутри транзакции только для нового подписания:
// выбранные условия уже заблокированы, повтор запроса не создаёт второй файл.
type PrepareContract func(domain.Application, domain.OfferRecord) (domain.Document, error)

type SigningRepository interface {
	Accept(context.Context, string, string, string, string, time.Time, PrepareContract) (domain.SigningSession, error)
	Signing(context.Context, string, string) (domain.SigningSession, error)
	Confirm(context.Context, string, string, string, string, time.Time) (domain.SigningSession, error)
}

type SigningService struct {
	repo  SigningRepository
	files FileStorage
	open  func(string) (io.ReadCloser, error)
	now   func() time.Time
}

func NewSigningService(repo SigningRepository, files FileStorage, open func(string) (io.ReadCloser, error)) *SigningService {
	return &SigningService{repo: repo, files: files, open: open, now: time.Now}
}

func (s *SigningService) Accept(ctx context.Context, actorID, appID, offerID string) (domain.SigningSession, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return domain.SigningSession{}, fmt.Errorf("generate signing code: %w", err)
	}
	now := s.now().UTC()
	var createdKey string
	session, err := s.repo.Accept(ctx, actorID, appID, offerID, fmt.Sprintf("%06d", n.Int64()), now,
		func(app domain.Application, offer domain.OfferRecord) (domain.Document, error) {
			id := "doc_" + ulid.Make().String()
			name := "Кредитный договор " + app.Number + ".txt"
			text := fmt.Sprintf("ДЕМОНСТРАЦИОННЫЙ КРЕДИТНЫЙ ДОГОВОР\nЗаявка: %s (%s)\nЗаёмщик: %s\nИНН: %s\nПредложение: %s\nСумма: %d RUB\nСрок: %d мес.\nГодовая ставка: %.2f %%\nЕжемесячный платёж: %d RUB\nУсловия: %s\nСформирован: %s\n\nУчебный документ. Подтверждение кодом имитирует подписание; реальные выплаты не выполняются.\n",
				app.Number, app.ID, app.Company.Name, app.Company.INN, offer.ID, offer.Amount, offer.TermMonths,
				offer.Rate, offer.MonthlyPayment, string(offer.Terms), now.Format(time.RFC3339))
			st, err := s.files.Stage(name, strings.NewReader(text))
			if err != nil {
				return domain.Document{}, err
			}
			defer s.files.Discard(st)
			key := app.ID + "/" + id + ".txt"
			if err := s.files.Commit(st, key); err != nil {
				return domain.Document{}, err
			}
			createdKey = key
			return domain.Document{ID: id, ApplicationID: app.ID, Type: domain.DocLoanAgreementDraft,
				FileName: name, MimeType: "text/plain; charset=utf-8", SizeBytes: st.Size, SHA256: st.SHA256,
				StorageKey: key, SignatureState: domain.SignatureNotSigned,
				RecognitionState: domain.RecognitionNotApplicable, UploadedAt: now}, nil
		})
	if err != nil && createdKey != "" {
		s.files.Remove(createdKey)
	}
	return session, err
}

func (s *SigningService) Signing(ctx context.Context, actorID, appID string) (domain.SigningSession, error) {
	return s.repo.Signing(ctx, actorID, appID)
}

func (s *SigningService) Confirm(ctx context.Context, actorID, appID, code string) (domain.SigningSession, error) {
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return domain.SigningSession{}, &ValidationError{Fields: []FieldError{{Field: "code", Code: "invalid_format", Message: "введите 6 цифр"}}}
	}
	session, err := s.repo.Signing(ctx, actorID, appID)
	if err != nil {
		return domain.SigningSession{}, err
	}
	if session.Signer.ID != actorID {
		return domain.SigningSession{}, domain.ErrNotAuthorizedSigner
	}
	if session.State == domain.SigningAwaiting {
		f, err := s.open(session.Document.StorageKey)
		if err != nil {
			return domain.SigningSession{}, fmt.Errorf("open signing document: %w", err)
		}
		h := sha256.New()
		_, readErr := io.Copy(h, f)
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(h.Sum(nil)) != session.DocumentSHA256 {
			return domain.SigningSession{}, fmt.Errorf("signing document is unreadable or changed")
		}
	}
	return s.repo.Confirm(ctx, actorID, appID, session.ID, code, s.now().UTC())
}
