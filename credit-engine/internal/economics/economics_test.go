package economics_test

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

func calculator(t *testing.T, parameters config.Economics) *economics.Calculator {
	t.Helper()
	calc, err := economics.NewCalculator(parameters)
	if err != nil {
		t.Fatal(err)
	}
	return calc
}

func profile(t *testing.T) config.Economics {
	t.Helper()
	policy, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	return policy.Economics
}

func evaluate(t *testing.T, calc *economics.Calculator, terms contracts.LoanTerms) economics.Result {
	t.Helper()
	result, err := calc.Evaluate(terms)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func readFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func TestBankEconomicReferences(t *testing.T) {
	calc := calculator(t, profile(t))
	paths, err := filepath.Glob("../../testdata/bank/*.json")
	if err != nil || len(paths) != 7 {
		t.Fatalf("bank references: %v, %v", paths, err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var fixture struct {
				Request     contracts.BankCheckRequest `json:"request"`
				InitialBank contracts.BankState        `json:"initial_bank"`
				Expected    struct {
					Response contracts.BankCheckResponse `json:"response"`
					State    contracts.BankState         `json:"bank_state_saved"`
				} `json:"expected"`
			}
			readFixture(t, path, &fixture)
			result := evaluate(t, calc, fixture.Request.Terms)
			checkedNPV := false
			for _, check := range fixture.Expected.Response.Checks {
				if check.Code != "NPV" {
					continue
				}
				if check.After == nil {
					t.Fatal("NPV reference is missing")
				}
				closeTo(t, "bank NPV", result.NPVRUB, *check.After, 0.001)
				if result.NPVKopecks != int64(math.Round(*check.After*100)) || result.Profitable() != check.Passed {
					t.Fatal("rounded NPV criterion differs from reference")
				}
				checkedNPV = true
			}
			if !checkedNPV {
				t.Fatal("reference has no NPV check")
			}
			closeTo(t, "monthly payment", result.Schedule.MonthlyPaymentRUB, fixture.Request.Selection.MonthlyPaymentRUB, 0.01)
			closeTo(t, "overpayment", result.Schedule.OverpaymentRUB, fixture.Request.Selection.OverpaymentRUB, 0.01)
			closeTo(t, "reserve", result.ReserveRUB, 150000, 0.001)
			closeTo(t, "net exposure", result.NetExposureRUB, 2850000, 0.001)
			closeTo(t, "risk-weighted exposure", result.RiskWeightedExposureRUB, 2850000, 0.001)
			if fixture.Expected.Response.Verdict == contracts.BankVerdictIssued {
				closeTo(t, "long-term exposure", result.LongTermExposureRUB, fixture.Expected.State.Krd-fixture.InitialBank.Krd, 0.01)
			}
		})
	}
}

func TestSelectedDealEconomicReferences(t *testing.T) {
	calc := calculator(t, profile(t))
	paths, err := filepath.Glob("../../testdata/decisions/*.json")
	if err != nil || len(paths) != 18 {
		t.Fatalf("decision references: %v, %v", paths, err)
	}
	checked := 0
	for _, path := range paths {
		var fixture struct {
			Expected struct {
				Response  contracts.DecisionResponse `json:"response"`
				Selection *contracts.SelectionAudit  `json:"selection"`
			} `json:"expected"`
		}
		readFixture(t, path, &fixture)
		if fixture.Expected.Response.Terms == nil {
			continue
		}
		checked++
		t.Run(filepath.Base(path), func(t *testing.T) {
			selection := fixture.Expected.Selection
			if selection == nil {
				t.Fatal("selected deal requires an economic reference")
			}
			result := evaluate(t, calc, *fixture.Expected.Response.Terms)
			closeTo(t, "selected NPV", result.NPVRUB, selection.SelectedNPVRUB, 0.001)
			closeTo(t, "selected payment", result.Schedule.MonthlyPaymentRUB, selection.MonthlyPaymentRUB, 0.01)
			closeTo(t, "selected overpayment", result.Schedule.OverpaymentRUB, selection.OverpaymentRUB, 0.01)
			if !result.Profitable() {
				t.Fatal("selected deal must have positive NPV even when bank constraints fail")
			}
		})
	}
	if checked != 8 {
		t.Fatalf("expected 8 selected reference deals, got %d", checked)
	}
}

