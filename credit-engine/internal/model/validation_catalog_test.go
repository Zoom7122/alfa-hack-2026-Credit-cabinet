package model

import (
	"bytes"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func fragmentCatalog(d ModelDefinition) []Binding {
	parameters := map[string]Parameter{}
	for _, p := range d.Parameters {
		parameters[p.ID] = p
	}
	catalog := []Binding{}
	for _, p := range d.Parameters {
		if p.Source.Kind == Supplier {
			continue
		}
		deps := []Source{}
		for _, id := range p.DependsOn {
			deps = append(deps, parameters[id].Source)
		}
		catalog = append(catalog, Binding{Source: p.Source, Type: p.Type, Unit: p.Unit, DependsOn: deps})
	}
	return catalog
}

func TestFormatFragmentsAreSemanticallyExpressibleButNotReleases(t *testing.T) {
	for _, name := range []string{"ip", "legal", "supplier"} {
		t.Run(name, func(t *testing.T) {
			d, err := Decode(bytes.NewReader(fragment(t, name)))
			if err != nil {
				t.Fatal(err)
			}
			r := Validate(d, fragmentCatalog(d))
			if r.Valid {
				t.Fatal("partial format example was accepted as a release")
			}
			for _, issue := range r.Issues {
				if issue.Code != "QUESTIONNAIRE_MAXIMUM" && issue.Code != "ADDITIONAL_MAXIMUM" && issue.Code != "TOTAL_MAXIMUM" {
					t.Fatalf("existing v2 fragment cannot be expressed: %+v", r)
				}
			}
		})
	}
}

func computedDefinition(t *testing.T) (ModelDefinition, []Binding) {
	t.Helper()
	d := validDefinition(t)
	raw := Parameter{ID: "raw_value", Enabled: true, Title: "raw", Source: Source{Application, "raw_value"}, JSONName: "raw_value", Type: Number, Unit: RUB, Number: &NumberDomain{Lower: &Bound{NumberFromFloat(0), true}}, Required: true}
	d.Parameters = append(d.Parameters, raw)
	p := &d.Parameters[0]
	p.Source = Source{Computed, "existing_metric"}
	p.JSONName = "existing_metric"
	p.GoName = ""
	p.DependsOn = []string{raw.ID}
	catalog := []Binding{{Source: p.Source, Type: Number, Unit: RUB, DependsOn: []Source{raw.Source}}, {Source: raw.Source, Type: Number, Unit: RUB}}
	return d, catalog
}

func TestTrustedBindingsRequiredDependenciesAndSourceScopes(t *testing.T) {
	d, catalog := computedDefinition(t)
	if r := Validate(d, catalog); !r.Valid {
		t.Fatalf("valid trusted calculation rejected: %+v", r)
	}
	t.Run("missing_declared_dependency", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		d.Parameters[0].DependsOn = nil
		requireIssue(t, Validate(d, catalog), "MISSING_CALCULATION_DEPENDENCY")
	})
	t.Run("missing_source_parameter", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		d.Parameters = d.Parameters[:2]
		requireIssue(t, Validate(d, catalog), "MISSING_SOURCE_DEPENDENCY")
	})
	t.Run("disabled_dependency", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		d.Parameters[2].Enabled = false
		requireIssue(t, Validate(d, catalog), "DISABLED_DEPENDENCY")
	})
	t.Run("wrong_catalog_type", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		catalog[1].Type = Boolean
		catalog[1].Unit = Flag
		requireIssue(t, Validate(d, catalog), "SOURCE_TYPE_MISMATCH")
	})
	t.Run("duplicate_catalog", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		catalog = append(catalog, catalog[0])
		requireIssue(t, Validate(d, catalog), "DUPLICATE_SOURCE_BINDING")
	})
	t.Run("unknown_catalog_dependency", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		catalog[0].DependsOn = append(catalog[0].DependsOn, Source{Computed, "absent"})
		requireIssue(t, Validate(d, catalog), "UNKNOWN_CATALOG_DEPENDENCY")
	})
	t.Run("json_name_override", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		d.Parameters[2].JSONName = "renamed_raw_value"
		requireIssue(t, Validate(d, catalog), "JSON_NAME_MISMATCH")
	})
	t.Run("catalog_cycle", func(t *testing.T) {
		d, catalog := computedDefinition(t)
		d.Parameters[2].Source.Kind = Computed
		d.Parameters[2].DependsOn = []string{d.Parameters[0].ID}
		catalog[1].Source = d.Parameters[2].Source
		catalog[1].DependsOn = []Source{d.Parameters[0].Source}
		catalog[0].DependsOn = []Source{d.Parameters[2].Source}
		requireIssue(t, Validate(d, catalog), "DEPENDENCY_CYCLE")
	})
	t.Run("same_json_leaf_in_distinct_objects", func(t *testing.T) {
		d := validDefinition(t)
		d.BorrowerType = contracts.BorrowerTypeLegalEntity
		catalog := []Binding{}
		for _, period := range []string{"current", "previous"} {
			p := Parameter{ID: period + "_revenue", Enabled: true, Title: period, Source: Source{Statements, period + ".profit_and_loss.revenue"}, JSONName: "revenue", Type: Number, Unit: RUB, Number: &NumberDomain{Integer: true}}
			d.Parameters = append(d.Parameters, p)
			catalog = append(catalog, Binding{Source: p.Source, Type: Number, Unit: RUB})
		}
		if r := Validate(d, catalog); !r.Valid {
			t.Fatalf("separate named forms share no JSON scope: %+v", r)
		}
	})
}

