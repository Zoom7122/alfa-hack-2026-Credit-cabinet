// Package sourcecatalog lists the existing, trusted v2 input bindings and
// calculations. An editor may select these sources, but cannot add formulas.
package sourcecatalog

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

// Entry also supplies initial editor metadata. Parameters returns fresh values;
// changing a draft cannot change the trusted bindings of another request.
type Entry struct {
	Parameter model.Parameter
	Binding   model.Binding
}

func Entries(borrower contracts.BorrowerType) ([]Entry, error) {
	if borrower != contracts.BorrowerTypeSoleProprietor && borrower != contracts.BorrowerTypeLegalEntity {
		return nil, fmt.Errorf("sourcecatalog: unsupported borrower type %q", borrower)
	}
	c := catalog{borrower: borrower}
	if borrower == contracts.BorrowerTypeSoleProprietor {
		c.fields(reflect.TypeOf(contracts.IpScoring{}), "scoring.ip.")
	} else {
		c.fields(reflect.TypeOf(contracts.LegalEntityScoring{}), "scoring.legal_entity.")
	}
	c.fields(reflect.TypeOf(contracts.CreditRequest{}), "credit_request.")
	c.fields(reflect.TypeOf(contracts.Need{}), "need.")
	c.fields(reflect.TypeOf(contracts.Cash{}), "cash.")
	c.fields(reflect.TypeOf(contracts.Evidence{}), "evidence.")
	c.add("financial_debt_rub", model.Application, "financial_debt_rub", model.Number, model.RUB, model.NumericInt64, true, false)
	if borrower == contracts.BorrowerTypeSoleProprietor {
		c.fields(reflect.TypeOf(contracts.IpEconomicData{}), "ip_economic_data.")
	}
	// These are existing preparation checks, with missing-path provenance kept
	// by the fact preparer. Numeric business thresholds remain in the model.
	for _, id := range strings.Fields("borrower_name_present borrower_inn_present purpose_present purpose_is_business_development history_complete future_complete debt_schedule_consistent") {
		c.prepared(id, model.Boolean, model.Flag, "", false)
	}
	c.prepared("facts_age_days", model.Number, model.Days, model.NumericRational, false)
	c.prepared("supplier_facts_age_days", model.Number, model.Days, model.NumericRational, false)
	for _, id := range strings.Fields("receipts_rub cfads_rub variable_rub fixed_rub historical_debt_rub annual_required_rub previous_quarter_receipts_rub recent_quarter_receipts_rub") {
		c.prepared(id, model.Number, model.RUB, model.NumericInteger, id != "cfads_rub")
	}
	c.prepared("monthly_required_rub", model.Number, model.RUB, model.NumericFloat64, true, "annual_required_rub")
	if borrower == contracts.BorrowerTypeSoleProprietor {
		// v2 subtracts already converted float64 aggregates here. Retain that
		// rounding for the IP_E13 special case, including very large ruble sums.
		c.prepared("receipts_minus_variable_rub", model.Number, model.RUB, model.NumericFloat64, false, "receipts_rub", "variable_rub")
	}
	c.prepared("positive_cfads_months", model.Number, model.Months, model.NumericInt64, true)
	c.prepared("confirmed_receipts_ratio", model.Number, model.Ratio, model.NumericRational, true, "receipts_rub", "confirmed_receipts_rub")
	c.prepared("unexplained_difference_ratio", model.Number, model.Ratio, model.NumericRational, true, "receipts_rub", "unexplained_difference_rub")
	c.prepared("taxes_cfads_ratio", model.Number, model.Ratio, model.NumericRational, true, "cfads_rub", "overdue_taxes_rub")
	if borrower == contracts.BorrowerTypeLegalEntity {
		c.prepared("statements_complete", model.Boolean, model.Flag, "", false)
		for _, period := range []string{"current", "previous"} {
			for _, field := range strings.Fields("non_current_assets inventories receivables short_term_investments cash current_assets total_assets equity long_term_borrowings long_term_liabilities short_term_borrowings current_liabilities") {
				c.add(period+"_"+field, model.Statements, period+".balance."+field, model.Number, model.RUB, model.NumericFloat64, true, field == "equity")
			}
			for _, field := range strings.Fields("revenue cost_of_sales sales_profit interest_payable net_profit") {
				c.add(period+"_"+field, model.Statements, period+".profit_and_loss."+field, model.Number, model.RUB, model.NumericFloat64, true, field == "sales_profit" || field == "net_profit")
			}
		}
		for _, field := range strings.Fields("total_assets equity receivables inventories") {
			c.prepared("average_"+field, model.Number, model.RUB, model.NumericFloat64, field != "equity", "current_"+field, "previous_"+field)
		}
		c.prepared("financial_borrowings_rub", model.Number, model.RUB, model.NumericFloat64, true, "current_long_term_borrowings", "current_short_term_borrowings")
	}
	for i, dependencies := range economicDependencies(borrower) {
		prefix := "ip"
		unit := model.Ratio
		if borrower == contracts.BorrowerTypeLegalEntity {
			prefix = "le"
			if i >= 16 {
				unit = model.Days
			}
		} else if i == 4 {
			unit = model.Months
		}
		c.prepared(fmt.Sprintf("%s_e%02d", prefix, i+1), model.Number, unit, model.NumericFloat64, false, dependencies...)
	}
	// Resolve IDs to trusted sources after all entries have been declared.
	byID := map[string]model.Source{}
	for _, e := range c.entries {
		byID[e.Parameter.ID] = e.Parameter.Source
	}
	for i := range c.entries {
		for _, id := range c.entries[i].Parameter.DependsOn {
			source, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("sourcecatalog: missing dependency %s", id)
			}
			c.entries[i].Binding.DependsOn = append(c.entries[i].Binding.DependsOn, source)
		}
	}
	return c.entries, nil
}

