package scoring

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

// Prepared contains source facts, original diagnostic paths and the existing
// cash flow. The caller supplies the date; preparation never reads a clock.
type Prepared struct {
	Input        rules.Input
	CashFlow     CashFlow
	BorrowerType contracts.BorrowerType
	BorrowerID   string
	Date         time.Time
}
type ModelAssessment struct {
	Result   rules.Result
	CashFlow CashFlow
}

func computed(id string) model.Source {
	return model.Source{Kind: model.Computed, Path: "prepared." + id}
}
func (p *Prepared) set(id string, v rules.Value) { p.Input.Values[computed(id)] = v }

// PrepareApplication keeps technical validation separate from editable
// completeness, thresholds, points and routes. It has no dependency calls.
func PrepareApplication(r contracts.DecisionRequest, date time.Time) (Prepared, error) {
	if date.IsZero() {
		return Prepared{}, fmt.Errorf("scoring: explicit evaluation date is required")
	}
	if err := Validate(r); err != nil {
		return Prepared{}, err
	}
	p := Prepared{Input: rules.Input{Values: map[model.Source]rules.Value{}, Origins: map[model.Source][]string{}, FieldOrigins: map[string][]string{}}, BorrowerType: r.Borrower.Type, BorrowerID: r.Borrower.ID, Date: dateOnly(date)}
	entries, err := sourcecatalog.Entries(r.Borrower.Type)
	if err != nil {
		return Prepared{}, err
	}
	for _, entry := range entries {
		source := entry.Parameter.Source
		if source.Kind == model.Statements {
			p.Input.Origins[source] = []string{"service_b.annual_reports"}
		}
		if source.Kind != model.Application {
			continue
		}
		value, origin := applicationValue(r, source.Path)
		p.Input.Values[source] = value
		if !value.Known() {
			p.Input.Origins[source] = []string{origin}
			p.Input.FieldOrigins[source.Path] = []string{origin}
		}
	}
	// inspectInput reports these six confirmations independently even when
	// the whole scoring object is missing. Keep that legacy provenance.
	prefix := "scoring.legal_entity."
	if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
		prefix = "scoring.ip."
	}
	for _, name := range strings.Fields("identity_and_authority_verified consents_verified bureau_received debts_complete documents_complete free_cash_source_verified") {
		delete(p.Input.FieldOrigins, prefix+name)
	}
	p.set("borrower_name_present", rules.Boolean(r.Borrower.Name != nil && strings.TrimSpace(*r.Borrower.Name) != ""))
	p.set("borrower_inn_present", rules.Boolean(r.Borrower.INN != nil && strings.TrimSpace(*r.Borrower.INN) != ""))
	purposePresent := r.CreditRequest != nil && r.CreditRequest.Purpose != nil && strings.TrimSpace(*r.CreditRequest.Purpose) != ""
	p.set("purpose_present", rules.Boolean(purposePresent))
	if r.CreditRequest == nil {
		p.Input.FieldOrigins["credit_request.purpose"] = []string{"credit_request"}
	}
	if r.CreditRequest != nil && r.CreditRequest.Purpose != nil {
		p.set("purpose_is_business_development", rules.Boolean(*r.CreditRequest.Purpose == "BUSINESS_DEVELOPMENT"))
	}
	if r.FactsDate != nil {
		factsDate, _ := time.Parse("2006-01-02", *r.FactsDate)
		p.set("facts_age_days", rules.Rational(big.NewRat(int64(p.Date.Sub(factsDate)), int64(24*time.Hour))))
	}
	values := inspectValues(r)
	d := inputData{}
	historyComplete := history(r, p.Date, &d)
	historyMissing := append([]string{}, d.missing...)
	for _, field := range values.missing {
		if strings.HasPrefix(field, "monthly_history") {
			historyMissing = append(historyMissing, field)
		}
	}
	p.set("history_complete", rules.Boolean(historyComplete))
	p.Input.FieldOrigins["monthly_history"] = unique(historyMissing)
	d.missing = nil
	future(r, p.Date, &d)
	futureMissing := append([]string{}, d.missing...)
	for _, field := range values.missing {
		if strings.HasPrefix(field, "future_debt_payments") {
			futureMissing = append(futureMissing, field)
		}
	}
	futureComplete, debtConsistent := futureStatus(r, p.Date)
	p.set("future_complete", rules.Boolean(futureComplete))
	p.set("debt_schedule_consistent", rules.Boolean(debtConsistent))
	p.Input.FieldOrigins["future_debt_payments"] = unique(futureMissing)
	if !debtConsistent {
		p.Input.FieldOrigins["financial_debt_rub"] = []string{"financial_debt_rub"}
	}
	p.CashFlow = d.flow
	if historyComplete {
		for _, v := range []struct {
			id    string
			value *big.Int
		}{{"receipts_rub", &d.receiptsRUB}, {"cfads_rub", &d.cfadsRUB}, {"variable_rub", &d.variableRUB}, {"fixed_rub", &d.fixedRUB}, {"historical_debt_rub", &d.historicalDebtRUB}, {"annual_required_rub", &d.annualRequiredRUB}} {
			p.set(v.id, rules.Integer(v.value))
		}
		p.set("monthly_required_rub", rules.Float64(d.flow.MonthlyRequired))
		p.set("positive_cfads_months", rules.Int64(int64(d.flow.PositiveMonths)))
		if r.Evidence != nil {
			if d.receiptsRUB.Sign() > 0 {
				for _, v := range []struct {
					id    string
					value *int64
				}{{"confirmed_receipts_ratio", r.Evidence.ConfirmedReceiptsRUB}, {"unexplained_difference_ratio", r.Evidence.UnexplainedDifferenceRUB}} {
					if v.value != nil {
						p.set(v.id, rules.Rational(new(big.Rat).SetFrac(big.NewInt(*v.value), &d.receiptsRUB)))
					}
				}
			}
		}
		taxes, _ := p.Input.Values[model.Source{Kind: model.Application, Path: map[bool]string{true: "scoring.ip.", false: "scoring.legal_entity."}[r.Borrower.Type == contracts.BorrowerTypeSoleProprietor] + "overdue_taxes_rub"}].Rational()
		if d.cfadsRUB.Sign() > 0 && taxes != nil {
			p.set("taxes_cfads_ratio", rules.Rational(new(big.Rat).Quo(taxes, new(big.Rat).SetInt(&d.cfadsRUB))))
		}
		if r.Borrower.Type == contracts.BorrowerTypeSoleProprietor {
			p.set("receipts_minus_variable_rub", rules.Float64(d.flow.Receipts-d.flow.Variable))
			previous, recent := new(big.Int), new(big.Int)
			start := time.Date(p.Date.Year(), p.Date.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -12, 0)
			for _, month := range r.MonthlyHistory {
				date, _ := time.Parse("2006-01", *month.Month)
				index := (date.Year()-start.Year())*12 + int(date.Month()-start.Month())
				if index >= 6 && index < 9 {
					previous.Add(previous, big.NewInt(*month.OperatingReceiptsRUB))
				}
				if index >= 9 {
					recent.Add(recent, big.NewInt(*month.OperatingReceiptsRUB))
				}
			}
			p.set("previous_quarter_receipts_rub", rules.Integer(previous))
			p.set("recent_quarter_receipts_rub", rules.Integer(recent))
			metrics := calculateIPMetrics(r, d.flow)
			for id, value := range metrics.values {
				p.set(fmt.Sprintf("ip_e%02d", id), rules.Float64(*value))
			}
		}
	}
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		p.set("statements_complete", rules.Boolean(false))
		p.Input.FieldOrigins["service_b.annual_reports"] = []string{"service_b.annual_reports"}
	}
	return p, nil
}

