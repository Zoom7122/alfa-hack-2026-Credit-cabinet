package modelgen

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

//go:embed templates/*.json
var documentTemplates embed.FS

type object = map[string]any

func templateDocument(name string) (object, error) {
	data, err := documentTemplates.ReadFile("templates/" + name + ".openapi.json")
	if err != nil {
		return nil, err
	}
	var doc object
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("modelgen: template %s: %w", name, err)
	}
	return doc, nil
}
func nested(o object, key string) object { return o[key].(map[string]any) }
func schemaRef(name string) object       { return object{"$ref": "#/components/schemas/" + name} }
func jsonCopy(value object) object {
	// Templates contain only JSON-compatible values; UseNumber preserves bounds.
	data, _ := json.Marshal(value)
	var copy object
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	_ = d.Decode(&copy)
	return copy
}
func generateOpenAPI(definitions []model.ModelDefinition, facts FactsOutput, release string) ([]byte, []byte, error) {
	main, err := templateDocument("transport")
	if err != nil {
		return nil, nil, err
	}
	supplier, err := templateDocument("facts")
	if err != nil {
		return nil, nil, err
	}
	mainSchemas := nested(nested(main, "components"), "schemas")
	supplierSchemas := nested(nested(supplier, "components"), "schemas")
	versions := []string{}
	metadata := []object{}
	factsByVersion := map[string]FactsVersion{}
	for _, info := range facts.Versions {
		factsByVersion[info.Version] = info
	}
	requests, responses := []object{}, []object{}
	for _, d := range definitions {
		versions = append(versions, d.Version)
		checksum, err := DefinitionSHA256(d)
		if err != nil {
			return nil, nil, err
		}
		schemaChecksum, err := model.FactsSchemaSHA256(d.Parameters)
		if err != nil {
			return nil, nil, err
		}
		info, usesFacts := factsByVersion[d.Version]
		meta := object{"id": d.ID, "version": d.Version, "borrower_type": d.BorrowerType, "definition_sha256": checksum, "facts_schema_version": d.FactsSchemaVersion, "facts_schema_sha256": schemaChecksum, "uses_facts": usesFacts}
		if usesFacts {
			meta["facts_type"] = info.FactsType
			meta["response_type"] = info.ResponseType
		}
		metadata = append(metadata, meta)
		if !usesFacts {
			continue
		}
		named := namedFactsSchema(d)
		response := jsonCopy(supplierSchemas["ScoringFactsResponseEnvelope"].(map[string]any))
		properties := nested(response, "properties")
		properties["borrower_type"] = object{"type": "string", "const": d.BorrowerType}
		properties["model_version"] = object{"type": "string", "const": d.Version}
		properties["facts_schema_version"] = object{"type": "string", "const": d.FactsSchemaVersion}
		properties["additional_facts"] = schemaRef(info.FactsType)
		request := jsonCopy(supplierSchemas["ScoringFactsRequest"].(map[string]any))
		requestProperties := nested(request, "properties")
		requestProperties["model_version"] = object{"type": "string", "const": d.Version}
		requestProperties["facts_schema_version"] = object{"type": "string", "const": d.FactsSchemaVersion}
		requestProperties["borrower"] = object{"allOf": []object{schemaRef("FactsBorrower"), {"properties": object{"type": object{"const": d.BorrowerType}}}}}
		for name, value := range map[string]object{info.FactsType: named, info.ResponseType: response, info.FactsType + "Request": request} {
			if _, exists := mainSchemas[name]; exists {
				return nil, nil, fmt.Errorf("modelgen: OpenAPI schema name collision %s", name)
			}
			mainSchemas[name] = value
			supplierSchemas[name] = value
		}
		requests = append(requests, schemaRef(info.FactsType+"Request"))
		responses = append(responses, schemaRef(info.ResponseType))
	}
	for _, doc := range []object{main, supplier} {
		nested(doc, "info")["version"] = release
		doc["x-scoring-release"] = release
		doc["x-modelgen-documents-version"] = DocumentsFormatVersion
		doc["x-scoring-models"] = metadata
	}
	nested(main, "info")["description"] = "REST contracts for scoring, bank operations and administrative events. Models and supplier schemas are generated from checked definitions. Loan rates are percentages: 1 = 1%, 100 = 100%; money input is integer RUB."
	nested(nested(mainSchemas["DecisionRequest"].(map[string]any), "properties"), "model_version")["enum"] = versions
	nested(nested(mainSchemas["ScoreResult"].(map[string]any), "properties"), "model")["enum"] = versions
	operation := nested(nested(main, "paths"), "/api/v1/decisions")["post"].(map[string]any)
	for status, value := range nested(operation, "responses") {
		if status == "400" || status == "422" {
			continue
		}
		value.(map[string]any)["headers"] = object{"X-Scoring-Model-Version": object{"description": "Exact selected model, including score=null and dependency errors; absent if selection failed.", "schema": object{"type": "string", "enum": versions}}}
	}
	if len(requests) > 0 {
		supplierSchemas["PublishedScoringFactsRequest"] = object{"oneOf": requests}
		supplierSchemas["PublishedScoringFactsResponse"] = object{"oneOf": responses}
		path := nested(nested(supplier, "paths"), "/api/v1/scoring-facts")
		post := nested(path, "post")
		nested(nested(nested(post, "requestBody"), "content"), "application/json")["schema"] = schemaRef("PublishedScoringFactsRequest")
		success := nested(nested(post, "responses"), "200")
		success["description"] = "Exact named response for a published model/schema pair; every fact key is required and nullable."
		nested(nested(success, "content"), "application/json")["schema"] = schemaRef("PublishedScoringFactsResponse")
		// The supplier file is standalone; its error schemas share the transport template.
		for _, value := range nested(post, "responses") {
			response := value.(map[string]any)
			if content, ok := response["content"].(map[string]any); ok {
				if problem, ok := content["application/problem+json"].(map[string]any); ok {
					problem["schema"] = schemaRef("Problem")
				}
			}
		}
		supplierSchemas["Problem"] = mainSchemas["Problem"]
		for _, name := range []string{"FactsBorrower", "PublishedScoringFactsRequest", "PublishedScoringFactsResponse"} {
			mainSchemas[name] = supplierSchemas[name]
		}
		nested(main, "paths")["/api/v1/scoring-facts"] = path
	} else {
		supplier["paths"] = object{}
		nested(supplier, "info")["description"] = "This release contains no models requiring supplier facts; MS1 does not call the supplier."
	}
	a, err := json.MarshalIndent(main, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	b, err := json.MarshalIndent(supplier, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(a, '\n'), append(b, '\n'), nil
}

func namedFactsSchema(d model.ModelDefinition) object {
	properties := object{}
	required := []string{}
	for _, p := range d.Parameters {
		if !p.Enabled || p.Source.Kind != model.Supplier {
			continue
		}
		kind := "number"
		if p.Type == model.Boolean {
			kind = "boolean"
		} else if p.Number.Integer {
			kind = "integer"
		}
		field := object{"type": []string{kind, "null"}, "title": p.Title, "description": p.Description, "x-unit": p.Unit, "x-business-required": p.Required}
		if p.RequiredWhen != nil {
			field["x-required-when"] = p.RequiredWhen
		}
		if len(p.DependsOn) > 0 {
			field["x-depends-on"] = p.DependsOn
		}
		if p.Type == model.Number {
			field["format"] = "double"
			if p.Number.Integer {
				field["format"] = "int64"
				field["allOf"] = []object{{"minimum": json.Number("-9223372036854775808"), "maximum": json.Number("9223372036854775807")}}
			}
			if bound := p.Number.Lower; bound != nil {
				key := "minimum"
				if !bound.Inclusive {
					key = "exclusiveMinimum"
				}
				field[key] = bound.Value
			}
			if bound := p.Number.Upper; bound != nil {
				key := "maximum"
				if !bound.Inclusive {
					key = "exclusiveMaximum"
				}
				field[key] = bound.Value
			}
		}
		properties[p.JSONName] = field
		required = append(required, p.JSONName)
	}
	sort.Strings(required)
	return object{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}
