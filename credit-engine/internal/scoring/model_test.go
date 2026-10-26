package scoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

func modelEngine(t *testing.T, borrower contracts.BorrowerType) *rules.Engine {
	t.Helper()
	version := "ip-v2"
	if borrower == contracts.BorrowerTypeLegalEntity {
		version = "legal-v2"
	}
	file, err := os.Open(filepath.Join("..", "..", "models", version+".json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	d, err := model.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := sourcecatalog.Bindings(borrower)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := rules.Compile(d, catalog)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}
func modelAssess(t *testing.T, e *rules.Engine, f fixture) (ModelAssessment, error) {
	t.Helper()
	p, err := PrepareApplication(f.Request, evaluationDate)
	if err != nil {
		return ModelAssessment{}, err
	}
	stops, err := e.Stops(p.Input)
	if err != nil {
		return ModelAssessment{}, err
	}
	if len(stops) > 0 {
		return EvaluatePrepared(e, p)
	}
	if f.Request.Borrower.Type == contracts.BorrowerTypeLegalEntity {
		p, err = WithStatements(p, f.Statements)
		if err != nil {
			return ModelAssessment{}, err
		}
	}
	return EvaluatePrepared(e, p)
}
func compareModel(t *testing.T, actual ModelAssessment, want Assessment) {
	t.Helper()
	got := actual.Result
	if string(got.Route) != string(want.Route) {
		t.Fatalf("route %s want %s, reasons %+v", got.Route, want.Route, got.Reasons)
	}
	if (got.Score == nil) != (want.Score == nil) {
		t.Fatalf("score presence %v/%v", got.Score, want.Score)
	}
	if got.Score != nil {
		s := got.Score
		expected := want.Score
		if s.Model != expected.Model || s.RawTotal != expected.RawTotal || s.Total != expected.Total || s.Grade != expected.Grade || s.QuestionnairePoints != expected.Questionnaire.Points || s.AdditionalPoints != expected.Economics.Points {
			t.Fatalf("score got %+v want %+v", s, expected)
		}
		q, e := 0, 0
		for _, c := range s.Criteria {
			if c.Block == model.Questionnaire {
				w := expected.Questionnaire.Criteria[q]
				q++
				if c.ID != w.ID || c.Answer != w.Answer || int(c.Points) != w.Points || c.MaxPoints != w.MaxPoints {
					t.Fatalf("question %+v want %+v", c, w)
				}
			} else {
				w := expected.Economics.Criteria[e]
				e++
				if c.ID != w.ID || c.Points != w.Points || c.MaxPoints != w.MaxPoints || c.Value.Known() != (w.Value != nil) {
					t.Fatalf("economic %+v want %+v", c, w)
				}
				if w.Value != nil {
					value, ok := c.Value.Float()
					if !ok || value != *w.Value {
						t.Fatalf("%s value %v want %v", c.ID, value, *w.Value)
					}
				}
			}
		}
		if q != len(expected.Questionnaire.Criteria) || e != len(expected.Economics.Criteria) {
			t.Fatal("criterion count")
		}
		if !reflect.DeepEqual(actual.CashFlow, want.CashFlow) {
			t.Fatal("cash flow changed")
		}
	}
	if len(got.Reasons) != len(want.Reasons) {
		t.Fatalf("reasons %+v want %+v", got.Reasons, want.Reasons)
	}
	for i, r := range got.Reasons {
		w := want.Reasons[i]
		fields, wfields := append([]string{}, r.Fields...), append([]string{}, w.Fields...)
		sort.Strings(fields)
		sort.Strings(wfields)
		if r.Code != w.Code || r.Message != w.Message || !reflect.DeepEqual(fields, wfields) {
			t.Fatalf("reason %+v want %+v", r, w)
		}
	}
}

func TestPureModelDecisionFixtures(t *testing.T) {
	engines := map[contracts.BorrowerType]*rules.Engine{contracts.BorrowerTypeSoleProprietor: modelEngine(t, contracts.BorrowerTypeSoleProprietor), contracts.BorrowerTypeLegalEntity: modelEngine(t, contracts.BorrowerTypeLegalEntity)}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "decisions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			f := loadFixture(t, "decisions/"+filepath.Base(path))
			expected, oldErr, _ := assess(t, f)
			actual, err := modelAssess(t, engines[f.Request.Borrower.Type], f)
			if (err == nil) != (oldErr == nil) {
				t.Fatalf("errors %v want %v", err, oldErr)
			}
			if oldErr == nil {
				compareModel(t, actual, expected)
			}
		})
	}
}

