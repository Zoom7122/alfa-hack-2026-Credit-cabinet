package scoring

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

// Decode accepts unknown extra fields, but not invalid types or trailing JSON.
func Decode(reader io.Reader) (contracts.DecisionRequest, error) {
	var r contracts.DecisionRequest
	d := json.NewDecoder(reader)
	if err := d.Decode(&r); err != nil {
		return r, &Error{Status: 400, Code: "INVALID_JSON"}
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return r, &Error{Status: 400, Code: "INVALID_JSON"}
	}
	return r, Validate(r)
}

type inputCheck struct{ invalid, missing []string }

func checkNumber[T ~int | ~int64](c *inputCheck, path string, p *T) {
	if p == nil {
		c.missing = append(c.missing, path)
	} else if *p < 0 {
		c.invalid = append(c.invalid, path)
	}
}
func checkBool(c *inputCheck, path string, p *bool) {
	if p == nil {
		c.missing = append(c.missing, path)
	}
}
func checkEnum[T ~string](c *inputCheck, path string, p *T, allowed ...T) {
	if p == nil {
		c.missing = append(c.missing, path)
		return
	}
	for _, a := range allowed {
		if *p == a {
			return
		}
	}
	c.invalid = append(c.invalid, path)
}
func checkDate(c *inputCheck, path string, p *string, layout string) {
	if p == nil {
		c.missing = append(c.missing, path)
		return
	}
	if _, err := time.Parse(layout, *p); err != nil {
		c.invalid = append(c.invalid, path)
	}
}
func checkString(c *inputCheck, path string, p *string) {
	if p == nil || strings.TrimSpace(*p) == "" {
		c.missing = append(c.missing, path)
	}
}

// Validate checks the technical envelope and inadmissible values before routing.
// Missing business facts are collected separately by Evaluate as NEED_INFO.
func Validate(r contracts.DecisionRequest) error {
	var technical []string
	if strings.TrimSpace(r.ApplicationID) == "" {
		technical = append(technical, "application_id")
	}
	if strings.TrimSpace(r.Borrower.ID) == "" {
		technical = append(technical, "borrower.id")
	}
	if r.Borrower.Type == "" {
		technical = append(technical, "borrower.type")
	}
	if len(technical) > 0 {
		return &Error{Status: 400, Code: "MISSING_ENVELOPE", Fields: technical}
	}
	c := inspectValues(r)
	if len(c.invalid) > 0 {
		return &Error{Status: 422, Code: "INVALID_VALUE", Fields: unique(c.invalid)}
	}
	return nil
}

