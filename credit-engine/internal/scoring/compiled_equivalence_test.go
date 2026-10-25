package scoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

func compiledEntries(t *testing.T) map[contracts.BorrowerType]*modelbindings.Entry {
	t.Helper()
	catalog, err := modelbindings.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	entries := map[contracts.BorrowerType]*modelbindings.Entry{}
	for _, metadata := range catalog.List() {
		entry, err := catalog.Lookup(metadata.Version, metadata.BorrowerType)
		if err != nil {
			t.Fatal(err)
		}
		entries[metadata.BorrowerType] = entry
	}
	return entries
}
func criterionBorrower(id string) contracts.BorrowerType {
	if strings.HasPrefix(id, "IP_") {
		return contracts.BorrowerTypeSoleProprietor
	}
	return contracts.BorrowerTypeLegalEntity
}
func approvedBase(t *testing.T, borrower contracts.BorrowerType) fixture {
	t.Helper()
	name := "decisions/uc04_le_approved.json"
	if borrower == contracts.BorrowerTypeSoleProprietor {
		name = "decisions/uc04_ip_approved.json"
	}
	return loadFixture(t, name)
}

func TestCompiledQuestionnaireBoundaryEquivalence(t *testing.T) {
	var corpus struct {
		Cases []struct {
			ID     string          `json:"id"`
			Fields json.RawMessage `json:"raw_fields"`
			Points float64         `json:"expected_points"`
			Max    int             `json:"max_points"`
		} `json:"questionnaire_cases"`
	}
	readJSON(t, "scoring_boundaries.json", &corpus)
	if len(corpus.Cases) != 393 {
		t.Fatal(len(corpus.Cases))
	}
	entries := compiledEntries(t)
	unknowns := 0
	for i, c := range corpus.Cases {
		t.Run(fmt.Sprintf("%s/%03d", c.ID, i), func(t *testing.T) {
			borrower := criterionBorrower(c.ID)
			f := approvedBase(t, borrower)
			var legacy contracts.QuestionnaireResult
			if borrower == contracts.BorrowerTypeSoleProprietor {
				if err := json.Unmarshal(c.Fields, f.Request.Scoring.IP); err != nil {
					t.Fatal(err)
				}
				legacy = ipQuestionnaire(f.Request)
			} else {
				if err := json.Unmarshal(c.Fields, f.Request.Scoring.LegalEntity); err != nil {
					t.Fatal(err)
				}
				legacy = legalQuestionnaire(f.Request)
			}
			old := criterion(t, legacy, c.ID)
			if float64(old.Points) != c.Points || old.MaxPoints != c.Max {
				t.Fatal("legacy differs from baseline")
			}
			p, err := PrepareApplication(f.Request, evaluationDate)
			if err != nil {
				t.Fatal(err)
			}
			got, err := entries[borrower].Engine().EvaluateCriterion(c.ID, p.Input)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(c.Fields, []byte("null")) {
				unknowns++
				if len(got.Missing) == 0 || got.Skipped {
					t.Fatal("required null was scored", got)
				}
				return
			}
			if len(got.Missing) != 0 || got.Skipped || got.Criterion.ID != old.ID || got.Criterion.Answer != old.Answer || got.Criterion.Points != float64(old.Points) || got.Criterion.MaxPoints != old.MaxPoints {
				t.Fatalf("compiled %+v want %+v", got, old)
			}
		})
	}
	if unknowns != 7 {
		t.Fatal("changed null baseline", unknowns)
	}
}

