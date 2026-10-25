// Package modelgen generates build-time artifacts from checked definitions.
// Generation is pure: callers decide when/where allowed files are written.
package modelgen

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/format"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

const modulePath = "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine"

type Artifact struct {
	Path string
	Data []byte
}
type FactsOptions struct {
	BindingsPackage string
	// TypePrefix isolates compiled test fixtures from real release types.
	// Release generation leaves this empty.
	TypePrefix string
}
type FactsVersion struct {
	Version, SchemaVersion                                                           string
	BorrowerType                                                                     contracts.BorrowerType
	FactsType, ResponseType, DecodeFunction, AccessorFunction, DTOPath, BindingsPath string
}
type FactsOutput struct {
	Artifacts []Artifact
	Versions  []FactsVersion
}

func GenerateFactsDTOs(definitions []model.ModelDefinition, options FactsOptions) (FactsOutput, error) {
	pkg := options.BindingsPackage
	if pkg == "" {
		pkg = "modelbindings"
	}
	if pkg == "_" || !token.IsIdentifier(pkg) || token.Lookup(pkg).IsKeyword() {
		return FactsOutput{}, fmt.Errorf("modelgen: invalid bindings package")
	}
	if prefix := options.TypePrefix; prefix != "" && (len(prefix) > 64 || !token.IsIdentifier(prefix) || prefix[0] < 'A' || prefix[0] > 'Z') {
		return FactsOutput{}, fmt.Errorf("modelgen: invalid type prefix")
	}
	models := append([]model.ModelDefinition(nil), definitions...)
	sort.Slice(models, func(i, j int) bool { return models[i].Version < models[j].Version })
	result := FactsOutput{Artifacts: []Artifact{}, Versions: []FactsVersion{}}
	versions, symbols, paths, schemas := map[string]bool{}, map[string]string{}, map[string]string{}, map[string]string{}
	for _, d := range models {
		catalog, err := sourcecatalog.Bindings(d.BorrowerType)
		if err != nil {
			return FactsOutput{}, err
		}
		validation := model.Validate(d, catalog)
		if !validation.Valid {
			return FactsOutput{}, &ValidationError{Version: d.Version, Result: validation}
		}
		if versions[d.Version] {
			return FactsOutput{}, fmt.Errorf("modelgen: duplicate model version %s", d.Version)
		}
		versions[d.Version] = true
		fields := []model.Parameter{}
		for _, p := range d.Parameters {
			if p.Enabled && p.Source.Kind == model.Supplier {
				fields = append(fields, p)
			}
		}
		if len(fields) == 0 {
			continue
		}
		signature, err := model.FactsSchemaSHA256(fields)
		if err != nil {
			return FactsOutput{}, err
		}
		if previous, ok := schemas[d.FactsSchemaVersion]; ok && previous != signature {
			return FactsOutput{}, fmt.Errorf("modelgen: facts schema %s has incompatible fields/domains; use a new schema version", d.FactsSchemaVersion)
		}
		schemas[d.FactsSchemaVersion] = signature
		name := options.TypePrefix + factsName(d)
		info := FactsVersion{Version: d.Version, SchemaVersion: d.FactsSchemaVersion, BorrowerType: d.BorrowerType, FactsType: name, ResponseType: name + "Response", DecodeFunction: "Decode" + name + "Response", AccessorFunction: name + "Values"}
		for _, symbol := range []string{info.FactsType, info.ResponseType, info.DecodeFunction} {
			if previous, ok := symbols[symbol]; ok {
				return FactsOutput{}, fmt.Errorf("modelgen: Go name %s collides between %s and %s", symbol, previous, d.Version)
			}
			symbols[symbol] = d.Version
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(d.Version)))
		info.DTOPath = "contracts/facts_" + hash[:16] + "_generated.go"
		info.BindingsPath = "internal/modelbindings/facts_" + hash[:16] + "_generated.go"
		for _, path := range []string{info.DTOPath, info.BindingsPath} {
			if previous, ok := paths[path]; ok {
				return FactsOutput{}, fmt.Errorf("modelgen: artifact path collision between %s and %s", previous, d.Version)
			}
			paths[path] = d.Version
		}
		dto, err := factsDTO(d, fields, info)
		if err != nil {
			return FactsOutput{}, err
		}
		binding, err := factsBinding(d, fields, info, pkg)
		if err != nil {
			return FactsOutput{}, err
		}
		result.Artifacts = append(result.Artifacts, Artifact{Path: info.DTOPath, Data: dto}, Artifact{Path: info.BindingsPath, Data: binding})
		result.Versions = append(result.Versions, info)
	}
	return result, nil
}

