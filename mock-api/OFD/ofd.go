// Package ofd эмулирует оператора фискальных данных: кассы клиента и выручку
// по чекам за месяц (наличные и безнал). Нужен компаниям с розницей: безнал
// по чекам сверяем с эквайрингом в Core Banking.
//
// Доступ — по токену клиента в заголовке X-OFD-Token.
// Данные — seed/<inn>.json. Нет касс — пустые списки.
package ofd

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const codeInvalidToken = "INVALID_OFD_TOKEN"

type organization struct {
	KKT    json.RawMessage   `json:"kkt"`
	Months []json.RawMessage `json:"months"` // новые первыми
}

type handler struct{ orgs map[string]*organization }

// Register вешает эндпоинты на группу (обычно /ofd/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{orgs: map[string]*organization{}}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var o organization
		mockkit.MustDecode(inn, body, &o)
		h.orgs[inn] = &o
	}
	rg.GET("/organizations/:inn/kkt", h.kkt)
	rg.GET("/organizations/:inn/receipts/monthly", h.receipts)
}

func (h *handler) find(c *gin.Context) (string, *organization, bool) {
	if !mockkit.RequireToken(c, "X-OFD-Token", codeInvalidToken) {
		return "", nil, false
	}
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return "", nil, false
	}
	return inn, h.orgs[inn], true
}

func (h *handler) kkt(c *gin.Context) {
	inn, o, ok := h.find(c)
	if !ok {
		return
	}
	kkt := json.RawMessage("[]")
	if o != nil {
		kkt = o.KKT
	}
	c.JSON(http.StatusOK, struct {
		INN string          `json:"inn"`
		KKT json.RawMessage `json:"kkt"`
	}{inn, kkt})
}

// receipts: выручка по чекам за последние months месяцев (1–12, по умолчанию 12).
func (h *handler) receipts(c *gin.Context) {
	inn, o, ok := h.find(c)
	if !ok {
		return
	}
	n, ok := mockkit.IntQuery(c, "months", 12, 1, 12)
	if !ok {
		return
	}
	months := []json.RawMessage{}
	if o != nil {
		months = o.Months[:min(n, len(o.Months))]
	}
	c.JSON(http.StatusOK, struct {
		INN    string            `json:"inn"`
		Months []json.RawMessage `json:"months"`
	}{inn, months})
}
