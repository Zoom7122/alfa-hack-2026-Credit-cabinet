package financial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"

	"github.com/oklog/ulid/v2"
)

func newID(prefix string) string { return prefix + ulid.Make().String() }
func hash(b []byte) string       { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Files never overwrites an existing object. Keys, unlike filenames, are generated internally.
type Files struct{ Root string }

var safeKey = regexp.MustCompile(`^[a-z]+_[0-9A-HJKMNP-TV-Z]{26}$`)

func (s Files) Path(key string) (string, error) {
	if !safeKey.MatchString(key) {
		return "", errors.New("invalid storage key")
	}
	return filepath.Join(s.Root, key), nil
}
func (s Files) Put(b []byte) (string, error) {
	if len(b) == 0 || len(b) > MaxFileSize {
		return "", errors.New("file must contain 1..20 MiB")
	}
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return "", err
	}
	key := newID("blob_")
	path, _ := s.Path(key)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return key, nil
}
func (s Files) Read(key string) ([]byte, error) {
	p, err := s.Path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if len(b) > MaxFileSize {
		return nil, errors.New("stored object too large")
	}
	return b, err
}

// HTTPVerifier is opt-in; an unset URL does not manufacture successful verification.
type HTTPVerifier struct {
	URL    string
	Client *http.Client
}

