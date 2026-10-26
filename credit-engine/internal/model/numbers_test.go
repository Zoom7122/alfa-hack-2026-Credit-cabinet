package model

import (
	"bytes"
	"math/big"
	"strings"
	"testing"
)

func exactLiteral(text string) *Literal { n := NumberLiteral(text); return &Literal{Number: &n} }

func TestNumericConfigurationTokensAreLossless(t *testing.T) {
	d := validDefinition(t)
	d.Parameters[0].Number.Integer = true
	d.Parameters[0].Number.Lower = &Bound{NumberLiteral("9007199254740992"), true}
	d.Parameters[0].Number.Upper = &Bound{NumberLiteral("9007199254740996"), true}
	d.Criteria[1].Cases[0].When = Condition{Op: GE, Parameter: d.Parameters[0].ID, Value: exactLiteral("9007199254740993")}
	encoded, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"number": 9007199254740993`)) {
		t.Fatal("integer literal rounded or encoded as a string")
	}
	decoded, err := Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Criteria[1].Cases[0].When.Value.Number.String() != "9007199254740993" {
		t.Fatal("exact token lost")
	}
	quoted := strings.Replace(string(encoded), `"number": 9007199254740993`, `"number": "9007199254740993"`, 1)
	if _, err := Decode(strings.NewReader(quoted)); err == nil {
		t.Fatal("numeric string silently coerced")
	}
}

func TestNativeInt64CoverageIncludesValuesBetweenAdjacentFloats(t *testing.T) {
	d := validDefinition(t)
	p := &d.Parameters[0]
	p.Number = &NumberDomain{Integer: true, Lower: &Bound{NumberLiteral("9007199254740992"), true}, Upper: &Bound{NumberLiteral("9007199254740996"), true}}
	id := p.ID
	d.Criteria[1].Cases = []ScoreCase{{ID: "lower", When: Condition{Op: LE, Parameter: id, Value: exactLiteral("9007199254740992")}, Points: 250}, {ID: "upper", When: Condition{Op: GE, Parameter: id, Value: exactLiteral("9007199254740994")}, Points: 0}}
	requireIssue(t, Validate(d, nil), "CASE_GAP")
	d.Criteria[1].Cases[1].When.Value = exactLiteral("9007199254740993")
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("native adjacent integers rejected: %+v", r)
	}
	p.Number.Lower = &Bound{NumberLiteral("9223372036854775808"), true}
	p.Number.Upper = nil
	requireIssue(t, Validate(d, nil), "EMPTY_DOMAIN")
}

func TestRationalControlsAndBinaryScoringRemainDistinct(t *testing.T) {
	d := validDefinition(t)
	p := &d.Parameters[0]
	p.Source = Source{Computed, "existing_ratio"}
	p.JSONName = "existing_ratio"
	p.GoName = ""
	p.Unit = Ratio
	p.Number = &NumberDomain{Lower: &Bound{NumberLiteral("0"), true}, Upper: &Bound{NumberLiteral("1"), true}}
	d.Criteria[1].Cases = []ScoreCase{{ID: "lower", When: Condition{Op: LE, Parameter: p.ID, Value: exactLiteral("0.1")}, Points: 250}, {ID: "upper", When: Condition{Op: GE, Parameter: p.ID, Value: exactLiteral("0.10000000000000002")}, Points: 0}}
	catalog := []Binding{{Source: p.Source, Type: Number, Unit: Ratio, NumericKind: NumericFloat64}}
	if r := Validate(d, catalog); !r.Valid {
		t.Fatalf("old binary scoring boundary changed: %+v", r)
	}
	catalog[0].NumericKind = NumericRational
	requireIssue(t, Validate(d, catalog), "CASE_GAP")
	left := proofValue{known: true, typ: Number, exact: big.NewRat(1, 10), number: .1}
	condition := Condition{Op: EQ, Parameter: "ratio", Value: exactLiteral("0.1")}
	if proofEval(condition, map[string]proofValue{"ratio": left}) != trueTruth {
		t.Fatal("exact decimal threshold changed to binary rounding")
	}
}

func TestNumericTokenResourceAndGrammarLimits(t *testing.T) {
	for _, text := range []string{"NaN", "Infinity", "01", "1/3", "1e-999999999", "1e999999999", strings.Repeat("1", MaxNumberTokenBytes+1)} {
		if validNumber(NumberLiteral(text)) {
			t.Fatalf("invalid/unbounded number accepted: %s", text)
		}
	}
	for _, text := range []string{"0", "-0", "0.1", "9223372036854775807", "1e-512"} {
		if !validNumber(NumberLiteral(text)) {
			t.Fatalf("valid numeric token rejected: %s", text)
		}
	}
}

func TestRuleDataDependenciesAndReasonOrdering(t *testing.T) {
	d := validDefinition(t)
	id := d.Parameters[1].ID
	d.DataChecks = []DataCheck{{ID: "finance_verified", Enabled: true, Title: "Finance", SatisfiedWhen: Condition{Op: EQ, Parameter: id, Value: blit(true)}, Fields: []string{id}, Message: "Need finance"}}
	d.Stops = []Rule{{ID: "cash_stop", Enabled: true, Title: "Cash", When: Condition{Op: Always}, Reason: Reason{Code: "STOP", Message: "Stop", Fields: []string{}}, RequiresChecks: []string{"finance_verified"}}}
	d.ManualReviews = []Rule{{ID: "after_economic", Enabled: true, Title: "Review", When: Condition{Op: Always}, Reason: Reason{Code: "REVIEW", Message: "Review", Fields: []string{}}, AfterCriterion: d.Criteria[1].ID}}
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("valid ordering/gate dependencies rejected: %+v", r)
	}
	d.DataChecks[0].Enabled = false
	requireIssue(t, Validate(d, nil), "DISABLED_CHECK_DEPENDENCY")
	d.DataChecks[0].Enabled = true
	d.Stops[0].RequiresChecks = []string{"missing"}
	requireIssue(t, Validate(d, nil), "UNKNOWN_DATA_CHECK")
	d.Stops[0].RequiresChecks = nil
	d.Stops[0].AfterCriterion = d.Criteria[0].ID
	requireIssue(t, Validate(d, nil), "INVALID_REASON_ORDER")
	d.Stops[0].AfterCriterion = ""
	d.ManualReviews[0].AfterCriterion = "missing"
	requireIssue(t, Validate(d, nil), "UNKNOWN_CRITERION")
}
