package signature_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature/sigtest"
)

// now — «сегодня» проверки: тот же день, что mockkit.AsOf в mock-api.
var now = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

var (
	certFrom  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	certUntil = time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Люди и компании — из сидов mock-api: ООО «Северный ветер» (директор Ковалёв, бухгалтер
// Литвинова с МЧД 0f5a2c1e…), Гусев с отозванной МЧД, ИП Смирнова, директор «Стройресурса».
var (
	kovalev = sigtest.Subject{Surname: "Ковалёв", GivenName: "Игорь Сергеевич", Title: "Генеральный директор",
		Organization: `ООО "СЕВЕРНЫЙ ВЕТЕР"`, INN: "779900010139", OrgINN: "7799000012", OGRN: "1217700412345"}
	litvinova = sigtest.Subject{Surname: "Литвинова", GivenName: "Марина Олеговна", Title: "Главный бухгалтер",
		INN: "779900010333", SNILS: "148-562-907 41"}
	gusev    = sigtest.Subject{Surname: "Гусев", GivenName: "Роман Ильич", INN: "779900010417"}
	smirnova = sigtest.Subject{Surname: "Смирнова", GivenName: "Анна Викторовна", INN: "779900010202", OGRNIP: "326774600012345"}
	orlov    = sigtest.Subject{Surname: "Орлов", GivenName: "Денис Павлович", Title: "Генеральный директор",
		Organization: `ООО "СТРОЙРЕСУРС"`, INN: "779900020136", OrgINN: "7799000020", OGRN: "1197700523456"}
)

// pki — тестовый УЦ профиля TEST с CRL и выданными сертификатами; собирается один раз.
type pki struct {
	ca      *sigtest.CA
	crl     *x509.RevocationList
	signers map[string]*sigtest.Signer
}

var (
	pkiOnce sync.Once
	pkiVal  *pki
	pkiErr  error
)

func testPKI(t *testing.T) *pki {
	t.Helper()
	pkiOnce.Do(func() { pkiVal, pkiErr = newPKI() })
	if pkiErr != nil {
		t.Fatal(pkiErr)
	}
	return pkiVal
}

func newPKI() (*pki, error) {
	ca, err := sigtest.NewCA("Тестовый корневой УЦ (TEST)", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, err
	}
	p := &pki{ca: ca, signers: map[string]*sigtest.Signer{}}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	valid := sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil}
	for name, x := range map[string]struct {
		subj sigtest.Subject
		opts sigtest.CertOptions
	}{
		"kovalev":        {kovalev, valid},
		"kovalev-rsa":    {kovalev, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil, Key: rsaKey}},
		"kovalev-ed":     {kovalev, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil, Key: edKey}},
		"kovalev-person": {sigtest.Subject{Surname: "Ковалёв", GivenName: "Игорь Сергеевич", INN: "779900010139"}, valid},
		"litvinova":      {litvinova, valid},
		"gusev":          {gusev, valid},
		"smirnova":       {smirnova, valid},
		"orlov":          {orlov, valid},
		"revoked":        {litvinova, valid},
		"no-id":          {sigtest.Subject{CommonName: "Без идентификаторов"}, valid},
		"expired": {litvinova, sigtest.CertOptions{NotBefore: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			NotAfter: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}},
		"expired-after": {litvinova, sigtest.CertOptions{NotBefore: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			NotAfter: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}},
		"no-key-usage":   {litvinova, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil, OmitKeyUsage: true}},
		"cert-sign-only": {litvinova, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil, KeyUsage: x509.KeyUsageCertSign}},
	} {
		s, err := ca.Issue(x.subj, x.opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		p.signers[name] = s
	}
	crl, err := ca.CRL(now.Add(-24*time.Hour), now.Add(30*24*time.Hour),
		sigtest.Revoked{Cert: p.signers["revoked"].Cert, At: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		return nil, err
	}
	if p.crl, err = x509.ParseRevocationList(crl); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *pki) signer(t *testing.T, name string) *sigtest.Signer {
	t.Helper()
	s, ok := p.signers[name]
	if !ok {
		t.Fatalf("no signer %s", name)
	}
	return s
}

func (p *pki) trust() *signature.TrustStore {
	return signature.NewTrustStore([]*x509.Certificate{p.ca.Cert}, nil, []*x509.RevocationList{p.crl})
}

func newVerifier(trust *signature.TrustStore) *signature.Verifier {
	return signature.NewVerifier(trust, signature.DefaultRules(), signature.WithClock(func() time.Time { return now }))
}

// МЧД из сидов mock-api.
var (
	poaActive = domain.PowerOfAttorney{Number: "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37", Status: "ACTIVE",
		ValidFrom: "2026-01-15", ValidUntil: "2027-01-14", PrincipalINN: "7799000012", RepresentativeINN: "779900010333",
		RepresentativeSNILS: "148-562-907 41", RepresentativeName: "Литвинова Марина Олеговна",
		Powers: []string{"LOAN_APPLICATION_SUBMIT", "FINANCIAL_DOCS_SIGN", "BANK_DOCS_RECEIVE"}}
	poaExpired = domain.PowerOfAttorney{Number: "3c9e7d10-55a2-4f61-8b0e-2a7c4f9d6b12", Status: "EXPIRED",
		ValidFrom: "2025-01-10", ValidUntil: "2025-12-31", PrincipalINN: "7799000012", RepresentativeINN: "779900010333",
		Powers: []string{"LOAN_APPLICATION_SUBMIT"}}
	poaRevoked = domain.PowerOfAttorney{Number: "a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945", Status: "REVOKED",
		ValidFrom: "2025-11-03", ValidUntil: "2026-11-02", RevokedAt: "2026-05-12", PrincipalINN: "7799000012",
		RepresentativeINN: "779900010417", Powers: []string{"LOAN_APPLICATION_SUBMIT", "FINANCIAL_DOCS_SIGN"}}
)

// llc — ООО «Северный ветер» с данными ЕГРЮЛ и реестра МЧД, как после сбора.
func llc(poas ...domain.PowerOfAttorney) signature.Applicant {
	return signature.Applicant{INN: "7799000012", Type: domain.CompanyLegalEntity, OGRN: "1217700412345",
		Name: `ООО "СЕВЕРНЫЙ ВЕТЕР"`, HeadsKnown: true, POAsKnown: true, POAs: poas, Source: "сбор col_test",
		Heads: []domain.RegistryHead{{INN: "779900010139", FullName: "КОВАЛЁВ ИГОРЬ СЕРГЕЕВИЧ", Position: "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР"}}}
}

// soleProprietor — ИП Смирнова; данные реестров ИП самому себе не нужны.
func soleProprietor() signature.Applicant {
	return signature.Applicant{INN: "779900010202", Type: domain.CompanySoleProprietor, OGRN: "326774600012345",
		Name: "ИП Смирнова Анна Викторовна", HeadsUnavailable: "сбор данных ещё не завершён", POAsUnavailable: "сбор данных ещё не завершён"}
}

// loader отдаёт заявителя и считает вызовы.
type loader struct {
	a     signature.Applicant
	err   error
	calls int
}

func (l *loader) load(context.Context) (signature.Applicant, error) {
	l.calls++
	return l.a, l.err
}

func file(name string, data []byte) signature.File {
	sum := sha256.Sum256(data)
	return signature.File{Name: name, Data: data, SHA256: hex.EncodeToString(sum[:]), Recorded: hex.EncodeToString(sum[:]),
		Size: int64(len(data))}
}

func sign(t *testing.T, content []byte, s *sigtest.Signer, opts sigtest.SignOptions) []byte {
	t.Helper()
	if opts.SigningTime.IsZero() {
		opts.SigningTime = now.Add(-time.Hour)
	}
	sig, err := sigtest.Sign(content, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

func detached(docType domain.DocumentType, doc, sig []byte) signature.Input {
	s := file("balance.pdf.sig", sig)
	return signature.Input{DocumentType: docType, Container: signature.ContainerDetached, Document: file("balance.pdf", doc),
		Signature: &s}
}

func verify(t *testing.T, v *signature.Verifier, in signature.Input, l *loader) *signature.Report {
	t.Helper()
	r, err := v.Verify(context.Background(), in, l.load)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// summary — статусы этапов одной строкой: целостность формат сертификат личность полномочия → итог.
func summary(r *signature.Report) string {
	s := r.Stages
	return fmt.Sprintf("%s %s %s %s %s → %s", s.Integrity.Status, s.Format.Status, s.Certificate.Status, s.Identity.Status,
		s.Authority.Status, r.Decision)
}

// check — статус проверки с кодом code (первой найденной); "" — такой проверки нет.
func check(r *signature.Report, code string) domain.VerificationStatus {
	for _, st := range []signature.Stage{r.Stages.Integrity, r.Stages.Format, r.Stages.Certificate, r.Stages.Identity,
		r.Stages.Authority, r.Stages.Decision} {
		for _, c := range st.Checks {
			if c.Code == code {
				return c.Status
			}
		}
	}
	return ""
}

func expect(t *testing.T, r *signature.Report, want string) {
	t.Helper()
	if got := summary(r); got != want {
		t.Fatalf("stages = %s, want %s\nreason: %s\n%s", got, want, r.FailureReason, reportJSON(t, r))
	}
}

func reportJSON(t *testing.T, r *signature.Report) string {
	t.Helper()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const allVerified = "VERIFIED VERIFIED VERIFIED VERIFIED VERIFIED → VERIFIED"

// Открепленная подпись проверяется над исходными байтами файла любого формата: PDF, XLSX, XLS, произвольные данные.
func TestVerifyDetachedAnyFileFormat(t *testing.T) {
	p := testPKI(t)
	v := newVerifier(p.trust())
	for name, doc := range map[string][]byte{
		"pdf":    []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\n%%EOF\n"),
		"xlsx":   append([]byte("PK\x03\x04\x14\x00\x06\x00"), []byte("[Content_Types].xml xl/workbook.xml")...),
		"xls":    append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, []byte("OLE2 Workbook")...),
		"binary": {0x00, 0xff, 0x00, 0x10, 0x80, 0x30, 0x82},
	} {
		t.Run(name, func(t *testing.T) {
			l := &loader{a: llc(poaActive, poaExpired, poaRevoked)}
			r := verify(t, v, detached(domain.DocFinancialStatements, doc, sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})), l)
			expect(t, r, allVerified)
			sum := sha256.Sum256(doc)
			if r.Verdict != "VALID_WITH_WARNINGS" || r.Profile != "TEST" || r.TrustMode != "MOCK" || r.Format != "CADES_BES" ||
				r.Policy != "FINANCIAL_STATEMENTS" || !r.PolicyResult.Satisfied || r.DocumentSHA256 != hex.EncodeToString(sum[:]) ||
				r.Authority.Basis != "POWER_OF_ATTORNEY" || r.Authority.PowerOfAttorney.Number != poaActive.Number ||
				r.Signer.INNIndividual != "779900010333" || r.Signer.SNILSMasked != "***-***-907 41" || l.calls != 1 {
				t.Fatalf("report = %s (loader calls %d)", reportJSON(t, r), l.calls)
			}
		})
	}
}

// Директор ООО по ЕГРЮЛ, ИП сам за себя, разные алгоритмы и цепочка через промежуточный УЦ.
func TestVerifySignersAndAlgorithms(t *testing.T) {
	p := testPKI(t)
	inter, err := p.ca.Intermediate("Тестовый промежуточный УЦ (TEST)", certFrom, certUntil)
	if err != nil {
		t.Fatal(err)
	}
	viaInter, err := inter.Issue(kovalev, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil})
	if err != nil {
		t.Fatal(err)
	}
	interCRL, err := inter.CRL(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rootCRL, err := p.ca.CRL(now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	crls := []*x509.RevocationList{mustCRL(t, interCRL), mustCRL(t, rootCRL)}
	doc := []byte("%PDF-1.4 баланс за 2025 год")

	tests := []struct {
		name      string
		signer    *sigtest.Signer
		opts      sigtest.SignOptions
		applicant signature.Applicant
		trust     *signature.TrustStore
		basis     string
		algorithm string
	}{
		{"director by registry, ECDSA", p.signer(t, "kovalev"), sigtest.SignOptions{}, llc(), p.trust(), "REGISTRY_HEAD", "ECDSA / SHA-256"},
		{"director with personal certificate", p.signer(t, "kovalev-person"), sigtest.SignOptions{}, llc(), p.trust(), "REGISTRY_HEAD", "ECDSA / SHA-256"},
		{"RSA", p.signer(t, "kovalev-rsa"), sigtest.SignOptions{}, llc(), p.trust(), "REGISTRY_HEAD", "RSA / SHA-256"},
		{"Ed25519 with SHA-512", p.signer(t, "kovalev-ed"), sigtest.SignOptions{Hash: crypto.SHA512}, llc(), p.trust(), "REGISTRY_HEAD", "Ed25519 / SHA-512"},
		{"ECDSA with SHA-384", p.signer(t, "kovalev"), sigtest.SignOptions{Hash: crypto.SHA384}, llc(), p.trust(), "REGISTRY_HEAD", "ECDSA / SHA-384"},
		{"sole proprietor signs for herself", p.signer(t, "smirnova"), sigtest.SignOptions{}, soleProprietor(), p.trust(), "SOLE_PROPRIETOR", "ECDSA / SHA-256"},
		{"chain through intermediate CA", viaInter, sigtest.SignOptions{ExtraCerts: []*x509.Certificate{inter.Cert}}, llc(),
			signature.NewTrustStore([]*x509.Certificate{p.ca.Cert}, nil, crls), "REGISTRY_HEAD", "ECDSA / SHA-256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(tt.trust), detached(domain.DocFinancialStatements, doc, sign(t, doc, tt.signer, tt.opts)),
				&loader{a: tt.applicant})
			expect(t, r, allVerified)
			if r.Authority.Basis != tt.basis || r.Algorithm != tt.algorithm {
				t.Fatalf("basis %s, algorithm %s", r.Authority.Basis, r.Algorithm)
			}
		})
	}
}

// Документ изменён после подписания: хеш не совпадает — INVALID, полномочия даже не загружаются.
func TestVerifyModifiedDocument(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 выручка 48 200 000")
	sig := sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})
	modified := []byte("%PDF-1.4 выручка 98 200 000")
	l := &loader{a: llc(poaActive)}
	r := verify(t, newVerifier(p.trust()), detached(domain.DocFinancialStatements, modified, sig), l)
	expect(t, r, "INVALID VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INVALID")
	if check(r, "INTEGRITY") != domain.VerificationInvalid || check(r, "SIGNATURE_VALUE") != domain.VerificationVerified ||
		!strings.HasPrefix(r.FailureReason, "целостность: ") || r.Verdict != "INVALID" || r.PolicyResult.Satisfied || l.calls != 0 {
		t.Fatalf("report = %s", reportJSON(t, r))
	}
}

