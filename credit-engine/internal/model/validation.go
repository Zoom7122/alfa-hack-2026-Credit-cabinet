package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

// Validate checks a definition for release. It never mutates, normalizes or
// publishes the definition and does not contact any runtime dependency.
func Validate(d ModelDefinition, catalog []Binding) ValidationResult {
	v := validator{d: d, result: ValidationResult{Issues: []Issue{}}, parameters: map[string]Parameter{}, parameterPaths: map[string]string{}, sources: map[Source]string{}, bindings: map[Source]Binding{}, dependencies: map[string]map[string]bool{}}
	if _, err := Marshal(d); err != nil {
		var format *FormatError
		if errors.As(err, &format) {
			v.issue(format.Path, format.Code, format.Message)
		} else {
			v.issue("/", "INVALID_DOCUMENT", err.Error())
		}
	}
	v.metadata()
	for i, b := range catalog {
		path := fmt.Sprintf("/source_catalog/%d", i)
		if b.Source.Kind != Application && b.Source.Kind != Statements && b.Source.Kind != Computed || strings.TrimSpace(b.Source.Path) == "" || !validTypeUnit(b.Type, b.Unit) {
			v.issue(path, "INVALID_SOURCE_BINDING", "Trusted binding needs a supported source, type and unit.")
		}
		expected := sourceBorrowerType(b.Source)
		if b.BorrowerType != "" && b.BorrowerType != contracts.BorrowerTypeSoleProprietor && b.BorrowerType != contracts.BorrowerTypeLegalEntity || expected != "" && b.BorrowerType != "" && b.BorrowerType != expected {
			v.issue(path, "INVALID_SOURCE_BINDING", "Source binding has an incompatible borrower type.")
		}
		if b.BorrowerType == "" {
			b.BorrowerType = expected
		}
		if b.NumericKind != "" && (b.Type != Number || b.NumericKind != NumericFloat64 && b.NumericKind != NumericInt64 && b.NumericKind != NumericInteger && b.NumericKind != NumericRational) {
			v.issue(path, "INVALID_NUMERIC_KIND", "Numeric kind must describe a supported numeric source representation.")
		}
		if _, ok := v.bindings[b.Source]; ok {
			v.issue(path, "DUPLICATE_SOURCE_BINDING", "Trusted source is listed more than once.")
		} else {
			v.bindings[b.Source] = b
		}
	}
	for i, b := range catalog {
		seen := map[Source]bool{}
		for j, dep := range b.DependsOn {
			path := fmt.Sprintf("/source_catalog/%d/depends_on/%d", i, j)
			if _, ok := v.bindings[dep]; !ok {
				v.issue(path, "UNKNOWN_CATALOG_DEPENDENCY", "Trusted dependency has no source binding.")
			}
			if seen[dep] {
				v.issue(path, "DUPLICATE_CATALOG_DEPENDENCY", "Trusted dependency appears more than once.")
			}
			seen[dep] = true
		}
	}
	v.parameterMetadata()
	v.parameterDependencies()
	v.rules()
	v.criteria()
	v.classes()
	if v.result.QuestionnaireMaximum != QuestionnaireMaximum {
		v.issue("/criteria", "QUESTIONNAIRE_MAXIMUM", fmt.Sprintf("Questionnaire maximum is %d; exactly 750 is required.", v.result.QuestionnaireMaximum))
	}
	if v.result.AdditionalMaximum != AdditionalMaximum {
		v.issue("/criteria", "ADDITIONAL_MAXIMUM", fmt.Sprintf("Additional maximum is %d; exactly 250 is required.", v.result.AdditionalMaximum))
	}
	v.result.TotalMaximum = v.result.QuestionnaireMaximum + v.result.AdditionalMaximum
	if v.result.TotalMaximum != TotalMaximum {
		v.issue("/criteria", "TOTAL_MAXIMUM", fmt.Sprintf("Total maximum is %d; exactly 1000 is required.", v.result.TotalMaximum))
	}
	sort.SliceStable(v.result.Issues, func(i, j int) bool {
		a, b := v.result.Issues[i], v.result.Issues[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	v.result.Valid = len(v.result.Issues) == 0
	return v.result
}

func sourceBorrowerType(source Source) contracts.BorrowerType {
	if source.Kind == Statements || source.Kind == Application && strings.HasPrefix(source.Path, "scoring.legal_entity.") || source.Kind == Computed && strings.HasPrefix(source.Path, "LE_E") {
		return contracts.BorrowerTypeLegalEntity
	}
	if source.Kind == Application && (strings.HasPrefix(source.Path, "scoring.ip.") || strings.HasPrefix(source.Path, "ip_economic_data.")) || source.Kind == Computed && strings.HasPrefix(source.Path, "IP_E") {
		return contracts.BorrowerTypeSoleProprietor
	}
	return ""
}

func (v *validator) rules() {
	ids := map[string]string{}
	checks := map[string]DataCheck{}
	for _, check := range v.d.DataChecks {
		checks[check.ID] = check
	}
	criteria := map[string]Criterion{}
	for _, criterion := range v.d.Criteria {
		criteria[criterion.ID] = criterion
	}
	idCheck := func(id, title, path string) {
		if len(id) > 128 || !objectIDPattern.MatchString(id) {
			v.issue(path+"/id", "INVALID_ID", "Rule ID must be a stable ASCII identifier.")
		}
		if old, ok := ids[id]; ok {
			v.issue(path+"/id", "DUPLICATE_ID", "Rule ID already belongs to "+old+".")
		} else {
			ids[id] = path
		}
		if strings.TrimSpace(title) == "" {
			v.issue(path+"/title", "REQUIRED", "Rule title is required.")
		}
	}
	for _, group := range []struct {
		name  string
		rules []Rule
	}{{"stops", v.d.Stops}, {"manual_reviews", v.d.ManualReviews}} {
		for i, r := range group.rules {
			path := fmt.Sprintf("/%s/%d", group.name, i)
			idCheck(r.ID, r.Title, path)
			v.condition(r.When, path+"/when", r.Enabled, "", 0)
			v.reason(r.Reason, path+"/reason")
			seen := map[string]bool{}
			for j, id := range r.RequiresChecks {
				depPath := fmt.Sprintf("%s/requires_checks/%d", path, j)
				if seen[id] {
					v.issue(depPath, "DUPLICATE_CHECK_DEPENDENCY", "A check dependency appears more than once.")
				}
				seen[id] = true
				check, ok := checks[id]
				if !ok {
					v.issue(depPath, "UNKNOWN_DATA_CHECK", "Required data check does not exist.")
				} else if r.Enabled && !check.Enabled {
					v.issue(depPath, "DISABLED_CHECK_DEPENDENCY", "Enabled rule requires a disabled data check.")
				}
			}
			if r.AfterCriterion != "" {
				if group.name != "manual_reviews" {
					v.issue(path+"/after_criterion", "INVALID_REASON_ORDER", "Only manual review reasons may be ordered after a criterion.")
				}
				_, ok := criteria[r.AfterCriterion]
				if !ok {
					v.issue(path+"/after_criterion", "UNKNOWN_CRITERION", "Reason ordering target does not exist.")
				}
			}
		}
	}
	for i, check := range v.d.DataChecks {
		path := fmt.Sprintf("/data_checks/%d", i)
		idCheck(check.ID, check.Title, path)
		if check.AppliesWhen != nil {
			v.condition(*check.AppliesWhen, path+"/applies_when", check.Enabled, "", 0)
		}
		v.condition(check.SatisfiedWhen, path+"/satisfied_when", check.Enabled, "", 0)
		if strings.TrimSpace(check.Message) == "" {
			v.issue(path+"/message", "REQUIRED", "Data-check explanation is required.")
		}
		if len(check.Fields) == 0 {
			v.issue(path+"/fields", "REQUIRED_DATA_FIELDS", "Data check must identify the missing source fields.")
		}
		seen := map[string]bool{}
		for j, field := range check.Fields {
			if strings.TrimSpace(field) == "" || seen[field] {
				v.issue(fmt.Sprintf("%s/fields/%d", path, j), "INVALID_DATA_FIELD", "Fields must be nonempty and unique.")
			}
			seen[field] = true
		}
	}
}

func (v *validator) criteria() {
	ids := map[string]string{}
	parametersOK := len(v.result.Issues) == 0
	for i, c := range v.d.Criteria {
		path := fmt.Sprintf("/criteria/%d", i)
		before := len(v.result.Issues)
		if len(c.ID) > 128 || !objectIDPattern.MatchString(c.ID) {
			v.issue(path+"/id", "INVALID_ID", "Criterion ID must be a stable ASCII identifier.")
		}
		if old, ok := ids[c.ID]; ok {
			v.issue(path+"/id", "DUPLICATE_ID", "Criterion ID already belongs to "+old+".")
		} else {
			ids[c.ID] = path
		}
		if strings.TrimSpace(c.Title) == "" {
			v.issue(path+"/title", "REQUIRED", "Criterion title is required.")
		}
		limit := 0
		switch c.Block {
		case Questionnaire:
			limit = QuestionnaireMaximum
		case Additional:
			limit = AdditionalMaximum
		default:
			v.issue(path+"/block", "INVALID_BLOCK", "Expected QUESTIONNAIRE or ADDITIONAL.")
		}
		if c.MaxPoints < 0 || c.MaxPoints > limit {
			v.issue(path+"/max_points", "INVALID_MAXIMUM", "Criterion maximum must fit its scoring block.")
		}
		if c.Enabled && c.MaxPoints >= 0 && c.MaxPoints <= TotalMaximum {
			if c.Block == Questionnaire {
				v.result.QuestionnaireMaximum += c.MaxPoints
			}
			if c.Block == Additional {
				v.result.AdditionalMaximum += c.MaxPoints
			}
		}
		if c.UnknownAction != NeedInfo && c.UnknownAction != Zero && c.UnknownAction != Skip {
			v.issue(path+"/unknown_action", "INVALID_UNKNOWN_ACTION", "Declare NEED_INFO, ZERO or SKIP explicitly.")
		}
		var primary Parameter
		primaryOK := false
		if c.ValueParameter != "" {
			primary, primaryOK = v.reference(c.ValueParameter, path+"/value_parameter", c.Enabled, "")
		}
		if c.Block == Additional && (c.ValueParameter == "" || primaryOK && primary.Type != Number && primary.Type != Boolean) {
			v.issue(path+"/value_parameter", "ADDITIONAL_VALUE_TYPE", "Additional criteria need a numeric or boolean value parameter.")
		}
		if c.Block == Questionnaire {
			if c.Answer == nil {
				v.issue(path+"/answer", "REQUIRED_ANSWER", "Questionnaire criteria need an explicit answer template.")
			} else {
				v.template(c.Answer.Template, path+"/answer/template", c.Enabled)
				for j, a := range c.Answer.Cases {
					apath := fmt.Sprintf("%s/answer/cases/%d", path, j)
					v.condition(a.When, apath+"/when", c.Enabled, "", 0)
					v.template(a.Template, apath+"/template", c.Enabled)
				}
			}
		} else if c.Answer != nil {
			v.issue(path+"/answer", "UNEXPECTED_ANSWER", "Only questionnaire results have text answers.")
		}
		if c.AppliesWhen != nil {
			v.condition(*c.AppliesWhen, path+"/applies_when", c.Enabled, "", 0)
		}
		if c.Cases == nil || c.SpecialCases == nil {
			v.issue(path, "REQUIRED_CASE_ARRAYS", "Both cases and special_cases must be explicit arrays.")
		}
		if len(c.Cases) == 0 {
			v.issue(path+"/cases", "EMPTY_SCALE", "An ordinary scoring scale is required.")
		}
		caseIDs := map[string]string{}
		for _, group := range []struct {
			name  string
			cases []ScoreCase
		}{{"special_cases", c.SpecialCases}, {"cases", c.Cases}} {
			for j, s := range group.cases {
				spath := fmt.Sprintf("%s/%s/%d", path, group.name, j)
				if len(s.ID) > 128 || !objectIDPattern.MatchString(s.ID) {
					v.issue(spath+"/id", "INVALID_ID", "Case ID must be a stable ASCII identifier.")
				}
				if old, ok := caseIDs[s.ID]; ok {
					v.issue(spath+"/id", "DUPLICATE_CASE_ID", "Case ID already belongs to "+old+".")
				} else {
					caseIDs[s.ID] = spath
				}
				v.condition(s.When, spath+"/when", c.Enabled, "", 0)
				if !finite(s.Points) || s.Points < 0 || s.Points > float64(c.MaxPoints) {
					v.issue(spath+"/points", "INVALID_POINTS", "Points must be finite and between zero and the criterion maximum.")
				}
				if c.Block == Questionnaire && finite(s.Points) && s.Points != math.Trunc(s.Points) {
					v.issue(spath+"/points", "FRACTIONAL_QUESTIONNAIRE_POINTS", "Questionnaire points must remain integers.")
				}
				if s.ReviewReason != nil {
					v.reason(*s.ReviewReason, spath+"/review_reason")
				}
				if s.Result != nil {
					if c.Block != Additional {
						v.issue(spath+"/result", "UNEXPECTED_RESULT_OVERRIDE", "Only additional result values can be overridden.")
					}
					switch s.Result.Mode {
					case NullResult:
						if s.Result.Value != nil {
							v.issue(spath+"/result/value", "UNEXPECTED_LITERAL", "NULL result must not carry a literal.")
						}
					case LiteralResult:
						t, ok := literalType(s.Result.Value)
						if !ok {
							v.issue(spath+"/result/value", "INVALID_LITERAL", "VALUE result needs exactly one finite typed literal.")
						} else if primaryOK && t != primary.Type {
							v.issue(spath+"/result/value", "RESULT_TYPE_MISMATCH", "Result override must preserve the criterion value type.")
						}
					default:
						v.issue(spath+"/result/mode", "INVALID_RESULT_MODE", "Expected NULL or VALUE.")
					}
				}
			}
		}
		if parametersOK && len(v.result.Issues) == before {
			v.coverage(c, path)
		}
	}
}
