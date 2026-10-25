package model

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

func numericScale(t *testing.T, domain NumberDomain, conditions ...Condition) ModelDefinition {
	t.Helper()
	d := validDefinition(t)
	d.Parameters[0].Number = &domain
	d.Criteria[0].ValueParameter = d.Parameters[0].ID
	d.Criteria[0].Answer = &Answer{Template: "value=${" + d.Parameters[0].ID + "}"}
	d.Criteria[0].Cases = []ScoreCase{}
	for i, c := range conditions {
		points := 0.0
		if i == 0 {
			points = 750
		}
		d.Criteria[0].Cases = append(d.Criteria[0].Cases, ScoreCase{ID: fmt.Sprintf("case_%d", i), When: c, Points: points})
	}
	d.Criteria[1].Cases = []ScoreCase{{ID: "all", When: Condition{Op: Always}, Points: 250}}
	return d
}

func TestExactNumericScaleBoundariesAndSpecialCases(t *testing.T) {
	id := "annual_contract_value_rub"
	continuous := NumberDomain{Lower: &Bound{NumberFromFloat(0), true}, Upper: &Bound{NumberFromFloat(2), true}}
	integer := continuous
	integer.Integer = true
	next := math.Nextafter(1, math.Inf(1))
	nextTwice := math.Nextafter(next, math.Inf(1))
	cases := []struct {
		name, code string
		domain     NumberDomain
		when       []Condition
	}{
		{"closed_open", "", continuous, []Condition{compare(LE, id, 1), compare(GT, id, 1)}},
		{"one_point_gap", "CASE_GAP", continuous, []Condition{compare(LT, id, 1), compare(GT, id, 1)}},
		{"one_point_overlap", "CASE_OVERLAP", continuous, []Condition{compare(LE, id, 1), compare(GE, id, 1)}},
		{"integer_adjacent", "", integer, []Condition{compare(LE, id, 0), compare(GE, id, 1)}},
		{"continuous_between_integers", "CASE_GAP", continuous, []Condition{compare(LE, id, 0), compare(GE, id, 1)}},
		{"one_float_gap", "CASE_GAP", continuous, []Condition{compare(LE, id, 1), compare(GE, id, nextTwice)}},
		{"adjacent_float_values", "", continuous, []Condition{compare(LE, id, 1), compare(GE, id, next)}},
		{"negative_unbounded", "", NumberDomain{}, []Condition{compare(LE, id, 0), compare(GT, id, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Validate(numericScale(t, tc.domain, tc.when...), nil)
			if tc.code != "" {
				requireIssue(t, r, tc.code)
			} else if !r.Valid {
				t.Fatalf("exact partition rejected: %+v", r)
			}
		})
	}
	d := numericScale(t, continuous, compare(LT, id, 1), compare(GT, id, 1))
	d.Criteria[0].SpecialCases = []ScoreCase{{ID: "special_one", When: compare(EQ, id, 1), Points: 0}}
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("special case did not close ordinary gap: %+v", r)
	}
	d.Criteria[0].SpecialCases = append(d.Criteria[0].SpecialCases, ScoreCase{ID: "duplicate_special", When: compare(EQ, id, 1), Points: 0})
	requireIssue(t, Validate(d, nil), "CASE_OVERLAP")
	d = numericScale(t, continuous, compare(LE, id, 1), compare(GT, id, 1))
	d.Criteria[0].AppliesWhen = &Condition{Op: Never}
	requireIssue(t, Validate(d, nil), "UNREACHABLE_CRITERION")
}

func binaryScale(t *testing.T, integer bool, conditions ...Condition) ModelDefinition {
	t.Helper()
	domain := NumberDomain{Integer: integer, Lower: &Bound{NumberFromFloat(-2), true}, Upper: &Bound{NumberFromFloat(2), true}}
	d := numericScale(t, domain, conditions...)
	d.Parameters[1].Type = Number
	d.Parameters[1].Unit = RUB
	otherDomain := domain
	d.Parameters[1].Number = &otherDomain
	return d
}