func applicationValue(r contracts.DecisionRequest, path string) (rules.Value, string) {
	v := reflect.ValueOf(r)
	prefix := []string{}
	for _, part := range strings.Split(path, ".") {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return rules.Unknown(), strings.Join(prefix, ".")
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return rules.Unknown(), path
		}
		found := false
		for i := 0; i < v.NumField(); i++ {
			if strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0] == part {
				v = v.Field(i)
				found = true
				break
			}
		}
		if !found {
			return rules.Unknown(), path
		}
		prefix = append(prefix, part)
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return rules.Unknown(), path
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Bool:
		return rules.Boolean(v.Bool()), path
	case reflect.String:
		return rules.String(v.String()), path
	case reflect.Int, reflect.Int64:
		return rules.Int64(v.Int()), path
	case reflect.Float64:
		return rules.Float64(v.Float()), path
	}
	return rules.Unknown(), path
}

func futureStatus(r contracts.DecisionRequest, date time.Time) (bool, bool) {
	complete, consistent := len(r.FutureDebtPayments) == 36, true
	seen := map[int]bool{}
	start := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	for _, month := range r.FutureDebtPayments {
		if month.Month == nil || month.PaymentRUB == nil {
			complete = false
			continue
		}
		parsed, err := time.Parse("2006-01", *month.Month)
		index := (parsed.Year()-start.Year())*12 + int(parsed.Month()-start.Month())
		if err != nil || index < 0 || index >= 36 || seen[index] {
			complete = false
			continue
		}
		seen[index] = true
		if r.FinancialDebtRUB != nil && *r.FinancialDebtRUB == 0 && *month.PaymentRUB > 0 {
			consistent = false
		}
	}
	return complete && len(seen) == 36, consistent
}

