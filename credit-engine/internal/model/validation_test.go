package model

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func nlit(x float64) *Literal {
	n := NumberLiteral(strconv.FormatFloat(x, 'g', -1, 64))
	return &Literal{Number: &n}
}
func blit(x bool) *Literal   { return &Literal{Boolean: &x} }
func slit(x string) *Literal { return &Literal{String: &x} }
func compare(op Operator, id string, x float64) Condition {
	return Condition{Op: op, Parameter: id, Value: nlit(x)}
}
func allOf(conditions ...Condition) Condition { return Condition{Op: All, Conditions: conditions} }

// Synthetic validation fixture only: it is not an active scoring model.
func validDefinition(t *testing.T) ModelDefinition {
	t.Helper()
	d, err := Decode(bytes.NewReader(fragment(t, "supplier")))
	if err != nil {
		t.Fatal(err)
	}
	d.Parameters[0].Enabled = true
	d.Parameters[1].Enabled = true
	d.Criteria[0].Enabled = true
	d.Criteria[0].MaxPoints = 750
	d.Criteria[0].Cases[0].Points = 750
	d.Criteria[1].Enabled = true
	d.Criteria[1].MaxPoints = 250
	d.Criteria[1].Cases[0].Points = 250
	return d
}

func hasIssue(result ValidationResult, code string) bool {
	for _, issue := range result.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
func requireIssue(t *testing.T, result ValidationResult, code string) {
	t.Helper()
	if result.Valid || !hasIssue(result, code) {
		t.Fatalf("want %s, got %+v", code, result)
	}
	for _, issue := range result.Issues {
		if !strings.HasPrefix(issue.Path, "/") || issue.Message == "" {
			t.Fatalf("unaddressed issue: %+v", issue)
		}
	}
}

func TestValidDefinitionsAndBlockMaximumGates(t *testing.T) {
	for _, kind := range []contracts.BorrowerType{contracts.BorrowerTypeSoleProprietor, contracts.BorrowerTypeLegalEntity} {
		d := validDefinition(t)
		d.BorrowerType = kind
		result := Validate(d, nil)
		if !result.Valid || len(result.Issues) != 0 || result.QuestionnaireMaximum != 750 || result.AdditionalMaximum != 250 || result.TotalMaximum != 1000 {
			t.Fatalf("%s: %+v", kind, result)
		}
	}
	for _, tc := range []struct{ q, e int }{{750, 249}, {751, 250}, {760, 240}} {
		t.Run(fmt.Sprintf("%d_%d", tc.q, tc.e), func(t *testing.T) {
			d := validDefinition(t)
			d.Criteria[0].MaxPoints = tc.q
			d.Criteria[0].Cases[0].Points = float64(tc.q)
			d.Criteria[1].MaxPoints = tc.e
			d.Criteria[1].Cases[0].Points = float64(tc.e)
			r := Validate(d, nil)
			if r.Valid {
				t.Fatal("invalid block maxima published")
			}
			if tc.q != 750 && !hasIssue(r, "QUESTIONNAIRE_MAXIMUM") {
				t.Fatalf("questionnaire gate missing: %+v", r)
			}
			if tc.e != 250 && !hasIssue(r, "ADDITIONAL_MAXIMUM") {
				t.Fatalf("additional gate missing: %+v", r)
			}
		})
	}
}

func TestDefinitionMetadataNamesAndTypes(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*ModelDefinition)
	}{
		{"duplicate_parameter", "DUPLICATE_ID", func(d *ModelDefinition) { d.Parameters[1].ID = d.Parameters[0].ID }},
		{"duplicate_criterion", "DUPLICATE_ID", func(d *ModelDefinition) { d.Criteria[1].ID = d.Criteria[0].ID }},
		{"duplicate_case", "DUPLICATE_CASE_ID", func(d *ModelDefinition) { d.Criteria[0].Cases[1].ID = d.Criteria[0].Cases[0].ID }},
		{"duplicate_json", "DUPLICATE_JSON_NAME", func(d *ModelDefinition) {
			d.Parameters[1].JSONName = d.Parameters[0].JSONName
			d.Parameters[1].Source.Path = d.Parameters[0].Source.Path
		}},
		{"go_collision", "GO_NAME_COLLISION", func(d *ModelDefinition) { d.Parameters[0].GoName = "SameField"; d.Parameters[1].GoName = "SameField" }},
		{"go_keyword", "INVALID_GO_NAME", func(d *ModelDefinition) { d.Parameters[0].GoName = "for" }},
		{"go_unexported", "INVALID_GO_NAME", func(d *ModelDefinition) { d.Parameters[0].GoName = "hidden" }},
		{"go_reserved", "INVALID_GO_NAME", func(d *ModelDefinition) { d.Parameters[0].GoName = "MarshalJSON" }},
		{"json_shape", "INVALID_JSON_NAME", func(d *ModelDefinition) {
			d.Parameters[0].JSONName = "bad__field"
			d.Parameters[0].Source.Path = "bad__field"
		}},
		{"source_name_mismatch", "INVALID_JSON_NAME", func(d *ModelDefinition) { d.Parameters[0].Source.Path = "another_field" }},
		{"supplier_string", "SUPPLIER_TYPE", func(d *ModelDefinition) { d.Parameters[0].Type = String }},
		{"type_unit", "TYPE_UNIT_MISMATCH", func(d *ModelDefinition) { d.Parameters[0].Unit = Flag }},
		{"unknown_source", "UNKNOWN_SOURCE", func(d *ModelDefinition) {
			d.Parameters[0].Source = Source{Computed, "invented_formula"}
			d.Parameters[0].GoName = ""
		}},
		{"source_kind", "INVALID_SOURCE_KIND", func(d *ModelDefinition) { d.Parameters[0].Source.Kind = "MAGIC" }},
		{"schema_version", "REQUIRED_SCHEMA_VERSION", func(d *ModelDefinition) { d.FactsSchemaVersion = "" }},
		{"invalid_borrower", "INVALID_BORROWER_TYPE", func(d *ModelDefinition) { d.BorrowerType = "OTHER" }},
		{"empty_title", "REQUIRED", func(d *ModelDefinition) { d.Title = " " }},
		{"bad_parameter_id", "INVALID_ID", func(d *ModelDefinition) { d.Parameters[0].ID = "Unsafe ID" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { d := validDefinition(t); tc.change(&d); requireIssue(t, Validate(d, nil), tc.code) })
	}
	p := Parameter{JSONName: "client_id_and_value_rub"}
	if GeneratedGoName(p) != "ClientIDAndValueRUB" {
		t.Fatal("generator and validator naming contract differs")
	}
}

