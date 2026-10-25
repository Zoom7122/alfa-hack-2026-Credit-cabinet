package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const MaxValidationConditionNodes = 10000

func literalType(l *Literal) (ValueType, bool) {
	if l == nil {
		return "", false
	}
	n := 0
	var typ ValueType
	if l.Number != nil {
		n++
		typ = Number
		if !validNumber(*l.Number) {
			return "", false
		}
	}
	if l.Boolean != nil {
		n++
		typ = Boolean
	}
	if l.String != nil {
		n++
		typ = String
	}
	return typ, n == 1
}

func (v *validator) reference(id, path string, active bool, owner string) (Parameter, bool) {
	p, ok := v.parameters[id]
	if !ok {
		v.issue(path, "UNKNOWN_PARAMETER", "Parameter reference does not exist.")
		return Parameter{}, false
	}
	if active && !p.Enabled {
		v.issue(path, "DISABLED_DEPENDENCY", "An active object depends on a disabled parameter.")
	}
	if owner != "" {
		if _, ok := v.dependencies[owner]; ok {
			v.dependencies[owner][id] = true
		}
	}
	return p, true
}

func (v *validator) condition(c Condition, path string, active bool, owner string, depth int) {
	if v.conditionNodes >= MaxValidationConditionNodes {
		if v.conditionNodes == MaxValidationConditionNodes {
			v.issue(path, "CONDITION_COMPLEXITY", "Condition tree exceeds its node limit.")
		}
		v.conditionNodes++
		return
	}
	v.conditionNodes++
	if depth > MaxDefinitionDepth {
		v.issue(path, "CONDITION_COMPLEXITY", "Condition tree exceeds its depth limit.")
		return
	}
	shape := func(allowedParameter, allowedOther, allowedValue, allowedValues, allowedConditions bool) bool {
		return (allowedParameter || c.Parameter == "") && (allowedOther || c.OtherParameter == "") && (allowedValue || c.Value == nil) && (allowedValues || len(c.Values) == 0) && (allowedConditions || len(c.Conditions) == 0)
	}
	badShape := false
	switch c.Op {
	case Always, Never:
		badShape = !shape(false, false, false, false, false)
	case All, Any, Not:
		badShape = !shape(false, false, false, false, true) || len(c.Conditions) == 0 || c.Op == Not && len(c.Conditions) != 1
		for i, child := range c.Conditions {
			v.condition(child, fmt.Sprintf("%s/conditions/%d", path, i), active, owner, depth+1)
		}
	case Known, Unknown:
		badShape = !shape(true, false, false, false, false) || c.Parameter == ""
		if c.Parameter != "" {
			v.reference(c.Parameter, path+"/parameter", active, owner)
		}
	case EQ, NE, LT, LE, GT, GE, In, NotIn:
		membership := c.Op == In || c.Op == NotIn
		badShape = !shape(true, !membership, !membership, membership, false) || c.Parameter == ""
		if membership {
			badShape = badShape || len(c.Values) == 0
		} else {
			badShape = badShape || (c.OtherParameter == "") == (c.Value == nil)
		}
		left, leftOK := v.reference(c.Parameter, path+"/parameter", active, owner)
		checkLiteral := func(l *Literal, lpath string) {
			t, ok := literalType(l)
			if !ok {
				v.issue(lpath, "INVALID_LITERAL", "A literal must contain exactly one finite typed value.")
				return
			}
			if leftOK && t != left.Type {
				v.issue(lpath, "CONDITION_TYPE_MISMATCH", "Literal type differs from the parameter type.")
			}
			if leftOK && left.Unit == Date && t == String {
				if _, err := time.Parse(time.DateOnly, *l.String); err != nil {
					v.issue(lpath, "INVALID_DATE_LITERAL", "Expected a real YYYY-MM-DD date.")
				}
			}
		}
		if c.Value != nil {
			checkLiteral(c.Value, path+"/value")
		}
		for i := range c.Values {
			checkLiteral(&c.Values[i], fmt.Sprintf("%s/values/%d", path, i))
		}
		if c.OtherParameter != "" {
			right, rightOK := v.reference(c.OtherParameter, path+"/other_parameter", active, owner)
			if leftOK && rightOK && (left.Type != right.Type || left.Unit != right.Unit) {
				v.issue(path, "CONDITION_TYPE_MISMATCH", "Compared parameters must have matching types and units.")
			}
		}
		if leftOK && (c.Op == LT || c.Op == LE || c.Op == GT || c.Op == GE) && left.Type != Number {
			v.issue(path+"/op", "ORDER_REQUIRES_NUMBER", "Order comparisons require numeric parameters.")
		}
		if membership {
			seen := map[string]bool{}
			for i, l := range c.Values {
				key := literalKey(l)
				if leftOK && left.Type == Number && v.numericKind(left) == NumericFloat64 && l.Number != nil && validNumber(*l.Number) {
					x := numberFloat(*l.Number)
					if x == 0 {
						key = "n:0"
					} else {
						key = fmt.Sprintf("n:%g", x)
					}
				}
				if seen[key] {
					v.issue(fmt.Sprintf("%s/values/%d", path, i), "DUPLICATE_LITERAL", "Membership values must be unique.")
				}
				seen[key] = true
			}
		}
	default:
		v.issue(path+"/op", "UNKNOWN_OPERATOR", "Unknown condition operator.")
	}
	if badShape {
		v.issue(path, "INVALID_CONDITION_SHAPE", "Operator operands do not match its declared shape.")
	}
}

