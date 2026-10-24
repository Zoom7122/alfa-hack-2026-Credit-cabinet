package economics

import (
	"fmt"
	"math"
)

// Period is one unrounded annuity payment and its principal/interest split.
type Period struct {
	Month             int
	OpeningBalanceRUB float64
	PaymentRUB        float64
	InterestRUB       float64
	PrincipalRUB      float64
	ClosingBalanceRUB float64
}

type Schedule struct {
	MonthlyPaymentRUB float64
	TotalPaymentsRUB  float64
	OverpaymentRUB    float64
	BalanceAfter12RUB float64
	Periods           []Period
}

// AnnuityFactor accepts an annual fraction (0.24 = 24%), not a REST percentage.
// Log1p/Expm1 evaluate the specified annuity formula stably near a zero rate.
func AnnuityFactor(termMonths int, annualRate float64) (float64, error) {
	if termMonths < 1 || termMonths > 36 {
		return 0, fmt.Errorf("%w: term must be between 1 and 36 months", ErrInvalidInput)
	}
	if !finite(annualRate) || annualRate < 0 || annualRate > 1 {
		return 0, fmt.Errorf("%w: annual mathematical rate must be between 0 and 1", ErrInvalidInput)
	}
	i := annualRate / 12
	if i == 0 {
		return 1 / float64(termMonths), nil
	}
	factor := i / -math.Expm1(-float64(termMonths)*math.Log1p(i))
	if !finite(factor) || factor <= 0 {
		return 0, fmt.Errorf("%w: annuity factor", ErrArithmetic)
	}
	return factor, nil
}

func Amortize(amountRUB int64, termMonths int, annualRate float64) (Schedule, error) {
	if amountRUB <= 0 {
		return Schedule{}, fmt.Errorf("%w: principal must be positive", ErrInvalidInput)
	}
	factor, err := AnnuityFactor(termMonths, annualRate)
	if err != nil {
		return Schedule{}, err
	}
	balance := float64(amountRUB)
	payment := balance * factor
	if !finite(payment) || payment <= 0 {
		return Schedule{}, fmt.Errorf("%w: monthly payment", ErrArithmetic)
	}
	schedule := Schedule{MonthlyPaymentRUB: payment, Periods: make([]Period, 0, termMonths)}
	for month := 1; month <= termMonths; month++ {
		interest := balance * (annualRate / 12)
		principal := payment - interest
		actualPayment := payment
		closing := balance - principal
		if month == termMonths {
			// Repay the exact remaining float64 principal; never leave a numerical
			// residual debt or round every monthly payment to kopecks.
			principal = balance
			closing = 0
			actualPayment = principal + interest
		}
		if !finite(interest) || !finite(principal) || !finite(actualPayment) || !finite(closing) || principal <= 0 || closing < 0 {
			return Schedule{}, fmt.Errorf("%w: amortization period", ErrArithmetic)
		}
		schedule.Periods = append(schedule.Periods, Period{
			Month: month, OpeningBalanceRUB: balance, PaymentRUB: actualPayment,
			InterestRUB: interest, PrincipalRUB: principal, ClosingBalanceRUB: closing,
		})
		// Sum interest rather than subtracting two almost equal totals at low rates.
		schedule.OverpaymentRUB += interest
		if month == 12 {
			schedule.BalanceAfter12RUB = closing
		}
		balance = closing
	}
	schedule.TotalPaymentsRUB = float64(amountRUB) + schedule.OverpaymentRUB
	if !finite(schedule.TotalPaymentsRUB) {
		return Schedule{}, fmt.Errorf("%w: total payments", ErrArithmetic)
	}
	return schedule, nil
}
