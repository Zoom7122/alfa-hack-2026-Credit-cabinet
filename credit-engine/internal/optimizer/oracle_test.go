package optimizer

import (
	"math"
	"math/big"
	"reflect"
	"sort"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

// exhaustive enumerates every grid amount and both exact endpoints, instead of
// deriving a single P_cap. It shares the required financial calculator, but not
// the production amount, grid, retention or ranking helpers.
func exhaustive(t *testing.T, p config.Policy, in Input) Result {
	t.Helper()
	calculator, err := economics.NewCalculator(p.Economics)
	if err != nil {
		t.Fatal(err)
	}
	upper := *in.RequestedTerms.AmountRUB
	if need := *in.Need.ConfirmedCostRUB - *in.Need.OwnContributionRUB; need < upper {
		upper = need
	}
	minimum := *in.RequestedTerms.MinAcceptableAmountRUB
	if upper < minimum {
		return Result{}
	}
	amounts := []int64{minimum}
	for a := p.Optimizer.AmountStepRUB; a <= upper; a += p.Optimizer.AmountStepRUB {
		if a >= minimum {
			amounts = append(amounts, a)
		}
	}
	amounts = append(amounts, upper)
	var all []candidate
	maximumRate, err := economics.BasisPointsFromPercent(*in.RequestedTerms.MaxAnnualRate)
	if err != nil {
		t.Fatal(err)
	}
	step := int(math.Round(p.Optimizer.RateStep * 10000))
	var rates []int
	for bp := 0; bp <= maximumRate; bp++ {
		if bp%step == 0 || bp == maximumRate {
			rates = append(rates, bp)
		}
	}
	feasiblePairs := 0
	for n := 1; n <= *in.RequestedTerms.MaxAcceptableTermMonths && n <= p.Optimizer.MaxTermMonths; n++ {
		for _, bp := range rates {
			found := false
			for _, amount := range amounts {
				monthlyRate := float64(bp) / 120000
				payment := float64(amount) / float64(n)
				if bp != 0 {
					growth := math.Pow(1+monthlyRate, float64(n))
					payment = float64(amount) * monthlyRate * growth / (growth - 1)
				}
				affordable := true
				for month := 0; month < n; month++ {
					if payment+in.FutureDebtPaymentsRUB[month] > p.Optimizer.DSRCap*in.AnnualCFADSRUB/12 {
						affordable = false
						break
					}
				}
				if !affordable {
					continue
				}
				terms := contracts.LoanTerms{AmountRUB: amount, TermMonths: n, AnnualRate: float64(bp) / 100}
				econ, err := calculator.Evaluate(terms)
				if err != nil {
					t.Fatal(err)
				}
				if econ.NPVKopecks <= 0 {
					continue
				}
				found = true
				paymentCents, err := economics.RoundToKopecks(econ.Schedule.MonthlyPaymentRUB)
				if err != nil {
					t.Fatal(err)
				}
				costCents, err := economics.RoundToKopecks(econ.Schedule.OverpaymentRUB)
				if err != nil {
					t.Fatal(err)
				}
				all = append(all, candidate{terms: terms, npv: econ.NPVKopecks, payment: paymentCents, overpayment: costCents, rawPayment: econ.Schedule.MonthlyPaymentRUB, rawOverpayment: econ.Schedule.OverpaymentRUB})
			}
			if found {
				feasiblePairs++
			}
		}
	}
	if len(all) == 0 {
		return Result{}
	}
	largest := int64(0)
	for _, c := range all {
		if c.terms.AmountRUB > largest {
			largest = c.terms.AmountRUB
		}
	}
	maxNPV := int64(0)
	for _, c := range all {
		if c.terms.AmountRUB == largest && c.npv > maxNPV {
			maxNPV = c.npv
		}
	}
	var eligible []candidate
	samePairs := make(map[contracts.LoanTerms]bool)
	for _, c := range all {
		if c.terms.AmountRUB != largest {
			continue
		}
		samePairs[c.terms] = true
		// Test scenarios use a retention percentage with at most three digits.
		if c.npv*1000 >= int64(math.Round(p.Optimizer.ProfitRetention*1000))*maxNPV {
			eligible = append(eligible, c)
		}
	}
	preferred := *in.RequestedTerms.TermMonths
	distance := func(term int) int {
		if term < preferred {
			return preferred - term
		}
		return term - preferred
	}
	sort.Slice(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		left := [5]float64{float64(a.overpayment), float64(distance(a.terms.TermMonths)), float64(a.payment), a.terms.AnnualRate, float64(a.terms.TermMonths)}
		right := [5]float64{float64(b.overpayment), float64(distance(b.terms.TermMonths)), float64(b.payment), b.terms.AnnualRate, float64(b.terms.TermMonths)}
		for k := range left {
			if left[k] != right[k] {
				return left[k] < right[k]
			}
		}
		return false
	})
	best := eligible[0]
	return Result{Terms: &best.terms, Audit: contracts.SelectionAudit{FeasibleCandidateCount: feasiblePairs, SameAmountCandidateCount: len(samePairs), MaxNPVRUB: float64(maxNPV) / 100, SelectedNPVRUB: float64(best.npv) / 100, ProfitRetention: p.Optimizer.ProfitRetention, MonthlyPaymentRUB: best.rawPayment, OverpaymentRUB: best.rawOverpayment}, Reasons: []contracts.Reason{}}
}

