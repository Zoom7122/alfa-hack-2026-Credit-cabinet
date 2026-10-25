package scoring

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

func TestMissingCommonBusinessFields(t *testing.T) {
	for _, path := range []string{
		"borrower.inn", "borrower.name", "facts_date", "credit_request", "credit_request.purpose", "credit_request.amount_rub", "credit_request.term_months", "credit_request.max_annual_rate", "credit_request.min_acceptable_amount_rub", "credit_request.max_acceptable_term_months",
		"need", "need.confirmed_cost_rub", "need.own_contribution_rub", "cash", "cash.free_after_own_contribution_rub", "cash.minimum_required_rub", "financial_debt_rub", "monthly_history", "monthly_history[*].month", "monthly_history[*].operating_receipts_rub", "monthly_history[*].variable_payments_rub", "monthly_history[*].fixed_payments_rub", "monthly_history[*].taxes_paid_rub", "monthly_history[*].maintenance_investment_rub", "monthly_history[*].owner_required_payments_rub", "monthly_history[*].existing_debt_payment_rub", "future_debt_payments", "future_debt_payments[*].month", "future_debt_payments[*].payment_rub",
		"evidence", "evidence.confirmed_receipts_rub", "evidence.unexplained_difference_rub", "evidence.largest_buyer_group_receipts_rub", "ip_economic_data", "ip_economic_data.previous_comparable_receipts_rub", "scoring", "scoring.ip",
	} {
		t.Run(path, func(t *testing.T) {
			f := loadFixture(t, "decisions/uc04_ip_approved.json")
			mutate(t, &f.Request, change(path, "null"))
			a, err, _ := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != RouteNeedInfo || a.Score != nil {
				t.Fatalf("%+v", a)
			}
		})
	}
}

func TestAllQuestionnaireCountersAndEnumsInvalid(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
		var p any
		branch := "scoring.ip."
		if kind == "ip" {
			p = f.Request.Scoring.IP
		} else {
			p = f.Request.Scoring.LegalEntity
			branch = "scoring.legal_entity."
		}
		typ := reflect.TypeOf(p).Elem()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			base := field.Type.Elem()
			raw := ""
			switch base.Kind() {
			case reflect.Int, reflect.Int64:
				raw = "-1"
			case reflect.String:
				raw = `"UNKNOWN_ENUM"`
			default:
				continue
			}
			t.Run(branch+field.Name, func(t *testing.T) {
				current := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
				mutate(t, &current.Request, change(branch+field.Tag.Get("json"), raw))
				_, err, s := assess(t, current)
				checkError(t, err, 422)
				if s.calls != 0 {
					t.Fatal("B called")
				}
			})
		}
	}
}

func TestQuestionnaireControlsRouting(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		branch := "scoring.ip."
		if kind == "le" {
			branch = "scoring.legal_entity."
		}
		for _, field := range []string{"identity_and_authority_verified", "consents_verified", "bureau_received", "debts_complete", "documents_complete", "free_cash_source_verified"} {
			t.Run(kind+"/"+field, func(t *testing.T) {
				f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
				mutate(t, &f.Request, change(branch+field, "false"))
				a, err, _ := assess(t, f)
				if err != nil {
					t.Fatal(err)
				}
				if a.Route != RouteNeedInfo || a.Score != nil {
					t.Fatalf("%+v", a)
				}
			})
		}
		for _, c := range []struct{ field, raw string }{
			{"credit_history_months", "0"}, {"business_age_months", "11"}, {"current_dpd_days", "29"}, {"applications_90d", "8"},
			{"material_account_restriction", "true"}, {"material_dispute_or_enforcement", "true"}, {"key_buyer_loss", "true"}, {"structural_business_change", "true"}, {"overdue_taxes_rub", "900000000"},
		} {
			t.Run(kind+"/manual/"+c.field, func(t *testing.T) {
				f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
				mutate(t, &f.Request, change(branch+c.field, c.raw))
				a, err, _ := assess(t, f)
				if err != nil {
					t.Fatal(err)
				}
				if a.Route != RouteManual || a.Score == nil {
					t.Fatalf("%+v", a)
				}
			})
		}
	}
	for _, c := range []struct {
		kind, path, raw string
		route           Route
	}{
		{"ip", "scoring.ip.business_location", `"UNVERIFIED"`, RouteNeedInfo}, {"ip", "scoring.ip.income_evidence", `"NONE"`, RouteNeedInfo},
		{"le", "scoring.legal_entity.report_verification_level", "0", RouteNeedInfo}, {"le", "scoring.legal_entity.report_verification_level", "1", RouteNeedInfo},
		{"le", "scoring.legal_entity.organization_and_related_debts_complete", "false", RouteNeedInfo}, {"le", "scoring.legal_entity.auditor_opinion", `"REQUIRED_MISSING"`, RouteNeedInfo},
		{"le", "scoring.legal_entity.need_evidence", `"ORAL_ONLY"`, RouteNeedInfo}, {"le", "scoring.legal_entity.material_register_inaccuracy", "true", RouteManual},
		{"le", "scoring.legal_entity.operating_nonfinancial_company", "false", RouteManual},
	} {
		t.Run(c.path+c.raw, func(t *testing.T) {
			f := loadFixture(t, "decisions/uc04_"+c.kind+"_approved.json")
			mutate(t, &f.Request, change(c.path, c.raw))
			a, err, _ := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != c.route {
				t.Fatalf("%+v", a)
			}
		})
	}
}

