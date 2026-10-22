// Package bank эмулирует внутренние системы банка: расчётный счёт, кредиты,
// эквайринг, зарплатный проект и картотеку неоплаченных требований.
//
// Ответы — JSON-файлы рядом с этим кодом, они встроены в бинарник.
// Пока на любой корректный ИНН возвращается один и тот же ответ.
package bank

import (
	"embed"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
)

//go:embed *.json
var fixtures embed.FS

var innRe = regexp.MustCompile(`^(\d{10}|\d{12})$`)

// routes: путь относительно группы → файл с ответом.
var routes = map[string]string{
	"/companies/:inn/accounts":       "accounts.json",       // расчётный счёт: счета, остатки, транзакции
	"/companies/:inn/credits":        "credits.json",        // кредиты и долговая нагрузка
	"/companies/:inn/acquiring":      "acquiring.json",      // эквайринг: терминалы, обороты, СБП
	"/companies/:inn/salary-project": "salary_project.json", // зарплатный проект: реестры, ФОТ
	"/companies/:inn/card-index":     "card_index.json",     // картотека: неоплаченные требования, блокировки
}

// Register вешает эндпоинты эмулятора на группу (обычно /bank/v1).
func Register(rg *gin.RouterGroup) {
	for path, file := range routes {
		body, err := fixtures.ReadFile(file)
		if err != nil {
			panic("bank: missing fixture " + file) // ловится при старте, а не на запросе
		}
		rg.GET(path, serve(body))
	}
}

func serve(body []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !innRe.MatchString(c.Param("inn")) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "inn must be 10 or 12 digits"})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	}
}