func TestParameterComparisonsAndMixedDomains(t *testing.T) {
	x, y := "annual_contract_value_rub", "has_confirmed_contracts"
	other := func(op Operator) Condition { return Condition{Op: op, Parameter: x, OtherParameter: y} }
	for _, integer := range []bool{false, true} {
		d := binaryScale(t, integer, other(LT), other(EQ), other(GT))
		if r := Validate(d, nil); !r.Valid {
			t.Fatalf("complete relative-order scale rejected: %+v", r)
		}
		d = binaryScale(t, integer, other(LT), other(GT))
		requireIssue(t, Validate(d, nil), "CASE_GAP")
		d = binaryScale(t, integer, other(LE), other(GE))
		requireIssue(t, Validate(d, nil), "CASE_OVERLAP")
	}
	d := binaryScale(t, false, other(LT), other(EQ), other(GT))
	d.Parameters[1].Number.Integer = true
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("mixed integer/float scale rejected: %+v", r)
	}
	// The only values in this interval are the endpoints and one interior float.
	a := 1.0
	b := math.Nextafter(a, math.Inf(1))
	c := math.Nextafter(b, math.Inf(1))
	d.Parameters[0].Number = &NumberDomain{Lower: &Bound{NumberFromFloat(a), true}, Upper: &Bound{NumberFromFloat(c), true}}
	d.Parameters[1].Number = &NumberDomain{Lower: &Bound{NumberFromFloat(a), true}, Upper: &Bound{NumberFromFloat(c), true}}
	if r := Validate(d, nil); !r.Valid {
		t.Fatalf("adjacent floating domain rejected: %+v", r)
	}
}

// Independent oracle on small finite integer domains. It evaluates the source
// predicate grammar separately and exhausts all 25 pairs, rather than using
// domain representatives or proofEval from the validator.
func oraclePredicate(c Condition, values map[string]int) bool {
	switch c.Op {
	case Always:
		return true
	case Never:
		return false
	case All:
		for _, x := range c.Conditions {
			if !oraclePredicate(x, values) {
				return false
			}
		}
		return true
	case Any:
		for _, x := range c.Conditions {
			if oraclePredicate(x, values) {
				return true
			}
		}
		return false
	case Not:
		return !oraclePredicate(c.Conditions[0], values)
	}
	a := values[c.Parameter]
	b := 0
	if c.OtherParameter != "" {
		b = values[c.OtherParameter]
	} else {
		b = int(numberFloat(*c.Value.Number))
	}
	switch c.Op {
	case EQ:
		return a == b
	case NE:
		return a != b
	case LT:
		return a < b
	case LE:
		return a <= b
	case GT:
		return a > b
	case GE:
		return a >= b
	}
	panic("unexpected oracle operator")
}

func TestDomainProofAgainstIndependentExhaustiveIntegerOracle(t *testing.T) {
	random := rand.New(rand.NewSource(20261025))
	ids := []string{"annual_contract_value_rub", "has_confirmed_contracts"}
	ops := []Operator{EQ, NE, LT, LE, GT, GE}
	atom := func() Condition {
		c := Condition{Op: ops[random.Intn(len(ops))], Parameter: ids[random.Intn(2)]}
		if random.Intn(2) == 0 {
			c.OtherParameter = ids[random.Intn(2)]
		} else {
			c.Value = nlit(float64(random.Intn(5) - 2))
		}
		return c
	}
	for iteration := 0; iteration < 120; iteration++ {
		cases := []Condition{}
		for count := 0; count < 3; count++ {
			c := atom()
			switch random.Intn(4) {
			case 0:
				c = allOf(c, atom())
			case 1:
				c = Condition{Op: Any, Conditions: []Condition{c, atom()}}
			case 2:
				c = Condition{Op: Not, Conditions: []Condition{c}}
			}
			cases = append(cases, c)
		}
		counterexample := false
		for x := -2; x <= 2; x++ {
			for y := -2; y <= 2; y++ {
				matches := 0
				for _, c := range cases {
					if oraclePredicate(c, map[string]int{ids[0]: x, ids[1]: y}) {
						matches++
					}
				}
				counterexample = counterexample || matches != 1
			}
		}
		r := Validate(binaryScale(t, true, cases...), nil)
		proofFound := hasIssue(r, "CASE_GAP") || hasIssue(r, "CASE_OVERLAP")
		if proofFound != counterexample {
			t.Fatalf("case %d: oracle=%t, issues=%+v, conditions=%+v", iteration, counterexample, r.Issues, cases)
		}
	}
}