// Повреждённая подпись, подпись к другому документу, обрезанный контейнер, чужой сертификат в атрибуте.
func TestVerifyDamagedOrForeignSignature(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	litv := p.signer(t, "litvinova")
	good := sign(t, doc, litv, sigtest.SignOptions{})
	tests := []struct {
		name string
		in   signature.Input
		want string
		code string
	}{
		{"corrupted signature value", detached(domain.DocFinancialStatements, doc, sign(t, doc, litv, sigtest.SignOptions{CorruptSignature: true})),
			"INVALID VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INVALID", "SIGNATURE_VALUE"},
		{"signature of another document", detached(domain.DocFinancialStatements, doc, sign(t, []byte("другой файл"), litv, sigtest.SignOptions{})),
			"INVALID VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INVALID", "INTEGRITY"},
		{"truncated container", detached(domain.DocFinancialStatements, doc, good[:len(good)/2]),
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "FORMAT_PARSED"},
		{"signing-certificate points to another certificate", detached(domain.DocFinancialStatements, doc,
			sign(t, doc, litv, sigtest.SignOptions{WrongCertHash: true})),
			"VERIFIED INVALID VERIFIED INDETERMINATE INDETERMINATE → INVALID", "SIGNING_CERTIFICATE_BINDING"},
		{"attached container holds another document", detached(domain.DocFinancialStatements, doc,
			sign(t, []byte("другой файл"), litv, sigtest.SignOptions{Attached: true})),
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "SIGNED_DATA_COVERAGE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(p.trust()), tt.in, &loader{a: llc(poaActive)})
			expect(t, r, tt.want)
			if check(r, tt.code) != domain.VerificationInvalid || r.Verdict != "INVALID" {
				t.Fatalf("%s = %s\n%s", tt.code, check(r, tt.code), reportJSON(t, r))
			}
		})
	}
}