func TestOptionalAndConditionalFields(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	f.Request.Scoring.IP.MilitaryStatus = nil
	f.Request.Scoring.IP.BusinessLocation = ptr(contracts.BusinessLocationVerifiedOnlineOrMobile)
	f.Request.Scoring.IP.LocationStabilityMonths = nil
	a, err, _ := assess(t, f)
	if err != nil || a.Route != RouteSelection {
		t.Fatalf("%+v %v", a, err)
	}
	if criterion(t, a.Score.Questionnaire, "IP_Q31").Answer != "OTHER_OR_UNKNOWN" {
		t.Fatal("military default")
	}
	f = loadFixture(t, "decisions/uc04_le_approved.json")
	f.Request.Evidence.LargestBuyerGroupReceiptsRUB = nil
	f.Request.IPEconomicData = &contracts.IpEconomicData{}
	a, err, _ = assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	compareScore(t, a.Score, f.Expected.Response.Score)
	// A debt beyond 36 months does not require extending or balancing the graph.
	f = loadFixture(t, "decisions/uc04_ip_approved.json")
	f.Request.FinancialDebtRUB = ptr(int64(1000000000))
	a, err, _ = assess(t, f)
	if err != nil || a.Route == RouteNeedInfo {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestConfirmedCashStopAndCashTrust(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		branch := "scoring.ip."
		if kind == "le" {
			branch = "scoring.legal_entity."
		}
		for _, cfads := range []int64{0, -1} {
			t.Run(fmt.Sprintf("%s/CFADS=%d", kind, cfads), func(t *testing.T) {
				f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
				for i := range f.Request.MonthlyHistory {
					m := &f.Request.MonthlyHistory[i]
					cost := *m.FixedPaymentsRUB + *m.TaxesPaidRUB + *m.MaintenanceInvestmentRUB + *m.OwnerRequiredPaymentsRUB
					m.VariablePaymentsRUB = ptr(*m.OperatingReceiptsRUB - cost - cfads)
				}
				a, err, s := assess(t, f)
				if err != nil || a.Route != RouteRejected || a.Score != nil || s.calls != 0 || !hasReason(a, "NONPOSITIVE_VERIFIED_CFADS") {
					t.Fatalf("%+v %v B=%d", a, err, s.calls)
				}
				for _, c := range []struct{ path, raw string }{{branch + "documents_complete", "false"}, {branch + "free_cash_source_verified", "false"}, {branch + "debts_complete", "false"}, {"facts_date", `"2026-01-01"`}, {"monthly_history[*].taxes_paid_rub", "null"}, {"evidence.confirmed_receipts_rub", "1"}, {"evidence.unexplained_difference_rub", "9000000000"}} {
					t.Run(c.path, func(t *testing.T) {
						copy := f
						b, _ := json.Marshal(f.Request)
						_ = json.Unmarshal(b, &copy.Request)
						mutate(t, &copy.Request, change(c.path, c.raw))
						result, err, _ := assess(t, copy)
						if err != nil || result.Route != RouteNeedInfo || result.Score != nil {
							t.Fatalf("%+v %v", result, err)
						}
					})
				}
			})
		}
	}
	// Zero receipts, confirmations and costs are known zeros, not missing facts.
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	for i := range f.Request.MonthlyHistory {
		m := &f.Request.MonthlyHistory[i]
		m.OperatingReceiptsRUB = ptr(int64(0))
		m.VariablePaymentsRUB = ptr(int64(0))
		m.FixedPaymentsRUB = ptr(int64(0))
		m.TaxesPaidRUB = ptr(int64(0))
		m.MaintenanceInvestmentRUB = ptr(int64(0))
		m.OwnerRequiredPaymentsRUB = ptr(int64(0))
	}
	f.Request.Evidence = &contracts.Evidence{ConfirmedReceiptsRUB: ptr(int64(0)), UnexplainedDifferenceRUB: ptr(int64(0)), LargestBuyerGroupReceiptsRUB: ptr(int64(0))}
	a, err, s := assess(t, f)
	if err != nil || a.Route != RouteRejected || s.calls != 0 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSourceMoneyAggregationAndThresholds(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	for i := range f.Request.MonthlyHistory {
		m := &f.Request.MonthlyHistory[i]
		m.OperatingReceiptsRUB = ptr(int64(math.MaxInt64))
		m.VariablePaymentsRUB = ptr(int64(math.MaxInt64 - 1))
		m.FixedPaymentsRUB = ptr(int64(0))
		m.TaxesPaidRUB = ptr(int64(0))
		m.MaintenanceInvestmentRUB = ptr(int64(0))
		m.OwnerRequiredPaymentsRUB = ptr(int64(0))
	}
	d := inspectInput(f.Request, dateOnly(evaluationDate))
	if d.flow.CFADS != 12 || d.flow.PositiveMonths != 12 {
		t.Fatalf("CFADS=%g positive=%d", d.flow.CFADS, d.flow.PositiveMonths)
	}
	// Exact original-unit checks distinguish R from R+1 above float precision.
	f.Request.MonthlyHistory = f.Request.MonthlyHistory[:1]
	m := &f.Request.MonthlyHistory[0]
	m.OperatingReceiptsRUB = ptr(int64(9007199254740992))
	m.VariablePaymentsRUB = ptr(int64(0))
	var in inputData
	history(f.Request, dateOnly(evaluationDate), &in)
	if compareRatio(9007199254740993, &in.receiptsRUB, 1, 1) <= 0 {
		t.Fatal("one ruble lost")
	}
	// Exact 80%, 5% and 10% pass their inclusive limits.
	for _, c := range []struct {
		confirmed, difference int64
		route                 Route
	}{{19200000, 1200000, RouteSelection}, {19200000, 1200001, RouteManual}, {19200000, 2400000, RouteManual}, {19199999, 0, RouteNeedInfo}, {24000000, 2400001, RouteNeedInfo}} {
		g := loadFixture(t, "decisions/uc04_ip_approved.json")
		g.Request.Evidence.ConfirmedReceiptsRUB = &c.confirmed
		g.Request.Evidence.UnexplainedDifferenceRUB = &c.difference
		a, err, _ := assess(t, g)
		if err != nil || a.Route != c.route {
			t.Fatalf("case=%+v result=%+v err=%v", c, a, err)
		}
	}
}

func TestRawFieldsOnlyAndNoMutation(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	b, _ := json.Marshal(f.Request)
	var tree map[string]any
	_ = json.Unmarshal(b, &tree)
	tree["score"] = 0
	tree["grade"] = "D"
	tree["ratio"] = 0
	tree["scoring"].(map[string]any)["ip"].(map[string]any)["points"] = 0
	b, _ = json.Marshal(tree)
	r, err := Decode(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(r)
	e, _ := NewEvaluator(nil, clock.Fixed{Time: evaluationDate})
	type evaluation struct {
		a   Assessment
		err error
	}
	results := make(chan evaluation, 16)
	for i := 0; i < 16; i++ {
		go func() {
			a, err := e.Evaluate(context.Background(), r)
			results <- evaluation{a, err}
		}()
	}
	for i := 0; i < 16; i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		compareScore(t, result.a.Score, f.Expected.Response.Score)
	}
	after, _ := json.Marshal(r)
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
}

func TestCalendarOrderingAndActualCashMonths(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
		for i := 0; i < 5; i++ {
			m := &f.Request.MonthlyHistory[i]
			m.VariablePaymentsRUB = ptr(*m.OperatingReceiptsRUB - *m.FixedPaymentsRUB - *m.TaxesPaidRUB - *m.MaintenanceInvestmentRUB - *m.OwnerRequiredPaymentsRUB + 1)
		}
		a, err, _ := assess(t, f)
		if err != nil || a.Route != RouteManual || !hasReason(a, "FEWER_THAN_8_POSITIVE_CFADS_MONTHS") {
			t.Fatalf("%+v %v", a, err)
		}
	}
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	before, err, _ := assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(f.Request.MonthlyHistory)-1; i < j; i, j = i+1, j-1 {
		f.Request.MonthlyHistory[i], f.Request.MonthlyHistory[j] = f.Request.MonthlyHistory[j], f.Request.MonthlyHistory[i]
	}
	for i, j := 0, len(f.Request.FutureDebtPayments)-1; i < j; i, j = i+1, j-1 {
		f.Request.FutureDebtPayments[i], f.Request.FutureDebtPayments[j] = f.Request.FutureDebtPayments[j], f.Request.FutureDebtPayments[i]
	}
	a, err, _ := assess(t, f)
	if err != nil {
		t.Fatal(err)
	}
	compareScore(t, a.Score, before.Score)
	if !reflect.DeepEqual(a.CashFlow, before.CashFlow) {
		t.Fatal("cash flow must follow calendar dates")
	}
	for _, c := range []struct{ path, date string }{{"monthly_history[*].month", "2026-10"}, {"monthly_history[*].month", "2025-09"}, {"future_debt_payments[*].month", "2026-10"}, {"future_debt_payments[*].month", "2029-11"}} {
		g := loadFixture(t, "decisions/uc04_ip_approved.json")
		mutate(t, &g.Request, change(c.path, `"`+c.date+`"`))
		a, err, _ := assess(t, g)
		if err != nil || a.Route != RouteNeedInfo {
			t.Fatalf("%+v %v", a, err)
		}
	}
	f = loadFixture(t, "decisions/uc04_ip_approved.json")
	f.Request.FactsDate = ptr("2026-09-08")
	a, err, _ = assess(t, f)
	if err != nil || a.Route != RouteSelection {
		t.Fatalf("45 day boundary: %+v %v", a, err)
	}
}
