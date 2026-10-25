package signature

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Разбор CMS SignedData (RFC 5652) строго по DER, без сторонних библиотек: только то, что нужно
// для проверки одной подписи CAdES-BES. BER (неопределённая длина, составные OCTET STRING), который
// выдают потоковые подписчики, не поддержан: такой контейнер — INDETERMINATE, а не INVALID.

// formatError — контейнер подписи не разобран. unsupported — формат не поддерживается
// (INDETERMINATE), иначе контейнер испорчен (INVALID). code — код проверки в отчёте;
// пусто — FORMAT_PARSED.
type formatError struct {
	unsupported bool
	code        string
	msg         string
}

func (e *formatError) Error() string { return e.msg }

func unsupportedFormat(format string, args ...any) error {
	return &formatError{unsupported: true, msg: fmt.Sprintf(format, args...)}
}

func brokenFormat(format string, args ...any) error {
	return &formatError{msg: fmt.Sprintf(format, args...)}
}

// signedData — разобранный CMS SignedData.
type signedData struct {
	contentType  asn1.ObjectIdentifier // eContentType
	content      []byte                // eContent; nil — подпись открепленная
	certificates []*x509.Certificate
	badCerts     int // сертификаты, которые не удалось разобрать, и не X.509 (attribute и т. п.)
	signers      []signerInfo
	encoding     string // DER, PEM или BASE64 — как пришёл файл
}

// signerInfo — одна подпись в SignedData.
type signerInfo struct {
	issuer             []byte // DER имени издателя (IssuerAndSerialNumber); nil — указан SubjectKeyIdentifier
	serial             *big.Int
	subjectKeyID       []byte
	digestAlgorithm    pkix.AlgorithmIdentifier
	signedAttrs        []byte // DER SET OF Attribute — над ним считается подпись; nil — атрибутов нет
	attrs              []attribute
	signatureAlgorithm pkix.AlgorithmIdentifier
	signature          []byte
	unsignedAttrs      []attribute
}

// attribute — атрибут SignerInfo: тип и значения из SET OF.
type attribute struct {
	oid    asn1.ObjectIdentifier
	values []asn1.RawValue
}

