package decision

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// bfoReport — год бухотчётности ГИР БО (fns.bo_bfo): строки форм в тыс. руб. с префиксом current/previous.
type bfoReport struct {
	Period          string `json:"period"`
	TypeCorrections []struct {
		Correction struct {
			Balance         map[string]float64 `json:"balance"`
			FinancialResult map[string]float64 `json:"financialResult"`
		} `json:"correction"`
	} `json:"typeCorrections"`
}

// StatementsFromFNS — ответ поставщика Б по данным ФНС из сбора: годы years (обычно два последних
// завершённых) из ГИР БО. Отчёт за год берётся как «текущий» период своей формы, а если её нет —
// как «предыдущий» период формы следующего года. Строки, которых в форме нет, — нули (в ГИР БО
// нулевые строки не сдают); итог пассива равен итогу актива. Нет бухотчётности (ИП) — пустой список.
func StatementsFromFNS(fns json.RawMessage, borrowerID string, years []int) (Statements, error) {
	out := Statements{BorrowerID: borrowerID, AnnualReports: []AnnualReport{}}
	var d struct {
		BOBFO []bfoReport `json:"bo_bfo"`
	}
	if len(fns) > 0 && string(fns) != "null" {
		if err := json.Unmarshal(fns, &d); err != nil {
			return out, fmt.Errorf("decode fns.bo_bfo: %w", err)
		}
	}
	forms := map[int]map[string]float64{} // год → строки формы с префиксом current/previous
	for _, r := range d.BOBFO {
		year, err := strconv.Atoi(r.Period)
		if err != nil || len(r.TypeCorrections) == 0 {
			continue
		}
		c := r.TypeCorrections[len(r.TypeCorrections)-1].Correction // последняя корректировка
		lines := map[string]float64{}
		for k, v := range c.Balance {
			lines[k] = v
		}
		for k, v := range c.FinancialResult {
			lines[k] = v
		}
		forms[year] = lines
	}
	for _, year := range years {
		lines, prefix := forms[year], "current"
		if lines == nil {
			lines, prefix = forms[year+1], "previous"
		}
		if lines == nil {
			continue
		}
		out.AnnualReports = append(out.AnnualReports, annualReport(year, lines, prefix))
	}
	return out, nil
}

func annualReport(year int, lines map[string]float64, prefix string) AnnualReport {
	line := func(code string) *int64 {
		v := int64(lines[prefix+code]) // нет строки — ноль
		return &v
	}
	unit := "THOUSAND_RUB"
	end := fmt.Sprintf("%d-12-31", year)
	b := &Balance{Unit: &unit, NonCurrentAssets: line("1100"), Inventories: line("1210"), Receivables: line("1230"),
		ShortTermFinancialInvest: line("1240"), CashAndEquivalents: line("1250"), CurrentAssets: line("1200"),
		TotalAssets: line("1600"), Equity: line("1300"), LongTermBorrowings: line("1410"), LongTermLiabilities: line("1400"),
		ShortTermBorrowings: line("1510"), Payables: line("1520"), CurrentLiabilities: line("1500"),
		TotalEquityAndLiabilities: line("1700")}
	if *b.TotalEquityAndLiabilities == 0 {
		b.TotalEquityAndLiabilities = b.TotalAssets
	}
	p := &ProfitAndLoss{Unit: &unit, Revenue: line("2110"), CostOfSales: line("2120"), SellingExpenses: line("2210"),
		AdministrativeExpenses: line("2220"), SalesProfit: line("2200"), InterestPayable: line("2330"), NetProfit: line("2400")}
	return AnnualReport{Year: &year, PeriodEnd: &end, Balance: b, ProfitAndLoss: p}
}
