package bank_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	bank "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Bank"
)

func TestEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	bank.Register(r.Group("/bank/v1"))

	for _, path := range []string{"accounts", "credits", "acquiring", "salary-project", "card-index"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bank/v1/companies/7701234567/"+path, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("body is not valid JSON")
			}
		})
	}
}

func TestInvalidINN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	bank.Register(r.Group("/bank/v1"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bank/v1/companies/123/accounts", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}