type contentInfoASN struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type signedDataASN struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	EncapContentInfo encapContentInfoASN
	Certificates     asn1.RawValue   `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue   `asn1:"optional,tag:1"`
	SignerInfos      []signerInfoASN `asn1:"set"`
}

type encapContentInfoASN struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type signerInfoASN struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type issuerAndSerialASN struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

type attributeASN struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue
}

// parseCMS разбирает файл подписи: DER, PEM или base64 от DER.
func parseCMS(data []byte) (*signedData, error) {
	der, encoding, err := decodeContainer(data)
	if err != nil {
		return nil, err
	}

	var ci contentInfoASN
	rest, err := asn1.Unmarshal(der, &ci)
	if err != nil {
		return nil, classifyASN1(der, "контейнер CMS", err)
	}
	if len(rest) > 0 {
		return nil, brokenFormat("после контейнера CMS лишние данные (%d байт)", len(rest))
	}
	switch {
	case ci.ContentType.Equal(oidSignedData):
	case len(ci.ContentType) > len(oidPKCS7Arc) && ci.ContentType[:len(oidPKCS7Arc)].Equal(oidPKCS7Arc):
		return nil, unsupportedFormat("контейнер CMS типа %s — не подпись (SignedData)", ci.ContentType)
	default:
		return nil, unsupportedFormat("формат подписи не распознан: ожидался CMS SignedData")
	}
	if len(ci.Content.Bytes) == 0 {
		return nil, brokenFormat("в контейнере CMS нет SignedData")
	}

	var sd signedDataASN
	if rest, err = asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, classifyASN1(ci.Content.Bytes, "SignedData", err)
	}
	if len(rest) > 0 {
		return nil, brokenFormat("после SignedData лишние данные")
	}

	out := &signedData{contentType: sd.EncapContentInfo.EContentType, encoding: encoding}
	if ec := sd.EncapContentInfo.EContent; len(ec.FullBytes) > 0 {
		var content []byte
		if rest, err := asn1.Unmarshal(ec.Bytes, &content); err != nil || len(rest) > 0 {
			return nil, unsupportedFormat("документ внутри контейнера закодирован не по DER (составной OCTET STRING)")
		}
		if content == nil {
			content = []byte{}
		}
		out.content = content
	}
	if len(sd.Certificates.FullBytes) > 0 {
		for rest := sd.Certificates.Bytes; len(rest) > 0; {
			var raw asn1.RawValue
			if rest, err = asn1.Unmarshal(rest, &raw); err != nil {
				return nil, brokenFormat("список сертификатов CMS повреждён: %v", err)
			}
			if raw.Class != asn1.ClassUniversal || raw.Tag != asn1.TagSequence {
				out.badCerts++ // расширенные и attribute-сертификаты не используются
				continue
			}
			cert, err := x509.ParseCertificate(raw.FullBytes)
			if err != nil {
				out.badCerts++
				continue
			}
			out.certificates = append(out.certificates, cert)
		}
	}
	for i, si := range sd.SignerInfos {
		s, err := parseSignerInfo(si)
		if err != nil {
			return nil, brokenFormat("подпись %d: %v", i, err)
		}
		out.signers = append(out.signers, s)
	}
	return out, nil
}

func parseSignerInfo(si signerInfoASN) (signerInfo, error) {
	s := signerInfo{digestAlgorithm: si.DigestAlgorithm, signatureAlgorithm: si.SignatureAlgorithm, signature: si.Signature}
	switch sid := si.SID; {
	case sid.Class == asn1.ClassUniversal && sid.Tag == asn1.TagSequence:
		var ias issuerAndSerialASN
		if rest, err := asn1.Unmarshal(sid.FullBytes, &ias); err != nil || len(rest) > 0 || ias.SerialNumber == nil {
			return s, fmt.Errorf("issuerAndSerialNumber повреждён")
		}
		s.issuer, s.serial = ias.Issuer.FullBytes, ias.SerialNumber
	case sid.Class == asn1.ClassContextSpecific && sid.Tag == 0 && !sid.IsCompound:
		s.subjectKeyID = sid.Bytes
	default:
		return s, fmt.Errorf("неизвестный идентификатор подписанта")
	}
	if len(si.SignedAttrs.FullBytes) > 0 {
		attrs, err := parseAttributes(si.SignedAttrs.Bytes)
		if err != nil {
			return s, fmt.Errorf("подписанные атрибуты: %w", err)
		}
		// подпись считается над DER SET OF Attribute: тот же байтовый текст с тегом SET вместо [0]
		s.signedAttrs = append([]byte{0x31}, si.SignedAttrs.FullBytes[1:]...)
		s.attrs = attrs
	}
	if len(si.UnsignedAttrs.FullBytes) > 0 {
		attrs, err := parseAttributes(si.UnsignedAttrs.Bytes)
		if err != nil {
			return s, fmt.Errorf("неподписанные атрибуты: %w", err)
		}
		s.unsignedAttrs = attrs
	}
	return s, nil
}

func parseAttributes(data []byte) ([]attribute, error) {
	var out []attribute
	for rest := data; len(rest) > 0; {
		var a attributeASN
		var err error
		if rest, err = asn1.Unmarshal(rest, &a); err != nil {
			return nil, err
		}
		if a.Values.Class != asn1.ClassUniversal || a.Values.Tag != asn1.TagSet || !a.Values.IsCompound {
			return nil, fmt.Errorf("значения атрибута %s — не SET", a.Type)
		}
		attr := attribute{oid: a.Type}
		for vals := a.Values.Bytes; len(vals) > 0; {
			var v asn1.RawValue
			if vals, err = asn1.Unmarshal(vals, &v); err != nil {
				return nil, err
			}
			attr.values = append(attr.values, v)
		}
		out = append(out, attr)
	}
	return out, nil
}

// find — атрибут по типу; n — сколько раз он встретился.
func find(attrs []attribute, oid asn1.ObjectIdentifier) (a attribute, n int) {
	for _, x := range attrs {
		if x.oid.Equal(oid) {
			if n == 0 {
				a = x
			}
			n++
		}
	}
	return a, n
}

// signedAttributes — значения обязательных подписанных атрибутов CMS.
type signedAttributes struct {
	contentType   asn1.ObjectIdentifier
	messageDigest []byte
	signingTime   time.Time // нулевое — атрибута нет
}

// parseSignedAttributes проверяет contentType, messageDigest и signingTime: каждый не больше
// одного раза и ровно с одним значением (RFC 5652, 5.3 и 11).
func parseSignedAttributes(attrs []attribute) (signedAttributes, error) {
	var out signedAttributes
	single := func(oid asn1.ObjectIdentifier, name string, required bool) (asn1.RawValue, bool, error) {
		a, n := find(attrs, oid)
		switch {
		case n == 0 && required:
			return asn1.RawValue{}, false, fmt.Errorf("нет атрибута %s", name)
		case n == 0:
			return asn1.RawValue{}, false, nil
		case n > 1 || len(a.values) != 1:
			return asn1.RawValue{}, false, fmt.Errorf("атрибут %s встречается несколько раз или с несколькими значениями", name)
		}
		return a.values[0], true, nil
	}

	v, _, err := single(oidContentType, "content-type", true)
	if err != nil {
		return out, err
	}
	if rest, err := asn1.Unmarshal(v.FullBytes, &out.contentType); err != nil || len(rest) > 0 {
		return out, fmt.Errorf("атрибут content-type повреждён")
	}
	if v, _, err = single(oidMessageDigest, "message-digest", true); err != nil {
		return out, err
	}
	if rest, err := asn1.Unmarshal(v.FullBytes, &out.messageDigest); err != nil || len(rest) > 0 || len(out.messageDigest) == 0 {
		return out, fmt.Errorf("атрибут message-digest повреждён")
	}
	v, ok, err := single(oidSigningTime, "signing-time", false)
	if err != nil {
		return out, err
	}
	if ok {
		if rest, err := asn1.Unmarshal(v.FullBytes, &out.signingTime); err != nil || len(rest) > 0 {
			return out, fmt.Errorf("атрибут signing-time повреждён")
		}
		out.signingTime = out.signingTime.UTC()
	}
	return out, nil
}

// essCertID — ссылка на сертификат подписанта из атрибута signing-certificate(-v2) (RFC 5035).
type essCertID struct {
	hash   asn1.ObjectIdentifier // алгоритм хеша сертификата
	digest []byte
	v2     bool
}

// parseSigningCertificate — первая ссылка из signing-certificate-v2 (хеш по умолчанию SHA-256)
// или signing-certificate (SHA-1). false — атрибута нет.
func parseSigningCertificate(attrs []attribute) (essCertID, bool, error) {
	a, n := find(attrs, oidSigningCertV2)
	v2 := n > 0
	if !v2 {
		if a, n = find(attrs, oidSigningCert); n == 0 {
			return essCertID{}, false, nil
		}
	}
	if n > 1 || len(a.values) != 1 {
		return essCertID{}, true, fmt.Errorf("атрибут signing-certificate встречается несколько раз")
	}
	var sc struct {
		Certs []asn1.RawValue
		Rest  asn1.RawValue `asn1:"optional"`
	}
	if rest, err := asn1.Unmarshal(a.values[0].FullBytes, &sc); err != nil || len(rest) > 0 || len(sc.Certs) == 0 {
		return essCertID{}, true, fmt.Errorf("атрибут signing-certificate повреждён")
	}
	id := essCertID{hash: oidSHA1, v2: v2}
	if v2 {
		id.hash = oidSHA256
	}
	first := sc.Certs[0].Bytes
	var el asn1.RawValue
	rest, err := asn1.Unmarshal(first, &el)
	if err != nil {
		return essCertID{}, true, fmt.Errorf("атрибут signing-certificate повреждён")
	}
	if v2 && el.Class == asn1.ClassUniversal && el.Tag == asn1.TagSequence { // hashAlgorithm указан явно
		var alg pkix.AlgorithmIdentifier
		if _, err := asn1.Unmarshal(el.FullBytes, &alg); err != nil {
			return essCertID{}, true, fmt.Errorf("алгоритм в signing-certificate-v2 повреждён")
		}
		id.hash = alg.Algorithm
		if rest, err = asn1.Unmarshal(rest, &el); err != nil {
			return essCertID{}, true, fmt.Errorf("атрибут signing-certificate повреждён")
		}
	}
	if el.Class != asn1.ClassUniversal || el.Tag != asn1.TagOctetString || len(el.Bytes) == 0 {
		return essCertID{}, true, fmt.Errorf("в signing-certificate нет хеша сертификата")
	}
	id.digest = el.Bytes
	return id, true, nil
}

// decodeContainer — DER контейнера: сам файл, PEM («-----BEGIN PKCS7-----», «CMS») или base64.
func decodeContainer(data []byte) (der []byte, encoding string, err error) {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(data) == 0:
		return nil, "", brokenFormat("файл подписи пустой")
	case data[0] == 0x30:
		return data, "DER", nil
	case bytes.HasPrefix(trimmed, []byte("-----BEGIN")):
		block, rest := pem.Decode(trimmed)
		if block == nil || len(bytes.TrimSpace(rest)) > 0 {
			return nil, "", brokenFormat("PEM-контейнер подписи повреждён")
		}
		if !strings.Contains(block.Type, "PKCS7") && !strings.Contains(block.Type, "CMS") {
			return nil, "", unsupportedFormat("PEM-блок %q — не подпись CMS", block.Type)
		}
		return block.Bytes, "PEM", nil
	}
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, string(trimmed))
	if decoded, err := base64.StdEncoding.DecodeString(compact); err == nil && len(decoded) > 0 && decoded[0] == 0x30 {
		return decoded, "BASE64", nil
	}
	return nil, "", unsupportedFormat("формат подписи не распознан: ожидался CMS/CAdES (.sig, .p7s)")
}

// classifyASN1 — почему не разобралась структура: корректный BER с тем, чего нет в DER
// (неопределённая длина, составные строки, неминимальная длина), — не поддержан; иначе
// контейнер испорчен.
func classifyASN1(data []byte, what string, err error) error {
	var ber berScan
	if n, ok := ber.element(data, 0); ok && n == len(data) && ber.notDER {
		return unsupportedFormat("%s закодирован в BER (неопределённая длина или составные строки), поддерживается только DER", what)
	}
	return brokenFormat("%s повреждён: %v", what, err)
}

// berScan проверяет структуру BER и отмечает конструкции, которых нет в DER.
type berScan struct {
	notDER bool
}

// element разбирает элемент BER с начала data и возвращает его длину; depth ограничивает вложенность.
func (s *berScan) element(data []byte, depth int) (int, bool) {
	if depth > 64 || len(data) < 2 {
		return 0, false
	}
	pos := 1
	constructed := data[0]&0x20 != 0
	if data[0]&0x1f == 0x1f { // длинный номер тега
		for pos < len(data) && data[pos]&0x80 != 0 {
			pos++
		}
		pos++
	}
	if pos >= len(data) {
		return 0, false
	}
	// составная строка (OCTET STRING, BIT STRING и т. п.) бывает только в BER
	if universal, tag := data[0]&0xc0 == 0, data[0]&0x1f; constructed && universal && tag != asn1.TagSequence && tag != asn1.TagSet {
		s.notDER = true
	}
	l := int(data[pos])
	pos++
	switch {
	case l == 0x80: // неопределённая длина: элементы до end-of-contents 00 00
		if !constructed {
			return 0, false
		}
		s.notDER = true
		for {
			if pos+1 < len(data) && data[pos] == 0 && data[pos+1] == 0 {
				return pos + 2, true
			}
			n, ok := s.element(data[pos:], depth+1)
			if !ok {
				return 0, false
			}
			pos += n
		}
	case l > 0x80:
		size := l & 0x7f
		if size > 4 || pos+size > len(data) {
			return 0, false
		}
		l = 0
		for _, b := range data[pos : pos+size] {
			l = l<<8 | int(b)
		}
		if l < 0x80 || data[pos] == 0 { // длинная форма там, где хватило бы короткой
			s.notDER = true
		}
		pos += size
	}
	if l < 0 || pos+l > len(data) {
		return 0, false
	}
	if constructed {
		for inner := pos; inner < pos+l; {
			n, ok := s.element(data[inner:pos+l], depth+1)
			if !ok {
				return 0, false
			}
			inner += n
		}
	}
	return pos + l, true
}
