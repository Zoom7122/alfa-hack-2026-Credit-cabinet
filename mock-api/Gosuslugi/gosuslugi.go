// Package gosuslugi эмулирует «Цифровой профиль» Госуслуг: данные физлица
// (собственник, поручитель, ИП) по его согласию — паспорт, ИНН, СНИЛС, адрес,
// доходы, трудовая деятельность, статус самозанятого.
//
// Флоу асинхронный: POST создаёт запрос (202, ждём согласия человека), GET отдаёт
// результат. Мок одобряет сразу. Заголовок X-Mock-Consent: deny — человек отказал.
//
// Данные — seed/<ИНН физлица>.json.
package gosuslugi

import (
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const (
	codePersonNotFound = "PERSON_NOT_FOUND"
	codeRequestMissing = "REQUEST_NOT_FOUND"

	consentExpiresAt = "2026-10-29T10:18:00+03:00"
	approvedAt       = mockkit.AsOf + "T10:19:31+03:00"
)

// scopeField: право из запроса → поле в data.
var scopeField = map[string]string{
	"PASSPORT": "passport", "INN": "inn", "SNILS": "snils", "ADDRESS": "registration_address",
	"INCOME": "income", "EMPLOYMENT": "employment", "NPD_STATUS": "npd_status",
}

type person struct {
	data map[string]json.RawMessage
}

type request struct {
	inn    string
	scopes []string
	denied bool
}

type profileRequest struct {
	Person *struct {
		LastName   string `json:"last_name"`
		FirstName  string `json:"first_name"`
		MiddleName string `json:"middle_name"`
		BirthDate  string `json:"birth_date"`
		INN        string `json:"inn"`
	} `json:"person"`
	Scopes        []string `json:"scopes"`
	Purpose       string   `json:"purpose"`
	ApplicationID string   `json:"application_id"`
}

type handler struct {
	byINN    map[string]*person
	byName   map[string]string // "ФИО|дата рождения" → ИНН
	requests *mockkit.Store[request]
}

// Register вешает эндпоинты на группу (обычно /gosuslugi/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{byINN: map[string]*person{}, byName: map[string]string{}, requests: mockkit.NewStore[request]()}
	for inn, body := range mockkit.CabinetSeeds(seedFS, "seed") {
		var s struct {
			Person struct {
				LastName   string `json:"last_name"`
				FirstName  string `json:"first_name"`
				MiddleName string `json:"middle_name"`
				BirthDate  string `json:"birth_date"`
				INN        string `json:"inn"`
			} `json:"person"`
			Data map[string]json.RawMessage `json:"data"`
		}
		mockkit.MustDecode(inn, body, &s)
		if s.Person.INN != inn {
			panic("gosuslugi: " + inn + ": person.inn does not match file name")
		}
		for _, f := range scopeField {
			if _, ok := s.Data[f]; !ok {
				panic("gosuslugi: " + inn + ": data has no " + f)
			}
		}
		h.byINN[inn] = &person{data: s.Data}
		h.byName[nameKey(s.Person.LastName, s.Person.FirstName, s.Person.MiddleName, s.Person.BirthDate)] = inn
	}

	rg.POST("/profile-requests", h.create)
	rg.GET("/profile-requests/:id", h.result)
}

func nameKey(last, first, middle, birth string) string {
	return mockkit.NormName(last+" "+first+" "+middle) + "|" + birth
}

func denied(c *gin.Context) bool { return strings.EqualFold(c.GetHeader("X-Mock-Consent"), "deny") }

func (h *handler) create(c *gin.Context) {
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}
	p := req.Person
	if p == nil || (p.INN == "" && (p.LastName == "" || p.FirstName == "" || p.BirthDate == "")) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
			"person.inn or person.last_name, first_name and birth_date are required")
		return
	}
	if len(req.Scopes) == 0 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "scopes must not be empty")
		return
	}
	for _, s := range req.Scopes {
		if _, ok := scopeField[s]; !ok {
			mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "unknown scope "+s)
			return
		}
	}

	inn := p.INN
	if inn == "" {
		inn = h.byName[nameKey(p.LastName, p.FirstName, p.MiddleName, p.BirthDate)]
	}
	if h.byINN[inn] == nil {
		mockkit.Error(c, http.StatusNotFound, codePersonNotFound, "person has no Gosuslugi account")
		return
	}

	scopes := append([]string(nil), req.Scopes...)
	sort.Strings(scopes)
	id := mockkit.ID("DP-", inn, strings.Join(scopes, ","), req.ApplicationID)
	h.requests.Put(id, request{inn: inn, scopes: scopes, denied: denied(c)})
	c.JSON(http.StatusAccepted, gin.H{"request_id": id, "status": "WAITING_USER_CONSENT",
		"consent_expires_at": consentExpiresAt})
}

func (h *handler) result(c *gin.Context) {
	id := c.Param("id")
	req, ok := h.requests.Get(id)
	if !ok {
		mockkit.Error(c, http.StatusNotFound, codeRequestMissing, "profile request not found")
		return
	}
	if req.denied || denied(c) {
		c.JSON(http.StatusOK, gin.H{"request_id": id, "status": "DENIED"})
		return
	}
	data := map[string]json.RawMessage{}
	for _, s := range req.scopes {
		f := scopeField[s]
		data[f] = h.byINN[req.inn].data[f]
	}
	c.JSON(http.StatusOK, struct {
		RequestID  string                     `json:"request_id"`
		Status     string                     `json:"status"`
		ApprovedAt string                     `json:"approved_at"`
		Data       map[string]json.RawMessage `json:"data"`
	}{id, "APPROVED", approvedAt, data})
}