func TestSourceBorrowerTypeCannotBeSubstituted(t *testing.T) {
	for _, source := range []Source{{Application, "scoring.ip.owner_age"}, {Computed, "IP_E07"}, {Statements, "current.profit_and_loss.revenue"}, {Application, "scoring.legal_entity.business_age_months"}} {
		t.Run(string(source.Kind)+"_"+source.Path, func(t *testing.T) {
			d := validDefinition(t)
			p := &d.Parameters[0]
			p.Source = source
			p.JSONName = source.Path
			for i := len(p.JSONName) - 1; i >= 0; i-- {
				if p.JSONName[i] == '.' {
					p.JSONName = p.JSONName[i+1:]
					break
				}
			}
			p.GoName = ""
			expected := sourceBorrowerType(source)
			d.BorrowerType = contracts.BorrowerTypeSoleProprietor
			if expected == d.BorrowerType {
				d.BorrowerType = contracts.BorrowerTypeLegalEntity
			}
			catalog := []Binding{{Source: source, Type: Number, Unit: RUB}}
			requireIssue(t, Validate(d, catalog), "SOURCE_BORROWER_MISMATCH")
		})
	}
}

func TestStringDomainsDataChecksAndResultOverrides(t *testing.T) {
	t.Run("enum_scale", func(t *testing.T) {
		d := validDefinition(t)
		p := &d.Parameters[1]
		p.Type = String
		p.Unit = Category
		p.Source = Source{Application, "state"}
		p.JSONName = "state"
		p.GoName = ""
		p.StringValues = []string{"X", "Y", "Z"}
		d.Criteria[0].Cases = []ScoreCase{{ID: "xy", When: Condition{Op: In, Parameter: p.ID, Values: []Literal{*slit("X"), *slit("Y")}}, Points: 750}, {ID: "z", When: Condition{Op: EQ, Parameter: p.ID, Value: slit("Z")}, Points: 0}}
		catalog := []Binding{{Source: p.Source, Type: p.Type, Unit: p.Unit}}
		if r := Validate(d, catalog); !r.Valid {
			t.Fatalf("enum scale rejected: %+v", r)
		}
		p.StringValues = append(p.StringValues, "X")
		requireIssue(t, Validate(d, catalog), "INVALID_ENUM_VALUE")
	})
	t.Run("business_data_check", func(t *testing.T) {
		d := validDefinition(t)
		id := d.Parameters[1].ID
		d.DataChecks = []DataCheck{{ID: "verified", Enabled: true, Title: "Verified", SatisfiedWhen: Condition{Op: EQ, Parameter: id, Value: blit(true)}, Fields: []string{"additional_facts." + id}, Message: "Verification required"}}
		if r := Validate(d, nil); !r.Valid {
			t.Fatalf("business data check rejected: %+v", r)
		}
		d.DataChecks[0].Fields = nil
		requireIssue(t, Validate(d, nil), "REQUIRED_DATA_FIELDS")
	})
	t.Run("special_null_and_manual", func(t *testing.T) {
		d := validDefinition(t)
		id := d.Parameters[0].ID
		d.Criteria[1].SpecialCases = []ScoreCase{{ID: "zero", When: compare(EQ, id, 0), Points: 250, Result: &ResultOverride{Mode: NullResult}, ReviewReason: &Reason{Code: "SPECIAL", Message: "Review", Fields: []string{id}}}}
		if r := Validate(d, nil); !r.Valid {
			t.Fatalf("special result rejected: %+v", r)
		}
		d.Criteria[1].SpecialCases[0].Result = &ResultOverride{Mode: LiteralResult, Value: blit(false)}
		requireIssue(t, Validate(d, nil), "RESULT_TYPE_MISMATCH")
	})
	t.Run("date_literal", func(t *testing.T) {
		d := validDefinition(t)
		p := &d.Parameters[1]
		p.Type = String
		p.Unit = Date
		p.Source = Source{Application, "facts_date"}
		p.JSONName = "facts_date"
		p.GoName = ""
		d.Criteria[0].Cases = []ScoreCase{{ID: "all", When: Condition{Op: Always}, Points: 750}}
		d.Stops = []Rule{{ID: "date_stop", Enabled: true, Title: "date", When: Condition{Op: EQ, Parameter: p.ID, Value: slit("2026-02-30")}, Reason: Reason{Code: "STOP", Message: "Stop", Fields: []string{}}}}
		requireIssue(t, Validate(d, []Binding{{Source: p.Source, Type: String, Unit: Date}}), "INVALID_DATE_LITERAL")
	})
}