func factsName(d model.ModelDefinition) string {
	prefix, version := "LegalFacts", strings.TrimPrefix(d.Version, "legal-")
	if d.BorrowerType == contracts.BorrowerTypeSoleProprietor {
		prefix, version = "IPFacts", strings.TrimPrefix(d.Version, "ip-")
	}
	parts := strings.FieldsFunc(version, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	for _, part := range parts {
		prefix += strings.ToUpper(part[:1]) + part[1:]
	}
	return prefix
}

func boundCode(bound *model.Bound) string {
	if bound == nil {
		return "nil"
	}
	return fmt.Sprintf("&jsonstrict.Bound{Value:%q, Inclusive:%t}", bound.Value.String(), bound.Inclusive)
}
func fieldType(p model.Parameter) string {
	if p.Type == model.Boolean {
		return "bool"
	}
	if p.Number.Integer {
		return "int64"
	}
	return "float64"
}

func factsDTO(d model.ModelDefinition, fields []model.Parameter, info FactsVersion) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by Credit Engine modelgen. DO NOT EDIT.\n// Model: %s; facts schema: %s.\npackage contracts\n\nimport (\"encoding/json\";\"io\"; %q)\n", d.Version, d.FactsSchemaVersion, modulePath+"/internal/jsonstrict")
	fmt.Fprintf(&out, "type %s struct {\n", info.FactsType)
	for _, p := range fields {
		fmt.Fprintf(&out, "// %s; unit %s; explicit null means unknown.\n%s *%s `json:%q`\n", p.JSONName, p.Unit, model.GeneratedGoName(p), fieldType(p), p.JSONName)
	}
	fmt.Fprintln(&out, "}")
	keys := []string{}
	for _, p := range fields {
		keys = append(keys, strconv.Quote(p.JSONName))
	}
	keyCode := "[]string{" + strings.Join(keys, ",") + "}"
	fmt.Fprintf(&out, "func (value *%s) UnmarshalJSON(data []byte) error { object,err:=jsonstrict.Object(data,\"/additional_facts\",%s,%s);if err!=nil{return factsJSONError(err)};var decoded %s\n", info.FactsType, keyCode, keyCode, info.FactsType)
	for _, p := range fields {
		name := model.GeneratedGoName(p)
		path := "/additional_facts/" + p.JSONName
		fmt.Fprintf(&out, "if !jsonstrict.Null(object[%q]) {\n", p.JSONName)
		if p.Type == model.Boolean {
			fmt.Fprintf(&out, "v,err:=jsonstrict.Boolean(object[%q],%q)\n", p.JSONName, path)
		} else {
			fn := "Float64"
			if p.Number.Integer {
				fn = "Int64"
			}
			fmt.Fprintf(&out, "v,err:=jsonstrict.%s(object[%q],%q,%s,%s)\n", fn, p.JSONName, path, boundCode(p.Number.Lower), boundCode(p.Number.Upper))
		}
		fmt.Fprintf(&out, "if err!=nil{return factsJSONError(err)};decoded.%s=&v\n}\n", name)
	}
	fmt.Fprintln(&out, "*value=decoded;return nil\n}")
	fmt.Fprintf(&out, "type %s struct {ScoringFactsMetadata;AdditionalFacts %s `json:\"additional_facts\"`}\n", info.ResponseType, info.FactsType)
	parser := "parse" + info.ResponseType
	fmt.Fprintf(&out, "func %s(data []byte,expected *ScoringFactsExpectation)(%s,error){var result %s;object,err:=jsonstrict.Object(data,\"/\",[]string{\"application_id\",\"borrower_id\",\"borrower_type\",\"model_version\",\"facts_schema_version\",\"facts_date\",\"additional_facts\"},nil);if err!=nil{return result,factsJSONError(err)}\n", parser, info.ResponseType, info.ResponseType)
	fmt.Fprintf(&out, "metadata,err:=decodeFactsMetadata(object,expected,%q,%q,BorrowerType(%q));if err!=nil{return result,err};result.ScoringFactsMetadata=metadata;if err=json.Unmarshal(object[\"additional_facts\"],&result.AdditionalFacts);err!=nil{return %s{},err};return result,nil\n}\n", d.Version, d.FactsSchemaVersion, d.BorrowerType, info.ResponseType)
	fmt.Fprintf(&out, "func (value *%s) UnmarshalJSON(data []byte)error{decoded,err:=%s(data,nil);if err!=nil{return err};*value=decoded;return nil}\n", info.ResponseType, parser)
	fmt.Fprintf(&out, "func %s(reader io.Reader,expected ScoringFactsExpectation)(%s,error){data,err:=jsonstrict.Read(reader);if err!=nil{return %s{},factsJSONError(err)};return %s(data,&expected)}\n", info.DecodeFunction, info.ResponseType, info.ResponseType, parser)
	code, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("modelgen: format facts DTO %s: %w", d.Version, err)
	}
	return code, nil
}

func factsBinding(d model.ModelDefinition, fields []model.Parameter, info FactsVersion, pkg string) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "// Code generated by Credit Engine modelgen. DO NOT EDIT.\n// Model: %s; facts schema: %s.\npackage %s\nimport (%q;%q)\n", d.Version, d.FactsSchemaVersion, pkg, modulePath+"/contracts", modulePath+"/internal/rules")
	fmt.Fprintf(&out, "func %s(value contracts.%s)map[string]rules.Value{result:=map[string]rules.Value{}\n", info.AccessorFunction, info.FactsType)
	for _, p := range fields {
		name := model.GeneratedGoName(p)
		factory := "Float64"
		if p.Type == model.Boolean {
			factory = "Boolean"
		} else if p.Number.Integer {
			factory = "Int64"
		}
		fmt.Fprintf(&out, "result[%q]=rules.Unknown();if value.%s!=nil {result[%q]=rules.%s(*value.%s)}\n", p.JSONName, name, p.JSONName, factory, name)
	}
	fmt.Fprintln(&out, "return result\n}")
	code, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("modelgen: format facts bindings %s: %w", d.Version, err)
	}
	return code, nil
}
