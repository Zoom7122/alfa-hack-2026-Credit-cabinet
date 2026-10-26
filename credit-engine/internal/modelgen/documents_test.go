package modelgen_test

import (
	"bytes"
	"encoding/json"
	"html"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

func documentArtifacts(t *testing.T, definitions []model.ModelDefinition, input []byte) map[string][]byte {
	t.Helper()
	output, err := modelgen.GenerateDocuments(definitions, modelgen.DocumentOptions{ReleaseVersion: "2.0.0", InputsMarkdown: input})
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string][]byte{}
	for _, artifact := range output.Artifacts {
		if _, exists := artifacts[artifact.Path]; exists {
			t.Fatal("duplicate artifact")
		}
		artifacts[artifact.Path] = artifact.Data
	}
	if len(artifacts) != 3 {
		t.Fatal(len(artifacts))
	}
	return artifacts
}
func documentObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var result map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func documentMap(o map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		o = o[key].(map[string]any)
	}
	return o
}

func TestDocumentsAreReproducibleAndPreserveHandwrittenSections(t *testing.T) {
	definitions := baseModels(t)
	input := []byte("# Handwritten\r\n\r\nFormula x/y stays unchanged.\r\n")
	before := compactDefinitions(t, definitions)
	first := documentArtifacts(t, definitions, input)
	second := documentArtifacts(t, definitions, first["docs/02_inputs_and_scoring.md"])
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated generation changed bytes")
	}
	definitions[0], definitions[1] = definitions[1], definitions[0]
	if !reflect.DeepEqual(first, documentArtifacts(t, definitions, input)) {
		t.Fatal("model order changed documents")
	}
	definitions[0], definitions[1] = definitions[1], definitions[0]
	if !reflect.DeepEqual(before, compactDefinitions(t, definitions)) {
		t.Fatal("generation mutated models")
	}
	markdown := first["docs/02_inputs_and_scoring.md"]
	if !bytes.HasPrefix(markdown, input) || bytes.Count(markdown, []byte(modelgen.ModelsMarkdownStart)) != 1 || bytes.Count(markdown, []byte(modelgen.ModelsMarkdownEnd)) != 1 {
		t.Fatal("manual bytes or markers changed")
	}
	plain := html.UnescapeString(string(markdown))
	for _, d := range definitions {
		checksum, err := modelgen.DefinitionSHA256(d)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plain, checksum) || !strings.Contains(plain, d.Version) {
			t.Fatal("missing identity")
		}
		for _, p := range d.Parameters {
			if !strings.Contains(plain, p.ID+" / "+p.Title) {
				t.Fatal("missing parameter", p.ID)
			}
		}
		for _, c := range d.Criteria {
			if !strings.Contains(plain, c.ID+" / "+c.Title) {
				t.Fatal("missing criterion", c.ID)
			}
			for _, entry := range append(append([]model.ScoreCase{}, c.SpecialCases...), c.Cases...) {
				if !strings.Contains(plain, c.ID+" / "+entry.ID) {
					t.Fatal("missing case", c.ID, entry.ID)
				}
			}
		}
		for _, r := range append(append([]model.Rule{}, d.Stops...), d.ManualReviews...) {
			if !strings.Contains(plain, r.ID+" / "+r.Title) || !strings.Contains(plain, r.Reason.Code) {
				t.Fatal("missing rule", r.ID)
			}
		}
		for _, c := range d.DataChecks {
			if !strings.Contains(plain, c.ID+" / "+c.Title) {
				t.Fatal("missing check", c.ID)
			}
		}
	}
	for _, data := range []([]byte){first["contracts/openapi.json"], first["contracts/scoring_facts.openapi.json"]} {
		doc := documentObject(t, data)
		assertDocumentRefs(t, doc, doc)
		if doc["x-scoring-release"] != "2.0.0" {
			t.Fatal(doc)
		}
	}
	if len(documentMap(documentObject(t, first["contracts/scoring_facts.openapi.json"]), "paths")) != 0 {
		t.Fatal("v2 added a supplier route")
	}
}
func compactDefinitions(t *testing.T, definitions []model.ModelDefinition) [][]byte {
	t.Helper()
	result := [][]byte{}
	for _, d := range definitions {
		data, err := model.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, data)
	}
	return result
}

