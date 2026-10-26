package sourcecatalog_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	m "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

func load(t *testing.T, version string) m.ModelDefinition {
	t.Helper()
	f, err := os.Open("../../models/" + version + ".json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := m.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func read(t *testing.T, path string, out any) {
	t.Helper()
	f, err := os.Open("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.UseNumber()
	if err = decoder.Decode(out); err != nil {
		t.Fatal(err)
	}
}
func criteria(t *testing.T) map[string]m.Criterion {
	t.Helper()
	out := map[string]m.Criterion{}
	for _, version := range []string{"ip-v2", "legal-v2"} {
		for _, c := range load(t, version).Criteria {
			out[c.ID] = c
		}
	}
	return out
}

// Independent test-only predicate interpreter. It does not calculate money,
// contact suppliers or implement the production engine of plan step 7.
// -1 unknown, 0 false, 1 true; NOT never turns absence into a known value.
func predicate(c m.Condition, facts map[string]any, binary bool) int {
	truth := func(ok bool) int {
		if ok {
			return 1
		}
		return 0
	}
	switch c.Op {
	case m.Always:
		return 1
	case m.Never:
		return 0
	case m.Not:
		r := predicate(c.Conditions[0], facts, binary)
		if r < 0 {
			return -1
		}
		return 1 - r
	case m.All, m.Any:
		unknown := false
		for _, child := range c.Conditions {
			r := predicate(child, facts, binary)
			if r < 0 {
				unknown = true
			}
			if c.Op == m.All && r == 0 {
				return 0
			}
			if c.Op == m.Any && r == 1 {
				return 1
			}
		}
		if unknown {
			return -1
		}
		return truth(c.Op == m.All)
	}
	a, known := facts[c.Parameter]
	known = known && a != nil
	if c.Op == m.Known {
		return truth(known)
	}
	if c.Op == m.Unknown {
		return truth(!known)
	}
	if !known {
		return -1
	}
	var z any
	if c.OtherParameter != "" {
		var ok bool
		z, ok = facts[c.OtherParameter]
		if !ok || z == nil {
			return -1
		}
	} else if c.Value != nil {
		if c.Value.Number != nil {
			z = *c.Value.Number
		}
		if c.Value.Boolean != nil {
			z = *c.Value.Boolean
		}
		if c.Value.String != nil {
			z = *c.Value.String
		}
	}
	comparison := 0
	switch x := a.(type) {
	case json.Number:
		y := z.(json.Number)
		if binary {
			xf, _ := x.Float64()
			yf, _ := y.Float64()
			if xf < yf {
				comparison = -1
			}
			if xf > yf {
				comparison = 1
			}
		} else {
			xr, ok := new(big.Rat).SetString(x.String())
			if !ok {
				panic(x)
			}
			yr, ok := new(big.Rat).SetString(y.String())
			if !ok {
				panic(y)
			}
			comparison = xr.Cmp(yr)
		}
	case string:
		comparison = strings.Compare(x, z.(string))
	case bool:
		if x != z.(bool) {
			comparison = 1
		}
	default:
		panic(fmt.Sprintf("unsupported test fact %T", a))
	}
	switch c.Op {
	case m.EQ:
		return truth(comparison == 0)
	case m.NE:
		return truth(comparison != 0)
	case m.LT:
		return truth(comparison < 0)
	case m.LE:
		return truth(comparison <= 0)
	case m.GT:
		return truth(comparison > 0)
	case m.GE:
		return truth(comparison >= 0)
	}
	panic(c.Op)
}
func match(t *testing.T, cases []m.ScoreCase, facts map[string]any, binary bool) m.ScoreCase {
	t.Helper()
	var out m.ScoreCase
	count := 0
	for _, c := range cases {
		if predicate(c.When, facts, binary) == 1 {
			out = c
			count++
		}
	}
	if count != 1 {
		t.Fatalf("matched %d cases for %v", count, facts)
	}
	return out
}

func TestV2DefinitionsAndTrustedCatalog(t *testing.T) {
	for _, v := range []struct {
		version             string
		q, e, stops, manual int
	}{{"ip-v2", 31, 13, 7, 14}, {"legal-v2", 27, 18, 8, 20}} {
		t.Run(v.version, func(t *testing.T) {
			d := load(t, v.version)
			bindings, err := sourcecatalog.Bindings(d.BorrowerType)
			if err != nil {
				t.Fatal(err)
			}
			result := m.Validate(d, bindings)
			if !result.Valid {
				t.Fatalf("%+v", result.Issues)
			}
			if result.QuestionnaireMaximum != 750 || result.AdditionalMaximum != 250 || result.TotalMaximum != 1000 {
				t.Fatal(result)
			}
			if d.Version != v.version || d.FactsSchemaVersion != "" || len(d.Criteria) != v.q+v.e || len(d.Stops) != v.stops || len(d.ManualReviews) != v.manual {
				t.Fatal("model identity/cardinality")
			}
			for i, c := range d.Criteria {
				prefix, n := map[bool]string{true: "IP", false: "LE"}[d.ID == "ip"], i+1
				block := "Q"
				if i >= v.q {
					n = i - v.q + 1
					block = "E"
				}
				if c.ID != fmt.Sprintf("%s_%s%02d", prefix, block, n) {
					t.Fatal("criterion order", c.ID)
				}
			}
			for _, p := range d.Parameters {
				if p.Source.Kind == m.Supplier {
					t.Fatal("v2 invented supplier facts")
				}
			}
		})
	}
	if _, err := sourcecatalog.Entries("unsupported"); err == nil {
		t.Fatal("unknown borrower accepted")
	}
}

func TestV2QuestionnaireBoundaryExamples(t *testing.T) {
	var fixture struct {
		Cases []struct {
			ID     string         `json:"id"`
			Fields map[string]any `json:"raw_fields"`
			Points float64        `json:"expected_points"`
			Max    int            `json:"max_points"`
		} `json:"questionnaire_cases"`
	}
	read(t, "testdata/scoring_boundaries.json", &fixture)
	if len(fixture.Cases) != 393 {
		t.Fatal(len(fixture.Cases))
	}
	all := criteria(t)
	for i, x := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d", x.ID, i), func(t *testing.T) {
			c := all[x.ID]
			for field, value := range x.Fields {
				if value == nil {
					// These legacy fixtures exercise nil in a private classifier.
					// A real decision must request data before that classifier runs.
					required := false
					version := "legal-v2"
					if strings.HasPrefix(x.ID, "IP_") {
						version = "ip-v2"
					}
					for _, p := range load(t, version).Parameters {
						if p.ID == field {
							required = p.Required
						}
					}
					if !required || c.UnknownAction != m.NeedInfo || x.Points != 0 {
						t.Fatal("null bypasses completeness", x)
					}
					for _, band := range c.Cases {
						if predicate(band.When, x.Fields, false) == 1 {
							t.Fatal("null treated as known", field)
						}
					}
					return
				}
			}
			got := match(t, c.Cases, x.Fields, false)
			if got.Points != x.Points || c.MaxPoints != x.Max {
				t.Fatalf("got %g/%d want %g/%d", got.Points, c.MaxPoints, x.Points, x.Max)
			}
		})
	}
}

