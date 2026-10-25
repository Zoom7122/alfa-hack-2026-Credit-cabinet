package model

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
)

// NumberLiteral preserves a configuration number's decimal token. It remains
// a JSON number, including when its integer value is larger than 2^53.
type NumberLiteral = json.Number

type NumericKind string

const (
	NumericFloat64      NumericKind = "FLOAT64"
	NumericInt64        NumericKind = "INT64"
	NumericInteger      NumericKind = "INTEGER"
	NumericRational     NumericKind = "RATIONAL"
	MaxNumberTokenBytes             = 256
	MaxNumberExponent               = 512
)

// NumberFromFloat is for migrating existing binary floating-point constants.
// Exact new thresholds should be constructed from their original decimal text.
func NumberFromFloat(value float64) NumberLiteral {
	return NumberLiteral(strconv.FormatFloat(value, 'g', -1, 64))
}

func validNumber(n NumberLiteral) bool {
	raw := n.String()
	if len(raw) == 0 || len(raw) > MaxNumberTokenBytes || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') || !json.Valid([]byte(raw)) {
		return false
	}
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(raw[i+1:])
		if err != nil || exponent < -MaxNumberExponent || exponent > MaxNumberExponent {
			return false
		}
	}
	x, err := n.Float64()
	return err == nil && finite(x)
}

func numberFloat(n NumberLiteral) float64 { x, _ := n.Float64(); return x }

func numberRat(n NumberLiteral) (*big.Rat, bool) {
	if !validNumber(n) {
		return nil, false
	}
	x, ok := new(big.Rat).SetString(n.String())
	return x, ok
}
