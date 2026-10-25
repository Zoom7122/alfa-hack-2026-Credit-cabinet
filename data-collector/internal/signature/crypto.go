package signature

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
)

// minRSABits — минимальная длина ключа RSA в профиле TEST.
const minRSABits = 2048

// errSignatureMismatch — значение подписи не сходится с ключом сертификата.
var errSignatureMismatch = errors.New("значение подписи не сходится с открытым ключом сертификата")

// algorithm — чем проверять подпись SignerInfo.
type algorithm struct {
	name   string      // для отчёта: «ECDSA / SHA-256»
	hash   crypto.Hash // хеш документа (message-digest) и подписанных атрибутов
	scheme string      // RSA, RSA-PSS, ECDSA, Ed25519
	pss    *rsa.PSSOptions
}

// resolveAlgorithm — алгоритм подписи по digestAlgorithm и signatureAlgorithm. Ошибка
// unsupported (*formatError) — алгоритм не поддерживается профилем: ГОСТ, слабый хеш, неизвестный;
// иначе алгоритмы указаны противоречиво.
func resolveAlgorithm(si signerInfo) (algorithm, error) {
	d, s := si.digestAlgorithm.Algorithm, si.signatureAlgorithm.Algorithm
	if isRussian(d) || isRussian(s) {
		return algorithm{}, unsupportedFormat("алгоритм ГОСТ (OID %s / %s) не поддерживается: в стеке нет реализации "+
			"ГОСТ Р 34.10/34.11 в сертифицированном СКЗИ, подпись не проверялась", d, s)
	}
	if name, weak := weakDigests[d.String()]; weak {
		return algorithm{}, unsupportedFormat("хеш %s ниже требований профиля (нужен SHA-256, SHA-384 или SHA-512)", name)
	}
	h, ok := digests[d.String()]
	if !ok {
		return algorithm{}, unsupportedFormat("неизвестный алгоритм хеша %s", d)
	}

	implied := func(want crypto.Hash, scheme string) (algorithm, error) {
		if want != h {
			return algorithm{}, brokenFormat("хеш в алгоритме подписи (%s) не совпадает с digestAlgorithm (%s)", hashName(want), hashName(h))
		}
		return algorithm{name: scheme + " / " + hashName(h), hash: h, scheme: scheme}, nil
	}
	switch {
	case s.Equal(oidRSAEncryption):
		return algorithm{name: "RSA / " + hashName(h), hash: h, scheme: "RSA"}, nil
	case s.Equal(oidSHA256WithRSA):
		return implied(crypto.SHA256, "RSA")
	case s.Equal(oidSHA384WithRSA):
		return implied(crypto.SHA384, "RSA")
	case s.Equal(oidSHA512WithRSA):
		return implied(crypto.SHA512, "RSA")
	case s.Equal(oidECPublicKey):
		return algorithm{name: "ECDSA / " + hashName(h), hash: h, scheme: "ECDSA"}, nil
	case s.Equal(oidECDSAWithSHA256):
		return implied(crypto.SHA256, "ECDSA")
	case s.Equal(oidECDSAWithSHA384):
		return implied(crypto.SHA384, "ECDSA")
	case s.Equal(oidECDSAWithSHA512):
		return implied(crypto.SHA512, "ECDSA")
	case s.Equal(oidEd25519):
		return algorithm{name: "Ed25519 / " + hashName(h), hash: h, scheme: "Ed25519"}, nil
	case s.Equal(oidRSAPSS):
		opts, err := parsePSS(si.signatureAlgorithm.Parameters, h)
		if err != nil {
			return algorithm{}, err
		}
		return algorithm{name: "RSA-PSS / " + hashName(h), hash: h, scheme: "RSA-PSS", pss: opts}, nil
	case s.Equal(oidSHA1WithRSA), s.Equal(oidSHA224WithRSA), s.Equal(oidECDSAWithSHA1), s.Equal(oidECDSAWithSHA224):
		return algorithm{}, unsupportedFormat("алгоритм подписи %s ниже требований профиля", s)
	}
	return algorithm{}, unsupportedFormat("алгоритм подписи %s не поддерживается", s)
}

