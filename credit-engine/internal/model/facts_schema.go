package model

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// FactsSchemaSHA256 identifies the wire shape of active supplier fields.
// Parameter IDs, Go names and business requiredness do not change that shape:
// all declared wire keys are required and nullable. Input is not modified.
func FactsSchemaSHA256(parameters []Parameter) (string, error) {
	fields := []Parameter{}
	for _, p := range parameters {
		if p.Enabled && p.Source.Kind == Supplier {
			fields = append(fields, p)
		}
	}
	if len(fields) == 0 {
		return "", nil
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].JSONName < fields[j].JSONName })
	type bound struct {
		Value     string
		Inclusive bool
	}
	type field struct {
		Name         string
		Type         ValueType
		Unit         Unit
		Integer      bool
		Lower, Upper *bound
	}
	shape := []field{}
	for _, p := range fields {
		if p.Type != Number && p.Type != Boolean {
			return "", fmt.Errorf("model: invalid supplier type for %s", p.ID)
		}
		x := field{Name: p.JSONName, Type: p.Type, Unit: p.Unit}
		if p.Type == Number {
			if p.Number == nil {
				return "", fmt.Errorf("model: numeric supplier domain missing for %s", p.ID)
			}
			x.Integer = p.Number.Integer
			convert := func(b *Bound) (*bound, error) {
				if b == nil {
					return nil, nil
				}
				if !validNumber(b.Value) {
					return nil, fmt.Errorf("model: invalid supplier bound for %s", p.ID)
				}
				value := ""
				if p.Number.Integer {
					r, ok := numberRat(b.Value)
					if !ok {
						return nil, fmt.Errorf("model: invalid exact supplier bound for %s", p.ID)
					}
					value = r.RatString()
				} else {
					value = strconv.FormatFloat(numberFloat(b.Value), 'g', -1, 64)
				}
				return &bound{Value: value, Inclusive: b.Inclusive}, nil
			}
			var err error
			x.Lower, err = convert(p.Number.Lower)
			if err != nil {
				return "", err
			}
			x.Upper, err = convert(p.Number.Upper)
			if err != nil {
				return "", err
			}
		}
		shape = append(shape, x)
	}
	data, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
