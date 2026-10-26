// Package fedresurs эмулирует Федресурс / ЕФРСБ: сообщения о банкротстве,
// намерении кредитора обратиться в суд, договорах лизинга, ликвидации,
// реорганизации и снижении чистых активов. Суммы лизинговых платежей
// в Федресурсе нет, как и в реальности.
//
// Данные: seed/companies/<inn>.json — сообщения о компании или ИП;
// seed/persons/<inn>.json — банкротство физлица (собственник, ИП).
package fedresurs

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

const checkedAt = mockkit.AsOf + "T10:15:06Z"

var emptySummary = json.RawMessage(`{"bankruptcy_stage":"NONE","has_bankruptcy_intention":false,` +
	`"active_leasing_count":0,"liquidation":false,"reorganization":false}`)

type company struct {
	CheckedAt string            `json:"checked_at"`
	Summary   json.RawMessage   `json:"summary"`
	Messages  []json.RawMessage `json:"messages"` // новые первыми

	published []string // Messages[i].published_at
}

type handler struct {
	companies map[string]*company
	persons   map[string][]byte
}

// Register вешает эндпоинты на группу (обычно /fedresurs/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{companies: map[string]*company{}, persons: mockkit.CabinetSeeds(seedFS, "seed/persons")}
	for inn, body := range mockkit.CabinetSeeds(seedFS, "seed/companies") {
		var co company
		mockkit.MustDecode(inn, body, &co)
		for _, raw := range co.Messages {
			var m struct {
				PublishedAt string `json:"published_at"`
			}
			mockkit.MustDecode(inn+" message", raw, &m)
			co.published = append(co.published, m.PublishedAt)
		}
		if !sort.SliceIsSorted(co.published, func(i, j int) bool { return co.published[i] > co.published[j] }) {
			panic("fedresurs: " + inn + ": messages must be sorted newest first")
		}
		h.companies[inn] = &co
	}

	rg.GET("/companies/:inn/messages", h.messages)
	rg.GET("/persons/:inn/bankruptcy", h.personBankruptcy)
}

// messages: сообщения, опубликованные не раньше from. summary — по всем сообщениям.
func (h *handler) messages(c *gin.Context) {
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return
	}
	from := c.Query("from")
	if !mockkit.ValidDate(from) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "from must be YYYY-MM-DD")
		return
	}

	resp := struct {
		INN       string            `json:"inn"`
		CheckedAt string            `json:"checked_at"`
		Summary   json.RawMessage   `json:"summary"`
		Messages  []json.RawMessage `json:"messages"`
	}{inn, checkedAt, emptySummary, []json.RawMessage{}}

	if co := h.companies[inn]; co != nil {
		resp.CheckedAt, resp.Summary = co.CheckedAt, co.Summary
		for i, raw := range co.Messages {
			if from == "" || co.published[i] >= from {
				resp.Messages = append(resp.Messages, raw)
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

func (h *handler) personBankruptcy(c *gin.Context) {
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return
	}
	if body, ok := h.persons[inn]; ok {
		mockkit.JSON(c, body)
		return
	}
	c.JSON(http.StatusOK, struct {
		INN             string `json:"inn"`
		FullName        any    `json:"full_name"`
		CheckedAt       string `json:"checked_at"`
		BankruptcyStage string `json:"bankruptcy_stage"`
		Messages        []any  `json:"messages"`
	}{inn, nil, checkedAt, "NONE", []any{}})
}