func TestCompiledEconomicBoundaryAndWorkbookEquivalence(t *testing.T) {
	type example struct {
		ID     string  `json:"id"`
		Value  float64 `json:"value"`
		Points float64 `json:"expected_points"`
		Max    int     `json:"max_points"`
	}
	var boundaries struct {
		Cases []example `json:"economic_cases"`
	}
	var reference struct {
		Examples []example `json:"examples"`
	}
	readJSON(t, "scoring_boundaries.json", &boundaries)
	readJSON(t, "scoring_reference_examples.json", &reference)
	if len(boundaries.Cases) != 368 || len(reference.Examples) != 31 {
		t.Fatal("changed baseline")
	}
	entries := compiledEntries(t)
	prepared := map[contracts.BorrowerType]Prepared{}
	sources := map[string]model.Source{}
	for borrower, entry := range entries {
		f := approvedBase(t, borrower)
		p, err := PrepareApplication(f.Request, evaluationDate)
		if err != nil {
			t.Fatal(err)
		}
		if borrower == contracts.BorrowerTypeLegalEntity {
			p, err = WithStatements(p, f.Statements)
			if err != nil {
				t.Fatal(err)
			}
		}
		prepared[borrower] = p
		d, err := entry.Definition()
		if err != nil {
			t.Fatal(err)
		}
		parameters := map[string]model.Source{}
		for _, p := range d.Parameters {
			parameters[p.ID] = p.Source
		}
		for _, c := range d.Criteria {
			sources[c.ID] = parameters[c.ValueParameter]
		}
	}
	for _, group := range []struct {
		name  string
		cases []example
	}{{"boundaries", boundaries.Cases}, {"workbook", reference.Examples}} {
		t.Run(group.name, func(t *testing.T) {
			for i, c := range group.cases {
				t.Run(fmt.Sprintf("%s/%03d", c.ID, i), func(t *testing.T) {
					old, max := economicPoints(c.ID, c.Value)
					if old != c.Points || c.Max != 0 && max != c.Max {
						t.Fatal("legacy differs from baseline")
					}
					borrower := criterionBorrower(c.ID)
					p := clonePrepared(prepared[borrower])
					p.Input.Values[sources[c.ID]] = rules.Float64(c.Value)
					got, err := entries[borrower].Engine().EvaluateCriterion(c.ID, p.Input)
					if err != nil || len(got.Missing) != 0 || got.Skipped || got.Criterion.Special || got.Criterion.Points != old || got.Criterion.MaxPoints != max {
						t.Fatalf("compiled %+v err %v want %g/%d", got, err, old, max)
					}
					value, known := got.Criterion.Value.Float()
					if !known || value != c.Value {
						t.Fatal("coefficient value changed")
					}
				})
			}
		})
	}
}

func TestCompiledClassScaleAndRoundingEquivalence(t *testing.T) {
	var data struct {
		Grades []struct {
			Raw   float64         `json:"raw_total"`
			Total int             `json:"expected_total"`
			Grade contracts.Grade `json:"expected_grade"`
		} `json:"grade_cases"`
		Combined []struct {
			Questionnaire, Economics float64
			Total                    int
			Grade                    contracts.Grade
		} `json:"combination_cases"`
	}
	readJSON(t, "scoring_boundaries.json", &data)
	for borrower, entry := range compiledEntries(t) {
		t.Run(string(borrower), func(t *testing.T) {
			for _, c := range data.Grades {
				old, grade := totalAndGrade(c.Raw)
				got, err := entry.Engine().Classify(c.Raw)
				if err != nil || got.Total != old || got.Grade != grade || got.Total != c.Total || got.Grade != c.Grade {
					t.Fatal(c, got, err)
				}
			}
			for _, c := range data.Combined {
				got, err := entry.Engine().Classify(c.Questionnaire + c.Economics)
				if err != nil || got.Total != c.Total || got.Grade != c.Grade {
					t.Fatal(c, got, err)
				}
			}
			for _, bad := range []float64{math.NaN(), math.Inf(1), -1, 1001} {
				if _, err := entry.Engine().Classify(bad); err == nil {
					t.Fatal("inadmissible total accepted")
				}
			}
		})
	}
}

func TestCompiledDecisionAndSingleFactorEquivalence(t *testing.T) {
	entries := compiledEntries(t)
	files, err := filepath.Glob("../../testdata/decisions/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 18 {
		t.Fatal("changed scenario baseline", len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			f := loadFixture(t, "decisions/"+filepath.Base(file))
			old, oldErr, _ := assess(t, f)
			got, err := modelAssess(t, entries[f.Request.Borrower.Type].Engine(), f)
			if (err == nil) != (oldErr == nil) {
				t.Fatal(err, oldErr)
			}
			if err == nil {
				compareModel(t, got, old)
			}
		})
	}
	var controls struct {
		Stops []struct {
			Base string `json:"base_case"`
			Set  map[string]json.RawMessage
		} `json:"single_factor_stops"`
		Cash []struct {
			Base string `json:"base_case"`
			Set  map[string]json.RawMessage
		} `json:"cash_stop_confirmation"`
	}
	readJSON(t, "unit_cases.json", &controls)
	for i, c := range append(controls.Stops, controls.Cash...) {
		t.Run(fmt.Sprintf("control/%d", i), func(t *testing.T) {
			f := loadFixture(t, c.Base)
			mutate(t, &f.Request, c.Set)
			old, oldErr, source := assess(t, f)
			got, err := modelAssess(t, entries[f.Request.Borrower.Type].Engine(), f)
			if oldErr != nil || err != nil {
				t.Fatal(err, oldErr)
			}
			compareModel(t, got, old)
			if got.Result.Route == rules.Rejected && source.calls != 0 {
				t.Fatal("stop called B")
			}
		})
	}
}

