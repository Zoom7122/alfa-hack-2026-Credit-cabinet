package rules

import (
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

type Truth int

const (
	UnknownTruth Truth = iota
	False
	True
)

func truth(v bool) Truth {
	if v {
		return True
	}
	return False
}

// Condition implements three-valued logic. facts is borrowed read-only.
func Condition(c model.Condition, facts map[string]Value) (Truth, error) {
	switch c.Op {
	case model.Always:
		return True, nil
	case model.Never:
		return False, nil
	case model.All, model.Any:
		unknown := false
		for _, child := range c.Conditions {
			v, err := Condition(child, facts)
			if err != nil {
				return UnknownTruth, err
			}
			if v == UnknownTruth {
				unknown = true
			}
			if c.Op == model.All && v == False {
				return False, nil
			}
			if c.Op == model.Any && v == True {
				return True, nil
			}
		}
		if unknown {
			return UnknownTruth, nil
		}
		return truth(c.Op == model.All), nil
	case model.Not:
		if len(c.Conditions) != 1 {
			return UnknownTruth, fmt.Errorf("rules: NOT requires one condition")
		}
		v, err := Condition(c.Conditions[0], facts)
		if err != nil || v == UnknownTruth {
			return UnknownTruth, err
		}
		return truth(v == False), nil
	}
	a := facts[c.Parameter]
	if c.Op == model.Known {
		return truth(a.Known()), nil
	}
	if c.Op == model.Unknown {
		return truth(!a.Known()), nil
	}
	if !a.Known() {
		return UnknownTruth, nil
	}
	if c.Op == model.In || c.Op == model.NotIn {
		matched := false
		for i := range c.Values {
			b, err := literal(&c.Values[i], a.numeric)
			if err != nil {
				return UnknownTruth, err
			}
			cmp, err := compare(a, b)
			if err != nil {
				return UnknownTruth, err
			}
			matched = matched || cmp == 0
		}
		if c.Op == model.NotIn {
			matched = !matched
		}
		return truth(matched), nil
	}
	var b Value
	if c.OtherParameter != "" {
		b = facts[c.OtherParameter]
		if !b.Known() {
			return UnknownTruth, nil
		}
	} else {
		var err error
		b, err = literal(c.Value, a.numeric)
		if err != nil {
			return UnknownTruth, err
		}
	}
	cmp, err := compare(a, b)
	if err != nil {
		return UnknownTruth, err
	}
	switch c.Op {
	case model.EQ:
		return truth(cmp == 0), nil
	case model.NE:
		return truth(cmp != 0), nil
	case model.LT:
		return truth(cmp < 0), nil
	case model.LE:
		return truth(cmp <= 0), nil
	case model.GT:
		return truth(cmp > 0), nil
	case model.GE:
		return truth(cmp >= 0), nil
	}
	return UnknownTruth, fmt.Errorf("rules: unsupported condition operator %q", c.Op)
}
