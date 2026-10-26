// Package financial implements immutable document processing independently of external data collection.
package financial

import (
	"context"
	"encoding/json"
	"time"
)

const EventRequested = "financial.document.requested"
const MaxFileSize = 20 << 20

type Applicant struct {
	Type      string `json:"applicant_type"`
	INN       string `json:"inn"`
	TaxRegime string `json:"tax_regime"`
	LegalForm string `json:"legal_form"`
}

type SignatureSummary struct {
	State   string `json:"state"`
	Verdict string `json:"verdict,omitempty"`
}

type Document struct {
	IsTest           bool             `json:"is_test,omitempty"`
	Origin           string           `json:"origin"`
	Signatures       SignatureSummary `json:"signatures"`
	ID               string           `json:"id"`
	ApplicationID    string           `json:"application_id"`
	Type             string           `json:"type"`
	FileName         string           `json:"file_name"`
	MIME             string           `json:"mime_type"`
	Size             int64            `json:"size_bytes"`
	SHA256           string           `json:"sha256"`
	StorageKey       string           `json:"-"`
	SignedDocumentID string           `json:"signed_document_id,omitempty"`
	InfoRequestID    string           `json:"info_request_id,omitempty"`
	Recognition      json.RawMessage  `json:"recognition,omitempty" swaggertype:"object"`
	Generation       int              `json:"generation"`
	UploadedAt       time.Time        `json:"uploaded_at"`
}

type Source = map[string]any
type Diagnostic struct {
	Period     string   `json:"period,omitempty"`
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Severity   string   `json:"severity"`
	Sources    []Source `json:"sources"`
	Indicators []string `json:"indicators,omitempty"`
}
type Indicator struct {
	Name         string   `json:"name"`
	Period       string   `json:"period"`
	PeriodKind   string   `json:"period_kind,omitempty"`
	Amount       string   `json:"amount"`
	Basis        string   `json:"basis"`
	Sources      []Source `json:"sources"`
	DataEligible bool     `json:"data_eligible"`
	Reasons      []string `json:"reasons"`
	Eligible     bool     `json:"eligible"`
}
type Extraction struct {
	ParserVersion  string          `json:"parser_version"`
	DocumentSHA256 string          `json:"document_sha256"`
	Extracted      json.RawMessage `json:"extracted" swaggertype:"object"`
	Validation     json.RawMessage `json:"validation" swaggertype:"object"`
	Diagnostics    []Diagnostic    `json:"diagnostics"`
	Indicators     []Indicator     `json:"indicators"`
	DataEligible   bool            `json:"data_eligible"`
}
type Result struct {
	Extraction
	ID                string          `json:"id"`
	DocumentID        string          `json:"document_id"`
	Generation        int             `json:"generation"`
	SignatureReportID string          `json:"signature_report_id"`
	Signature         SignatureReport `json:"signature"`
	ScoringIndicators []Indicator     `json:"scoring_indicators"`
	ProcessedAt       time.Time       `json:"processed_at"`
	ContentSHA256     string          `json:"content_sha256,omitempty"`
	ContentStorageKey string          `json:"-"`
}

// VerifyInput is the local adapter boundary. The optional HTTP adapter uses the same
// JSON (byte slices encoded as base64); no filesystem paths cross the trust boundary.
type VerifyInput struct {
	Document       []byte    `json:"document"`
	DocumentSHA256 string    `json:"document_sha256"`
	Signatures     [][]byte  `json:"signatures"`
	Applicant      Applicant `json:"applicant"`
	Policy         string    `json:"policy"`
}
type SignatureVerifier interface {
	Verify(context.Context, VerifyInput) (SignatureReport, error)
}
type SignatureCheck struct {
	Message string          `json:"message,omitempty"`
	Details json.RawMessage `json:"details,omitempty" swaggertype:"object" extensions:"x-raw-json"`
	Code    string          `json:"code"`
	Status  string          `json:"status"`
}
type SignatureInfo struct {
	Certificate json.RawMessage  `json:"certificate,omitempty" swaggertype:"object"`
	SigningTime json.RawMessage  `json:"signing_time,omitempty" swaggertype:"object"`
	Authority   json.RawMessage  `json:"authority,omitempty" swaggertype:"object"`
	Index       int              `json:"index"`
	Format      string           `json:"format"`
	Container   string           `json:"container"`
	Kind        string           `json:"kind"`
	Verdict     string           `json:"verdict"`
	Checks      []SignatureCheck `json:"checks"`
	Signer      struct {
		FullName     string `json:"full_name"`
		Kind         string `json:"kind"`
		INN          string `json:"inn_individual"`
		Organization *struct {
			INN string `json:"inn"`
		} `json:"organization"`
	} `json:"signer"`
}
type SignatureReport struct {
	ID             string    `json:"id"`
	Verdict        string    `json:"verdict"`
	Policy         string    `json:"policy"`
	DocumentSHA256 string    `json:"document_sha256"`
	VerifiedAt     time.Time `json:"verified_at"`
	TrustMode      string    `json:"trust_mode"`
	PolicyResult   struct {
		Satisfied bool   `json:"satisfied"`
		Message   string `json:"message,omitempty"`
	} `json:"policy_result"`
	Signatures []SignatureInfo `json:"signatures"`
	// Optional exact encapsulated bytes, returned only by the trusted verifier.
	SignedContent       []byte `json:"signed_content,omitempty"`
	SignedContentSHA256 string `json:"signed_content_sha256,omitempty"`
	// Full verifier response, including certificate and individual check diagnostics.
	Raw json.RawMessage `json:"raw,omitempty" swaggertype:"object"`
}

type Parser interface {
	Extract(context.Context, string, Applicant) (Extraction, error)
}
type Job struct {
	ID           string
	DocumentID   string
	Generation   int
	Applicant    Applicant
	SignatureIDs []string
	Status       string
	Attempts     int
}
type Event struct {
	Event  string `json:"event"`
	TaskID string `json:"task_id"`
}
