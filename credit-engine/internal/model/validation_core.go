package model

import (
	"fmt"
	"go/token"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid                bool    `json:"valid"`
	Issues               []Issue `json:"issues"`
	QuestionnaireMaximum int     `json:"questionnaire_maximum"`
	AdditionalMaximum    int     `json:"additional_maximum"`
	TotalMaximum         int     `json:"total_maximum"`
}

var parameterIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var objectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var jsonNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
var reasonCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

type validator struct {
	d              ModelDefinition
	result         ValidationResult
	parameters     map[string]Parameter
	parameterPaths map[string]string
	sources        map[Source]string
	bindings       map[Source]Binding
	dependencies   map[string]map[string]bool
	conditionNodes int
	coverageStates int
}

func (v *validator) issue(path, code, message string) {
	v.result.Issues = append(v.result.Issues, Issue{path, code, message})
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// GeneratedGoName is shared with DTO generation so collision checks and the
// emitted exported identifiers cannot use different naming rules.
func GeneratedGoName(p Parameter) string {
	if p.GoName != "" {
		return p.GoName
	}
	initialisms := map[string]bool{"id": true, "inn": true, "url": true, "http": true, "api": true, "rub": true, "npv": true, "cfads": true, "pd": true}
	var result strings.Builder
	for _, part := range strings.Split(p.JSONName, "_") {
		if part == "" {
			continue
		}
		if initialisms[part] {
			result.WriteString(strings.ToUpper(part))
		} else {
			r, size := utf8.DecodeRuneInString(part)
			result.WriteRune(unicode.ToUpper(r))
			result.WriteString(part[size:])
		}
	}
	return result.String()
}

func exportedIdentifier(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return token.IsIdentifier(name) && unicode.IsUpper(r)
}

func (v *validator) metadata() {
	if v.d.FormatVersion != FormatVersion {
		v.issue("/format_version", "UNSUPPORTED_FORMAT", "Expected format_version=1.")
	}
	for _, field := range []struct{ path, value string }{{"id", v.d.ID}, {"version", v.d.Version}} {
		if len(field.value) > 128 || !objectIDPattern.MatchString(field.value) {
			v.issue("/"+field.path, "INVALID_ID", "Use a nonempty stable ASCII identifier, at most 128 bytes.")
		}
	}
	if strings.TrimSpace(v.d.Title) == "" {
		v.issue("/title", "REQUIRED", "Model title is required.")
	}
	if v.d.BorrowerType != contracts.BorrowerTypeSoleProprietor && v.d.BorrowerType != contracts.BorrowerTypeLegalEntity {
		v.issue("/borrower_type", "INVALID_BORROWER_TYPE", "Expected SOLE_PROPRIETOR or LEGAL_ENTITY.")
	}
	if v.d.FactsSchemaVersion != "" && (len(v.d.FactsSchemaVersion) > 128 || !objectIDPattern.MatchString(v.d.FactsSchemaVersion)) {
		v.issue("/facts_schema_version", "INVALID_ID", "Invalid facts schema version.")
	}
	for _, list := range []struct {
		path    string
		missing bool
	}{{"parameters", v.d.Parameters == nil}, {"criteria", v.d.Criteria == nil}, {"data_checks", v.d.DataChecks == nil}, {"stops", v.d.Stops == nil}, {"manual_reviews", v.d.ManualReviews == nil}, {"classes", v.d.Classes == nil}} {
		if list.missing {
			v.issue("/"+list.path, "REQUIRED_ARRAY", "Use an explicit array, including [] when empty.")
		}
	}
}

func validTypeUnit(t ValueType, u Unit) bool {
	switch t {
	case Number:
		return u == RUB || u == Ratio || u == Percent || u == Count || u == Months || u == Years || u == Days
	case Boolean:
		return u == Flag
	case String:
		return u == Category || u == Date
	}
	return false
}

func (v *validator) parameterMetadata() {
	jsonNames := map[string]string{}
	goNames := map[string]string{}
	activeSupplier := false
	for i, p := range v.d.Parameters {
		path := fmt.Sprintf("/parameters/%d", i)
		if len(p.ID) > 128 || !parameterIDPattern.MatchString(p.ID) {
			v.issue(path+"/id", "INVALID_ID", "Use a lowercase parameter identifier with letters, digits and underscores.")
		}
		if old, ok := v.parameterPaths[p.ID]; ok {
			v.issue(path+"/id", "DUPLICATE_ID", "Parameter ID already appears at "+old+".")
		} else {
			v.parameters[p.ID] = p
			v.parameterPaths[p.ID] = path
			v.dependencies[p.ID] = map[string]bool{}
		}
		if strings.TrimSpace(p.Title) == "" {
			v.issue(path+"/title", "REQUIRED", "Parameter title is required.")
		}
		if !validTypeUnit(p.Type, p.Unit) {
			v.issue(path+"/unit", "TYPE_UNIT_MISMATCH", "The value type and unit are incompatible.")
		}
		if strings.TrimSpace(p.JSONName) == "" {
			v.issue(path+"/json_name", "REQUIRED", "JSON name is required.")
		}
		if p.RequiredWhen != nil && !p.Required {
			v.issue(path+"/required_when", "INACTIVE_REQUIRED_CONDITION", "required_when requires required=true.")
		}
		if strings.TrimSpace(p.Source.Path) == "" {
			v.issue(path+"/source/path", "REQUIRED", "Source path is required.")
		}
		scope := string(p.Source.Kind) + ":"
		if p.Source.Kind == Application || p.Source.Kind == Statements {
			if dot := strings.LastIndex(p.Source.Path, "."); dot >= 0 {
				scope += p.Source.Path[:dot]
			}
		}
		key := scope + ":" + p.JSONName
		if old, ok := jsonNames[key]; ok {
			v.issue(path+"/json_name", "DUPLICATE_JSON_NAME", "JSON name collides with "+old+" in the same object.")
		} else {
			jsonNames[key] = path
		}
		if old, ok := v.sources[p.Source]; ok {
			v.issue(path+"/source", "DUPLICATE_SOURCE", "Source binding already belongs to "+old+".")
		} else {
			v.sources[p.Source] = p.ID
		}
		switch p.Source.Kind {
		case Supplier:
			activeSupplier = activeSupplier || p.Enabled
			if p.Type != Number && p.Type != Boolean {
				v.issue(path+"/type", "SUPPLIER_TYPE", "New supplier facts must be NUMBER or BOOLEAN.")
			}
			if !jsonNamePattern.MatchString(p.JSONName) || p.Source.Path != p.JSONName {
				v.issue(path+"/json_name", "INVALID_JSON_NAME", "Supplier source path must equal its lowercase snake_case JSON name.")
			}
			name := GeneratedGoName(p)
			if !exportedIdentifier(name) || name == "MarshalJSON" || name == "UnmarshalJSON" {
				v.issue(path+"/go_name", "INVALID_GO_NAME", "Expected a nonreserved exported Go identifier.")
			}
			if old, ok := goNames[name]; ok {
				v.issue(path+"/go_name", "GO_NAME_COLLISION", "Generated field collides with "+old+".")
			} else {
				goNames[name] = path
			}
		case Application, Statements, Computed:
			leaf := p.Source.Path
			if dot := strings.LastIndex(leaf, "."); dot >= 0 {
				leaf = leaf[dot+1:]
			}
			if p.JSONName != leaf {
				v.issue(path+"/json_name", "JSON_NAME_MISMATCH", "Existing source JSON names must match the trusted source path.")
			}
			binding, ok := v.bindings[p.Source]
			if !ok {
				v.issue(path+"/source", "UNKNOWN_SOURCE", "Source is not in the trusted existing-source catalog.")
			} else if binding.Type != p.Type || binding.Unit != p.Unit {
				v.issue(path+"/source", "SOURCE_TYPE_MISMATCH", "Type/unit differ from the trusted source binding.")
			}
			if ok && binding.BorrowerType != "" && binding.BorrowerType != v.d.BorrowerType {
				v.issue(path+"/source", "SOURCE_BORROWER_MISMATCH", "Source belongs to the other borrower type.")
			}
			if p.GoName != "" {
				v.issue(path+"/go_name", "SOURCE_NAME_OVERRIDE", "Existing source Go names cannot be overridden.")
			}
		default:
			v.issue(path+"/source/kind", "INVALID_SOURCE_KIND", "Unknown source kind.")
		}
		if p.Type == Number {
			if p.Number == nil {
				v.issue(path+"/number", "REQUIRED_NUMBER_DOMAIN", "Declare the numeric domain, including integer=true/false.")
			} else {
				boundsOK := true
				for _, b := range []struct {
					name  string
					bound *Bound
				}{{"lower", p.Number.Lower}, {"upper", p.Number.Upper}} {
					if b.bound != nil && !validNumber(b.bound.Value) {
						v.issue(path+"/number/"+b.name, "NONFINITE_BOUND", "Domain bounds must be finite.")
						boundsOK = false
					}
				}
				if boundsOK && !v.numberDomainNonempty(p) {
					v.issue(path+"/number", "EMPTY_DOMAIN", "The declared numeric domain has no reachable value.")
				}
			}
		} else if p.Number != nil {
			v.issue(path+"/number", "UNEXPECTED_NUMBER_DOMAIN", "Only numeric parameters have numeric domains.")
		}
		if p.Type == String && p.Unit == Category {
			if len(p.StringValues) == 0 {
				v.issue(path+"/string_values", "EMPTY_DOMAIN", "Categorical source values must be enumerated.")
			}
			seen := map[string]bool{}
			for j, value := range p.StringValues {
				if strings.TrimSpace(value) == "" || seen[value] {
					v.issue(fmt.Sprintf("%s/string_values/%d", path, j), "INVALID_ENUM_VALUE", "Enum values must be nonempty and unique.")
				}
				seen[value] = true
			}
		} else if len(p.StringValues) > 0 {
			v.issue(path+"/string_values", "UNEXPECTED_ENUM", "Only categorical string parameters have string_values.")
		}
	}
	if activeSupplier && v.d.FactsSchemaVersion == "" {
		v.issue("/facts_schema_version", "REQUIRED_SCHEMA_VERSION", "Enabled supplier facts require an explicit schema version.")
	}
}

func (v *validator) classes() {
	if len(v.d.Classes) != 4 {
		v.issue("/classes", "CLASS_COUNT", "Exactly the four grades A-D are required.")
	}
	seen := map[contracts.Grade]bool{}
	indexes := make([]int, len(v.d.Classes))
	for i, c := range v.d.Classes {
		indexes[i] = i
		path := fmt.Sprintf("/classes/%d", i)
		if c.Grade != contracts.GradeA && c.Grade != contracts.GradeB && c.Grade != contracts.GradeC && c.Grade != contracts.GradeD {
			v.issue(path+"/grade", "INVALID_GRADE", "Expected A, B, C or D.")
		}
		if seen[c.Grade] {
			v.issue(path+"/grade", "DUPLICATE_GRADE", "Grade appears more than once.")
		}
		seen[c.Grade] = true
		if c.MinTotal < 0 || c.MaxTotal > TotalMaximum || c.MinTotal > c.MaxTotal {
			v.issue(path, "INVALID_CLASS_RANGE", "Inclusive class bounds must form a nonempty subset of 0..1000.")
		}
		if c.Grade == contracts.GradeC || c.Grade == contracts.GradeD {
			if c.Reason == nil {
				v.issue(path+"/reason", "REQUIRED_REASON", "Grades C and D need an explicit reason.")
			} else {
				v.reason(*c.Reason, path+"/reason")
			}
		} else if c.Reason != nil {
			v.issue(path+"/reason", "UNEXPECTED_CLASS_REASON", "A/B proceed to selection without a class review/rejection reason.")
		}
	}
	sort.SliceStable(indexes, func(i, j int) bool { return v.d.Classes[indexes[i]].MinTotal < v.d.Classes[indexes[j]].MinTotal })
	next := 0
	order := []contracts.Grade{contracts.GradeD, contracts.GradeC, contracts.GradeB, contracts.GradeA}
	for rank, i := range indexes {
		c := v.d.Classes[i]
		path := fmt.Sprintf("/classes/%d", i)
		if c.MinTotal < next {
			v.issue(path, "CLASS_OVERLAP", "Class overlaps the previous class.")
		} else if c.MinTotal > next {
			v.issue(path, "CLASS_GAP", "Class scale has an uncovered integer total.")
		}
		if rank < len(order) && c.Grade != order[rank] {
			v.issue(path+"/grade", "CLASS_ORDER", "Grades must ascend D, C, B, A with increasing totals.")
		}
		if c.MaxTotal >= 0 && c.MaxTotal <= TotalMaximum {
			next = c.MaxTotal + 1
		}
	}
	if next != TotalMaximum+1 {
		v.issue("/classes", "CLASS_GAP", "Class scale must end at 1000.")
	}
}

func (v *validator) reason(r Reason, path string) {
	if len(r.Code) > 128 || !reasonCodePattern.MatchString(r.Code) {
		v.issue(path+"/code", "INVALID_REASON_CODE", "Use an uppercase reason code with digits/underscores.")
	}
	if strings.TrimSpace(r.Message) == "" {
		v.issue(path+"/message", "REQUIRED", "Reason message is required.")
	}
	if r.Fields == nil {
		v.issue(path+"/fields", "REQUIRED_ARRAY", "Use an explicit fields array.")
	}
	seen := map[string]bool{}
	for i, f := range r.Fields {
		if strings.TrimSpace(f) == "" || seen[f] {
			v.issue(fmt.Sprintf("%s/fields/%d", path, i), "INVALID_REASON_FIELD", "Reason fields must be nonempty and unique.")
		}
		seen[f] = true
	}
}