// pssParams — RSASSA-PSS-params (RFC 4055). Поля без значения — по умолчанию SHA-1, их профиль не принимает.
type pssParams struct {
	Hash         pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:0"`
	MGF          pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:1"`
	SaltLength   int                      `asn1:"explicit,optional,tag:2,default:20"`
	TrailerField int                      `asn1:"explicit,optional,tag:3,default:1"`
}

func parsePSS(raw asn1.RawValue, h crypto.Hash) (*rsa.PSSOptions, error) {
	var p pssParams
	if rest, err := asn1.Unmarshal(raw.FullBytes, &p); err != nil || len(rest) > 0 {
		return nil, brokenFormat("параметры RSA-PSS повреждены")
	}
	ph, ok := digests[p.Hash.Algorithm.String()]
	if !ok {
		return nil, unsupportedFormat("RSA-PSS с хешем %s не поддерживается (нужен SHA-256/384/512)", p.Hash.Algorithm)
	}
	var mgfHash pkix.AlgorithmIdentifier
	if !p.MGF.Algorithm.Equal(oidMGF1) {
		return nil, unsupportedFormat("RSA-PSS с функцией маски %s не поддерживается", p.MGF.Algorithm)
	}
	if rest, err := asn1.Unmarshal(p.MGF.Parameters.FullBytes, &mgfHash); err != nil || len(rest) > 0 ||
		!mgfHash.Algorithm.Equal(p.Hash.Algorithm) {
		return nil, unsupportedFormat("RSA-PSS: хеш MGF1 должен совпадать с хешем подписи")
	}
	if ph != h {
		return nil, brokenFormat("хеш RSA-PSS (%s) не совпадает с digestAlgorithm (%s)", hashName(ph), hashName(h))
	}
	if p.TrailerField != 1 || p.SaltLength < 0 {
		return nil, brokenFormat("параметры RSA-PSS вне стандарта")
	}
	return &rsa.PSSOptions{SaltLength: p.SaltLength, Hash: ph}, nil
}

// verifySignature проверяет значение подписи над signed (DER подписанных атрибутов) ключом
// сертификата. *formatError (unsupported) — ключ не поддерживается; errSignatureMismatch и
// другие ошибки — подпись недействительна.
func verifySignature(alg algorithm, cert *x509.Certificate, signed, sig []byte) error {
	if cert.PublicKey == nil {
		return unsupportedFormat("ключ сертификата (%s) не поддерживается", cert.PublicKeyAlgorithm)
	}
	digest := func() []byte {
		hh := alg.hash.New()
		hh.Write(signed)
		return hh.Sum(nil)
	}
	switch alg.scheme {
	case "RSA", "RSA-PSS":
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("ключ сертификата %s не подходит к подписи %s", cert.PublicKeyAlgorithm, alg.scheme)
		}
		if pub.N.BitLen() < minRSABits {
			return unsupportedFormat("ключ RSA %d бит короче %d — ниже требований профиля", pub.N.BitLen(), minRSABits)
		}
		var err error
		if alg.scheme == "RSA" {
			err = rsa.VerifyPKCS1v15(pub, alg.hash, digest(), sig)
		} else {
			err = rsa.VerifyPSS(pub, alg.hash, digest(), sig, alg.pss)
		}
		if err != nil {
			return errSignatureMismatch
		}
	case "ECDSA":
		pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("ключ сертификата %s не подходит к подписи ECDSA", cert.PublicKeyAlgorithm)
		}
		if !ecdsa.VerifyASN1(pub, digest(), sig) {
			return errSignatureMismatch
		}
	case "Ed25519":
		pub, ok := cert.PublicKey.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("ключ сертификата %s не подходит к подписи Ed25519", cert.PublicKeyAlgorithm)
		}
		if !ed25519.Verify(pub, signed, sig) {
			return errSignatureMismatch
		}
	default:
		return unsupportedFormat("схема подписи %s не поддерживается", alg.scheme)
	}
	return nil
}

// keyName — алгоритм ключа сертификата для отчёта (enum key_algorithm из api/openapi.yaml).
func keyName(cert *x509.Certificate) string {
	switch cert.PublicKeyAlgorithm {
	case x509.RSA:
		return "RSA"
	case x509.ECDSA:
		return "ECDSA"
	}
	return "OTHER" // Ed25519 и неизвестные, в том числе ГОСТ (его ключ Go не разбирает)
}
