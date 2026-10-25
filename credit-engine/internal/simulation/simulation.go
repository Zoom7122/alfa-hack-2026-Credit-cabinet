// Package simulation evaluates an explicit test set with shared pure scoring.
// It has no clients, clock, database, optimizer, bank, or notification adapter.
package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

type Sample struct {
	EvaluationDate  string                        `json:"evaluation_date"`
	Application     contracts.DecisionRequest     `json:"application"`
	Statements      *contracts.StatementsResponse `json:"statements"`
	FactsDate       *string                       `json:"facts_date"`
	AdditionalFacts json.RawMessage               `json:"additional_facts"`
}
type Error struct {
	Code   string
	Fields []string
}

func (e *Error) Error() string        { return "simulation " + e.Code }
func invalid(code, path string) error { return &Error{Code: code, Fields: []string{path}} }

type Value struct {
	Known       bool              `json:"known"`
	Type        model.ValueType   `json:"type"`
	NumericKind model.NumericKind `json:"numeric_kind"`
	Value       json.RawMessage   `json:"value"`
	ExactValue  string            `json:"exact_value"`
}
type ParameterResult struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Source       model.Source    `json:"source"`
	Unit         model.Unit      `json:"unit"`
	ExpectedType model.ValueType `json:"expected_type"`
	Raw          Value           `json:"raw"`
	Effective    Value           `json:"effective"`
	Origins      []string        `json:"origins"`
}
type CriterionResult struct {
	ID                 string        `json:"id"`
	Title              string        `json:"title"`
	Block              model.Block   `json:"block"`
	Value              Value         `json:"value"`
	Points             float64       `json:"points"`
	MaxPoints          int           `json:"max_points"`
	Answer             string        `json:"answer"`
	CaseID             string        `json:"case_id"`
	Special            bool          `json:"special"`
	Unknown            bool          `json:"unknown"`
	Skipped            bool          `json:"skipped"`
	ContributesToScore bool          `json:"contributes_to_score"`
	Missing            []string      `json:"missing"`
	ReviewReason       *model.Reason `json:"review_reason"`
}
type CheckResult struct {
	ID      string   `json:"id"`
	Passed  bool     `json:"passed"`
	Skipped bool     `json:"skipped"`
	Status  string   `json:"status"`
	Fields  []string `json:"fields"`
	Message string   `json:"message"`
}
type RuleResult struct {
	ID             string       `json:"id"`
	Title          string       `json:"title"`
	Kind           string       `json:"kind"`
	Enabled        bool         `json:"enabled"`
	AfterCriterion string       `json:"after_criterion"`
	Status         string       `json:"status"`
	ChecksPassed   bool         `json:"checks_passed"`
	Confirmed      bool         `json:"confirmed"`
	Applied        bool         `json:"applied"`
	Reason         model.Reason `json:"reason"`
}
type Result struct {
	ModelVersion     string                 `json:"model_version"`
	DefinitionSHA256 string                 `json:"definition_sha256"`
	EvaluationDate   string                 `json:"evaluation_date"`
	Route            rules.Route            `json:"route"`
	Score            *contracts.ScoreResult `json:"score"`
	Reasons          []contracts.Reason     `json:"reasons"`
	Missing          []string               `json:"missing"`
	Parameters       []ParameterResult      `json:"parameters"`
	Criteria         []CriterionResult      `json:"criteria"`
	Checks           []CheckResult          `json:"checks"`
	Rules            []RuleResult           `json:"rules"`
}

func truth(status rules.Truth) string {
	switch status {
	case rules.True:
		return "TRUE"
	case rules.False:
		return "FALSE"
	default:
		return "UNKNOWN"
	}
}
func describe(v rules.Value) (Value, error) {
	out := Value{Known: v.Known(), Type: v.Type(), NumericKind: v.NumericKind(), Value: json.RawMessage("null"), ExactValue: v.Format()}
	if !v.Known() {
		return out, nil
	}
	var value any
	switch v.Type() {
	case model.Boolean:
		value, _ = v.Bool()
	case model.String:
		value, _ = v.Text()
	case model.Number:
		if v.NumericKind() == model.NumericInteger || v.NumericKind() == model.NumericInt64 {
			exact, _ := v.Rational()
			out.Value = json.RawMessage(exact.Num().String())
			return out, nil
		}
		var ok bool
		value, ok = v.Float()
		if !ok {
			return Value{}, invalid("NONFINITE_VALUE", "/")
		}
	default:
		return Value{}, invalid("UNSUPPORTED_VALUE", "/")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return Value{}, err
	}
	out.Value = data
	return out, nil
}

func Run(ctx context.Context, definition model.ModelDefinition, sample Sample) (Result, error) {
	return run(ctx, definition, sample, []model.ModelDefinition{definition})
}