func clonePrepared(p Prepared) Prepared {
	old := p.Input
	p.Input = rules.Input{Values: map[model.Source]rules.Value{}, Origins: map[model.Source][]string{}, FieldOrigins: map[string][]string{}}
	for source, value := range old.Values {
		p.Input.Values[source] = value
	}
	for source, origins := range old.Origins {
		p.Input.Origins[source] = append([]string{}, origins...)
	}
	for field, origins := range old.FieldOrigins {
		p.Input.FieldOrigins[field] = append([]string{}, origins...)
	}
	return p
}

// WithStatements normalizes and checks supplied B data. It never fetches it
// and never mutates the application preparation supplied by the caller.
func WithStatements(p Prepared, response contracts.StatementsResponse) (Prepared, error) {
	if p.Date.IsZero() {
		return Prepared{}, fmt.Errorf("scoring: explicit evaluation date is required")
	}
	if p.BorrowerType != contracts.BorrowerTypeLegalEntity {
		return Prepared{}, fmt.Errorf("scoring: statements require a legal entity")
	}
	reports, missing, err := inspectStatements(response, p.BorrowerID, p.Date)
	if err != nil {
		return Prepared{}, err
	}
	p = clonePrepared(p)
	for source := range p.Input.Values {
		if source.Kind == model.Statements || statementDerivedSource(source) {
			delete(p.Input.Values, source)
		}
	}
	p.set("statements_complete", rules.Boolean(len(missing) == 0))
	p.Input.FieldOrigins["service_b.annual_reports"] = append([]string{}, missing...)
	if len(missing) > 0 {
		for source := range p.Input.Origins {
			if source.Kind == model.Statements {
				p.Input.Origins[source] = append([]string{}, missing...)
			}
		}
		return p, nil
	}
	populateStatementFacts(&p, reports)
	return p, nil
}

func statementDerivedSource(source model.Source) bool {
	if source.Kind != model.Computed {
		return false
	}
	if strings.HasPrefix(source.Path, "prepared.le_e") {
		return true
	}
	switch source.Path {
	case "prepared.average_total_assets", "prepared.average_equity", "prepared.average_receivables", "prepared.average_inventories", "prepared.financial_borrowings_rub":
		return true
	}
	return false
}