// Нет файла документа или подписи — INDETERMINATE: проверять нечего, но это не отказ подписи.
// Файл в хранилище не совпадает с загруженным — INVALID.
func TestVerifyMissingOrChangedFiles(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	sig := sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})
	missing := func(f signature.File) signature.File {
		f.Data, f.SHA256, f.Unavailable = nil, "", "не найден в хранилище"
		return f
	}

	in := detached(domain.DocFinancialStatements, doc, sig)
	in.Document = missing(in.Document)
	r := verify(t, newVerifier(p.trust()), in, &loader{})
	expect(t, r, "INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE")
	if check(r, "FILE_AVAILABLE") != domain.VerificationIndeterminate || !strings.Contains(r.FailureReason, "не найден в хранилище") {
		t.Fatalf("missing document: %s", reportJSON(t, r))
	}

	in = detached(domain.DocFinancialStatements, doc, sig)
	*in.Signature = missing(*in.Signature)
	expect(t, verify(t, newVerifier(p.trust()), in, &loader{}),
		"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE")

	in = detached(domain.DocFinancialStatements, doc, sig)
	in.Document.Recorded = strings.Repeat("0", 64) // при загрузке хеш был другим
	r = verify(t, newVerifier(p.trust()), in, &loader{a: llc(poaActive)})
	if check(r, "STORED_FILE_UNCHANGED") != domain.VerificationInvalid || r.Decision != domain.VerificationInvalid {
		t.Fatalf("changed in storage: %s", reportJSON(t, r))
	}
}

