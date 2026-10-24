package economics_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

func TestPercentRates(t *testing.T) {
	for _, tc := range []struct {
		percent     float64
		basisPoints int
		fraction    float64
	}{
		{0, 0, 0}, {1, 100, 0.01}, {100, 10000, 1}, {23.75, 2375, 0.2375},
		{0.01, 1, 0.0001}, {0.24, 24, 0.0024}, {19.99, 1999, 0.1999}, {1.15, 115, 0.0115}, {99.99, 9999, 0.9999},
	} {
		points, err := economics.BasisPointsFromPercent(tc.percent)
		if err != nil || points != tc.basisPoints {
			t.Fatalf("rate %v: bp=%d, err=%v", tc.percent, points, err)
		}
		rate, err := economics.AnnualRateFromPercent(tc.percent)
		if err != nil || rate != tc.fraction {
			t.Fatalf("rate %v: fraction=%v, err=%v", tc.percent, rate, err)
		}
	}
	// Every hundredth of a percentage point in the agreed range is valid.
	for bp := 0; bp <= 10000; bp++ {
		got, err := economics.BasisPointsFromPercent(float64(bp) / 100)
		if err != nil || got != bp {
			t.Fatalf("basis-point round trip %d: got %d, err=%v", bp, got, err)
		}
	}
	for _, percent := range []float64{-0.01, 100.01, math.Nextafter(100, math.Inf(1)), 0.001, 23.751, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := economics.AnnualRateFromPercent(percent); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("invalid rate %v: %v", percent, err)
		}
	}
}

func TestRoundToKopecks(t *testing.T) {
	for _, tc := range []struct {
		rub  float64
		want int64
	}{
		{0, 0}, {1, 100}, {0.0049, 0}, {0.005, 1}, {0.0051, 1}, {-0.0049, 0}, {-0.005, -1}, {-0.0051, -1},
		{1.005, 101}, {-1.005, -101}, {1.125, 113}, {-1.125, -113}, {2.675, 268}, {118945.54, 11894554},
		{math.Nextafter(0.005, 0), 0}, {math.Nextafter(0.005, 1), 1}, {9e16, 9000000000000000000}, {-9e16, -9000000000000000000},
	} {
		got, err := economics.RoundToKopecks(tc.rub)
		if err != nil || got != tc.want {
			t.Fatalf("round %v: got %d, want %d, err=%v", tc.rub, got, tc.want, err)
		}
	}
	for _, rub := range []float64{1e17, -1e17, math.MaxFloat64, -math.MaxFloat64} {
		if _, err := economics.RoundToKopecks(rub); !errors.Is(err, economics.ErrAmountOverflow) {
			t.Fatalf("overflow %v: %v", rub, err)
		}
	}
	for _, rub := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := economics.RoundToKopecks(rub); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("non-finite money: %v", err)
		}
	}
}
