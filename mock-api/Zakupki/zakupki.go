// Package zakupki эмулирует ЕИС «Закупки»: госконтракты, где компания поставщик
// (44-ФЗ, 223-ФЗ), и реестр недобросовестных поставщиков (РНП).
// Незакрытая сумма действующих контрактов — подтверждённая будущая выручка,
// попадание в РНП — стоп по нашим правилам.
//
// Данные — seed/<inn>.json: сводка по всем контрактам, контракты и записи РНП.
package zakupki

import (
	"embed"
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

var emptySummary = json.RawMessage(`{"active_count":0,"active_amount":0.00,"active_unpaid_amount":0.00,` +
	`"completed_count_36m":0,"completed_amount_36m":0.00,"terminated_count_36m":0,"penalties_amount_36m":0.00}`)

type supplier struct {
	Summary   json.RawMessage   `json:"summary"`
	Contracts []json.RawMessage `json:"contracts"` // новые первыми
	RNP       []json.RawMessage `json:"rnp"`

	signed []string // Contracts[i].signed_at
}

type handler struct{ suppliers map[string]*supplier }

// Register вешает эндпоинты на группу (обычно /zakupki/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{suppliers: map[string]*supplier{}}
	for inn, body := range mockkit.CabinetSeeds(seedFS, "seed") {
		var s supplier
		mockkit.MustDecode(inn, body, &s)
		for _, raw := range s.Contracts {
			var ct struct {
				SignedAt string `json:"signed_at"`
			}
			mockkit.MustDecode(inn+" contract", raw, &ct)
			s.signed = append(s.signed, ct.SignedAt)
		}
		if !sort.SliceIsSorted(s.signed, func(i, j int) bool { return s.signed[i] > s.signed[j] }) {
			panic("zakupki: " + inn + ": contracts must be sorted newest first")
		}
		h.suppliers[inn] = &s
	}
	rg.GET("/contracts", h.contracts)
	rg.GET("/rnp", h.rnp)
}

// contracts: контракты поставщика, подписанные не раньше date_from. summary — по всем.
func (h *handler) contracts(c *gin.Context) {
	inn, from := c.Query("supplier_inn"), c.Query("date_from")
	if !mockkit.ValidINN(inn) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "supplier_inn must be 10 or 12 digits")
		return
	}
	if !mockkit.ValidDate(from) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "date_from must be YYYY-MM-DD")
		return
	}
	resp := struct {
		SupplierINN string            `json:"supplier_inn"`
		Summary     json.RawMessage   `json:"summary"`
		Items       []json.RawMessage `json:"items"`
	}{inn, emptySummary, []json.RawMessage{}}
	if s := h.suppliers[inn]; s != nil {
		resp.Summary = s.Summary
		for i, raw := range s.Contracts {
			if from == "" || s.signed[i] >= from {
				resp.Items = append(resp.Items, raw)
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (h *handler) rnp(c *gin.Context) {
	inn := c.Query("inn")
	if !mockkit.ValidINN(inn) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
		return
	}
	entries := []json.RawMessage{}
	if s := h.suppliers[inn]; s != nil && s.RNP != nil {
		entries = s.RNP
	}
	c.JSON(http.StatusOK, struct {
		INN     string            `json:"inn"`
		Listed  bool              `json:"listed"`
		Entries []json.RawMessage `json:"entries"`
	}{inn, len(entries) > 0, entries})
}