func (v HTTPVerifier) Verify(ctx context.Context, in VerifyInput) (SignatureReport, error) {
	if v.URL == "" {
		return SignatureReport{}, errors.New("signature verifier is not configured")
	}
	body, err := json.Marshal(in)
	if err != nil {
		return SignatureReport{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.URL, bytes.NewReader(body))
	if err != nil {
		return SignatureReport{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return SignatureReport{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return SignatureReport{}, fmt.Errorf("verifier HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 30<<20))
	if err != nil {
		return SignatureReport{}, err
	}
	var out SignatureReport
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	// Preserve complete report but avoid duplicating an encapsulated document in API responses.
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return out, err
	}
	delete(fields, "signed_content")
	delete(fields, "raw")
	out.Raw, _ = json.Marshal(fields)
	return out, nil
}

type PythonParser struct {
	Python string
	Script string
}

func (p PythonParser) Extract(ctx context.Context, path string, a Applicant) (Extraction, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	input := struct {
		Applicant
		Path string `json:"path"`
	}{a, path}
	body, _ := json.Marshal(input)
	python := p.Python
	if python == "" {
		python = "python3"
	}
	cmd := exec.CommandContext(ctx, python, p.Script)
	cmd.Stdin = bytes.NewReader(body)
	var stdout limitedBuffer
	stdout.max = 16 << 20
	var stderr limitedBuffer
	stderr.max = 8192
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Extraction{}, fmt.Errorf("parser failed: %w", err)
	}
	var out Extraction
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return out, err
	}
	if out.ParserVersion == "" || out.Extracted == nil {
		return out, errors.New("incomplete parser response")
	}
	return out, nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("subprocess output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func policy(a Applicant) string {
	if a.Type == "SOLE_PROPRIETOR" {
		return "KUDIR_IP"
	}
	return "GIRBO_FNS"
}
func signatureEligible(r SignatureReport, a Applicant, sha string, pdf bool) bool {
	if testdocument.IsSHA256(sha) {
		return false
	}
	if r.DocumentSHA256 != sha || r.Policy != policy(a) || !r.PolicyResult.Satisfied || r.TrustMode != "LIVE" {
		return false
	}
	if r.Verdict != "VALID" && r.Verdict != "VALID_WITH_WARNINGS" {
		return false
	}
	for _, s := range r.Signatures {
		switch s.Format {
		case "CADES_BES", "CADES_T", "CADES_X_LONG_TYPE_1", "PADES", "XADES", "XMLDSIG":
		default:
			continue
		}
		if s.Kind != "QUALIFIED" || s.Format == "" || s.Format == "UNKNOWN" || (s.Verdict != "VALID" && s.Verdict != "VALID_WITH_WARNINGS") {
			continue
		}
		checks := map[string]string{}
		for _, c := range s.Checks {
			if checks[c.Code] != "" && checks[c.Code] != c.Status {
				return false
			}
			checks[c.Code] = c.Status
		}
		required := []string{"FORMAT_PARSED", "INTEGRITY", "SIGNATURE_VALUE", "CERT_VALID_AT_SIGNING_TIME", "CHAIN_TO_TRUSTED_ROOT", "ISSUER_ACCREDITED", "NOT_REVOKED", "QUALIFIED_ATTRIBUTES", "KEY_USAGE"}
		if pdf {
			required = append(required, "DOCUMENT_NOT_MODIFIED")
		}
		if a.Type == "SOLE_PROPRIETOR" {
			if s.Signer.Kind != "SOLE_PROPRIETOR" || s.Signer.INN != a.INN {
				continue
			}
			required = append(required, "SIGNER_MATCHES_APPLICANT", "SIGNER_AUTHORITY")
		} else {
			if s.Signer.Organization == nil || s.Signer.Organization.INN != "7707329152" {
				continue
			}
			required = append(required, "SIGNER_IS_FNS")
		}
		ok := true
		for _, c := range required {
			if checks[c] != "PASSED" {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// EligibleForScoring rechecks the persisted signature boundary when a decision
// reads parser results. TEST/MOCK and incomplete verification remain excluded.
func (r Result) EligibleForScoring(a Applicant) bool {
	return signatureEligible(r.Signature, a, r.DocumentSHA256, a.Type == "LEGAL_ENTITY")
}

type Processor struct {
	Files    Files
	Verifier SignatureVerifier
	Parser   Parser
}

func (p Processor) Process(ctx context.Context, j Job, d Document, signatures []Document) (Result, error) {
	raw, err := p.Files.Read(d.StorageKey)
	if err != nil {
		return Result{}, err
	}
	result := Result{ID: newID("fin_"), DocumentID: d.ID, Generation: j.Generation, ProcessedAt: time.Now().UTC(), ScoringIndicators: []Indicator{}}
	in := VerifyInput{Document: raw, DocumentSHA256: d.SHA256, Applicant: j.Applicant, Policy: policy(j.Applicant), Signatures: [][]byte{}}
	integrity := hash(raw) == d.SHA256
	for _, s := range signatures {
		b, e := p.Files.Read(s.StorageKey)
		if e != nil {
			return Result{}, e
		}
		integrity = integrity && hash(b) == s.SHA256
		in.Signatures = append(in.Signatures, b)
	}
	var report SignatureReport
	if integrity && p.Verifier != nil {
		report, err = p.Verifier.Verify(ctx, in)
	} else {
		err = errors.New("original or signature integrity check failed")
	}
	if err != nil {
		report = SignatureReport{Verdict: "INDETERMINATE", Policy: in.Policy, DocumentSHA256: d.SHA256, TrustMode: "LIVE", Signatures: []SignatureInfo{}}
		report.PolicyResult.Message = "Verification unavailable or incomplete"
	}
	if !integrity {
		report.Verdict = "INVALID"
		report.PolicyResult.Satisfied = false
		report.PolicyResult.Message = "Stored original/signature hash mismatch"
	}
	if report.Verdict != "VALID" && report.Verdict != "VALID_WITH_WARNINGS" && report.Verdict != "INVALID" && report.Verdict != "INDETERMINATE" {
		report.Verdict = "INDETERMINATE"
		report.PolicyResult.Satisfied = false
	}
	report.ID = newID("sigr_")
	report.VerifiedAt = time.Now().UTC()
	if report.TrustMode != "LIVE" && report.TrustMode != "MOCK" {
		report.TrustMode = "LIVE"
		report.PolicyResult.Satisfied = false
	}
	if report.Policy != in.Policy || report.DocumentSHA256 != d.SHA256 {
		report.PolicyResult.Satisfied = false
		report.Verdict = "INDETERMINATE"
	}
	// Local report fields remain tied to this verification request even on malformed service responses.
	report.Policy = in.Policy
	payload := raw
	key := d.StorageKey
	if len(report.SignedContent) > 0 {
		if len(report.SignedContent) > MaxFileSize || hash(report.SignedContent) != report.SignedContentSHA256 || !signatureEligible(report, j.Applicant, d.SHA256, false) {
			report.PolicyResult.Satisfied = false
			report.Verdict = "INDETERMINATE"
		} else {
			payload = report.SignedContent
			key, err = p.Files.Put(payload)
			if err != nil {
				return Result{}, err
			}
			result.ContentStorageKey = key
			result.ContentSHA256 = hash(payload)
		}
	}
	report.SignedContent = nil
	result.Signature = report
	result.SignatureReportID = report.ID
	path, _ := p.Files.Path(key)
	extracted, e := p.Parser.Extract(ctx, path, j.Applicant)
	if e != nil {
		extracted = Extraction{ParserVersion: "unavailable", DocumentSHA256: hash(payload), Extracted: json.RawMessage(`{}`), Validation: json.RawMessage(`[]`), Diagnostics: []Diagnostic{{Code: "PARSE_FAILED", Severity: "ERROR", Message: "Financial parser failed"}}}
	}
	if extracted.DocumentSHA256 != hash(payload) {
		extracted.Indicators = nil
		extracted.DataEligible = false
		extracted.Diagnostics = append(extracted.Diagnostics, Diagnostic{Code: "PARSER_HASH_MISMATCH", Severity: "ERROR", Message: "Extraction is not bound to the original"})
	}
	result.Extraction = extracted
	if !integrity {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "STORED_HASH_MISMATCH", Severity: "ERROR", Message: "Stored original or signature bytes do not match their recorded hash"})
	}
	// Top-level SHA always identifies uploaded bytes, not an extracted container payload.
	result.DocumentSHA256 = d.SHA256
	signed := integrity && signatureEligible(report, j.Applicant, d.SHA256, bytes.HasPrefix(payload, []byte("%PDF")))
	for i := range result.Indicators {
		item := &result.Indicators[i]
		item.Eligible = item.DataEligible && signed
		if !signed {
			item.Reasons = append(item.Reasons, "SIGNATURE_NOT_VERIFIED")
		}
		if item.Eligible {
			result.ScoringIndicators = append(result.ScoringIndicators, *item)
		}
	}
	if !signed {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "SIGNATURE_NOT_VERIFIED", Severity: "ERROR", Message: "Required signature checks have not all passed"})
	}
	if result.Extracted == nil {
		result.Extracted = json.RawMessage(`{}`)
	}
	if result.Validation == nil {
		result.Validation = json.RawMessage(`[]`)
	}
	if result.Indicators == nil {
		result.Indicators = []Indicator{}
	}
	if result.Diagnostics == nil {
		result.Diagnostics = []Diagnostic{}
	}
	for i := range result.Diagnostics {
		if result.Diagnostics[i].Sources == nil {
			result.Diagnostics[i].Sources = []Source{}
		}
	}
	if result.Signature.Signatures == nil {
		result.Signature.Signatures = []SignatureInfo{}
	}
	return result, nil
}
