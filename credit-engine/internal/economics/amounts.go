// Package economics implements the shared, stateless loan mathematics of MS1
// and MS2. REST loan rates are percentages; mathematical rates are fractions.
package economics

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

var (
	ErrInvalidInput   = errors.New("invalid financial input")
	ErrArithmetic     = errors.New("invalid financial calculation")
	ErrAmountOverflow = errors.New("amount does not fit int64 kopecks")
)

// BasisPointsFromPercent checks the agreed REST range and precision without
// an epsilon or a floating-point percent*100 integer comparison.
func BasisPointsFromPercent(percent float64) (int, error) {
	if !finite(percent) || percent < 0 || percent > 100 {
		return 0, fmt.Errorf("%w: annual rate must be between 0 and 100 percent", ErrInvalidInput)
	}
	whole, fraction, _ := strings.Cut(strconv.FormatFloat(percent, 'f', -1, 64), ".")
	if len(fraction) > 2 {
		return 0, fmt.Errorf("%w: annual rate must have at most two decimal places", ErrInvalidInput)
	}
	units, err := strconv.Atoi(whole)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid annual rate", ErrInvalidInput)
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	basisPoints, err := strconv.Atoi(fraction)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid annual rate", ErrInvalidInput)
	}
	return units*100 + basisPoints, nil
}

func AnnualRateFromPercent(percent float64) (float64, error) {
	basisPoints, err := BasisPointsFromPercent(percent)
	if err != nil {
		return 0, err
	}
	return float64(basisPoints) / 10000, nil
}

// RoundToKopecks rounds once, half away from zero, using the shortest decimal
// representation of a computed float64. Rational arithmetic is used only at
// this rounding boundary; all financial calculations remain float64.
// This avoids both an arbitrary epsilon and the binary 1.005*100 tie problem.
func RoundToKopecks(amountRUB float64) (int64, error) {
	if !finite(amountRUB) {
		return 0, fmt.Errorf("%w: money must be finite", ErrInvalidInput)
	}
	amount, ok := new(big.Rat).SetString(strconv.FormatFloat(amountRUB, 'f', -1, 64))
	if !ok {
		return 0, fmt.Errorf("%w: cannot represent money", ErrArithmetic)
	}
	numerator := new(big.Int).Mul(amount.Num(), big.NewInt(100))
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(numerator, amount.Denom(), remainder)
	twiceRemainder := new(big.Int).Lsh(new(big.Int).Abs(remainder), 1)
	if twiceRemainder.Cmp(amount.Denom()) >= 0 {
		whole.Add(whole, big.NewInt(int64(numerator.Sign())))
	}
	if !whole.IsInt64() {
		return 0, ErrAmountOverflow
	}
	return whole.Int64(), nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
