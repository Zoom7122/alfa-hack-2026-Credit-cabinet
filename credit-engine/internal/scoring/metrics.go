package scoring

import (
	"math"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

// metricValues contains existing v2 calculations only. Missing/undefined
// values remain nil; model rules decide their points and routing separately.
type metricValues struct {
	values                                                             map[int]*float64
	averageAssets, averageEquity, averageReceivables, averageInventory float64
}

func (m *metricValues) set(id int, value float64) { m.values[id] = &value }

func calculateIPMetrics(r contracts.DecisionRequest, f CashFlow) metricValues {
	m := metricValues{values: map[int]*float64{}}
	if f.Receipts > 0 {
		m.set(1, f.CFADS/f.Receipts)
	}
	if f.CFADS > 0 {
		m.set(2, f.HistoricalDebt/f.CFADS)
		if r.FinancialDebtRUB != nil {
			m.set(3, float64(*r.FinancialDebtRUB)/f.CFADS)
		}
	}
	if f.MonthlyRequired != 0 && r.Cash != nil && r.Cash.FreeAfterOwnContributionRUB != nil {
		m.set(4, float64(*r.Cash.FreeAfterOwnContributionRUB)/f.MonthlyRequired)
	}
	m.set(5, float64(f.PositiveMonths))
	mean := f.Receipts / 12
	variance := 0.0
	for _, v := range f.MonthlyReceipts {
		variance += (v - mean) * (v - mean) / 12
	}
	if mean > 0 {
		m.set(6, math.Sqrt(variance)/mean)
	}
	if r.IPEconomicData != nil && r.IPEconomicData.PreviousComparableReceiptsRUB != nil {
		base := float64(*r.IPEconomicData.PreviousComparableReceiptsRUB)
		if base != 0 {
			m.set(7, f.Receipts/base-1)
		}
	}
	previous, recent := 0.0, 0.0
	for i := 6; i < 9; i++ {
		previous += f.MonthlyReceipts[i]
	}
	for i := 9; i < 12; i++ {
		recent += f.MonthlyReceipts[i]
	}
	if previous != 0 {
		m.set(8, recent/previous-1)
	}
	if f.Receipts > 0 && r.Evidence != nil {
		e := r.Evidence
		if e.LargestBuyerGroupReceiptsRUB != nil {
			m.set(9, float64(*e.LargestBuyerGroupReceiptsRUB)/f.Receipts)
		}
		if e.ConfirmedReceiptsRUB != nil {
			m.set(10, float64(*e.ConfirmedReceiptsRUB)/f.Receipts)
		}
		if e.UnexplainedDifferenceRUB != nil {
			m.set(11, float64(*e.UnexplainedDifferenceRUB)/f.Receipts)
		}
	}
	if r.Need != nil && r.Need.ConfirmedCostRUB != nil && r.Need.OwnContributionRUB != nil && *r.Need.ConfirmedCostRUB != 0 {
		m.set(12, float64(*r.Need.OwnContributionRUB)/float64(*r.Need.ConfirmedCostRUB))
	}
	if f.Receipts-f.Variable > 0 {
		m.set(13, (f.Receipts-f.Fixed/(1-f.Variable/f.Receipts))/f.Receipts)
	}
	return m
}

func calculateLegalMetrics(r reports) metricValues {
	b, p := r.current.balance, r.current.profit
	oldB, oldP := r.previous.balance, r.previous.profit
	m := metricValues{values: map[int]*float64{}, averageAssets: (oldB.assets + b.assets) / 2, averageEquity: (oldB.equity + b.equity) / 2, averageReceivables: (oldB.receivables + b.receivables) / 2, averageInventory: (oldB.inventories + b.inventories) / 2}
	if b.currentLiabilities != 0 {
		m.set(1, b.currentAssets/b.currentLiabilities)
		m.set(2, (b.receivables+b.shortInvestments+b.cash)/b.currentLiabilities)
		m.set(3, (b.shortInvestments+b.cash)/b.currentLiabilities)
	}
	if b.assets != 0 {
		m.set(4, b.equity/b.assets)
		m.set(5, (b.equity+b.longLiabilities)/b.assets)
	}
	if b.currentAssets != 0 {
		m.set(6, (b.equity-b.nonCurrentAssets)/b.currentAssets)
	}
	if p.salesProfit > 0 {
		m.set(7, (b.longBorrowings+b.shortBorrowings-b.shortInvestments-b.cash)/p.salesProfit)
		if p.interest != 0 {
			m.set(8, p.salesProfit/p.interest)
		}
	}
	debt := b.longBorrowings + b.shortBorrowings
	if debt != 0 {
		m.set(9, b.shortBorrowings/debt)
	}
	if p.revenue != 0 {
		m.set(10, (p.revenue-p.cost)/p.revenue)
		m.set(11, p.salesProfit/p.revenue)
		m.set(12, p.netProfit/p.revenue)
		m.set(17, m.averageReceivables/p.revenue*365)
	}
	if m.averageAssets > 0 {
		m.set(13, p.netProfit/m.averageAssets)
		m.set(16, p.revenue/m.averageAssets)
	}
	if b.equity > 0 && m.averageEquity > 0 {
		m.set(14, p.netProfit/m.averageEquity)
	}
	if oldP.revenue != 0 {
		m.set(15, p.revenue/oldP.revenue-1)
	}
	if p.cost != 0 {
		m.set(18, m.averageInventory/p.cost*365)
	}
	return m
}