func TestCompiledRepeatedStatementsDoNotKeepOldFacts(t *testing.T) {
	f := approvedBase(t, contracts.BorrowerTypeLegalEntity)
	p, err := PrepareApplication(f.Request, evaluationDate)
	if err != nil {
		t.Fatal(err)
	}
	p, err = WithStatements(p, f.Statements)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Input.Values[computed("le_e01")].Known() {
		t.Fatal("baseline metric missing")
	}
	missing := contracts.StatementsResponse{BorrowerID: f.Request.Borrower.ID, AnnualReports: []contracts.AnnualReport{}}
	updated, err := WithStatements(p, missing)
	if err != nil {
		t.Fatal(err)
	}
	for source, value := range updated.Input.Values {
		if (source.Kind == model.Statements || source == computed("le_e01")) && value.Known() {
			t.Fatalf("old report fact survived replacement: %s", source.Path)
		}
	}
	if !p.Input.Values[computed("le_e01")].Known() {
		t.Fatal("replacement changed original preparation")
	}
}

func expectedEconomicAssessment(r contracts.DecisionRequest, data inputData, economic contracts.EconomicResult, manual []contracts.Reason) Assessment {
	q := ipQuestionnaireResult(r)
	version := "ip-v2"
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		version = "legal-v2"
	}
	raw := float64(q.Points) + economic.Points
	total, grade := totalAndGrade(raw)
	out := Assessment{Route: RouteSelection, Score: &contracts.ScoreResult{Model: version, Questionnaire: q, Economics: economic, RawTotal: raw, Total: total, Grade: grade}, Reasons: manual, CashFlow: data.flow}
	if grade == contracts.GradeD {
		out.Route = RouteRejected
		out.Reasons = []contracts.Reason{reason("SCORE_CLASS_D", "Итоговый класс D.")}
		return out
	}
	if grade == contracts.GradeC {
		out.Reasons = append([]contracts.Reason{reason("SCORE_CLASS_C", "Итоговый класс C.")}, out.Reasons...)
	}
	if len(out.Reasons) > 0 {
		out.Route = RouteManual
	}
	return out
}
func ipQuestionnaireResult(r contracts.DecisionRequest) contracts.QuestionnaireResult {
	if r.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		return legalQuestionnaire(r)
	}
	return ipQuestionnaire(r)
}

func TestCompiledLegalSpecialValuesAndManualOrder(t *testing.T) {
	entry := compiledEntries(t)[contracts.BorrowerTypeLegalEntity]
	for _, test := range []struct {
		name   string
		mutate func(*reports)
	}{
		{"current_liabilities_zero", func(r *reports) { r.current.balance.currentLiabilities = 0 }},
		{"assets_zero", func(r *reports) { r.current.balance.assets = 0 }},
		{"current_assets_zero", func(r *reports) { r.current.balance.currentAssets = 0 }},
		{"sales_profit_zero", func(r *reports) { r.current.profit.salesProfit = 0 }},
		{"sales_profit_negative", func(r *reports) { r.current.profit.salesProfit = -1 }},
		{"interest_zero", func(r *reports) { r.current.profit.interest = 0 }},
		{"interest_and_profit_zero", func(r *reports) { r.current.profit.interest = 0; r.current.profit.salesProfit = 0 }},
		{"financial_debt_zero", func(r *reports) { r.current.balance.longBorrowings = 0; r.current.balance.shortBorrowings = 0 }},
		{"revenue_zero", func(r *reports) { r.current.profit.revenue = 0 }},
		{"average_assets_zero", func(r *reports) { r.current.balance.assets = 0; r.previous.balance.assets = 0 }},
		{"equity_zero", func(r *reports) { r.current.balance.equity = 0 }},
		{"equity_negative", func(r *reports) { r.current.balance.equity = -1 }},
		{"average_equity_zero", func(r *reports) { r.previous.balance.equity = -r.current.balance.equity }},
		{"previous_revenue_zero", func(r *reports) { r.previous.profit.revenue = 0 }},
		{"cost_zero", func(r *reports) { r.current.profit.cost = 0 }},
		{"cost_and_inventory_zero", func(r *reports) {
			r.current.profit.cost = 0
			r.current.balance.inventories = 0
			r.previous.balance.inventories = 0
		}},
		{"loss_two_years", func(r *reports) { r.current.profit.netProfit = -1; r.previous.profit.netProfit = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := approvedBase(t, contracts.BorrowerTypeLegalEntity)
			normalized := strongReports(t)
			test.mutate(&normalized)
			data := inspectInput(f.Request, dateOnly(evaluationDate))
			old, manual := legalEconomics(normalized, data.manual)
			expected := expectedEconomicAssessment(f.Request, data, old, manual)
			p, err := PrepareApplication(f.Request, evaluationDate)
			if err != nil {
				t.Fatal(err)
			}
			p, err = WithStatements(p, f.Statements)
			if err != nil {
				t.Fatal(err)
			}
			populateStatementFacts(&p, normalized)
			actual, err := EvaluatePrepared(entry.Engine(), p)
			if err != nil {
				t.Fatal(err)
			}
			compareModel(t, actual, expected)
		})
	}
}

