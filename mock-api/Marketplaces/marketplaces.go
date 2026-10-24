// Package marketplaces эмулирует кабинеты продавца Wildberries и Ozon: профиль,
// продажи и выплаты по месяцам, сами выплаты (для сверки с банковской выпиской).
// Контракт свой, упрощённый: адаптеры WB API и Ozon Seller API мапят ответы в него.
//
// Доступ — по токену продавца в заголовке X-Seller-Token.
// Данные — seed/<ИНН продавца>.json: площадка → профиль, месяцы, выплаты.
package marketplaces

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const (
	codeInvalidToken   = "INVALID_SELLER_TOKEN"
	codeSellerNotFound = "SELLER_NOT_FOUND"
)

var marketplaces = map[string]bool{"wildberries": true, "ozon": true}

type seller struct {
	Profile json.RawMessage   `json:"profile"`
	Months  []json.RawMessage `json:"months"`  // новые первыми
	Payouts []json.RawMessage `json:"payouts"` // по возрастанию даты

	payoutDates []string
}

type handler struct{ sellers map[string]*seller } // "marketplace|inn"

// Register вешает эндпоинты на группу (обычно /marketplaces/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{sellers: map[string]*seller{}}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var byMarket map[string]*seller
		mockkit.MustDecode(inn, body, &byMarket)
		for mp, s := range byMarket {
			if !marketplaces[mp] {
				panic("marketplaces: " + inn + ": unknown marketplace " + mp)
			}
			for _, raw := range s.Payouts {
				var p struct {
					Date string `json:"date"`
				}
				mockkit.MustDecode(inn+" payout", raw, &p)
				s.payoutDates = append(s.payoutDates, p.Date)
			}
			h.sellers[mp+"|"+inn] = s
		}
	}

	rg.GET("/:marketplace/sellers/:inn", h.profile)
	rg.GET("/:marketplace/sellers/:inn/sales/monthly", h.monthly)
	rg.GET("/:marketplace/sellers/:inn/payouts", h.payouts)
}

func (h *handler) find(c *gin.Context) (string, string, *seller) {
	mp := c.Param("marketplace")
	if !marketplaces[mp] {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "marketplace must be wildberries or ozon")
		return "", "", nil
	}
	if !mockkit.RequireToken(c, "X-Seller-Token", codeInvalidToken) {
		return "", "", nil
	}
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return "", "", nil
	}
	s := h.sellers[mp+"|"+inn]
	if s == nil {
		mockkit.Error(c, http.StatusNotFound, codeSellerNotFound, "inn does not sell on "+mp)
		return "", "", nil
	}
	return mp, inn, s
}

func (h *handler) profile(c *gin.Context) {
	if _, _, s := h.find(c); s != nil {
		mockkit.JSON(c, s.Profile)
	}
}

// monthly: последние months месяцев (1–36, по умолчанию 24).
func (h *handler) monthly(c *gin.Context) {
	mp, inn, s := h.find(c)
	if s == nil {
		return
	}
	n, ok := mockkit.IntQuery(c, "months", 24, 1, 36)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, struct {
		Marketplace string            `json:"marketplace"`
		INN         string            `json:"inn"`
		Currency    string            `json:"currency"`
		Months      []json.RawMessage `json:"months"`
	}{mp, inn, "RUB", s.Months[:min(n, len(s.Months))]})
}

// payouts: выплаты продавцу за период from–to (даты включительно).
func (h *handler) payouts(c *gin.Context) {
	_, _, s := h.find(c)
	if s == nil {
		return
	}
	from, to := c.Query("from"), c.Query("to")
	if !mockkit.ValidDate(from) || !mockkit.ValidDate(to) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "from and to must be YYYY-MM-DD")
		return
	}
	items := []json.RawMessage{}
	for i, raw := range s.Payouts {
		if (from == "" || s.payoutDates[i] >= from) && (to == "" || s.payoutDates[i] <= to) {
			items = append(items, raw)
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
