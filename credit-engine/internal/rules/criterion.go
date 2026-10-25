package rules

import (
	"fmt"
	"math"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

// CriterionEvaluation is an isolated explanation, never a credit decision.
// The full Evaluate additionally checks all required facts and routing rules.
type CriterionEvaluation struct {
	Criterion    CriterionResult
	ReviewReason *model.Reason
	Skipped      bool
	Missing      []string
}

// EvaluateCriterion uses the same case executor as Evaluate. It is useful for
// range verification and editor explanations without bypassing a full decision.
func (e *Engine) EvaluateCriterion(id string, input Input) (CriterionEvaluation, error) {
	var criterion *model.Criterion
	for i := range e.definition.Criteria {
		if e.definition.Criteria[i].ID == id {
			criterion = &e.definition.Criteria[i]
			break
		}
	}
	if criterion == nil {
		return CriterionEvaluation{}, fmt.Errorf("rules: unknown criterion %s", id)
	}
	c := *criterion
	empty := CriterionResult{ID: c.ID, Block: c.Block, MaxPoints: c.MaxPoints}
	if !c.Enabled {
		return CriterionEvaluation{Criterion: empty, Skipped: true, Missing: []string{}}, nil
	}
	references := map[string]bool{}
	var collect func(model.Condition)
	collect = func(condition model.Condition) {
		if condition.Parameter != "" {
			references[condition.Parameter] = true
		}
		if condition.OtherParameter != "" {
			references[condition.OtherParameter] = true
		}
		for _, child := range condition.Conditions {
			collect(child)
		}
	}
	if c.ValueParameter != "" {
		references[c.ValueParameter] = true
	}
	if c.AppliesWhen != nil {
		collect(*c.AppliesWhen)
	}
	for _, cases := range [][]model.ScoreCase{c.SpecialCases, c.Cases} {
		for _, candidate := range cases {
			collect(candidate.When)
		}
	}
	if c.Answer != nil {
		templates := []string{c.Answer.Template}
		for _, candidate := range c.Answer.Cases {
			collect(candidate.When)
			templates = append(templates, candidate.Template)
		}
		for _, template := range templates {
			for _, match := range placeholder.FindAllStringSubmatch(template, -1) {
				references[match[1]] = true
			}
		}
	}
	expanded := map[string]bool{}
	for {
		changed := false
		for _, p := range e.definition.Parameters {
			if !references[p.ID] || expanded[p.ID] {
				continue
			}
			expanded[p.ID] = true
			changed = true
			for _, id := range p.DependsOn {
				references[id] = true
			}
			if p.RequiredWhen != nil {
				collect(*p.RequiredWhen)
			}
		}
		if !changed {
			break
		}
	}
	filtered := Input{Values: map[model.Source]Value{}, Origins: input.Origins, FieldOrigins: input.FieldOrigins}
	for _, p := range e.definition.Parameters {
		if references[p.ID] {
			filtered.Values[p.Source] = input.Values[p.Source]
		}
	}
	x, err := e.prepare(filtered)
	if err != nil {
		return CriterionEvaluation{}, err
	}
	x.missing = nil
	if c.AppliesWhen != nil {
		status, err := Condition(*c.AppliesWhen, x.facts)
		if err != nil {
			return CriterionEvaluation{}, err
		}
		if status == False {
			return CriterionEvaluation{Criterion: empty, Skipped: true, Missing: []string{}}, nil
		}
		if status == UnknownTruth {
			x.needCondition(*c.AppliesWhen)
			return CriterionEvaluation{Criterion: empty, Missing: unique(x.missing)}, nil
		}
	}
	for _, p := range e.definition.Parameters {
		if !references[p.ID] || !p.Enabled {
			continue
		}
		value := filtered.Values[p.Source]
		if value.Known() {
			inside, err := domain(value, p)
			if err != nil {
				return CriterionEvaluation{}, err
			}
			if !inside {
				x.needParameter(p)
			}
		}
		if !p.Required {
			continue
		}
		if p.RequiredWhen != nil {
			status, err := Condition(*p.RequiredWhen, x.facts)
			if err != nil {
				return CriterionEvaluation{}, err
			}
			if status == False {
				continue
			}
			if status == UnknownTruth {
				x.needCondition(*p.RequiredWhen)
			}
		}
		if !x.facts[p.ID].Known() {
			x.needParameter(p)
		}
	}
	if len(x.missing) > 0 {
		return CriterionEvaluation{Criterion: empty, Missing: unique(x.missing)}, nil
	}
	result, reason, skip, err := x.criterion(c)
	if err != nil {
		return CriterionEvaluation{}, err
	}
	return CriterionEvaluation{Criterion: result, ReviewReason: reason, Skipped: skip, Missing: unique(x.missing)}, nil
}

type Classification struct {
	Total  int
	Grade  contracts.Grade
	Reason *model.Reason
}

// Classify performs the single final half-up rounding against the compiled
// class scale; it is also used by the full evaluation path.
func (e *Engine) Classify(raw float64) (Classification, error) {
	if !finite(raw) || raw < 0 || raw > model.TotalMaximum {
		return Classification{}, fmt.Errorf("rules: total is outside 0..1000")
	}
	total := int(math.Floor(raw + 0.5))
	for _, band := range e.definition.Classes {
		if total < band.MinTotal || total > band.MaxTotal {
			continue
		}
		result := Classification{Total: total, Grade: band.Grade}
		if band.Reason != nil {
			r := copyReason(*band.Reason)
			result.Reason = &r
		}
		return result, nil
	}
	return Classification{}, fmt.Errorf("rules: score %d outside compiled class scale", total)
}
