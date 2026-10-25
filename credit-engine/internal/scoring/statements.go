package scoring

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

type balance struct{ nonCurrentAssets, inventories, receivables, shortInvestments, cash, currentAssets, assets, equity, longBorrowings, longLiabilities, shortBorrowings, currentLiabilities float64 }
type profit struct{ revenue, cost, salesProfit, interest, netProfit float64 }
type report struct {
	balance balance
	profit  profit
}
type reports struct{ previous, current report }

// DecodeStatements is shared with the future REST adapter. Unknown fields are
// ignored; a wrong JSON type is a dependency error, not missing business data.
func DecodeStatements(reader io.Reader) (contracts.StatementsResponse, error) {
	var r contracts.StatementsResponse
	d := json.NewDecoder(reader)
	if err := d.Decode(&r); err != nil {
		return r, &Error{Status: 502, Code: "INVALID_STATEMENTS"}
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return r, &Error{Status: 502, Code: "INVALID_STATEMENTS"}
	}
	return r, nil
}

func inspectStatements(r contracts.StatementsResponse, id string, now time.Time) (reports, []string, error) {
	var out reports
	if r.BorrowerID != id {
		return out, nil, &Error{Status: 502, Code: "STATEMENTS_BORROWER_MISMATCH", Fields: []string{"service_b.borrower_id"}}
	}
	var missing []string
	var selected [2]*contracts.AnnualReport
	for i := range r.AnnualReports {
		a := &r.AnnualReports[i]
		if a.Year == nil {
			missing = append(missing, "service_b.annual_reports.year")
			continue
		}
		idx := *a.Year - (now.Year() - 2)
		if idx < 0 || idx > 1 {
			continue
		}
		if selected[idx] != nil {
			missing = append(missing, fmt.Sprintf("service_b.annual_reports.%d", *a.Year))
		}
		selected[idx] = a
	}
	for i, a := range selected {
		year := now.Year() - 2 + i
		path := fmt.Sprintf("service_b.annual_reports.%d", year)
		if a == nil {
			missing = append(missing, path)
			continue
		}
		end := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
		if a.PeriodEnd == nil || *a.PeriodEnd != end.Format("2006-01-02") || i == 1 && now.Sub(end) > 365*24*time.Hour {
			missing = append(missing, path+".period_end")
		}
		bMissing, bErr := checkBalance(a.Balance, path+".balance")
		if bErr != nil {
			return out, nil, bErr
		}
		missing = append(missing, bMissing...)
		pMissing, pErr := checkProfit(a.ProfitAndLoss, path+".profit_and_loss")
		if pErr != nil {
			return out, nil, pErr
		}
		missing = append(missing, pMissing...)
		if len(bMissing) == 0 && len(pMissing) == 0 {
			normalized := normalizeReport(a)
			if i == 0 {
				out.previous = normalized
			} else {
				out.current = normalized
			}
		}
	}
	return out, missing, nil
}

type statementLine struct {
	name             string
	value            *int64
	required, signed bool
}

