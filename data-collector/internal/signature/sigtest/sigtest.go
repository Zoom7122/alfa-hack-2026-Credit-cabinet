// Package sigtest — тестовый УЦ, сертификаты, списки отзыва и подписи CMS/CAdES для тестов
// и демо-файлов профиля TEST. Ключи генерируются в памяти и никуда не сохраняются; такие
// сертификаты — не КЭП и годятся только для проверки по тестовым корням.
package sigtest

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"
)

var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidSigningCertV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}

	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}

	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}

	// OID алгоритмов ГОСТ — для проверки, что такие подписи не считаются проверенными
	OIDGOST3411_2012_256 = asn1.ObjectIdentifier{1, 2, 643, 7, 1, 1, 2, 2}
	OIDGOST3410_2012_256 = asn1.ObjectIdentifier{1, 2, 643, 7, 1, 1, 1, 1}
	OIDSHA1              = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	OIDEnvelopedData     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	oidSurname           = asn1.ObjectIdentifier{2, 5, 4, 4}
	oidGivenName         = asn1.ObjectIdentifier{2, 5, 4, 42}
	oidTitle             = asn1.ObjectIdentifier{2, 5, 4, 12}
	oidCommonName        = asn1.ObjectIdentifier{2, 5, 4, 3}
	oidOrganization      = asn1.ObjectIdentifier{2, 5, 4, 10}
	oidCountry           = asn1.ObjectIdentifier{2, 5, 4, 6}
	oidINN               = asn1.ObjectIdentifier{1, 2, 643, 3, 131, 1, 1}
	oidOGRN              = asn1.ObjectIdentifier{1, 2, 643, 100, 1}
	oidSNILS             = asn1.ObjectIdentifier{1, 2, 643, 100, 3}
	oidINNLE             = asn1.ObjectIdentifier{1, 2, 643, 100, 4}
	oidOGRNIP            = asn1.ObjectIdentifier{1, 2, 643, 100, 5}
)

// CA — тестовый удостоверяющий центр.
type CA struct {
	Cert   *x509.Certificate
	Key    crypto.Signer
	serial int64
}

// NewCA — самоподписанный корневой УЦ с ключом ECDSA P-256.
func NewCA(name string, notBefore, notAfter time.Time) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := caTemplate(name, 1, notBefore, notAfter)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("create CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key, serial: 1}, nil
}