func TestNumericDomainsAndPointValues(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*ModelDefinition)
	}{
		{"missing_domain", "REQUIRED_NUMBER_DOMAIN", func(d *ModelDefinition) { d.Parameters[0].Number = nil }},
		{"inverted", "EMPTY_DOMAIN", func(d *ModelDefinition) {
			d.Parameters[0].Number.Lower = &Bound{NumberFromFloat(10), true}
			d.Parameters[0].Number.Upper = &Bound{NumberFromFloat(1), true}
		}},
		{"open_point", "EMPTY_DOMAIN", func(d *ModelDefinition) {
			d.Parameters[0].Number.Lower = &Bound{NumberFromFloat(0), false}
			d.Parameters[0].Number.Upper = &Bound{NumberFromFloat(0), true}
		}},
		{"integer_hole", "EMPTY_DOMAIN", func(d *ModelDefinition) {
			d.Parameters[0].Number = &NumberDomain{Integer: true, Lower: &Bound{NumberFromFloat(.1), true}, Upper: &Bound{NumberFromFloat(.9), true}}
		}},
		{"infinite_bound", "NONFINITE_BOUND", func(d *ModelDefinition) { d.Parameters[0].Number.Upper = &Bound{NumberFromFloat(math.Inf(1)), true} }},
		{"nan_bound", "NONFINITE_BOUND", func(d *ModelDefinition) { d.Parameters[0].Number.Lower = &Bound{NumberFromFloat(math.NaN()), true} }},
		{"boolean_domain", "UNEXPECTED_NUMBER_DOMAIN", func(d *ModelDefinition) { d.Parameters[1].Number = &NumberDomain{} }},
		{"unexpected_enum", "UNEXPECTED_ENUM", func(d *ModelDefinition) { d.Parameters[0].StringValues = []string{"x"} }},
		{"negative_points", "INVALID_POINTS", func(d *ModelDefinition) { d.Criteria[1].Cases[0].Points = -1 }},
		{"points_above_max", "INVALID_POINTS", func(d *ModelDefinition) { d.Criteria[1].Cases[0].Points = 251 }},
		{"nan_points", "INVALID_POINTS", func(d *ModelDefinition) { d.Criteria[1].Cases[0].Points = math.NaN() }},
		{"inf_points", "INVALID_POINTS", func(d *ModelDefinition) { d.Criteria[1].Cases[0].Points = math.Inf(1) }},
		{"fraction_questionnaire", "FRACTIONAL_QUESTIONNAIRE_POINTS", func(d *ModelDefinition) { d.Criteria[0].Cases[1].Points = .5 }},
		{"unreached_max", "MAXIMUM_NOT_REACHED", func(d *ModelDefinition) { d.Criteria[1].Cases[0].Points = 249 }},
		{"invalid_unknown_policy", "INVALID_UNKNOWN_ACTION", func(d *ModelDefinition) { d.Criteria[0].UnknownAction = "COERCE_FALSE" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { d := validDefinition(t); tc.change(&d); requireIssue(t, Validate(d, nil), tc.code) })
	}
	d := validDefinition(t)
	id := d.Parameters[0].ID
	d.Criteria[1].Cases = []ScoreCase{{ID: "below", When: compare(LT, id, 1), Points: 3.75}, {ID: "high", When: compare(GE, id, 1), Points: 250}}
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("fractional additional points rejected: %+v", r)
	}
}

