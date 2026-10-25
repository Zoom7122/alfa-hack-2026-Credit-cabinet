// Package m4d эмулирует реестр машиночитаемых доверенностей (МЧД) ФНС:
// действует ли доверенность и какие полномочия она даёт представителю.
// Нужен проверкам SIGNER_AUTHORITY и POA_VALID (docs/e-signature.md).
//
// Данные — seed/<номер МЧД>.json. Коды полномочий — наш упрощённый справочник,
// классификатор ФНС мапится в адаптере.
package m4d

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

type poa struct {
	body           json.RawMessage
	issuedAt       string
	principal, rep string
}

type handler struct {
	byNumber map[string]*poa
	all      []*poa // новые первыми
}

// Register вешает эндпоинты на группу (обычно /m4d/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{byNumber: map[string]*poa{}}
	for number, body := range mockkit.Seeds(seedFS, "seed") {
		var p struct {
			Number    string `json:"number"`
			IssuedAt  string `json:"issued_at"`
			Principal struct {
				INN string `json:"inn"`
			} `json:"principal"`
			Representative struct {
				INN string `json:"inn"`
			} `json:"representative"`
		}
		mockkit.MustDecode(number, body, &p)
		if p.Number != number {
			panic("m4d: " + number + ": number does not match file name")
		}
		x := &poa{body: body, issuedAt: p.IssuedAt, principal: p.Principal.INN, rep: p.Representative.INN}
		h.byNumber[number] = x
		h.all = append(h.all, x)
	}
	sort.Slice(h.all, func(i, j int) bool { return h.all[i].issuedAt > h.all[j].issuedAt })

	rg.GET("/poa/:number", h.get)
	rg.GET("/poa", h.search)
}

func (h *handler) get(c *gin.Context) {
	if p := h.byNumber[c.Param("number")]; p != nil {
		mockkit.JSON(c, p.body)
		return
	}
	mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "power of attorney not found")
}

// search: доверенности компании principal_inn, при representative_inn — только на этого человека.
func (h *handler) search(c *gin.Context) {
	principal, rep := c.Query("principal_inn"), c.Query("representative_inn")
	if !mockkit.ValidINN(principal) || (rep != "" && !mockkit.ValidINN(rep)) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN,
			"principal_inn is required; principal_inn and representative_inn must be 10 or 12 digits")
		return
	}
	items := []json.RawMessage{}
	for _, p := range h.all {
		if p.principal == principal && (rep == "" || p.rep == rep) {
			items = append(items, p.body)
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
