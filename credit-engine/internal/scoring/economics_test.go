package scoring

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func strongReports(t *testing.T) reports {
	t.Helper()
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	r, missing, err := inspectStatements(f.Statements, f.Request.Borrower.ID, dateOnly(evaluationDate))
	if err != nil || len(missing) != 0 {
		t.Fatalf("%v %v", missing, err)
	}
	return r
}
func finiteCriteria(t *testing.T, r contracts.EconomicResult) {
	t.Helper()
	for _, c := range r.Criteria {
		if c.Value != nil && (math.IsNaN(*c.Value) || math.IsInf(*c.Value, 0)) {
			t.Fatalf("%s nonfinite", c.ID)
		}
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatal(err)
	}
}
func TestLegalSpecialDenominators(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*reports)
		ids    []string
		points []float64
		manual bool
	}{
		{"current liabilities zero", func(r *reports) { r.current.balance.currentLiabilities = 0 }, []string{"LE_E01", "LE_E02", "LE_E03"}, []float64{20, 15, 10}, false},
		{"assets zero", func(r *reports) { r.current.balance.assets = 0 }, []string{"LE_E04", "LE_E05"}, []float64{0, 0}, true},
		{"current assets zero", func(r *reports) { r.current.balance.currentAssets = 0 }, []string{"LE_E06"}, []float64{0}, true},
		{"sales profit zero", func(r *reports) { r.current.profit.salesProfit = 0 }, []string{"LE_E07", "LE_E08"}, []float64{0, 0}, true},
		{"sales profit negative", func(r *reports) { r.current.profit.salesProfit = -1 }, []string{"LE_E07", "LE_E08"}, []float64{0, 0}, true},
		{"interest zero profit positive", func(r *reports) { r.current.profit.interest = 0 }, []string{"LE_E08"}, []float64{20}, false},
		{"interest and profit zero", func(r *reports) { r.current.profit.interest = 0; r.current.profit.salesProfit = 0 }, []string{"LE_E08"}, []float64{0}, true},
		{"financial debt zero", func(r *reports) { r.current.balance.longBorrowings = 0; r.current.balance.shortBorrowings = 0 }, []string{"LE_E09"}, []float64{10}, false},
		{"revenue zero", func(r *reports) { r.current.profit.revenue = 0 }, []string{"LE_E10", "LE_E11", "LE_E12", "LE_E17"}, []float64{0, 0, 0, 0}, true},
		{"average assets zero", func(r *reports) { r.current.balance.assets = 0; r.previous.balance.assets = 0 }, []string{"LE_E13", "LE_E16"}, []float64{0, 0}, true},
		{"current equity zero", func(r *reports) { r.current.balance.equity = 0 }, []string{"LE_E14"}, []float64{0}, true},
		{"current equity negative", func(r *reports) { r.current.balance.equity = -1 }, []string{"LE_E14"}, []float64{0}, true},
		{"average equity zero", func(r *reports) { r.previous.balance.equity = -r.current.balance.equity }, []string{"LE_E14"}, []float64{0}, true},
		{"previous revenue zero", func(r *reports) { r.previous.profit.revenue = 0 }, []string{"LE_E15"}, []float64{0}, true},
		{"cost zero inventory positive", func(r *reports) { r.current.profit.cost = 0 }, []string{"LE_E18"}, []float64{0}, true},
		{"cost inventory zero", func(r *reports) {
			r.current.profit.cost = 0
			r.current.balance.inventories = 0
			r.previous.balance.inventories = 0
		}, []string{"LE_E18"}, []float64{10}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := strongReports(t)
			c.change(&r)
			out, manual := legalEconomics(r, nil)
			finiteCriteria(t, out)
			for i, id := range c.ids {
				got := economic(t, out, id)
				if got.Value != nil || got.Points != c.points[i] {
					t.Errorf("%s got %+v", id, got)
				}
			}
			if (len(manual) > 0) != c.manual {
				t.Fatalf("manual=%+v", manual)
			}
		})
	}
	t.Run("negative net debt", func(t *testing.T) {
		r := strongReports(t)
		r.current.balance.longBorrowings = 0
		r.current.balance.shortBorrowings = 0
		out, _ := legalEconomics(r, nil)
		c := economic(t, out, "LE_E07")
		if c.Value == nil || *c.Value >= 0 || c.Points != 20 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("loss both years", func(t *testing.T) {
		r := strongReports(t)
		r.current.profit.netProfit = -1
		r.previous.profit.netProfit = -1
		out, manual := legalEconomics(r, nil)
		finiteCriteria(t, out)
		found := false
		for _, m := range manual {
			if m.Code == "TWO_YEAR_NET_LOSS" {
				found = true
			}
		}
		if !found {
			t.Fatal(manual)
		}
	})
}

func TestIPSpecialDenominators(t *testing.T) {
	for _, c := range []struct {
		name, id string
		change   func(*contracts.DecisionRequest, *CashFlow)
		points   float64
		manual   bool
	}{
		{"Q zero", "IP_E04", func(r *contracts.DecisionRequest, f *CashFlow) { f.MonthlyRequired = 0 }, 25, false},
		{"previous receipts zero", "IP_E07", func(r *contracts.DecisionRequest, f *CashFlow) {
			r.IPEconomicData.PreviousComparableReceiptsRUB = ptr(int64(0))
		}, 0, true},
		{"previous quarter zero", "IP_E08", func(r *contracts.DecisionRequest, f *CashFlow) {
			for i := 6; i < 9; i++ {
				f.MonthlyReceipts[i] = 0
			}
		}, 0, true},
		{"cost zero", "IP_E12", func(r *contracts.DecisionRequest, f *CashFlow) { r.Need.ConfirmedCostRUB = ptr(int64(0)) }, 0, true},
		{"contribution margin zero", "IP_E13", func(r *contracts.DecisionRequest, f *CashFlow) { f.Variable = f.Receipts }, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fixture := loadFixture(t, "decisions/uc04_ip_approved.json")
			r := fixture.Request
			data := inspectInput(r, dateOnly(evaluationDate))
			c.change(&r, &data.flow)
			out, manual := ipEconomics(r, data.flow, nil)
			finiteCriteria(t, out)
			got := economic(t, out, c.id)
			if got.Value != nil || got.Points != c.points || (len(manual) > 0) != c.manual {
				t.Fatalf("%+v manual=%+v", got, manual)
			}
		})
	}
	t.Run("zero receipts CFADS", func(t *testing.T) {
		f := loadFixture(t, "decisions/uc04_ip_approved.json")
		out, _ := ipEconomics(f.Request, CashFlow{}, nil)
		finiteCriteria(t, out)
		for _, id := range []string{"IP_E01", "IP_E02", "IP_E03", "IP_E06", "IP_E09", "IP_E10", "IP_E11"} {
			c := economic(t, out, id)
			if c.Value != nil || c.Points != 0 {
				t.Errorf("%+v", c)
			}
		}
	})
	t.Run("zero debt", func(t *testing.T) {
		f := loadFixture(t, "decisions/uc04_ip_approved.json")
		data := inspectInput(f.Request, dateOnly(evaluationDate))
		data.flow.HistoricalDebt = 0
		f.Request.FinancialDebtRUB = ptr(int64(0))
		out, _ := ipEconomics(f.Request, data.flow, nil)
		if economic(t, out, "IP_E02").Points != 30 || economic(t, out, "IP_E03").Points != 25 {
			t.Fatal(out)
		}
	})
}

