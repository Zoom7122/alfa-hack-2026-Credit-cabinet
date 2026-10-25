// Package signature проверяет электронную подпись документа заёмщика: открепленную или
// присоединённую подпись CMS/CAdES над исходными байтами файла (формат файла не важен — PDF,
// XLS, XLSX…), сертификат подписанта по доверенным корням профиля, личность подписанта и его
// полномочия за заявителя, и выносит решение по правилам банка.
//
// Сейчас есть только профиль TEST: локальные тестовые корни и CRL, полномочия — по мокам
// ЕГРЮЛ и реестра МЧД. ГОСТ-алгоритмы и встроенные подписи PDF/XML не проверяются: такие
// подписи получают INDETERMINATE, а не VERIFIED. Верная подпись не подтверждает ни достоверность
// цифр в документе, ни платёжеспособность.
package signature

import (
	"bytes"
	"context"
	"crypto"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	// ProfileTest — тестовый профиль: тестовые корни, тестовые CRL, моки реестров.
	ProfileTest = "TEST"
	// TrustModeMock — signature_reports.trust_mode для профиля TEST.
	TrustModeMock = "MOCK"
)

const profileNotice = "Профиль TEST: подпись проверена локальными тестовыми корнями и тестовыми CRL, полномочия — " +
	"по мокам ЕГРЮЛ и реестра МЧД. Это не проверка УКЭП: аккредитация УЦ, признаки КЭП и ГОСТ-алгоритмы не проверяются, " +
	"поэтому вердикт не выше VALID_WITH_WARNINGS."

const scopeNote = "Подпись подтверждает только, что файл не изменён после подписания и кем он подписан. Она не " +
	"подтверждает достоверность цифр в документе и платёжеспособность заёмщика; данные документа в скоринг отсюда не передаются."

// limitations — чего профиль TEST не умеет; пишется в каждый отчёт.
var limitations = []string{
	"ГОСТ Р 34.10-2012 / 34.11-2012 не поддерживается: такие подписи — INDETERMINATE",
	"встроенные подписи PDF (PAdES) и XML (XMLDSig/XAdES) только обнаруживаются: INDETERMINATE",
	"метки времени (CAdES-T) и OCSP не проверяются: время подписи — заявленное подписантом, отзыв — по локальным CRL",
	"аккредитация УЦ и квалифицированность сертификата (УКЭП) не проверяются",
	"в контейнере проверяется только одна подпись; BER-кодирование не поддерживается",
}

// File — файл, как его прочитал worker.
type File struct {
	Name        string
	Data        []byte // nil — файл не прочитан, причина — в Unavailable
	Unavailable string
	SHA256      string // хеш Data, посчитан до проверки
	Recorded    string // documents.sha256 — хеш при загрузке
	Size        int64  // documents.size_bytes
}

// Input — что проверить.
type Input struct {
	DocumentType domain.DocumentType
	Container    Container
	Document     File  // подписанный файл; у ATTACHED не используется — документ внутри Signature
	Signature    *File // файл подписи; nil — подпись внутри документа (PDF_EMBEDDED, XML_ENVELOPED)
}

// Verifier проверяет подписи по профилю TEST. Безопасен для параллельного использования.
type Verifier struct {
	trust *TrustStore
	rules Rules
	now   func() time.Time
}

// Option настраивает Verifier (используется в тестах).
type Option func(*Verifier)

// WithClock подменяет время проверки.
func WithClock(now func() time.Time) Option { return func(v *Verifier) { v.now = now } }

