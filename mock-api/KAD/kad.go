// Package kad эмулирует картотеку арбитражных дел (kad.arbitr.ru):
// дела, где компания истец, ответчик или должник в банкротстве.
//
// Данные — seed/<inn>.json: сводка по всем делам и сами дела.
package kad

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

var emptySummary = json.RawMessage(`{"as_defendant_count_12m":0,"as_defendant_amount_12m":0.00,` +
	`"as_defendant_active_amount":0.00,"as_plaintiff_count_12m":0,"as_plaintiff_amount_12m":0.00,` +
	`"bankruptcy_as_debtor":false}`)

// roleFilter: какие роли в деле подходят под фильтр запроса. Должник в банкротстве — тоже ответчик.
var roleFilter = map[string]map[string]bool{
	"ANY":         nil,
	"PLAINTIFF":   {"PLAINTIFF": true},
	"DEFENDANT":   {"DEFENDANT": true, "DEBTOR": true},
	"THIRD_PARTY": {"THIRD_PARTY": true},
}

type company struct {
	CheckedAt string            `json:"checked_at"`
	Summary   json.RawMessage   `json:"summary"`
	Items     []json.RawMessage `json:"items"` // новые дела первыми

	roles, registered []string // Items[i].role, Items[i].registered_at
}

type searchRequest struct {
	INN      string `json:"inn"`
	Role     string `json:"role"`
	DateFrom string `json:"date_from"`
	Page     int    `json:"page"`
	Size     int    `json:"size"`
}

type handler struct{ companies map[string]*company }

// Register вешает эндпоинты на группу (обычно /kad/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{companies: map[string]*company{}}
	for inn, body := range mockkit.CabinetSeeds(seedFS, "seed") {
		var co company
		mockkit.MustDecode(inn, body, &co)
		for _, raw := range co.Items {
			var it struct {
				Role         string `json:"role"`
				RegisteredAt string `json:"registered_at"`
			}
			mockkit.MustDecode(inn+" case", raw, &it)
			co.roles = append(co.roles, it.Role)
			co.registered = append(co.registered, it.RegisteredAt)
		}
		if !sort.SliceIsSorted(co.registered, func(i, j int) bool { return co.registered[i] > co.registered[j] }) {
			panic("kad: " + inn + ": cases must be sorted newest first")
		}
		h.companies[inn] = &co
	}
	rg.POST("/cases/search", h.search)
}

func (h *handler) search(c *gin.Context) {
	var req searchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}
	if !mockkit.ValidINN(req.INN) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
		return
	}
	if req.Role == "" {
		req.Role = "ANY"
	}
	roles, ok := roleFilter[req.Role]
	if !ok {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
			"role must be ANY, PLAINTIFF, DEFENDANT or THIRD_PARTY")
		return
	}
	if !mockkit.ValidDate(req.DateFrom) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "date_from must be YYYY-MM-DD")
		return
	}
	if req.Page == 0 {
		req.Page = 1
	}
	if req.Size == 0 {
		req.Size = 50
	}
	if req.Page < 1 || req.Size < 1 || req.Size > 100 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "page must be >= 1, size from 1 to 100")
		return
	}

	resp := struct {
		INN       string            `json:"inn"`
		CheckedAt string            `json:"checked_at"`
		Summary   json.RawMessage   `json:"summary"`
		Total     int               `json:"total"`
		Items     []json.RawMessage `json:"items"`
	}{INN: req.INN, CheckedAt: mockkit.CheckedAt, Summary: emptySummary, Items: []json.RawMessage{}}

	if co := h.companies[req.INN]; co != nil {
		// summary — по всем делам компании, фильтры влияют только на список
		resp.CheckedAt, resp.Summary = co.CheckedAt, co.Summary
		var items []json.RawMessage
		for i, raw := range co.Items {
			if (roles == nil || roles[co.roles[i]]) && (req.DateFrom == "" || co.registered[i] >= req.DateFrom) {
				items = append(items, raw)
			}
		}
		resp.Total = len(items)
		resp.Items = mockkit.Paginate(items, req.Page, req.Size)
	}
	c.JSON(http.StatusOK, resp)
}
