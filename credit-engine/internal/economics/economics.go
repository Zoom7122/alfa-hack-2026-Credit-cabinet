package economics

import (
	"fmt"
	"math"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

// Calculator holds a validated copy of the static profile. Evaluation neither
// changes it nor reads bank state, so one instance may serve concurrent requests.
type Calculator struct {
	parameters  config.Economics
	initialized bool
}

type Result struct {
	Schedule                Schedule
	FTPAnnual               float64
	ReserveRUB              float64
	NetExposureRUB          float64
	RiskWeightedExposureRUB float64
	LongTermExposureRUB     float64
	NPVKopecks              int64
	NPVRUB                  float64
	// RAROC is informational; nil means no allocated-capital denominator.
	RAROC *float64
}

func (r Result) Profitable() bool { return r.NPVKopecks > 0 }

func NewCalculator(parameters config.Economics) (*Calculator, error) {
	if err := parameters.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return &Calculator{parameters: parameters, initialized: true}, nil
}

// Evaluate accepts existing REST terms (annual_rate=24 means 24 percent).
// The same result is intended for MS1 selection and the independent MS2 check.
func (c *Calculator) Evaluate(terms contracts.LoanTerms) (Result, error) {
	if c == nil || !c.initialized {
		return Result{}, fmt.Errorf("%w: use NewCalculator before evaluation", ErrInvalidInput)
	}
	annualRate, err := AnnualRateFromPercent(terms.AnnualRate)
	if err != nil {
		return Result{}, err
	}
	schedule, err := Amortize(terms.AmountRUB, terms.TermMonths, annualRate)
	if err != nil {
		return Result{}, err
	}
	p := c.parameters
	ftp := p.FTPAnnual
	if terms.TermMonths > 24 {
		ftp += p.FTPLongAddition
	}
	reserve := float64(terms.AmountRUB) * p.ReserveRate
	netExposure := float64(terms.AmountRUB) - reserve
	riskWeightedExposure := netExposure * p.RiskWeight
	longTermExposure := 0.0
	if terms.TermMonths > 12 {
		longTermExposure = schedule.BalanceAfter12RUB * (1 - p.ReserveRate)
	}
	if !finite(reserve) || !finite(netExposure) || !finite(riskWeightedExposure) || !finite(longTermExposure) {
		return Result{}, fmt.Errorf("%w: loan exposure", ErrArithmetic)
	}
	npv, preCapitalMarginPV, allocatedCapitalYearsPV := -p.UpfrontCostRUB, -p.UpfrontCostRUB, 0.0
	for _, period := range schedule.Periods {
		balance := period.OpeningBalanceRUB
		income := period.InterestRUB
		funding := balance * (ftp / 12)
		opex := balance * (p.OpexAnnual / 12)
		expectedLoss := balance * p.PDAnnual * p.LGD / 12
		allocatedCapital := balance * (1 - p.ReserveRate) * p.RiskWeight * p.CapitalAllocation
		capitalCost := allocatedCapital * (p.CapitalPriceAnnual / 12)
		marginBeforeCapital := income - funding - opex - expectedLoss
		margin := marginBeforeCapital - capitalCost
		discountFactor := math.Exp(-math.Log1p(p.DiscountAnnual) * float64(period.Month) / 12)
		npv += margin * discountFactor
		preCapitalMarginPV += marginBeforeCapital * discountFactor
		allocatedCapitalYearsPV += (allocatedCapital / 12) * discountFactor
		if !finite(allocatedCapital) || !finite(margin) || !finite(npv) || !finite(preCapitalMarginPV) || !finite(allocatedCapitalYearsPV) {
			return Result{}, fmt.Errorf("%w: discounted cash flow", ErrArithmetic)
		}
	}
	kopecks, err := RoundToKopecks(npv)
	if err != nil {
		return Result{}, fmt.Errorf("NPV: %w", err)
	}
	var raroc *float64
	if allocatedCapitalYearsPV > 0 {
		value := preCapitalMarginPV / allocatedCapitalYearsPV
		if !finite(value) {
			return Result{}, fmt.Errorf("%w: RAROC", ErrArithmetic)
		}
		raroc = &value
	}
	return Result{
		Schedule: schedule, FTPAnnual: ftp, ReserveRUB: reserve, NetExposureRUB: netExposure,
		RiskWeightedExposureRUB: riskWeightedExposure, LongTermExposureRUB: longTermExposure,
		NPVKopecks: kopecks, NPVRUB: float64(kopecks) / 100, RAROC: raroc,
	}, nil
}
