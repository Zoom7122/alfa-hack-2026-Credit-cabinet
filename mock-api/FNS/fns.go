// Package fns эмулирует сервисы ФНС: ЕГРЮЛ/ЕГРИП (egrul.nalog.ru),
// «Прозрачный бизнес» (pb.nalog.ru), ГИР БО (bo.nalog.gov.ru) и блокировки
// счетов (service.nalog.ru/bi2-proc.json).
//
// Пути и поля повторяют реальные сервисы: один адаптер парсит и мок, и живой
// ответ, переключение — смена base URL. Двухшаговые запросы (токен → результат)
// мок отрабатывает сразу. Блокировки — формат свой, реальный неизвестен.
//
// Данные — seed/<inn>.json, встроены в бинарник. Суммы ГИР БО в тыс. руб.
package fns

import (
	"embed"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const (
	egrulTokenPrefix = "MOCK-T-"
	pbTokenPrefix    = "MOCK-PB-"
	biCheckedAt      = mockkit.AsOf + "T10:15:03"
)

var bikRe = regexp.MustCompile(`^\d{9}$`)

type company struct {
	Egrul json.RawMessage `json:"egrul"` // строка rows[] ЕГРЮЛ/ЕГРИП
	PB    json.RawMessage `json:"pb"`    // ответ company-proc.json (method=get-response)
	BO    *struct {
		Organization json.RawMessage `json:"organization"` // элемент content[] поиска
		BFO          json.RawMessage `json:"bfo"`          // отчётность по годам
	} `json:"bo"` // нет у ИП: они не сдают бухотчётность
	BI struct {
		CheckedAt string                     `json:"checked_at"`
		ByBIK     map[string]json.RawMessage `json:"by_bik"` // БИК → решения о приостановлении
	} `json:"bi"`

	inn, ogrn, name string // из строки ЕГРЮЛ, для поиска в «Прозрачном бизнесе»
}

type handler struct {
	companies map[string]*company
	boByID    map[int64]*company
}

// Captcha — ответ на X-Mock-Fault: captcha, как у реального egrul.nalog.ru.
func Captcha(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"captchaRequired": true})
}

// Register вешает эндпоинты на группу (обычно /fns).
func Register(rg *gin.RouterGroup) {
	h := load()
	rg.POST("/egrul/", h.egrulSearch)
	rg.GET("/egrul/search-result/:t", h.egrulResult)
	rg.POST("/pb/search-proc.json", h.pbSearch)
	rg.POST("/pb/company-proc.json", h.pbCompany)
	rg.GET("/bo/advanced-search/organizations/search", h.boSearch)
	rg.GET("/bo/nbo/organizations/:id/bfo/", h.boReports)
	rg.POST("/bi/bi2-proc.json", h.blocks)
}

func load() *handler {
	h := &handler{companies: map[string]*company{}, boByID: map[int64]*company{}}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var co company
		mockkit.MustDecode(inn, body, &co)

		var row struct{ I, O, C, N string }
		mockkit.MustDecode(inn+" egrul", co.Egrul, &row)
		if row.I != inn {
			panic("fns: " + inn + ": egrul.i does not match file name")
		}
		co.inn, co.ogrn, co.name = row.I, row.O, row.C
		if co.name == "" {
			co.name = row.N // у ИП нет краткого наименования
		}

		if co.BO != nil {
			var org struct{ ID int64 }
			mockkit.MustDecode(inn+" bo", co.BO.Organization, &org)
			h.boByID[org.ID] = &co
		}
		h.companies[inn] = &co
	}
	return h
}

// egrulSearch — шаг 1: по ИНН (или любой строке) выдаёт токен поиска.
func (h *handler) egrulSearch(c *gin.Context) {
	q := strings.TrimSpace(c.PostForm("query"))
	if q == "" {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "query is required")
		return
	}
	c.JSON(http.StatusOK, struct {
		T               string `json:"t"`
		CaptchaRequired bool   `json:"captchaRequired"`
	}{egrulTokenPrefix + q, false})
}