func TestV2EconomicBoundaryAndReferenceExamples(t *testing.T) {
	type example struct {
		ID     string      `json:"id"`
		Value  json.Number `json:"value"`
		Points float64     `json:"expected_points"`
		Max    int         `json:"max_points"`
	}
	var boundary struct {
		Cases []example `json:"economic_cases"`
	}
	var reference struct {
		Cases []example `json:"examples"`
	}
	read(t, "testdata/scoring_boundaries.json", &boundary)
	read(t, "testdata/scoring_reference_examples.json", &reference)
	if len(boundary.Cases) != 368 || len(reference.Cases) != 31 {
		t.Fatal(len(boundary.Cases), len(reference.Cases))
	}
	all := criteria(t)
	for _, group := range []struct {
		name  string
		cases []example
	}{{"boundaries", boundary.Cases}, {"references", reference.Cases}} {
		t.Run(group.name, func(t *testing.T) {
			for i, x := range group.cases {
				t.Run(fmt.Sprintf("%s/%d", x.ID, i), func(t *testing.T) {
					c := all[x.ID]
					got := match(t, c.Cases, map[string]any{c.ValueParameter: x.Value}, true)
					if got.Points != x.Points || x.Max != 0 && c.MaxPoints != x.Max {
						t.Fatalf("got %g/%d want %g/%d", got.Points, c.MaxPoints, x.Points, x.Max)
					}
				})
			}
		})
	}
}

