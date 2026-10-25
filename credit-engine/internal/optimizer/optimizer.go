// Package optimizer selects one loan on the approved finite grid. It neither
// reads bank state nor changes the borrower's score or sends a deal for issue.
package optimizer

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

// Input comes from a complete scoring assessment on RouteSelection. Debt is
// already validated and ordered, starting in the next calendar month; the fixed
// array is not a replacement for validating missing raw periods in scoring.
type Input struct {
	RequestedTerms        contracts.CreditRequest
	Need                  contracts.Need
	AnnualCFADSRUB        float64
	FutureDebtPaymentsRUB [36]float64
}

// A nil Terms with NO_FEASIBLE_OFFER requests manual review. A selected Terms
// still requires the independent bank check before it can become APPROVED.
type Result struct {
	Terms   *contracts.LoanTerms
	Audit   contracts.SelectionAudit
	Reasons []contracts.Reason
}

type Selector struct {
	settings   config.Optimizer
	calculator *economics.Calculator
	rateStepBP int
	retention  *big.Rat
}

// New stores only immutable copies of static selection/economics parameters.
func New(policy config.Policy) (*Selector, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("optimizer: %w: %v", economics.ErrInvalidInput, err)
	}
	calculator, err := economics.NewCalculator(policy.Economics)
	if err != nil {
		return nil, err
	}
	step, err := policy.Optimizer.RateStepBasisPoints()
	if err != nil {
		return nil, err
	}
	retention, ok := new(big.Rat).SetString(strconv.FormatFloat(policy.Optimizer.ProfitRetention, 'f', -1, 64))
	if !ok {
		return nil, fmt.Errorf("optimizer: %w: profit retention", economics.ErrInvalidInput)
	}
	return &Selector{settings: policy.Optimizer, calculator: calculator, rateStepBP: step, retention: retention}, nil
}

func (s *Selector) Select(in Input) (Result, error) {
	if s == nil || s.calculator == nil {
		return Result{}, fmt.Errorf("optimizer: %w: use New before selection", economics.ErrInvalidInput)
	}
	maxRate, err := validate(in)
	if err != nil {
		return Result{}, err
	}
	upper := min(*in.RequestedTerms.AmountRUB, *in.Need.ConfirmedCostRUB-*in.Need.OwnContributionRUB)
	minimum := *in.RequestedTerms.MinAcceptableAmountRUB
	if upper < minimum {
		return s.noOffer(), nil
	}
	maxTerm := min(*in.RequestedTerms.MaxAcceptableTermMonths, s.settings.MaxTermMonths)
	rates := rateGrid(maxRate, s.rateStepBP)
	var candidates []candidate
	paymentCap := math.Inf(1)
	basePaymentCap := s.settings.DSRCap * in.AnnualCFADSRUB / 12
	for term := 1; term <= maxTerm; term++ {
		paymentCap = min(paymentCap, basePaymentCap-in.FutureDebtPaymentsRUB[term-1])
		if paymentCap <= 0 {
			break
		} // Every later prefix contains this same month.
		for _, bp := range rates {
			factor, err := economics.AnnuityFactor(term, float64(bp)/10000)
			if err != nil {
				return Result{}, err
			}
			amount := maxAmount(upper, minimum, s.settings.AmountStepRUB, paymentCap, factor)
			if amount == 0 {
				continue
			}
			terms := contracts.LoanTerms{AmountRUB: amount, TermMonths: term, AnnualRate: float64(bp) / 100}
			calculated, err := s.calculator.Evaluate(terms)
			if err != nil {
				return Result{}, fmt.Errorf("optimizer economics: %w", err)
			}
			if !calculated.Profitable() {
				continue
			}
			payment, err := economics.RoundToKopecks(calculated.Schedule.MonthlyPaymentRUB)
			if err != nil {
				return Result{}, err
			}
			overpayment, err := economics.RoundToKopecks(calculated.Schedule.OverpaymentRUB)
			if err != nil {
				return Result{}, err
			}
			candidates = append(candidates, candidate{terms: terms, npv: calculated.NPVKopecks, payment: payment, overpayment: overpayment, rawPayment: calculated.Schedule.MonthlyPaymentRUB, rawOverpayment: calculated.Schedule.OverpaymentRUB})
		}
	}
	return s.rank(candidates, *in.RequestedTerms.TermMonths), nil
}

func validate(in Input) (int, error) {
	c, n := in.RequestedTerms, in.Need
	bad := func(field string) (int, error) {
		return 0, fmt.Errorf("optimizer: %w: %s", economics.ErrInvalidInput, field)
	}
	if c.AmountRUB == nil || c.TermMonths == nil || c.MaxAnnualRate == nil || c.MinAcceptableAmountRUB == nil || c.MaxAcceptableTermMonths == nil {
		return bad("complete credit terms are required")
	}
	if n.ConfirmedCostRUB == nil || n.OwnContributionRUB == nil {
		return bad("confirmed need is required")
	}
	if *c.AmountRUB <= 0 || *c.MinAcceptableAmountRUB <= 0 || *c.MinAcceptableAmountRUB > *c.AmountRUB {
		return bad("amount bounds")
	}
	if *c.TermMonths < 1 || *c.MaxAcceptableTermMonths < *c.TermMonths {
		return bad("term bounds")
	}
	if *n.ConfirmedCostRUB < 0 || *n.OwnContributionRUB < 0 {
		return bad("need amounts")
	}
	if !finite(in.AnnualCFADSRUB) {
		return bad("finite CFADS is required")
	}
	for _, debt := range in.FutureDebtPaymentsRUB {
		if !finite(debt) || debt < 0 {
			return bad("future debt payments")
		}
	}
	return economics.BasisPointsFromPercent(*c.MaxAnnualRate)
}

func rateGrid(maximum, step int) []int {
	rates := make([]int, 0, maximum/step+2)
	for bp := 0; bp <= maximum; bp += step {
		rates = append(rates, bp)
	}
	if rates[len(rates)-1] != maximum {
		rates = append(rates, maximum)
	}
	return rates
}

// maxAmount preserves exact client endpoints. Internal grid values are floored
// in integer rubles; the float capacity is never cast outside int64's range.
func maxAmount(upper, minimum, step int64, paymentCap, factor float64) int64 {
	fits := func(amount int64) bool { return float64(amount)*factor <= paymentCap }
	if upper < minimum || !fits(minimum) {
		return 0
	}
	if fits(upper) {
		return upper
	}
	capacity := paymentCap / factor
	whole, _ := new(big.Float).SetFloat64(capacity).Int(nil)
	// fits(upper) was false. Clamp before converting even if division rounded
	// a capacity at MaxInt64 up to 2^63.
	if whole.Cmp(big.NewInt(upper)) > 0 {
		whole.SetInt64(upper)
	}
	amount := whole.Int64() / step * step
	// Check the original payment inequality, without epsilon. This resolves
	// division rounding exactly on a grid point in either direction.
	for amount >= minimum && !fits(amount) {
		amount -= step
	}
	for amount <= upper-step && fits(amount+step) {
		amount += step
	}
	return max(amount, minimum)
}

func (s *Selector) noOffer() Result {
	return Result{Audit: contracts.SelectionAudit{ProfitRetention: s.settings.ProfitRetention}, Reasons: []contracts.Reason{{Code: "NO_FEASIBLE_OFFER", Message: "В допустимых границах суммы, срока и ставки нет посильного предложения с положительным NPV.", Fields: []string{}}}}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
