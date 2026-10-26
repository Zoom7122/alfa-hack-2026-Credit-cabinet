package app_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

// Inspect the actual Go types compiled in this generated release, including
// pointer nullability and zero-value JSON. No DTO is manually reconstructed.
func TestGeneratedFactsPipelineDocuments(t *testing.T) {
	catalog, err := modelbindings.Default()
	if err != nil {
		t.Fatal(err)
	}
	definitions := []model.ModelDefinition{}
	for _, meta := range catalog.List() {
		entry, err := catalog.Lookup(meta.Version, meta.BorrowerType)
		if err != nil {
			t.Fatal(err)
		}
		definition, err := entry.Definition()
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, definition)
	}
	output, err := modelgen.GenerateDocuments(definitions, modelgen.DocumentOptions{ReleaseVersion: "compiled-test-release", InputsMarkdown: []byte("manual\n")})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	for _, artifact := range output.Artifacts {
		if artifact.Path == "contracts/openapi.json" {
			if err := json.Unmarshal(artifact.Data, &doc); err != nil {
				t.Fatal(err)
			}
		}
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, typ := range []reflect.Type{reflect.TypeOf(contracts.IPFactsDemoV1{}), reflect.TypeOf(contracts.IPFactsDemoV2{}), reflect.TypeOf(contracts.LegalFactsDemoV1{})} {
		t.Run(typ.Name(), func(t *testing.T) {
			schema := schemas[typ.Name()].(map[string]any)
			properties := schema["properties"].(map[string]any)
			if len(properties) != typ.NumField() || len(schema["required"].([]any)) != typ.NumField() {
				t.Fatal("schema/DTO field count drift")
			}
			data, err := json.Marshal(reflect.New(typ).Interface())
			if err != nil {
				t.Fatal(err)
			}
			var empty map[string]any
			if err := json.Unmarshal(data, &empty); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				value, present := empty[name]
				if !present || value != nil || field.Type.Kind() != reflect.Pointer {
					t.Fatal("DTO lost explicit nullable key", name, string(data))
				}
				property := properties[name].(map[string]any)
				kinds := property["type"].([]any)
				kind := ""
				switch field.Type.Elem().Kind() {
				case reflect.Bool:
					kind = "boolean"
				case reflect.Float64:
					kind = "number"
				case reflect.Int64:
					kind = "integer"
				default:
					t.Fatal(field)
				}
				if !reflect.DeepEqual(kinds, []any{kind, "null"}) {
					t.Fatal("schema/DTO type drift", name, kinds, field.Type)
				}
			}
		})
	}
}
