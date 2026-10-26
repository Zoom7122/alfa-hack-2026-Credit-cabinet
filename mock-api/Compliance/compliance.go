// Package compliance эмулирует проверки 115-ФЗ: уровень риска на платформе ЦБ
// «Знай своего клиента» (ЗСК) и скрининг по перечням Росфинмониторинга
// и санкционным спискам (OFAC, UK, EU).
//
// Данные: seed/zsk/<inn>.json — уровень риска; seed/lists.json — версии
// списков и записи. В списках есть тестовые «попадания» для проверки стопа
// (ГРОМОВ ВИКТОР АНДРЕЕВИЧ, ООО «ТРАНЗИТ-ИНВЕСТ») и тёзки клиентов,
// которые совпадать не должны.
package compliance

import (
	"embed"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const screenedAt = mockkit.AsOf + "T10:15:05Z"

const (
	typeLegal  = "LEGAL"
	typePerson = "PERSON"
)

type entry struct {
	List      string  `json:"list"`
	EntryID   string  `json:"entry_id"`
	Type      string  `json:"type"`
	Name      string  `json:"name"`
	BirthDate *string `json:"birth_date"`
	INN       *string `json:"inn"`
}

type lists struct {
	Versions map[string]string `json:"versions"`
	Entries  []entry           `json:"entries"`
}

type screeningRequest struct {
	Lists    []string `json:"lists"`
	Subjects []struct {
		Ref       string `json:"ref"`
		Type      string `json:"type"`
		INN       string `json:"inn"`
		Name      string `json:"name"`
		FullName  string `json:"full_name"`
		BirthDate string `json:"birth_date"`
	} `json:"subjects"`
}

// score печатается с двумя знаками, как в доке (0.97, 1.00).
type score float64

func (s score) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatFloat(float64(s), 'f', 2, 64)), nil
}

type match struct {
	List       string  `json:"list"`
	EntryID    string  `json:"entry_id"`
	Name       string  `json:"name"`
	BirthDate  *string `json:"birth_date,omitempty"`
	MatchScore score   `json:"match_score"`
}

type result struct {
	Ref     string  `json:"ref"`
	Listed  bool    `json:"listed"`
	Matches []match `json:"matches"`
}

type handler struct {
	zsk   map[string][]byte
	lists lists
}

// Register вешает эндпоинты на группу (обычно /compliance/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{zsk: mockkit.CabinetSeeds(seedFS, "seed/zsk")}
	body, err := seedFS.ReadFile("seed/lists.json")
	if err != nil {
		panic("compliance: missing seed/lists.json")
	}
	mockkit.MustDecode("lists.json", body, &h.lists)
	for _, e := range h.lists.Entries {
		if _, ok := h.lists.Versions[e.List]; !ok {
			panic("compliance: entry " + e.EntryID + " is in unknown list " + e.List)
		}
	}

	rg.GET("/zsk/:inn", h.riskLevel)
	rg.POST("/screening", h.screening)
}

func (h *handler) riskLevel(c *gin.Context) {
	inn, ok := mockkit.INNParam(c)
	if !ok {
		return
	}
	body, ok := h.zsk[inn]
	if !ok {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "no ZSK risk level for this inn")
		return
	}
	mockkit.JSON(c, body)
}

func (h *handler) screening(c *gin.Context) {
	var req screeningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}
	if len(req.Lists) == 0 || len(req.Subjects) == 0 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "lists and subjects must not be empty")
		return
	}
	versions := map[string]string{}
	for _, l := range req.Lists {
		v, ok := h.lists.Versions[l]
		if !ok {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "unknown list "+l)
			return
		}
		versions[l] = v
	}

	results := make([]result, 0, len(req.Subjects))
	for _, s := range req.Subjects {
		if s.Ref == "" || (s.Type != typeLegal && s.Type != typePerson) {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
				"each subject needs ref and type LEGAL or PERSON")
			return
		}
		name := s.Name
		if s.Type == typePerson && s.FullName != "" {
			name = s.FullName
		}
		r := result{Ref: s.Ref, Matches: []match{}}
		for _, e := range h.lists.Entries {
			if _, requested := versions[e.List]; !requested || e.Type != s.Type {
				continue
			}
			if sc, ok := matchScore(e, s.INN, name, s.BirthDate); ok {
				r.Matches = append(r.Matches, match{e.List, e.EntryID, e.Name, e.BirthDate, sc})
			}
		}
		r.Listed = len(r.Matches) > 0
		results = append(results, r)
	}

	c.JSON(http.StatusOK, struct {
		CheckedAt    string            `json:"checked_at"`
		ListVersions map[string]string `json:"list_versions"`
		Results      []result          `json:"results"`
	}{screenedAt, versions, results})
}

// matchScore: совпал ИНН — 1.00. Без ИНН: у юрлица точное название — 0.90,
// у физлица ФИО и дата рождения — 0.97 (тёзка с другой датой — не совпадение).
func matchScore(e entry, inn, name, birthDate string) (score, bool) {
	if inn != "" && e.INN != nil && *e.INN == inn {
		return 1, true
	}
	if name == "" || mockkit.NormName(name) != mockkit.NormName(e.Name) {
		return 0, false
	}
	if e.Type == typeLegal {
		return 0.9, true
	}
	if e.BirthDate != nil && strings.TrimSpace(birthDate) == *e.BirthDate {
		return 0.97, true
	}
	return 0, false
}
