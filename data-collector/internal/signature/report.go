package signature

import (
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	statusVerified      = domain.VerificationVerified
	statusInvalid       = domain.VerificationInvalid
	statusIndeterminate = domain.VerificationIndeterminate
	statusNotRequired   = domain.VerificationNotRequired
)

// Как подпись связана с документом (container в отчёте, SignatureInfo.container в api/openapi.yaml).
type Container string

const (
	ContainerDetached Container = "DETACHED"      // открепленная подпись .sig/.p7s к отдельному файлу
	ContainerAttached Container = "ATTACHED"      // присоединённая: документ внутри контейнера CMS
	ContainerPDF      Container = "PDF_EMBEDDED"  // подпись внутри PDF (PAdES): проверяется, если одна и покрывает весь файл
	ContainerXML      Container = "XML_ENVELOPED" // подпись внутри XML (XMLDSig/XAdES)
)

// Report — подробный отчёт проверки одной подписи (signature_reports.report).
// Поля верхнего уровня совместимы с SignatureReport из api/openapi.yaml; этапы, профиль
// и итог по правилам банка — дополнительно.
type Report struct {
	ID            string `json:"id"`
	Profile       string `json:"profile"`        // TEST
	ProfileNotice string `json:"profile_notice"` // чем профиль отличается от проверки УКЭП
	TrustMode     string `json:"trust_mode"`     // MOCK
	RulesVersion  string `json:"rules_version"`

	ApplicationID       string              `json:"application_id,omitempty"`
	DocumentID          string              `json:"document_id,omitempty"`
	DocumentType        domain.DocumentType `json:"document_type"`
	FileName            string              `json:"file_name,omitempty"`
	DocumentSHA256      string              `json:"document_sha256"` // проверенные байты документа
	SignatureDocumentID string              `json:"signature_document_id,omitempty"`
	SignatureFileName   string              `json:"signature_file_name,omitempty"`
	SignatureSHA256     string              `json:"signature_sha256,omitempty"`
	Container           Container           `json:"container"`
	Format              string              `json:"format"` // CADES_BES, CMS, PADES, XMLDSIG, UNKNOWN
	Encoding            string              `json:"encoding,omitempty"`

	Decision      domain.VerificationStatus `json:"decision"`
	Verdict       string                    `json:"verdict"` // VALID_WITH_WARNINGS, INVALID, INDETERMINATE
	FailureReason string                    `json:"failure_reason,omitempty"`
	FailedCheck   string                    `json:"failed_check,omitempty"`   // код первой непройденной проверки
	InternalError bool                      `json:"internal_error,omitempty"` // проверка упала: отчёт INDETERMINATE
	Policy        string                    `json:"policy"`
	PolicyResult  PolicyResult              `json:"policy_result"`
	Stages        Stages                    `json:"stages"`

	PDF         *PDFSignature      `json:"pdf,omitempty"` // подпись внутри PDF
	Algorithm   string             `json:"algorithm,omitempty"`
	SigningTime *SigningTime       `json:"signing_time,omitempty"`
	Signer      *SignerReport      `json:"signer,omitempty"`
	Certificate *CertificateReport `json:"certificate,omitempty"`
	Authority   *AuthorityReport   `json:"authority,omitempty"`

	ScopeNote   string    `json:"scope_note"`
	Limitations []string  `json:"limitations"`
	VerifiedAt  time.Time `json:"verified_at"`
}

// PDFSignature — подпись внутри PDF: что подписано.
type PDFSignature struct {
	ByteRange   []int  `json:"byte_range"` // [0, b, c, d]: подписаны байты [0, b) и [c, c+d)
	SubFilter   string `json:"sub_filter"`
	SignedBytes int    `json:"signed_bytes"`
}

// PolicyResult — выполнена ли политика (signature_reports.policy_satisfied).
type PolicyResult struct {
	Satisfied bool   `json:"satisfied"`
	Message   string `json:"message"`
}

// Stages — результаты этапов в порядке проверки.
type Stages struct {
	Integrity   Stage `json:"integrity"`
	Format      Stage `json:"format"`
	Certificate Stage `json:"certificate"`
	Identity    Stage `json:"identity"`
	Authority   Stage `json:"authority"`
	Decision    Stage `json:"decision"`
}

// Stage — итог этапа и его проверки. Status — худший из статусов проверок: INVALID, затем
// INDETERMINATE; NOT_REQUIRED — когда этап по правилам не нужен.
type Stage struct {
	Status   domain.VerificationStatus `json:"status"`
	Reason   string                    `json:"reason,omitempty"`
	Checks   []Check                   `json:"checks,omitempty"`
	Warnings []string                  `json:"warnings,omitempty"`

	closed bool // статус выставлен: finish, skip или notRequired
}

// Check — одна проверка этапа. Коды — SignatureCheckCode из api/openapi.yaml и свои для MVP.
type Check struct {
	Code    string                    `json:"code"`
	Status  domain.VerificationStatus `json:"status"`
	Message string                    `json:"message,omitempty"`
}