// Неподдерживаемые контейнеры и алгоритмы — INDETERMINATE и никогда VERIFIED.
func TestVerifyUnsupported(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	litv := p.signer(t, "litvinova")
	ber, err := sigtest.ToBER(sign(t, doc, litv, sigtest.SignOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	opensslBER, err := os.ReadFile("testdata/openssl/report.xls.ber.sig")
	if err != nil {
		t.Fatal(err)
	}
	signedXML := []byte(`<?xml version="1.0"?><Доверенность><ds:Signature xmlns:ds="http://www.w3.org/2000/09/xmldsig#">` +
		`<ds:SignedInfo/></ds:Signature></Доверенность>`)
	embedded := func(c signature.Container, data []byte) signature.Input {
		return signature.Input{DocumentType: domain.DocPowerOfAttorney, Container: c, Document: file("doc", data)}
	}
	attached := func(sig []byte) signature.Input {
		s := file("contract.sig", sig)
		return signature.Input{DocumentType: domain.DocOther, Container: signature.ContainerAttached, Signature: &s}
	}

	for _, tt := range []struct {
		name string
		in   signature.Input
		code string
	}{
		{"GOST R 34.10-2012 / 34.11-2012", detached(domain.DocFinancialStatements, doc, sign(t, doc, litv, sigtest.SignOptions{
			DigestOID: sigtest.OIDGOST3411_2012_256, SignatureOID: sigtest.OIDGOST3410_2012_256})), "ALGORITHM_SUPPORTED"},
		{"SHA-1", detached(domain.DocFinancialStatements, doc, sign(t, doc, litv, sigtest.SignOptions{DigestOID: sigtest.OIDSHA1})),
			"ALGORITHM_SUPPORTED"},
		{"PKCS#7 without signed attributes", detached(domain.DocFinancialStatements, doc,
			sign(t, doc, litv, sigtest.SignOptions{NoSignedAttrs: true})), "SIGNED_ATTRIBUTES"},
		{"two signatures in one container", detached(domain.DocFinancialStatements, doc, sign(t, doc, litv, sigtest.SignOptions{Signers: 2})),
			"SINGLE_SIGNER"},
		{"signed content is not data", detached(domain.DocFinancialStatements, doc,
			sign(t, doc, litv, sigtest.SignOptions{ContentType: sigtest.OIDEnvelopedData})), "CONTENT_TYPE"},
		{"EnvelopedData instead of signature", detached(domain.DocFinancialStatements, doc,
			sigtest.Wrap(sigtest.OIDEnvelopedData, []byte{0x02, 0x01, 0x00})), "FORMAT_PARSED"},
		{"BER with indefinite length", detached(domain.DocFinancialStatements, doc, ber), "FORMAT_PARSED"},
		{"BER from openssl -stream", detached(domain.DocFinancialStatements, doc, opensslBER), "FORMAT_PARSED"},
		{"not a CMS container", detached(domain.DocFinancialStatements, doc,
			[]byte("demo detached signature: not a real CMS container")), "FORMAT_PARSED"},
		{"XML with XMLDSig", embedded(signature.ContainerXML, signedXML), "FORMAT_PARSED"},
		{"detached signature uploaded without document", attached(sign(t, doc, litv, sigtest.SignOptions{})), "SIGNED_DATA_COVERAGE"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(p.trust()), tt.in, &loader{a: llc(poaActive)})
			if r.Decision != domain.VerificationIndeterminate || r.Stages.Format.Status != domain.VerificationIndeterminate ||
				check(r, tt.code) != domain.VerificationIndeterminate || r.Verdict != "INDETERMINATE" {
				t.Fatalf("%s: %s, %s = %s\n%s", tt.name, summary(r), tt.code, check(r, tt.code), reportJSON(t, r))
			}
		})
	}
}

