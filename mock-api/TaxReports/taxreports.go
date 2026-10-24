// Package taxreports эмулирует получение налоговой отчётности из ФНС по согласию
// клиента (налоговая тайна снимается согласием, ст. 102 НК РФ): декларации
// по прибыли, НДС, УСН, доходы НПД и сальдо единого налогового счёта (ЕНС).
//
// Реальный канал асинхронный, поэтому мок двухшаговый: POST создаёт запрос (202),
// GET сразу отдаёт READY. Запросы хранятся в памяти процесса.
//
// Данные — seed/<inn>.json: режим налогообложения, все документы и то,
// чего нет (с причиной).
package taxreports

import (
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const (
	codeConsentScope   = "CONSENT_SCOPE"
	codeRequestMissing = "REQUEST_NOT_FOUND"

	scopeDeclarations = "DECLARATIONS"
	scopeENS          = "ENS_BALANCE"
)

// scopeFor: какое право в согласии нужно для типа документа.
var scopeFor = map[string]string{
	"PROFIT": scopeDeclarations, "VAT": scopeDeclarations, "USN": scopeDeclarations,
	"NPD_INCOME": scopeDeclarations, "ENS_BALANCE": scopeENS,
}

type document struct {
	raw    json.RawMessage
	typ    string
	period string // пусто у ENS_BALANCE
}

type taxpayer struct {
	INN          string            `json:"inn"`
	TaxpayerType string            `json:"taxpayer_type"`
	TaxRegime    json.RawMessage   `json:"tax_regime"`
	NotAvailable []json.RawMessage `json:"not_available"`

	docs []document
	na   []document
}

type request struct {
	inn       string
	documents []string
	periods   []string
}

type createRequest struct {
	INN           string `json:"inn"`
	ApplicationID string `json:"application_id"`
	Consent       *struct {
		ID         string   `json:"id"`
		SignedAt   string   `json:"signed_at"`
		ValidUntil string   `json:"valid_until"`
		Scope      []string `json:"scope"`
	} `json:"consent"`
	Documents []string `json:"documents"`
	Periods   []string `json:"periods"`
}

type handler struct {
	taxpayers map[string]*taxpayer
	requests  *mockkit.Store[request]
}

// Register вешает эндпоинты на группу (обычно /tax-reports/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{taxpayers: map[string]*taxpayer{}, requests: mockkit.NewStore[request]()}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var tp struct {
			taxpayer
			Documents []json.RawMessage `json:"documents"`
		}
		mockkit.MustDecode(inn, body, &tp)
		if tp.INN != inn {
			panic("taxreports: " + inn + ": inn does not match file name")
		}
		t := tp.taxpayer
		t.docs = project(inn, tp.Documents)
		t.na = project(inn, t.NotAvailable)
		h.taxpayers[inn] = &t
	}

	rg.POST("/requests", h.create)
	rg.GET("/requests/:id", h.result)
}

func project(inn string, raws []json.RawMessage) []document {
	out := make([]document, 0, len(raws))
	for _, raw := range raws {
		var d struct {
			Type   string `json:"type"`
			Period string `json:"period"`
		}
		mockkit.MustDecode(inn+" document", raw, &d)
		if _, ok := scopeFor[d.Type]; !ok {
			panic("taxreports: " + inn + ": unknown document type " + d.Type)
		}
		out = append(out, document{raw, d.Type, d.Period})
	}
	return out
}

func (h *handler) create(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}
	if !mockkit.ValidINN(req.INN) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
		return
	}
	if len(req.Documents) == 0 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "documents must not be empty")
		return
	}
	for _, d := range req.Documents {
		if _, ok := scopeFor[d]; !ok {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "unknown document type "+d)
			return
		}
	}

	cs := req.Consent
	if cs == nil || cs.ID == "" || cs.ValidUntil == "" {
		mockkit.Error(c, http.StatusForbidden, mockkit.CodeConsentRequired, "taxpayer consent is required (art. 102 TC RF)")
		return
	}
	if !mockkit.ValidDate(cs.ValidUntil) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "consent.valid_until must be YYYY-MM-DD")
		return
	}
	if cs.ValidUntil < mockkit.AsOf {
		mockkit.Error(c, http.StatusForbidden, mockkit.CodeConsentExpired, "consent expired on "+cs.ValidUntil)
		return
	}
	granted := map[string]bool{}
	for _, s := range cs.Scope {
		granted[s] = true
	}
	for _, d := range req.Documents {
		if !granted[scopeFor[d]] {
			mockkit.Error(c, http.StatusForbidden, codeConsentScope, "consent scope has no "+scopeFor[d]+" for "+d)
			return
		}
	}
	if h.taxpayers[req.INN] == nil {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "taxpayer not found")
		return
	}

	docs, periods := sorted(req.Documents), sorted(req.Periods)
	id := mockkit.ID("TXR-", req.INN, strings.Join(docs, ","), strings.Join(periods, ","), req.ApplicationID)
	h.requests.Put(id, request{inn: req.INN, documents: docs, periods: periods})
	c.JSON(http.StatusAccepted, gin.H{"request_id": id, "status": "PROCESSING"})
}

func (h *handler) result(c *gin.Context) {
	id := c.Param("id")
	req, ok := h.requests.Get(id)
	if !ok {
		mockkit.Error(c, http.StatusNotFound, codeRequestMissing, "tax report request not found")
		return
	}
	tp := h.taxpayers[req.inn]
	c.JSON(http.StatusOK, struct {
		RequestID    string            `json:"request_id"`
		Status       string            `json:"status"`
		INN          string            `json:"inn"`
		TaxpayerType string            `json:"taxpayer_type"`
		TaxRegime    json.RawMessage   `json:"tax_regime"`
		Documents    []json.RawMessage `json:"documents"`
		NotAvailable []json.RawMessage `json:"not_available"`
	}{id, "READY", tp.INN, tp.TaxpayerType, tp.TaxRegime, req.filter(tp.docs), req.filter(tp.na)})
}

// filter: документы запрошенных типов за запрошенные периоды. Период документа
// подходит, если начинается с запрошенного ("2026-Q3" и "2026-01-01..2026-07-27" — к "2026").
// Без периода (ЕНС, «не применяется») — подходит всегда.
func (r request) filter(docs []document) []json.RawMessage {
	out := []json.RawMessage{}
	for _, d := range docs {
		if !contains(r.documents, d.typ) {
			continue
		}
		if d.period != "" && len(r.periods) > 0 && !hasPrefixAny(d.period, r.periods) {
			continue
		}
		out = append(out, d.raw)
	}
	return out
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasPrefixAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