// SigningTime — время подписи и откуда оно взято.
type SigningTime struct {
	Value  *time.Time `json:"value"`
	Source string     `json:"source"` // SIGNED_ATTRIBUTE — заявлено подписантом; NONE — нет
}

// SignerReport — подписант по сертификату (SignerIdentity из api/openapi.yaml).
type SignerReport struct {
	Kind          string        `json:"kind"` // LEGAL_ENTITY_HEAD, LEGAL_ENTITY_EMPLOYEE, SOLE_PROPRIETOR, INDIVIDUAL
	FullName      string        `json:"full_name"`
	Position      string        `json:"position,omitempty"`
	INNIndividual string        `json:"inn_individual,omitempty"`
	SNILSMasked   string        `json:"snils_masked,omitempty"`
	OGRNIP        string        `json:"ogrnip,omitempty"`
	Organization  *Organization `json:"organization,omitempty"`
}

// Organization — организация из сертификата.
type Organization struct {
	Name string `json:"name,omitempty"`
	INN  string `json:"inn,omitempty"`
	OGRN string `json:"ogrn,omitempty"`
}

// CertificateReport — сертификат подписанта (CertificateInfo из api/openapi.yaml).
type CertificateReport struct {
	Subject      string            `json:"subject"`
	SerialNumber string            `json:"serial_number"`
	Thumbprint   string            `json:"thumbprint"` // SHA-1, hex
	SHA256       string            `json:"sha256"`
	ValidFrom    time.Time         `json:"valid_from"`
	ValidTo      time.Time         `json:"valid_to"`
	Issuer       CertificateIssuer `json:"issuer"`
	KeyAlgorithm string            `json:"key_algorithm"`
	KeyUsage     []string          `json:"key_usage,omitempty"`
	Chain        []string          `json:"chain,omitempty"`
	Revocation   *Revocation       `json:"revocation,omitempty"`
}

// CertificateIssuer — издатель сертификата. Accredited — всегда false в профиле TEST.
type CertificateIssuer struct {
	Name       string `json:"name"`
	Accredited bool   `json:"accredited"`
}

// Revocation — проверка отзыва сертификата подписанта.
type Revocation struct {
	Status    string     `json:"status"` // GOOD, REVOKED, UNKNOWN
	Method    string     `json:"method"` // CRL, NONE
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CheckedAt time.Time  `json:"checked_at"`
}

// AuthorityReport — на каком основании подписант действует за заявителя.
type AuthorityReport struct {
	Basis           string           `json:"basis"` // REGISTRY_HEAD, SOLE_PROPRIETOR, POWER_OF_ATTORNEY, NONE
	ApplicantINN    string           `json:"applicant_inn"`
	ApplicantType   string           `json:"applicant_type"`
	RequiredPower   string           `json:"required_power,omitempty"`
	PowerOfAttorney *PowerOfAttorney `json:"power_of_attorney,omitempty"`
	Source          string           `json:"source,omitempty"` // откуда данные ЕГРЮЛ и МЧД
}

// PowerOfAttorney — МЧД, по которой проверялись полномочия.
type PowerOfAttorney struct {
	Number         string   `json:"number"`
	RegistryStatus string   `json:"registry_status"` // ACTIVE, EXPIRED, REVOKED
	ValidFrom      string   `json:"valid_from,omitempty"`
	ValidTo        string   `json:"valid_to,omitempty"`
	RevokedAt      string   `json:"revoked_at,omitempty"`
	Powers         []string `json:"powers"`
}

// add добавляет проверку в этап.
func (s *Stage) add(code string, status domain.VerificationStatus, msg string) {
	s.Checks = append(s.Checks, Check{Code: code, Status: status, Message: msg})
}

// warn добавляет замечание: на статус этапа не влияет.
func (s *Stage) warn(msg string) {
	s.Warnings = append(s.Warnings, msg)
}

// finish выставляет статус этапа по проверкам: INVALID, затем INDETERMINATE, затем VERIFIED;
// если обязательных проверок нет — NOT_REQUIRED. Причина — сообщение первой худшей проверки.
func (s *Stage) finish() {
	s.Status, s.Reason, s.closed = statusNotRequired, "", true
	for _, want := range []domain.VerificationStatus{statusInvalid, statusIndeterminate, statusVerified} {
		for _, c := range s.Checks {
			if c.Status == want {
				s.Status = want
				if want != statusVerified {
					s.Reason = c.Message
				}
				return
			}
		}
	}
}

// skip — этап не проверялся из-за предыдущего: INDETERMINATE с причиной.
func (s *Stage) skip(reason string) {
	s.Status, s.Reason, s.Checks, s.closed = statusIndeterminate, "не проверялось: "+reason, nil, true
}

// notRequired — этап не нужен по правилам банка.
func (s *Stage) notRequired(reason string) {
	s.Status, s.Reason, s.Checks, s.closed = statusNotRequired, reason, nil, true
}

// ok — этап пройден или не нужен.
func (s *Stage) ok() bool {
	return s.Status == statusVerified || s.Status == statusNotRequired
}