func TestGeneratedOpenAPINamedFieldsVersionsAndExactBounds(t *testing.T) {
	definitions := append(baseModels(t), fixtures(t)...)
	for i := 2; i < len(definitions); i++ {
		definitions[i].Parameters[len(definitions[i].Parameters)-2].GoName = "AnnualContractValueRUB"
	}
	numeric := &definitions[3].Parameters[len(definitions[3].Parameters)-2]
	numeric.Number.Lower = &model.Bound{Value: "9007199254740993", Inclusive: false}
	numeric.Number.Upper = &model.Bound{Value: "9223372036854775807", Inclusive: true}
	unused := definitions[2].Parameters[len(definitions[2].Parameters)-1]
	unused.ID = "disabled_flag"
	unused.JSONName = "disabled_flag"
	unused.Source.Path = "disabled_flag"
	unused.Enabled = false
	definitions[2].Parameters = append(definitions[2].Parameters, unused)
	artifacts := documentArtifacts(t, definitions, []byte("manual\n"))
	main := documentObject(t, artifacts["contracts/openapi.json"])
	supplier := documentObject(t, artifacts["contracts/scoring_facts.openapi.json"])
	assertDocumentRefs(t, main, main)
	assertDocumentRefs(t, supplier, supplier)
	facts, err := modelgen.GenerateFactsDTOs(definitions, modelgen.FactsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range facts.Versions {
		t.Run(info.Version, func(t *testing.T) {
			schema := documentMap(main, "components", "schemas", info.FactsType)
			if !reflect.DeepEqual(schema, documentMap(supplier, "components", "schemas", info.FactsType)) {
				t.Fatal("standalone schema drift")
			}
			if schema["additionalProperties"] != false || len(schema["required"].([]any)) != 2 {
				t.Fatal(schema)
			}
			properties := documentMap(schema, "properties")
			if len(properties) != 2 || properties["disabled_flag"] != nil {
				t.Fatal(properties)
			}
			flag := documentMap(properties, "has_confirmed_contracts")
			if !reflect.DeepEqual(flag["type"], []any{"boolean", "null"}) || flag["x-business-required"] != false {
				t.Fatal(flag)
			}
			number := documentMap(properties, "annual_contract_value_rub")
			if number["x-unit"] != "RUB" || number["x-business-required"] != true {
				t.Fatal(number)
			}
			if info.Version == "ip-demo-v2" {
				if !reflect.DeepEqual(number["type"], []any{"integer", "null"}) || number["format"] != "int64" || number["exclusiveMinimum"].(json.Number).String() != "9007199254740993" || number["maximum"].(json.Number).String() != "9223372036854775807" {
					t.Fatal(number)
				}
			} else if !reflect.DeepEqual(number["type"], []any{"number", "null"}) || number["minimum"].(json.Number).String() != "0" {
				t.Fatal(number)
			}
			response := documentMap(main, "components", "schemas", info.ResponseType, "properties")
			if documentMap(response, "model_version")["const"] != info.Version || documentMap(response, "facts_schema_version")["const"] != info.SchemaVersion || documentMap(response, "borrower_type")["const"] != string(info.BorrowerType) {
				t.Fatal(response)
			}
			request := documentMap(main, "components", "schemas", info.FactsType+"Request", "properties")
			if documentMap(request, "model_version")["const"] != info.Version || documentMap(request, "facts_schema_version")["const"] != info.SchemaVersion {
				t.Fatal(request)
			}
		})
	}
	if strings.Contains(string(artifacts["contracts/scoring_facts.openapi.json"]), "ExampleNamedFacts") {
		t.Fatal("illustrative schema leaked into release")
	}
	version := documentMap(main, "components", "schemas", "DecisionRequest", "properties", "model_version")
	if version["type"] != "string" || len(version["enum"].([]any)) != 5 {
		t.Fatal(version)
	}
	metadata := main["x-scoring-models"].([]any)
	if len(metadata) != 5 {
		t.Fatal(metadata)
	}
	if !strings.Contains(string(artifacts["docs/02_inputs_and_scoring.md"]), "AnnualContractValueRUB") {
		t.Fatal("Go field name missing")
	}
}

func assertDocumentRefs(t *testing.T, root map[string]any, value any) {
	t.Helper()
	switch current := value.(type) {
	case map[string]any:
		if ref, ok := current["$ref"].(string); ok {
			if !strings.HasPrefix(ref, "#/") {
				t.Fatal("nonlocal schema reference", ref)
			}
			var target any = root
			for _, key := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				key = strings.NewReplacer("~1", "/", "~0", "~").Replace(key)
				o, ok := target.(map[string]any)
				if !ok {
					t.Fatal("invalid ref", ref)
				}
				target, ok = o[key]
				if !ok {
					t.Fatal("unresolved ref", ref)
				}
			}
		}
		for _, child := range current {
			assertDocumentRefs(t, root, child)
		}
	case []any:
		for _, child := range current {
			assertDocumentRefs(t, root, child)
		}
	}
}

