package rules

import (
	"bytes"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

type ParameterExplanation struct {
	Parameter      model.Parameter
	RawValue       Value
	EffectiveValue Value
}
type RuleExplanation struct {
	Kind         string
	Rule         model.Rule
	Status       Truth
	ChecksPassed bool
	Confirmed    bool
	Applied      bool
}
type Explanation struct {
	Result     Result
	Parameters []ParameterExplanation
	Rules      []RuleExplanation
}

// Explain uses the ordinary evaluator and the same domain/check preparation.
// Diagnostics never alter the result or convert a matched but gated rule into
// a decision. Caller-owned input and the compiled definition remain unchanged.
func (e *Engine) Explain(input Input) (Explanation, error) {
	result, err := e.Evaluate(input)
	if err != nil {
		return Explanation{}, err
	}
	x, err := e.prepare(input)
	if err != nil {
		return Explanation{}, err
	}
	data, err := model.Marshal(e.definition)
	if err != nil {
		return Explanation{}, err
	}
	metadata, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		return Explanation{}, err
	}
	out := Explanation{Result: result, Parameters: []ParameterExplanation{}, Rules: []RuleExplanation{}}
	for _, p := range metadata.Parameters {
		if p.Enabled {
			out.Parameters = append(out.Parameters, ParameterExplanation{Parameter: p, RawValue: input.Values[p.Source], EffectiveValue: x.facts[p.ID]})
		}
	}
	rawStop := false
	for _, rule := range metadata.Stops {
		if len(rule.RequiresChecks) == 0 {
			confirmed, err := x.confirmed(rule)
			if err != nil {
				return Explanation{}, err
			}
			rawStop = rawStop || confirmed
		}
	}
	for _, group := range []struct {
		kind  string
		rules []model.Rule
	}{{"STOP", metadata.Stops}, {"MANUAL_REVIEW", metadata.ManualReviews}} {
		for _, rule := range group.rules {
			status := UnknownTruth
			checksPassed := true
			if rule.Enabled {
				status, err = Condition(rule.When, x.facts)
				if err != nil {
					return Explanation{}, err
				}
			}
			for _, id := range rule.RequiresChecks {
				checksPassed = checksPassed && x.checks[id]
			}
			confirmed, err := x.confirmed(rule)
			if err != nil {
				return Explanation{}, err
			}
			applied := false
			if group.kind == "STOP" {
				applied = confirmed && result.Route == Rejected && result.Score == nil && (!rawStop || len(rule.RequiresChecks) == 0)
			} else {
				applied = confirmed && result.Score != nil && result.Score.Grade != "D"
			}
			out.Rules = append(out.Rules, RuleExplanation{Kind: group.kind, Rule: rule, Status: status, ChecksPassed: checksPassed, Confirmed: confirmed, Applied: applied})
		}
	}
	return out, nil
}
