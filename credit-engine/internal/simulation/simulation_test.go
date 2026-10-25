package simulation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/simulation"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

type statementsSource struct {
	value contracts.StatementsResponse
	calls int
}

func (s *statementsSource) Statements(context.Context, string, []int) (contracts.StatementsResponse, error) {
	s.calls++
	if s.value.BorrowerID == "" {
		return s.value, scoring.ErrStatementsNotFound
	}
	return s.value, nil
}

type forbiddenFacts struct{ calls int }

func (f *forbiddenFacts) Fetch(context.Context, *modelbindings.Entry, contracts.ScoringFactsRequest, time.Time) (modelbindings.Facts, error) {
	f.calls++
	return modelbindings.Facts{}, errors.New("v2 must not fetch facts")
}

func load(t *testing.T, name string) (model.ModelDefinition, simulation.Sample) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/decisions/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Request    contracts.DecisionRequest    `json:"request"`
		Statements contracts.StatementsResponse `json:"service_b_response"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	version, err := modelbindings.LegacyVersion(f.Request.Borrower.Type)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := modelbindings.Default()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalog.Lookup(version, f.Request.Borrower.Type)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := entry.Definition()
	if err != nil {
		t.Fatal(err)
	}
	return definition, simulation.Sample{EvaluationDate: "2026-10-23", Application: f.Request, Statements: &f.Statements, AdditionalFacts: json.RawMessage(`{}`)}
}

func TestSimulationMatchesCompiledScoringAcrossExistingFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/decisions/*.json")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	catalog, err := modelbindings.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		var envelope struct {
			Request *contracts.DecisionRequest `json:"request"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Request == nil {
			continue
		}
		count++
		t.Run(filepath.Base(path), func(t *testing.T) {
			definition, sample := load(t, strings.TrimSuffix(filepath.Base(path), ".json"))
			before, _ := json.Marshal(sample)
			statements := &statementsSource{value: *sample.Statements}
			facts := &forbiddenFacts{}
			date, _ := time.Parse(time.DateOnly, sample.EvaluationDate)
			working, err := scoring.NewEvaluatorWithCatalog(statements, facts, clock.Fixed{Time: date}, catalog)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := working.Evaluate(context.Background(), sample.Application)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := simulation.Run(context.Background(), definition, sample)
			if err != nil {
				t.Fatal(err)
			}
			if string(actual.Route) != string(expected.Route) || !reflect.DeepEqual(actual.Score, expected.Score) || !reflect.DeepEqual(actual.Reasons, expected.Reasons) {
				t.Fatalf("simulation differs from compiled evaluator: %s / %s", actual.Route, expected.Route)
			}
			after, _ := json.Marshal(sample)
			if !bytes.Equal(before, after) || facts.calls != 0 {
				t.Fatal("input mutated or facts requested")
			}
			if len(actual.Criteria) == 0 || len(actual.Parameters) == 0 || len(actual.Checks) == 0 {
				t.Fatal("missing diagnostics")
			}
			for _, criterion := range actual.Criteria {
				if actual.Score == nil && criterion.ContributesToScore {
					t.Fatal("partial criterion presented as final score")
				}
			}
		})
	}
	if count != 18 {
		t.Fatal("too few parity fixtures", count)
	}
}

func addFacts(d model.ModelDefinition) model.ModelDefinition {
	d.Version = "simulation-ip-v3"
	d.FactsSchemaVersion = "simulation-facts-v1"
	d.Parameters = append(d.Parameters, model.Parameter{ID: "new_numeric_fact", Enabled: true, Title: "Exact new integer", Source: model.Source{Kind: model.Supplier, Path: "new_numeric_fact"}, JSONName: "new_numeric_fact", Type: model.Number, Unit: model.Count, Number: &model.NumberDomain{Integer: true}, Required: true}, model.Parameter{ID: "new_boolean_fact", Enabled: true, Title: "New boolean", Source: model.Source{Kind: model.Supplier, Path: "new_boolean_fact"}, JSONName: "new_boolean_fact", Type: model.Boolean, Unit: model.Flag, Required: true})
	return d
}

