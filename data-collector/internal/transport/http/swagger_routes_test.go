package httptransport_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/financial"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
	"github.com/gin-gonic/gin"
)

// Check the real registered method/path pairs, including inline financial routes.
// No database or service calls are needed to register the transport.
func TestSwaggerCoversRegisteredBackendRoutes(t *testing.T) {
	router := httptransport.NewRouter(nil, nil, nil, nil,
		httptransport.WithAuth(httptransport.NewAuthHandler(nil, discardLog)),
		httptransport.WithDraft(httptransport.NewDraftHandler(nil, discardLog)),
		httptransport.WithCabinet(httptransport.NewCabinetHandler(nil, discardLog)),
		httptransport.WithCatalog(httptransport.NewCatalogHandler(nil)),
		httptransport.WithOutcome(httptransport.NewOutcomeHandler(nil, 1<<20, discardLog)),
		httptransport.WithSigning(httptransport.NewSigningHandler(nil, discardLog)),
		httptransport.WithBackoffice(httptransport.NewBackofficeHandler(nil, discardLog)),
		httptransport.WithEngineHooks(httptransport.NewEngineHandler(nil, discardLog)),
		func(_ *gin.Engine, api *gin.RouterGroup) {
			financial.RegisterForCabinet(api, financial.Repository{}, financial.Files{})
		},
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/swagger/doc.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("Swagger HTTP status: %d", response.Code)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	actual := map[string]bool{}
	for _, route := range router.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/") {
			continue // healthz and the Swagger assets are outside its basePath.
		}
		parts := strings.Split(strings.TrimPrefix(route.Path, "/api/v1"), "/")
		for i, part := range parts {
			if strings.HasPrefix(part, ":") {
				parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
			}
		}
		path := strings.Join(parts, "/")
		method := strings.ToLower(route.Method)
		actual[method+" "+path] = true
		if _, ok := spec.Paths[path][method]; !ok {
			t.Errorf("backend route missing from Swagger: %s %s", route.Method, path)
		}
	}
	for path, methods := range spec.Paths {
		for method := range methods {
			if method == "parameters" {
				continue
			}
			if !actual[method+" "+path] {
				t.Errorf("Swagger declares an unregistered route: %s %s", method, path)
			}
		}
	}
}