func TestV2DocumentationCriteria(t *testing.T) {
	f, err := os.Open("../../docs/02_inputs_and_scoring.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	all := criteria(t)
	seen := map[string]bool{}
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "| IP_Q") && !strings.HasPrefix(line, "| LE_Q") && !strings.HasPrefix(line, "| IP_E") && !strings.HasPrefix(line, "| LE_E") {
			continue
		}
		parts := strings.Split(line, "|")
		label := strings.TrimSpace(parts[1])
		dot := strings.Index(label, ".")
		id := label[:dot]
		c, ok := all[id]
		if !ok || seen[id] {
			t.Fatal("missing/duplicate", id)
		}
		seen[id] = true
		max, err := strconv.Atoi(strings.TrimSpace(parts[4]))
		if err != nil {
			t.Fatal(err)
		}
		if c.Title != strings.TrimSpace(label[dot+1:]) || c.MaxPoints != max {
			t.Fatal("documentation differs", id)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 89 {
		t.Fatal(len(seen))
	}
}

func TestCatalogCopyIsolationAndNativeNumbers(t *testing.T) {
	for _, b := range []contracts.BorrowerType{contracts.BorrowerTypeSoleProprietor, contracts.BorrowerTypeLegalEntity} {
		before, err := sourcecatalog.Entries(b)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := sourcecatalog.Entries(b)
		if err != nil {
			t.Fatal(err)
		}
		for i := range changed {
			p := &changed[i].Parameter
			p.Title = "changed"
			if p.Number != nil {
				p.Number.Integer = !p.Number.Integer
				if p.Number.Lower != nil {
					p.Number.Lower.Value = "42"
				}
			}
			if len(p.StringValues) > 0 {
				p.StringValues[0] = "changed"
			}
			if len(p.DependsOn) > 0 {
				p.DependsOn[0] = "changed"
			}
			if len(changed[i].Binding.DependsOn) > 0 {
				changed[i].Binding.DependsOn[0].Path = "changed"
			}
		}
		after, err := sourcecatalog.Entries(b)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("catalog mutation leaked")
		}
		for _, e := range before {
			if e.Parameter.Source.Kind == m.Application && e.Parameter.Type == m.Number && e.Parameter.ID != "max_annual_rate" && e.Binding.NumericKind != m.NumericInt64 {
				t.Fatal(e)
			}
			if e.Parameter.ID == "confirmed_receipts_ratio" && e.Binding.NumericKind != m.NumericRational {
				t.Fatal(e)
			}
		}
	}
}

