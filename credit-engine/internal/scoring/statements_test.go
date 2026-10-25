package scoring

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

func TestStatementsSourceFailures(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	for _, c := range []struct {
		name   string
		err    error
		status int
	}{
		{"404", ErrStatementsNotFound, 0}, {"unavailable", errors.New("connection failure"), 502}, {"timeout", context.DeadlineExceeded, 504}, {"invalid response", &Error{Status: 502, Code: "INVALID_STATEMENTS"}, 502},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &sourceStub{response: f.Statements, err: c.err, t: t}
			e, _ := NewEvaluator(s, clock.Fixed{Time: evaluationDate})
			a, err := e.Evaluate(context.Background(), f.Request)
			if c.status != 0 {
				checkError(t, err, c.status)
			} else if err != nil || a.Route != RouteNeedInfo || a.Score != nil {
				t.Fatalf("%+v %v", a, err)
			}
			if s.calls != 1 {
				t.Fatal(s.calls)
			}
		})
	}
	e, _ := NewEvaluator(nil, clock.Fixed{Time: evaluationDate})
	_, err := e.Evaluate(context.Background(), f.Request)
	checkError(t, err, 502)
	if _, err = NewEvaluator(nil, nil); err == nil {
		t.Fatal("nil clock accepted")
	}
	for _, raw := range []string{`{`, `{"annual_reports":0}`, `{"annual_reports":[{"year":"2025"}]}`, `{"annual_reports":[{"balance":{"total_assets":1.5}}]}`, `{} {}`} {
		_, err := DecodeStatements(strings.NewReader(raw))
		checkError(t, err, 502)
	}
}

func TestStatementRequiredLinesAndOptionalDetails(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	for _, year := range []int{0, 1} {
		for _, group := range []string{"balance", "profit_and_loss"} {
			var lines []statementLine
			if group == "balance" {
				lines = balanceLines(f.Statements.AnnualReports[year].Balance)
			} else {
				lines = profitLines(f.Statements.AnnualReports[year].ProfitAndLoss)
			}
			for _, line := range lines {
				if !line.required {
					continue
				}
				t.Run(time.Date(2024+year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006")+"/"+group+"/"+line.name, func(t *testing.T) {
					current := loadFixture(t, "decisions/uc04_le_approved.json")
					a := &current.Statements.AnnualReports[year]
					if group == "balance" {
						for _, l := range balanceLines(a.Balance) {
							if l.name == line.name {
								clearPointerField(a.Balance, l.value)
							}
						}
					} else {
						for _, l := range profitLines(a.ProfitAndLoss) {
							if l.name == line.name {
								clearPointerField(a.ProfitAndLoss, l.value)
							}
						}
					}
					result, err, _ := assess(t, current)
					if err != nil {
						t.Fatal(err)
					}
					if result.Route != RouteNeedInfo || result.Score != nil {
						t.Fatalf("%+v", result)
					}
				})
			}
		}
	}
	for i := range f.Statements.AnnualReports {
		a := &f.Statements.AnnualReports[i]
		for _, line := range balanceLines(a.Balance) {
			if !line.required {
				clearPointerField(a.Balance, line.value)
			}
		}
		for _, line := range profitLines(a.ProfitAndLoss) {
			if !line.required {
				clearPointerField(a.ProfitAndLoss, line.value)
			}
		}
	}
	a, err, _ := assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	compareScore(t, a.Score, f.Expected.Response.Score)
}

// Reflection is only a fixture mutation helper; production validation is typed.
func clearPointerField(object any, p *int64) {
	v := reflect.ValueOf(object).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Type() == reflect.TypeOf(p) && !f.IsNil() && f.Interface().(*int64) == p {
			f.SetZero()
			return
		}
	}
}