// Сертификат: недоверенный, самоподписанный, просроченный, отозванный, без данных об отзыве, без назначения ключа.
func TestVerifyCertificate(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	other, err := sigtest.NewCA("Посторонний УЦ", certFrom, certUntil)
	if err != nil {
		t.Fatal(err)
	}
	untrusted, err := other.Issue(litvinova, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil})
	if err != nil {
		t.Fatal(err)
	}
	selfSigned, err := sigtest.SelfSigned(litvinova, certFrom, certUntil)
	if err != nil {
		t.Fatal(err)
	}
	staleCRL, err := p.ca.CRL(now.Add(-60*24*time.Hour), now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	noCRL := signature.NewTrustStore([]*x509.Certificate{p.ca.Cert}, nil, nil)
	stale := signature.NewTrustStore([]*x509.Certificate{p.ca.Cert}, nil, []*x509.RevocationList{mustCRL(t, staleCRL)})
	noRoots := signature.NewTrustStore(nil, nil, nil)
	signedEarlier := sigtest.SignOptions{SigningTime: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}

	for _, tt := range []struct {
		name   string
		trust  *signature.TrustStore
		signer *sigtest.Signer
		opts   sigtest.SignOptions
		want   string
		code   string
		status domain.VerificationStatus
	}{
		{"issued by untrusted CA", p.trust(), untrusted, sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "CHAIN_TO_TRUSTED_ROOT", domain.VerificationInvalid},
		{"self-signed", p.trust(), selfSigned, sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "CHAIN_TO_TRUSTED_ROOT", domain.VerificationInvalid},
		{"expired before signing", p.trust(), p.signer(t, "expired"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "CERT_VALID_AT_SIGNING_TIME", domain.VerificationInvalid},
		{"expired after signing, no trusted time", p.trust(), p.signer(t, "expired-after"), signedEarlier,
			"VERIFIED VERIFIED INDETERMINATE VERIFIED VERIFIED → INDETERMINATE", "CERT_VALID_AT_SIGNING_TIME", domain.VerificationIndeterminate},
		{"revoked in CRL", p.trust(), p.signer(t, "revoked"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "NOT_REVOKED", domain.VerificationInvalid},
		{"no CRL from issuer", noCRL, p.signer(t, "litvinova"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INDETERMINATE VERIFIED VERIFIED → INDETERMINATE", "NOT_REVOKED", domain.VerificationIndeterminate},
		{"stale CRL", stale, p.signer(t, "litvinova"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INDETERMINATE VERIFIED VERIFIED → INDETERMINATE", "NOT_REVOKED", domain.VerificationIndeterminate},
		{"no trusted roots configured", noRoots, p.signer(t, "litvinova"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INDETERMINATE VERIFIED VERIFIED → INDETERMINATE", "CHAIN_TO_TRUSTED_ROOT", domain.VerificationIndeterminate},
		{"no keyUsage", p.trust(), p.signer(t, "no-key-usage"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "KEY_USAGE", domain.VerificationInvalid},
		{"key not for signing", p.trust(), p.signer(t, "cert-sign-only"), sigtest.SignOptions{},
			"VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "KEY_USAGE", domain.VerificationInvalid},
		{"no certificate in container", p.trust(), p.signer(t, "litvinova"), sigtest.SignOptions{NoCertificates: true},
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "SIGNER_CERTIFICATE_FOUND",
			domain.VerificationIndeterminate},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(tt.trust), detached(domain.DocFinancialStatements, doc, sign(t, doc, tt.signer, tt.opts)),
				&loader{a: llc(poaActive)})
			expect(t, r, tt.want)
			if got := check(r, tt.code); got != tt.status {
				t.Fatalf("%s = %s, want %s\n%s", tt.code, got, tt.status, reportJSON(t, r))
			}
			// ни один из этих случаев не выдаётся за проверенную подпись
			if r.Decision == domain.VerificationVerified || r.PolicyResult.Satisfied {
				t.Fatalf("decision = %s", r.Decision)
			}
		})
	}
}

// Подписант не совпадает с ожидаемым заявителем: чужая организация, чужой ИП, сертификат без идентификаторов.
func TestVerifyIdentity(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	for _, tt := range []struct {
		name      string
		signer    string
		applicant signature.Applicant
		want      string
	}{
		{"director of another company", "orlov", llc(poaActive), "VERIFIED VERIFIED VERIFIED INVALID INDETERMINATE → INVALID"},
		{"organization certificate for a sole proprietor", "kovalev", soleProprietor(), "VERIFIED VERIFIED VERIFIED INVALID INDETERMINATE → INVALID"},
		{"another sole proprietor", "smirnova", func() signature.Applicant {
			a := soleProprietor()
			a.INN, a.OGRN = "779900099018", "326774600099999"
			return a
		}(), "VERIFIED VERIFIED VERIFIED INVALID INDETERMINATE → INVALID"},
		{"certificate without INN and SNILS", "no-id", llc(poaActive), "VERIFIED VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INDETERMINATE"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(p.trust()), detached(domain.DocFinancialStatements, doc, sign(t, doc, p.signer(t, tt.signer),
				sigtest.SignOptions{})), &loader{a: tt.applicant})
			expect(t, r, tt.want)
			if !strings.HasPrefix(r.FailureReason, "личность: ") {
				t.Fatalf("failure reason = %q", r.FailureReason)
			}
		})
	}
}

// Полномочия: разные правила для директора ООО, ИП и представителя по МЧД; без данных о полномочиях —
// INDETERMINATE, а криптографические этапы остаются VERIFIED.
func TestVerifyAuthority(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	noPower := poaActive
	noPower.Number, noPower.Powers = "11111111-0000-0000-0000-000000000001", []string{"BANK_DOCS_RECEIVE"}
	pastOnly := poaActive
	pastOnly.Number, pastOnly.ValidFrom, pastOnly.ValidUntil = "11111111-0000-0000-0000-000000000002", "2026-01-01", "2026-06-30"
	unknownRegistries := llc()
	unknownRegistries.HeadsKnown, unknownRegistries.POAsKnown = false, false
	unknownRegistries.HeadsUnavailable, unknownRegistries.POAsUnavailable = "сбор данных ещё не завершён", "сбор данных ещё не завершён"
	scoped := llc(poaActive)
	scoped.POAScopeINN = "779900010333"
	namesOnly := llc(poaActive)
	namesOnly.Heads = []domain.RegistryHead{{FullName: "Ковалев Игорь Сергеевич", Position: "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР"}}

	for _, tt := range []struct {
		name      string
		docType   domain.DocumentType
		signer    string
		opts      sigtest.SignOptions
		applicant signature.Applicant
		want      domain.VerificationStatus // этап полномочий; остальные этапы VERIFIED
		basis     string
	}{
		{"representative with valid MCHD", domain.DocFinancialStatements, "litvinova", sigtest.SignOptions{}, llc(poaActive, poaExpired),
			domain.VerificationVerified, "POWER_OF_ATTORNEY"},
		{"representative with expired MCHD only", domain.DocFinancialStatements, "litvinova", sigtest.SignOptions{}, llc(poaExpired),
			domain.VerificationInvalid, "POWER_OF_ATTORNEY"},
		{"representative with revoked MCHD", domain.DocFinancialStatements, "gusev", sigtest.SignOptions{}, llc(poaActive, poaRevoked),
			domain.VerificationInvalid, "POWER_OF_ATTORNEY"},
		{"representative without MCHD", domain.DocFinancialStatements, "gusev", sigtest.SignOptions{}, llc(poaActive),
			domain.VerificationInvalid, "NONE"},
		{"MCHD without the power for this document", domain.DocFinancialStatements, "litvinova", sigtest.SignOptions{}, llc(noPower),
			domain.VerificationInvalid, "POWER_OF_ATTORNEY"},
		{"loan contract by representative", domain.DocLoanAgreementDraft, "litvinova", sigtest.SignOptions{}, llc(poaActive),
			domain.VerificationInvalid, "NONE"},
		{"loan contract by director", domain.DocLoanAgreementDraft, "kovalev", sigtest.SignOptions{}, llc(),
			domain.VerificationVerified, "REGISTRY_HEAD"},
		{"MCHD valid only at claimed signing time", domain.DocFinancialStatements, "litvinova",
			sigtest.SignOptions{SigningTime: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}, llc(pastOnly),
			domain.VerificationIndeterminate, "POWER_OF_ATTORNEY"},
		{"no registry data", domain.DocFinancialStatements, "litvinova", sigtest.SignOptions{}, unknownRegistries,
			domain.VerificationIndeterminate, "NONE"},
		{"MCHD searched for another representative", domain.DocFinancialStatements, "gusev", sigtest.SignOptions{}, scoped,
			domain.VerificationIndeterminate, "NONE"},
		{"head matches only by name", domain.DocFinancialStatements, "kovalev-person", sigtest.SignOptions{}, namesOnly,
			domain.VerificationIndeterminate, "NONE"},
		{"sole proprietor without registry data", domain.DocFinancialStatements, "smirnova", sigtest.SignOptions{}, soleProprietor(),
			domain.VerificationVerified, "SOLE_PROPRIETOR"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(p.trust()), detached(tt.docType, doc, sign(t, doc, p.signer(t, tt.signer), tt.opts)),
				&loader{a: tt.applicant})
			decision := tt.want
			expect(t, r, fmt.Sprintf("VERIFIED VERIFIED VERIFIED VERIFIED %s → %s", tt.want, decision))
			if r.Authority == nil || r.Authority.Basis != tt.basis {
				t.Fatalf("authority = %+v", r.Authority)
			}
			if tt.want != domain.VerificationVerified && !strings.HasPrefix(r.FailureReason, "полномочия: ") {
				t.Fatalf("failure reason = %q", r.FailureReason)
			}
		})
	}
}

// Документ третьей стороны (выписка банка): личность и полномочия по правилам не нужны.
func TestVerifyThirdPartyDocument(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 выписка по счёту")
	l := &loader{}
	r := verify(t, newVerifier(p.trust()), detached(domain.DocBankStatement, doc, sign(t, doc, p.signer(t, "orlov"), sigtest.SignOptions{})), l)
	expect(t, r, "VERIFIED VERIFIED VERIFIED NOT_REQUIRED NOT_REQUIRED → VERIFIED")
	if l.calls != 0 || r.Signer == nil || r.Signer.Organization.INN != "7799000020" {
		t.Fatalf("loader calls %d, report %s", l.calls, reportJSON(t, r))
	}
}

// Присоединённая подпись (документ внутри контейнера), PEM и base64 вместо DER.
func TestVerifyAttachedAndEncodings(t *testing.T) {
	p := testPKI(t)
	doc := []byte("договор с покупателем")
	litv := p.signer(t, "litvinova")
	container := sign(t, doc, litv, sigtest.SignOptions{Attached: true})
	s := file("contract.sig", container)
	r := verify(t, newVerifier(p.trust()), signature.Input{DocumentType: domain.DocOther, Container: signature.ContainerAttached,
		Signature: &s}, &loader{a: llc(poaActive)})
	expect(t, r, allVerified)
	if r.DocumentSHA256 != s.SHA256 || r.FileName != "contract.sig" {
		t.Fatalf("attached: document %s %s", r.FileName, r.DocumentSHA256)
	}

	der := sign(t, doc, litv, sigtest.SignOptions{})
	for encoding, sig := range map[string][]byte{
		"PEM":    pem.EncodeToMemory(&pem.Block{Type: "PKCS7", Bytes: der}),
		"BASE64": []byte(base64.StdEncoding.EncodeToString(der) + "\n"),
	} {
		r := verify(t, newVerifier(p.trust()), detached(domain.DocOther, doc, sig), &loader{a: llc(poaActive)})
		expect(t, r, allVerified)
		if r.Encoding != encoding {
			t.Fatalf("encoding = %s, want %s", r.Encoding, encoding)
		}
	}
}

// Подписи, созданные OpenSSL (openssl cms -sign -cades): независимая реализация CMS проходит проверку.
// Фикстуры — testdata/openssl: корень, CRL, файл и подписи, без закрытых ключей.
func TestVerifyOpenSSLSignatures(t *testing.T) {
	trust, err := signature.LoadTrustStore("testdata/openssl")
	if err != nil {
		t.Fatal(err)
	}
	if roots, inter, crls := trust.Summary(); roots != 1 || inter != 0 || crls != 1 {
		t.Fatalf("trust store: %d roots, %d intermediates, %d CRLs", roots, inter, crls)
	}
	v := signature.NewVerifier(trust, signature.DefaultRules(),
		signature.WithClock(func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) }))
	doc, err := os.ReadFile("testdata/openssl/report.xls")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ file, algorithm, encoding string }{
		{"report.xls.rsa.sig", "RSA / SHA-256", "DER"},
		{"report.xls.pss.sig", "RSA-PSS / SHA-256", "DER"},
		{"report.xls.ec.p7s", "ECDSA / SHA-384", "PEM"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			sig, err := os.ReadFile(filepath.Join("testdata/openssl", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			r := verify(t, v, detached(domain.DocBankStatement, doc, sig), &loader{})
			expect(t, r, "VERIFIED VERIFIED VERIFIED NOT_REQUIRED NOT_REQUIRED → VERIFIED")
			if r.Algorithm != tt.algorithm || r.Encoding != tt.encoding || r.Format != "CADES_BES" || r.Signer.INNIndividual != "779900099018" {
				t.Fatalf("report = %s", reportJSON(t, r))
			}
		})
	}
	attached, err := os.ReadFile("testdata/openssl/report.attached.p7s")
	if err != nil {
		t.Fatal(err)
	}
	s := file("report.attached.p7s", attached)
	r := verify(t, v, signature.Input{DocumentType: domain.DocBankStatement, Container: signature.ContainerAttached, Signature: &s}, &loader{})
	expect(t, r, "VERIFIED VERIFIED VERIFIED NOT_REQUIRED NOT_REQUIRED → VERIFIED")
}

// Сбой загрузки данных о заявителе — ошибка Verify: проверку надо повторить, а не писать отчёт.
func TestVerifyLoaderError(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	boom := errors.New("db down")
	_, err := newVerifier(p.trust()).Verify(context.Background(),
		detached(domain.DocFinancialStatements, doc, sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})),
		(&loader{err: boom}).load)
	if !errors.Is(err, boom) {
		t.Fatalf("want loader error, got %v", err)
	}
}

