// Package bank owns MS2's in-memory state and operations. Norms are the agreed
// educational MVP model, computed from ruble amounts before any formatting.
package bank

import (
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

type Projector struct {
	policy     config.Policy
	calculator *economics.Calculator
}
type Projection struct {
	State     contracts.BankState
	Checks    []contracts.CheckResult
	Economics economics.Result
	Issued    bool
}

func NewProjector(p config.Policy) (*Projector, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	owned, err := copyJSON(p)
	if err != nil {
		return nil, err
	}
	c, err := economics.NewCalculator(p.Economics)
	if err != nil {
		return nil, err
	}
	return &Projector{policy: owned, calculator: c}, nil
}

func (p *Projector) Project(before contracts.BankState, applicationID, borrowerID string, terms contracts.LoanTerms) (Projection, error) {
	if p == nil || p.calculator == nil {
		return Projection{}, fmt.Errorf("bank projector is not initialized")
	}
	if err := config.ValidateBankState(before); err != nil {
		return Projection{}, err
	}
	if applicationID == "" || borrowerID == "" {
		return Projection{}, &Error{Status: 400, Code: "MISSING_ENVELOPE"}
	}
	if slices.Contains(before.IssuedApplicationIds, applicationID) {
		return Projection{}, conflict("APPLICATION_CONFLICT")
	}
	oldNorms := norms(before, p.policy.LargeExposureThreshold)
	for code, value := range oldNorms {
		if value == nil {
			return Projection{}, fmt.Errorf("invalid initial bank norm %s", code)
		}
	}
	econ, err := p.calculator.Evaluate(terms)
	if err != nil {
		return Projection{}, err
	}
	after := copyState(before)
	reserve, cost, principal := econ.ReserveRUB, p.policy.Economics.UpfrontCostRUB, float64(terms.AmountRUB)
	after.Reserves += reserve
	after.K1 -= reserve + cost
	after.Ka -= reserve + cost
	after.K0 -= reserve + cost
	after.RWA11 += econ.RiskWeightedExposureRUB
	after.RWA12 += econ.RiskWeightedExposureRUB
	after.RWA10 += econ.RiskWeightedExposureRUB
	after.Cash -= principal + cost
	after.Lam -= principal + cost
	after.Lat -= principal + cost
	after.Krd += econ.LongTermExposureRUB
	group, exists := after.BorrowerGroups[borrowerID]
	if !exists {
		group = borrowerID
	}
	after.Groups[group] += econ.NetExposureRUB
	newNorms := norms(after, p.policy.LargeExposureThreshold)
	checks := make([]contracts.CheckResult, 0, 15)
	for _, code := range normOrder {
		limit := p.policy.NormLimits[code]
		passed := newNorms[code] != nil && comparison(*newNorms[code], limit.Operator, limit.Value)
		checks = append(checks, contracts.CheckResult{Code: code, Before: oldNorms[code], After: newNorms[code], Operator: ptr(limit.Operator), Limit: ptr(limit.Value), Passed: passed, Detail: normDetail(code)})
	}
	checks = append(checks,
		contracts.CheckResult{Code: "CASH_MIN", Before: number(before.Cash), After: number(after.Cash), Operator: ptr(">="), Limit: ptr(after.CashMin), Passed: finite(after.Cash) && after.Cash >= after.CashMin, Detail: "Свободные средства после выдачи не ниже внутреннего минимума."},
		contracts.CheckResult{Code: "NPV", After: ptr(econ.NPVRUB), Operator: ptr(">"), Limit: ptr(0.0), Passed: econ.Profitable(), Detail: "NPV после однократного округления до копейки строго положителен."},
		contracts.CheckResult{Code: "QUALITY", Before: ptr(float64(p.policy.Economics.QualityCategory)), After: ptr(float64(p.policy.Economics.QualityCategory)), Operator: ptr("<="), Limit: ptr(float64(p.policy.MaxQualityCategory)), Passed: p.policy.Economics.QualityCategory <= p.policy.MaxQualityCategory, Detail: "Категория качества не хуже допустимой по политике."},
		contracts.CheckResult{Code: "ISSUANCE_BLOCK", Passed: after.IssuanceEnabled, Detail: "Внутреннее разрешение выдачи включено."},
	)
	valid := config.ValidateBankState(after) == nil
	for _, value := range newNorms {
		valid = valid && value != nil
	}
	checks = append(checks, contracts.CheckResult{Code: "STATE_VALID", Passed: valid, Detail: "Расчётное состояние неотрицательно, базы нормативов положительны, результаты конечны."})
	issued := true
	for _, c := range checks {
		issued = issued && c.Passed
	}
	if issued {
		after.IssuedApplicationIds = append(after.IssuedApplicationIds, applicationID)
	}
	return Projection{State: after, Checks: checks, Economics: econ, Issued: issued}, nil
}

var normOrder = []string{"N1_1", "N1_2", "N1_0", "N2", "N3", "N4", "N6", "N7", "N25", "N12"}

func norms(s contracts.BankState, threshold float64) map[string]*float64 {
	largest, largeSum, related := 0.0, 0.0, 0.0
	// Stable accumulation makes policy boundaries independent of map iteration.
	for _, key := range slices.Sorted(maps.Keys(s.Groups)) {
		risk := s.Groups[key]
		largest = max(largest, risk)
		if risk > threshold*s.K0 {
			largeSum += risk
		}
	}
	for _, key := range s.RelatedGroups {
		related = max(related, s.Groups[key])
	}
	return map[string]*float64{
		"N1_1": ratio(s.K1, s.RWA11), "N1_2": ratio(s.Ka, s.RWA12), "N1_0": ratio(s.K0, s.RWA10), "N2": ratio(s.Lam, s.D2), "N3": ratio(s.Lat, s.D3),
		"N4": ratio(s.Krd, s.K0+s.Funding4), "N6": ratio(largest, s.K0), "N7": ratio(largeSum, s.K0), "N25": ratio(related, s.K0), "N12": ratio(s.Invest, s.K0),
	}
}
func ratio(n, d float64) *float64 {
	if !finite(n) || !finite(d) || d <= 0 {
		return nil
	}
	return number(n / d)
}
func number(v float64) *float64 {
	if !finite(v) {
		return nil
	}
	return &v
}
func ptr[T any](v T) *T     { return &v }
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func comparison(v float64, op string, limit float64) bool {
	switch op {
	case ">=":
		return v >= limit
	case "<=":
		return v <= limit
	default:
		return false
	}
}
func copyState(s contracts.BankState) contracts.BankState {
	s.Groups = maps.Clone(s.Groups)
	s.BorrowerGroups = maps.Clone(s.BorrowerGroups)
	s.RelatedGroups = slices.Clone(s.RelatedGroups)
	s.IssuedApplicationIds = slices.Clone(s.IssuedApplicationIds)
	return s
}
func normDetail(code string) string {
	switch code {
	case "N1_1":
		return "Достаточность базового капитала."
	case "N1_2":
		return "Достаточность основного капитала."
	case "N1_0":
		return "Достаточность совокупного капитала."
	case "N2":
		return "Мгновенная ликвидность."
	case "N3":
		return "Текущая ликвидность."
	case "N4":
		return "Долгосрочная ликвидность."
	case "N6":
		return "Максимальный риск по всем группам заёмщиков."
	case "N7":
		return "Совокупный риск групп, превышающих порог крупного риска."
	case "N25":
		return "Максимальный риск группы, связанной с банком."
	default:
		return "Вложения в доли и акции относительно капитала."
	}
}
