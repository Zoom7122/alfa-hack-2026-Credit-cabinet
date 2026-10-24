// Package egrn эмулирует выписки ЕГРН (Росреестр) об объекте недвижимости:
// характеристики, правообладатели, обременения (ипотека, арест) и притязания.
// Нужен для оценки залога-недвижимости.
//
// Данные — seed/<кадастровый номер с "_" вместо ":">.json.
package egrn

import (
	"embed"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const codeObjectNotFound = "OBJECT_NOT_FOUND"

var cadastralRe = regexp.MustCompile(`^\d{2}:\d{2}:\d{6,7}:\d+$`)

type extractRequest struct {
	CadastralNumber string `json:"cadastral_number"`
	Purpose         string `json:"purpose"`
	ApplicationID   string `json:"application_id"`
}

type handler struct{ objects map[string][]byte }

// Register вешает эндпоинты на группу (обычно /egrn/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{objects: map[string][]byte{}}
	for name, body := range mockkit.Seeds(seedFS, "seed") {
		var e struct {
			Object struct {
				CadastralNumber string `json:"cadastral_number"`
			} `json:"object"`
		}
		mockkit.MustDecode(name, body, &e)
		if strings.ReplaceAll(e.Object.CadastralNumber, ":", "_") != name {
			panic("egrn: " + name + ": cadastral_number does not match file name")
		}
		h.objects[e.Object.CadastralNumber] = body
	}
	rg.POST("/extracts", h.extract)
}

func (h *handler) extract(c *gin.Context) {
	var req extractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "invalid JSON body")
		return
	}
	if !cadastralRe.MatchString(req.CadastralNumber) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "cadastral_number must look like 50:21:0110203:845")
		return
	}
	body, ok := h.objects[req.CadastralNumber]
	if !ok {
		mockkit.Error(c, http.StatusNotFound, codeObjectNotFound, "no object with this cadastral number")
		return
	}
	mockkit.JSON(c, body)
}