// В отчёте нет содержимого документа: только хеши, сертификат и результаты проверок.
func TestReportHasNoDocumentContent(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 СЕКРЕТНАЯ-ВЫРУЧКА-48200000")
	r := verify(t, newVerifier(p.trust()), detached(domain.DocFinancialStatements, doc,
		sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})), &loader{a: llc(poaActive)})
	if js := reportJSON(t, r); strings.Contains(js, "СЕКРЕТНАЯ") || strings.Contains(js, base64.StdEncoding.EncodeToString(doc)) ||
		!strings.Contains(js, r.DocumentSHA256) || !strings.Contains(js, "не подтверждает достоверность цифр") {
		t.Fatalf("report leaks content or misses notes:\n%s", js)
	}
}

func TestEmbeddedSignature(t *testing.T) {
	for name, tt := range map[string]struct {
		data []byte
		want signature.Container
	}{
		"plain PDF":       {[]byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj"), ""},
		"signed PDF":      {[]byte("%PDF-1.7\n<< /Type /Sig /ByteRange [0 1 2 3] /Contents <00> >>"), signature.ContainerPDF},
		"plain XML":       {[]byte(`<?xml version="1.0"?><root/>`), ""},
		"XMLDSig":         {[]byte(`<root><Signature xmlns="http://www.w3.org/2000/09/xmldsig#"/></root>`), signature.ContainerXML},
		"XLSX is not PDF": {[]byte("PK\x03\x04 /ByteRange /Contents"), ""},
	} {
		if got := signature.EmbeddedSignature(tt.data); got != tt.want {
			t.Errorf("%s: %q, want %q", name, got, tt.want)
		}
	}
}

func TestRules(t *testing.T) {
	rules := signature.DefaultRules()
	if err := rules.Validate(); err != nil {
		t.Fatal(err)
	}
	if r := rules.For(domain.DocFinancialStatements); !r.Authority || r.POAPower != domain.POAPowerFinancialDocs || r.Policy != "FINANCIAL_STATEMENTS" {
		t.Fatalf("financial statements rule = %+v", r)
	}
	if r := rules.For(domain.DocCharter); r != rules.Default {
		t.Fatalf("charter rule = %+v", r)
	}

	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, fmt.Sprintf("rules-%d.json", time.Now().UnixNano()))
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	loaded, err := signature.LoadRules(write(`{"version": "bank-1", "default": {"policy": "GENERIC"},
		"documents": {"TAX_RETURN": {"policy": "FINANCIAL_STATEMENTS", "identity": true, "authority": true, "poa_power": "FINANCIAL_DOCS_SIGN"}}}`))
	if err != nil || loaded.Version != "bank-1" || !loaded.For(domain.DocTaxReturn).Authority || loaded.For(domain.DocOther).Identity {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	for _, bad := range []string{
		`{"version": "x", "default": {"policy": "STRICT"}}`,
		`{"version": "x", "default": {"policy": "GENERIC", "authority": true}}`,
		`{"version": "x", "default": {"policy": "GENERIC"}, "documents": {"PASSPORT": {"policy": "GENERIC"}}}`,
		`{"default": {"policy": "GENERIC"}}`,
		`{"version":`,
	} {
		if _, err := signature.LoadRules(write(bad)); err == nil {
			t.Errorf("rules %s: want error", bad)
		}
	}
}

func TestLoadTrustStoreRejectsLeafCertificates(t *testing.T) {
	p := testPKI(t)
	dir := t.TempDir()
	leaf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.signer(t, "litvinova").Cert.Raw})
	if err := os.WriteFile(filepath.Join(dir, "leaf.pem"), leaf, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := signature.LoadTrustStore(dir); err == nil {
		t.Fatal("a signer certificate must not become a trust anchor")
	}
}