func TestOneMonthCostComponents(t *testing.T) {
	base := profile(t)
	base.DiscountAnnual = 0
	terms := contracts.LoanTerms{AmountRUB: 1200, TermMonths: 1, AnnualRate: 24}
	// Hand calculation: income 24 - FTP 15 - opex 1 - EL 1.80 - capital cost 2.28 = 3.92.
	for _, tc := range []struct {
		name    string
		change  func(*config.Economics)
		wantNPV float64
	}{
		{"base", func(*config.Economics) {}, 3.92},
		{"funding", func(p *config.Economics) { p.FTPAnnual += 0.12 }, -8.08},
		{"operating cost", func(p *config.Economics) { p.OpexAnnual += 0.12 }, -8.08},
		{"PD", func(p *config.Economics) { p.PDAnnual = 0.14 }, -0.58},
		{"LGD", func(p *config.Economics) { p.LGD = 0.95 }, 1.92},
		{"reserve is not a second NPV expense", func(p *config.Economics) { p.ReserveRate = 0.1 }, 4.04},
		{"risk weight", func(p *config.Economics) { p.RiskWeight = 2 }, 1.64},
		{"allocated capital", func(p *config.Economics) { p.CapitalAllocation = 0.24 }, 1.64},
		{"capital price", func(p *config.Economics) { p.CapitalPriceAnnual = 0.4 }, 1.64},
		{"upfront expense", func(p *config.Economics) { p.UpfrontCostRUB = 100 }, -96.08},
		{"discount", func(p *config.Economics) { p.DiscountAnnual = 0.15 }, 3.87},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.change(&p)
			result := evaluate(t, calculator(t, p), terms)
			closeTo(t, "NPV", result.NPVRUB, tc.wantNPV, 0.001)
		})
	}
	result := evaluate(t, calculator(t, base), terms)
	closeTo(t, "RAROC", *result.RAROC, 6.2/11.4, 1e-12)
	closeTo(t, "reserve", result.ReserveRUB, 60, 1e-12)
	closeTo(t, "net exposure", result.NetExposureRUB, 1140, 1e-12)
	if result.LongTermExposureRUB != 0 {
		t.Fatal("one-month loan has no long-term exposure")
	}
}

func TestFundingAndLongTermBoundaries(t *testing.T) {
	p := config.Economics{QualityCategory: 2, FTPLongAddition: 0.12, ReserveRate: 0.05}
	calc := calculator(t, p)
	for _, tc := range []struct {
		months                         int
		amount                         int64
		wantFTP, wantNPV, wantLongTerm float64
	}{
		{11, 1100, 0, 0, 0}, {12, 1200, 0, 0, 0}, {13, 1300, 0, 0, 95},
		{24, 2400, 0, 0, 1140}, {25, 2500, 0.12, -325, 1235}, {36, 3600, 0.12, -666, 2280},
	} {
		result := evaluate(t, calc, contracts.LoanTerms{AmountRUB: tc.amount, TermMonths: tc.months, AnnualRate: 0})
		closeTo(t, "FTP", result.FTPAnnual, tc.wantFTP, 1e-12)
		closeTo(t, "NPV", result.NPVRUB, tc.wantNPV, 0.001)
		closeTo(t, "long-term net exposure", result.LongTermExposureRUB, tc.wantLongTerm, 1e-8)
	}
}

func TestUpfrontCostIsChargedOnceBeforeDiscounting(t *testing.T) {
	p := profile(t)
	terms := contracts.LoanTerms{AmountRUB: 3000000, TermMonths: 24, AnnualRate: 24}
	base := evaluate(t, calculator(t, p), terms)
	p.UpfrontCostRUB = 1000
	withCost := evaluate(t, calculator(t, p), terms)
	if withCost.NPVKopecks != 11794554 || base.NPVKopecks-withCost.NPVKopecks != 100000 {
		t.Fatal("upfront cost must reduce the 24-month discounted NPV exactly once, without discounting the cost")
	}
	if base.ReserveRUB != withCost.ReserveRUB || base.Schedule.MonthlyPaymentRUB != withCost.Schedule.MonthlyPaymentRUB {
		t.Fatal("bank upfront cost must not change the borrower schedule or reserve")
	}
	// Two-month hand calculation at zero interest/discount: balances 2400+1200,
	// FTP 45, opex 3, EL 5.40, capital cost 6.84 and one upfront cost 100.
	p = profile(t)
	p.DiscountAnnual = 0
	p.UpfrontCostRUB = 100
	result := evaluate(t, calculator(t, p), contracts.LoanTerms{AmountRUB: 2400, TermMonths: 2, AnnualRate: 0})
	closeTo(t, "two-month NPV", result.NPVRUB, -160.24, 0.001)
	closeTo(t, "two-month RAROC", *result.RAROC, -153.4/34.2, 1e-12)
}