func TestSimulationNewFactsPreserveExactIntegerFalseAndUnknown(t *testing.T) {
	d, sample := load(t, "uc04_ip_approved")
	d = addFacts(d)
	sample.AdditionalFacts = []byte(`{"new_numeric_fact":9007199254740993,"new_boolean_fact":false}`)
	date := "2026-10-22"
	sample.FactsDate = &date
	result, err := simulation.Run(context.Background(), d, sample)
	if err != nil {
		t.Fatal(err)
	}
	if result.Route != rules.Selection {
		t.Fatal(result.Route, result.Missing)
	}
	found := 0
	for _, p := range result.Parameters {
		switch p.ID {
		case "new_numeric_fact":
			found++
			if string(p.Effective.Value) != "9007199254740993" || p.Effective.ExactValue != "9007199254740993" || p.Effective.NumericKind != model.NumericInt64 {
				t.Fatal("integer lost precision")
			}
		case "new_boolean_fact":
			found++
			if string(p.Effective.Value) != "false" || !p.Effective.Known || p.Effective.Type != model.Boolean {
				t.Fatal("false became unknown")
			}
		}
	}
	if found != 2 {
		t.Fatal("missing new parameters")
	}
	sample.AdditionalFacts = []byte(`{"new_numeric_fact":0,"new_boolean_fact":null}`)
	result, err = simulation.Run(context.Background(), d, sample)
	if err != nil || result.Route != rules.NeedInfo {
		t.Fatal(result.Route, err)
	}
	if !reflect.DeepEqual(result.Missing, []string{"additional_facts.new_boolean_fact"}) {
		t.Fatal(result.Missing)
	}
	sample.AdditionalFacts = []byte(`{"new_numeric_fact":0,"new_boolean_fact":false}`)
	result, err = simulation.Run(context.Background(), d, sample)
	if err != nil || result.Route != rules.Selection {
		t.Fatal(result.Route, err)
	}
	for _, raw := range []string{`{"new_numeric_fact":9223372036854775808,"new_boolean_fact":true}`, `{"new_numeric_fact":1.5,"new_boolean_fact":true}`, `{"new_numeric_fact":0,"new_boolean_fact":0}`, `{"unknown":0}`, `{"new_numeric_fact":0,"new_numeric_fact":1}`} {
		sample.AdditionalFacts = []byte(raw)
		if _, err := simulation.Run(context.Background(), d, sample); err == nil {
			t.Fatal("invalid supplier values accepted", raw)
		}
	}
}

func TestSimulationComparisonUsesOneSampleAndReportsBoundaryChanges(t *testing.T) {
	left, sample := load(t, "uc04_ip_approved")
	data, _ := model.Marshal(left)
	right, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	right.Version = "simulation-ip-v3"
	baseline, err := simulation.Run(context.Background(), left, sample)
	if err != nil {
		t.Fatal(err)
	}
	// Move this fixture's total into C while retaining a complete 0..1000 scale.
	total := baseline.Score.Total
	if total >= 1000 {
		t.Fatal("fixture needs a non-maximum A total")
	}
	right.Classes = []model.ClassBand{{Grade: contracts.GradeD, MinTotal: 0, MaxTotal: 549, Reason: &model.Reason{Code: "CLASS_D", Message: "D", Fields: []string{}}}, {Grade: contracts.GradeC, MinTotal: 550, MaxTotal: total, Reason: &model.Reason{Code: "CLASS_C", Message: "C", Fields: []string{}}}, {Grade: contracts.GradeB, MinTotal: total + 1, MaxTotal: 999}, {Grade: contracts.GradeA, MinTotal: 1000, MaxTotal: 1000}}
	before, _ := json.Marshal(sample)
	compared, err := simulation.Compare(context.Background(), left, right, sample)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(sample)
	if !bytes.Equal(before, after) || !compared.RouteChanged || !compared.GradeChanged || compared.TotalDelta == nil || *compared.TotalDelta != 0 || compared.Left.Route != rules.Selection || compared.Right.Route != rules.Manual {
		t.Fatal("invalid comparison")
	}
	// A shared test set may contain the union of selected named supplier fields.
	right = addFacts(right)
	sample.AdditionalFacts = []byte(`{"new_numeric_fact":1,"new_boolean_fact":false}`)
	if _, err := simulation.Compare(context.Background(), left, right, sample); err != nil {
		t.Fatal("union of selected fact fields rejected", err)
	}
}

func TestSimulationValidationDateAndRuleExplanationOwnership(t *testing.T) {
	d, sample := load(t, "uc04_ip_approved")
	for _, date := range []string{"", "2026-10-23T12:00:00Z", "2026-02-30"} {
		bad := sample
		bad.EvaluationDate = date
		if _, err := simulation.Run(context.Background(), d, bad); err == nil {
			t.Fatal("date accepted", date)
		}
	}
	bad := d
	bad.Criteria = append([]model.Criterion{}, d.Criteria...)
	bad.Criteria[0].MaxPoints++
	_, err := simulation.Run(context.Background(), bad, sample)
	var invalid *modelgen.ValidationError
	if !errors.As(err, &invalid) || invalid.Result.Valid {
		t.Fatal("invalid model simulated", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := simulation.Run(ctx, d, sample); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bindings, _ := sourcecatalog.Bindings(d.BorrowerType)
	engine, err := rules.Compile(d, bindings)
	if err != nil {
		t.Fatal(err)
	}
	date, _ := time.Parse(time.DateOnly, sample.EvaluationDate)
	prepared, err := scoring.PrepareApplication(sample.Application, date)
	if err != nil {
		t.Fatal(err)
	}
	explained, err := engine.Explain(prepared.Input)
	if err != nil {
		t.Fatal(err)
	}
	for i := range explained.Parameters {
		if n := explained.Parameters[i].Parameter.Number; n != nil && n.Lower != nil {
			n.Lower.Value = "999999999999999999999"
			break
		}
	}
	if len(explained.Rules) > 0 {
		explained.Rules[0].Rule.When = model.Condition{Op: model.Always}
		explained.Rules[0].Rule.Reason.Code = "MUTATED"
	}
	again, err := engine.Explain(prepared.Input)
	if err != nil || !reflect.DeepEqual(explained.Result, again.Result) {
		t.Fatal("diagnostics leaked mutable engine state", err)
	}
}