func mustCRL(t *testing.T, der []byte) *x509.RevocationList {
	t.Helper()
	crl, err := x509.ParseRevocationList(der)
	if err != nil {
		t.Fatal(err)
	}
	return crl
}

// Имя субъекта в отчёте читается по-русски, СНИЛС в нём замаскирован.
func TestReportSubjectMasksSNILS(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	r := verify(t, newVerifier(p.trust()), detached(domain.DocFinancialStatements, doc,
		sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})), &loader{a: llc(poaActive)})
	js := reportJSON(t, r)
	if r.Certificate.Subject != "CN=Литвинова Марина Олеговна, SN=Литвинова, G=Марина Олеговна, T=Главный бухгалтер, C=RU, "+
		"ИНН=779900010333, СНИЛС=***-***-907 41" || strings.Contains(js, "14856290741") || strings.Contains(js, "3134383536") {
		t.Fatalf("subject = %q\n%s", r.Certificate.Subject, js)
	}
}

// Внутренняя ошибка проверки (паника) — отчёт INDETERMINATE с пометкой, а не падение worker'а.
func TestVerifyInternalError(t *testing.T) {
	p := testPKI(t)
	doc := []byte("%PDF-1.4 баланс")
	r, err := newVerifier(p.trust()).Verify(context.Background(),
		detached(domain.DocFinancialStatements, doc, sign(t, doc, p.signer(t, "litvinova"), sigtest.SignOptions{})),
		func(context.Context) (signature.Applicant, error) { panic("boom") })
	if err != nil {
		t.Fatal(err)
	}
	expect(t, r, "INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE")
	if !r.InternalError || r.FailedCheck != "INTERNAL_ERROR" || r.Verdict != "INDETERMINATE" {
		t.Fatalf("report = %s", reportJSON(t, r))
	}
}