func TestPurePreparationExplicitDateAndCopies(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	before, _ := json.Marshal(f.Request)
	if _, err := PrepareApplication(f.Request, time.Time{}); err == nil {
		t.Fatal("implicit date allowed")
	}
	p, err := PrepareApplication(f.Request, evaluationDate)
	if err != nil {
		t.Fatal(err)
	}
	p2 := WithAdditionalFacts(p, map[string]rules.Value{"flag": rules.Boolean(false)})
	source := model.Source{Kind: model.Supplier, Path: "flag"}
	if p.Input.Values[source].Known() {
		t.Fatal("additional facts mutated original")
	}
	if value, known := p2.Input.Values[source].Bool(); !known || value {
		t.Fatal("false lost")
	}
	after, _ := json.Marshal(f.Request)
	if string(before) != string(after) {
		t.Fatal("request mutated")
	}
	if _, err := WithStatements(p, f.Statements); err == nil {
		t.Fatal("IP statements accepted")
	}
	if _, err := EvaluatePrepared(nil, p); err == nil {
		t.Fatal("nil engine")
	}
	if _, err := EvaluatePrepared(modelEngine(t, contracts.BorrowerTypeLegalEntity), p); err == nil {
		t.Fatal("wrong model type")
	}
}

func TestPureModelMissingAndIncompleteInputs(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		t.Run(kind, func(t *testing.T) {
			name := "decisions/uc04_" + kind + "_approved.json"
			base := loadFixture(t, name)
			engine := modelEngine(t, base.Request.Borrower.Type)
			var questionnaire reflect.Value
			if kind == "ip" {
				questionnaire = reflect.ValueOf(base.Request.Scoring.IP).Elem()
			} else {
				questionnaire = reflect.ValueOf(base.Request.Scoring.LegalEntity).Elem()
			}
			for i := 0; i < questionnaire.NumField(); i++ {
				t.Run(questionnaire.Type().Field(i).Name, func(t *testing.T) {
					f := loadFixture(t, name)
					var q reflect.Value
					if kind == "ip" {
						q = reflect.ValueOf(f.Request.Scoring.IP).Elem()
					} else {
						q = reflect.ValueOf(f.Request.Scoring.LegalEntity).Elem()
					}
					q.Field(i).SetZero()
					old, err, _ := assess(t, f)
					if err != nil {
						t.Fatal(err)
					}
					got, err := modelAssess(t, engine, f)
					if err != nil {
						t.Fatal(err)
					}
					compareModel(t, got, old)
				})
			}
			for _, change := range []struct {
				name  string
				apply func(*contracts.DecisionRequest)
			}{
				{"scoring", func(r *contracts.DecisionRequest) { r.Scoring = nil }},
				{"request", func(r *contracts.DecisionRequest) { r.CreditRequest = nil }},
				{"need", func(r *contracts.DecisionRequest) { r.Need = nil }},
				{"cash", func(r *contracts.DecisionRequest) { r.Cash = nil }},
				{"evidence", func(r *contracts.DecisionRequest) { r.Evidence = nil }},
				{"history_month", func(r *contracts.DecisionRequest) { r.MonthlyHistory[0].Month = nil }},
				{"history_value", func(r *contracts.DecisionRequest) { r.MonthlyHistory[0].FixedPaymentsRUB = nil }},
				{"history_count", func(r *contracts.DecisionRequest) { r.MonthlyHistory = r.MonthlyHistory[:11] }},
				{"history_duplicate", func(r *contracts.DecisionRequest) { r.MonthlyHistory[0].Month = r.MonthlyHistory[1].Month }},
				{"future_month", func(r *contracts.DecisionRequest) { r.FutureDebtPayments[0].Month = nil }},
				{"future_value", func(r *contracts.DecisionRequest) { r.FutureDebtPayments[0].PaymentRUB = nil }},
				{"future_count", func(r *contracts.DecisionRequest) { r.FutureDebtPayments = r.FutureDebtPayments[:35] }},
				{"future_duplicate", func(r *contracts.DecisionRequest) { r.FutureDebtPayments[0].Month = r.FutureDebtPayments[1].Month }},
				{"future_debt_inconsistent", func(r *contracts.DecisionRequest) { r.FinancialDebtRUB = ptr(int64(0)) }},
				{"facts_missing", func(r *contracts.DecisionRequest) { r.FactsDate = nil }},
				{"facts_old", func(r *contracts.DecisionRequest) { r.FactsDate = ptr("2026-09-07") }},
				{"facts_future", func(r *contracts.DecisionRequest) { r.FactsDate = ptr("2026-10-24") }},
			} {
				t.Run(change.name, func(t *testing.T) {
					f := loadFixture(t, name)
					change.apply(&f.Request)
					old, err, _ := assess(t, f)
					if err != nil {
						t.Fatal(err)
					}
					got, err := modelAssess(t, engine, f)
					if err != nil {
						t.Fatal(err)
					}
					compareModel(t, got, old)
				})
			}
		})
	}
}

