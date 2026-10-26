package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdminContractCoversRegisteredRoutesAndLocalReferences(t *testing.T) {
	data, err := os.ReadFile("../../contracts/admin.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		OpenAPI    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.OpenAPI != "3.1.0" {
		t.Fatal("unexpected OpenAPI dialect")
	}
	router := NewAdminRouter(adminTestAccess(t), time.Second)
	RegisterAdminModels(router, adminTestAccess(t), nil)
	RegisterAdminActivation(router, adminTestAccess(t), nil)
	RegisterAdminFields(router, adminTestAccess(t))
	RegisterAdminEvents(router, adminTestAccess(t), nil)
	RegisterAdminSimulation(router, adminTestAccess(t), nil)
	RegisterAdminRuntime(router, adminTestAccess(t), nil)
	registered := map[string]bool{}
	for _, route := range router.Routes() {
		parts := strings.Split(route.Path, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, ":") || strings.HasPrefix(part, "*") {
				parts[i] = "{" + part[1:] + "}"
			}
		}
		path := strings.Join(parts, "/")
		method := strings.ToLower(route.Method)
		registered[method+" "+path] = true
		if contract.Paths[path][method] == nil {
			t.Fatal("undocumented route", route.Method, path)
		}
	}
	ids := map[string]bool{}
	for path, operations := range contract.Paths {
		for method, raw := range operations {
			if !registered[method+" "+path] {
				t.Fatal("contract claims an unavailable route", method, path)
			}
			var operation struct {
				ID       string                       `json:"operationId"`
				Security []map[string]json.RawMessage `json:"security"`
			}
			if json.Unmarshal(raw, &operation) != nil || operation.ID == "" || ids[operation.ID] {
				t.Fatal("invalid/duplicate operation ID")
			}
			ids[operation.ID] = true
			if path != "/healthz" && !strings.HasSuffix(path, "/auth/login") && len(operation.Security) == 0 {
				t.Fatal("missing browser security", path)
			}
		}
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if ref, ok := node["$ref"].(string); ok {
				const prefix = "#/components/schemas/"
				if !strings.HasPrefix(ref, prefix) || contract.Components.Schemas[strings.TrimPrefix(ref, prefix)] == nil {
					t.Fatal("unresolved schema", ref)
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(document)
}
