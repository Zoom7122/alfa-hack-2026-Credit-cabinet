package rules_test

import (
	"math"
	"math/big"
	"reflect"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	m "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

func number(s string) *m.Literal { n := m.NumberLiteral(s); return &m.Literal{Number: &n} }
func boolean(b bool) *m.Literal  { return &m.Literal{Boolean: &b} }
func eq(id string, b bool) m.Condition {
	return m.Condition{Op: m.EQ, Parameter: id, Value: boolean(b)}
}
func reason(code string) *m.Reason { return &m.Reason{Code: code, Message: code, Fields: []string{}} }
func definition() m.ModelDefinition {
	return m.ModelDefinition{FormatVersion: 1, ID: "test", Version: "test-v1", BorrowerType: contracts.BorrowerTypeSoleProprietor, Title: "Test", FactsSchemaVersion: "facts-v1",
		Parameters: []m.Parameter{
			{ID: "flag", Enabled: true, Title: "Flag", Source: m.Source{Kind: m.Supplier, Path: "flag"}, JSONName: "flag", Type: m.Boolean, Unit: m.Flag, Required: true},
			{ID: "amount", Enabled: true, Title: "Amount", Source: m.Source{Kind: m.Supplier, Path: "amount"}, JSONName: "amount", Type: m.Number, Unit: m.RUB, Required: true, Number: &m.NumberDomain{Integer: true, Lower: &m.Bound{Value: "0", Inclusive: true}}},
		},
		Criteria: []m.Criterion{
			{ID: "Q", Enabled: true, Title: "Q", Block: m.Questionnaire, ValueParameter: "flag", Answer: &m.Answer{Template: "flag=${flag}"}, MaxPoints: 750, UnknownAction: m.NeedInfo, SpecialCases: []m.ScoreCase{}, Cases: []m.ScoreCase{{ID: "yes", When: eq("flag", true), Points: 750}, {ID: "no", When: eq("flag", false), Points: 500}}},
			{ID: "E", Enabled: true, Title: "E", Block: m.Additional, ValueParameter: "amount", MaxPoints: 250, UnknownAction: m.NeedInfo, SpecialCases: []m.ScoreCase{}, Cases: []m.ScoreCase{{ID: "small", When: m.Condition{Op: m.LT, Parameter: "amount", Value: number("10")}, Points: 0}, {ID: "large", When: m.Condition{Op: m.GE, Parameter: "amount", Value: number("10")}, Points: 250}}},
		}, DataChecks: []m.DataCheck{}, Stops: []m.Rule{}, ManualReviews: []m.Rule{}, Classes: []m.ClassBand{{Grade: contracts.GradeD, MinTotal: 0, MaxTotal: 549, Reason: reason("D")}, {Grade: contracts.GradeC, MinTotal: 550, MaxTotal: 649, Reason: reason("C")}, {Grade: contracts.GradeB, MinTotal: 650, MaxTotal: 799}, {Grade: contracts.GradeA, MinTotal: 800, MaxTotal: 1000}},
	}
}
func compile(t *testing.T, d m.ModelDefinition) *rules.Engine {
	t.Helper()
	e, err := rules.Compile(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func input(flag rules.Value, amount rules.Value) rules.Input {
	return rules.Input{Values: map[m.Source]rules.Value{{Kind: m.Supplier, Path: "flag"}: flag, {Kind: m.Supplier, Path: "amount"}: amount}}
}

func TestThreeValuedConditions(t *testing.T) {
	for _, a := range []rules.Truth{rules.UnknownTruth, rules.False, rules.True} {
		for _, b := range []rules.Truth{rules.UnknownTruth, rules.False, rules.True} {
			for _, op := range []m.Operator{m.All, m.Any} {
				facts := map[string]rules.Value{}
				if a != rules.UnknownTruth {
					facts["a"] = rules.Boolean(a == rules.True)
				}
				if b != rules.UnknownTruth {
					facts["b"] = rules.Boolean(b == rules.True)
				}
				c := m.Condition{Op: op, Conditions: []m.Condition{eq("a", true), eq("b", true)}}
				want := rules.UnknownTruth
				if op == m.All {
					if a == rules.False || b == rules.False {
						want = rules.False
					} else if a == rules.True && b == rules.True {
						want = rules.True
					}
				} else {
					if a == rules.True || b == rules.True {
						want = rules.True
					} else if a == rules.False && b == rules.False {
						want = rules.False
					}
				}
				got, err := rules.Condition(c, facts)
				if err != nil || got != want {
					t.Fatalf("%v/%v/%v: %v %v", a, b, op, got, err)
				}
			}
		}
	}
	for _, v := range []rules.Value{rules.Boolean(false), rules.Int64(0), rules.String("")} {
		status, err := rules.Condition(m.Condition{Op: m.Known, Parameter: "x"}, map[string]rules.Value{"x": v})
		if err != nil || status != rules.True {
			t.Fatal("known zero lost")
		}
	}
	status, err := rules.Condition(m.Condition{Op: m.Not, Conditions: []m.Condition{eq("x", true)}}, nil)
	if err != nil || status != rules.UnknownTruth {
		t.Fatal("NOT unknown")
	}
}

func TestExactAndBinaryThresholds(t *testing.T) {
	for _, x := range []struct {
		value     rules.Value
		threshold string
		want      rules.Truth
	}{
		{rules.Int64(9007199254740993), "9007199254740992", rules.True},
		{rules.Int64(math.MaxInt64), "9223372036854775806", rules.True},
		{rules.Rational(big.NewRat(1, 10)), "0.1", rules.False},
		{rules.Float64(.1), "0.1", rules.False},
		{rules.Rational(new(big.Rat).Add(big.NewRat(1, 10), big.NewRat(1, 1000000000000000000))), "0.1", rules.True},
	} {
		got, err := rules.Condition(m.Condition{Op: m.GT, Parameter: "x", Value: number(x.threshold)}, map[string]rules.Value{"x": x.value})
		if err != nil || got != x.want {
			t.Fatal(x, got, err)
		}
	}
	integer := big.NewInt(42)
	v := rules.Integer(integer)
	integer.SetInt64(0)
	r, ok := v.Rational()
	if !ok || r.Cmp(big.NewRat(42, 1)) != 0 {
		t.Fatal("constructor alias")
	}
	r.SetInt64(0)
	r, _ = v.Rational()
	if r.Cmp(big.NewRat(42, 1)) != 0 {
		t.Fatal("accessor alias")
	}
}

func TestRoutingPriorityAndUnknownFacts(t *testing.T) {
	d := definition()
	d.DataChecks = []m.DataCheck{{ID: "confirmed", Enabled: true, Title: "Confirmed", SatisfiedWhen: eq("flag", true), Fields: []string{"flag"}, Message: "Need flag"}}
	d.Stops = []m.Rule{{ID: "STOP", Enabled: true, Title: "Stop", When: m.Condition{Op: m.EQ, Parameter: "amount", Value: number("0")}, Reason: *reason("STOP"), RequiresChecks: []string{"confirmed"}}}
	e := compile(t, d)
	for _, x := range []struct {
		flag, amount rules.Value
		route        rules.Route
	}{{rules.Boolean(true), rules.Int64(0), rules.Rejected}, {rules.Boolean(false), rules.Int64(0), rules.NeedInfo}, {rules.Unknown(), rules.Int64(0), rules.NeedInfo}, {rules.Boolean(true), rules.Unknown(), rules.NeedInfo}, {rules.Boolean(true), rules.Int64(10), rules.Selection}} {
		got, err := e.Evaluate(input(x.flag, x.amount))
		if err != nil || got.Route != x.route {
			t.Fatalf("%+v %v", got, err)
		}
	}
	d.Stops = append([]m.Rule{{ID: "EARLY", Enabled: true, Title: "Early", When: eq("flag", true), Reason: *reason("EARLY")}}, d.Stops...)
	e = compile(t, d)
	got, err := e.Evaluate(input(rules.Boolean(true), rules.Int64(0)))
	if err != nil || len(got.Reasons) != 1 || got.Reasons[0].Code != "EARLY" || got.Score != nil {
		t.Fatal(got, err)
	}
	got, err = e.Evaluate(input(rules.Boolean(true), rules.Unknown()))
	if err != nil || got.Route != rules.Rejected {
		t.Fatal("stop must precede missing", got, err)
	}
}

func TestManualOrderSpecialsClassAndSingleRounding(t *testing.T) {
	d := definition()
	d.Criteria[0].Cases[1].Points = 550
	d.Criteria[1].Cases[0].Points = 99.75
	d.ManualReviews = []m.Rule{{ID: "FIRST", Enabled: true, Title: "First", When: m.Condition{Op: m.Always}, Reason: *reason("FIRST")}, {ID: "AFTER", Enabled: true, Title: "After", When: m.Condition{Op: m.Always}, Reason: *reason("AFTER"), AfterCriterion: "E"}}
	d.Criteria[1].SpecialCases = []m.ScoreCase{{ID: "zero", When: m.Condition{Op: m.EQ, Parameter: "amount", Value: number("0")}, Points: 0, Result: &m.ResultOverride{Mode: m.NullResult}, ReviewReason: reason("SPECIAL")}}
	e := compile(t, d)
	got, err := e.Evaluate(input(rules.Boolean(false), rules.Int64(0)))
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{}
	for _, r := range got.Reasons {
		codes = append(codes, r.Code)
	}
	if !reflect.DeepEqual(codes, []string{"C", "FIRST", "SPECIAL", "AFTER"}) {
		t.Fatal(codes)
	}
	if got.Score.Criteria[1].Value.Known() || !got.Score.Criteria[1].Special || got.Score.Criteria[0].Answer != "flag=false" {
		t.Fatal(got.Score.Criteria)
	}
	got, err = e.Evaluate(input(rules.Boolean(false), rules.Int64(1)))
	if err != nil || got.Score.RawTotal != 649.75 || got.Score.Total != 650 || got.Score.Grade != contracts.GradeB {
		t.Fatal(got, err)
	}
	d.Criteria[0].Cases[1].Points = 0
	e = compile(t, d)
	got, err = e.Evaluate(input(rules.Boolean(false), rules.Int64(1)))
	if err != nil || got.Route != rules.Rejected || len(got.Reasons) != 1 || got.Reasons[0].Code != "D" {
		t.Fatal(got, err)
	}
}

func TestEngineOwnershipConcurrencyAndInvalidFacts(t *testing.T) {
	d := definition()
	e := compile(t, d)
	d.Criteria[0].Cases[0].Points = 0
	d.Version = "changed"
	if e.Version() != "test-v1" {
		t.Fatal("draft changed engine")
	}
	var wg sync.WaitGroup
	errors := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := e.Evaluate(input(rules.Boolean(true), rules.Int64(12)))
			if err == nil && (got.Route != rules.Selection || got.Score.Total != 1000) {
				t.Errorf("changed/concurrent result: %+v", got)
			}
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []rules.Value{rules.Boolean(false), rules.Float64(math.Inf(1)), rules.Float64(1), rules.Integer(big.NewInt(1))} {
		if _, err := e.Evaluate(input(rules.Boolean(true), v)); err == nil {
			t.Fatal("invalid representation accepted", v)
		}
	}
	got, err := e.Evaluate(input(rules.Boolean(true), rules.Int64(-1)))
	if err != nil || got.Route != rules.NeedInfo {
		t.Fatal("out-of-domain fact", got, err)
	}
	d = definition()
	d.Criteria[0].MaxPoints = 749
	if _, err := rules.Compile(d, nil); err == nil {
		t.Fatal("invalid model compiled")
	}
}

func TestMembershipComparisonsAndUnknownPolicies(t *testing.T) {
	facts := map[string]rules.Value{"x": rules.Int64(2), "y": rules.Int64(3)}
	for _, test := range []struct {
		op   m.Operator
		want rules.Truth
	}{{m.EQ, rules.False}, {m.NE, rules.True}, {m.LT, rules.True}, {m.LE, rules.True}, {m.GT, rules.False}, {m.GE, rules.False}} {
		got, err := rules.Condition(m.Condition{Op: test.op, Parameter: "x", OtherParameter: "y"}, facts)
		if err != nil || got != test.want {
			t.Fatal(test, got, err)
		}
	}
	for _, op := range []m.Operator{m.In, m.NotIn} {
		got, err := rules.Condition(m.Condition{Op: op, Parameter: "x", Values: []m.Literal{*number("0"), *number("2")}}, facts)
		want := rules.True
		if op == m.NotIn {
			want = rules.False
		}
		if err != nil || got != want {
			t.Fatal(op, got, err)
		}
	}
	for _, policy := range []m.UnknownAction{m.NeedInfo, m.Zero, m.Skip} {
		d := definition()
		d.Parameters[1].Required = false
		d.Criteria[1].UnknownAction = policy
		e := compile(t, d)
		got, err := e.Evaluate(input(rules.Boolean(true), rules.Unknown()))
		if err != nil {
			t.Fatal(err)
		}
		if policy == m.NeedInfo {
			if got.Route != rules.NeedInfo || got.Score != nil {
				t.Fatal(got)
			}
			if len(got.Reasons) != 1 || !reflect.DeepEqual(got.Reasons[0].Fields, []string{"additional_facts.amount"}) {
				t.Fatal("supplier origin lost", got.Reasons)
			}
		} else {
			if got.Score == nil || got.Score.Total != 750 {
				t.Fatal(got)
			}
			want := 2
			if policy == m.Skip {
				want = 1
			}
			if len(got.Score.Criteria) != want {
				t.Fatal(got.Score.Criteria)
			}
		}
	}
	d := definition()
	d.Parameters[1].RequiredWhen = &m.Condition{Op: m.EQ, Parameter: "flag", Value: boolean(true)}
	d.Criteria[1].AppliesWhen = &m.Condition{Op: m.EQ, Parameter: "flag", Value: boolean(true)}
	e := compile(t, d)
	got, err := e.Evaluate(input(rules.Boolean(false), rules.Unknown()))
	if err != nil || got.Score == nil || got.Score.Total != 500 {
		t.Fatal(got, err)
	}
}
