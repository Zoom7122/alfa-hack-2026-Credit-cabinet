package config

import (
	"fmt"
	"math"
	"strings"
)

type Policy struct {
	ID                     string               `json:"id"`
	Economics              Economics            `json:"economics"`
	Optimizer              Optimizer            `json:"optimizer"`
	NormLimits             map[string]NormLimit `json:"norm_limits"`
	MaxQualityCategory     int                  `json:"max_quality_category"`
	LargeExposureThreshold float64              `json:"large_exposure_threshold"`
}

// Economics and Optimizer rates are internal fractions, not REST percentages.
type Economics struct {
	ReserveRate        float64 `json:"reserve_rate"`
	QualityCategory    int     `json:"quality_category"`
	RiskWeight         float64 `json:"risk_weight"`
	PDAnnual           float64 `json:"pd_annual"`
	LGD                float64 `json:"lgd"`
	FTPAnnual          float64 `json:"ftp_annual"`
	FTPLongAddition    float64 `json:"ftp_long_addition"`
	OpexAnnual         float64 `json:"opex_annual"`
	UpfrontCostRUB     float64 `json:"upfront_cost_rub"`
	CapitalAllocation  float64 `json:"capital_allocation"`
	CapitalPriceAnnual float64 `json:"capital_price_annual"`
	DiscountAnnual     float64 `json:"discount_annual"`
}

type Optimizer struct {
	AmountStepRUB   int64   `json:"amount_step_rub"`
	RateStep        float64 `json:"rate_step"`
	MaxTermMonths   int     `json:"max_term_months"`
	ProfitRetention float64 `json:"profit_retention"`
	DSRCap          float64 `json:"dsr_cap"`
}

type NormLimit struct {
	Operator string  `json:"operator"`
	Value    float64 `json:"value"`
}

func LoadPolicy(path string) (Policy, error) {
	var policy Policy
	if err := loadJSON(path, &policy); err != nil {
		return policy, err
	}
	if err := policy.Validate(); err != nil {
		return policy, fmt.Errorf("policy %q: %w", path, err)
	}
	return policy, nil
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("id must not be empty")
	}
	e := p.Economics
	for _, item := range []struct {
		name  string
		value float64
	}{
		{"reserve_rate", e.ReserveRate}, {"pd_annual", e.PDAnnual}, {"lgd", e.LGD}, {"capital_allocation", e.CapitalAllocation},
	} {
		if !finite(item.value) || item.value < 0 || item.value > 1 {
			return fmt.Errorf("economics.%s must be between 0 and 1", item.name)
		}
	}
	for _, item := range []struct {
		name  string
		value float64
	}{
		{"risk_weight", e.RiskWeight}, {"ftp_annual", e.FTPAnnual}, {"ftp_long_addition", e.FTPLongAddition},
		{"opex_annual", e.OpexAnnual}, {"upfront_cost_rub", e.UpfrontCostRUB},
		{"capital_price_annual", e.CapitalPriceAnnual}, {"discount_annual", e.DiscountAnnual},
	} {
		if !finite(item.value) || item.value < 0 {
			return fmt.Errorf("economics.%s must be finite and nonnegative", item.name)
		}
	}
	if e.QualityCategory < 1 || e.QualityCategory > 5 || p.MaxQualityCategory < 1 || p.MaxQualityCategory > 5 {
		return fmt.Errorf("quality categories must be between 1 and 5")
	}
	o := p.Optimizer
	if o.AmountStepRUB <= 0 {
		return fmt.Errorf("optimizer.amount_step_rub must be positive")
	}
	if !finite(o.RateStep) || o.RateStep <= 0 || o.RateStep > 1 || math.Trunc(o.RateStep*10000) != o.RateStep*10000 {
		return fmt.Errorf("optimizer.rate_step must be a positive whole number of basis points, at most 1")
	}
	if o.MaxTermMonths < 1 || o.MaxTermMonths > 36 {
		return fmt.Errorf("optimizer.max_term_months must be between 1 and 36")
	}
	if !finite(o.ProfitRetention) || o.ProfitRetention <= 0 || o.ProfitRetention > 1 {
		return fmt.Errorf("optimizer.profit_retention must be in (0, 1]")
	}
	if !finite(o.DSRCap) || o.DSRCap <= 0 || o.DSRCap > 1 {
		return fmt.Errorf("optimizer.dsr_cap must be in (0, 1]")
	}
	if !finite(p.LargeExposureThreshold) || p.LargeExposureThreshold <= 0 || p.LargeExposureThreshold > 1 {
		return fmt.Errorf("large_exposure_threshold must be in (0, 1]")
	}
	expected := map[string]string{"N1_1": ">=", "N1_2": ">=", "N1_0": ">=", "N2": ">=", "N3": ">=", "N4": "<=", "N6": "<=", "N7": "<=", "N25": "<=", "N12": "<="}
	if len(p.NormLimits) != len(expected) {
		return fmt.Errorf("norm_limits must contain all ten supported norms")
	}
	for name, operator := range expected {
		limit, ok := p.NormLimits[name]
		if !ok || limit.Operator != operator || !finite(limit.Value) || limit.Value < 0 {
			return fmt.Errorf("invalid norm_limits.%s", name)
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
