package config

import (
	"math"
	"testing"
)

func TestEveryWholeBasisPointRateStep(t *testing.T) {
	for bp := 1; bp <= 10000; bp++ {
		o := Optimizer{RateStep: float64(bp) / 10000}
		actual, err := o.RateStepBasisPoints()
		if err != nil || actual != bp {
			t.Fatalf("step=%g got %d %v want %d", o.RateStep, actual, err, bp)
		}
	}
	for _, step := range []float64{0, -.0025, .00001, .00251, 1.0001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := (Optimizer{RateStep: step}).RateStepBasisPoints(); err == nil {
			t.Fatalf("invalid step %g accepted", step)
		}
	}
}