func TestManagedMarkdownPreservesSuffixAndRejectsAmbiguity(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(newline, "\r", "CR"), func(t *testing.T) {
			prefix := "manual prefix" + newline + modelgen.ModelsMarkdownStart
			suffix := modelgen.ModelsMarkdownEnd + newline + "manual suffix\x00 unchanged"
			original := []byte(prefix + newline + "old" + newline + suffix)
			got, err := modelgen.UpdateModelsMarkdown(original, []byte("new\ncontent\n"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != prefix+newline+"new"+newline+"content"+newline+suffix {
				t.Fatal(string(got))
			}
		})
	}
	for _, input := range []string{modelgen.ModelsMarkdownStart, modelgen.ModelsMarkdownEnd, modelgen.ModelsMarkdownEnd + "\n" + modelgen.ModelsMarkdownStart, modelgen.ModelsMarkdownStart + "\n" + modelgen.ModelsMarkdownStart + "\n" + modelgen.ModelsMarkdownEnd, "inline " + modelgen.ModelsMarkdownStart + "\n" + modelgen.ModelsMarkdownEnd, modelgen.ModelsMarkdownStart + " trailing\n" + modelgen.ModelsMarkdownEnd} {
		if _, err := modelgen.UpdateModelsMarkdown([]byte(input), []byte("new")); err == nil {
			t.Fatal("ambiguous markers accepted", input)
		}
	}
	if _, err := modelgen.UpdateModelsMarkdown(nil, []byte(modelgen.ModelsMarkdownStart)); err == nil {
		t.Fatal("nested marker accepted")
	}
}
func TestDocumentsEscapeModelTextAndFailAtomically(t *testing.T) {
	definitions := baseModels(t)
	hostile := "<script>alert(1)</script> | [click](https://invalid) `code`\n# heading\n" + modelgen.ModelsMarkdownStart
	definitions[0].Title = hostile
	definitions[0].Description = hostile
	definitions[0].Criteria[0].Title = hostile
	output := documentArtifacts(t, definitions, []byte("manual\n"))
	md := string(output["docs/02_inputs_and_scoring.md"])
	if strings.Contains(md, "<script>") || strings.Contains(md, "[click]") || strings.Contains(md, "`code`") || strings.Contains(md, "\n# heading") || strings.Count(md, modelgen.ModelsMarkdownStart) != 1 || !strings.Contains(md, "&#124;") {
		t.Fatal("Markdown injection", md[:500])
	}
	for _, release := range []string{"", "../escape", "bad version", "x\nheader"} {
		out, err := modelgen.GenerateDocuments(definitions, modelgen.DocumentOptions{ReleaseVersion: release})
		if err == nil || len(out.Artifacts) != 0 {
			t.Fatal(release, err)
		}
	}
	definitions[0].Criteria[0].MaxPoints--
	if out, err := modelgen.GenerateDocuments(definitions, modelgen.DocumentOptions{ReleaseVersion: "release-1"}); err == nil || len(out.Artifacts) != 0 {
		t.Fatal("invalid model was documented", err)
	}
}

func TestGeneratedDocumentsMatchPublishedArtifacts(t *testing.T) {
	input, err := os.ReadFile("../../docs/02_inputs_and_scoring.md")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../models/release.json")
	if err != nil {
		t.Fatal(err)
	}
	var current struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &current); err != nil {
		t.Fatal(err)
	}
	output, err := modelgen.GenerateDocuments(baseModels(t), modelgen.DocumentOptions{ReleaseVersion: current.Version, InputsMarkdown: input})
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range output.Artifacts {
		path, generated := artifact.Path, artifact.Data
		current, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, generated) {
			t.Fatal("stale generated document", path)
		}
	}
}

func TestGeneratedDocumentsPreserveExistingTransportSchemas(t *testing.T) {
	data, err := os.ReadFile("templates/transport.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	base := documentObject(t, data)
	output := documentArtifacts(t, baseModels(t), nil)
	doc := documentObject(t, output["contracts/openapi.json"])
	for name, original := range documentMap(base, "components", "schemas") {
		got := documentMap(doc, "components", "schemas", name)
		switch name {
		case "DecisionRequest":
			delete(documentMap(got, "properties", "model_version"), "enum")
		case "ScoreResult":
			delete(documentMap(got, "properties", "model"), "enum")
		}
		if !reflect.DeepEqual(got, original) {
			t.Fatal("existing transport schema changed", name)
		}
	}
	for path, original := range documentMap(base, "paths") {
		if path == "/api/v1/decisions" {
			continue
		}
		if !reflect.DeepEqual(documentMap(doc, "paths")[path], original) {
			t.Fatal("existing transport operation changed", path)
		}
	}
}