func TestStatementConsistencyAndUnits(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*fixture)
		status int
		route  Route
	}{
		{"wrong borrower", func(f *fixture) { f.Statements.BorrowerID = "other" }, 502, ""},
		{"unit unknown", func(f *fixture) { f.Statements.AnnualReports[1].Balance.Unit = ptr(contracts.StatementUnit("USD")) }, 502, ""},
		{"unit missing", func(f *fixture) { f.Statements.AnnualReports[1].Balance.Unit = nil }, 0, RouteNeedInfo},
		{"year missing", func(f *fixture) { f.Statements.AnnualReports = f.Statements.AnnualReports[:1] }, 0, RouteNeedInfo},
		{"duplicate year", func(f *fixture) { f.Statements.AnnualReports[1].Year = ptr(2024) }, 0, RouteNeedInfo},
		{"period end", func(f *fixture) { f.Statements.AnnualReports[1].PeriodEnd = ptr("2025-12-30") }, 0, RouteNeedInfo},
		{"balance missing", func(f *fixture) { f.Statements.AnnualReports[1].Balance = nil }, 0, RouteNeedInfo},
		{"P and L missing", func(f *fixture) { f.Statements.AnnualReports[1].ProfitAndLoss = nil }, 0, RouteNeedInfo},
		{"negative asset", func(f *fixture) { f.Statements.AnnualReports[1].Balance.Inventories = ptr(int64(-1)) }, 0, RouteNeedInfo},
		{"negative expense", func(f *fixture) { f.Statements.AnnualReports[1].ProfitAndLoss.InterestPayable = ptr(int64(-1)) }, 0, RouteNeedInfo},
		{"different assets liabilities", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.TotalEquityAndLiabilities += 2 }, 0, RouteNeedInfo},
		{"asset sections", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.NonCurrentAssets += 2 }, 0, RouteNeedInfo},
		{"liability sections", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.Equity += 2 }, 0, RouteNeedInfo},
		{"asset detail", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.FixedAssets -= 2 }, 0, RouteNeedInfo},
		{"capital detail", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.TreasuryShares += 2 }, 0, RouteNeedInfo},
		{"part exceeds total", func(f *fixture) {
			f.Statements.AnnualReports[1].Balance.OtherCurrentAssets = nil
			*f.Statements.AnnualReports[1].Balance.Inventories = 99999999
		}, 0, RouteNeedInfo},
		{"sales reconciliation", func(f *fixture) { *f.Statements.AnnualReports[1].ProfitAndLoss.SalesProfit += 2 }, 0, RouteNeedInfo},
		{"gross reconciliation", func(f *fixture) { *f.Statements.AnnualReports[1].ProfitAndLoss.GrossProfit += 2 }, 0, RouteNeedInfo},
		{"one unit tolerance", func(f *fixture) { *f.Statements.AnnualReports[1].Balance.TotalEquityAndLiabilities += 1 }, 0, RouteSelection},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := loadFixture(t, "decisions/uc04_le_approved.json")
			c.mutate(&f)
			a, err, _ := assess(t, f)
			if c.status != 0 {
				checkError(t, err, c.status)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != c.route {
				t.Fatalf("got %+v", a)
			}
		})
	}
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	for i := range f.Statements.AnnualReports {
		a := &f.Statements.AnnualReports[i]
		for _, line := range balanceLines(a.Balance) {
			if line.value != nil {
				*line.value *= 1000
			}
		}
		for _, line := range profitLines(a.ProfitAndLoss) {
			if line.value != nil {
				*line.value *= 1000
			}
		}
		a.Balance.Unit = ptr(contracts.StatementUnitRUB)
		a.ProfitAndLoss.Unit = ptr(contracts.StatementUnitRUB)
	}
	a, err, _ := assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	compareScore(t, a.Score, f.Expected.Response.Score)
	// Profit and balance can use different declared units.
	f = loadFixture(t, "decisions/uc04_le_approved.json")
	for i := range f.Statements.AnnualReports {
		a := &f.Statements.AnnualReports[i]
		for _, line := range profitLines(a.ProfitAndLoss) {
			if line.value != nil {
				*line.value *= 1000
			}
		}
		a.ProfitAndLoss.Unit = ptr(contracts.StatementUnitRUB)
	}
	a, err, _ = assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	compareScore(t, a.Score, f.Expected.Response.Score)
}

func TestExactStatementReconciliation(t *testing.T) {
	max := int64(math.MaxInt64)
	if !equation(&max, []*int64{&max, &max}, []*int64{&max}) {
		t.Fatal("exact cancellation overflow")
	}
	if equation(&max, []*int64{&max, &max}, nil) {
		t.Fatal("overflow accepted")
	}
	if partsFit(&max, &max, &max) {
		t.Fatal("parts overflow accepted")
	}
	if !equation(ptr(int64(1000)), []*int64{ptr(int64(999))}, nil) || equation(ptr(int64(1000)), []*int64{ptr(int64(998))}, nil) {
		t.Fatal("one original unit tolerance")
	}
}

func TestSignedCapitalAndProfitAreBusinessFacts(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	for i := range f.Statements.AnnualReports {
		b := f.Statements.AnnualReports[i].Balance
		b.Equity = ptr(int64(-1000))
		b.RetainedEarnings = ptr(int64(-2000))
		b.LongTermLiabilities = ptr(*b.TotalAssets - *b.Equity - *b.CurrentLiabilities)
		b.OtherLongTermLiabilities = ptr(*b.LongTermLiabilities - *b.LongTermBorrowings)
	}
	a, err, _ := assess(t, f)
	if err != nil || a.Route != RouteManual || a.Score == nil || !hasReason(a, "NONPOSITIVE_EQUITY") {
		t.Fatalf("%+v %v", a, err)
	}
	if economic(t, a.Score.Economics, "LE_E14").Value != nil {
		t.Fatal("ROE must not be interpreted")
	}
	f = loadFixture(t, "decisions/uc04_le_approved.json")
	for i := range f.Statements.AnnualReports {
		p := f.Statements.AnnualReports[i].ProfitAndLoss
		p.NetProfit = ptr(int64(-1000))
		p.IncomeTax = ptr(*p.ProfitBeforeTax + 1000)
	}
	a, err, _ = assess(t, f)
	if err != nil || a.Route != RouteManual || !hasReason(a, "TWO_YEAR_NET_LOSS") {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestFullDetailReconciliationTolerance(t *testing.T) {
	for _, delta := range []int64{-2, -1, 1, 2} {
		f := loadFixture(t, "decisions/uc04_le_approved.json")
		b := f.Statements.AnnualReports[1].Balance
		b.FixedAssets = ptr(int64(7000))
		b.OtherNonCurrentAssets = ptr(int64(1000 + delta))
		a, err, _ := assess(t, f)
		if err != nil {
			t.Fatal(err)
		}
		want := RouteSelection
		if delta == 2 || delta == -2 {
			want = RouteNeedInfo
		}
		if a.Route != want {
			t.Fatalf("delta=%d result=%+v", delta, a)
		}
	}
}