// Демо-файлы (examples/files, tools/sigdemo) проверяются так, как описано в signature-demo/README.md.
// Время проверки — сразу после выпуска CRL демо-УЦ: тест не зависит от того, когда файлы перевыпустили.
func TestDemoFiles(t *testing.T) {
	trust, err := signature.LoadTrustStore("../../testdata/signature/trust")
	if err != nil {
		t.Fatal(err)
	}
	crlPEM, err := os.ReadFile("../../testdata/signature/trust/root.crl")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(crlPEM)
	crl := mustCRL(t, block.Bytes)
	v := signature.NewVerifier(trust, signature.DefaultRules(),
		signature.WithClock(func() time.Time { return crl.ThisUpdate.Add(2 * time.Hour) }))
	for _, tt := range []struct{ file, want string }{
		{"balance.pdf", allVerified},
		{"signature-demo/balance-director.pdf", allVerified},
		{"signature-demo/balance-tampered.pdf", "INVALID VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INVALID"},
		{"signature-demo/balance-revoked.pdf", "VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID"},
		{"signature-demo/balance-other-company.pdf", "VERIFIED VERIFIED VERIFIED INVALID INDETERMINATE → INVALID"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			doc, err := os.ReadFile(filepath.Join("../../examples/files", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			sig, err := os.ReadFile(filepath.Join("../../examples/files", tt.file+".sig"))
			if err != nil {
				t.Fatal(err)
			}
			expect(t, verify(t, v, detached(domain.DocFinancialStatements, doc, sig), &loader{a: llc(poaActive, poaExpired, poaRevoked)}), tt.want)
		})
	}
	// PDF с подписью внутри — без отдельного файла подписи
	for _, tt := range []struct{ file, want string }{
		{"signature-demo/balance-signed-inside.pdf", allVerified},
		{"signature-demo/balance-signed-inside-updated.pdf",
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			doc, err := os.ReadFile(filepath.Join("../../examples/files", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if c := signature.EmbeddedSignature(doc); c != signature.ContainerPDF {
				t.Fatalf("embedded signature = %q", c)
			}
			in := signature.Input{DocumentType: domain.DocFinancialStatements, Container: signature.ContainerPDF, Document: file(tt.file, doc)}
			expect(t, verify(t, v, in, &loader{a: llc(poaActive)}), tt.want)
		})
	}
}

// Подпись внутри PDF: проверяется тем же кодом, что открепленная, если она одна и /ByteRange
// покрывает весь файл, кроме своего /Contents. Иначе — INVALID или INDETERMINATE, но не VERIFIED.
func TestVerifyPDFSignature(t *testing.T) {
	p := testPKI(t)
	litv := p.signer(t, "litvinova")
	signPDF := func(t *testing.T, s *sigtest.Signer, opts sigtest.PDFOptions) []byte {
		t.Helper()
		if opts.SigningTime.IsZero() {
			opts.SigningTime = now.Add(-time.Hour)
		}
		pdf, err := sigtest.SignPDF("Balance 2025", s, opts)
		if err != nil {
			t.Fatal(err)
		}
		return pdf
	}
	pdfInput := func(docType domain.DocumentType, data []byte) signature.Input {
		return signature.Input{DocumentType: docType, Container: signature.ContainerPDF, Document: file("balance.pdf", data)}
	}
	valid := signPDF(t, litv, sigtest.PDFOptions{})

	t.Run("valid", func(t *testing.T) {
		r := verify(t, newVerifier(p.trust()), pdfInput(domain.DocFinancialStatements, valid), &loader{a: llc(poaActive)})
		expect(t, r, allVerified)
		if r.Format != "PADES" || r.Container != signature.ContainerPDF || r.PDF == nil || r.PDF.ByteRange[0] != 0 ||
			r.PDF.ByteRange[2]+r.PDF.ByteRange[3] != len(valid) || r.PDF.SubFilter != "ETSI.CAdES.detached" ||
			r.SignatureSHA256 == "" || r.SigningTime.Value == nil || !r.SigningTime.Value.Equal(now.Add(-time.Hour).Truncate(time.Second)) ||
			check(r, "DOCUMENT_NOT_MODIFIED") != domain.VerificationVerified || r.Authority.Basis != "POWER_OF_ATTORNEY" {
			t.Fatalf("report = %s", reportJSON(t, r))
		}
	})

	t.Run("director, adbe.pkcs7.detached, RSA", func(t *testing.T) {
		pdf := signPDF(t, p.signer(t, "kovalev-rsa"), sigtest.PDFOptions{SubFilter: "adbe.pkcs7.detached"})
		r := verify(t, newVerifier(p.trust()), pdfInput(domain.DocLoanAgreementDraft, pdf), &loader{a: llc()})
		expect(t, r, allVerified)
		if r.Authority.Basis != "REGISTRY_HEAD" || r.Signer.Kind != "LEGAL_ENTITY_HEAD" {
			t.Fatalf("report = %s", reportJSON(t, r))
		}
	})

	// время подписи в PAdES — /M словаря подписи: сертификат истёк уже после него — INDETERMINATE, не INVALID
	t.Run("signing time from /M", func(t *testing.T) {
		pdf := signPDF(t, p.signer(t, "expired-after"), sigtest.PDFOptions{SigningTime: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)})
		r := verify(t, newVerifier(p.trust()), pdfInput(domain.DocFinancialStatements, pdf), &loader{a: llc(poaActive)})
		expect(t, r, "VERIFIED VERIFIED INDETERMINATE VERIFIED VERIFIED → INDETERMINATE")
	})

	modify := func(data []byte, from, to string) []byte {
		out := bytes.Replace(append([]byte{}, data...), []byte(from), []byte(to), 1)
		if bytes.Equal(out, data) {
			t.Fatalf("%q is not in the PDF", from)
		}
		return out
	}
	open := bytes.Index(valid, []byte("/Contents <")) + len("/Contents ") // '<' значения /Contents
	end := open + bytes.IndexByte(valid[open:], '>')
	junk := append([]byte{}, valid...)
	junk[end-2] = 'f' // выравнивающий ноль в /Contents: не подписан, но проверяется
	noContentsKey := modify(valid, "/Contents <", "/Content2 <")
	shifted := modify(valid, "/ByteRange [0 ", "/ByteRange [1 ")
	notCovered := append([]byte{}, valid...) // та же длина, но промежуток /ByteRange начинается не с '<'
	notCovered[open-1], notCovered[open] = '<', ' '
	twoSignatures := append(append([]byte{}, valid...), []byte("\n6 0 obj << /Type /Sig /ByteRange [0 1 2 3] >> endobj\n%%EOF\n")...)
	unsigned := signPDF(t, litv, sigtest.PDFOptions{NoSignature: true})

	for _, tt := range []struct {
		name string
		pdf  []byte
		want string
		code string
	}{
		{"document changed after signing", modify(valid, "(Balance 2025)", "(Balance 2026)"),
			"INVALID VERIFIED VERIFIED INDETERMINATE INDETERMINATE → INVALID", "INTEGRITY"},
		{"incremental update after signing", append(append([]byte{}, valid...), []byte("\n% update\n%%EOF\n")...),
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "DOCUMENT_NOT_MODIFIED"},
		{"two signatures", twoSignatures,
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "SINGLE_SIGNER"},
		{"data hidden in /Contents padding", junk,
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "SIGNED_DATA_COVERAGE"},
		{"gap is not /Contents", noContentsKey,
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "SIGNED_DATA_COVERAGE"},
		{"/ByteRange does not start at 0", shifted,
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "SIGNED_DATA_COVERAGE"},
		{"gap does not match /ByteRange", notCovered,
			"INDETERMINATE INVALID INDETERMINATE INDETERMINATE INDETERMINATE → INVALID", "SIGNED_DATA_COVERAGE"},
		{"signature field is not signed", unsigned,
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "PDF_SIGNATURE"},
		{"legacy adbe.pkcs7.sha1", signPDF(t, litv, sigtest.PDFOptions{SubFilter: "adbe.pkcs7.sha1"}),
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "PDF_SIGNATURE"},
		{"GOST inside PDF", signPDF(t, litv, sigtest.PDFOptions{Sign: sigtest.SignOptions{DigestOID: sigtest.OIDGOST3411_2012_256,
			SignatureOID: sigtest.OIDGOST3410_2012_256}}),
			"INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE INDETERMINATE → INDETERMINATE", "ALGORITHM_SUPPORTED"},
		{"untrusted signer", func() []byte {
			other, err := sigtest.NewCA("Посторонний УЦ", certFrom, certUntil)
			if err != nil {
				t.Fatal(err)
			}
			s, err := other.Issue(litvinova, sigtest.CertOptions{NotBefore: certFrom, NotAfter: certUntil})
			if err != nil {
				t.Fatal(err)
			}
			return signPDF(t, s, sigtest.PDFOptions{})
		}(), "VERIFIED VERIFIED INVALID INDETERMINATE INDETERMINATE → INVALID", "CHAIN_TO_TRUSTED_ROOT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := verify(t, newVerifier(p.trust()), pdfInput(domain.DocFinancialStatements, tt.pdf), &loader{a: llc(poaActive)})
			expect(t, r, tt.want)
			if r.FailedCheck != tt.code || r.Format != "PADES" {
				t.Fatalf("failed check = %s, want %s (format %s): %s", r.FailedCheck, tt.code, r.Format, r.FailureReason)
			}
		})
	}
}

func TestParsePDFDateViaReport(t *testing.T) {
	p := testPKI(t)
	for _, tt := range []struct {
		m    time.Time
		want time.Time
	}{
		{time.Date(2026, 10, 22, 8, 30, 15, 0, time.UTC), time.Date(2026, 10, 22, 8, 30, 15, 0, time.UTC)},
	} {
		pdf, err := sigtest.SignPDF("x", p.signer(t, "litvinova"), sigtest.PDFOptions{SigningTime: tt.m})
		if err != nil {
			t.Fatal(err)
		}
		r := verify(t, newVerifier(p.trust()), signature.Input{DocumentType: domain.DocOther, Container: signature.ContainerPDF,
			Document: file("x.pdf", pdf)}, &loader{a: llc(poaActive)})
		if r.SigningTime == nil || r.SigningTime.Value == nil || !r.SigningTime.Value.Equal(tt.want) {
			t.Fatalf("signing time = %+v", r.SigningTime)
		}
	}
}