func oracleFloatPredicate(c Condition, values map[string]float64) bool {
	switch c.Op {
	case Always:
		return true
	case Never:
		return false
	case All:
		for _, x := range c.Conditions {
			if !oracleFloatPredicate(x, values) {
				return false
			}
		}
		return true
	case Any:
		for _, x := range c.Conditions {
			if oracleFloatPredicate(x, values) {
				return true
			}
		}
		return false
	case Not:
		return !oracleFloatPredicate(c.Conditions[0], values)
	}
	a := values[c.Parameter]
	b := 0.0
	if c.OtherParameter != "" {
		b = values[c.OtherParameter]
	} else {
		b = numberFloat(*c.Value.Number)
	}
	switch c.Op {
	case EQ:
		return a == b
	case NE:
		return a != b
	case LT:
		return a < b
	case LE:
		return a <= b
	case GT:
		return a > b
	case GE:
		return a >= b
	}
	panic("unexpected floating oracle operator")
}

func TestDomainProofAgainstExhaustiveAdjacentFloatAndMixedOracle(t *testing.T) {
	lo := math.Nextafter(math.Nextafter(1, math.Inf(-1)), math.Inf(-1))
	hi := math.Nextafter(math.Nextafter(1, math.Inf(1)), math.Inf(1))
	values := []float64{}
	for x := lo; x <= hi; x = math.Nextafter(x, math.Inf(1)) {
		values = append(values, x)
	}
	random := rand.New(rand.NewSource(20261026))
	ids := []string{"annual_contract_value_rub", "has_confirmed_contracts", "z_value"}
	ops := []Operator{EQ, NE, LT, LE, GT, GE}
	atom := func() Condition {
		c := Condition{Op: ops[random.Intn(len(ops))], Parameter: ids[random.Intn(3)]}
		if random.Intn(2) == 0 {
			c.OtherParameter = ids[random.Intn(3)]
		} else {
			c.Value = nlit(values[random.Intn(len(values))])
		}
		return c
	}
	for _, mixed := range []bool{false, true} {
		for iteration := 0; iteration < 60; iteration++ {
			conditions := []Condition{}
			for count := 0; count < 3; count++ {
				c := atom()
				if random.Intn(2) == 0 {
					c = allOf(c, atom())
				} else {
					c = Condition{Op: Any, Conditions: []Condition{c, atom()}}
				}
				conditions = append(conditions, c)
			}
			counterexample := false
			for _, x := range values {
				for _, y := range values {
					if mixed && math.Trunc(y) != y {
						continue
					}
					for _, z := range values {
						matches := 0
						for _, c := range conditions {
							if oracleFloatPredicate(c, map[string]float64{ids[0]: x, ids[1]: y, ids[2]: z}) {
								matches++
							}
						}
						counterexample = counterexample || matches != 1
					}
				}
			}
			d := binaryScale(t, false, conditions...)
			for i := range d.Parameters {
				d.Parameters[i].Number = &NumberDomain{Lower: &Bound{NumberFromFloat(lo), true}, Upper: &Bound{NumberFromFloat(hi), true}}
			}
			d.Parameters[1].Number.Integer = mixed
			d.Parameters = append(d.Parameters, Parameter{ID: ids[2], Enabled: true, Title: "z", Source: Source{Supplier, ids[2]}, JSONName: ids[2], Type: Number, Unit: RUB, Number: &NumberDomain{Lower: &Bound{NumberFromFloat(lo), true}, Upper: &Bound{NumberFromFloat(hi), true}}})
			r := Validate(d, nil)
			proofFound := hasIssue(r, "CASE_GAP") || hasIssue(r, "CASE_OVERLAP")
			if proofFound != counterexample {
				t.Fatalf("mixed=%t case=%d oracle=%t issues=%+v conditions=%+v", mixed, iteration, counterexample, r.Issues, conditions)
			}
		}
	}
}

func TestAnswerOverlapAndUntrustedTemplates(t *testing.T) {
	d := validDefinition(t)
	condition := Condition{Op: Known, Parameter: d.Parameters[1].ID}
	d.Criteria[0].Answer.Cases = []AnswerCase{{When: condition, Template: "first"}, {When: condition, Template: "second"}}
	requireIssue(t, Validate(d, nil), "ANSWER_OVERLAP")
	for _, text := range []string{"${missing}", "${fmt.Sprintf(x)}", "${unclosed"} {
		d = validDefinition(t)
		d.Criteria[0].Answer.Template = text
		r := Validate(d, nil)
		if r.Valid || !hasIssue(r, "INVALID_TEMPLATE") && !hasIssue(r, "UNKNOWN_PARAMETER") {
			t.Fatalf("unsafe/unknown template accepted: %+v", r)
		}
	}
}

