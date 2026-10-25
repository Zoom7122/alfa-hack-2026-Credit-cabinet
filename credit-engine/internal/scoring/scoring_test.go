package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
)

var evaluationDate = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }
func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	Request    contracts.DecisionRequest    `json:"request"`
	Statements contracts.StatementsResponse `json:"service_b_response"`
	Expected   struct {
		Response         contracts.DecisionResponse `json:"response"`
		StatementsCalled bool                       `json:"statements_called"`
	} `json:"expected"`
}

func loadFixture(t *testing.T, name string) fixture {
	t.Helper()
	var f fixture
	readJSON(t, name, &f)
	return f
}

type sourceStub struct {
	response contracts.StatementsResponse
	err      error
	calls    int
	t        *testing.T
	id       string
}

func (s *sourceStub) Statements(ctx context.Context, id string, years []int) (contracts.StatementsResponse, error) {
	s.calls++
	if s.id != "" && id != s.id {
		s.t.Errorf("borrower=%s want %s", id, s.id)
	}
	if !reflect.DeepEqual(years, []int{2024, 2025}) {
		s.t.Errorf("years=%v", years)
	}
	return s.response, s.err
}
func assess(t *testing.T, f fixture) (Assessment, error, *sourceStub) {
	t.Helper()
	source := &sourceStub{response: f.Statements, t: t, id: f.Request.Borrower.ID}
	e, err := NewEvaluator(source, clock.Fixed{Time: evaluationDate})
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.Evaluate(context.Background(), f.Request)
	return a, err, source
}
func criterion(t *testing.T, q contracts.QuestionnaireResult, id string) contracts.QuestionnaireCriterion {
	t.Helper()
	for _, c := range q.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing %s", id)
	return contracts.QuestionnaireCriterion{}
}
func economic(t *testing.T, q contracts.EconomicResult, id string) contracts.EconomicCriterion {
	t.Helper()
	for _, c := range q.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing %s", id)
	return contracts.EconomicCriterion{}
}
func hasReason(a Assessment, code string) bool {
	for _, r := range a.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
func checkError(t *testing.T, err error, status int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != status {
		t.Fatalf("error=%v want status %d", err, status)
	}
}

func TestQuestionnaireBoundaries(t *testing.T) {
	var data struct {
		Cases []struct {
			ID     string          `json:"id"`
			Fields json.RawMessage `json:"raw_fields"`
			Points float64         `json:"expected_points"`
			Max    int             `json:"max_points"`
		} `json:"questionnaire_cases"`
	}
	readJSON(t, "scoring_boundaries.json", &data)
	if len(data.Cases) != 393 {
		t.Fatalf("cases=%d", len(data.Cases))
	}
	for i, c := range data.Cases {
		t.Run(fmt.Sprintf("%s_%03d", c.ID, i), func(t *testing.T) {
			var q contracts.QuestionnaireResult
			if strings.HasPrefix(c.ID, "IP_") {
				f := loadFixture(t, "decisions/uc04_ip_approved.json")
				if err := json.Unmarshal(c.Fields, f.Request.Scoring.IP); err != nil {
					t.Fatal(err)
				}
				q = ipQuestionnaire(f.Request)
			} else {
				f := loadFixture(t, "decisions/uc04_le_approved.json")
				if err := json.Unmarshal(c.Fields, f.Request.Scoring.LegalEntity); err != nil {
					t.Fatal(err)
				}
				q = legalQuestionnaire(f.Request)
			}
			got := criterion(t, q, c.ID)
			if float64(got.Points) != c.Points || got.MaxPoints != c.Max {
				t.Fatalf("got %+v want %g/%d", got, c.Points, c.Max)
			}
		})
	}
}
func TestEconomicBoundaries(t *testing.T) {
	var data struct {
		Cases []struct {
			ID     string  `json:"id"`
			Value  float64 `json:"value"`
			Points float64 `json:"expected_points"`
			Max    int     `json:"max_points"`
		} `json:"economic_cases"`
	}
	readJSON(t, "scoring_boundaries.json", &data)
	if len(data.Cases) != 368 {
		t.Fatalf("cases=%d", len(data.Cases))
	}
	for i, c := range data.Cases {
		t.Run(fmt.Sprintf("%s_%03d", c.ID, i), func(t *testing.T) {
			p, m := economicPoints(c.ID, c.Value)
			if p != c.Points || m != c.Max {
				t.Fatalf("x=%.12g got %g/%d want %g/%d", c.Value, p, m, c.Points, c.Max)
			}
		})
	}
}
func TestWorkbookEconomicExamples(t *testing.T) {
	var data struct {
		Examples []struct {
			ID     string
			Value  float64
			Points float64 `json:"expected_points"`
		}
	}
	readJSON(t, "scoring_reference_examples.json", &data)
	if len(data.Examples) != 31 {
		t.Fatal(len(data.Examples))
	}
	for _, c := range data.Examples {
		t.Run(c.ID, func(t *testing.T) {
			p, _ := economicPoints(c.ID, c.Value)
			if p != c.Points {
				t.Fatalf("got %g want %g", p, c.Points)
			}
		})
	}
}
func TestTotalAndGrade(t *testing.T) {
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
	for _, c := range data.Grades {
		t.Run(fmt.Sprint(c.Raw), func(t *testing.T) {
			n, g := totalAndGrade(c.Raw)
			if n != c.Total || g != c.Grade {
				t.Fatalf("got %d/%s", n, g)
			}
		})
	}
	for _, c := range data.Combined {
		n, g := totalAndGrade(c.Questionnaire + c.Economics)
		if n != c.Total || g != c.Grade {
			t.Fatalf("got %d/%s", n, g)
		}
	}
}

func TestDecisionFixturesThroughScoring(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "decisions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 18 {
		t.Fatal(len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			f := loadFixture(t, filepath.Join("decisions", filepath.Base(file)))
			a, err, source := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if (source.calls == 1) != f.Expected.StatementsCalled {
				t.Fatalf("B calls=%d want %t", source.calls, f.Expected.StatementsCalled)
			}
			want := RouteSelection
			switch f.Expected.Response.Status {
			case contracts.DecisionStatusNeedInfo:
				want = RouteNeedInfo
			case contracts.DecisionStatusRejected:
				want = RouteRejected
			case contracts.DecisionStatusUnderReview:
				if f.Expected.Response.ReviewReason != nil && *f.Expected.Response.ReviewReason == contracts.ReviewReasonBorrower && !hasExpectedReason(f, "NO_FEASIBLE_OFFER") {
					want = RouteManual
				}
			}
			if a.Route != want {
				t.Fatalf("route=%s want %s reasons=%+v", a.Route, want, a.Reasons)
			}
			compareScore(t, a.Score, f.Expected.Response.Score)
			if want != RouteSelection {
				for _, r := range f.Expected.Response.Reasons {
					if !hasReason(a, r.Code) {
						t.Errorf("missing reason %s: %+v", r.Code, a.Reasons)
					}
					if r.Code == "MISSING_DATA" {
						for _, field := range r.Fields {
							found := false
							for _, s := range a.Reasons[0].Fields {
								if s == field {
									found = true
								}
							}
							if !found {
								t.Errorf("missing field %s: %v", field, a.Reasons[0].Fields)
							}
						}
					}
				}
			}
		})
	}
}
func hasExpectedReason(f fixture, code string) bool {
	for _, r := range f.Expected.Response.Reasons {
		if r.Code == code {
			return true
		}
	}
	return false
}
func compareScore(t *testing.T, got, want *contracts.ScoreResult) {
	t.Helper()
	if got == nil || want == nil {
		if got != nil || want != nil {
			t.Fatalf("score got=%v want=%v", got, want)
		}
		return
	}
	if got.Model != want.Model || got.RawTotal != want.RawTotal || got.Total != want.Total || got.Grade != want.Grade {
		t.Errorf("score %s %g/%d/%s want %s %g/%d/%s", got.Model, got.RawTotal, got.Total, got.Grade, want.Model, want.RawTotal, want.Total, want.Grade)
	}
	if !reflect.DeepEqual(got.Questionnaire, want.Questionnaire) {
		if len(got.Questionnaire.Criteria) != len(want.Questionnaire.Criteria) {
			t.Fatal("questionnaire criterion count")
		}
		for _, c := range want.Questionnaire.Criteria {
			actual := criterion(t, got.Questionnaire, c.ID)
			if actual != c {
				t.Errorf("question got=%+v want=%+v", actual, c)
			}
		}
		if got.Questionnaire.Points != want.Questionnaire.Points || got.Questionnaire.MaxPoints != 750 {
			t.Errorf("question totals %+v", got.Questionnaire)
		}
	}
	if got.Economics.Points != want.Economics.Points || got.Economics.MaxPoints != 250 {
		t.Errorf("economic totals %g want %g", got.Economics.Points, want.Economics.Points)
	}
	if len(got.Economics.Criteria) != len(want.Economics.Criteria) {
		t.Fatal("economic criterion count")
	}
	for _, c := range want.Economics.Criteria {
		actual := economic(t, got.Economics, c.ID)
		if actual.Points != c.Points || actual.MaxPoints != c.MaxPoints {
			t.Errorf("economic %s got %g/%d want %g/%d", c.ID, actual.Points, actual.MaxPoints, c.Points, c.MaxPoints)
		}
		if actual.Value == nil || c.Value == nil {
			if actual.Value != nil || c.Value != nil {
				t.Errorf("%s nil mismatch", c.ID)
			}
		} else if math.Abs(*actual.Value-*c.Value) > 1e-9 {
			t.Errorf("%s value %.13g want %.13g", c.ID, *actual.Value, *c.Value)
		}
	}
}

