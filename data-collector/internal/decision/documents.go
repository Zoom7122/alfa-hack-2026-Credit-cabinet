package decision

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
)

// DocumentIndicator is an approved value from the current generation of a
// document in this application. Amounts are whole rubles, never thousands.
type DocumentIndicator struct {
	DocumentID string           `json:"document_id"`
	ResultID   string           `json:"result_id"`
	Generation int              `json:"generation"`
	Name       string           `json:"name"`
	Period     string           `json:"period"`
	PeriodKind string           `json:"period_kind"`
	AmountRUB  int64            `json:"amount_rub"`
	Sources    []map[string]any `json:"sources"`
}

// WholeRubles rejects malformed amounts, fractions and overflow instead of
// rounding parser output into a different financial fact.
var decimalAmount = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,2})?$`)

func WholeRubles(amount string) (int64, bool) {
	if len(amount) > 40 || !decimalAmount.MatchString(amount) {
		return 0, false
	}
	r, ok := new(big.Rat).SetString(amount)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	return r.Num().Int64(), true
}

func documentYear(d DocumentIndicator, lastYear int) (int, bool) {
	year, err := strconv.Atoi(d.Period)
	return year, err == nil && strconv.Itoa(year) == d.Period && (year == lastYear || year == lastYear-1)
}

// applyDocumentStatements overlays only approved indicators. Newest documents
// arrive first; missing rows remain missing and are checked by credit-engine.
func applyDocumentStatements(base Statements, documents []DocumentIndicator, lastYear int) (Statements, []DocumentIndicator) {
	reports := map[int]AnnualReport{}
	for _, r := range base.AnnualReports {
		if r.Year != nil {
			reports[*r.Year] = r
		}
	}
	used := []DocumentIndicator{}
	seen := map[string]bool{}
	for _, d := range documents {
		year, ok := documentYear(d, lastYear)
		if !ok || seen[d.Name+":"+d.Period] {
			continue
		}
		expectedKind := "AS_OF"
		if d.Name == "accounting_revenue" || d.Name == "net_profit" {
			expectedKind = "YEAR"
		}
		if d.PeriodKind != expectedKind || d.AmountRUB < 0 && d.Name != "net_profit" && d.Name != "equity" {
			continue
		}
		r := rubReport(reports[year])
		if r.Year == nil {
			r.Year, r.PeriodEnd = ptr(year), ptr(fmt.Sprintf("%d-12-31", year))
		}
		var field **int64
		switch d.Name {
		case "accounting_revenue":
			field = &r.ProfitAndLoss.Revenue
		case "net_profit":
			field = &r.ProfitAndLoss.NetProfit
		case "total_assets":
			field = &r.Balance.TotalAssets
		case "equity":
			field = &r.Balance.Equity
		case "long_term_liabilities":
			field = &r.Balance.LongTermLiabilities
		case "short_term_liabilities":
			field = &r.Balance.CurrentLiabilities
		default:
			continue
		}
		*field = ptr(d.AmountRUB)
		reports[year] = r
		seen[d.Name+":"+d.Period] = true
		used = append(used, d)
	}
	if len(used) == 0 {
		return base, used
	}
	out := Statements{BorrowerID: base.BorrowerID, AnnualReports: []AnnualReport{}}
	for _, year := range []int{lastYear - 1, lastYear} {
		if r, ok := reports[year]; ok {
			out.AnnualReports = append(out.AnnualReports, r)
		}
	}
	return out, used
}

func scaleAmounts(unit **string, fields ...**int64) bool {
	factor := int64(1)
	if *unit == nil {
		return false
	}
	switch **unit {
	case "THOUSAND_RUB":
		factor = 1000
	case "RUB":
	default:
		return false
	}
	for _, field := range fields {
		if *field == nil {
			continue
		}
		v := new(big.Int).Mul(big.NewInt(**field), big.NewInt(factor))
		if !v.IsInt64() {
			return false
		}
		*field = ptr(v.Int64())
	}
	*unit = ptr("RUB")
	return true
}

func rubReport(r AnnualReport) AnnualReport {
	b, p := &Balance{Unit: ptr("RUB")}, &ProfitAndLoss{Unit: ptr("RUB")}
	if r.Balance != nil {
		*b = *r.Balance
		if !scaleAmounts(&b.Unit, &b.NonCurrentAssets, &b.Inventories, &b.Receivables, &b.ShortTermFinancialInvest,
			&b.CashAndEquivalents, &b.CurrentAssets, &b.TotalAssets, &b.Equity, &b.LongTermBorrowings,
			&b.LongTermLiabilities, &b.ShortTermBorrowings, &b.Payables, &b.CurrentLiabilities, &b.TotalEquityAndLiabilities) {
			b = &Balance{Unit: ptr("RUB")}
		}
	}
	if r.ProfitAndLoss != nil {
		*p = *r.ProfitAndLoss
		if !scaleAmounts(&p.Unit, &p.Revenue, &p.CostOfSales, &p.SellingExpenses, &p.AdministrativeExpenses,
			&p.SalesProfit, &p.InterestPayable, &p.NetProfit) {
			p = &ProfitAndLoss{Unit: ptr("RUB")}
		}
	}
	r.Balance, r.ProfitAndLoss = b, p
	return r
}

func documentIncome(documents []DocumentIndicator, year int) (int64, *DocumentIndicator) {
	for _, d := range documents {
		if d.Name == "usn_taxable_income" && d.PeriodKind == "YEAR" && d.Period == strconv.Itoa(year) && d.AmountRUB >= 0 {
			return d.AmountRUB, &d
		}
	}
	return 0, nil
}
