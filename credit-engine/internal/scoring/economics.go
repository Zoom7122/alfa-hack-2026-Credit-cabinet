package scoring

import (
	"fmt"
	"math"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func coefficient(out *contracts.EconomicResult, id string, x float64) {
	p, m := economicPoints(id, x)
	out.Criteria = append(out.Criteria, contracts.EconomicCriterion{ID: id, Value: &x, Points: p, MaxPoints: m})
	out.Points += p
}
func special(out *contracts.EconomicResult, id string, points float64) {
	_, m := economicPoints(id, 0)
	out.Criteria = append(out.Criteria, contracts.EconomicCriterion{ID: id, Points: points, MaxPoints: m})
	out.Points += points
}
func ipEconomics(r contracts.DecisionRequest, f CashFlow, manual []contracts.Reason) (contracts.EconomicResult, []contracts.Reason) {
	out := contracts.EconomicResult{MaxPoints: 250}
	add := func(n int, x float64) { coefficient(&out, fmt.Sprintf("IP_E%02d", n), x) }
	zero := func(n int, points float64, review bool) {
		id := fmt.Sprintf("IP_E%02d", n)
		special(&out, id, points)
		if review {
			manual = append(manual, reason("SPECIAL_"+id, routingMessage("SPECIAL_"+id), id))
		}
	}
	if f.Receipts > 0 {
		add(1, f.CFADS/f.Receipts)
	} else {
		zero(1, 0, false)
	}
	if f.CFADS > 0 {
		add(2, f.HistoricalDebt/f.CFADS)
		add(3, float64(*r.FinancialDebtRUB)/f.CFADS)
	} else {
		zero(2, 0, false)
		zero(3, 0, false)
	}
	if f.MonthlyRequired == 0 {
		zero(4, 25, false)
	} else {
		add(4, float64(*r.Cash.FreeAfterOwnContributionRUB)/f.MonthlyRequired)
	}
	add(5, float64(f.PositiveMonths))
	mean := f.Receipts / 12
	variance := 0.0
	for _, v := range f.MonthlyReceipts {
		variance += (v - mean) * (v - mean) / 12
	}
	if mean > 0 {
		add(6, math.Sqrt(variance)/mean)
	} else {
		zero(6, 0, false)
	}
	base := float64(*r.IPEconomicData.PreviousComparableReceiptsRUB)
	if base == 0 {
		zero(7, 0, true)
	} else {
		add(7, f.Receipts/base-1)
	}
	previous, recent := 0.0, 0.0
	for i := 6; i < 9; i++ {
		previous += f.MonthlyReceipts[i]
	}
	for i := 9; i < 12; i++ {
		recent += f.MonthlyReceipts[i]
	}
	if previous == 0 {
		zero(8, 0, true)
	} else {
		add(8, recent/previous-1)
	}
	if f.Receipts > 0 {
		add(9, float64(*r.Evidence.LargestBuyerGroupReceiptsRUB)/f.Receipts)
		add(10, float64(*r.Evidence.ConfirmedReceiptsRUB)/f.Receipts)
		add(11, float64(*r.Evidence.UnexplainedDifferenceRUB)/f.Receipts)
	} else {
		zero(9, 0, false)
		zero(10, 0, false)
		zero(11, 0, false)
	}
	if *r.Need.ConfirmedCostRUB == 0 {
		zero(12, 0, true)
	} else {
		add(12, float64(*r.Need.OwnContributionRUB)/float64(*r.Need.ConfirmedCostRUB))
	}
	if f.Receipts-f.Variable <= 0 {
		zero(13, 0, true)
	} else {
		add(13, (f.Receipts-f.Fixed/(1-f.Variable/f.Receipts))/f.Receipts)
	}
	return out, manual
}

func legalEconomics(r reports, manual []contracts.Reason) (contracts.EconomicResult, []contracts.Reason) {
	out := contracts.EconomicResult{MaxPoints: 250}
	b, p := r.current.balance, r.current.profit
	oldB, oldP := r.previous.balance, r.previous.profit
	avgAssets := (oldB.assets + b.assets) / 2
	avgEquity := (oldB.equity + b.equity) / 2
	avgReceivables := (oldB.receivables + b.receivables) / 2
	avgInventory := (oldB.inventories + b.inventories) / 2
	add := func(n int, x float64) { coefficient(&out, fmt.Sprintf("LE_E%02d", n), x) }
	zero := func(n int, points float64, review bool) {
		id := fmt.Sprintf("LE_E%02d", n)
		special(&out, id, points)
		if review {
			manual = append(manual, reason("SPECIAL_"+id, routingMessage("SPECIAL_"+id), id))
		}
	}
	if b.currentLiabilities == 0 {
		zero(1, 20, false)
		zero(2, 15, false)
		zero(3, 10, false)
	} else {
		add(1, b.currentAssets/b.currentLiabilities)
		add(2, (b.receivables+b.shortInvestments+b.cash)/b.currentLiabilities)
		add(3, (b.shortInvestments+b.cash)/b.currentLiabilities)
	}
	if b.assets == 0 {
		zero(4, 0, true)
		zero(5, 0, true)
	} else {
		add(4, b.equity/b.assets)
		add(5, (b.equity+b.longLiabilities)/b.assets)
	}
	if b.equity <= 0 {
		manual = append(manual, reason("NONPOSITIVE_EQUITY", "Неположительный собственный капитал.", "LE_E04"))
	}
	if b.currentAssets == 0 {
		zero(6, 0, true)
	} else {
		add(6, (b.equity-b.nonCurrentAssets)/b.currentAssets)
	}
	if p.salesProfit <= 0 {
		zero(7, 0, true)
		zero(8, 0, true)
	} else {
		add(7, (b.longBorrowings+b.shortBorrowings-b.shortInvestments-b.cash)/p.salesProfit)
		if p.interest == 0 {
			zero(8, 20, false)
		} else {
			add(8, p.salesProfit/p.interest)
		}
	}
	debt := b.longBorrowings + b.shortBorrowings
	if debt == 0 {
		zero(9, 10, false)
	} else {
		add(9, b.shortBorrowings/debt)
	}
	if p.revenue == 0 {
		zero(10, 0, true)
		zero(11, 0, true)
		zero(12, 0, true)
	} else {
		add(10, (p.revenue-p.cost)/p.revenue)
		add(11, p.salesProfit/p.revenue)
		add(12, p.netProfit/p.revenue)
		if p.salesProfit <= 0 {
			manual = append(manual, reason("NONPOSITIVE_SALES_PROFIT", "Неположительная прибыль от продаж.", "LE_E11"))
		}
	}
	if oldP.netProfit < 0 && p.netProfit < 0 {
		manual = append(manual, reason("TWO_YEAR_NET_LOSS", "Убыток в обоих годах.", "LE_E12"))
	}
	if avgAssets <= 0 {
		zero(13, 0, true)
	} else {
		add(13, p.netProfit/avgAssets)
	}
	if b.equity <= 0 || avgEquity <= 0 {
		zero(14, 0, true)
	} else {
		add(14, p.netProfit/avgEquity)
	}
	if oldP.revenue == 0 {
		zero(15, 0, true)
	} else {
		add(15, p.revenue/oldP.revenue-1)
	}
	if avgAssets <= 0 {
		zero(16, 0, true)
	} else {
		add(16, p.revenue/avgAssets)
	}
	if p.revenue == 0 {
		zero(17, 0, true)
	} else {
		add(17, avgReceivables/p.revenue*365)
	}
	if p.cost == 0 {
		if avgInventory == 0 {
			zero(18, 10, false)
		} else {
			zero(18, 0, true)
		}
	} else {
		add(18, avgInventory/p.cost*365)
	}
	return out, manual
}