// Mutation maps belong only to test fixtures; production uses named DTO fields.
func mutate(t *testing.T, r *contracts.DecisionRequest, changes map[string]json.RawMessage) {
	t.Helper()
	b, _ := json.Marshal(r)
	var tree map[string]any
	if err := json.Unmarshal(b, &tree); err != nil {
		t.Fatal(err)
	}
	for path, raw := range changes {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		setPath(t, tree, strings.Split(path, "."), value)
	}
	b, _ = json.Marshal(tree)
	*r = contracts.DecisionRequest{}
	if err := json.Unmarshal(b, r); err != nil {
		t.Fatal(err)
	}
}
func setPath(t *testing.T, node map[string]any, path []string, value any) {
	t.Helper()
	key := path[0]
	if strings.HasSuffix(key, "[*]") {
		key = strings.TrimSuffix(key, "[*]")
		for _, v := range node[key].([]any) {
			setPath(t, v.(map[string]any), path[1:], value)
		}
		return
	}
	if len(path) == 1 {
		node[key] = value
		return
	}
	setPath(t, node[key].(map[string]any), path[1:], value)
}
func change(path, raw string) map[string]json.RawMessage {
	return map[string]json.RawMessage{path: json.RawMessage(raw)}
}

func TestSingleFactorStops(t *testing.T) {
	var data struct {
		Cases []struct {
			Base   string `json:"base_case"`
			Set    map[string]json.RawMessage
			Reason string `json:"expected_reason"`
		} `json:"single_factor_stops"`
	}
	readJSON(t, "unit_cases.json", &data)
	if len(data.Cases) != 17 {
		t.Fatal(len(data.Cases))
	}
	for i, c := range data.Cases {
		t.Run(fmt.Sprintf("%s_%d", c.Reason, i), func(t *testing.T) {
			f := loadFixture(t, c.Base)
			mutate(t, &f.Request, c.Set)
			a, err, source := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != RouteRejected || a.Score != nil || !hasReason(a, c.Reason) || source.calls != 0 {
				t.Fatalf("assessment=%+v B=%d", a, source.calls)
			}
		})
	}
}
func TestUnconfirmedCashStop(t *testing.T) {
	var data struct {
		Cases []struct {
			Base string `json:"base_case"`
			Set  map[string]json.RawMessage
		} `json:"cash_stop_confirmation"`
	}
	readJSON(t, "unit_cases.json", &data)
	for _, c := range data.Cases {
		t.Run(c.Base, func(t *testing.T) {
			f := loadFixture(t, c.Base)
			mutate(t, &f.Request, c.Set)
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

func TestDecodeAndInvalidValues(t *testing.T) {
	for _, s := range []string{"", "null", "[]", "{}", `{"application_id":1}`, `{"application_id":"id","borrower":{"id":"b","type":"SOLE_PROPRIETOR"},"financial_debt_rub":0.5}`, `{"application_id":"id","borrower":{"id":"b","type":"SOLE_PROPRIETOR"},"scoring":{"ip":{"active":"false"}}}`, `{} {}`} {
		t.Run(s, func(t *testing.T) { _, err := Decode(strings.NewReader(s)); checkError(t, err, 400) })
	}
	for _, c := range []struct{ path, raw string }{
		{"borrower.type", `"UNKNOWN"`}, {"facts_date", `"2026-02-30"`}, {"monthly_history[*].month", `"2026-13"`},
		{"financial_debt_rub", "-1"}, {"credit_request.amount_rub", "0"}, {"credit_request.term_months", "0"}, {"credit_request.min_acceptable_amount_rub", "0"}, {"credit_request.max_acceptable_term_months", "0"},
		{"credit_request.max_annual_rate", "100.01"}, {"credit_request.max_annual_rate", "-1"}, {"credit_request.max_annual_rate", "1.001"},
		{"scoring.ip.owner_age", "-1"}, {"scoring.ip.marital_status", `"INVALID"`}, {"scoring.ip.military_status", `"INVALID"`}, {"scoring.legal_entity", "{}"},
	} {
		t.Run(c.path+c.raw, func(t *testing.T) {
			f := loadFixture(t, "decisions/uc04_ip_approved.json")
			mutate(t, &f.Request, change(c.path, c.raw))
			_, err, source := assess(t, f)
			checkError(t, err, 422)
			if source.calls != 0 {
				t.Fatal("B called for invalid input")
			}
		})
	}
	for _, rate := range []float64{0, 1, 23.75, 100} {
		f := loadFixture(t, "decisions/uc04_ip_approved.json")
		f.Request.CreditRequest.MaxAnnualRate = &rate
		if err := Validate(f.Request); err != nil {
			t.Fatal(err)
		}
	}
	for _, rate := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		f := loadFixture(t, "decisions/uc04_ip_approved.json")
		f.Request.CreditRequest.MaxAnnualRate = &rate
		checkError(t, Validate(f.Request), 422)
	}
	f := loadFixture(t, "decisions/uc04_le_approved.json")
	f.Request.Scoring.LegalEntity.ReportVerificationLevel = ptr(4)
	checkError(t, Validate(f.Request), 422)
}

func TestEveryRequiredQuestionnaireFieldMissing(t *testing.T) {
	for _, kind := range []string{"ip", "le"} {
		base := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
		var p any
		branch := "scoring.ip."
		if kind == "ip" {
			p = base.Request.Scoring.IP
		} else {
			p = base.Request.Scoring.LegalEntity
			branch = "scoring.legal_entity."
		}
		typeOf := reflect.TypeOf(p).Elem()
		for i := 0; i < typeOf.NumField(); i++ {
			field := typeOf.Field(i).Tag.Get("json")
			if field == "military_status" {
				continue
			}
			t.Run(branch+field, func(t *testing.T) {
				f := loadFixture(t, "decisions/uc04_"+kind+"_approved.json")
				mutate(t, &f.Request, change(branch+field, "null"))
				a, err, _ := assess(t, f)
				if err != nil {
					t.Fatal(err)
				}
				if a.Route != RouteNeedInfo || a.Score != nil {
					t.Fatalf("got %+v", a)
				}
			})
		}
	}
}

func TestRoutingPriorityAndDataConsistency(t *testing.T) {
	for _, c := range []struct {
		name, base string
		changes    map[string]json.RawMessage
		route      Route
		noB        bool
	}{
		{"stop before missing", "uc04_le_approved", map[string]json.RawMessage{"scoring.legal_entity.bankrupt": json.RawMessage("true"), "facts_date": json.RawMessage("null"), "monthly_history": json.RawMessage("null")}, RouteRejected, true},
		{"missing before manual", "uc04_ip_approved", map[string]json.RawMessage{"scoring.ip.material_account_restriction": json.RawMessage("true"), "financial_debt_rub": json.RawMessage("null")}, RouteNeedInfo, true},
		{"D before manual", "uc02_le_declined", change("scoring.legal_entity.material_account_restriction", "true"), RouteRejected, false},
		{"stale facts", "uc04_ip_approved", change("facts_date", `"2026-09-07"`), RouteNeedInfo, true},
		{"future facts", "uc04_ip_approved", change("facts_date", `"2026-10-24"`), RouteNeedInfo, true},
		{"minimum exceeds request", "uc04_ip_approved", change("credit_request.min_acceptable_amount_rub", "900000000"), RouteNeedInfo, true},
		{"max term below requested", "uc04_ip_approved", change("credit_request.max_acceptable_term_months", "1"), RouteNeedInfo, true},
		{"history absent", "uc04_ip_approved", change("monthly_history", "[]"), RouteNeedInfo, true},
		{"future absent", "uc04_ip_approved", change("future_debt_payments", "[]"), RouteNeedInfo, true},
		{"duplicate history", "uc04_ip_approved", change("monthly_history[*].month", `"2026-09"`), RouteNeedInfo, true},
		{"duplicate future", "uc04_ip_approved", change("future_debt_payments[*].month", `"2026-11"`), RouteNeedInfo, true},
		{"zero debt positive payments", "uc04_ip_approved", change("financial_debt_rub", "0"), RouteNeedInfo, true},
		{"confirmation exceeds receipts", "uc04_ip_approved", change("evidence.confirmed_receipts_rub", "9000000000"), RouteNeedInfo, true},
		{"buyer exceeds receipts", "uc04_ip_approved", change("evidence.largest_buyer_group_receipts_rub", "9000000000"), RouteNeedInfo, true},
		{"confirmation below 80 percent LE", "uc04_le_approved", change("evidence.confirmed_receipts_rub", "1"), RouteNeedInfo, false},
		{"difference above 10 percent", "uc04_ip_approved", change("evidence.unexplained_difference_rub", "9000000000"), RouteNeedInfo, true},
		{"different purpose", "uc04_ip_approved", change("credit_request.purpose", `"OTHER"`), RouteManual, true},
		{"cash below minimum", "uc04_ip_approved", change("cash.free_after_own_contribution_rub", "0"), RouteManual, true},
		{"no uncovered need", "uc04_ip_approved", change("need.confirmed_cost_rub", "0"), RouteManual, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := loadFixture(t, "decisions/"+c.base+".json")
			mutate(t, &f.Request, c.changes)
			a, err, s := assess(t, f)
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != c.route || c.noB && s.calls != 0 {
				t.Fatalf("got %+v calls=%d", a, s.calls)
			}
		})
	}
}
