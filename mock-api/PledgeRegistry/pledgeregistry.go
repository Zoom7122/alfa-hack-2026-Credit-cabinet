// Package pledgeregistry эмулирует реестр уведомлений о залоге движимого имущества
// (ФНП, reestr-zalogov.ru): что компания уже заложила и свободен ли конкретный
// автомобиль (VIN) или техника (заводской номер).
//
// Данные — seed/<ИНН залогодателя>.json; индексы по VIN и номеру строятся при старте.
package pledgeregistry

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

const checkedAt = mockkit.AsOf + "T10:16:11Z"

type handler struct {
	byINN, byVIN, bySerial map[string][]json.RawMessage
}

// Register вешает эндпоинты на группу (обычно /pledge-registry/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{byINN: map[string][]json.RawMessage{}, byVIN: map[string][]json.RawMessage{},
		bySerial: map[string][]json.RawMessage{}}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var s struct {
			Items []json.RawMessage `json:"items"`
		}
		mockkit.MustDecode(inn, body, &s)
		h.byINN[inn] = s.Items
		for _, raw := range s.Items {
			var n struct {
				Subjects []struct {
					VIN    *string `json:"vin"`
					Serial *string `json:"serial"`
				} `json:"subjects"`
			}
			mockkit.MustDecode(inn+" notification", raw, &n)
			for _, sub := range n.Subjects {
				if sub.VIN != nil {
					h.byVIN[strings.ToUpper(*sub.VIN)] = append(h.byVIN[strings.ToUpper(*sub.VIN)], raw)
				}
				if sub.Serial != nil {
					h.bySerial[strings.ToUpper(*sub.Serial)] = append(h.bySerial[strings.ToUpper(*sub.Serial)], raw)
				}
			}
		}
	}
	rg.GET("/notifications", h.notifications)
}

// notifications: поиск ровно по одному из pledgor_inn, vin, serial. Ничего не нашли — пустой список.
func (h *handler) notifications(c *gin.Context) {
	var (
		key   string
		value string
		index map[string][]json.RawMessage
		n     int
	)
	for _, q := range []struct {
		name  string
		index map[string][]json.RawMessage
	}{{"pledgor_inn", h.byINN}, {"vin", h.byVIN}, {"serial", h.bySerial}} {
		if v := strings.TrimSpace(c.Query(q.name)); v != "" {
			key, value, index = q.name, v, q.index
			n++
		}
	}
	if n != 1 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "exactly one of pledgor_inn, vin, serial is required")
		return
	}
	if key == "pledgor_inn" && !mockkit.ValidINN(value) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "pledgor_inn must be 10 or 12 digits")
		return
	}
	if key != "pledgor_inn" {
		value = strings.ToUpper(value)
	}
	items := index[value]
	if items == nil {
		items = []json.RawMessage{}
	}
	c.JSON(http.StatusOK, struct {
		Query     map[string]string `json:"query"`
		CheckedAt string            `json:"checked_at"`
		Total     int               `json:"total"`
		Items     []json.RawMessage `json:"items"`
	}{map[string]string{key: value}, checkedAt, len(items), items})
}