func TestMaximumBlocksAndSingleRounding(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
		var q contracts.QuestionnaireResult
		if kind == "ip" {
			q = ipQuestionnaire(f.Request)
		} else {
			q = legalQuestionnaire(f.Request)
		}
		max, points := 0, 0
		for _, c := range q.Criteria {
			max += c.MaxPoints
			points += c.Points
		}
		if max != 750 || points != 750 {
			t.Fatalf("%s %d/%d", kind, points, max)
		}
	}
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	r := f.Request
	r.Cash.FreeAfterOwnContributionRUB = ptr(int64(10000000))
	r.Need.OwnContributionRUB = ptr(int64(4000000))
	r.FinancialDebtRUB = ptr(int64(0))
	r.IPEconomicData.PreviousComparableReceiptsRUB = ptr(int64(10000000))
	flow := CashFlow{Receipts: 13200000, CFADS: 12000000, Variable: 1200000, PositiveMonths: 12}
	for i := range flow.MonthlyReceipts {
		flow.MonthlyReceipts[i] = 1000000
	}
	for i := 9; i < 12; i++ {
		flow.MonthlyReceipts[i] = 1400000
	}
	r.Evidence.ConfirmedReceiptsRUB = ptr(int64(flow.Receipts))
	r.Evidence.LargestBuyerGroupReceiptsRUB = ptr(int64(1000000))
	// Keep CV below 10%, and the latest quarter growth above 15%.
	flow.Receipts = 12600000
	flow.CFADS = 11400000
	for i := range flow.MonthlyReceipts {
		flow.MonthlyReceipts[i] = 1000000
	}
	for i := 9; i < 12; i++ {
		flow.MonthlyReceipts[i] = 1200000
	}
	r.Evidence.ConfirmedReceiptsRUB = ptr(int64(flow.Receipts))
	out, _ := ipEconomics(r, flow, nil)
	if out.Points != 250 {
		t.Fatalf("IP points %g %+v", out.Points, out.Criteria)
	}
	maximum := report{balance: balance{nonCurrentAssets: 2000000, inventories: 1000000, receivables: 1000000, shortInvestments: 2000000, cash: 4000000, currentAssets: 8000000, assets: 10000000, equity: 8000000, longLiabilities: 1000000, currentLiabilities: 1000000}, profit: profit{revenue: 30000000, cost: 15000000, salesProfit: 9000000, netProfit: 6000000}}
	old := maximum
	old.profit.revenue = 24000000
	out, _ = legalEconomics(reports{previous: old, current: maximum}, nil)
	if out.Points != 250 {
		t.Fatalf("LE points %g %+v", out.Points, out.Criteria)
	}
	total, grade := totalAndGrade(750 + out.Points)
	if total != 1000 || grade != contracts.GradeA {
		t.Fatalf("%d %s", total, grade)
	}
	total, grade = totalAndGrade(647.5)
	if total != 648 || grade != contracts.GradeC {
		t.Fatalf("%d %s", total, grade)
	}
}
