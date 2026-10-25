package signature

import (
	"context"
	"crypto/x509"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Applicant — компания-заявитель и данные реестров, по которым проверяется подписант.
// В профиле TEST реестры — ответы mock-api, собранные data-collector'ом (raw_collections).
type Applicant struct {
	INN  string
	Type domain.CompanyType
	OGRN string // пусто — ещё неизвестен
	Name string

	Heads            []domain.RegistryHead // руководители по ЕГРЮЛ
	HeadsKnown       bool                  // ЕГРЮЛ ответил при сборе
	HeadsUnavailable string                // почему данных ЕГРЮЛ нет

	POAs            []domain.PowerOfAttorney // МЧД, где доверитель — заявитель
	POAsKnown       bool                     // реестр МЧД ответил при сборе
	POAsUnavailable string                   // почему данных реестра МЧД нет
	POAScopeINN     string                   // МЧД искали только на этого представителя (ИНН из анкеты)

	Source string // откуда данные реестров: «сбор col_…»
}

// ApplicantLoader загружает заявителя и данные реестров. Вызывается, только когда подпись прошла
// криптографическую проверку и правила требуют сопоставить подписанта с заявителем: загрузка
// может ждать завершения сбора данных. Ошибка — сбой хранилища, проверку надо повторить.
type ApplicantLoader func(ctx context.Context) (Applicant, error)

// signerIdentity — подписант по атрибутам субъекта сертификата.
type signerIdentity struct {
	surname, givenName, commonName, title, organization string
	personINN, snils, orgINN, ogrn, ogrnip              string
}

// identityOf читает подписанта из сертификата: ФИО (SN, GN или CN), должность, организация,
// ИНН ФЛ, СНИЛС, ИНН ЮЛ, ОГРН, ОГРНИП.
func identityOf(cert *x509.Certificate) signerIdentity {
	s := signerIdentity{commonName: strings.TrimSpace(cert.Subject.CommonName)}
	if len(cert.Subject.Organization) > 0 {
		s.organization = strings.TrimSpace(cert.Subject.Organization[0])
	}
	var legacyOrgINN string
	for _, atv := range cert.Subject.Names {
		v, ok := atv.Value.(string)
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch {
		case atv.Type.Equal(oidSurname):
			s.surname = v
		case atv.Type.Equal(oidGivenName):
			s.givenName = v
		case atv.Type.Equal(oidTitle):
			s.title = v
		case atv.Type.Equal(oidINN):
			// в старых сертификатах сюда же писали ИНН юрлица: 12 цифр с ведущими 00
			if len(v) == 12 && strings.HasPrefix(v, "00") {
				legacyOrgINN = v[2:]
			} else {
				s.personINN = v
			}
		case atv.Type.Equal(oidINNLE):
			s.orgINN = v
		case atv.Type.Equal(oidOGRN):
			s.ogrn = v
		case atv.Type.Equal(oidOGRNIP):
			s.ogrnip = v
		case atv.Type.Equal(oidSNILS):
			s.snils = v
		}
	}
	if s.orgINN == "" {
		s.orgINN = legacyOrgINN
	}
	return s
}

// fullName — «Фамилия Имя Отчество»: SN + GN, иначе CN.
func (s signerIdentity) fullName() string {
	if s.surname != "" {
		return strings.Join(strings.Fields(s.surname+" "+s.givenName), " ")
	}
	return s.commonName
}

func (s signerIdentity) report() *SignerReport {
	r := &SignerReport{Kind: "INDIVIDUAL", FullName: s.fullName(), Position: s.title, INNIndividual: s.personINN,
		SNILSMasked: maskSNILS(s.snils), OGRNIP: s.ogrnip}
	switch {
	case s.ogrnip != "":
		r.Kind = "SOLE_PROPRIETOR"
	case s.orgINN != "" || s.ogrn != "":
		r.Kind = "LEGAL_ENTITY_EMPLOYEE"
		r.Organization = &Organization{Name: s.organization, INN: s.orgINN, OGRN: s.ogrn}
	}
	return r
}

// maskSNILS оставляет последние 5 цифр: «***-***-907 41».
func maskSNILS(snils string) string {
	digits := onlyDigits(snils)
	if len(digits) != 11 {
		return ""
	}
	return "***-***-" + digits[6:9] + " " + digits[9:]
}

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func digitsOfLen(s string, n int) bool {
	return len(s) == n && onlyDigits(s) == s
}

// checkIdentity — этап «личность подписанта»: подписант опознаётся по ИНН ФЛ или СНИЛС,
// а данные организации или ИП в сертификате не противоречат заявителю.
func checkIdentity(st *Stage, s signerIdentity, a Applicant) {
	name := s.fullName()
	switch {
	case s.personINN != "" && !digitsOfLen(s.personINN, 12):
		st.add("SIGNER_IDENTIFIED", statusIndeterminate, fmt.Sprintf("ИНН физлица в сертификате некорректен: %q", s.personINN))
	case s.personINN != "":
		st.add("SIGNER_IDENTIFIED", statusVerified, fmt.Sprintf("подписант %s, ИНН %s", name, s.personINN))
	case len(onlyDigits(s.snils)) == 11:
		st.add("SIGNER_IDENTIFIED", statusVerified, fmt.Sprintf("подписант %s, СНИЛС %s (ИНН в сертификате нет)", name, maskSNILS(s.snils)))
	default:
		st.add("SIGNER_IDENTIFIED", statusIndeterminate, "в сертификате нет ИНН физлица и СНИЛС: подписанта нельзя сопоставить с заявителем")
	}
	if a.INN == "" {
		st.add("SIGNER_MATCHES_APPLICANT", statusIndeterminate, "нет данных о заявителе")
		return
	}

	const code = "SIGNER_MATCHES_APPLICANT"
	switch {
	case s.ogrnip != "":
		switch {
		case a.Type != domain.CompanySoleProprietor:
			st.add(code, statusInvalid, fmt.Sprintf("сертификат индивидуального предпринимателя (ОГРНИП %s), а заявитель — юрлицо ИНН %s", s.ogrnip, a.INN))
		case s.personINN != a.INN:
			st.add(code, statusInvalid, fmt.Sprintf("сертификат другого ИП: ИНН %s, заявитель — ИНН %s", s.personINN, a.INN))
		case a.OGRN != "" && s.ogrnip != a.OGRN:
			st.add(code, statusInvalid, fmt.Sprintf("ОГРНИП в сертификате %s, у заявителя — %s", s.ogrnip, a.OGRN))
		default:
			st.add(code, statusVerified, "сертификат выдан самому индивидуальному предпринимателю — заявителю")
		}
	case s.orgINN != "" || s.ogrn != "":
		switch {
		case a.Type != domain.CompanyLegalEntity:
			st.add(code, statusInvalid, fmt.Sprintf("сертификат сотрудника организации ИНН %s, а заявитель — ИП ИНН %s", s.orgINN, a.INN))
		case s.orgINN != "" && s.orgINN != a.INN:
			st.add(code, statusInvalid, fmt.Sprintf("сертификат выдан для другой организации: ИНН ЮЛ %s, заявитель — ИНН %s", s.orgINN, a.INN))
		case s.ogrn != "" && a.OGRN != "" && s.ogrn != a.OGRN:
			st.add(code, statusInvalid, fmt.Sprintf("ОГРН в сертификате %s не совпадает с ОГРН заявителя %s", s.ogrn, a.OGRN))
		case s.orgINN == "":
			st.add(code, statusIndeterminate, "в сертификате организации нет ИНН ЮЛ: сопоставить с заявителем можно только по ОГРН")
		default:
			st.add(code, statusVerified, fmt.Sprintf("организация в сертификате — заявитель (ИНН %s)", a.INN))
		}
	case a.Type == domain.CompanySoleProprietor && s.personINN == a.INN:
		st.add(code, statusVerified, "подписант — сам индивидуальный предприниматель (ИНН совпадает с заявителем)")
	default:
		st.add(code, statusVerified, "сертификат физлица без данных организации: противоречий с заявителем нет, "+
			"право подписи за заявителя проверяется на этапе полномочий")
	}
}

// checkAuthority — этап «полномочия»: ИП подписывает сам за себя; за юрлицо — руководитель по
// ЕГРЮЛ; остальные — по действующей МЧД от заявителя с полномочием для этого типа документа.
// Нет данных реестров — INDETERMINATE, а не отказ.
func checkAuthority(st *Stage, s signerIdentity, a Applicant, rule DocumentRule, now, claimed time.Time) *AuthorityReport {
	rep := &AuthorityReport{Basis: "NONE", ApplicantINN: a.INN, ApplicantType: string(a.Type), RequiredPower: rule.POAPower,
		Source: a.Source}
	const code = "SIGNER_AUTHORITY"

	if a.Type == domain.CompanySoleProprietor && s.personINN != "" && s.personINN == a.INN {
		st.add(code, statusVerified, "индивидуальный предприниматель подписывает сам за себя")
		rep.Basis = "SOLE_PROPRIETOR"
		return rep
	}

	// руководитель юрлица по ЕГРЮЛ; headUnknown — по реестру нельзя сказать, руководитель ли подписант
	var headUnknown string
	if a.Type == domain.CompanyLegalEntity {
		switch status, msg := isHead(s, a); status {
		case statusVerified:
			st.add(code, statusVerified, msg)
			rep.Basis = "REGISTRY_HEAD"
			return rep
		case statusIndeterminate:
			headUnknown = msg
		}
	}

	// представитель по МЧД
	if rule.POAPower == "" {
		if headUnknown != "" {
			st.add(code, statusIndeterminate, headUnknown+"; по МЧД этот документ подписывать нельзя")
		} else {
			st.add(code, statusInvalid, "этот документ подписывает только руководитель заявителя, а подписант им не является")
		}
		return rep
	}
	if !a.POAsKnown {
		msg := "нет данных реестра МЧД"
		if a.POAsUnavailable != "" {
			msg += ": " + a.POAsUnavailable
		}
		if headUnknown != "" {
			msg = headUnknown + "; " + msg
		}
		st.add(code, statusIndeterminate, msg)
		return rep
	}

	var candidates []domain.PowerOfAttorney
	for _, p := range a.POAs {
		sameINN := s.personINN != "" && p.RepresentativeINN == s.personINN
		sameSNILS := s.snils != "" && onlyDigits(p.RepresentativeSNILS) == onlyDigits(s.snils)
		if p.PrincipalINN == a.INN && (sameINN || sameSNILS) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		switch {
		case a.POAScopeINN != "" && a.POAScopeINN != s.personINN:
			st.add(code, statusIndeterminate, fmt.Sprintf("реестр МЧД запрашивали только по представителю ИНН %s из анкеты: "+
				"о доверенностях подписанта данных нет", a.POAScopeINN))
		case headUnknown != "":
			st.add(code, statusIndeterminate, headUnknown+"; МЧД на подписанта от заявителя в реестре нет")
		default:
			st.add(code, statusInvalid, "подписант не руководитель заявителя и у него нет МЧД от заявителя")
		}
		return rep
	}

	rep.Basis = "POWER_OF_ATTORNEY"
	best, status, msg := choosePOA(candidates, rule.POAPower, now, claimed)
	rep.PowerOfAttorney = &PowerOfAttorney{Number: best.Number, RegistryStatus: best.Status, ValidFrom: best.ValidFrom,
		ValidTo: best.ValidUntil, RevokedAt: best.RevokedAt, Powers: nonNilStrings(best.Powers)}
	st.add("POA_VALID", status, msg)
	if status == statusVerified {
		st.add(code, statusVerified, "подписант действует по МЧД "+best.Number)
	}
	return rep
}

// isHead — руководитель ли подписант по ЕГРЮЛ: совпадение ИНН — да; ИНН руководителей известны
// и не совпали — нет (INVALID); в выписке только ФИО и оно совпало — сказать нельзя (INDETERMINATE);
// данных ЕГРЮЛ нет — INDETERMINATE.
func isHead(s signerIdentity, a Applicant) (domain.VerificationStatus, string) {
	if !a.HeadsKnown {
		msg := "нет данных ЕГРЮЛ о руководителе"
		if a.HeadsUnavailable != "" {
			msg += ": " + a.HeadsUnavailable
		}
		return statusIndeterminate, msg
	}
	byINN := false
	for _, h := range a.Heads {
		if h.INN == "" {
			continue
		}
		byINN = true
		if s.personINN != "" && h.INN == s.personINN {
			return statusVerified, fmt.Sprintf("подписант — руководитель по ЕГРЮЛ: %s %s", strings.ToLower(h.Position), h.FullName)
		}
	}
	if byINN {
		return statusInvalid, "подписант не руководитель по ЕГРЮЛ"
	}
	for _, h := range a.Heads {
		if normName(h.FullName) != "" && normName(h.FullName) == normName(s.fullName()) {
			return statusIndeterminate, "ФИО подписанта совпадает с руководителем по ЕГРЮЛ, но ИНН руководителя в выписке нет"
		}
	}
	return statusInvalid, "подписант не руководитель по ЕГРЮЛ"
}

// choosePOA выбирает МЧД подписанта: действующую сейчас и с нужным полномочием — VERIFIED;
// действующую без полномочия — INVALID; действовавшую только на заявленное время подписи —
// INDETERMINATE (время подписи не подтверждено меткой TSP); иначе — INVALID.
func choosePOA(poas []domain.PowerOfAttorney, power string, now, claimed time.Time) (domain.PowerOfAttorney, domain.VerificationStatus, string) {
	for _, p := range poas {
		if ok, _ := poaActive(p, now); ok && slices.Contains(p.Powers, power) {
			return p, statusVerified, fmt.Sprintf("МЧД %s действует до %s, полномочие %s есть", p.Number, p.ValidUntil, power)
		}
	}
	for _, p := range poas {
		if ok, _ := poaActive(p, now); ok {
			return p, statusInvalid, fmt.Sprintf("МЧД %s действует, но не даёт полномочия %s (есть: %s)", p.Number, power,
				strings.Join(p.Powers, ", "))
		}
	}
	if !claimed.IsZero() {
		for _, p := range poas {
			if ok, _ := poaActive(p, claimed); ok && slices.Contains(p.Powers, power) {
				return p, statusIndeterminate, fmt.Sprintf("МЧД %s действовала на заявленное время подписи %s, но сейчас нет; "+
					"без доверенной метки времени это не доказано", p.Number, claimed.Format(time.DateOnly))
			}
		}
	}
	_, why := poaActive(poas[0], now)
	return poas[0], statusInvalid, fmt.Sprintf("у подписанта нет действующей МЧД: %s %s", poas[0].Number, why)
}

// poaActive — действует ли МЧД на момент at: в сроке и не отозвана.
func poaActive(p domain.PowerOfAttorney, at time.Time) (bool, string) {
	day := at.UTC().Truncate(24 * time.Hour)
	from, errFrom := time.Parse(time.DateOnly, p.ValidFrom)
	until, errUntil := time.Parse(time.DateOnly, p.ValidUntil)
	switch {
	case p.Status == "REVOKED" && p.RevokedAt == "":
		return false, "отозвана"
	case p.RevokedAt != "":
		if revoked, err := time.Parse(time.DateOnly, p.RevokedAt); err != nil || !day.Before(revoked) {
			return false, "отозвана " + p.RevokedAt
		}
	}
	switch {
	case errFrom != nil || errUntil != nil:
		return false, "срок действия в реестре не указан"
	case day.Before(from):
		return false, "ещё не действует (с " + p.ValidFrom + ")"
	case day.After(until):
		return false, "истекла " + p.ValidUntil
	}
	return true, ""
}

// normName — ФИО для сравнения: верхний регистр, Ё → Е, одиночные пробелы.
func normName(s string) string {
	s = strings.NewReplacer("Ё", "Е", "ё", "е").Replace(strings.ToUpper(s))
	return strings.Join(strings.Fields(s), " ")
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