func run(ctx context.Context, definition model.ModelDefinition, sample Sample, allowed []model.ModelDefinition) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	date, err := time.Parse(time.DateOnly, sample.EvaluationDate)
	if err != nil || date.IsZero() {
		return Result{}, invalid("EXPLICIT_EVALUATION_DATE_REQUIRED", "/evaluation_date")
	}
	if definition.BorrowerType != sample.Application.Borrower.Type {
		return Result{}, invalid("MODEL_BORROWER_TYPE_MISMATCH", "/application/borrower/type")
	}
	bindings, err := sourcecatalog.Bindings(definition.BorrowerType)
	if err != nil {
		return Result{}, err
	}
	validation := model.Validate(definition, bindings)
	if !validation.Valid {
		return Result{}, &modelgen.ValidationError{Version: definition.Version, Result: validation}
	}
	engine, err := rules.Compile(definition, bindings)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	prepared, err := scoring.PrepareApplication(sample.Application, date)
	if err != nil {
		return Result{}, err
	}
	stops, err := engine.Stops(prepared.Input)
	if err != nil {
		return Result{}, err
	}
	if len(stops) == 0 {
		facts, err := decodeFacts(definition, allowed, sample.AdditionalFacts)
		if err != nil {
			return Result{}, err
		}
		usesFacts := false
		for _, p := range definition.Parameters {
			usesFacts = usesFacts || p.Enabled && p.Source.Kind == model.Supplier
		}
		if usesFacts {
			if sample.FactsDate != nil {
				prepared, err = scoring.WithSupplierFacts(prepared, facts, *sample.FactsDate)
				if err != nil {
					return Result{}, invalid("INVALID_FACTS_DATE", "/facts_date")
				}
			} else {
				prepared = scoring.WithAdditionalFacts(prepared, facts)
			}
			stops, err = engine.Stops(prepared.Input)
			if err != nil {
				return Result{}, err
			}
		}
		if len(stops) == 0 && prepared.BorrowerType == contracts.BorrowerTypeLegalEntity && sample.Statements != nil {
			prepared, err = scoring.WithStatements(prepared, *sample.Statements)
			if err != nil {
				return Result{}, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	explanation, err := engine.Explain(prepared.Input)
	if err != nil {
		return Result{}, err
	}
	assessment, err := scoring.ToAssessment(scoring.ModelAssessment{Result: explanation.Result, CashFlow: prepared.CashFlow})
	if err != nil {
		return Result{}, err
	}
	canonical, err := model.Marshal(definition)
	if err != nil {
		return Result{}, err
	}
	out := Result{ModelVersion: definition.Version, DefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(canonical)), EvaluationDate: sample.EvaluationDate, Route: explanation.Result.Route, Score: assessment.Score, Reasons: assessment.Reasons, Missing: []string{}, Parameters: []ParameterResult{}, Criteria: []CriterionResult{}, Checks: []CheckResult{}, Rules: []RuleResult{}}
	seen := map[string]bool{}
	if out.Route == rules.NeedInfo {
		for _, reason := range out.Reasons {
			for _, field := range reason.Fields {
				if !seen[field] {
					seen[field] = true
					out.Missing = append(out.Missing, field)
				}
			}
		}
	}
	for _, parameter := range explanation.Parameters {
		raw, err := describe(parameter.RawValue)
		if err != nil {
			return Result{}, err
		}
		effective, err := describe(parameter.EffectiveValue)
		if err != nil {
			return Result{}, err
		}
		origins := append([]string{}, prepared.Input.Origins[parameter.Parameter.Source]...)
		out.Parameters = append(out.Parameters, ParameterResult{ID: parameter.Parameter.ID, Title: parameter.Parameter.Title, Source: parameter.Parameter.Source, Unit: parameter.Parameter.Unit, ExpectedType: parameter.Parameter.Type, Raw: raw, Effective: effective, Origins: origins})
	}
	for _, check := range explanation.Result.Checks {
		out.Checks = append(out.Checks, CheckResult{check.ID, check.Passed, check.Skipped, truth(check.Status), append([]string{}, check.Fields...), check.Message})
	}
	for _, rule := range explanation.Rules {
		out.Rules = append(out.Rules, RuleResult{rule.Rule.ID, rule.Rule.Title, rule.Kind, rule.Rule.Enabled, rule.Rule.AfterCriterion, truth(rule.Status), rule.ChecksPassed, rule.Confirmed, rule.Applied, rule.Rule.Reason})
	}
	scored := map[string]bool{}
	if explanation.Result.Score != nil {
		for _, criterion := range explanation.Result.Score.Criteria {
			scored[criterion.ID] = true
		}
	}
	for _, criterion := range definition.Criteria {
		if !criterion.Enabled {
			continue
		}
		evaluated, err := engine.EvaluateCriterion(criterion.ID, prepared.Input)
		if err != nil {
			return Result{}, err
		}
		value, err := describe(evaluated.Criterion.Value)
		if err != nil {
			return Result{}, err
		}
		c := evaluated.Criterion
		out.Criteria = append(out.Criteria, CriterionResult{ID: criterion.ID, Title: criterion.Title, Block: criterion.Block, Value: value, Points: c.Points, MaxPoints: criterion.MaxPoints, Answer: c.Answer, CaseID: c.CaseID, Special: c.Special, Unknown: c.Unknown, Skipped: evaluated.Skipped, ContributesToScore: scored[criterion.ID], Missing: append([]string{}, evaluated.Missing...), ReviewReason: evaluated.ReviewReason})
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return out, nil
}
