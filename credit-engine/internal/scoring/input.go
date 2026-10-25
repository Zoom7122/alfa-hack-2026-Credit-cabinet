package scoring

import (
	"fmt"
	"math/big"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

type inputData struct {
	missing                                                     []string
	manual                                                      []contracts.Reason
	flow                                                        CashFlow
	cashConfirmed                                               bool
	receiptsRUB                                                 big.Int
	cfadsRUB                                                    big.Int
	variableRUB, fixedRUB, historicalDebtRUB, annualRequiredRUB big.Int
}

func knownStops(r contracts.DecisionRequest) []contracts.Reason {
	p, s := risk(r)
	var out []contracts.Reason
	add := func(ok bool, code, field string) {
		if ok {
			out = append(out, reason(code, routingMessage(code), s+field))
		}
	}
	add(p.active != nil && !*p.active, "INACTIVE_BORROWER", "active")
	add(yes(p.bankrupt), "BANKRUPT", "bankrupt")
	add(yes(p.forgery), "MATERIAL_FORGERY", "material_forgery")
	add(yes(p.prohibited), "CREDIT_PROHIBITED", "credit_prohibited")
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity && r.Scoring != nil && r.Scoring.LegalEntity != nil {
		add(yes(r.Scoring.LegalEntity.LiquidatedOrExcluded), "LIQUIDATED_OR_EXCLUDED", "liquidated_or_excluded")
	}
	add(p.currentDPD != nil && *p.currentDPD >= 30, "CURRENT_DPD_30_PLUS", "current_dpd_days")
	add(p.worstDPD != nil && *p.worstDPD >= 90, "WORST_REPAID_DPD_90_PLUS", "worst_repaid_dpd_days_24m")
	return out
}

func monthPath(block string, index int, month *string) string {
	if month != nil {
		return block + "." + *month
	}
	return fmt.Sprintf("%s[%d]", block, index)
}

func inspectInput(r contracts.DecisionRequest, now time.Time) inputData {
	d := inputData{missing: inspectValues(r).missing}
	p, s := risk(r)
	need := func(ok bool, field string) {
		if !ok {
			d.missing = append(d.missing, field)
		}
	}
	manual := func(ok bool, code, field string) {
		if ok {
			d.manual = append(d.manual, reason(code, routingMessage(code), field))
		}
	}
	factsOK := false
	if r.FactsDate != nil {
		date, _ := time.Parse("2006-01-02", *r.FactsDate)
		factsOK = !date.After(now) && now.Sub(date) <= 45*24*time.Hour
		need(factsOK, "facts_date")
	}
	for _, f := range []struct {
		name string
		p    *bool
	}{
		{"identity_and_authority_verified", p.identity}, {"consents_verified", p.consents}, {"bureau_received", p.bureau}, {"debts_complete", p.debts}, {"documents_complete", p.documents}, {"free_cash_source_verified", p.freeCash},
	} {
		need(yes(f.p), s+f.name)
	}
	historyOK := history(r, now, &d)
	evidenceOK := false
	if historyOK && r.Evidence != nil {
		e := r.Evidence
		R := d.flow.Receipts
		evidenceOK = e.ConfirmedReceiptsRUB != nil && e.UnexplainedDifferenceRUB != nil
		if e.ConfirmedReceiptsRUB != nil {
			ok := big.NewInt(*e.ConfirmedReceiptsRUB).Cmp(&d.receiptsRUB) <= 0 && (R == 0 || compareRatio(*e.ConfirmedReceiptsRUB, &d.receiptsRUB, 4, 5) >= 0)
			need(ok, "evidence.confirmed_receipts_rub")
			evidenceOK = evidenceOK && ok
		}
		if e.UnexplainedDifferenceRUB != nil {
			ok := R > 0 && compareRatio(*e.UnexplainedDifferenceRUB, &d.receiptsRUB, 1, 10) <= 0 || R == 0 && *e.UnexplainedDifferenceRUB == 0
			need(ok, "evidence.unexplained_difference_rub")
			evidenceOK = evidenceOK && ok
			manual(ok && R > 0 && compareRatio(*e.UnexplainedDifferenceRUB, &d.receiptsRUB, 1, 20) > 0, "UNEXPLAINED_DIFFERENCE", "evidence.unexplained_difference_rub")
		}
		if e.LargestBuyerGroupReceiptsRUB != nil {
			ok := big.NewInt(*e.LargestBuyerGroupReceiptsRUB).Cmp(&d.receiptsRUB) <= 0
			need(ok, "evidence.largest_buyer_group_receipts_rub")
			evidenceOK = evidenceOK && ok
		} else if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
			evidenceOK = false
		}
	}
	debtsOK := yes(p.debts)
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity && r.Scoring != nil && r.Scoring.LegalEntity != nil {
		debtsOK = debtsOK && yes(r.Scoring.LegalEntity.OrganizationAndRelatedDebtsComplete)
	}
	d.cashConfirmed = historyOK && factsOK && evidenceOK && yes(p.documents) && yes(p.freeCash) && debtsOK
	if historyOK && d.flow.CFADS <= 0 && !d.cashConfirmed {
		need(false, "monthly_history")
	}
	future(r, now, &d)
	if r.CreditRequest != nil {
		c := r.CreditRequest
		if c.AmountRUB != nil && c.MinAcceptableAmountRUB != nil {
			need(*c.MinAcceptableAmountRUB <= *c.AmountRUB, "credit_request.min_acceptable_amount_rub")
		}
		if c.TermMonths != nil && c.MaxAcceptableTermMonths != nil {
			need(*c.MaxAcceptableTermMonths >= *c.TermMonths, "credit_request.max_acceptable_term_months")
		}
		manual(c.Purpose != nil && *c.Purpose != "BUSINESS_DEVELOPMENT", "NONSTANDARD_PURPOSE", "credit_request.purpose")
	}
	if r.Need != nil && r.Need.ConfirmedCostRUB != nil && r.Need.OwnContributionRUB != nil {
		manual(*r.Need.ConfirmedCostRUB <= *r.Need.OwnContributionRUB, "NO_UNCOVERED_NEED", "need")
	}
	if r.Cash != nil && r.Cash.FreeAfterOwnContributionRUB != nil && r.Cash.MinimumRequiredRUB != nil {
		manual(*r.Cash.FreeAfterOwnContributionRUB < *r.Cash.MinimumRequiredRUB, "INSUFFICIENT_FREE_CASH", "cash")
	}
	manual(p.history != nil && *p.history == 0, "NO_CREDIT_HISTORY", s+"credit_history_months")
	manual(p.business != nil && *p.business < 12, "BUSINESS_AGE_UNDER_12_MONTHS", s+"business_age_months")
	manual(p.currentDPD != nil && *p.currentDPD > 0 && *p.currentDPD < 30, "CURRENT_DPD_1_29", s+"current_dpd_days")
	manual(p.applications != nil && *p.applications >= 8, "MANY_CREDIT_APPLICATIONS", s+"applications_90d")
	manual(yes(p.restriction), "MATERIAL_ACCOUNT_RESTRICTION", s+"material_account_restriction")
	manual(yes(p.dispute), "MATERIAL_DISPUTE_OR_ENFORCEMENT", s+"material_dispute_or_enforcement")
	manual(yes(p.buyerLoss), "KEY_BUYER_LOSS", s+"key_buyer_loss")
	manual(yes(p.change), "STRUCTURAL_BUSINESS_CHANGE", s+"structural_business_change")
	manual(historyOK && p.taxes != nil && compareRatio(*p.taxes, &d.cfadsRUB, 1, 10) > 0, "OVERDUE_TAXES_OVER_10_PERCENT_CFADS", s+"overdue_taxes_rub")
	manual(historyOK && d.flow.PositiveMonths < 8, "FEWER_THAN_8_POSITIVE_CFADS_MONTHS", "monthly_history")
	if r.Scoring != nil && r.Scoring.IP != nil {
		ip := r.Scoring.IP
		need(val(ip.BusinessLocation) != contracts.BusinessLocationUnverified, s+"business_location")
		need(val(ip.IncomeEvidence) != contracts.IncomeEvidenceNone, s+"income_evidence")
	}
	if r.Scoring != nil && r.Scoring.LegalEntity != nil {
		le := r.Scoring.LegalEntity
		need(le.ReportVerificationLevel != nil && *le.ReportVerificationLevel >= 2, s+"report_verification_level")
		need(yes(le.OrganizationAndRelatedDebtsComplete), s+"organization_and_related_debts_complete")
		need(val(le.AuditorOpinion) != contracts.AuditorOpinionRequiredMissing, s+"auditor_opinion")
		need(val(le.NeedEvidence) != contracts.NeedEvidenceOralOnly, s+"need_evidence")
		manual(yes(le.MaterialRegisterInaccuracy), "MATERIAL_REGISTER_INACCURACY", s+"material_register_inaccuracy")
		manual(val(le.AuditorOpinion) == contracts.AuditorOpinionMaterialModification, "MATERIAL_AUDITOR_MODIFICATION", s+"auditor_opinion")
		manual(le.OperatingNonfinancialCompany != nil && !*le.OperatingNonfinancialCompany, "NON_OPERATING_NONFINANCIAL_SEGMENT", s+"operating_nonfinancial_company")
	}
	return d
}

