package model

import (
	"encoding/json"
	"fmt"
	"sort"
)

func proofWitness(values map[string]proofValue) string {
	data := map[string]any{}
	for id, value := range values {
		if !value.known {
			data[id] = nil
			continue
		}
		switch value.typ {
		case Number:
			if value.exact != nil {
				data[id] = value.exact.RatString()
			} else {
				data[id] = value.number
			}
		case Boolean:
			data[id] = value.boolean
		case String:
			data[id] = value.text
		}
	}
	encoded, _ := json.Marshal(data)
	return string(encoded)
}

// coverage partitions the declared domains into predicate-equivalent cells.
// It exhausts those cells; hitting a resource limit is a validation error,
// never a partial proof reported as successful.
func (v *validator) coverage(c Criterion, path string) {
	if !c.Enabled {
		return
	}
	refs := map[string]bool{}
	conditions := []Condition{}
	if c.ValueParameter != "" {
		refs[c.ValueParameter] = true
	}
	if c.AppliesWhen != nil {
		collectCondition(*c.AppliesWhen, refs, &conditions)
	}
	for _, group := range [][]ScoreCase{c.SpecialCases, c.Cases} {
		for _, s := range group {
			collectCondition(s.When, refs, &conditions)
		}
	}
	if c.Answer != nil {
		for _, a := range c.Answer.Cases {
			collectCondition(a.When, refs, &conditions)
		}
	}
	// Requiredness is part of a criterion's reachable input context. Follow
	// conditional-requiredness references before proving its attainable cases.
	expanded := map[string]bool{}
	for {
		pending := []string{}
		for id := range refs {
			if !expanded[id] {
				pending = append(pending, id)
			}
		}
		sort.Strings(pending)
		if len(pending) == 0 {
			break
		}
		for _, id := range pending {
			expanded[id] = true
			if p := v.parameters[id]; p.RequiredWhen != nil {
				collectCondition(*p.RequiredWhen, refs, &conditions)
			}
		}
		if len(refs) > 64 {
			v.issue(path, "COVERAGE_COMPLEXITY", "Criterion and its conditional requiredness reference more than 64 independent parameters.")
			return
		}
	}
	ids := make([]string, 0, len(refs))
	for id := range refs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 64 {
		v.issue(path, "COVERAGE_COMPLEXITY", "Criterion references more than 64 independent parameters.")
		return
	}
	domains, ok := v.proofDomains(ids, conditions)
	if !ok {
		v.issue(path, "COVERAGE_COMPLEXITY", "Exact domain proof exceeds the bounded proof budget.")
		return
	}
	facts := map[string]proofValue{}
	specialReach := make([]int, len(c.SpecialCases))
	ordinaryReach := make([]int, len(c.Cases))
	applicable := 0
	maxPoints := -1.0
	failed := false
	mark := func(points float64) {
		if points > maxPoints {
			maxPoints = points
		}
	}
	var visit func(int)
	visit = func(index int) {
		if failed {
			return
		}
		v.coverageStates++
		if v.coverageStates > MaxCoverageStates {
			v.issue(path, "COVERAGE_COMPLEXITY", "Exact model proof exceeded 250000 visited states; release is blocked.")
			failed = true
			return
		}
		if c.AppliesWhen != nil && proofEval(*c.AppliesWhen, facts) == falseTruth {
			return
		}
		if index < len(ids) {
			id := ids[index]
			for _, value := range domains[index] {
				facts[id] = value
				visit(index + 1)
				if failed {
					break
				}
			}
			delete(facts, id)
			return
		}
		if c.AppliesWhen != nil && proofEval(*c.AppliesWhen, facts) != trueTruth {
			return
		}
		for _, id := range ids {
			p := v.parameters[id]
			if p.Required && !facts[id].known && (p.RequiredWhen == nil || proofEval(*p.RequiredWhen, facts) != falseTruth) {
				return
			}
		}
		applicable++
		if c.Answer != nil {
			matched := -1
			for i, a := range c.Answer.Cases {
				if proofEval(a.When, facts) == trueTruth {
					if matched >= 0 {
						v.issue(fmt.Sprintf("%s/answer/cases/%d", path, i), "ANSWER_OVERLAP", "Multiple answer templates match "+proofWitness(facts)+".")
						failed = true
						return
					}
					matched = i
				}
			}
		}
		matched := -1
		for i, s := range c.SpecialCases {
			if proofEval(s.When, facts) == trueTruth {
				if matched >= 0 {
					v.issue(fmt.Sprintf("%s/special_cases/%d/when", path, i), "CASE_OVERLAP", "Multiple special cases match "+proofWitness(facts)+".")
					failed = true
					return
				}
				matched = i
			}
		}
		if matched >= 0 {
			specialReach[matched]++
			mark(c.SpecialCases[matched].Points)
			return
		}
		if c.ValueParameter != "" && !facts[c.ValueParameter].known {
			return
		}
		matched = -1
		allKnown := true
		for _, value := range facts {
			allKnown = allKnown && value.known
		}
		for i, s := range c.Cases {
			if proofEval(s.When, facts) == trueTruth {
				if matched >= 0 {
					v.issue(fmt.Sprintf("%s/cases/%d/when", path, i), "CASE_OVERLAP", "Multiple ordinary cases match "+proofWitness(facts)+".")
					failed = true
					return
				}
				matched = i
			}
		}
		if matched < 0 {
			if allKnown {
				v.issue(path+"/cases", "CASE_GAP", "No ordinary case covers known values "+proofWitness(facts)+".")
				failed = true
			}
			return
		}
		ordinaryReach[matched]++
		mark(c.Cases[matched].Points)
	}
	visit(0)
	if failed {
		return
	}
	if applicable == 0 {
		v.issue(path+"/applies_when", "UNREACHABLE_CRITERION", "The enabled criterion never applies in its declared domain.")
		return
	}
	for i, count := range specialReach {
		if count == 0 {
			v.issue(fmt.Sprintf("%s/special_cases/%d/when", path, i), "UNREACHABLE_CASE", "Special case cannot match its declared domain.")
		}
	}
	for i, count := range ordinaryReach {
		if count == 0 {
			v.issue(fmt.Sprintf("%s/cases/%d/when", path, i), "UNREACHABLE_CASE", "Ordinary case cannot match outside the special cases.")
		}
	}
	if maxPoints != float64(c.MaxPoints) {
		v.issue(path+"/max_points", "MAXIMUM_NOT_REACHED", fmt.Sprintf("Reachable case maximum is %g, declared maximum is %d.", maxPoints, c.MaxPoints))
	}
}
