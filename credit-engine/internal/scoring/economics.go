package scoring

import (
	"fmt"

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
	metrics := calculateIPMetrics(r, f)
	add := func(n int, x float64) { coefficient(&out, fmt.Sprintf("IP_E%02d", n), x) }
	zero := func(n int, points float64, review bool) {
		id := fmt.Sprintf("IP_E%02d", n)
		special(&out, id, points)
		if review {
			manual = append(manual, reason("SPECIAL_"+id, routingMessage("SPECIAL_"+id), id))
		}
	}
	if f.Receipts > 0 {
		add(1, *metrics.values[1])
	} else {
		zero(1, 0, false)
	}
	if f.CFADS > 0 {
		add(2, *metrics.values[2])
		add(3, *metrics.values[3])
	} else {
		zero(2, 0, false)
		zero(3, 0, false)
	}
	if f.MonthlyRequired == 0 {
		zero(4, 25, false)
	} else {
		add(4, *metrics.values[4])
	}
	add(5, *metrics.values[5])
	mean := f.Receipts / 12
	if mean > 0 {
		add(6, *metrics.values[6])
	} else {
		zero(6, 0, false)
	}
	base := float64(*r.IPEconomicData.PreviousComparableReceiptsRUB)
	if base == 0 {
		zero(7, 0, true)
	} else {
		add(7, *metrics.values[7])
	}
	previous := 0.0
	for i := 6; i < 9; i++ {
		previous += f.MonthlyReceipts[i]
	}
	if previous == 0 {
		zero(8, 0, true)
	} else {
		add(8, *metrics.values[8])
	}
	if f.Receipts > 0 {
		add(9, *metrics.values[9])
		add(10, *metrics.values[10])
		add(11, *metrics.values[11])
	} else {
		zero(9, 0, false)
		zero(10, 0, false)
		zero(11, 0, false)
	}
	if *r.Need.ConfirmedCostRUB == 0 {
		zero(12, 0, true)
	} else {
		add(12, *metrics.values[12])
	}
	if f.Receipts-f.Variable <= 0 {
		zero(13, 0, true)
	} else {
		add(13, *metrics.values[13])
	}
	return out, manual
}

func legalEconomics(r reports, manual []contracts.Reason) (contracts.EconomicResult, []contracts.Reason) {
	out := contracts.EconomicResult{MaxPoints: 250}
	metrics := calculateLegalMetrics(r)
	b, p := r.current.balance, r.current.profit
	oldP := r.previous.profit
	avgAssets := metrics.averageAssets
	avgEquity := metrics.averageEquity
	avgInventory := metrics.averageInventory
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
		add(1, *metrics.values[1])
		add(2, *metrics.values[2])
		add(3, *metrics.values[3])
	}
	if b.assets == 0 {
		zero(4, 0, true)
		zero(5, 0, true)
	} else {
		add(4, *metrics.values[4])
		add(5, *metrics.values[5])
	}
	if b.equity <= 0 {
		manual = append(manual, reason("NONPOSITIVE_EQUITY", "Неположительный собственный капитал.", "LE_E04"))
	}
	if b.currentAssets == 0 {
		zero(6, 0, true)
	} else {
		add(6, *metrics.values[6])
	}
	if p.salesProfit <= 0 {
		zero(7, 0, true)
		zero(8, 0, true)
	} else {
		add(7, *metrics.values[7])
		if p.interest == 0 {
			zero(8, 20, false)
		} else {
			add(8, *metrics.values[8])
		}
	}
	debt := b.longBorrowings + b.shortBorrowings
	if debt == 0 {
		zero(9, 10, false)
	} else {
		add(9, *metrics.values[9])
	}
	if p.revenue == 0 {
		zero(10, 0, true)
		zero(11, 0, true)
		zero(12, 0, true)
	} else {
		add(10, *metrics.values[10])
		add(11, *metrics.values[11])
		add(12, *metrics.values[12])
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
		add(13, *metrics.values[13])
	}
	if b.equity <= 0 || avgEquity <= 0 {
		zero(14, 0, true)
	} else {
		add(14, *metrics.values[14])
	}
	if oldP.revenue == 0 {
		zero(15, 0, true)
	} else {
		add(15, *metrics.values[15])
	}
	if avgAssets <= 0 {
		zero(16, 0, true)
	} else {
		add(16, *metrics.values[16])
	}
	if p.revenue == 0 {
		zero(17, 0, true)
	} else {
		add(17, *metrics.values[17])
	}
	if p.cost == 0 {
		if avgInventory == 0 {
			zero(18, 10, false)
		} else {
			zero(18, 0, true)
		}
	} else {
		add(18, *metrics.values[18])
	}
	return out, manual
}
