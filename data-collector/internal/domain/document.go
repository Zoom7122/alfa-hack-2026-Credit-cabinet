package domain

import (
	"slices"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
)

// DocumentType — тип документа (documents.type, DocumentType в api/openapi.yaml).
type DocumentType string

const (
	DocFinancialStatements  DocumentType = "FINANCIAL_STATEMENTS"
	DocTaxReturn            DocumentType = "TAX_RETURN"
	DocBankStatement        DocumentType = "BANK_STATEMENT"
	DocManagementReport     DocumentType = "MANAGEMENT_REPORT"
	DocCharter              DocumentType = "CHARTER"
	DocPowerOfAttorney      DocumentType = "POWER_OF_ATTORNEY"
	DocDirectorPassport     DocumentType = "DIRECTOR_PASSPORT"
	DocCollateralTitle      DocumentType = "COLLATERAL_TITLE"
	DocEGRNExtract          DocumentType = "EGRN_EXTRACT"
	DocLeaseAgreement       DocumentType = "LEASE_AGREEMENT"
	DocContractWithCustomer DocumentType = "CONTRACT_WITH_CUSTOMER"
	DocLoanAgreementDraft   DocumentType = "LOAN_AGREEMENT_DRAFT"
	DocSignature            DocumentType = "SIGNATURE" // открепленная подпись к другому документу
	DocOther                DocumentType = "OTHER"
)

// DocumentTypes — все типы документов.
var DocumentTypes = []DocumentType{DocFinancialStatements, DocTaxReturn, DocBankStatement, DocManagementReport,
	DocCharter, DocPowerOfAttorney, DocDirectorPassport, DocCollateralTitle, DocEGRNExtract, DocLeaseAgreement,
	DocContractWithCustomer, DocLoanAgreementDraft, DocSignature, DocOther}

// Valid сообщает, входит ли значение в допустимый перечень.
func (t DocumentType) Valid() bool { return slices.Contains(DocumentTypes, t) }

// SignatureState — проверка электронной подписи документа (её делает worker подписи).
type SignatureState string

const (
	SignatureNotSigned SignatureState = "NOT_SIGNED" // подписи нет (встроенную в PDF/XML worker найдёт сам)
	SignaturePending   SignatureState = "PENDING"    // есть подпись, ждёт проверки
	SignatureDone      SignatureState = "DONE"       // проверена: отчёт в signature_reports, вердикт — в signature_verdict
	SignatureFailed    SignatureState = "FAILED"     // проверить не удалось: нет файла или внутренняя ошибка (отчёт тоже есть)
)

// RecognitionState — распознавание документа (его сделает отдельный worker OCR).
type RecognitionState string

const (
	RecognitionNotApplicable RecognitionState = "NOT_APPLICABLE" // нечего распознавать: подпись, XML
	RecognitionPending       RecognitionState = "PENDING"        // ждёт распознавания
)

// Document — файл заявки. Сам файл лежит в хранилище по StorageKey.
type Document struct {
	ID               string // doc_<ULID>
	ApplicationID    string
	Type             DocumentType
	FileName         string // имя, с которым файл пришёл
	MimeType         string
	SizeBytes        int64
	SHA256           string
	StorageKey       string // путь в хранилище: {application_id}/{id}.{ext}
	UploadedBy       string // usr_…
	SignedDocumentID string // у открепленной подписи — какой документ она подписывает
	InfoRequestID    string // вопрос банка, к которому загружен документ
	SignatureState   SignatureState
	SignatureVerdict string // итог проверки подписи (VALID, VALID_WITH_WARNINGS, INVALID, INDETERMINATE); пусто — не проверяли
	RecognitionState RecognitionState
	UploadedAt       time.Time
}

// IsTest is derived from bytes hashed by storage, never a request flag or filename.
func (d Document) IsTest() bool { return testdocument.IsSHA256(d.SHA256) }