func NewVerifier(trust *TrustStore, rules Rules, opts ...Option) *Verifier {
	v := &Verifier{trust: trust, rules: rules, now: time.Now}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify проверяет подпись по этапам: целостность, формат, сертификат, личность, полномочия —
// и выносит решение по правилам банка. load вызывается, только если подпись прошла
// криптографическую проверку и правила требуют сверить подписанта с заявителем; его ошибка —
// ошибка Verify (сбой хранилища, проверку надо повторить). Всё остальное — в отчёте.
func (v *Verifier) Verify(ctx context.Context, in Input, load ApplicantLoader) (*Report, error) {
	now := v.now().UTC()
	rule := v.rules.For(in.DocumentType)
	r := &Report{Profile: ProfileTest, ProfileNotice: profileNotice, TrustMode: TrustModeMock, RulesVersion: v.rules.Version,
		DocumentType: in.DocumentType, Container: in.Container, Format: "UNKNOWN", Policy: rule.Policy,
		ScopeNote: scopeNote, Limitations: limitations, VerifiedAt: now,
		FileName: in.Document.Name, DocumentSHA256: fileHash(in.Document)}
	if s := in.Signature; s != nil {
		r.SignatureFileName, r.SignatureSHA256 = s.Name, fileHash(*s)
		if in.Container == ContainerAttached { // документ — сам контейнер
			r.FileName, r.DocumentSHA256 = s.Name, fileHash(*s)
		}
	}
	e := &evaluation{v: v, in: in, rule: rule, now: now, r: r}
	if err := e.safeRun(ctx, load); err != nil {
		return nil, err
	}
	e.decide()
	return r, nil
}

// evaluation — одна проверка: вход, правило и отчёт, который она заполняет.
type evaluation struct {
	v       *Verifier
	in      Input
	rule    DocumentRule
	now     time.Time
	r       *Report
	claimed time.Time         // время подписи из signing-time; нулевое — не заявлено
	cert    *x509.Certificate // сертификат подписанта
}

// safeRun — run с защитой от паники: внутренняя ошибка проверки даёт INDETERMINATE во всех этапах,
// а не падение worker'а и не бесконечные повторы сообщения.
func (e *evaluation) safeRun(ctx context.Context, load ApplicantLoader) (err error) {
	defer func() {
		if p := recover(); p != nil {
			const reason = "внутренняя ошибка проверки"
			st := &e.r.Stages
			*st = Stages{}
			st.Integrity.add("INTERNAL_ERROR", statusIndeterminate, fmt.Sprintf("%s: %v", reason, p))
			st.Integrity.finish()
			st.Format.skip(reason)
			st.Certificate.skip(reason)
			e.skipParty(reason)
			e.r.InternalError, err = true, nil
		}
	}()
	return e.run(ctx, load)
}

func (e *evaluation) run(ctx context.Context, load ApplicantLoader) error {
	st := &e.r.Stages
	if !e.checkFiles() {
		reason := "нет файла"
		st.Format.skip(reason)
		st.Certificate.skip(reason)
		e.skipParty(reason)
		return nil
	}
	if e.in.Signature == nil {
		e.embedded()
		return nil
	}

	sd, err := parseCMS(e.in.Signature.Data)
	if err != nil {
		st.Format.add("FORMAT_PARSED", statusOf(err), err.Error())
		e.skipAfterFormat()
		return nil
	}
	e.r.Encoding = sd.encoding
	si, content, attrs, alg, ok := e.checkFormat(sd)
	if !ok {
		e.skipAfterFormat()
		return nil
	}
	e.claimed = attrs.signingTime
	e.r.Algorithm = alg.name
	e.r.SigningTime = &SigningTime{Source: "NONE"}
	if !e.claimed.IsZero() {
		t := e.claimed
		e.r.SigningTime = &SigningTime{Value: &t, Source: "SIGNED_ATTRIBUTE"}
	}

	e.cert = signerCertificate(sd, si)
	e.checkIntegrity(si, content, attrs, alg)
	e.checkBinding(si)
	st.Integrity.finish()
	st.Format.finish()
	if e.cert == nil {
		msg := "сертификата подписанта нет в контейнере"
		if sd.badCerts > 0 {
			msg += fmt.Sprintf(" (%d сертификатов не разобрано)", sd.badCerts)
		}
		st.Certificate.add("SIGNER_CERTIFICATE_FOUND", statusIndeterminate, msg)
		st.Certificate.finish()
		e.skipParty("сертификат подписанта не найден")
		return nil
	}
	e.checkCertificate(sd)
	st.Certificate.finish()
	return e.checkParty(ctx, load)
}

// checkFiles — файлы прочитаны и совпадают с загруженными (sha256 и размер из documents).
// false — какого-то файла нет: проверять нечего.
func (e *evaluation) checkFiles() bool {
	st := &e.r.Stages.Integrity
	type named struct {
		role string
		f    *File
	}
	var files []named
	if e.in.Container != ContainerAttached {
		files = append(files, named{"документа", &e.in.Document})
	}
	if e.in.Signature != nil {
		files = append(files, named{"подписи", e.in.Signature})
	}
	ok := true
	for _, x := range files {
		f := x.f
		if f.Data == nil {
			st.add("FILE_AVAILABLE", statusIndeterminate, fmt.Sprintf("файл %s %s", x.role, f.Unavailable))
			ok = false
			continue
		}
		st.add("FILE_AVAILABLE", statusVerified, fmt.Sprintf("файл %s прочитан: %d байт, sha256 %s", x.role, len(f.Data), f.SHA256))
		if f.SHA256 != f.Recorded || int64(len(f.Data)) != f.Size {
			st.add("STORED_FILE_UNCHANGED", statusInvalid, fmt.Sprintf("файл %s в хранилище не совпадает с загруженным: "+
				"sha256 %s, %d байт; при загрузке — %s, %d байт", x.role, f.SHA256, len(f.Data), f.Recorded, f.Size))
		} else {
			st.add("STORED_FILE_UNCHANGED", statusVerified, fmt.Sprintf("файл %s не менялся после загрузки", x.role))
		}
	}
	if !ok {
		st.finish()
	}
	return ok
}

// embedded — подпись внутри PDF или XML: найдена, но не поддерживается.
func (e *evaluation) embedded() {
	st := &e.r.Stages
	msg := "подпись внутри PDF (PAdES) не поддерживается: проверка подписанного диапазона /ByteRange и изменений, " +
		"внесённых после подписания, не реализована — считать такую подпись проверенной нельзя"
	e.r.Format = "PADES"
	if e.in.Container == ContainerXML {
		e.r.Format = "XMLDSIG"
		msg = "подпись внутри XML (XMLDSig/XAdES) не поддерживается: канонизация и проверка ссылок XMLDSig не реализованы"
	}
	st.Format.add("FORMAT_PARSED", statusIndeterminate, msg)
	st.Format.add("DOCUMENT_NOT_MODIFIED", statusIndeterminate, "охват подписи и изменения после подписания не проверялись")
	e.skipAfterFormat()
}

// skipAfterFormat — формат не разобран или не поддержан: целостность не проверить, дальше — не проверялось.
func (e *evaluation) skipAfterFormat() {
	st := &e.r.Stages
	st.Format.finish()
	reason := "формат подписи не поддержан"
	if st.Format.Status == statusInvalid {
		reason = "контейнер подписи повреждён"
	}
	st.Integrity.add("INTEGRITY", statusIndeterminate, "не проверялась: "+reason)
	st.Integrity.finish()
	st.Certificate.skip(reason)
	e.skipParty(reason)
}

// skipParty — личность и полномочия не проверялись (или не нужны по правилам).
func (e *evaluation) skipParty(reason string) {
	st := &e.r.Stages
	if e.rule.Identity {
		st.Identity.skip(reason)
	} else {
		st.Identity.notRequired(e.notRequiredReason("сверять подписанта с заявителем"))
	}
	if e.rule.Authority {
		st.Authority.skip(reason)
	} else {
		st.Authority.notRequired(e.notRequiredReason("проверять полномочия подписанта"))
	}
}

func (e *evaluation) notRequiredReason(what string) string {
	return fmt.Sprintf("правила банка для документа %s не требуют %s: его подписывает не заявитель, а третья сторона",
		e.in.DocumentType, what)
}

// checkFormat — этап «формат и охват»: одна подпись CMS над данными документа, подписанные
// атрибуты CAdES, поддерживаемый алгоритм. Возвращает подпись, подписанные байты и атрибуты;
// false — дальше проверять нечего.
func (e *evaluation) checkFormat(sd *signedData) (si signerInfo, content []byte, attrs signedAttributes, alg algorithm, ok bool) {
	st := &e.r.Stages.Format
	st.add("FORMAT_PARSED", statusVerified, "контейнер CMS SignedData, "+sd.encoding)
	switch n := len(sd.signers); {
	case n == 0:
		st.add("SINGLE_SIGNER", statusInvalid, "в контейнере нет ни одной подписи")
		return
	case n > 1:
		st.add("SINGLE_SIGNER", statusIndeterminate, fmt.Sprintf("в контейнере %d подписи: MVP проверяет только контейнер с одной подписью", n))
		return
	}
	st.add("SINGLE_SIGNER", statusVerified, "в контейнере одна подпись")
	si = sd.signers[0]
	if !sd.contentType.Equal(oidData) {
		st.add("CONTENT_TYPE", statusIndeterminate, fmt.Sprintf("подписаны не данные документа: eContentType %s", sd.contentType))
		return
	}

	switch doc := e.in.Document.Data; {
	case e.in.Container == ContainerAttached && sd.content == nil:
		st.add("SIGNED_DATA_COVERAGE", statusIndeterminate, "это открепленная подпись, а подписанный файл не загружен в паре с ней")
		return
	case e.in.Container == ContainerAttached:
		content = sd.content
		st.add("SIGNED_DATA_COVERAGE", statusVerified, fmt.Sprintf("присоединённая подпись: документ внутри контейнера, %d байт, "+
			"sha256 %s", len(content), sha256Hex(content)))
	case sd.content == nil:
		content = doc
		st.add("SIGNED_DATA_COVERAGE", statusVerified, fmt.Sprintf("открепленная подпись над всеми байтами файла (%d байт); "+
			"формат файла не важен", len(doc)))
	case bytes.Equal(sd.content, doc):
		content = doc
		st.add("SIGNED_DATA_COVERAGE", statusVerified, "внутри контейнера тот же документ, что загружен в паре, байт в байт")
	default:
		st.add("SIGNED_DATA_COVERAGE", statusInvalid, "внутри контейнера подписан другой документ, а не загруженный в паре файл")
		return
	}
	st.add("DOCUMENT_NOT_MODIFIED", statusNotRequired, "подпись CMS охватывает документ целиком: дописать что-то после "+
		"подписания, как в PDF с инкрементальными изменениями, нельзя — любое изменение байтов ломает хеш")

	if si.signedAttrs == nil {
		st.add("SIGNED_ATTRIBUTES", statusIndeterminate, "нет подписанных атрибутов: подпись PKCS#7 без атрибутов — не CAdES, профиль её не проверяет")
		return
	}
	attrs, err := parseSignedAttributes(si.attrs)
	switch {
	case err != nil:
		st.add("SIGNED_ATTRIBUTES", statusInvalid, err.Error())
		return
	case !attrs.contentType.Equal(sd.contentType):
		st.add("SIGNED_ATTRIBUTES", statusInvalid, "атрибут content-type не совпадает с типом подписанных данных")
		return
	}
	st.add("SIGNED_ATTRIBUTES", statusVerified, "подписанные атрибуты content-type и message-digest на месте")

	if alg, err = resolveAlgorithm(si); err != nil {
		st.add("ALGORITHM_SUPPORTED", statusOf(err), err.Error())
		return
	}
	st.add("ALGORITHM_SUPPORTED", statusVerified, "алгоритм "+alg.name+" (профиль TEST, не ГОСТ)")

	if _, n := find(si.unsignedAttrs, oidTimeStamp); n > 0 {
		st.warn("есть метка времени (CAdES-T), но она не проверялась: время подписи — заявленное подписантом")
	}
	if _, n := find(si.unsignedAttrs, oidCounterSign); n > 0 {
		st.warn("есть заверяющая подпись (countersignature) — она не проверялась")
	}
	if e.in.Container == ContainerDetached && EmbeddedSignature(e.in.Document.Data) != "" {
		st.warn("в самом документе есть встроенная подпись — она не проверялась, проверена только открепленная")
	}
	return si, content, attrs, alg, true
}

// checkIntegrity — этап «целостность»: хеш документа совпадает с message-digest, значение
// подписи над подписанными атрибутами верно для ключа сертификата.
func (e *evaluation) checkIntegrity(si signerInfo, content []byte, attrs signedAttributes, alg algorithm) {
	st := &e.r.Stages.Integrity
	h := alg.hash.New()
	h.Write(content)
	if bytes.Equal(h.Sum(nil), attrs.messageDigest) {
		st.add("INTEGRITY", statusVerified, fmt.Sprintf("хеш %s документа совпадает с подписанным", hashName(alg.hash)))
	} else {
		st.add("INTEGRITY", statusInvalid, "хеш документа не совпадает с подписанным (message-digest): файл изменён после "+
			"подписания или подпись относится к другому документу")
	}
	if e.cert == nil {
		st.add("SIGNATURE_VALUE", statusIndeterminate, "нет сертификата подписанта: значение подписи нечем проверить")
		return
	}
	switch err := verifySignature(alg, e.cert, si.signedAttrs, si.signature); {
	case err == nil:
		st.add("SIGNATURE_VALUE", statusVerified, "значение подписи верно для ключа сертификата "+displayName(e.cert))
	case errors.Is(err, errSignatureMismatch):
		st.add("SIGNATURE_VALUE", statusInvalid, err.Error()+": подпись повреждена или подписанные атрибуты изменены")
	default:
		st.add("SIGNATURE_VALUE", statusOf(err), err.Error())
	}
}

// checkBinding — атрибут signing-certificate(-v2) указывает на сертификат подписанта (CAdES-BES).
// Без атрибута подпись — обычный CMS: это замечание, а не отказ.
func (e *evaluation) checkBinding(si signerInfo) {
	st := &e.r.Stages.Format
	const code = "SIGNING_CERTIFICATE_BINDING"
	id, found, err := parseSigningCertificate(si.attrs)
	switch {
	case err != nil:
		st.add(code, statusInvalid, err.Error())
		return
	case !found:
		e.r.Format = "CMS"
		st.add(code, statusNotRequired, "нет атрибута signing-certificate-v2: подпись CMS без привязки к сертификату, не CAdES-BES")
		st.warn("подпись без signing-certificate-v2: сертификат подписанта не закреплён подписью (не CAdES-BES)")
		return
	case e.cert == nil:
		st.add(code, statusIndeterminate, "сертификата подписанта нет: привязку не проверить")
		return
	}
	var sum []byte
	switch {
	case id.hash.Equal(oidSHA1) && !id.v2:
		s := sha1.Sum(e.cert.Raw)
		sum = s[:]
	default:
		hh, ok := digests[id.hash.String()]
		if !ok {
			st.add(code, statusIndeterminate, fmt.Sprintf("хеш %s в signing-certificate-v2 не поддерживается", id.hash))
			return
		}
		sum = hashOf(hh, e.cert.Raw)
	}
	if !bytes.Equal(sum, id.digest) {
		st.add(code, statusInvalid, "атрибут signing-certificate указывает на другой сертификат, чем тот, которым проверена подпись")
		return
	}
	e.r.Format = "CADES_BES"
	st.add(code, statusVerified, "подписанный атрибут signing-certificate-v2 указывает на сертификат подписанта (CAdES-BES)")
}

// checkCertificate — этап «сертификат»: цепочка до доверенного корня, срок действия, назначение
// ключа, отзыв. Аккредитация и признаки КЭП в профиле TEST не проверяются.
func (e *evaluation) checkCertificate(sd *signedData) {
	st := &e.r.Stages.Certificate
	cert := e.cert
	st.add("SIGNER_CERTIFICATE_FOUND", statusVerified, "сертификат подписанта: "+displayName(cert))
	rep := certificateReport(cert)
	e.r.Certificate = rep

	chain := e.v.trust.buildChain(cert, sd.certificates, e.now)
	st.add("CHAIN_TO_TRUSTED_ROOT", chain.status, chain.msg)
	validity := []*x509.Certificate{cert}
	if chain.chain != nil {
		validity = chain.chain
		for _, c := range chain.chain {
			rep.Chain = append(rep.Chain, displayName(c))
		}
	}
	e.checkValidity(st, validity)
	st.add("SIGNING_TIME_TRUSTED", statusNotRequired, "время подписи заявлено подписантом (signing-time); метки времени TSP профиль не проверяет")
	st.warn("время подписи не подтверждено доверенной меткой времени")

	switch {
	case cert.IsCA:
		st.add("KEY_USAGE", statusInvalid, "это сертификат УЦ, а не подписанта")
	case cert.KeyUsage == 0:
		st.add("KEY_USAGE", statusInvalid, "в сертификате нет расширения keyUsage: назначение ключа не указано")
	case cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0:
		st.add("KEY_USAGE", statusInvalid, "ключ не предназначен для подписи документов: нет digitalSignature и nonRepudiation")
	default:
		st.add("KEY_USAGE", statusVerified, "назначение ключа: "+strings.Join(rep.KeyUsage, ", "))
	}

	if chain.chain == nil {
		st.add("NOT_REVOKED", statusIndeterminate, "не проверялся: цепочка до доверенного корня не построена")
		rep.Revocation = &Revocation{Status: "UNKNOWN", Method: "NONE", CheckedAt: e.now}
	}
	for i := 0; chain.chain != nil && i < len(chain.chain)-1; i++ {
		rv := e.v.trust.revocationStatus(chain.chain[i], chain.chain[i+1], e.now)
		status := statusVerified
		switch rv.status {
		case "REVOKED":
			status = statusInvalid
		case "UNKNOWN":
			status = statusIndeterminate
		}
		st.add("NOT_REVOKED", status, rv.msg)
		if i == 0 {
			rep.Revocation = &Revocation{Status: rv.status, Method: "CRL", CheckedAt: e.now}
			if rv.status == "UNKNOWN" {
				rep.Revocation.Method = "NONE"
			}
			if !rv.revokedAt.IsZero() {
				t := rv.revokedAt
				rep.Revocation.RevokedAt = &t
			}
		}
	}
	st.add("ISSUER_ACCREDITED", statusNotRequired, "профиль TEST не проверяет аккредитацию УЦ (перечень Минцифры): это не проверка УКЭП")
	st.add("QUALIFIED_ATTRIBUTES", statusNotRequired, "профиль TEST не проверяет признаки КЭП (subjectSignTool, issuerSignTool, классы КС)")
}

// checkValidity — срок действия сертификатов цепочки. Время подписи заявлено подписантом и не
// подтверждено: если сертификат истёк уже после него — INDETERMINATE, если подпись заявлена вне
// срока или срок истёк до неё — INVALID.
func (e *evaluation) checkValidity(st *Stage, chain []*x509.Certificate) {
	const code = "CERT_VALID_AT_SIGNING_TIME"
	leaf, day := chain[0], func(t time.Time) string { return t.UTC().Format(time.DateOnly) }
	if t := e.claimed; !t.IsZero() && (t.Before(leaf.NotBefore) || t.After(leaf.NotAfter)) {
		st.add(code, statusInvalid, fmt.Sprintf("подпись заявлена %s — вне срока действия сертификата (%s — %s)",
			day(t), day(leaf.NotBefore), day(leaf.NotAfter)))
		return
	}
	for _, c := range chain {
		switch {
		case e.now.Before(c.NotBefore):
			st.add(code, statusInvalid, fmt.Sprintf("сертификат %s ещё не действует (с %s)", displayName(c), day(c.NotBefore)))
			return
		case e.now.After(c.NotAfter) && !e.claimed.IsZero() && validAt(chain, e.claimed):
			st.add(code, statusIndeterminate, fmt.Sprintf("сертификат %s истёк %s; подпись заявлена %s, в срок действия, но "+
				"без доверенной метки времени это не доказано", displayName(c), day(c.NotAfter), day(e.claimed)))
			return
		case e.now.After(c.NotAfter):
			st.add(code, statusInvalid, fmt.Sprintf("сертификат %s истёк %s", displayName(c), day(c.NotAfter)))
			return
		}
	}
	st.add(code, statusVerified, fmt.Sprintf("сертификат действует: %s — %s", day(leaf.NotBefore), day(leaf.NotAfter)))
}

func validAt(chain []*x509.Certificate, t time.Time) bool {
	for _, c := range chain {
		if t.Before(c.NotBefore) || t.After(c.NotAfter) {
			return false
		}
	}
	return true
}

// checkParty — этапы «личность» и «полномочия». Проверяются, только если подпись верна, а
// сертификат не отвергнут: иначе неизвестно, кто на самом деле подписал.
func (e *evaluation) checkParty(ctx context.Context, load ApplicantLoader) error {
	st := &e.r.Stages
	s := identityOf(e.cert)
	e.r.Signer = s.report()
	if !e.rule.Identity && !e.rule.Authority {
		e.skipParty("")
		return nil
	}
	switch {
	case !st.Integrity.ok() || !st.Format.ok():
		e.skipParty("подпись не прошла проверку целостности или формата")
		return nil
	case st.Certificate.Status == statusInvalid:
		e.skipParty("сертификат подписанта недействителен")
		return nil
	}
	a, err := load(ctx)
	if err != nil {
		return err
	}
	if !e.rule.Identity {
		st.Identity.notRequired(e.notRequiredReason("сверять подписанта с заявителем"))
	} else {
		checkIdentity(&st.Identity, s, a)
		st.Identity.finish()
	}
	switch {
	case !e.rule.Authority:
		st.Authority.notRequired(e.notRequiredReason("проверять полномочия подписанта"))
	case st.Identity.Status != statusVerified:
		st.Authority.skip("личность подписанта не подтверждена")
	default:
		e.r.Authority = checkAuthority(&st.Authority, s, a, e.rule, e.now, e.claimed)
		st.Authority.finish()
		if e.r.Authority.Basis == "REGISTRY_HEAD" {
			e.r.Signer.Kind = "LEGAL_ENTITY_HEAD"
		}
	}
	return nil
}

// decide — итог по правилам банка: INVALID, если хоть один этап INVALID; иначе INDETERMINATE,
// если хоть один этап INDETERMINATE; иначе VERIFIED. Причина — первый непройденный этап.
func (e *evaluation) decide() {
	st := &e.r.Stages
	stages := []struct {
		name string
		s    *Stage
	}{
		{"целостность", &st.Integrity}, {"формат", &st.Format}, {"сертификат", &st.Certificate},
		{"личность", &st.Identity}, {"полномочия", &st.Authority},
	}
	for _, x := range stages {
		if !x.s.closed {
			x.s.finish()
		}
	}
	// причина — первый этап с худшим статусом; этап, не проверявшийся из-за другого, уступает причине
	decision, reason := statusVerified, ""
	for _, want := range []domain.VerificationStatus{statusInvalid, statusIndeterminate} {
		var cause *Stage
		for _, x := range stages {
			if x.s.Status == want && (cause == nil || derived(cause.Reason) && !derived(x.s.Reason)) {
				cause, reason = x.s, x.name+": "+x.s.Reason
			}
		}
		if cause == nil {
			continue
		}
		decision = want
		for _, c := range cause.Checks {
			if c.Status == want {
				e.r.FailedCheck = c.Code
				break
			}
		}
		break
	}

	who := "только подпись и сертификат"
	switch {
	case e.rule.Authority && e.rule.POAPower != "":
		who = "подпись, сертификат, личность и полномочия (руководитель, ИП или МЧД с " + e.rule.POAPower + ")"
	case e.rule.Authority:
		who = "подпись, сертификат, личность и полномочия (только руководитель)"
	case e.rule.Identity:
		who = "подпись, сертификат и личность"
	}
	st.Decision = Stage{Status: decision, Reason: reason, closed: true, Checks: []Check{{Code: "BANK_RULES", Status: decision,
		Message: fmt.Sprintf("правила %s, документ %s, политика %s: нужны %s", e.v.rules.Version, e.in.DocumentType, e.rule.Policy, who)}}}

	e.r.Decision, e.r.FailureReason = decision, reason
	switch decision {
	case statusVerified:
		// профиль TEST не подтверждает квалифицированную подпись: VALID оставлен для проверки УКЭП
		e.r.Verdict = "VALID_WITH_WARNINGS"
		st.Decision.warn("профиль TEST: вердикт VALID_WITH_WARNINGS, а не VALID — это не проверка УКЭП")
		e.r.PolicyResult = PolicyResult{Satisfied: true, Message: "подпись проверена по профилю TEST и правилам " + e.v.rules.Version}
	default:
		e.r.Verdict = string(decision)
		e.r.PolicyResult = PolicyResult{Satisfied: false, Message: reason}
	}
}

// derived — этап не проверялся из-за другого этапа («не проверялось: …»).
func derived(reason string) bool {
	return strings.HasPrefix(reason, "не проверял")
}

// fileHash — sha256 прочитанного файла; если файл не прочитан — хеш из documents (версия документа).
func fileHash(f File) string {
	if f.SHA256 != "" {
		return f.SHA256
	}
	return f.Recorded
}

// signerCertificate — сертификат подписанта из контейнера по issuerAndSerialNumber или subjectKeyIdentifier.
func signerCertificate(sd *signedData, si signerInfo) *x509.Certificate {
	for _, c := range sd.certificates {
		switch {
		case si.issuer != nil:
			if bytes.Equal(c.RawIssuer, si.issuer) && c.SerialNumber.Cmp(si.serial) == 0 {
				return c
			}
		case len(si.subjectKeyID) > 0 && bytes.Equal(c.SubjectKeyId, si.subjectKeyID):
			return c
		}
	}
	return nil
}

func certificateReport(c *x509.Certificate) *CertificateReport {
	thumb := sha1.Sum(c.Raw)
	rep := &CertificateReport{Subject: nameString(c.Subject), SerialNumber: hex.EncodeToString(c.SerialNumber.Bytes()),
		Thumbprint: hex.EncodeToString(thumb[:]), SHA256: sha256Hex(c.Raw), ValidFrom: c.NotBefore.UTC(),
		ValidTo: c.NotAfter.UTC(), Issuer: CertificateIssuer{Name: nameString(c.Issuer)}, KeyAlgorithm: keyName(c)}
	for _, ku := range []struct {
		bit  x509.KeyUsage
		name string
	}{
		{x509.KeyUsageDigitalSignature, "digitalSignature"}, {x509.KeyUsageContentCommitment, "nonRepudiation"},
		{x509.KeyUsageKeyEncipherment, "keyEncipherment"}, {x509.KeyUsageDataEncipherment, "dataEncipherment"},
		{x509.KeyUsageKeyAgreement, "keyAgreement"}, {x509.KeyUsageCertSign, "keyCertSign"}, {x509.KeyUsageCRLSign, "cRLSign"},
	} {
		if c.KeyUsage&ku.bit != 0 {
			rep.KeyUsage = append(rep.KeyUsage, ku.name)
		}
	}
	return rep
}

// nameAttrs — подписи атрибутов имени для отчёта. Атрибутов не из списка (почта, адрес) в отчёте нет.
var nameAttrs = []struct {
	oid   asn1.ObjectIdentifier
	label string
}{
	{asn1.ObjectIdentifier{2, 5, 4, 3}, "CN"}, {oidSurname, "SN"}, {oidGivenName, "G"}, {oidTitle, "T"},
	{asn1.ObjectIdentifier{2, 5, 4, 10}, "O"}, {asn1.ObjectIdentifier{2, 5, 4, 11}, "OU"}, {asn1.ObjectIdentifier{2, 5, 4, 6}, "C"},
	{oidINN, "ИНН"}, {oidSNILS, "СНИЛС"}, {oidINNLE, "ИНН ЮЛ"}, {oidOGRN, "ОГРН"}, {oidOGRNIP, "ОГРНИП"},
}

// nameString — имя субъекта или издателя по-русски: «CN=…, SN=…, ИНН=…». СНИЛС маскируется,
// почта и адрес не выводятся (pkix.Name.String кодирует такие атрибуты в hex — СНИЛС оттуда читается).
func nameString(n pkix.Name) string {
	var parts []string
	for _, a := range nameAttrs {
		for _, atv := range n.Names {
			v, ok := atv.Value.(string)
			if !ok || !atv.Type.Equal(a.oid) {
				continue
			}
			if a.oid.Equal(oidSNILS) {
				v = maskSNILS(v)
			}
			parts = append(parts, a.label+"="+v)
		}
	}
	return strings.Join(parts, ", ")
}

// statusOf — статус проверки по ошибке: формат или алгоритм не поддержан — INDETERMINATE, иначе INVALID.
func statusOf(err error) domain.VerificationStatus {
	var fe *formatError
	if errors.As(err, &fe) && fe.unsupported {
		return statusIndeterminate
	}
	return statusInvalid
}

func hashOf(h crypto.Hash, data []byte) []byte {
	hh := h.New()
	hh.Write(data)
	return hh.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
