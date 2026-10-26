// Package signals эмулирует слабые сигналы о компании: госпроверки (ЕРКНМ),
// товарные знаки (ФИПС) и новости. Сбор данных объединяет новости с событиями
// КАД и Федресурса в ленту событий по клиенту.
//
// Данные — seed/<inn>.json. Неизвестный ИНН — пустые списки.
package signals

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

type company struct {
	Inspections []json.RawMessage `json:"inspections"`
	Trademarks  []json.RawMessage `json:"trademarks"`
	News        []json.RawMessage `json:"news"` // новые первыми

	published []string // News[i].published_at (дата)
}

type handler struct{ companies map[string]*company }

// Register вешает эндпоинты на группу (обычно /signals/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{companies: map[string]*company{}}
	for inn, body := range mockkit.CabinetSeeds(seedFS, "seed") {
		var co company
		mockkit.MustDecode(inn, body, &co)
		for _, raw := range co.News {
			var n struct {
				PublishedAt string `json:"published_at"`
			}
			mockkit.MustDecode(inn+" news", raw, &n)
			co.published = append(co.published, n.PublishedAt[:10])
		}
		h.companies[inn] = &co
	}
	rg.GET("/:inn/inspections", h.list(func(co *company) []json.RawMessage { return co.Inspections }))
	rg.GET("/:inn/trademarks", h.list(func(co *company) []json.RawMessage { return co.Trademarks }))
	rg.GET("/:inn/news", h.news)
}

func respond(c *gin.Context, inn string, items []json.RawMessage) {
	if items == nil {
		items = []json.RawMessage{}
	}
	c.JSON(http.StatusOK, struct {
		INN   string            `json:"inn"`
		Items []json.RawMessage `json:"items"`
	}{inn, items})
}

func (h *handler) list(pick func(*company) []json.RawMessage) gin.HandlerFunc {
	return func(c *gin.Context) {
		inn, ok := mockkit.INNParam(c)
		if !ok {
			return
		}
		var items []json.RawMessage
		if co := h.companies[inn]; co != nil {
			items = pick(co)
		}
		respond(c, inn, items)
	}
}

// news: новости, опубликованные не раньше from.
func (h *handler) news(c *gin.Context) {
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return
	}
	from := c.Query("from")
	if !mockkit.ValidDate(from) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "from must be YYYY-MM-DD")
		return
	}
	var items []json.RawMessage
	if co := h.companies[inn]; co != nil {
		for i, raw := range co.News {
			if from == "" || co.published[i] >= from {
				items = append(items, raw)
			}
		}
	}
	respond(c, inn, items)
}
