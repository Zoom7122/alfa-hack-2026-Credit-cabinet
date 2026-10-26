package financial_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/financial"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
	"github.com/gin-gonic/gin"
)

func TestFinancialRoutesCanBeRegisteredWithCabinet(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	router := httptransport.NewRouter(nil, nil, nil, nil,
		httptransport.WithCabinet(httptransport.NewCabinetHandler(nil, log)),
		func(_ *gin.Engine, api *gin.RouterGroup) {
			financial.RegisterForCabinet(api, financial.Repository{}, financial.Files{Root: root})
		},
	)

	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	base := "/api/v1/applications/:id/documents"
	for _, route := range []string{
		"POST " + base,
		"GET " + base,
		"GET " + base + "/:documentId",
		"GET " + base + "/:documentId/content",
		"GET " + base + "/:documentId/signature-report",
		"GET " + base + "/:documentId/financial-result",
		"POST " + base + "/:documentId/signature-verification",
	} {
		if !routes[route] {
			t.Errorf("missing route %s", route)
		}
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthz returned %d", response.Code)
	}
}
