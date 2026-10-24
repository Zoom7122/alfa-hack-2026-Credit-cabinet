// Package fssp эмулирует банк данных исполнительных производств ФССП.
// Поиск по ИНН (юрлицо, ИП) или по ФИО и дате рождения (физлицо, ИП).
//
// Данные — seed/<inn>.json: должник, сводка и производства.
package fssp

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

const checkedAt = mockkit.AsOf + "T10:15:06Z"

var emptySummary = json.RawMessage(`{"active_count":0,"active_amount":0.00,"finished_count":0,"finished_no_assets_count":0}`)

type debtor struct {
	Type      string `json:"type"` // LEGAL | IP | PERSON
	INN       any    `json:"inn"`
	Name      any    `json:"name"`
	BirthDate string `json:"birth_date,omitempty"`
	Region    string `json:"region,omitempty"`
}

type person struct {
	last, first, middle, birthDate, region string
	body                                   []byte
}

type handler struct {
	byINN   map[string][]byte
	persons []person // должники-ФЛ и ИП, для поиска по ФИО
}

// Register вешает эндпоинты на группу (обычно /fssp/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{byINN: mockkit.Seeds(seedFS, "seed")}
	for inn, body := range h.byINN {
		var s struct {
			Debtor struct {
				Type      string `json:"type"`
				INN       string `json:"inn"`
				Name      string `json:"name"`
				BirthDate string `json:"birth_date"`
				Region    string `json:"region"`
			} `json:"debtor"`
		}
		mockkit.MustDecode(inn, body, &s)
		if s.Debtor.INN != inn {
			panic("fssp: " + inn + ": debtor.inn does not match file name")
		}
		if s.Debtor.Type == "LEGAL" {
			continue
		}
		parts := strings.Fields(mockkit.NormName(s.Debtor.Name))
		if len(parts) < 2 {
			panic("fssp: " + inn + ": person name must be 'Фамилия Имя [Отчество]'")
		}
		p := person{last: parts[0], first: parts[1], birthDate: s.Debtor.BirthDate, region: s.Debtor.Region, body: body}
		if len(parts) > 2 {
			p.middle = strings.Join(parts[2:], " ")
		}
		h.persons = append(h.persons, p)
	}

	rg.GET("/proceedings", h.proceedings)
}

func (h *handler) proceedings(c *gin.Context) {
	if inn := c.Query("inn"); inn != "" {
		h.byINNQuery(c, inn)
		return
	}
	h.byNameQuery(c)
}

func (h *handler) byINNQuery(c *gin.Context, inn string) {
	if !mockkit.ValidINN(inn) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
		return
	}
	if body, ok := h.byINN[inn]; ok {
		mockkit.JSON(c, body)
		return
	}
	typ := "LEGAL"
	if len(inn) == 12 {
		typ = "IP"
	}
	empty(c, debtor{Type: typ, INN: inn})
}

func (h *handler) byNameQuery(c *gin.Context) {
	last, first := mockkit.NormName(c.Query("last_name")), mockkit.NormName(c.Query("first_name"))
	middle, birth, region := mockkit.NormName(c.Query("middle_name")), c.Query("birth_date"), c.Query("region")
	if last == "" || first == "" {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
			"either inn or last_name and first_name are required")
		return
	}
	if !mockkit.ValidDate(birth) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "birth_date must be YYYY-MM-DD")
		return
	}
	for _, p := range h.persons {
		if p.last == last && p.first == first &&
			(middle == "" || p.middle == middle) &&
			(birth == "" || p.birthDate == birth) &&
			(region == "" || p.region == region) {
			mockkit.JSON(c, p.body)
			return
		}
	}
	name := strings.TrimSpace(c.Query("last_name") + " " + c.Query("first_name") + " " + c.Query("middle_name"))
	empty(c, debtor{Type: "PERSON", Name: name, BirthDate: birth, Region: region})
}

// empty — производств нет: это не ошибка, а пустой результат поиска.
func empty(c *gin.Context, d debtor) {
	c.JSON(http.StatusOK, struct {
		Debtor    debtor          `json:"debtor"`
		CheckedAt string          `json:"checked_at"`
		Summary   json.RawMessage `json:"summary"`
		Items     []any           `json:"items"`
	}{d, checkedAt, emptySummary, []any{}})
}
