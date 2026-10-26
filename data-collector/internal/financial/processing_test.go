package financial

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type verifierFunc func(context.Context, VerifyInput) (SignatureReport, error)

func (f verifierFunc) Verify(c context.Context, in VerifyInput) (SignatureReport, error) {
	return f(c, in)
}

type parserFunc func(context.Context, string, Applicant) (Extraction, error)

func (f parserFunc) Extract(c context.Context, path string, a Applicant) (Extraction, error) {
	return f(c, path, a)
}

func validReport(in VerifyInput) SignatureReport {
	r := SignatureReport{Verdict: "VALID", Policy: in.Policy, DocumentSHA256: in.DocumentSHA256, TrustMode: "LIVE"}
	r.PolicyResult.Satisfied = true
	s := SignatureInfo{Index: 0, Format: "CADES_BES", Kind: "QUALIFIED", Verdict: "VALID"}
	s.Signer.Kind = "SOLE_PROPRIETOR"
	s.Signer.INN = in.Applicant.INN
	for _, code := range []string{"FORMAT_PARSED", "INTEGRITY", "SIGNATURE_VALUE", "CERT_VALID_AT_SIGNING_TIME", "CHAIN_TO_TRUSTED_ROOT", "ISSUER_ACCREDITED", "NOT_REVOKED", "QUALIFIED_ATTRIBUTES", "KEY_USAGE", "SIGNER_MATCHES_APPLICANT", "SIGNER_AUTHORITY", "DOCUMENT_NOT_MODIFIED", "SIGNER_IS_FNS"} {
		s.Checks = append(s.Checks, SignatureCheck{Code: code, Status: "PASSED"})
	}
	r.Signatures = []SignatureInfo{s}
	return r
}

