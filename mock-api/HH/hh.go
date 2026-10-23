// Package hh эмулирует api.hh.ru: поиск работодателя по названию и его вакансии.
// Форма ответов как у настоящего API, поэтому переход на живой источник —
// смена base URL. Нужен как сигнал: компания нанимает или нет.
//
// Данные — seed/employers.json. В поиске есть тёзки (АЛЬТАИР ГРУПП и т. п.),
// поэтому работодателя сверяем по названию из ЕГРЮЛ.
package hh

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

const perPage = 20

type vacancy struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Salary      json.RawMessage `json:"salary"`
	PublishedAt string          `json:"published_at"`
}

type employer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Vacancies []vacancy `json:"vacancies"`
}

type employerView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OpenVacancies int    `json:"open_vacancies"`
	URL           string `json:"url"`
	VacanciesURL  string `json:"vacancies_url"`
}

type vacancyView struct {
	vacancy
	Employer struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"employer"`
	Area struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"area"`
}

type handler struct{ employers []employer }

// Register вешает эндпоинты на группу (обычно /hh).
func Register(rg *gin.RouterGroup) {
	body, err := seedFS.ReadFile("seed/employers.json")
	if err != nil {
		panic("hh: missing seed/employers.json")
	}
	var s struct {
		Employers []employer `json:"employers"`
	}
	mockkit.MustDecode("employers.json", body, &s)
	h := &handler{s.Employers}

	rg.GET("/employers", h.search)
	rg.GET("/employers/:id", h.get)
	rg.GET("/vacancies", h.vacancies)
}

func view(e employer) employerView {
	return employerView{e.ID, e.Name, len(e.Vacancies), "/hh/employers/" + e.ID, "/hh/vacancies?employer_id=" + e.ID}
}

func page(found int, items any) gin.H {
	return gin.H{"found": found, "items": items, "page": 0, "pages": (found + perPage - 1) / perPage,
		"per_page": perPage}
}

// search: работодатели, в названии которых есть text (без учёта регистра, кавычек и Ё).
func (h *handler) search(c *gin.Context) {
	text := mockkit.NormName(c.Query("text"))
	if text == "" {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "text is required")
		return
	}
	items := []employerView{}
	for _, e := range h.employers {
		if strings.Contains(mockkit.NormName(e.Name), text) {
			items = append(items, view(e))
		}
	}
	c.JSON(http.StatusOK, page(len(items), items))
}

func (h *handler) get(c *gin.Context) {
	for _, e := range h.employers {
		if e.ID == c.Param("id") {
			c.JSON(http.StatusOK, view(e))
			return
		}
	}
	mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "employer not found")
}

func (h *handler) vacancies(c *gin.Context) {
	id := c.Query("employer_id")
	if id == "" {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "employer_id is required")
		return
	}
	items := []vacancyView{}
	for _, e := range h.employers {
		if e.ID != id {
			continue
		}
		for _, v := range e.Vacancies {
			vv := vacancyView{vacancy: v}
			vv.Employer.ID, vv.Employer.Name = e.ID, e.Name
			vv.Area.ID, vv.Area.Name = "1", "Москва"
			items = append(items, vv)
		}
	}
	c.JSON(http.StatusOK, page(len(items), items))
}