func history(r contracts.DecisionRequest, now time.Time, d *inputData) bool {
	complete := len(r.MonthlyHistory) == 12
	if !complete {
		d.missing = append(d.missing, "monthly_history")
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -12, 0)
	var seen [12]bool
	var annualCFADS, annualVariable, annualFixed, annualDebt, annualRequired big.Int
	for _, m := range r.MonthlyHistory {
		if m.Month == nil {
			complete = false
			continue
		}
		t, err := time.Parse("2006-01", *m.Month)
		idx := (t.Year()-start.Year())*12 + int(t.Month()-start.Month())
		if err != nil || idx < 0 || idx >= 12 || seen[idx] {
			complete = false
			d.missing = append(d.missing, "monthly_history")
			continue
		}
		seen[idx] = true
		if m.OperatingReceiptsRUB == nil || m.VariablePaymentsRUB == nil || m.FixedPaymentsRUB == nil || m.TaxesPaidRUB == nil || m.MaintenanceInvestmentRUB == nil || m.OwnerRequiredPaymentsRUB == nil || m.ExistingDebtPaymentRUB == nil {
			complete = false
			continue
		}
		R, V := big.NewInt(*m.OperatingReceiptsRUB), big.NewInt(*m.VariablePaymentsRUB)
		K := sumRUB(*m.FixedPaymentsRUB, *m.TaxesPaidRUB, *m.MaintenanceInvestmentRUB, *m.OwnerRequiredPaymentsRUB)
		F := new(big.Int).Sub(R, V)
		F.Sub(F, K)
		d.flow.MonthlyReceipts[idx], _ = R.Float64()
		d.flow.MonthlyCFADS[idx], _ = F.Float64()
		d.receiptsRUB.Add(&d.receiptsRUB, R)
		annualCFADS.Add(&annualCFADS, F)
		annualVariable.Add(&annualVariable, V)
		annualFixed.Add(&annualFixed, K)
		annualDebt.Add(&annualDebt, big.NewInt(*m.ExistingDebtPaymentRUB))
		annualRequired.Add(&annualRequired, sumRUB(*m.FixedPaymentsRUB, *m.OwnerRequiredPaymentsRUB, *m.ExistingDebtPaymentRUB))
		if F.Sign() > 0 {
			d.flow.PositiveMonths++
		}
	}
	d.flow.Receipts, _ = d.receiptsRUB.Float64()
	d.cfadsRUB.Set(&annualCFADS)
	d.variableRUB.Set(&annualVariable)
	d.fixedRUB.Set(&annualFixed)
	d.historicalDebtRUB.Set(&annualDebt)
	d.annualRequiredRUB.Set(&annualRequired)
	d.flow.CFADS, _ = annualCFADS.Float64()
	d.flow.Variable, _ = annualVariable.Float64()
	d.flow.Fixed, _ = annualFixed.Float64()
	d.flow.HistoricalDebt, _ = annualDebt.Float64()
	required, _ := annualRequired.Float64()
	d.flow.MonthlyRequired = required / 12
	return complete
}

