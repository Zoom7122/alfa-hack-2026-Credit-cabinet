package modelgen_test

import (
	"bytes"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

func fixtures(t *testing.T) []model.ModelDefinition {
	t.Helper()
	result := []model.ModelDefinition{}
	for _, version := range []string{"ip-demo-v1", "ip-demo-v2", "legal-demo-v1"} {
		base := "ip-v2"
		if strings.HasPrefix(version, "legal") {
			base = "legal-v2"
		}
		raw, err := os.ReadFile("../../models/" + base + ".json")
		if err != nil {
			t.Fatal(err)
		}
		d, err := model.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		d.Version = version
		d.FactsSchemaVersion = "demo-facts-v1"
		integer := strings.HasSuffix(version, "v2")
		if integer {
			d.FactsSchemaVersion = "demo-facts-v2"
		}
		d.Parameters = append(d.Parameters, model.Parameter{ID: "annual_contract_value_rub", Enabled: true, Title: "Protocol example", Source: model.Source{Kind: model.Supplier, Path: "annual_contract_value_rub"}, JSONName: "annual_contract_value_rub", Type: model.Number, Unit: model.RUB, Number: &model.NumberDomain{Integer: integer, Lower: &model.Bound{Value: "0", Inclusive: true}}, Required: true}, model.Parameter{ID: "has_confirmed_contracts", Enabled: true, Title: "Protocol example", Source: model.Source{Kind: model.Supplier, Path: "has_confirmed_contracts"}, JSONName: "has_confirmed_contracts", Type: model.Boolean, Unit: model.Flag, Required: false})
		result = append(result, d)
	}
	return result
}
func TestFactsGeneratorMatchesCompiledFixtures(t *testing.T) {
	d := fixtures(t)
	before := []string{}
	for _, m := range d {
		b, err := model.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		before = append(before, string(b))
	}
	result, err := modelgen.GenerateFactsDTOs(d, modelgen.FactsOptions{BindingsPackage: "contracts_test", TypePrefix: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 6 || len(result.Versions) != 3 {
		t.Fatal(result)
	}
	for i, v := range result.Versions {
		base := "../../contracts/facts_fixture_" + strings.ReplaceAll(v.Version, "-", "_")
		for j, suffix := range []string{"_generated_test.go", "_accessors_generated_test.go"} {
			raw, err := os.ReadFile(base + suffix)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, result.Artifacts[i*2+j].Data) {
				t.Fatal("stale generated fixture", v.Version, suffix)
			}
		}
	}
	d[0], d[2] = d[2], d[0]
	reordered, err := modelgen.GenerateFactsDTOs(d, modelgen.FactsOptions{BindingsPackage: "contracts_test", TypePrefix: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, reordered) {
		t.Fatal("input order changed output")
	}
	d[0], d[2] = d[2], d[0]
	for i, m := range d {
		b, err := model.Marshal(m)
		if err != nil || string(b) != before[i] {
			t.Fatal("generation mutated definition", err)
		}
	}
}
func TestFactsGenerationRejectsInconsistentVersionsAndNames(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]model.ModelDefinition) []model.ModelDefinition
	}{
		{"duplicate_version", func(d []model.ModelDefinition) []model.ModelDefinition { return append(d, d[0]) }},
		{"go_version_collision", func(d []model.ModelDefinition) []model.ModelDefinition {
			other := d[0]
			other.Version = "ip-demo_v1"
			return append(d, other)
		}},
		{"schema_changed_fields", func(d []model.ModelDefinition) []model.ModelDefinition {
			d[1].FactsSchemaVersion = d[0].FactsSchemaVersion
			return d
		}},
		{"invalid_maximum", func(d []model.ModelDefinition) []model.ModelDefinition { d[0].Criteria[0].MaxPoints--; return d }},
		{"bad_field_name", func(d []model.ModelDefinition) []model.ModelDefinition {
			d[0].Parameters[len(d[0].Parameters)-1].GoName = "MarshalJSON"
			return d
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := modelgen.GenerateFactsDTOs(test.edit(fixtures(t)), modelgen.FactsOptions{})
			if err == nil || len(result.Artifacts) != 0 {
				t.Fatal("invalid generation did not fail atomically", err)
			}
		})
	}
	if _, err := modelgen.GenerateFactsDTOs(fixtures(t), modelgen.FactsOptions{BindingsPackage: "bad; package"}); err == nil {
		t.Fatal("invalid package accepted")
	}
}
func TestFactsGeneratorPreservesExactBoundAndExplicitGoName(t *testing.T) {
	d := fixtures(t)[1]
	p := &d.Parameters[len(d.Parameters)-2]
	p.GoName = "ContractValueRUB"
	p.Number.Lower = &model.Bound{Value: "9007199254740993", Inclusive: false}
	result, err := modelgen.GenerateFactsDTOs([]model.ModelDefinition{d}, modelgen.FactsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Artifacts[0].Data, []byte(`Value: "9007199254740993", Inclusive: false`)) || !regexp.MustCompile(`ContractValueRUB\s+\*int64`).Match(result.Artifacts[0].Data) {
		t.Fatal("exact bound/name lost")
	}
	if !bytes.Contains(result.Artifacts[1].Data, []byte("*value.ContractValueRUB")) {
		t.Fatal("accessor ignores explicit field name")
	}
	for i := range d.Parameters {
		if d.Parameters[i].Source.Kind == model.Supplier {
			d.Parameters[i].Enabled = false
		}
	}
	d.FactsSchemaVersion = ""
	result, err = modelgen.GenerateFactsDTOs([]model.ModelDefinition{d}, modelgen.FactsOptions{})
	if err != nil || len(result.Artifacts) != 0 || len(result.Versions) != 0 {
		t.Fatal("supplier-free model generated dependencies", err)
	}
}