// egrulResult — шаг 2: строки выписки по токену; неизвестный ИНН — пустой rows.
func (h *handler) egrulResult(c *gin.Context) {
	rows := []json.RawMessage{}
	if inn, ok := strings.CutPrefix(c.Param("t"), egrulTokenPrefix); ok {
		if co := h.companies[inn]; co != nil {
			rows = append(rows, co.Egrul)
		}
	}
	c.JSON(http.StatusOK, gin.H{"rows": rows})
}

type pbItem struct {
	Token string `json:"token"`
	INN   string `json:"inn"`
	OGRN  string `json:"ogrn"`
	NameC string `json:"namec"`
}

type pbPage struct {
	HasMore  bool     `json:"hasMore"`
	Page     int      `json:"page"`
	PageSize int      `json:"pageSize"`
	RowCount int      `json:"rowCount"`
	Data     []pbItem `json:"data"`
}

// pbSearch ищет по ИНН. Юрлица — в секции "ul", ИП — в "ip".
func (h *handler) pbSearch(c *gin.Context) {
	q := strings.TrimSpace(c.PostForm("query"))
	if q == "" {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "query is required")
		return
	}
	page := pbPage{Page: 1, PageSize: 10, Data: []pbItem{}}
	if co := h.companies[q]; co != nil {
		page.Data = append(page.Data, pbItem{pbTokenPrefix + co.inn, co.inn, co.ogrn, co.name})
		page.RowCount = 1
	}
	section := "ul"
	if len(q) == 12 {
		section = "ip"
	}
	c.JSON(http.StatusOK, gin.H{section: page})
}

// pbCompany: method=get-request → {token, id}; method=get-response → карточка компании.
func (h *handler) pbCompany(c *gin.Context) {
	token := c.PostForm("token")
	inn, _ := strings.CutPrefix(token, pbTokenPrefix)
	co := h.companies[inn]

	switch c.PostForm("method") {
	case "get-request":
		if co == nil {
			mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "unknown token")
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token, "id": token})
	case "get-response":
		if co == nil || c.PostForm("id") != token {
			mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "unknown token or id")
			return
		}
		mockkit.JSON(c, co.PB)
	default:
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "method must be get-request or get-response")
	}
}

// boSearch: организация в ГИР БО по ИНН; у ИП и неизвестных ИНН — пусто.
func (h *handler) boSearch(c *gin.Context) {
	content := []json.RawMessage{}
	if co := h.companies[strings.TrimSpace(c.Query("query"))]; co != nil && co.BO != nil {
		content = append(content, co.BO.Organization)
	}
	c.JSON(http.StatusOK, struct {
		Content       []json.RawMessage `json:"content"`
		TotalElements int               `json:"totalElements"`
	}{content, len(content)})
}

// boReports: бухотчётность по id из поиска, новые годы первыми.
func (h *handler) boReports(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "id must be a number")
		return
	}
	co := h.boByID[id]
	if co == nil {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "organization not found")
		return
	}
	mockkit.JSON(c, co.BO.BFO)
}

// blocks: действующие решения о приостановлении операций по счетам в банке с этим БИК.
func (h *handler) blocks(c *gin.Context) {
	inn, bik := c.PostForm("inn"), c.PostForm("bik")
	if !mockkit.ValidINN(inn) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeInvalidINN, "inn must be 10 or 12 digits")
		return
	}
	if !bikRe.MatchString(bik) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "bik must be 9 digits")
		return
	}
	resp := struct {
		INN       string          `json:"inn"`
		BIK       string          `json:"bik"`
		CheckedAt string          `json:"checked_at"`
		Decisions json.RawMessage `json:"decisions"`
	}{inn, bik, biCheckedAt, json.RawMessage("[]")}
	if co := h.companies[inn]; co != nil {
		resp.CheckedAt = co.BI.CheckedAt
		if d, ok := co.BI.ByBIK[bik]; ok {
			resp.Decisions = d
		}
	}
	c.JSON(http.StatusOK, resp)
}