// Integer source money is aggregated before conversion for ratios. In
// particular, a positive one-ruble CFADS cannot become a false zero stop.
func sumRUB(values ...int64) *big.Int {
	out := new(big.Int)
	for _, v := range values {
		out.Add(out, big.NewInt(v))
	}
	return out
}
func compareRatio(n int64, d *big.Int, numerator, denominator int64) int {
	left := new(big.Int).Mul(big.NewInt(n), big.NewInt(denominator))
	right := new(big.Int).Mul(d, big.NewInt(numerator))
	return left.Cmp(right)
}
func future(r contracts.DecisionRequest, now time.Time, d *inputData) {
	if len(r.FutureDebtPayments) != 36 {
		d.missing = append(d.missing, "future_debt_payments")
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	var seen [36]bool
	for _, m := range r.FutureDebtPayments {
		if m.Month == nil {
			continue
		}
		t, err := time.Parse("2006-01", *m.Month)
		idx := (t.Year()-start.Year())*12 + int(t.Month()-start.Month())
		if err != nil || idx < 0 || idx >= 36 || seen[idx] {
			d.missing = append(d.missing, "future_debt_payments")
			continue
		}
		seen[idx] = true
		if m.PaymentRUB != nil {
			d.flow.FutureDebt[idx] = float64(*m.PaymentRUB)
			if r.FinancialDebtRUB != nil && *r.FinancialDebtRUB == 0 && *m.PaymentRUB > 0 {
				d.missing = append(d.missing, "financial_debt_rub", "future_debt_payments")
			}
		}
	}
}