func TestEditedQualityCheckControlsFinancialStop(t *testing.T) {
	f := loadFixture(t, "decisions/uc04_ip_approved.json")
	receipts := int64(0)
	for i := range f.Request.MonthlyHistory {
		month := &f.Request.MonthlyHistory[i]
		receipts += *month.OperatingReceiptsRUB
		k := *month.FixedPaymentsRUB + *month.TaxesPaidRUB + *month.MaintenanceInvestmentRUB + *month.OwnerRequiredPaymentsRUB
		month.VariablePaymentsRUB = ptr(*month.OperatingReceiptsRUB - k)
	}
	f.Request.Evidence.ConfirmedReceiptsRUB = ptr(receipts / 2)
	f.Request.Borrower.Name = nil
	p, err := PrepareApplication(f.Request, evaluationDate)
	if err != nil {
		t.Fatal(err)
	}
	e := modelEngine(t, contracts.BorrowerTypeSoleProprietor)
	before, err := EvaluatePrepared(e, p)
	if err != nil || before.Result.Route != rules.NeedInfo {
		t.Fatal(before, err)
	}
	file, err := os.Open("../../models/ip-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	d, err := model.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	var update func(*model.Condition)
	update = func(c *model.Condition) {
		if c.Parameter == "confirmed_receipts_ratio" {
			n := model.NumberLiteral("0.4")
			c.Value.Number = &n
		}
		for i := range c.Conditions {
			update(&c.Conditions[i])
		}
	}
	for i := range d.DataChecks {
		if d.DataChecks[i].ID == "confirmed_receipts" {
			update(&d.DataChecks[i].SatisfiedWhen)
		}
	}
	catalog, err := sourcecatalog.Bindings(d.BorrowerType)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := rules.Compile(d, catalog)
	if err != nil {
		t.Fatal(err)
	}
	after, err := EvaluatePrepared(edited, p)
	if err != nil || after.Result.Route != rules.Rejected || len(after.Result.Reasons) != 1 || after.Result.Reasons[0].Code != "NONPOSITIVE_VERIFIED_CFADS" {
		t.Fatal(after, err)
	}
}
