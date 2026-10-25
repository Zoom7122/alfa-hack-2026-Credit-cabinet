package jsonstrict

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const MaxNumberBytes = 256
const MaxNumberExponent = 512

type Bound struct {
	Value     string
	Inclusive bool
}

func number(raw []byte, path string) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > MaxNumberBytes || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') || !json.Valid(raw) {
		return "", fail(path, "INVALID_NUMBER", "expected a bounded JSON number token")
	}
	s := string(raw)
	if index := strings.IndexAny(s, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(s[index+1:])
		if err != nil || exponent < -MaxNumberExponent || exponent > MaxNumberExponent {
			return "", fail(path, "INVALID_NUMBER", "number exponent exceeds supported limits")
		}
	}
	return s, nil
}
func Float64(raw json.RawMessage, path string, lower, upper *Bound) (float64, error) {
	token, err := number(raw, path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseFloat(token, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fail(path, "INVALID_NUMBER", "expected finite float64")
	}
	for _, edge := range []struct {
		bound *Bound
		lower bool
	}{{lower, true}, {upper, false}} {
		if edge.bound == nil {
			continue
		}
		b, err := strconv.ParseFloat(edge.bound.Value, 64)
		if err != nil || math.IsNaN(b) || math.IsInf(b, 0) {
			return 0, fail(path, "INVALID_BOUND", "invalid generated domain bound")
		}
		if edge.lower && (value < b || value == b && !edge.bound.Inclusive) || !edge.lower && (value > b || value == b && !edge.bound.Inclusive) {
			return 0, fail(path, "OUT_OF_DOMAIN", "number is outside the declared domain")
		}
	}
	return value, nil
}
func Int64(raw json.RawMessage, path string, lower, upper *Bound) (int64, error) {
	token, err := number(raw, path)
	if err != nil {
		return 0, err
	}
	r, ok := new(big.Rat).SetString(token)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, fail(path, "INVALID_NUMBER", "expected an integer within int64")
	}
	for _, edge := range []struct {
		bound *Bound
		lower bool
	}{{lower, true}, {upper, false}} {
		if edge.bound == nil {
			continue
		}
		b, ok := new(big.Rat).SetString(edge.bound.Value)
		if !ok {
			return 0, fail(path, "INVALID_BOUND", "invalid generated domain bound")
		}
		cmp := r.Cmp(b)
		if edge.lower && (cmp < 0 || cmp == 0 && !edge.bound.Inclusive) || !edge.lower && (cmp > 0 || cmp == 0 && !edge.bound.Inclusive) {
			return 0, fail(path, "OUT_OF_DOMAIN", "number is outside the declared domain")
		}
	}
	return r.Num().Int64(), nil
}
