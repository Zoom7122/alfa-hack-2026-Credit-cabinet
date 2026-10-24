// Package bki эмулирует бюро кредитных историй (ответ «как НБКИ»).
// Отчёт запрашивается по компании (LEGAL) и по собственнику (PERSON),
// без действующего согласия субъекта (218-ФЗ) бюро не отвечает.
//
// Данные — seed/<inn>.json: {"LEGAL": отчёт, "PERSON": отчёт}. У ИП есть оба.
package bki

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const (
	subjectLegal  = "LEGAL"
	subjectPerson = "PERSON"
)

// emptySummary — сводка «истории нет», в формате сумм как в seed.
var emptySummary = json.RawMessage(`{"active_accounts":0,"closed_accounts":0,"total_limit":0.00,` +
	`"total_outstanding":0.00,"total_monthly_payment":0.00,"current_overdue_amount":0.00,` +
	`"max_overdue_days_ever":0,"overdue_30p_24m":0,"overdue_60p_24m":0,"overdue_90p_24m":0,` +
	`"restructured_count":0,"inquiries_7d":0,"inquiries_30d":0,"inquiries_90d":0}`)

type reportRequest struct {
	SubjectType string `json:"subject_type"`
	INN         string `json:"inn"`
	OGRN        string `json:"ogrn"`
	Person      *struct {
		LastName   string `json:"last_name"`
		FirstName  string `json:"first_name"`
		MiddleName string `json:"middle_name"`
		BirthDate  string `json:"birth_date"`
		INN        string `json:"inn"`
		Passport   string `json:"passport"`
	} `json:"person"`
	Consent *struct {
		ID         string `json:"id"`
		SignedAt   string `json:"signed_at"`
		ValidUntil string `json:"valid_until"`
	} `json:"consent"`
	Purpose       string `json:"purpose"`
	ApplicationID string `json:"application_id"`
}

// subject — субъект отчёта, когда истории нет (берётся из запроса).
type subject struct {
	Type      string `json:"type"`
	INN       any    `json:"inn"`
	Name      any    `json:"name"`
	BirthDate any    `json:"birth_date,omitempty"`
}

type handler struct {
	reports  map[string]map[string]json.RawMessage // inn → subject_type → отчёт
	byPerson map[string]json.RawMessage            // "ФИО|дата рождения" → отчёт PERSON
}

// Register вешает эндпоинты на группу (обычно /bki/v1).
func Register(rg *gin.RouterGroup) {
	h := load()
	rg.POST("/reports", h.report)
}

func load() *handler {
	h := &handler{reports: map[string]map[string]json.RawMessage{}, byPerson: map[string]json.RawMessage{}}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var bySubject map[string]json.RawMessage
		mockkit.MustDecode(inn, body, &bySubject)
		for typ, rep := range bySubject {
			if typ != subjectLegal && typ != subjectPerson {
				panic("bki: " + inn + ": unknown subject type " + typ)
			}
			var r struct {
				Subject struct {
					INN       string `json:"inn"`
					Name      string `json:"name"`
					BirthDate string `json:"birth_date"`
				} `json:"subject"`
			}
			mockkit.MustDecode(inn+" "+typ, rep, &r)
			if r.Subject.INN != inn {
				panic("bki: " + inn + ": subject.inn does not match file name")
			}
			if typ == subjectPerson {
				h.byPerson[personKey(r.Subject.Name, r.Subject.BirthDate)] = rep
			}
		}
		h.reports[inn] = bySubject
	}
	return h
}

func personKey(fullName, birthDate string) string {
	return mockkit.NormName(fullName) + "|" + birthDate
}

func (h *handler) report(c *gin.Context) {
	var req reportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}

	var (
		rep  json.RawMessage
		subj subject
	)
	switch req.SubjectType {
	case subjectLegal:
		if !mockkit.ValidINN(req.INN) {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
			return
		}
		if !checkConsent(c, req) {
			return
		}
		rep = h.reports[req.INN][subjectLegal]
		subj = subject{Type: subjectLegal, INN: req.INN}
	case subjectPerson:
		p := req.Person
		if p == nil || (p.INN == "" && (p.LastName == "" || p.FirstName == "" || p.BirthDate == "")) {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
				"person.inn or person.last_name, first_name and birth_date are required")
			return
		}
		if p.INN != "" && (len(p.INN) != 12 || !mockkit.ValidINN(p.INN)) {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "person.inn must be 12 digits")
			return
		}
		if !mockkit.ValidDate(p.BirthDate) {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "person.birth_date must be YYYY-MM-DD")
			return
		}
		if !checkConsent(c, req) {
			return
		}
		fullName := strings.TrimSpace(p.LastName + " " + p.FirstName + " " + p.MiddleName)
		if p.INN != "" {
			rep = h.reports[p.INN][subjectPerson]
		} else {
			rep = h.byPerson[personKey(fullName, p.BirthDate)]
		}
		subj = subject{Type: subjectPerson, INN: nullable(p.INN), Name: fullName, BirthDate: nullable(p.BirthDate)}
	default:
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "subject_type must be LEGAL or PERSON")
		return
	}

	if rep != nil {
		mockkit.JSON(c, rep)
		return
	}
	// Истории нет — это не ошибка.
	c.JSON(http.StatusOK, struct {
		ReportID    string          `json:"report_id"`
		Bureau      string          `json:"bureau"`
		GeneratedAt string          `json:"generated_at"`
		Subject     subject         `json:"subject"`
		HasHistory  bool            `json:"has_history"`
		Score       any             `json:"score"`
		Summary     json.RawMessage `json:"summary"`
		Accounts    []any           `json:"accounts"`
		Inquiries   []any           `json:"inquiries"`
	}{"NBKI-RPT-EMPTY", "NBKI", mockkit.AsOf + "T10:15:07Z", subj, false, nil, emptySummary, []any{}, []any{}})
}

// checkConsent: без согласия — 403 CONSENT_REQUIRED, истёкшее — 403 CONSENT_EXPIRED.
func checkConsent(c *gin.Context, req reportRequest) bool {
	cs := req.Consent
	if cs == nil || cs.ID == "" || cs.ValidUntil == "" {
		mockkit.Error(c, http.StatusForbidden, mockkit.CodeConsentRequired, "subject consent is required (218-FZ)")
		return false
	}
	if !mockkit.ValidDate(cs.ValidUntil) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "consent.valid_until must be YYYY-MM-DD")
		return false
	}
	if cs.ValidUntil < mockkit.AsOf {
		mockkit.Error(c, http.StatusForbidden, mockkit.CodeConsentExpired, "subject consent expired on "+cs.ValidUntil)
		return false
	}
	return true
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
