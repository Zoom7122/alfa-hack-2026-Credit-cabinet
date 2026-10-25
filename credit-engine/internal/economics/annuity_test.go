package economics_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

func closeTo(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsInf(got, 0) || math.Abs(got-want) > tolerance {
		t.Fatalf("%s: got %.15g, want %.15g (tolerance %g)", label, got, want, tolerance)
	}
}

func TestAnnuityValues(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		amount                                int64
		months                                int
		rate, payment, overpayment, balance12 float64
	}{
		{"zero rate", 3000000, 24, 0, 125000, 0, 1500000},
		{"one month", 1200, 1, 0.24, 1224, 24, 0},
		{"fixed bank deal", 3000000, 24, 0.24, 158613.29175974967, 806719.0022339921, 1677389.6825323158},
		{"selected extension", 3000000, 24, 0.2375, 158239.20719439682, 797740.9726655239, 1675576.7086581055},
		{"selected 22 months", 3000000, 22, 0.24, 169894.2015821492, 737672.4348072825, 1526089.1077791578},
		{"maximum rate", 1200, 1, 1, 1300, 100, 0},
		{"full repayment at 12", 1200, 12, 0, 100, 0, 0},
		{"first long term month", 1300, 13, 0, 100, 0, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := economics.Amortize(tc.amount, tc.months, tc.rate)
			if err != nil {
				t.Fatal(err)
			}
			closeTo(t, "payment", s.MonthlyPaymentRUB, tc.payment, 0.01)
			closeTo(t, "overpayment", s.OverpaymentRUB, tc.overpayment, 0.01)
			closeTo(t, "balance after 12", s.BalanceAfter12RUB, tc.balance12, 0.01)
			if len(s.Periods) != tc.months || s.Periods[len(s.Periods)-1].ClosingBalanceRUB != 0 {
				t.Fatal("loan must be fully repaid at maturity")
			}
		})
	}
}

func TestAmortizationConservation(t *testing.T) {
	for _, amount := range []int64{1, 10001, 3000000} {
		for months := 1; months <= 36; months++ {
			for _, rate := range []float64{0, 0.0001, 0.2375, 1} {
				s, err := economics.Amortize(amount, months, rate)
				if err != nil {
					t.Fatalf("amount=%d months=%d rate=%v: %v", amount, months, rate, err)
				}
				balance, totalPrincipal, totalInterest, totalPayment := float64(amount), 0.0, 0.0, 0.0
				for index, row := range s.Periods {
					if row.Month != index+1 || row.PrincipalRUB <= 0 || row.InterestRUB < 0 || row.ClosingBalanceRUB < 0 || row.ClosingBalanceRUB >= row.OpeningBalanceRUB {
						t.Fatalf("invalid amortization row: %+v", row)
					}
					closeTo(t, "opening balance", row.OpeningBalanceRUB, balance, 1e-8)
					closeTo(t, "payment split", row.PaymentRUB, row.PrincipalRUB+row.InterestRUB, 1e-8)
					closeTo(t, "closing balance", row.ClosingBalanceRUB, balance-row.PrincipalRUB, 1e-8)
					closeTo(t, "annuity payment", row.PaymentRUB, s.MonthlyPaymentRUB, 1e-7)
					totalPrincipal += row.PrincipalRUB
					totalInterest += row.InterestRUB
					totalPayment += row.PaymentRUB
					balance = row.ClosingBalanceRUB
				}
				closeTo(t, "principal conservation", totalPrincipal, float64(amount), 1e-7)
				closeTo(t, "interest conservation", totalInterest, s.OverpaymentRUB, 1e-8)
				closeTo(t, "total payments", totalPayment, s.TotalPaymentsRUB, 1e-7)
				if rate == 0 && s.OverpaymentRUB != 0 {
					t.Fatal("zero-rate overpayment must be exactly zero")
				}
			}
		}
	}
}

func TestAnnuityNearZeroAndInvalidInput(t *testing.T) {
	for _, rate := range []float64{math.SmallestNonzeroFloat64, 1e-300, 1e-14} {
		factor, err := economics.AnnuityFactor(36, rate)
		if err != nil {
			t.Fatal(err)
		}
		closeTo(t, "near-zero annuity factor", factor, 1.0/36, 1e-14)
	}
	for _, tc := range []struct {
		amount int64
		months int
		rate   float64
	}{
		{0, 24, 0.24}, {-1, 24, 0.24}, {1, 0, 0}, {1, -1, 0}, {1, 37, 0},
		{1, 1, -0.01}, {1, 1, 1.01}, {1, 1, math.NaN()}, {1, 1, math.Inf(1)}, {1, 1, math.Inf(-1)},
	} {
		if _, err := economics.Amortize(tc.amount, tc.months, tc.rate); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("invalid amortization input %+v: %v", tc, err)
		}
	}
}
