package httptransport_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func TestCatalog(t *testing.T) {
	r := gin.New()
	httptransport.WithCatalog(httptransport.NewCatalogHandler(service.NewCatalogService()))(r, r.Group("/api/v1"))
	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	codes := func(w *httptest.ResponseRecorder) []string {
		var body struct {
			Items []struct{ Code string } `json:"items"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		out := []string{}
		for _, p := range body.Items {
			out = append(out, p.Code)
		}
		return out
	}

	if got := codes(do(http.MethodGet, "/api/v1/products", "")); len(got) != 7 {
		t.Errorf("all products: %v", got)
	}
	// кредитная линия — только для юрлиц
	if got := strings.Join(codes(do(http.MethodGet, "/api/v1/products?purpose=WORKING_CAPITAL&applicant_type=SOLE_PROPRIETOR", "")), ","); got != "WORKING_CAPITAL,EXPRESS" {
		t.Errorf("filtered: %s", got)
	}
	if w := do(http.MethodGet, "/api/v1/products?purpose=YACHT", ""); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown purpose: %d", w.Code)
	}
	w := do(http.MethodGet, "/api/v1/products/EXPRESS", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"required_above_amount":null`) {
		t.Errorf("express: %d %s", w.Code, w.Body)
	}
	if w := do(http.MethodGet, "/api/v1/products/NOPE", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown product: %d", w.Code)
	}

	// 1 000 000 ₽ на 12 мес. под 18,5 % — 91 918 ₽ в месяц
	w = do(http.MethodPost, "/api/v1/products/WORKING_CAPITAL/calculate", `{"amount":1000000,"term_months":12}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"monthly_payment_from":91918`) || !strings.Contains(w.Body.String(), `"rate_annual_from":18.5`) {
		t.Errorf("calculate: %d %s", w.Code, w.Body)
	}
	w = do(http.MethodPost, "/api/v1/products/EXPRESS/calculate", `{"amount":8000000,"term_months":36}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "requested.amount") || !strings.Contains(w.Body.String(), "requested.term_months") {
		t.Errorf("out of range: %d %s", w.Code, w.Body)
	}
	if w := do(http.MethodPost, "/api/v1/products/WORKING_CAPITAL/calculate", `{"amount":1000000}`); w.Code != http.StatusBadRequest {
		t.Errorf("no term: %d", w.Code)
	}
	if w := do(http.MethodPost, "/api/v1/products/NOPE/calculate", `{"amount":1000000,"term_months":12}`); w.Code != http.StatusNotFound {
		t.Errorf("calculate unknown: %d", w.Code)
	}
}