func checkLines(lines []statementLine, path string) []string {
	var missing []string
	for _, line := range lines {
		if line.value == nil {
			if line.required {
				missing = append(missing, path+"."+line.name)
			}
		} else if !line.signed && *line.value < 0 {
			missing = append(missing, path+"."+line.name)
		}
	}
	return missing
}
func unitFactor(unit *contracts.StatementUnit, path string) (float64, []string, error) {
	if unit == nil {
		return 0, []string{path + ".unit"}, nil
	}
	switch *unit {
	case contracts.StatementUnitRUB:
		return 1, nil, nil
	case contracts.StatementUnitThousandRUB:
		return 1000, nil, nil
	default:
		return 0, nil, &Error{Status: 502, Code: "INVALID_STATEMENT_UNIT", Fields: []string{path + ".unit"}}
	}
}
func balanceLines(b *contracts.BalanceShit) []statementLine {
	return []statementLine{
		{"intangible_assets", b.IntangibleAssets, false, false}, {"research_and_development", b.ResearchAndDevelopment, false, false}, {"intangible_exploration_assets", b.IntangibleExplorationAssets, false, false}, {"tangible_exploration_assets", b.TangibleExplorationAssets, false, false},
		{"fixed_assets", b.FixedAssets, false, false}, {"income_bearing_tangible_investments", b.IncomeBearingTangibleInvestments, false, false}, {"long_term_financial_investments", b.LongTermFinancialInvestments, false, false}, {"deferred_tax_assets", b.DeferredTaxAssets, false, false}, {"other_non_current_assets", b.OtherNonCurrentAssets, false, false},
		{"non_current_assets", b.NonCurrentAssets, true, false}, {"inventories", b.Inventories, true, false}, {"vat_on_acquired_assets", b.VATOnAcquiredAssets, false, false}, {"receivables", b.Receivables, true, false}, {"short_term_financial_investments", b.ShortTermFinancialInvestments, true, false}, {"cash_and_equivalents", b.CashAndEquivalents, true, false}, {"other_current_assets", b.OtherCurrentAssets, false, false},
		{"current_assets", b.CurrentAssets, true, false}, {"total_assets", b.TotalAssets, true, false}, {"charter_capital", b.CharterCapital, false, false}, {"treasury_shares", b.TreasuryShares, false, false}, {"revaluation_reserve", b.RevaluationReserve, false, true}, {"additional_capital", b.AdditionalCapital, false, true}, {"reserve_capital", b.ReserveCapital, false, false}, {"retained_earnings", b.RetainedEarnings, false, true},
		{"equity", b.Equity, true, true}, {"long_term_borrowings", b.LongTermBorrowings, true, false}, {"deferred_tax_liabilities", b.DeferredTaxLiabilities, false, false}, {"long_term_provisions", b.LongTermProvisions, false, false}, {"other_long_term_liabilities", b.OtherLongTermLiabilities, false, false}, {"long_term_liabilities", b.LongTermLiabilities, true, false},
		{"short_term_borrowings", b.ShortTermBorrowings, true, false}, {"payables", b.Payables, false, false}, {"deferred_income", b.DeferredIncome, false, false}, {"short_term_provisions", b.ShortTermProvisions, false, false}, {"other_short_term_liabilities", b.OtherShortTermLiabilities, false, false}, {"current_liabilities", b.CurrentLiabilities, true, false}, {"total_equity_and_liabilities", b.TotalEquityAndLiabilities, true, false},
	}
}
func profitLines(p *contracts.ProfitAndLoss) []statementLine {
	return []statementLine{
		{"revenue", p.Revenue, true, false}, {"cost_of_sales", p.CostOfSales, true, false}, {"gross_profit", p.GrossProfit, false, true}, {"selling_expenses", p.SellingExpenses, true, false}, {"administrative_expenses", p.AdministrativeExpenses, true, false}, {"sales_profit", p.SalesProfit, true, true},
		{"participation_income", p.ParticipationIncome, false, false}, {"interest_receivable", p.InterestReceivable, false, false}, {"interest_payable", p.InterestPayable, true, false}, {"other_income", p.OtherIncome, false, false}, {"other_expenses", p.OtherExpenses, false, false}, {"profit_before_tax", p.ProfitBeforeTax, false, true}, {"income_tax", p.IncomeTax, false, true}, {"net_profit", p.NetProfit, true, true},
	}
}

