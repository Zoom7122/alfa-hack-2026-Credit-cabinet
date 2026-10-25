package signature

import (
	"crypto"
	"encoding/asn1"
)

// CMS (RFC 5652) и атрибуты CAdES (RFC 5126, ETSI EN 319 122).
var (
	oidData          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSignedData    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidPKCS7Arc      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7} // остальные типы CMS: enveloped, digested…
	oidContentType   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningTime   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidCounterSign   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 6}
	oidSigningCert   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	oidSigningCertV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	oidTimeStamp     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 14}
)

// Хеши и алгоритмы подписи.
var (
	oidMD5    = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 5}
	oidSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA224 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 4}
	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}

	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidSHA1WithRSA     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidSHA224WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 14}
	oidRSAPSS          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidMGF1            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	oidECPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	oidECDSAWithSHA224 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidEd25519         = asn1.ObjectIdentifier{1, 3, 101, 112}

	// российская ветка: ГОСТ Р 34.10-2012/2001 (подпись), ГОСТ Р 34.11-2012 «Стрибог» и 34.11-94 (хеш)
	oidRussianArc = asn1.ObjectIdentifier{1, 2, 643}
)

// Атрибуты имени субъекта сертификата (X.520 и приказ ФСБ России № 795).
var (
	oidSurname   = asn1.ObjectIdentifier{2, 5, 4, 4}
	oidGivenName = asn1.ObjectIdentifier{2, 5, 4, 42}
	oidTitle     = asn1.ObjectIdentifier{2, 5, 4, 12}
	oidINN       = asn1.ObjectIdentifier{1, 2, 643, 3, 131, 1, 1} // ИНН физлица; в старых сертификатах — и ИНН ЮЛ с ведущими 00
	oidOGRN      = asn1.ObjectIdentifier{1, 2, 643, 100, 1}
	oidSNILS     = asn1.ObjectIdentifier{1, 2, 643, 100, 3}
	oidINNLE     = asn1.ObjectIdentifier{1, 2, 643, 100, 4} // ИНН юрлица
	oidOGRNIP    = asn1.ObjectIdentifier{1, 2, 643, 100, 5}
)

// digests — хеши, которые профиль принимает. Остальные известные — слабые (MD5, SHA-1, SHA-224)
// или ГОСТ, которого в стеке нет: такая подпись не проверяется.
var digests = map[string]crypto.Hash{
	oidSHA256.String(): crypto.SHA256,
	oidSHA384.String(): crypto.SHA384,
	oidSHA512.String(): crypto.SHA512,
}

// weakDigests — известные хеши ниже требований профиля.
var weakDigests = map[string]string{
	oidMD5.String():    "MD5",
	oidSHA1.String():   "SHA-1",
	oidSHA224.String(): "SHA-224",
}

// hashName — имя хеша для отчёта.
func hashName(h crypto.Hash) string {
	switch h {
	case crypto.SHA1:
		return "SHA-1"
	case crypto.SHA256:
		return "SHA-256"
	case crypto.SHA384:
		return "SHA-384"
	case crypto.SHA512:
		return "SHA-512"
	}
	return h.String()
}

// isRussian — OID из российской ветки 1.2.643: алгоритмы ГОСТ.
func isRussian(oid asn1.ObjectIdentifier) bool {
	return len(oid) > len(oidRussianArc) && oid[:len(oidRussianArc)].Equal(oidRussianArc)
}