func inspectValues(r contracts.DecisionRequest) inputCheck {
	c := inputCheck{}
	checkEnum(&c, "borrower.type", &r.Borrower.Type, contracts.BorrowerTypeSoleProprietor, contracts.BorrowerTypeLegalEntity)
	checkString(&c, "borrower.inn", r.Borrower.INN)
	checkString(&c, "borrower.name", r.Borrower.Name)
	checkDate(&c, "facts_date", r.FactsDate, "2006-01-02")
	if p := r.CreditRequest; p == nil {
		c.missing = append(c.missing, "credit_request")
	} else {
		checkString(&c, "credit_request.purpose", p.Purpose)
		checkNumber(&c, "credit_request.amount_rub", p.AmountRUB)
		checkNumber(&c, "credit_request.min_acceptable_amount_rub", p.MinAcceptableAmountRUB)
		checkNumber(&c, "credit_request.term_months", p.TermMonths)
		checkNumber(&c, "credit_request.max_acceptable_term_months", p.MaxAcceptableTermMonths)
		if p.AmountRUB != nil && *p.AmountRUB == 0 {
			c.invalid = append(c.invalid, "credit_request.amount_rub")
		}
		if p.MinAcceptableAmountRUB != nil && *p.MinAcceptableAmountRUB == 0 {
			c.invalid = append(c.invalid, "credit_request.min_acceptable_amount_rub")
		}
		if p.TermMonths != nil && *p.TermMonths == 0 {
			c.invalid = append(c.invalid, "credit_request.term_months")
		}
		if p.MaxAcceptableTermMonths != nil && *p.MaxAcceptableTermMonths == 0 {
			c.invalid = append(c.invalid, "credit_request.max_acceptable_term_months")
		}
		if p.MaxAnnualRate == nil {
			c.missing = append(c.missing, "credit_request.max_annual_rate")
		} else if _, err := economics.BasisPointsFromPercent(*p.MaxAnnualRate); err != nil {
			c.invalid = append(c.invalid, "credit_request.max_annual_rate")
		}
	}
	if p := r.Need; p == nil {
		c.missing = append(c.missing, "need")
	} else {
		checkNumber(&c, "need.confirmed_cost_rub", p.ConfirmedCostRUB)
		checkNumber(&c, "need.own_contribution_rub", p.OwnContributionRUB)
	}
	if p := r.Cash; p == nil {
		c.missing = append(c.missing, "cash")
	} else {
		checkNumber(&c, "cash.free_after_own_contribution_rub", p.FreeAfterOwnContributionRUB)
		checkNumber(&c, "cash.minimum_required_rub", p.MinimumRequiredRUB)
	}
	checkNumber(&c, "financial_debt_rub", r.FinancialDebtRUB)
	for i, m := range r.MonthlyHistory {
		path := monthPath("monthly_history", i, m.Month)
		checkDate(&c, path+".month", m.Month, "2006-01")
		checkNumber(&c, path+".operating_receipts_rub", m.OperatingReceiptsRUB)
		checkNumber(&c, path+".variable_payments_rub", m.VariablePaymentsRUB)
		checkNumber(&c, path+".fixed_payments_rub", m.FixedPaymentsRUB)
		checkNumber(&c, path+".taxes_paid_rub", m.TaxesPaidRUB)
		checkNumber(&c, path+".maintenance_investment_rub", m.MaintenanceInvestmentRUB)
		checkNumber(&c, path+".owner_required_payments_rub", m.OwnerRequiredPaymentsRUB)
		checkNumber(&c, path+".existing_debt_payment_rub", m.ExistingDebtPaymentRUB)
	}
	for i, m := range r.FutureDebtPayments {
		path := monthPath("future_debt_payments", i, m.Month)
		checkDate(&c, path+".month", m.Month, "2006-01")
		checkNumber(&c, path+".payment_rub", m.PaymentRUB)
	}
	if p := r.Evidence; p == nil {
		c.missing = append(c.missing, "evidence")
	} else {
		checkNumber(&c, "evidence.confirmed_receipts_rub", p.ConfirmedReceiptsRUB)
		checkNumber(&c, "evidence.unexplained_difference_rub", p.UnexplainedDifferenceRUB)
		if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor || p.LargestBuyerGroupReceiptsRUB != nil {
			checkNumber(&c, "evidence.largest_buyer_group_receipts_rub", p.LargestBuyerGroupReceiptsRUB)
		}
	}
	if p := r.IPEconomicData; p != nil {
		if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor || p.PreviousComparableReceiptsRUB != nil {
			checkNumber(&c, "ip_economic_data.previous_comparable_receipts_rub", p.PreviousComparableReceiptsRUB)
		}
	} else if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
		c.missing = append(c.missing, "ip_economic_data")
	}
	if r.Scoring == nil {
		c.missing = append(c.missing, "scoring")
		return c
	}
	if r.Scoring.IP != nil && (r.Borrower.Type != contracts.BorrowerTypeSoleProprietor || r.Scoring.LegalEntity != nil) || r.Scoring.LegalEntity != nil && r.Borrower.Type != contracts.BorrowerTypeLegalEntity {
		c.invalid = append(c.invalid, "scoring")
	}
	if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
		if r.Scoring.IP == nil {
			c.missing = append(c.missing, "scoring.ip")
		} else {
			checkIP(&c, r.Scoring.IP)
		}
	} else if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		if r.Scoring.LegalEntity == nil {
			c.missing = append(c.missing, "scoring.legal_entity")
		} else {
			checkLegal(&c, r.Scoring.LegalEntity)
		}
	}
	return c
}