func TestRoundingOnlyOnceAndProfitability(t *testing.T) {
	calc := calculator(t, config.Economics{QualityCategory: 2})
	result := evaluate(t, calc, contracts.LoanTerms{AmountRUB: 1, TermMonths: 4, AnnualRate: 24})
	// Unrounded interest totals about 0.0505; rounding each month would give 0.06.
	if result.NPVKopecks != 5 {
		t.Fatalf("monthly rounding changed NPV: %v", result.NPVRUB)
	}
	for _, tc := range []struct {
		upfront     float64
		wantKopecks int64
		profitable  bool
	}{
		{0.9951, 0, false}, {0.995, 1, true}, {1, 0, false}, {1.0051, -1, false},
	} {
		p := config.Economics{QualityCategory: 2, UpfrontCostRUB: tc.upfront}
		result := evaluate(t, calculator(t, p), contracts.LoanTerms{AmountRUB: 12, TermMonths: 1, AnnualRate: 100})
		if result.NPVKopecks != tc.wantKopecks || result.Profitable() != tc.profitable {
			t.Fatalf("upfront %v: NPV=%v, profitable=%v", tc.upfront, result.NPVRUB, result.Profitable())
		}
	}
	for _, change := range []func(*config.Economics){
		func(p *config.Economics) { p.CapitalAllocation = 0 }, func(p *config.Economics) { p.RiskWeight = 0 }, func(p *config.Economics) { p.ReserveRate = 1 },
	} {
		p := profile(t)
		change(&p)
		result := evaluate(t, calculator(t, p), contracts.LoanTerms{AmountRUB: 1200, TermMonths: 1, AnnualRate: 24})
		if result.RAROC != nil {
			t.Fatal("RAROC must be undefined when allocated capital is zero")
		}
	}
}

func TestInvalidParametersAndArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Economics)
	}{
		{"reserve", func(p *config.Economics) { p.ReserveRate = 1.01 }},
		{"category", func(p *config.Economics) { p.QualityCategory = 0 }},
		{"risk weight", func(p *config.Economics) { p.RiskWeight = -1 }},
		{"PD", func(p *config.Economics) { p.PDAnnual = math.NaN() }},
		{"LGD", func(p *config.Economics) { p.LGD = -0.1 }},
		{"FTP", func(p *config.Economics) { p.FTPAnnual = math.Inf(1) }},
		{"long FTP", func(p *config.Economics) { p.FTPLongAddition = -0.01 }},
		{"combined FTP overflow", func(p *config.Economics) { p.FTPAnnual = math.MaxFloat64; p.FTPLongAddition = math.MaxFloat64 }},
		{"opex", func(p *config.Economics) { p.OpexAnnual = -0.01 }},
		{"upfront", func(p *config.Economics) { p.UpfrontCostRUB = -1 }},
		{"capital allocation", func(p *config.Economics) { p.CapitalAllocation = 1.01 }},
		{"capital price", func(p *config.Economics) { p.CapitalPriceAnnual = -0.1 }},
		{"discount", func(p *config.Economics) { p.DiscountAnnual = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := profile(t)
			tc.change(&p)
			if _, err := economics.NewCalculator(p); !errors.Is(err, economics.ErrInvalidInput) {
				t.Fatalf("invalid parameters accepted: %v", err)
			}
		})
	}
	terms := contracts.LoanTerms{AmountRUB: 1200, TermMonths: 1, AnnualRate: 24}
	var uninitialized economics.Calculator
	var nilCalculator *economics.Calculator
	for _, calc := range []*economics.Calculator{&uninitialized, nilCalculator} {
		if _, err := calc.Evaluate(terms); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("uninitialized calculator: %v", err)
		}
	}
	p := profile(t)
	p.RiskWeight = math.MaxFloat64
	if _, err := calculator(t, p).Evaluate(terms); !errors.Is(err, economics.ErrArithmetic) {
		t.Fatalf("exposure overflow: %v", err)
	}
	p = profile(t)
	p.CapitalPriceAnnual = math.MaxFloat64
	if _, err := calculator(t, p).Evaluate(terms); !errors.Is(err, economics.ErrArithmetic) {
		t.Fatalf("cash-flow overflow: %v", err)
	}
	p = profile(t)
	p.UpfrontCostRUB = 1e17
	if _, err := calculator(t, p).Evaluate(terms); !errors.Is(err, economics.ErrAmountOverflow) {
		t.Fatalf("kopeck overflow: %v", err)
	}
	for _, invalid := range []contracts.LoanTerms{
		{AmountRUB: 0, TermMonths: 24, AnnualRate: 24}, {AmountRUB: 1, TermMonths: 37, AnnualRate: 24},
		{AmountRUB: 1, TermMonths: 24, AnnualRate: 100.01}, {AmountRUB: 1, TermMonths: 24, AnnualRate: 23.751},
	} {
		if _, err := calculator(t, profile(t)).Evaluate(invalid); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("invalid REST terms: %v", err)
		}
	}
}

func TestCalculatorIsIndependentOfCallerAndConcurrent(t *testing.T) {
	p := profile(t)
	calc := calculator(t, p)
	p.FTPAnnual = 1
	terms := contracts.LoanTerms{AmountRUB: 3000000, TermMonths: 24, AnnualRate: 24}
	first := evaluate(t, calc, terms)
	first.Schedule.Periods[0].OpeningBalanceRUB = 0
	errorsCh := make(chan error, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := calc.Evaluate(terms)
			if err != nil {
				errorsCh <- err
				return
			}
			if result.NPVKopecks != 11894554 || result.Schedule.Periods[0].OpeningBalanceRUB != 3000000 {
				errorsCh <- errors.New("calculator shares mutable state")
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}