func TestCompiledIPSpecialCasesThroughActualPreparation(t *testing.T) {
	entry := compiledEntries(t)[contracts.BorrowerTypeSoleProprietor]
	for _, test := range []struct {
		name   string
		mutate func(*contracts.DecisionRequest)
	}{
		{"monthly_required_zero", func(r *contracts.DecisionRequest) {
			for i := range r.MonthlyHistory {
				r.MonthlyHistory[i].FixedPaymentsRUB = ptr(int64(0))
				r.MonthlyHistory[i].OwnerRequiredPaymentsRUB = ptr(int64(0))
				r.MonthlyHistory[i].ExistingDebtPaymentRUB = ptr(int64(0))
			}
		}},
		{"previous_receipts_zero", func(r *contracts.DecisionRequest) { r.IPEconomicData.PreviousComparableReceiptsRUB = ptr(int64(0)) }},
		{"previous_quarter_zero", func(r *contracts.DecisionRequest) {
			var receipts int64
			for i := range r.MonthlyHistory {
				if i >= 6 && i < 9 {
					r.MonthlyHistory[i].OperatingReceiptsRUB = ptr(int64(0))
				}
				receipts += *r.MonthlyHistory[i].OperatingReceiptsRUB
			}
			r.Evidence.ConfirmedReceiptsRUB = ptr(receipts)
		}},
		{"cost_zero", func(r *contracts.DecisionRequest) { r.Need.ConfirmedCostRUB = ptr(int64(0)) }},
		{"contribution_margin_zero", func(r *contracts.DecisionRequest) {
			for i := range r.MonthlyHistory {
				r.MonthlyHistory[i].VariablePaymentsRUB = r.MonthlyHistory[i].OperatingReceiptsRUB
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := approvedBase(t, contracts.BorrowerTypeSoleProprietor)
			test.mutate(&f.Request)
			old, err, _ := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := modelAssess(t, entry.Engine(), f)
			if err != nil {
				t.Fatal(err)
			}
			compareModel(t, actual, old)
		})
	}
}

func TestCompiledZeroPointControlsAndOptionalMilitary(t *testing.T) {
	var data struct {
		Controls []struct {
			ID  string `json:"id"`
			Max int    `json:"expected_max_points"`
		} `json:"questionnaire_controls"`
	}
	readJSON(t, "unit_cases.json", &data)
	if len(data.Controls) != 17 {
		t.Fatal(len(data.Controls))
	}
	entries := compiledEntries(t)
	for _, c := range data.Controls {
		t.Run(c.ID, func(t *testing.T) {
			borrower := criterionBorrower(c.ID)
			f := approvedBase(t, borrower)
			p, err := PrepareApplication(f.Request, evaluationDate)
			if err != nil {
				t.Fatal(err)
			}
			got, err := entries[borrower].Engine().EvaluateCriterion(c.ID, p.Input)
			if err != nil || len(got.Missing) > 0 || got.Skipped || got.Criterion.Points != 0 || got.Criterion.MaxPoints != c.Max {
				t.Fatal(got, err)
			}
			legacy := criterion(t, ipQuestionnaireResult(f.Request), c.ID)
			if got.Criterion.Answer != legacy.Answer {
				t.Fatal("control answer changed", got, legacy)
			}
		})
	}
	f := approvedBase(t, contracts.BorrowerTypeSoleProprietor)
	f.Request.Scoring.IP.MilitaryStatus = nil
	p, err := PrepareApplication(f.Request, evaluationDate)
	if err != nil {
		t.Fatal(err)
	}
	got, err := entries[f.Request.Borrower.Type].Engine().EvaluateCriterion("IP_Q31", p.Input)
	if err != nil || got.Skipped || len(got.Missing) > 0 || got.Criterion.Answer != "OTHER_OR_UNKNOWN" || got.Criterion.Points != 0 {
		t.Fatal(got, err)
	}
}
