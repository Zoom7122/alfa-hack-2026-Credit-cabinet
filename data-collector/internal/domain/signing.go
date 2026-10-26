package domain

import (
	"errors"
	"time"
)

const (
	SigningAwaiting = "AWAITING_SIGNER"
	SigningSigned   = "SIGNED"
	SigningFailed   = "FAILED"
	SigningAttempts = 3
	SigningCodeTTL  = 10 * time.Minute
)

var (
	ErrOfferExpired        = errors.New("offer expired")
	ErrNotAuthorizedSigner = errors.New("not authorized signer")
)

// SigningSession — демо-подписание конкретных байтов договора по выбранному офферу.
// Code доступен только подписанту и только пока сессия ждёт подтверждения.
type SigningSession struct {
	ID             string
	ApplicationID  string
	OfferID        string
	State          string
	Signer         UserRef
	Document       Document
	DocumentSHA256 string
	Code           string
	CodeExpiresAt  time.Time
	AttemptsLeft   int
	SignedAt       *time.Time
	Disbursement   *MockDisbursement
}

type MockDisbursement struct {
	Amount        int64     `json:"amount"`
	AccountMasked string    `json:"account_masked"`
	ExecutedAt    time.Time `json:"executed_at"`
}

// SigningCodeError возвращается после сохранения неудачной попытки в БД.
type SigningCodeError struct {
	Expired      bool
	AttemptsLeft int
}

func (e *SigningCodeError) Error() string {
	if e.Expired {
		return "signing code expired"
	}
	return "invalid signing code"
}