func TestV2OptionalMilitaryControlAndCashStopDependencies(t *testing.T) {
	d := load(t, "ip-v2")
	c := d.Criteria[30]
	if c.ID != "IP_Q31" || c.UnknownAction != m.Zero || c.Answer.UnknownText != "OTHER_OR_UNKNOWN" || c.MaxPoints != 0 {
		t.Fatal(c)
	}
	for _, version := range []string{"ip-v2", "legal-v2"} {
		d := load(t, version)
		stop := d.Stops[len(d.Stops)-1]
		if stop.ID != "NONPOSITIVE_VERIFIED_CFADS" {
			t.Fatal(stop.ID)
		}
		for _, id := range stop.RequiresChecks {
			if id == "borrower_name" || id == "borrower_inn" || id == "statements_complete" {
				t.Fatal("cash stop delayed by unrelated missing data")
			}
		}
		if len(stop.RequiresChecks) < 8 {
			t.Fatal("unconfirmed financial stop")
		}
		for _, c := range d.Criteria {
			if c.Block == m.Additional {
				for _, s := range c.SpecialCases {
					if s.Result == nil || s.Result.Mode != m.NullResult {
						t.Fatal(c.ID, "special value not null")
					}
					if s.ReviewReason != nil && s.ReviewReason.Code != "SPECIAL_"+c.ID {
						t.Fatal(c.ID, "special reason")
					}
				}
			}
		}
	}
}

func TestV2ReasonTextsAndManualOrder(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "../scoring/reasons.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	messages := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		cl, ok := node.(*ast.CaseClause)
		if !ok || len(cl.List) == 0 {
			return true
		}
		ret, ok := cl.Body[0].(*ast.ReturnStmt)
		if !ok {
			return true
		}
		message, err := strconv.Unquote(ret.Results[0].(*ast.BasicLit).Value)
		if err != nil {
			t.Fatal(err)
		}
		for _, expr := range cl.List {
			key, err := strconv.Unquote(expr.(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			messages[key] = message
		}
		return true
	})
	for code, text := range map[string]string{"NONPOSITIVE_VERIFIED_CFADS": "Подтверждённый годовой CFADS неположителен.", "NONPOSITIVE_EQUITY": "Неположительный собственный капитал.", "NONPOSITIVE_SALES_PROFIT": "Неположительная прибыль от продаж.", "TWO_YEAR_NET_LOSS": "Убыток в обоих годах.", "SCORE_CLASS_C": "Итоговый класс C.", "SCORE_CLASS_D": "Итоговый класс D."} {
		messages[code] = text
	}
	seen := map[string]bool{}
	check := func(r m.Reason) {
		t.Helper()
		if r.Message != messages[r.Code] || r.Message == "" {
			t.Fatal("legacy reason text differs", r)
		}
		seen[r.Code] = true
	}
	for _, v := range []string{"ip-v2", "legal-v2"} {
		d := load(t, v)
		for _, r := range d.Stops {
			check(r.Reason)
		}
		for _, r := range d.ManualReviews {
			check(r.Reason)
		}
		for _, c := range d.Criteria {
			for _, s := range c.SpecialCases {
				if s.ReviewReason != nil {
					check(*s.ReviewReason)
				}
			}
		}
		for _, c := range d.Classes {
			if c.Reason != nil {
				check(*c.Reason)
			}
		}
		want := strings.Fields("UNEXPLAINED_DIFFERENCE NONSTANDARD_PURPOSE NO_UNCOVERED_NEED INSUFFICIENT_FREE_CASH NO_CREDIT_HISTORY BUSINESS_AGE_UNDER_12_MONTHS CURRENT_DPD_1_29 MANY_CREDIT_APPLICATIONS MATERIAL_ACCOUNT_RESTRICTION MATERIAL_DISPUTE_OR_ENFORCEMENT KEY_BUYER_LOSS STRUCTURAL_BUSINESS_CHANGE OVERDUE_TAXES_OVER_10_PERCENT_CFADS FEWER_THAN_8_POSITIVE_CFADS_MONTHS")
		if v == "legal-v2" {
			want = append(want, strings.Fields("MATERIAL_REGISTER_INACCURACY MATERIAL_AUDITOR_MODIFICATION NON_OPERATING_NONFINANCIAL_SEGMENT NONPOSITIVE_EQUITY NONPOSITIVE_SALES_PROFIT TWO_YEAR_NET_LOSS")...)
		}
		for i, id := range want {
			if d.ManualReviews[i].ID != id {
				t.Fatal("manual order", v, i)
			}
		}
		if v == "legal-v2" {
			for i, want := range []string{"LE_E05", "LE_E12", "LE_E12"} {
				if d.ManualReviews[17+i].AfterCriterion != want {
					t.Fatal("economic reason order")
				}
			}
		}
	}
	if len(seen) != len(messages) {
		t.Fatalf("reason coverage %d/%d", len(seen), len(messages))
	}
}

