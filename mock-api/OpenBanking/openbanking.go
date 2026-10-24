// Package openbanking эмулирует открытые API банков (мультибанкинг): счета клиента
// в других банках по его согласию. Форма близка к стандарту ЦБ «Получение
// информации о счетах» (UK Open Banking: Data/Links/Meta, суммы строкой).
//
// Банк выбирается заголовком X-Bank-Bic, клиент — X-Customer-Inn.
// Согласие мок одобряет сразу; согласия хранятся в памяти процесса.
//
// Данные — seed/<inn>.json: БИК → счета с остатком и транзакциями.
package openbanking

import (
	"embed"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

const createdAt = mockkit.AsOf + "T10:20:00+03:00"

var bicRe = regexp.MustCompile(`^\d{9}$`)

type account struct {
	Account      json.RawMessage   `json:"account"`
	Balance      json.RawMessage   `json:"balance"`
	Transactions []json.RawMessage `json:"transactions"` // новые первыми

	id     string
	booked []time.Time
}

type consent struct {
	inn, bic string
	data     json.RawMessage
}

type handler struct {
	accounts map[string][]*account // "inn|bic" → счета
	consents *mockkit.Store[consent]
}

// Register вешает эндпоинты на группу (обычно /open-banking/v1).
func Register(rg *gin.RouterGroup) {
	h := &handler{accounts: map[string][]*account{}, consents: mockkit.NewStore[consent]()}
	for inn, body := range mockkit.Seeds(seedFS, "seed") {
		var s struct {
			INN   string                `json:"inn"`
			Banks map[string][]*account `json:"banks"`
		}
		mockkit.MustDecode(inn, body, &s)
		if s.INN != inn {
			panic("openbanking: " + inn + ": inn does not match file name")
		}
		for bic, accs := range s.Banks {
			for _, a := range accs {
				var acc struct {
					AccountID string `json:"accountId"`
				}
				mockkit.MustDecode(inn+" account", a.Account, &acc)
				a.id = acc.AccountID
				for _, raw := range a.Transactions {
					var t struct {
						BookingDateTime time.Time `json:"bookingDateTime"`
					}
					mockkit.MustDecode(inn+" transaction", raw, &t)
					a.booked = append(a.booked, t.BookingDateTime)
				}
			}
			h.accounts[inn+"|"+bic] = accs
		}
	}

	rg.POST("/account-consents", h.createConsent)
	rg.GET("/account-consents/:id", h.getConsent)
	rg.GET("/accounts", h.listAccounts)
	rg.GET("/accounts/:id/balances", h.balances)
	rg.GET("/accounts/:id/transactions", h.transactions)
}

// customer читает X-Bank-Bic и X-Customer-Inn.
func customer(c *gin.Context) (inn, bic string, ok bool) {
	inn, bic = c.GetHeader("X-Customer-Inn"), c.GetHeader("X-Bank-Bic")
	if !mockkit.ValidINN(inn) || !bicRe.MatchString(bic) {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest,
			"X-Customer-Inn (10 or 12 digits) and X-Bank-Bic (9 digits) headers are required")
		return "", "", false
	}
	return inn, bic, true
}

func links(self string) gin.H { return gin.H{"self": self} }

var meta = gin.H{"totalPages": 1}

func (h *handler) createConsent(c *gin.Context) {
	inn, bic, ok := customer(c)
	if !ok {
		return
	}
	var req struct {
		Data struct {
			Permissions             []string `json:"permissions"`
			ExpirationDateTime      string   `json:"expirationDateTime"`
			TransactionFromDateTime string   `json:"transactionFromDateTime"`
			TransactionToDateTime   string   `json:"transactionToDateTime"`
		} `json:"Data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Data.Permissions) == 0 {
		mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, "Data.permissions must not be empty")
		return
	}
	id := strings.ToLower(mockkit.ID("aac-", inn, bic, strings.Join(req.Data.Permissions, ","),
		req.Data.TransactionFromDateTime, req.Data.TransactionToDateTime))
	data, _ := json.Marshal(gin.H{
		"consentId": id, "status": "Authorised", "creationDateTime": createdAt,
		"permissions": req.Data.Permissions, "expirationDateTime": req.Data.ExpirationDateTime,
		"transactionFromDateTime": req.Data.TransactionFromDateTime,
		"transactionToDateTime":   req.Data.TransactionToDateTime,
	})
	h.consents.Put(id, consent{inn: inn, bic: bic, data: data})
	c.JSON(http.StatusCreated, gin.H{"Data": gin.H{"consentId": id, "status": "AwaitingAuthorisation",
		"creationDateTime": createdAt}, "Links": links("/open-banking/v1/account-consents/" + id), "Meta": meta})
}

func (h *handler) getConsent(c *gin.Context) {
	cs, ok := h.consents.Get(c.Param("id"))
	if !ok {
		mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "consent not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"Data": cs.data, "Links": links("/open-banking/v1/account-consents/" + c.Param("id")),
		"Meta": meta})
}

func (h *handler) listAccounts(c *gin.Context) {
	inn, bic, ok := customer(c)
	if !ok {
		return
	}
	list := []json.RawMessage{}
	for _, a := range h.accounts[inn+"|"+bic] {
		list = append(list, a.Account)
	}
	c.JSON(http.StatusOK, gin.H{"Data": gin.H{"Account": list}, "Links": links("/open-banking/v1/accounts"), "Meta": meta})
}

func (h *handler) find(c *gin.Context) *account {
	inn, bic, ok := customer(c)
	if !ok {
		return nil
	}
	for _, a := range h.accounts[inn+"|"+bic] {
		if a.id == c.Param("id") {
			return a
		}
	}
	mockkit.Error(c, http.StatusNotFound, mockkit.CodeNotFound, "account not found for this customer and bank")
	return nil
}

func (h *handler) balances(c *gin.Context) {
	if a := h.find(c); a != nil {
		c.JSON(http.StatusOK, gin.H{"Data": gin.H{"Balance": []json.RawMessage{a.Balance}},
			"Links": links("/open-banking/v1/accounts/" + a.id + "/balances"), "Meta": meta})
	}
}

// transactions: фильтр по bookingDateTime (RFC 3339, границы включительно).
func (h *handler) transactions(c *gin.Context) {
	a := h.find(c)
	if a == nil {
		return
	}
	var from, to time.Time
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"fromBookingDateTime", &from}, {"toBookingDateTime", &to}} {
		if v := c.Query(p.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				mockkit.Error(c, http.StatusBadRequest, mockkit.CodeBadRequest, p.name+" must be RFC 3339")
				return
			}
			*p.dst = t
		}
	}
	items := []json.RawMessage{}
	for i, raw := range a.Transactions {
		if (from.IsZero() || !a.booked[i].Before(from)) && (to.IsZero() || !a.booked[i].After(to)) {
			items = append(items, raw)
		}
	}
	c.JSON(http.StatusOK, gin.H{"Data": gin.H{"Transaction": items},
		"Links": links("/open-banking/v1/accounts/" + a.id + "/transactions"), "Meta": meta})
}
