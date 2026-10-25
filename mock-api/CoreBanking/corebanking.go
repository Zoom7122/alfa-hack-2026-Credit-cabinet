// Package corebanking эмулирует АБС банка: профиль клиента, обороты,
// транзакции, наши кредиты и ограничения по счетам (картотека, блокировки, аресты).
//
// Данные — seed/<inn>.json, встроены в бинарник. Транзакции в seed — ключевые
// операции (10–20 в месяц), обороты по месяцам лежат отдельно и с ними не сходятся.
package corebanking

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

type client struct {
	Client       json.RawMessage   `json:"client"`
	Turnover     []json.RawMessage `json:"turnover"`     // новые месяцы первыми
	Transactions []json.RawMessage `json:"transactions"` // по убыванию даты
	Loans        json.RawMessage   `json:"loans"`
	Restrictions json.RawMessage   `json:"restrictions"`

	months []string // Turnover[i].month
	dates  []string // Transactions[i].date
}

type handler struct{ clients map[string]*client }

// Register вешает эндпоинты на группу (обычно /core-banking/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{clients: load()}
	rg.GET("/clients/:inn", h.profile)
	rg.GET("/clients/:inn/turnover", h.turnover)
	rg.GET("/clients/:inn/transactions", h.transactions)
	rg.GET("/clients/:inn/loans", h.loans)
	rg.GET("/clients/:inn/restrictions", h.restrictions)
}

func load() map[string]*client {
	out := map[string]*client{}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var cl client
		mockkit.MustDecode(inn, body, &cl)

		for _, raw := range cl.Turnover {
			var m struct{ Month string }
			mockkit.MustDecode(inn+" turnover", raw, &m)
			cl.months = append(cl.months, m.Month)
		}
		if !sort.SliceIsSorted(cl.months, func(i, j int) bool { return cl.months[i] > cl.months[j] }) {
			panic("corebanking: " + inn + ": turnover must be sorted newest first")
		}

		for _, raw := range cl.Transactions {
			var t struct{ Date string }
			mockkit.MustDecode(inn+" transaction", raw, &t)
			cl.dates = append(cl.dates, t.Date)
		}
		if !sort.SliceIsSorted(cl.dates, func(i, j int) bool { return cl.dates[i] > cl.dates[j] }) {
			panic("corebanking: " + inn + ": transactions must be sorted newest first")
		}
		out[inn] = &cl
	}
	return out
}

func (h *handler) find(c *gin.Context) (string, *client, bool) {
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return "", nil, false
	}
	cl, ok := h.clients[inn]
	if !ok {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "client not found")
		return "", nil, false
	}
	return inn, cl, true
}

func (h *handler) profile(c *gin.Context) {
	if _, cl, ok := h.find(c); ok {
		mockkit.JSON(c, cl.Client)
	}
}

func (h *handler) restrictions(c *gin.Context) {
	if _, cl, ok := h.find(c); ok {
		mockkit.JSON(c, cl.Restrictions)
	}
}

func (h *handler) loans(c *gin.Context) {
	inn, cl, ok := h.find(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, struct {
		INN   string          `json:"inn"`
		Items json.RawMessage `json:"items"`
	}{inn, cl.Loans})
}

// turnover: последние months месяцев (1–12, по умолчанию 12; у нового клиента их меньше).
func (h *handler) turnover(c *gin.Context) {
	inn, cl, ok := h.find(c)
	if !ok {
		return
	}
	n, ok := mockkit.IntQuery(c, "months", 12, 1, 12)
	if !ok {
		return
	}
	n = min(n, len(cl.Turnover))
	resp := struct {
		INN        string            `json:"inn"`
		PeriodFrom string            `json:"period_from"`
		PeriodTo   string            `json:"period_to"`
		Months     []json.RawMessage `json:"months"`
	}{INN: inn, Months: cl.Turnover[:n]}
	if n > 0 {
		resp.PeriodFrom, resp.PeriodTo = cl.months[n-1], cl.months[0]
	}
	c.JSON(http.StatusOK, resp)
}

// transactions: фильтр по датам from/to (включительно), новые первыми, постранично.
func (h *handler) transactions(c *gin.Context) {
	inn, cl, ok := h.find(c)
	if !ok {
		return
	}
	from, to := c.Query("from"), c.Query("to")
	if !mockkit.ValidDate(from) || !mockkit.ValidDate(to) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "from and to must be YYYY-MM-DD")
		return
	}
	if from != "" && to != "" && from > to {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "from must not be after to")
		return
	}
	page, ok := mockkit.IntQuery(c, "page", 1, 1, 1_000_000)
	if !ok {
		return
	}
	size, ok := mockkit.IntQuery(c, "size", 100, 1, 500)
	if !ok {
		return
	}

	items := make([]json.RawMessage, 0, len(cl.Transactions))
	for i, raw := range cl.Transactions {
		if (from == "" || cl.dates[i] >= from) && (to == "" || cl.dates[i] <= to) {
			items = append(items, raw)
		}
	}
	c.JSON(http.StatusOK, struct {
		INN   string            `json:"inn"`
		Page  int               `json:"page"`
		Size  int               `json:"size"`
		Total int               `json:"total"`
		Items []json.RawMessage `json:"items"`
	}{inn, page, size, len(items), mockkit.Paginate(items, page, size)})
}
