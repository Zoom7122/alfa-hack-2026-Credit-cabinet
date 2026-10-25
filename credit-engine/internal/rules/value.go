// Package rules executes checked scoring definitions without I/O or clocks.
package rules

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

// Value is immutable. Its zero value is unknown, distinct from 0 and false.
// Constructors and accessors copy mutable big numbers.
type Value struct {
	kind     model.ValueType
	numeric  model.NumericKind
	floating float64
	exact    *big.Rat
	boolean  bool
	text     string
}

func Unknown() Value        { return Value{} }
func Boolean(v bool) Value  { return Value{kind: model.Boolean, boolean: v} }
func String(v string) Value { return Value{kind: model.String, text: v} }
func Int64(v int64) Value {
	return Value{kind: model.Number, numeric: model.NumericInt64, exact: new(big.Rat).SetInt64(v)}
}
func Integer(v *big.Int) Value {
	if v == nil {
		return Unknown()
	}
	return Value{kind: model.Number, numeric: model.NumericInteger, exact: new(big.Rat).SetInt(v)}
}
func Rational(v *big.Rat) Value {
	if v == nil {
		return Unknown()
	}
	return Value{kind: model.Number, numeric: model.NumericRational, exact: new(big.Rat).Set(v)}
}
func Float64(v float64) Value {
	return Value{kind: model.Number, numeric: model.NumericFloat64, floating: v}
}
func (v Value) Known() bool                    { return v.kind != "" }
func (v Value) Type() model.ValueType          { return v.kind }
func (v Value) NumericKind() model.NumericKind { return v.numeric }
func (v Value) Bool() (bool, bool)             { return v.boolean, v.kind == model.Boolean }
func (v Value) Text() (string, bool)           { return v.text, v.kind == model.String }
func (v Value) Float() (float64, bool) {
	if v.kind != model.Number {
		return 0, false
	}
	if v.exact != nil {
		f, _ := v.exact.Float64()
		return f, finite(f)
	}
	return v.floating, finite(v.floating)
}
func (v Value) Rational() (*big.Rat, bool) {
	if v.kind != model.Number {
		return nil, false
	}
	if v.exact != nil {
		return new(big.Rat).Set(v.exact), true
	}
	if !finite(v.floating) {
		return nil, false
	}
	return new(big.Rat).SetFloat64(v.floating), true
}
func (v Value) Format() string {
	if !v.Known() {
		return ""
	}
	switch v.kind {
	case model.Boolean:
		return strconv.FormatBool(v.boolean)
	case model.String:
		return v.text
	case model.Number:
		if v.exact != nil {
			if v.exact.IsInt() {
				return v.exact.Num().String()
			}
			return v.exact.RatString()
		}
		return strconv.FormatFloat(v.floating, 'g', -1, 64)
	}
	return ""
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func literal(l *model.Literal, numeric model.NumericKind) (Value, error) {
	if l == nil {
		return Unknown(), fmt.Errorf("rules: missing literal")
	}
	if l.Boolean != nil {
		return Boolean(*l.Boolean), nil
	}
	if l.String != nil {
		return String(*l.String), nil
	}
	if l.Number == nil {
		return Unknown(), fmt.Errorf("rules: untyped literal")
	}
	if numeric == model.NumericFloat64 {
		v, err := l.Number.Float64()
		if err != nil || !finite(v) {
			return Unknown(), fmt.Errorf("rules: invalid floating literal")
		}
		return Float64(v), nil
	}
	r, ok := new(big.Rat).SetString(l.Number.String())
	if !ok {
		return Unknown(), fmt.Errorf("rules: invalid exact literal")
	}
	return Rational(r), nil
}

func compare(a, b Value) (int, error) {
	if a.kind != b.kind {
		return 0, fmt.Errorf("rules: incomparable %s and %s", a.kind, b.kind)
	}
	switch a.kind {
	case model.Number:
		ar, ok := a.Rational()
		if !ok {
			return 0, fmt.Errorf("rules: nonfinite number")
		}
		br, ok := b.Rational()
		if !ok {
			return 0, fmt.Errorf("rules: nonfinite number")
		}
		return ar.Cmp(br), nil
	case model.String:
		if a.text < b.text {
			return -1, nil
		}
		if a.text > b.text {
			return 1, nil
		}
		return 0, nil
	case model.Boolean:
		if a.boolean == b.boolean {
			return 0, nil
		}
		if a.boolean {
			return 1, nil
		}
		return -1, nil
	}
	return 0, fmt.Errorf("rules: unknown value cannot be compared")
}