func TestSignatureGate(t *testing.T) {
	a := Applicant{Type: "SOLE_PROPRIETOR", INN: "123456789012", TaxRegime: "USN_INCOME"}
	in := VerifyInput{DocumentSHA256: hash([]byte("original")), Applicant: a, Policy: policy(a)}
	tests := []struct {
		name   string
		mutate func(*SignatureReport)
		want   bool
	}{
		{"valid", func(r *SignatureReport) {}, true},
		{"unavailable", func(r *SignatureReport) { r.Verdict = "INDETERMINATE" }, false},
		{"modified", func(r *SignatureReport) { r.DocumentSHA256 = hash([]byte("changed")) }, false},
		{"mock trust", func(r *SignatureReport) { r.TrustMode = "MOCK" }, false},
		{"policy missing", func(r *SignatureReport) { r.PolicyResult.Satisfied = false }, false},
		{"unqualified", func(r *SignatureReport) { r.Signatures[0].Kind = "UNQUALIFIED" }, false},
		{"unsupported", func(r *SignatureReport) { r.Signatures[0].Format = "UNKNOWN" }, false},
		{"unknown format", func(r *SignatureReport) { r.Signatures[0].Format = "ALIEN" }, false},
		{"wrong borrower", func(r *SignatureReport) { r.Signatures[0].Signer.INN = "000000000000" }, false},
		{"revocation unavailable", func(r *SignatureReport) {
			for i, c := range r.Signatures[0].Checks {
				if c.Code == "NOT_REVOKED" {
					r.Signatures[0].Checks[i].Status = "SKIPPED"
				}
			}
		}, false},
		{"checks absent", func(r *SignatureReport) { r.Signatures[0].Checks = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validReport(in)
			tt.mutate(&r)
			if got := signatureEligible(r, a, in.DocumentSHA256, false); got != tt.want {
				t.Fatalf("eligible=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestFNSIsSignerNotBorrowerOrIssuer(t *testing.T) {
	a := Applicant{Type: "LEGAL_ENTITY", INN: "1234567890", LegalForm: "OOO"}
	in := VerifyInput{DocumentSHA256: hash([]byte("PDF")), Applicant: a, Policy: policy(a)}
	r := validReport(in)
	r.Signatures[0].Signer.Organization = &struct {
		INN string `json:"inn"`
	}{INN: "7707329152"}
	if !signatureEligible(r, a, in.DocumentSHA256, true) {
		t.Fatal("FNS signature should not require signer INN = borrower")
	}
	r.Signatures[0].Signer.Organization.INN = a.INN
	if signatureEligible(r, a, in.DocumentSHA256, true) {
		t.Fatal("borrower signature must not satisfy FNS policy")
	}
}

func TestProcessorVerifiesBeforeParsingAndBlocksIncomplete(t *testing.T) {
	files := Files{Root: t.TempDir()}
	raw := []byte("original XLS")
	key, err := files.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	d := Document{ID: "doc", StorageKey: key, SHA256: hash(raw)}
	j := Job{Generation: 1, Applicant: Applicant{Type: "SOLE_PROPRIETOR", INN: "123456789012"}}
	verified := false
	p := Processor{Files: files, Verifier: verifierFunc(func(ctx context.Context, in VerifyInput) (SignatureReport, error) {
		if string(in.Document) != string(raw) {
			t.Fatal("original bytes changed")
		}
		verified = true
		return validReport(in), nil
	}), Parser: parserFunc(func(ctx context.Context, path string, a Applicant) (Extraction, error) {
		if !verified {
			t.Fatal("parser called before verifier")
		}
		return Extraction{DocumentSHA256: hash(raw), ParserVersion: "test", Extracted: json.RawMessage(`{}`), Validation: json.RawMessage(`[]`), Indicators: []Indicator{{Name: "usn_taxable_income", Amount: "6355000.00", DataEligible: false}}}, nil
	})}
	result, err := p.Process(context.Background(), j, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Signature.Verdict != "VALID" || len(result.ScoringIndicators) != 0 {
		t.Fatal("valid signature must not admit incomplete data")
	}
}

func TestChangedStoredOriginalNeverReusesSignature(t *testing.T) {
	files := Files{Root: t.TempDir()}
	key, _ := files.Put([]byte("original"))
	path, _ := files.Path(key)
	if err := os.WriteFile(path, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	p := Processor{Files: files, Verifier: verifierFunc(func(context.Context, VerifyInput) (SignatureReport, error) {
		called = true
		return SignatureReport{}, nil
	}),
		Parser: parserFunc(func(context.Context, string, Applicant) (Extraction, error) {
			return Extraction{DocumentSHA256: hash([]byte("modified")), Indicators: []Indicator{{DataEligible: true}}}, nil
		})}
	r, err := p.Process(context.Background(), Job{Applicant: Applicant{Type: "SOLE_PROPRIETOR"}}, Document{StorageKey: key, SHA256: hash([]byte("original"))}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if called || r.Signature.Verdict != "INVALID" || len(r.ScoringIndicators) != 0 {
		t.Fatal("tampered original admitted")
	}
}

func TestAbsentVerifierBlocksEvenGoodData(t *testing.T) {
	files := Files{Root: t.TempDir()}
	raw := []byte("original")
	key, _ := files.Put(raw)
	p := Processor{Files: files, Verifier: HTTPVerifier{}, Parser: parserFunc(func(context.Context, string, Applicant) (Extraction, error) {
		return Extraction{DocumentSHA256: hash(raw), Indicators: []Indicator{{DataEligible: true, Name: "accounting_revenue"}}}, nil
	})}
	r, err := p.Process(context.Background(), Job{}, Document{StorageKey: key, SHA256: hash(raw)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Signature.Verdict != "INDETERMINATE" || len(r.ScoringIndicators) > 0 {
		t.Fatal("absent verifier considered successful")
	}
}

func TestParserFailureDoesNotEmitIndicators(t *testing.T) {
	files := Files{Root: t.TempDir()}
	raw := []byte("original")
	key, _ := files.Put(raw)
	p := Processor{Files: files, Verifier: HTTPVerifier{}, Parser: parserFunc(func(context.Context, string, Applicant) (Extraction, error) {
		return Extraction{}, errors.New("broken")
	})}
	r, err := p.Process(context.Background(), Job{}, Document{StorageKey: key, SHA256: hash(raw)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ScoringIndicators) > 0 || r.Diagnostics[0].Code != "PARSE_FAILED" {
		t.Fatal("parse failure lost")
	}
}

func TestFilesRejectPathTraversal(t *testing.T) {
	files := Files{Root: t.TempDir()}
	if _, err := files.Read("../../etc/passwd"); err == nil {
		t.Fatal("path traversal accepted")
	}
	a, _ := files.Put([]byte("one"))
	b, _ := files.Put([]byte("one"))
	if a == b {
		t.Fatal("uploads must preserve separate originals")
	}
}

func privateFixture(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("FINANCIAL_FIXTURES_DIR")
	if dir == "" {
		dir = "../../financial_parser/testdata"
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("private fixture missing; set FINANCIAL_FIXTURES_DIR: " + name)
	} else if err != nil {
		t.Fatal(err)
	}
	return path
}

type fixtureCase struct {
	Applicant Applicant         `json:"applicant"`
	Expected  map[string]string `json:"expected"`
}

func privateCase(t *testing.T, name string) fixtureCase {
	t.Helper()
	raw, err := os.ReadFile(privateFixture(t, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases map[string]fixtureCase
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	c, ok := cases[name]
	if !ok || c.Applicant.INN == "" {
		t.Fatal("private case missing: " + name)
	}
	return c
}

// FINANCIAL_REAL_TESTS=1 enables actual Go -> Python -> XLS/PDF integration.
func TestRealDocumentsThroughProcessor(t *testing.T) {
	if os.Getenv("FINANCIAL_REAL_TESTS") != "1" {
		t.Skip("set FINANCIAL_REAL_TESTS=1 after installing parser requirements")
	}
	for _, file := range []string{"kudir-2026.xls", "girbo-2025.pdf"} {
		t.Run(file, func(t *testing.T) {
			tc := privateCase(t, file)
			raw, err := os.ReadFile(privateFixture(t, file))
			if err != nil {
				t.Fatal(err)
			}
			files := Files{Root: t.TempDir()}
			key, err := files.Put(raw)
			if err != nil {
				t.Fatal(err)
			}
			parserPath, err := filepath.Abs("../../financial_parser/parser.py")
			if err != nil {
				t.Fatal(err)
			}
			p := Processor{Files: files, Verifier: HTTPVerifier{}, Parser: PythonParser{Script: parserPath}}
			result, err := p.Process(context.Background(), Job{Generation: 1, Applicant: tc.Applicant}, Document{ID: "test", StorageKey: key, SHA256: hash(raw)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.ParserVersion != "financial-v1" || len(result.Indicators) == 0 {
				t.Fatalf("extraction failed: %+v", result.Diagnostics)
			}
			if result.Signature.Verdict != "INDETERMINATE" || len(result.ScoringIndicators) != 0 {
				t.Fatal("actual documents must not bypass missing signature verification")
			}
			if file == "kudir-2026.xls" && result.Indicators[0].Amount != "6355000.00" {
				t.Fatal(result.Indicators)
			}
			if file == "girbo-2025.pdf" {
				found := false
				for _, i := range result.Indicators {
					if i.Name == "accounting_revenue" && i.Period == "2025" && i.Amount == tc.Expected["2110/2025"] {
						found = true
					}
				}
				if !found {
					t.Fatal("revenue not extracted")
				}
			}
		})
	}
}