func literalKey(l Literal) string {
	if t, ok := literalType(&l); ok {
		switch t {
		case Number:
			r, _ := numberRat(*l.Number)
			return "n:" + r.RatString()
		case Boolean:
			return fmt.Sprintf("b:%t", *l.Boolean)
		case String:
			return "s:" + *l.String
		}
	}
	return "invalid"
}

func (v *validator) template(text, path string, active bool) {
	if text == "" {
		v.issue(path, "REQUIRED_TEMPLATE", "Answer template is required.")
		return
	}
	for pos := 0; pos < len(text); {
		start := strings.Index(text[pos:], "${")
		if start < 0 {
			return
		}
		start += pos
		end := strings.IndexByte(text[start+2:], '}')
		if end < 0 {
			v.issue(path, "INVALID_TEMPLATE", "Unclosed parameter placeholder.")
			return
		}
		end += start + 2
		id := text[start+2 : end]
		if !parameterIDPattern.MatchString(id) {
			v.issue(path, "INVALID_TEMPLATE", "Only ${parameter_id} placeholders are supported.")
		} else {
			v.reference(id, path, active, "")
		}
		pos = end + 1
	}
}

func (v *validator) parameterDependencies() {
	for i, p := range v.d.Parameters {
		path := fmt.Sprintf("/parameters/%d", i)
		seen := map[string]bool{}
		for j, id := range p.DependsOn {
			depPath := fmt.Sprintf("%s/depends_on/%d", path, j)
			if seen[id] {
				v.issue(depPath, "DUPLICATE_DEPENDENCY", "Dependency appears more than once.")
			}
			seen[id] = true
			v.reference(id, depPath, p.Enabled, p.ID)
		}
		if p.RequiredWhen != nil {
			v.condition(*p.RequiredWhen, path+"/required_when", p.Enabled, p.ID, 0)
		}
		if binding, ok := v.bindings[p.Source]; ok {
			for _, source := range binding.DependsOn {
				id, exists := v.sources[source]
				if !exists {
					v.issue(path+"/depends_on", "MISSING_SOURCE_DEPENDENCY", "Trusted calculation dependency is not declared as a parameter.")
					continue
				}
				v.reference(id, path+"/depends_on", p.Enabled, p.ID)
				if !seen[id] {
					v.issue(path+"/depends_on", "MISSING_CALCULATION_DEPENDENCY", "Declare trusted calculation dependency "+id+" explicitly.")
				}
			}
		}
	}
	state := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		if state[id] == 2 {
			return
		}
		if state[id] == 1 {
			v.issue(v.parameterPaths[id]+"/depends_on", "DEPENDENCY_CYCLE", "Parameter dependencies contain a cycle.")
			return
		}
		state[id] = 1
		deps := make([]string, 0, len(v.dependencies[id]))
		for dep := range v.dependencies[id] {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			if _, exists := v.parameters[dep]; exists {
				visit(dep)
			}
		}
		state[id] = 2
	}
	for _, p := range v.d.Parameters {
		visit(p.ID)
	}
}