func TestAgainstExhaustiveAmountSearch(t *testing.T) {
	for _, c := range []struct {
		name                    string
		annualCFADS, rate       float64
		amount, minimum, step   int64
		term, maxTerm           int
		retention, dsr, upfront float64
		debts                   [36]float64
	}{
		{"exact upper", 240000, 30, 90123, 12345, 10000, 6, 8, .9, .75, 0, [36]float64{}},
		{"constrained grid", 120000, 30, 150000, 12345, 10000, 6, 8, .9, .75, 0, [36]float64{1000, 2000, 0, 500}},
		{"upfront expense", 120000, 30, 150000, 12345, 10000, 6, 8, .9, .75, 500, [36]float64{1000, 2000}},
		{"no profit", 120000, 10, 150000, 12345, 10000, 6, 8, .9, .75, 0, [36]float64{}},
		{"custom policy", 240000, 30.13, 150000, 12345, 20000, 6, 8, .85, .6, 100, [36]float64{2000, 1000, 3000}},
		{"long term FTP", 36000, 30, 150000, 12345, 10000, 24, 36, .9, .75, 100, [36]float64{0, 500}},
		{"penny cost ties", 240000, 24, 1, 1, 10000, 6, 10, .9, .75, 0, [36]float64{}},
		{"minimum and principal too low", 240000, 24, 1, 1, 10000, 6, 10, .9, .75, 100, [36]float64{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := policy(t)
			p.Optimizer.RateStep = .025
			p.Optimizer.AmountStepRUB = c.step
			p.Optimizer.ProfitRetention = c.retention
			p.Optimizer.DSRCap = c.dsr
			p.Economics.UpfrontCostRUB = c.upfront
			in := Input{RequestedTerms: contracts.CreditRequest{AmountRUB: &c.amount, MinAcceptableAmountRUB: &c.minimum, TermMonths: &c.term, MaxAcceptableTermMonths: &c.maxTerm, MaxAnnualRate: &c.rate}, Need: contracts.Need{ConfirmedCostRUB: &c.amount, OwnContributionRUB: ptr(int64(0))}, AnnualCFADSRUB: c.annualCFADS, FutureDebtPaymentsRUB: c.debts}
			got := selectOK(t, selector(t, p), in)
			want := exhaustive(t, p, in)
			if want.Terms == nil {
				assertNoOffer(t, got)
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("optimized=%+v terms=%+v; exhaustive=%+v terms=%+v", got, got.Terms, want, want.Terms)
			}
		})
	}
}

func TestExactMinimumEndToEndAndZeroNPV(t *testing.T) {
	p := policy(t)
	s := selector(t, p)
	in := simpleInput()
	in.RequestedTerms.AmountRUB = ptr(int64(100000))
	in.RequestedTerms.MinAcceptableAmountRUB = ptr(int64(12345))
	in.RequestedTerms.TermMonths = ptr(1)
	in.RequestedTerms.MaxAcceptableTermMonths = ptr(1)
	in.RequestedTerms.MaxAnnualRate = ptr(20.09)
	factor, err := economics.AnnuityFactor(1, .2009)
	if err != nil {
		t.Fatal(err)
	}
	in.AnnualCFADSRUB = (12345*factor + 1) * 12 / p.Optimizer.DSRCap
	r := selectOK(t, s, in)
	if r.Terms == nil || r.Terms.AmountRUB != 12345 {
		t.Fatalf("exact minimum dropped: %+v", r)
	}
	in.RequestedTerms.MaxAnnualRate = ptr(20.08)
	assertNoOffer(t, selectOK(t, s, in))
}

func TestConcurrentSelectionAndIndependentResults(t *testing.T) {
	p := policy(t)
	s := selector(t, p)
	in := simpleInput()
	before := in
	want := selectOK(t, s, in)
	results := make(chan Result, 16)
	for i := 0; i < 16; i++ {
		go func() {
			r, err := s.Select(in)
			if err != nil {
				t.Error(err)
			}
			results <- r
		}()
	}
	for i := 0; i < 16; i++ {
		if got := <-results; !reflect.DeepEqual(got, want) {
			t.Fatalf("inconsistent result %+v", got)
		}
	}
	if !reflect.DeepEqual(in, before) {
		t.Fatal("input changed")
	}
	want.Terms.AmountRUB = 1
	want.Reasons = append(want.Reasons, contracts.Reason{Code: "MUTATED"})
	got := selectOK(t, s, in)
	if got.Terms.AmountRUB != 3000000 || len(got.Reasons) != 0 {
		t.Fatal("result shared state")
	}
	p.Optimizer.AmountStepRUB = 1
	p.Economics.UpfrontCostRUB = math.MaxFloat64
	if got := selectOK(t, s, in); !reflect.DeepEqual(got.Terms, ptr(contracts.LoanTerms{AmountRUB: 3000000, TermMonths: 22, AnnualRate: 24})) {
		t.Fatal("caller policy mutation affected selector")
	}
	// Reading the retention rational during selection must not mutate it.
	if s.retention.Cmp(big.NewRat(9, 10)) != 0 {
		t.Fatal("retention changed")
	}
}