func TestV2EverySpecialCase(t *testing.T) {
	type example struct {
		id, setup string
		points    float64
		review    bool
	}
	cases := []example{
		{"IP_E01", "receipts_rub=0", 0, false}, {"IP_E02", "cfads_rub=0", 0, false}, {"IP_E03", "cfads_rub=-1", 0, false}, {"IP_E04", "monthly_required_rub=0", 25, false},
		{"IP_E06", "receipts_rub=0", 0, false}, {"IP_E07", "previous_comparable_receipts_rub=0", 0, true}, {"IP_E08", "previous_quarter_receipts_rub=0", 0, true},
		{"IP_E09", "receipts_rub=0", 0, false}, {"IP_E10", "receipts_rub=0", 0, false}, {"IP_E11", "receipts_rub=0", 0, false},
		{"IP_E12", "confirmed_cost_rub=0", 0, true}, {"IP_E13", "receipts_minus_variable_rub=0", 0, true},
		{"LE_E01", "current_current_liabilities=0", 20, false}, {"LE_E02", "current_current_liabilities=0", 15, false}, {"LE_E03", "current_current_liabilities=0", 10, false},
		{"LE_E04", "current_total_assets=0", 0, true}, {"LE_E05", "current_total_assets=0", 0, true}, {"LE_E06", "current_current_assets=0", 0, true},
		{"LE_E07", "current_sales_profit=-1", 0, true}, {"LE_E08", "current_sales_profit=0", 0, true}, {"LE_E08", "current_sales_profit=1 current_interest_payable=0", 20, false},
		{"LE_E09", "financial_borrowings_rub=0", 10, false}, {"LE_E10", "current_revenue=0", 0, true}, {"LE_E11", "current_revenue=0", 0, true}, {"LE_E12", "current_revenue=0", 0, true},
		{"LE_E13", "average_total_assets=0", 0, true}, {"LE_E14", "current_equity=0 average_equity=1", 0, true}, {"LE_E14", "current_equity=1 average_equity=0", 0, true},
		{"LE_E15", "previous_revenue=0", 0, true}, {"LE_E16", "average_total_assets=0", 0, true}, {"LE_E17", "current_revenue=0", 0, true},
		{"LE_E18", "current_cost_of_sales=0 average_inventories=0", 10, false}, {"LE_E18", "current_cost_of_sales=0 average_inventories=1", 0, true},
	}
	all := criteria(t)
	seen := map[string]bool{}
	for i, x := range cases {
		t.Run(fmt.Sprintf("%s/%d", x.id, i), func(t *testing.T) {
			facts := map[string]any{}
			for _, v := range []string{"ip-v2", "legal-v2"} {
				for _, p := range load(t, v).Parameters {
					if p.Type == m.Number {
						facts[p.ID] = json.Number("1")
					}
				}
			}
			c := all[x.id]
			delete(facts, c.ValueParameter)
			for _, pair := range strings.Fields(x.setup) {
				a := strings.Split(pair, "=")
				facts[a[0]] = json.Number(a[1])
			}
			got := match(t, c.SpecialCases, facts, false)
			if got.Points != x.points || (got.ReviewReason != nil) != x.review || got.Result == nil || got.Result.Mode != m.NullResult {
				t.Fatal(got)
			}
			seen[x.id+"/"+got.ID] = true
		})
	}
	for _, c := range all {
		for _, s := range c.SpecialCases {
			if !seen[c.ID+"/"+s.ID] {
				t.Fatal("untested special", c.ID, s.ID)
			}
		}
	}
}
