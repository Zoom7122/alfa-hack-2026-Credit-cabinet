package model

import (
	"math/big"
	"sort"
	"time"
)

const MaxCoverageStates = 250000

type proofValue struct {
	known   bool
	typ     ValueType
	number  float64
	exact   *big.Rat
	boolean bool
	text    string
}

type truth uint8

const (
	unknownTruth truth = iota
	falseTruth
	trueTruth
)

func fromLiteral(l Literal) proofValue {
	if l.Number != nil {
		return proofValue{known: true, typ: Number, number: numberFloat(*l.Number)}
	}
	if l.Boolean != nil {
		return proofValue{known: true, typ: Boolean, boolean: *l.Boolean}
	}
	if l.String != nil {
		return proofValue{known: true, typ: String, text: *l.String}
	}
	return proofValue{}
}

func fromLiteralFor(l Literal, left proofValue) proofValue {
	result := fromLiteral(l)
	if l.Number != nil && left.exact != nil {
		result.exact, _ = numberRat(*l.Number)
	}
	return result
}

func proofRat(value proofValue) *big.Rat {
	if value.exact != nil {
		return value.exact
	}
	return new(big.Rat).SetFloat64(value.number)
}

func compareProof(a, b proofValue) int {
	if a.exact == nil && b.exact == nil {
		if a.number < b.number {
			return -1
		}
		if a.number > b.number {
			return 1
		}
		return 0
	}
	return proofRat(a).Cmp(proofRat(b))
}

func equalProof(a, b proofValue) bool {
	if a.typ != b.typ {
		return false
	}
	switch a.typ {
	case Number:
		return compareProof(a, b) == 0
	case Boolean:
		return a.boolean == b.boolean
	case String:
		return a.text == b.text
	}
	return false
}

// Unassigned proof variables differ from explicitly unknown business facts.
// This also permits safe pruning without treating unassigned KNOWN as false.
func proofEval(c Condition, facts map[string]proofValue) truth {
	switch c.Op {
	case Always:
		return trueTruth
	case Never:
		return falseTruth
	case All, Any:
		unknown := false
		for _, child := range c.Conditions {
			t := proofEval(child, facts)
			if c.Op == All && t == falseTruth {
				return falseTruth
			}
			if c.Op == Any && t == trueTruth {
				return trueTruth
			}
			unknown = unknown || t == unknownTruth
		}
		if unknown {
			return unknownTruth
		}
		if c.Op == All {
			return trueTruth
		}
		return falseTruth
	case Not:
		if len(c.Conditions) != 1 {
			return unknownTruth
		}
		t := proofEval(c.Conditions[0], facts)
		if t == trueTruth {
			return falseTruth
		}
		if t == falseTruth {
			return trueTruth
		}
		return unknownTruth
	}
	left, assigned := facts[c.Parameter]
	if !assigned {
		return unknownTruth
	}
	if c.Op == Known || c.Op == Unknown {
		b := left.known
		if c.Op == Unknown {
			b = !b
		}
		if b {
			return trueTruth
		}
		return falseTruth
	}
	if !left.known {
		return unknownTruth
	}
	if c.Op == In || c.Op == NotIn {
		found := false
		for _, literal := range c.Values {
			found = found || equalProof(left, fromLiteralFor(literal, left))
		}
		if c.Op == NotIn {
			found = !found
		}
		if found {
			return trueTruth
		}
		return falseTruth
	}
	var right proofValue
	if c.OtherParameter != "" {
		var exists bool
		right, exists = facts[c.OtherParameter]
		if !exists || !right.known {
			return unknownTruth
		}
	} else if c.Value != nil {
		right = fromLiteralFor(*c.Value, left)
	} else {
		return unknownTruth
	}
	b := false
	switch c.Op {
	case EQ:
		b = equalProof(left, right)
	case NE:
		b = !equalProof(left, right)
	case LT:
		b = compareProof(left, right) < 0
	case LE:
		b = compareProof(left, right) <= 0
	case GT:
		b = compareProof(left, right) > 0
	case GE:
		b = compareProof(left, right) >= 0
	default:
		return unknownTruth
	}
	if b {
		return trueTruth
	}
	return falseTruth
}

func collectCondition(c Condition, refs map[string]bool, conditions *[]Condition) {
	*conditions = append(*conditions, c)
	if c.Parameter != "" {
		refs[c.Parameter] = true
	}
	if c.OtherParameter != "" {
		refs[c.OtherParameter] = true
	}
	for _, child := range c.Conditions {
		collectCondition(child, refs, conditions)
	}
}

func (v *validator) proofDomains(ids []string, conditions []Condition) ([][]proofValue, bool) {
	parent := map[string]string{}
	for _, id := range ids {
		if v.parameters[id].Type == Number {
			parent[id] = id
		}
	}
	var root func(string) string
	root = func(id string) string {
		if parent[id] != id {
			parent[id] = root(parent[id])
		}
		return parent[id]
	}
	for _, c := range conditions {
		if c.OtherParameter != "" && v.parameters[c.Parameter].Type == Number {
			a, b := root(c.Parameter), root(c.OtherParameter)
			parent[b] = a
		}
	}
	groups := map[string][]string{}
	for id := range parent {
		r := root(id)
		groups[r] = append(groups[r], id)
	}
	numeric := map[string][]proofValue{}
	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	for _, key := range groupKeys {
		group := groups[key]
		sort.Strings(group)
		candidates, ok := v.numericCandidates(group, conditions)
		if !ok {
			return nil, false
		}
		for id, values := range candidates {
			numeric[id] = values
		}
	}
	dateLiterals := map[string]bool{}
	for _, c := range conditions {
		if c.Value != nil && c.Value.String != nil {
			dateLiterals[*c.Value.String] = true
		}
		for _, l := range c.Values {
			if l.String != nil {
				dateLiterals[*l.String] = true
			}
		}
	}
	dateValues := []string{}
	for s := range dateLiterals {
		if _, err := time.Parse(time.DateOnly, s); err == nil {
			dateValues = append(dateValues, s)
		}
	}
	sort.Strings(dateValues)
	date := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for count := 0; count < len(ids); date = date.AddDate(0, 0, 1) {
		s := date.Format(time.DateOnly)
		if !dateLiterals[s] {
			dateValues = append(dateValues, s)
			count++
		}
	}
	domains := make([][]proofValue, len(ids))
	states := 1
	for i, id := range ids {
		p := v.parameters[id]
		domain := []proofValue{{typ: p.Type}}
		switch p.Type {
		case Number:
			domain = append(domain, numeric[id]...)
		case Boolean:
			domain = append(domain, proofValue{known: true, typ: Boolean}, proofValue{known: true, typ: Boolean, boolean: true})
		case String:
			values := p.StringValues
			if p.Unit == Date {
				values = dateValues
			}
			for _, s := range values {
				domain = append(domain, proofValue{known: true, typ: String, text: s})
			}
		}
		if len(domain) < 2 || states > MaxCoverageStates/len(domain) {
			return nil, false
		}
		states *= len(domain)
		domains[i] = domain
	}
	return domains, true
}