func Bindings(borrower contracts.BorrowerType) ([]model.Binding, error) {
	entries, err := Entries(borrower)
	if err != nil {
		return nil, err
	}
	result := make([]model.Binding, len(entries))
	for i, entry := range entries {
		result[i] = entry.Binding
	}
	return result, nil
}

func Parameters(borrower contracts.BorrowerType) ([]model.Parameter, error) {
	entries, err := Entries(borrower)
	if err != nil {
		return nil, err
	}
	result := make([]model.Parameter, len(entries))
	for i, entry := range entries {
		result[i] = entry.Parameter
	}
	return result, nil
}

type catalog struct {
	borrower contracts.BorrowerType
	entries  []Entry
}

func (c *catalog) add(id string, kind model.SourceKind, path string, typ model.ValueType, unit model.Unit, numeric model.NumericKind, nonnegative, signed bool) {
	p := model.Parameter{ID: id, Enabled: true, Title: id, Source: model.Source{Kind: kind, Path: path}, JSONName: path[strings.LastIndex(path, ".")+1:], Type: typ, Unit: unit, Required: true}
	if typ == model.Number {
		p.Number = &model.NumberDomain{Integer: numeric == model.NumericInt64 || numeric == model.NumericInteger || kind == model.Statements}
		if nonnegative && !signed {
			p.Number.Lower = &model.Bound{Value: "0", Inclusive: true}
		}
	}
	c.entries = append(c.entries, Entry{Parameter: p, Binding: model.Binding{Source: p.Source, Type: typ, Unit: unit, BorrowerType: c.borrower, NumericKind: numeric}})
}

func (c *catalog) prepared(id string, typ model.ValueType, unit model.Unit, numeric model.NumericKind, nonnegative bool, dependencies ...string) {
	c.add(id, model.Computed, "prepared."+id, typ, unit, numeric, nonnegative, false)
	p := &c.entries[len(c.entries)-1].Parameter
	p.Required = false // Unknown derived results are handled by checks/specials.
	p.DependsOn = append([]string(nil), dependencies...)
}

func (c *catalog) fields(t reflect.Type, prefix string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "purpose" {
			continue
		} // Free text uses the existing presence/preparation check.
		typ, unit, numeric := model.Number, model.Count, model.NumericInt64
		switch f.Type.Elem().Kind() {
		case reflect.String:
			typ, unit, numeric = model.String, model.Category, ""
		case reflect.Bool:
			typ, unit, numeric = model.Boolean, model.Flag, ""
		case reflect.Float64:
			numeric = model.NumericFloat64
		}
		if strings.HasSuffix(name, "_rub") {
			unit = model.RUB
		}
		if strings.HasSuffix(name, "_months") {
			unit = model.Months
		}
		if strings.HasSuffix(name, "_years") || name == "owner_age" {
			unit = model.Years
		}
		if strings.Contains(name, "dpd_days") {
			unit = model.Days
		}
		if name == "max_annual_rate" {
			unit = model.Percent
		}
		c.add(name, model.Application, prefix+name, typ, unit, numeric, true, false)
		p := &c.entries[len(c.entries)-1].Parameter
		p.StringValues = append([]string(nil), enums[name]...)
		if name == "military_status" || name == "largest_buyer_group_receipts_rub" && c.borrower == contracts.BorrowerTypeLegalEntity {
			p.Required = false
		}
		if name == "location_stability_months" {
			physical := "VERIFIED_PHYSICAL"
			p.RequiredWhen = &model.Condition{Op: model.EQ, Parameter: "business_location", Value: &model.Literal{String: &physical}}
		}
		if name == "report_verification_level" {
			p.Number.Upper = &model.Bound{Value: "3", Inclusive: true}
		}
		if name == "max_annual_rate" {
			p.Number.Upper = &model.Bound{Value: "100", Inclusive: true}
		}
		if name == "amount_rub" || name == "min_acceptable_amount_rub" || name == "term_months" || name == "max_acceptable_term_months" {
			p.Number.Lower.Inclusive = false
		}
	}
}