// Projection is separate from report validation so undefined financial states
// can be checked independently; the caller owns p's maps.
func populateStatementFacts(p *Prepared, reports reports) {
	for id := 1; id <= 18; id++ {
		p.set(fmt.Sprintf("le_e%02d", id), rules.Unknown())
	}
	for _, period := range []struct {
		name   string
		report report
	}{{"current", reports.current}, {"previous", reports.previous}} {
		b, f := period.report.balance, period.report.profit
		for _, field := range []struct {
			name  string
			value float64
		}{{"non_current_assets", b.nonCurrentAssets}, {"inventories", b.inventories}, {"receivables", b.receivables}, {"short_term_investments", b.shortInvestments}, {"cash", b.cash}, {"current_assets", b.currentAssets}, {"total_assets", b.assets}, {"equity", b.equity}, {"long_term_borrowings", b.longBorrowings}, {"long_term_liabilities", b.longLiabilities}, {"short_term_borrowings", b.shortBorrowings}, {"current_liabilities", b.currentLiabilities}} {
			p.Input.Values[model.Source{Kind: model.Statements, Path: period.name + ".balance." + field.name}] = rules.Float64(field.value)
		}
		for _, field := range []struct {
			name  string
			value float64
		}{{"revenue", f.revenue}, {"cost_of_sales", f.cost}, {"sales_profit", f.salesProfit}, {"interest_payable", f.interest}, {"net_profit", f.netProfit}} {
			p.Input.Values[model.Source{Kind: model.Statements, Path: period.name + ".profit_and_loss." + field.name}] = rules.Float64(field.value)
		}
	}
	metrics := calculateLegalMetrics(reports)
	p.set("average_total_assets", rules.Float64(metrics.averageAssets))
	p.set("average_equity", rules.Float64(metrics.averageEquity))
	p.set("average_receivables", rules.Float64(metrics.averageReceivables))
	p.set("average_inventories", rules.Float64(metrics.averageInventory))
	p.set("financial_borrowings_rub", rules.Float64(reports.current.balance.longBorrowings+reports.current.balance.shortBorrowings))
	for id, value := range metrics.values {
		p.set(fmt.Sprintf("le_e%02d", id), rules.Float64(*value))
	}
}

func WithAdditionalFacts(p Prepared, facts map[string]rules.Value) Prepared {
	p = clonePrepared(p)
	for name, value := range facts {
		p.Input.Values[model.Source{Kind: model.Supplier, Path: name}] = value
	}
	return p
}

// WithSupplierFacts retains the provider date as an existing age preparation
// value. No business freshness threshold is introduced implicitly.
func WithSupplierFacts(p Prepared, facts map[string]rules.Value, date string) (Prepared, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || p.Date.IsZero() || parsed.After(p.Date) {
		return Prepared{}, fmt.Errorf("scoring: invalid supplier facts date")
	}
	p = WithAdditionalFacts(p, facts)
	p.set("supplier_facts_age_days", rules.Rational(big.NewRat(p.Date.Unix()-parsed.Unix(), 24*60*60)))
	return p, nil
}

func EvaluatePrepared(engine *rules.Engine, p Prepared) (ModelAssessment, error) {
	if engine == nil {
		return ModelAssessment{}, fmt.Errorf("scoring: model engine is required")
	}
	if p.Date.IsZero() {
		return ModelAssessment{}, fmt.Errorf("scoring: explicit evaluation date is required")
	}
	if engine.BorrowerType() != p.BorrowerType {
		return ModelAssessment{}, fmt.Errorf("scoring: model and borrower type differ")
	}
	result, err := engine.Evaluate(p.Input)
	if err != nil {
		return ModelAssessment{}, err
	}
	return ModelAssessment{Result: result, CashFlow: p.CashFlow}, nil
}