func TestConditionReferencesShapesAndLiteralTypes(t *testing.T) {
	cases := []struct {
		name, code string
		condition  func(string) Condition
	}{
		{"unknown_parameter", "UNKNOWN_PARAMETER", func(string) Condition { return Condition{Op: Known, Parameter: "absent"} }},
		{"unknown_operator", "UNKNOWN_OPERATOR", func(string) Condition { return Condition{Op: "EXEC"} }},
		{"invalid_literal", "INVALID_LITERAL", func(id string) Condition {
			return Condition{Op: EQ, Parameter: id, Value: &Literal{Number: new(NumberLiteral), Boolean: new(bool)}}
		}},
		{"null_literal", "INVALID_LITERAL", func(id string) Condition { return Condition{Op: EQ, Parameter: id, Value: &Literal{}} }},
		{"wrong_literal_type", "CONDITION_TYPE_MISMATCH", func(id string) Condition { return Condition{Op: EQ, Parameter: id, Value: blit(false)} }},
		{"both_operands", "INVALID_CONDITION_SHAPE", func(id string) Condition { return Condition{Op: EQ, Parameter: id, OtherParameter: id, Value: nlit(0)} }},
		{"empty_group", "INVALID_CONDITION_SHAPE", func(string) Condition { return Condition{Op: All} }},
		{"double_not", "INVALID_CONDITION_SHAPE", func(string) Condition { return Condition{Op: Not, Conditions: []Condition{{Op: Always}, {Op: Never}}} }},
		{"unused_operand", "INVALID_CONDITION_SHAPE", func(id string) Condition { return Condition{Op: Always, Parameter: id} }},
		{"duplicate_membership", "DUPLICATE_LITERAL", func(id string) Condition {
			return Condition{Op: In, Parameter: id, Values: []Literal{*nlit(0), *nlit(math.Copysign(0, -1))}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDefinition(t)
			d.Criteria[1].Cases[0].When = tc.condition(d.Parameters[0].ID)
			requireIssue(t, Validate(d, nil), tc.code)
		})
	}
	d := validDefinition(t)
	d.Criteria[0].Cases[0].When = Condition{Op: LT, Parameter: d.Parameters[1].ID, Value: blit(true)}
	requireIssue(t, Validate(d, nil), "ORDER_REQUIRES_NUMBER")
	d = validDefinition(t)
	d.Criteria[1].Cases[0].When = Condition{Op: EQ, Parameter: d.Parameters[0].ID, OtherParameter: d.Parameters[1].ID}
	requireIssue(t, Validate(d, nil), "CONDITION_TYPE_MISMATCH")
}

func TestDisabledReferencesDependenciesAndCycles(t *testing.T) {
	for _, site := range []string{"value", "condition", "template", "data_check", "stop", "manual", "required_when"} {
		t.Run(site, func(t *testing.T) {
			d := validDefinition(t)
			id := d.Parameters[0].ID
			d.Parameters[0].Enabled = false
			d.Criteria[1].Enabled = false
			condition := Condition{Op: Known, Parameter: id}
			switch site {
			case "value":
				d.Criteria[0].ValueParameter = id
			case "condition":
				d.Criteria[0].AppliesWhen = &condition
			case "template":
				d.Criteria[0].Answer.Template = "${" + id + "}"
			case "data_check":
				d.DataChecks = []DataCheck{{ID: "check", Enabled: true, Title: "check", SatisfiedWhen: condition, Fields: []string{id}, Message: "Need source"}}
			case "stop":
				d.Stops = []Rule{{ID: "stop", Enabled: true, Title: "stop", When: condition, Reason: Reason{"STOP", "Stop", []string{}}}}
			case "manual":
				d.ManualReviews = []Rule{{ID: "manual", Enabled: true, Title: "manual", When: condition, Reason: Reason{"MANUAL", "Review", []string{}}}}
			case "required_when":
				d.Parameters[1].RequiredWhen = &condition
			}
			requireIssue(t, Validate(d, nil), "DISABLED_DEPENDENCY")
		})
	}
	d := validDefinition(t)
	id := d.Parameters[0].ID
	d.Parameters[0].RequiredWhen = &Condition{Op: Known, Parameter: id}
	requireIssue(t, Validate(d, nil), "DEPENDENCY_CYCLE")
	d = validDefinition(t)
	d.Parameters[0].DependsOn = []string{d.Parameters[1].ID}
	d.Parameters[1].DependsOn = []string{d.Parameters[0].ID}
	requireIssue(t, Validate(d, nil), "DEPENDENCY_CYCLE")
	d = validDefinition(t)
	d.Parameters[0].DependsOn = []string{"absent"}
	requireIssue(t, Validate(d, nil), "UNKNOWN_PARAMETER")
}

func TestClassesAndReasonsAreCompleteAndAddressed(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*ModelDefinition)
	}{
		{"class_gap", "CLASS_GAP", func(d *ModelDefinition) { d.Classes[1].MinTotal = 551 }},
		{"class_overlap", "CLASS_OVERLAP", func(d *ModelDefinition) { d.Classes[1].MinTotal = 549 }},
		{"class_upper", "CLASS_GAP", func(d *ModelDefinition) { d.Classes[3].MaxTotal = 999 }},
		{"class_lower", "CLASS_GAP", func(d *ModelDefinition) { d.Classes[0].MinTotal = 1 }},
		{"class_negative", "INVALID_CLASS_RANGE", func(d *ModelDefinition) { d.Classes[0].MinTotal = -1 }},
		{"missing_class", "CLASS_COUNT", func(d *ModelDefinition) { d.Classes = d.Classes[:3] }},
		{"wrong_grade", "INVALID_GRADE", func(d *ModelDefinition) { d.Classes[3].Grade = "E" }},
		{"duplicate_grade", "DUPLICATE_GRADE", func(d *ModelDefinition) { d.Classes[3].Grade = d.Classes[2].Grade }},
		{"reversed_grades", "CLASS_ORDER", func(d *ModelDefinition) {
			d.Classes[2].Grade, d.Classes[3].Grade = d.Classes[3].Grade, d.Classes[2].Grade
		}},
		{"missing_reason", "REQUIRED_REASON", func(d *ModelDefinition) { d.Classes[0].Reason = nil }},
		{"reason_code", "INVALID_REASON_CODE", func(d *ModelDefinition) { d.Classes[0].Reason.Code = "lower" }},
		{"reason_message", "REQUIRED", func(d *ModelDefinition) { d.Classes[0].Reason.Message = " " }},
		{"reason_fields", "REQUIRED_ARRAY", func(d *ModelDefinition) { d.Classes[0].Reason.Fields = nil }},
		{"reason_duplicate_fields", "INVALID_REASON_FIELD", func(d *ModelDefinition) { d.Classes[0].Reason.Fields = []string{"same", "same"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { d := validDefinition(t); tc.change(&d); requireIssue(t, Validate(d, nil), tc.code) })
	}
}

func TestValidationDoesNotMutateAndIsConcurrentDeterministic(t *testing.T) {
	d := validDefinition(t)
	d.Criteria[1].Cases = []ScoreCase{{ID: "gap", When: compare(GT, d.Parameters[0].ID, 0), Points: 250}}
	before, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	expected := Validate(d, nil)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := Validate(d, nil)
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("nondeterministic validation: %+v vs %+v", got, expected)
			}
		}()
	}
	wg.Wait()
	after, err := Marshal(d)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("validator mutated input")
	}
}
