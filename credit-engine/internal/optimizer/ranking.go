package optimizer

import (
	"math/big"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

// npv, payment and overpayment are integer kopecks. Unrounded schedule values
// are retained separately for the existing SelectionAudit contract.
type candidate struct {
	terms                      contracts.LoanTerms
	npv, payment, overpayment  int64
	rawPayment, rawOverpayment float64
}

func (s *Selector) rank(candidates []candidate, preferredTerm int) Result {
	if len(candidates) == 0 {
		return s.noOffer()
	}
	amount := int64(0)
	for _, c := range candidates {
		amount = max(amount, c.terms.AmountRUB)
	}
	maxNPV, sameAmount := int64(0), 0
	for _, c := range candidates {
		if c.terms.AmountRUB == amount {
			sameAmount++
			maxNPV = max(maxNPV, c.npv)
		}
	}
	var selected *candidate
	for i := range candidates {
		c := &candidates[i]
		if c.terms.AmountRUB != amount || !s.retainsProfit(c.npv, maxNPV) {
			continue
		}
		if selected == nil || cheaper(*c, *selected, preferredTerm) {
			selected = c
		}
	}
	terms := selected.terms
	return Result{Terms: &terms, Audit: contracts.SelectionAudit{
		FeasibleCandidateCount: len(candidates), SameAmountCandidateCount: sameAmount,
		MaxNPVRUB: float64(maxNPV) / 100, SelectedNPVRUB: float64(selected.npv) / 100, ProfitRetention: s.settings.ProfitRetention,
		MonthlyPaymentRUB: selected.rawPayment, OverpaymentRUB: selected.rawOverpayment,
	}, Reasons: []contracts.Reason{}}
}

func (s *Selector) retainsProfit(npv, maximum int64) bool {
	// For the configured 90% this is exactly 10*npv >= 9*maximum. big.Int
	// keeps the comparison exact when either int64 product would overflow.
	left := new(big.Int).Mul(big.NewInt(npv), s.retention.Denom())
	right := new(big.Int).Mul(big.NewInt(maximum), s.retention.Num())
	return left.Cmp(right) >= 0
}

func cheaper(a, b candidate, preferredTerm int) bool {
	if a.overpayment != b.overpayment {
		return a.overpayment < b.overpayment
	}
	// Terms are positive, so these distances stay within int even if the
	// client's preferred term is greater than the configured search horizon.
	distance := func(term int) int {
		if term > preferredTerm {
			return term - preferredTerm
		}
		return preferredTerm - term
	}
	if da, db := distance(a.terms.TermMonths), distance(b.terms.TermMonths); da != db {
		return da < db
	}
	if a.payment != b.payment {
		return a.payment < b.payment
	}
	if a.terms.AnnualRate != b.terms.AnnualRate {
		return a.terms.AnnualRate < b.terms.AnnualRate
	}
	return a.terms.TermMonths < b.terms.TermMonths
}
