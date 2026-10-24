package optimizer

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func option(amount int64, term int, rate float64, npv, payment, cost int64) candidate {
	return candidate{terms: contracts.LoanTerms{AmountRUB: amount, TermMonths: term, AnnualRate: rate}, npv: npv, payment: payment, overpayment: cost, rawPayment: float64(payment) / 100, rawOverpayment: float64(cost) / 100}
}
func TestRankingExternalCases(t *testing.T) {
	var data struct {
		Cases []struct {
			Name           string
			Max            float64   `json:"max_npv"`
			Eligible       []float64 `json:"eligible_npvs"`
			Excluded       []float64 `json:"excluded_npvs"`
			Amounts        []int64   `json:"candidate_amounts"`
			NPVs           []float64 `json:"candidate_npvs"`
			ExpectedAmount int64     `json:"expected_amount"`
			Preferred      int       `json:"preferred_term"`
			Terms          []int
			ExpectedTerm   int `json:"expected_term"`
		} `json:"ranking_cases"`
	}
	b, err := os.ReadFile("../../testdata/unit_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	s := selector(t, policy(t))
	if len(data.Cases) != 3 {
		t.Fatal(len(data.Cases))
	}
	for _, c := range data.Cases {
		t.Run(c.Name, func(t *testing.T) {
			switch c.Name {
			case "90_percent_inclusive":
				for _, n := range c.Eligible {
					if !s.retainsProfit(int64(math.Round(n*100)), int64(math.Round(c.Max*100))) {
						t.Errorf("%g excluded", n)
					}
				}
				for _, n := range c.Excluded {
					if s.retainsProfit(int64(math.Round(n*100)), int64(math.Round(c.Max*100))) {
						t.Errorf("%g included", n)
					}
				}
			case "coverage_before_profit":
				var candidates []candidate
				for i, amount := range c.Amounts {
					candidates = append(candidates, option(amount, 24, 24, int64(c.NPVs[i]*100), 10000, 10000))
				}
				r := s.rank(candidates, 24)
				if r.Terms.AmountRUB != c.ExpectedAmount {
					t.Fatal(r)
				}
			case "equal_cost_preferred_term":
				var candidates []candidate
				for _, term := range c.Terms {
					candidates = append(candidates, option(3000000, term, 24, 10000, 10000, 10000))
				}
				r := s.rank(candidates, c.Preferred)
				if r.Terms.TermMonths != c.ExpectedTerm {
					t.Fatal(r)
				}
			}
		})
	}
}

func TestRetentionAndEveryTieBreaker(t *testing.T) {
	s := selector(t, policy(t))
	for _, c := range []struct {
		name                string
		options             []candidate
		preferred, wantTerm int
		wantRate            float64
	}{
		{"inclusive 90 percent", []candidate{option(3000000, 24, 24, 100000, 20000, 20000), option(3000000, 22, 24, 90000, 19000, 10000), option(3000000, 21, 24, 89999, 18000, 9000)}, 24, 22, 24},
		{"closer term", []candidate{option(3000000, 23, 24, 100000, 20000, 20000), option(3000000, 24, 24, 90000, 21000, 20000)}, 24, 24, 24},
		{"lower rounded payment", []candidate{option(3000000, 23, 24, 100000, 20001, 20000), option(3000000, 25, 24, 90000, 20000, 20000)}, 24, 25, 24},
		{"lower rate", []candidate{option(3000000, 23, 24, 100000, 20000, 20000), option(3000000, 25, 23.75, 90000, 20000, 20000)}, 24, 25, 23.75},
		{"shorter term", []candidate{option(3000000, 23, 24, 100000, 20000, 20000), option(3000000, 25, 24, 90000, 20000, 20000)}, 24, 23, 24},
		{"desired term outside horizon", []candidate{option(3000000, 23, 24, 100000, 20000, 20000), option(3000000, 25, 24, 90000, 20000, 20000)}, math.MaxInt, 25, 24},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := s.rank(c.options, c.preferred)
			if want.Terms.TermMonths != c.wantTerm || want.Terms.AnnualRate != c.wantRate {
				t.Fatalf("%+v", want)
			}
			rng := rand.New(rand.NewSource(5))
			for i := 0; i < 24; i++ {
				permuted := append([]candidate(nil), c.options...)
				rng.Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
				got := s.rank(permuted, c.preferred)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("order-dependent choice: %+v vs %+v", got, want)
				}
			}
		})
	}
	if !s.retainsProfit(900000000000000000, 1000000000000000000) || s.retainsProfit(899999999999999999, 1000000000000000000) {
		t.Fatal("overflow or floating-point retention comparison")
	}
	for _, retention := range []float64{1, .75, .333} {
		p := policy(t)
		p.Optimizer.ProfitRetention = retention
		s = selector(t, p)
		if !s.retainsProfit(int64(retention*1000), 1000) || s.retainsProfit(int64(retention*1000)-1, 1000) {
			t.Fatalf("retention=%g", retention)
		}
	}
}