func TestProofLimitsNeverBecomeSuccessfulPartialValidation(t *testing.T) {
	d := validDefinition(t)
	conditions := []Condition{}
	for i := 0; i < 13; i++ {
		id := fmt.Sprintf("flag_%02d", i)
		d.Parameters = append(d.Parameters, Parameter{ID: id, Enabled: true, Title: id, Source: Source{Supplier, id}, JSONName: id, Type: Boolean, Unit: Flag})
		conditions = append(conditions, Condition{Op: Any, Conditions: []Condition{{Op: Known, Parameter: id}, {Op: Unknown, Parameter: id}}})
	}
	d.Criteria[0].ValueParameter = ""
	d.Criteria[0].Answer = &Answer{Template: "CONTROL"}
	d.Criteria[0].Cases = []ScoreCase{{ID: "all", When: Condition{Op: All, Conditions: conditions}, Points: 750}}
	requireIssue(t, Validate(d, nil), "COVERAGE_COMPLEXITY")
}

func TestUnknownRequiredFactCannotBeTheOnlyReachableMaximum(t *testing.T) {
	d := validDefinition(t)
	id := d.Parameters[1].ID
	d.Criteria[0].Cases = []ScoreCase{{ID: "known", When: Condition{Op: Always}, Points: 0}}
	d.Criteria[0].SpecialCases = []ScoreCase{{ID: "unknown_max", When: Condition{Op: Unknown, Parameter: id}, Points: 750}}
	r := Validate(d, nil)
	requireIssue(t, r, "UNREACHABLE_CASE")
	requireIssue(t, r, "MAXIMUM_NOT_REACHED")
}

func TestConditionNodeAndIndependentParameterLimits(t *testing.T) {
	t.Run("condition_nodes", func(t *testing.T) {
		d := validDefinition(t)
		conditions := make([]Condition, MaxValidationConditionNodes+1)
		for i := range conditions {
			conditions[i] = Condition{Op: Known, Parameter: d.Parameters[1].ID}
		}
		d.Criteria[0].Cases[0].When = Condition{Op: All, Conditions: conditions}
		requireIssue(t, Validate(d, nil), "CONDITION_COMPLEXITY")
	})
	t.Run("independent_parameters", func(t *testing.T) {
		d := validDefinition(t)
		conditions := []Condition{}
		for i := 0; i < 65; i++ {
			id := fmt.Sprintf("field_%02d", i)
			d.Parameters = append(d.Parameters, Parameter{ID: id, Enabled: true, Title: id, Source: Source{Supplier, id}, JSONName: id, Type: Boolean, Unit: Flag})
			conditions = append(conditions, Condition{Op: Known, Parameter: id})
		}
		d.Criteria[0].ValueParameter = ""
		d.Criteria[0].Answer = &Answer{Template: "CONTROL"}
		d.Criteria[0].Cases = []ScoreCase{{ID: "all", When: Condition{Op: All, Conditions: conditions}, Points: 750}}
		requireIssue(t, Validate(d, nil), "COVERAGE_COMPLEXITY")
	})
}

func TestThreeValuedConditionsPreserveFalseZeroAndUnknown(t *testing.T) {
	for _, known := range []bool{false, true} {
		for _, value := range []bool{false, true} {
			facts := map[string]proofValue{"flag": {known: known, typ: Boolean, boolean: value}, "zero": {known: true, typ: Number, number: 0}}
			want := unknownTruth
			if known {
				want = falseTruth
				if value {
					want = trueTruth
				}
			}
			if got := proofEval(Condition{Op: EQ, Parameter: "flag", Value: blit(true)}, facts); got != want {
				t.Fatalf("false/unknown conflated: %v/%v => %v", known, value, got)
			}
			if proofEval(compare(EQ, "zero", 0), facts) != trueTruth {
				t.Fatal("zero became unknown")
			}
			if proofEval(allOf(Condition{Op: Never}, Condition{Op: EQ, Parameter: "flag", Value: blit(true)}), facts) != falseTruth {
				t.Fatal("false AND unknown is not false")
			}
			if proofEval(Condition{Op: Any, Conditions: []Condition{{Op: Always}, {Op: EQ, Parameter: "flag", Value: blit(true)}}}, facts) != trueTruth {
				t.Fatal("true OR unknown is not true")
			}
		}
	}
	if proofEval(Condition{Op: Known, Parameter: "unassigned"}, map[string]proofValue{}) != unknownTruth {
		t.Fatal("unassigned proof variable treated as absent business fact")
	}
}