// Reconciliation is exact in original integer form units. This avoids both
// int64 overflow and loss of the one-unit tolerance before RUB normalization.
func equation(total *int64, positive, negative []*int64) bool {
	if total == nil {
		return true
	}
	sum := new(big.Int)
	for _, p := range positive {
		if p == nil {
			return true
		}
		sum.Add(sum, big.NewInt(*p))
	}
	for _, p := range negative {
		if p == nil {
			return true
		}
		sum.Sub(sum, big.NewInt(*p))
	}
	sum.Sub(sum, big.NewInt(*total))
	sum.Abs(sum)
	return sum.Cmp(big.NewInt(1)) <= 0
}
func partsFit(total *int64, parts ...*int64) bool {
	if total == nil {
		return true
	}
	sum := new(big.Int)
	for _, p := range parts {
		if p != nil {
			if *p > *total {
				return false
			}
			sum.Add(sum, big.NewInt(*p))
		}
	}
	sum.Sub(sum, big.NewInt(*total))
	return sum.Cmp(big.NewInt(1)) <= 0
}
func checkBalance(b *contracts.BalanceShit, path string) ([]string, error) {
	if b == nil {
		return []string{path}, nil
	}
	_, missing, err := unitFactor(b.Unit, path)
	if err != nil {
		return nil, err
	}
	missing = append(missing, checkLines(balanceLines(b), path)...)
	valid := equation(b.TotalAssets, []*int64{b.TotalEquityAndLiabilities}, nil) && equation(b.TotalAssets, []*int64{b.NonCurrentAssets, b.CurrentAssets}, nil) && equation(b.TotalEquityAndLiabilities, []*int64{b.Equity, b.LongTermLiabilities, b.CurrentLiabilities}, nil)
	noncurrent := []*int64{b.IntangibleAssets, b.ResearchAndDevelopment, b.IntangibleExplorationAssets, b.TangibleExplorationAssets, b.FixedAssets, b.IncomeBearingTangibleInvestments, b.LongTermFinancialInvestments, b.DeferredTaxAssets, b.OtherNonCurrentAssets}
	current := []*int64{b.Inventories, b.VATOnAcquiredAssets, b.Receivables, b.ShortTermFinancialInvestments, b.CashAndEquivalents, b.OtherCurrentAssets}
	long := []*int64{b.LongTermBorrowings, b.DeferredTaxLiabilities, b.LongTermProvisions, b.OtherLongTermLiabilities}
	short := []*int64{b.ShortTermBorrowings, b.Payables, b.DeferredIncome, b.ShortTermProvisions, b.OtherShortTermLiabilities}
	valid = valid && equation(b.NonCurrentAssets, noncurrent, nil) && equation(b.CurrentAssets, current, nil) && equation(b.LongTermLiabilities, long, nil) && equation(b.CurrentLiabilities, short, nil)
	valid = valid && equation(b.Equity, []*int64{b.CharterCapital, b.RevaluationReserve, b.AdditionalCapital, b.ReserveCapital, b.RetainedEarnings}, []*int64{b.TreasuryShares})
	valid = valid && partsFit(b.TotalAssets, b.NonCurrentAssets, b.CurrentAssets) && partsFit(b.NonCurrentAssets, noncurrent...) && partsFit(b.CurrentAssets, current...) && partsFit(b.LongTermLiabilities, long...) && partsFit(b.CurrentLiabilities, short...)
	if !valid {
		missing = append(missing, path)
	}
	return missing, nil
}
func checkProfit(p *contracts.ProfitAndLoss, path string) ([]string, error) {
	if p == nil {
		return []string{path}, nil
	}
	_, missing, err := unitFactor(p.Unit, path)
	if err != nil {
		return nil, err
	}
	missing = append(missing, checkLines(profitLines(p), path)...)
	valid := equation(p.SalesProfit, []*int64{p.Revenue}, []*int64{p.CostOfSales, p.SellingExpenses, p.AdministrativeExpenses}) && equation(p.GrossProfit, []*int64{p.Revenue}, []*int64{p.CostOfSales})
	valid = valid && equation(p.ProfitBeforeTax, []*int64{p.SalesProfit, p.ParticipationIncome, p.InterestReceivable, p.OtherIncome}, []*int64{p.InterestPayable, p.OtherExpenses}) && equation(p.NetProfit, []*int64{p.ProfitBeforeTax}, []*int64{p.IncomeTax})
	if !valid {
		missing = append(missing, path)
	}
	return missing, nil
}

func normalizeReport(a *contracts.AnnualReport) report {
	b, p := a.Balance, a.ProfitAndLoss
	bf, _, _ := unitFactor(b.Unit, "")
	pf, _, _ := unitFactor(p.Unit, "")
	bv := func(v *int64) float64 { return float64(*v) * bf }
	pv := func(v *int64) float64 { return float64(*v) * pf }
	return report{balance: balance{nonCurrentAssets: bv(b.NonCurrentAssets), inventories: bv(b.Inventories), receivables: bv(b.Receivables), shortInvestments: bv(b.ShortTermFinancialInvestments), cash: bv(b.CashAndEquivalents), currentAssets: bv(b.CurrentAssets), assets: bv(b.TotalAssets), equity: bv(b.Equity), longBorrowings: bv(b.LongTermBorrowings), longLiabilities: bv(b.LongTermLiabilities), shortBorrowings: bv(b.ShortTermBorrowings), currentLiabilities: bv(b.CurrentLiabilities)}, profit: profit{revenue: pv(p.Revenue), cost: pv(p.CostOfSales), salesProfit: pv(p.SalesProfit), interest: pv(p.InterestPayable), netProfit: pv(p.NetProfit)}}
}