// Intermediate — промежуточный УЦ, выданный этим.
func (ca *CA) Intermediate(name string, notBefore, notAfter time.Time) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, caTemplate(name, ca.nextSerial(), notBefore, notAfter), ca.Cert,
		key.Public(), ca.Key)
	if err != nil {
		return nil, fmt.Errorf("create intermediate CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, Key: key, serial: 1000}, nil
}

func caTemplate(name string, serial int64, notBefore, notAfter time.Time) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: name, Organization: []string{"Тестовый УЦ (TEST, не для КЭП)"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
}

func (ca *CA) nextSerial() int64 {
	ca.serial++
	return ca.serial
}

// Subject — кому выдан сертификат: ФИО, должность, организация и российские идентификаторы
// (ИНН ФЛ, СНИЛС, ИНН ЮЛ, ОГРН, ОГРНИП) — как в сертификатах по приказу ФСБ № 795.
type Subject struct {
	Surname      string // Фамилия
	GivenName    string // Имя Отчество
	CommonName   string // пусто — «Фамилия Имя Отчество»
	Title        string
	Organization string
	INN          string // ИНН физлица
	SNILS        string
	OrgINN       string // ИНН юрлица
	OGRN         string
	OGRNIP       string
}

// CertOptions — срок и назначение ключа сертификата подписанта.
type CertOptions struct {
	NotBefore, NotAfter time.Time
	KeyUsage            x509.KeyUsage // 0 — digitalSignature | nonRepudiation
	OmitKeyUsage        bool          // без расширения keyUsage
	Key                 crypto.Signer // nil — новый ECDSA P-256
}

// Signer — сертификат подписанта с ключом.
type Signer struct {
	Cert *x509.Certificate
	Key  crypto.Signer
}

// Issue выдаёт сертификат подписанту.
func (ca *CA) Issue(subj Subject, opts CertOptions) (*Signer, error) {
	key := opts.Key
	if key == nil {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		key = k
	}
	ku := opts.KeyUsage
	if ku == 0 {
		ku = x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment
	}
	if opts.OmitKeyUsage {
		ku = 0
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(ca.nextSerial()),
		Subject:      pkix.Name{ExtraNames: subj.names()},
		NotBefore:    opts.NotBefore,
		NotAfter:     opts.NotAfter,
		KeyUsage:     ku,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, fmt.Errorf("issue certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Signer{Cert: cert, Key: key}, nil
}

// SelfSigned — самоподписанный сертификат подписанта (не из доверенного УЦ).
func SelfSigned(subj Subject, notBefore, notAfter time.Time) (*Signer, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{ExtraNames: subj.names()},
		NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Signer{Cert: cert, Key: key}, nil
}

// names — атрибуты имени: строки — UTF8String, идентификаторы — NumericString, страна — PrintableString.
func (s Subject) names() []pkix.AttributeTypeAndValue {
	utf8 := func(v string) asn1.RawValue { return asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte(v)} }
	numeric := func(v string) asn1.RawValue { return asn1.RawValue{Tag: asn1.TagNumericString, Bytes: []byte(v)} }
	cn := s.CommonName
	if cn == "" {
		cn = s.Surname + " " + s.GivenName
	}
	out := []pkix.AttributeTypeAndValue{{Type: oidCountry, Value: asn1.RawValue{Tag: asn1.TagPrintableString, Bytes: []byte("RU")}}}
	add := func(oid asn1.ObjectIdentifier, v string, enc func(string) asn1.RawValue) {
		if v != "" {
			out = append(out, pkix.AttributeTypeAndValue{Type: oid, Value: enc(v)})
		}
	}
	add(oidOrganization, s.Organization, utf8)
	add(oidTitle, s.Title, utf8)
	add(oidSurname, s.Surname, utf8)
	add(oidGivenName, s.GivenName, utf8)
	add(oidCommonName, cn, utf8)
	add(oidINN, s.INN, numeric)
	add(oidSNILS, onlyDigits(s.SNILS), numeric)
	add(oidINNLE, s.OrgINN, numeric)
	add(oidOGRN, s.OGRN, numeric)
	add(oidOGRNIP, s.OGRNIP, numeric)
	return out
}

func onlyDigits(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// Revoked — отозванный сертификат и время отзыва.
type Revoked struct {
	Cert *x509.Certificate
	At   time.Time
}

// CRL — список отзыва УЦ в DER.
func (ca *CA) CRL(thisUpdate, nextUpdate time.Time, revoked ...Revoked) ([]byte, error) {
	tmpl := &x509.RevocationList{Number: big.NewInt(ca.nextSerial()), ThisUpdate: thisUpdate, NextUpdate: nextUpdate}
	for _, r := range revoked {
		tmpl.RevokedCertificateEntries = append(tmpl.RevokedCertificateEntries,
			x509.RevocationListEntry{SerialNumber: r.Cert.SerialNumber, RevocationTime: r.At})
	}
	return x509.CreateRevocationList(rand.Reader, tmpl, ca.Cert, ca.Key)
}

// SignOptions — как собрать подпись CMS.
type SignOptions struct {
	Attached         bool                  // документ внутри контейнера
	SigningTime      time.Time             // нулевое — без атрибута signing-time
	NoSigningCert    bool                  // без signing-certificate-v2: обычный CMS, не CAdES-BES
	NoSignedAttrs    bool                  // PKCS#7 без подписанных атрибутов
	Hash             crypto.Hash           // 0 — SHA-256
	DigestOID        asn1.ObjectIdentifier // подменить OID хеша (ГОСТ, SHA-1)
	SignatureOID     asn1.ObjectIdentifier // подменить OID алгоритма подписи
	ContentType      asn1.ObjectIdentifier // подменить eContentType
	ExtraCerts       []*x509.Certificate   // промежуточные УЦ в контейнер
	NoCertificates   bool                  // без сертификатов в контейнере
	Signers          int                   // сколько раз повторить SignerInfo; 0 — один
	CorruptSignature bool                  // испортить значение подписи
	WrongCertHash    bool                  // signing-certificate-v2 с чужим хешем
}

// Sign подписывает content: открепленная (по умолчанию) или присоединённая подпись CMS
// SignedData в DER с атрибутами CAdES-BES (content-type, message-digest, signing-time,
// signing-certificate-v2).
func Sign(content []byte, s *Signer, opts SignOptions) ([]byte, error) {
	h := opts.Hash
	if h == 0 {
		h = crypto.SHA256
	}
	digestOID, sigOID, sigParams, err := algorithms(s.Key, h)
	if err != nil {
		return nil, err
	}
	if opts.DigestOID != nil {
		digestOID = opts.DigestOID
	}
	if opts.SignatureOID != nil {
		sigOID, sigParams = opts.SignatureOID, nil
	}
	contentType := oidData
	if opts.ContentType != nil {
		contentType = opts.ContentType
	}

	toSign := content
	var signedAttrs []byte
	if !opts.NoSignedAttrs {
		attrs := [][]byte{
			attribute(oidContentType, mustMarshal(contentType)),
			attribute(oidMessageDigest, mustMarshal(digest(h, content))),
		}
		if !opts.SigningTime.IsZero() {
			attrs = append(attrs, attribute(oidSigningTime, mustMarshal(opts.SigningTime.UTC())))
		}
		if !opts.NoSigningCert {
			certHash := sha256.Sum256(s.Cert.Raw)
			if opts.WrongCertHash {
				certHash[0] ^= 0xff
			}
			issuerSerial := tlv(0x30, tlv(0x30, tlv(0xa4, s.Cert.RawIssuer)), mustMarshal(s.Cert.SerialNumber))
			essCertIDv2 := tlv(0x30, mustMarshal(certHash[:]), issuerSerial) // hashAlgorithm SHA-256 — по умолчанию
			attrs = append(attrs, attribute(oidSigningCertV2, tlv(0x30, tlv(0x30, essCertIDv2))))
		}
		slices.SortFunc(attrs, bytes.Compare) // DER: SET OF упорядочен по кодировке
		signedAttrs = tlv(0x31, attrs...)
		toSign = signedAttrs
	}

	var sig []byte
	switch key := s.Key.(type) {
	case ed25519.PrivateKey:
		sig, err = key.Sign(rand.Reader, toSign, crypto.Hash(0))
	default:
		sig, err = s.Key.Sign(rand.Reader, digest(h, toSign), h)
	}
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	if opts.CorruptSignature {
		sig[len(sig)/2] ^= 0x01
	}

	sigAlg := [][]byte{mustMarshal(sigOID)}
	if sigParams != nil {
		sigAlg = append(sigAlg, sigParams)
	}
	signerInfo := [][]byte{
		mustMarshal(1),
		tlv(0x30, s.Cert.RawIssuer, mustMarshal(s.Cert.SerialNumber)),
		tlv(0x30, mustMarshal(digestOID)),
	}
	if signedAttrs != nil {
		signerInfo = append(signerInfo, append([]byte{0xa0}, signedAttrs[1:]...)) // [0] IMPLICIT
	}
	signerInfo = append(signerInfo, tlv(0x30, sigAlg...), mustMarshal(sig))
	si := tlv(0x30, signerInfo...)
	signers := [][]byte{si}
	for i := 1; i < opts.Signers; i++ {
		signers = append(signers, si)
	}

	encap := [][]byte{mustMarshal(contentType)}
	if opts.Attached {
		encap = append(encap, tlv(0xa0, mustMarshal(content)))
	}
	sd := [][]byte{mustMarshal(1), tlv(0x31, tlv(0x30, mustMarshal(digestOID))), tlv(0x30, encap...)}
	if !opts.NoCertificates {
		certs := [][]byte{s.Cert.Raw}
		for _, c := range opts.ExtraCerts {
			certs = append(certs, c.Raw)
		}
		sd = append(sd, tlv(0xa0, certs...))
	}
	sd = append(sd, tlv(0x31, signers...))
	return tlv(0x30, mustMarshal(oidSignedData), tlv(0xa0, tlv(0x30, sd...))), nil
}

// Wrap — ContentInfo другого типа CMS (например, EnvelopedData) с произвольным содержимым.
func Wrap(contentType asn1.ObjectIdentifier, inner []byte) []byte {
	return tlv(0x30, mustMarshal(contentType), tlv(0xa0, tlv(0x30, inner)))
}

// ToBER перекодирует DER-контейнер с неопределённой длиной внешних элементов — так пишут
// потоковые подписчики (openssl cms -stream).
func ToBER(der []byte) ([]byte, error) {
	var ci struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, err
	}
	out := []byte{0x30, 0x80}
	out = append(out, mustMarshal(ci.ContentType)...)
	out = append(out, 0xa0, 0x80)
	out = append(out, ci.Content.Bytes...)
	return append(out, 0, 0, 0, 0), nil
}

func algorithms(key crypto.Signer, h crypto.Hash) (digestOID, sigOID asn1.ObjectIdentifier, sigParams []byte, err error) {
	switch h {
	case crypto.SHA256:
		digestOID = oidSHA256
	case crypto.SHA384:
		digestOID = oidSHA384
	case crypto.SHA512:
		digestOID = oidSHA512
	default:
		return nil, nil, nil, errors.New("sigtest: unsupported hash")
	}
	switch key.(type) {
	case *ecdsa.PrivateKey:
		sigOID = map[crypto.Hash]asn1.ObjectIdentifier{crypto.SHA256: oidECDSAWithSHA256, crypto.SHA384: oidECDSAWithSHA384,
			crypto.SHA512: oidECDSAWithSHA512}[h]
	case *rsa.PrivateKey:
		sigOID, sigParams = oidRSAEncryption, []byte{0x05, 0x00}
	case ed25519.PrivateKey:
		sigOID = oidEd25519
	default:
		return nil, nil, nil, errors.New("sigtest: unsupported key")
	}
	return digestOID, sigOID, sigParams, nil
}

func attribute(oid asn1.ObjectIdentifier, values ...[]byte) []byte {
	return tlv(0x30, mustMarshal(oid), tlv(0x31, values...))
}

func digest(h crypto.Hash, data []byte) []byte {
	hh := h.New()
	hh.Write(data)
	return hh.Sum(nil)
}

// tlv — элемент DER: тег, длина, содержимое.
func tlv(tag byte, content ...[]byte) []byte {
	body := bytes.Join(content, nil)
	out := []byte{tag}
	switch n := len(body); {
	case n < 0x80:
		out = append(out, byte(n))
	default:
		var l []byte
		for x := n; x > 0; x >>= 8 {
			l = append([]byte{byte(x)}, l...)
		}
		out = append(append(out, 0x80|byte(len(l))), l...)
	}
	return append(out, body...)
}

func mustMarshal(v any) []byte {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