var enums = map[string][]string{
	"marital_status":        {"MARRIED", "NEVER_MARRIED", "DIVORCED_ONCE", "DIVORCED_MULTIPLE", "WIDOWED"},
	"primary_income_source": {"BUSINESS", "PUBLIC_EMPLOYMENT", "PRIVATE_EMPLOYMENT", "PENSION", "SELF_EMPLOYED_OR_GPH", "NONE"},
	"qualification":         {"NONE", "SUPPORT", "SPECIALIST", "MANAGER_OR_HIGHLY_QUALIFIED"},
	"car_class":             {"NONE", "ECONOMY", "COMFORT", "PREMIUM"},
	"education_level":       {"BELOW_GENERAL", "GENERAL", "VOCATIONAL", "HIGHER", "ACADEMIC_DEGREE"},
	"real_estate_status":    {"UNENCUMBERED_WHOLE", "UNENCUMBERED_SHARE", "ENCUMBERED_ONLY", "NONE"},
	"business_location":     {"VERIFIED_PHYSICAL", "VERIFIED_ONLINE_OR_MOBILE", "UNVERIFIED"},
	"income_evidence":       {"VERIFIED", "DOCUMENTS_PENDING_CROSSCHECK", "NONE"},
	"buyer_contracts":       {"ACTIVE_AND_PERFORMED", "REPEAT_ORDERS", "PLANS_ONLY"},
	"military_status":       {"SERVED", "EXEMPT_OR_DEFERRED", "NOT_LIABLE", "OTHER_OR_UNKNOWN"},
	"auditor_opinion":       {"UNMODIFIED", "NOT_REQUIRED", "MATERIAL_MODIFICATION", "REQUIRED_MISSING"},
	"need_evidence":         {"CONTRACT_AND_COST", "VERIFIED_ESTIMATE", "ORAL_ONLY"},
}

func economicDependencies(b contracts.BorrowerType) [][]string {
	if b == contracts.BorrowerTypeSoleProprietor {
		return [][]string{
			{"cfads_rub", "receipts_rub"}, {"historical_debt_rub", "cfads_rub"}, {"financial_debt_rub", "cfads_rub"},
			{"free_after_own_contribution_rub", "monthly_required_rub"}, {"positive_cfads_months"}, {"history_complete", "receipts_rub"},
			{"receipts_rub", "previous_comparable_receipts_rub"}, {"recent_quarter_receipts_rub", "previous_quarter_receipts_rub"},
			{"largest_buyer_group_receipts_rub", "receipts_rub"}, {"confirmed_receipts_rub", "receipts_rub"}, {"unexplained_difference_rub", "receipts_rub"},
			{"own_contribution_rub", "confirmed_cost_rub"}, {"receipts_rub", "variable_rub", "fixed_rub"},
		}
	}
	return [][]string{
		{"current_current_assets", "current_current_liabilities"}, {"current_receivables", "current_short_term_investments", "current_cash", "current_current_liabilities"},
		{"current_short_term_investments", "current_cash", "current_current_liabilities"}, {"current_equity", "current_total_assets"},
		{"current_equity", "current_long_term_liabilities", "current_total_assets"}, {"current_equity", "current_non_current_assets", "current_current_assets"},
		{"current_long_term_borrowings", "current_short_term_borrowings", "current_short_term_investments", "current_cash", "current_sales_profit"},
		{"current_sales_profit", "current_interest_payable"}, {"current_short_term_borrowings", "financial_borrowings_rub"},
		{"current_revenue", "current_cost_of_sales"}, {"current_sales_profit", "current_revenue"}, {"current_net_profit", "current_revenue"},
		{"current_net_profit", "average_total_assets"}, {"current_net_profit", "current_equity", "average_equity"},
		{"current_revenue", "previous_revenue"}, {"current_revenue", "average_total_assets"}, {"average_receivables", "current_revenue"}, {"average_inventories", "current_cost_of_sales"},
	}
}
