package optimizer

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

func ptr[T any](v T) *T { return &v }
func policy(t *testing.T) config.Policy {
	t.Helper()
	p, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func selector(t *testing.T, p config.Policy) *Selector {
	t.Helper()
	s, err := New(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func simpleInput() Input {
	return Input{RequestedTerms: contracts.CreditRequest{AmountRUB: ptr(int64(3000000)), TermMonths: ptr(24), MinAcceptableAmountRUB: ptr(int64(1000000)), MaxAcceptableTermMonths: ptr(24), MaxAnnualRate: ptr(24.0)}, Need: contracts.Need{ConfirmedCostRUB: ptr(int64(4000000)), OwnContributionRUB: ptr(int64(1000000))}, AnnualCFADSRUB: 12000000}
}
func selectOK(t *testing.T, s *Selector, in Input) Result {
	t.Helper()
	r, err := s.Select(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func checkMoney(t *testing.T, name string, actual, expected float64) {
	t.Helper()
	if math.Abs(actual-expected) > .01 {
		t.Errorf("%s=%0.12f want %0.12f", name, actual, expected)
	}
}
func assertNoOffer(t *testing.T, r Result) {
	t.Helper()
	if r.Terms != nil || len(r.Reasons) != 1 || r.Reasons[0].Code != "NO_FEASIBLE_OFFER" || r.Audit.FeasibleCandidateCount != 0 {
		t.Fatalf("expected no offer, got %+v", r)
	}
}

type fixture struct {
	Request    contracts.DecisionRequest    `json:"request"`
	Statements contracts.StatementsResponse `json:"service_b_response"`
	Expected   struct {
		Selection   *contracts.SelectionAudit   `json:"selection"`
		BankRequest *contracts.BankCheckRequest `json:"bank_check_request"`
		Response    contracts.DecisionResponse  `json:"response"`
	} `json:"expected"`
}
type statementsStub struct{ response contracts.StatementsResponse }

func (s statementsStub) Statements(context.Context, string, []int) (contracts.StatementsResponse, error) {
	return s.response, nil
}

func TestScoringAndRealSelectionFixtures(t *testing.T) {
	s := selector(t, policy(t))
	files, err := filepath.Glob("../../testdata/decisions/*.json")
	if err != nil {
		t.Fatal(err)
	}
	selected, empty, bypassed := 0, 0, 0
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var f fixture
			if err = json.Unmarshal(b, &f); err != nil {
				t.Fatal(err)
			}
			e, err := scoring.NewEvaluator(statementsStub{f.Statements}, clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatal(err)
			}
			a, err := e.Evaluate(context.Background(), f.Request)
			if err != nil {
				t.Fatal(err)
			}
			if a.Route != scoring.RouteSelection {
				bypassed++
				return
			}
			before, _ := json.Marshal(a)
			r := selectOK(t, s, Input{RequestedTerms: *f.Request.CreditRequest, Need: *f.Request.Need, AnnualCFADSRUB: a.CashFlow.CFADS, FutureDebtPaymentsRUB: a.CashFlow.FutureDebt})
			after, _ := json.Marshal(a)
			if string(before) != string(after) {
				t.Fatal("selection changed the assessment")
			}
			if f.Expected.Selection == nil {
				empty++
				assertNoOffer(t, r)
				if f.Expected.Response.Score == nil || a.Score.Total != f.Expected.Response.Score.Total {
					t.Fatal("score must remain available on manual review")
				}
				return
			}
			selected++
			if r.Terms == nil || *r.Terms != f.Expected.BankRequest.Terms {
				t.Fatalf("terms=%+v want %+v", r.Terms, f.Expected.BankRequest.Terms)
			}
			want := *f.Expected.Selection
			if r.Audit.FeasibleCandidateCount != want.FeasibleCandidateCount || r.Audit.SameAmountCandidateCount != want.SameAmountCandidateCount || r.Audit.ProfitRetention != want.ProfitRetention {
				t.Errorf("audit=%+v want %+v", r.Audit, want)
			}
			checkMoney(t, "maximum NPV", r.Audit.MaxNPVRUB, want.MaxNPVRUB)
			checkMoney(t, "selected NPV", r.Audit.SelectedNPVRUB, want.SelectedNPVRUB)
			checkMoney(t, "payment", r.Audit.MonthlyPaymentRUB, want.MonthlyPaymentRUB)
			checkMoney(t, "overpayment", r.Audit.OverpaymentRUB, want.OverpaymentRUB)
			if len(r.Reasons) != 0 {
				t.Fatal(r.Reasons)
			}
			// The bank is never consulted here; even BANK_REVIEW fixtures retain
			// exactly the same selected deal for the subsequent independent check.
		})
	}
	if selected != 8 || empty != 2 || bypassed != 8 {
		t.Fatalf("selected=%d empty=%d bypassed=%d", selected, empty, bypassed)
	}
}

func TestClientEndpointsAndNeedLimit(t *testing.T) {
	s := selector(t, policy(t))
	for _, c := range []struct {
		name                            string
		upper, minimum, cost, own, want int64
	}{
		{"exact requested upper", 3000123, 1000000, 5000000, 0, 3000123},
		{"exact need upper", 3000000, 1000000, 3500456, 1000000, 2500456},
		{"minimum above need", 3000000, 2900000, 3000000, 2000000, 0},
		{"need zero", 3000000, 1000000, 0, 0, 0},
		{"own contribution covers need", 3000000, 1000000, 1000000, 2000000, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := simpleInput()
			in.RequestedTerms.AmountRUB = &c.upper
			in.RequestedTerms.MinAcceptableAmountRUB = &c.minimum
			in.Need.ConfirmedCostRUB = &c.cost
			in.Need.OwnContributionRUB = &c.own
			r := selectOK(t, s, in)
			if c.want == 0 {
				assertNoOffer(t, r)
			} else if r.Terms == nil || r.Terms.AmountRUB != c.want {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestMaxAmountGridAndExactBounds(t *testing.T) {
	for _, c := range []struct {
		name                 string
		upper, minimum, step int64
		cap, factor          float64
		want                 int64
	}{
		{"exact upper", 123456, 10000, 10000, 123456, 1, 123456},
		{"floor capacity", 100000, 10000, 10000, 45999.99, 1, 40000},
		{"exact grid", 100000, 10000, 10000, 40000, 1, 40000},
		{"exact minimum", 100000, 12345, 10000, 15000, 1, 12345},
		{"below minimum", 100000, 12345, 10000, 12344.99, 1, 0},
		{"upper below minimum", 10000, 10001, 10000, 90000, 1, 0},
		{"step larger than upper", 9999, 123, 10000, 125, 1, 123},
		{"zero rate factor", 100000, 12345, 10000, 5000, 1.0 / 3, 12345},
		{"int64 upper", math.MaxInt64, 1, 10000, 1e25, 1, math.MaxInt64},
		{"grid division boundary", 10000000, 1, 10000, 3000000 * .0528710958078074, .0528710958078074, 3000000},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := maxAmount(c.upper, c.minimum, c.step, c.cap, c.factor)
			if got != c.want {
				t.Fatalf("amount=%d want %d", got, c.want)
			}
			if got > 0 && float64(got)*c.factor > c.cap {
				t.Fatal("unaffordable amount")
			}
		})
	}
}

func TestRateMaximumAndPercentUnits(t *testing.T) {
	p := policy(t)
	s := selector(t, p)
	in := simpleInput()
	in.RequestedTerms.TermMonths = ptr(1)
	in.RequestedTerms.MaxAcceptableTermMonths = ptr(1)
	in.RequestedTerms.MaxAnnualRate = ptr(20.09)
	in.AnnualCFADSRUB = 1e9
	r := selectOK(t, s, in)
	if r.Terms == nil || r.Terms.AnnualRate != 20.09 || r.Audit.FeasibleCandidateCount != 1 {
		t.Fatalf("off-grid maximum: %+v", r)
	}
	for _, rate := range []float64{0, 1} {
		in.RequestedTerms.MaxAnnualRate = &rate
		assertNoOffer(t, selectOK(t, s, in))
	}
	p.Economics = config.Economics{QualityCategory: 2}
	s = selector(t, p)
	in.RequestedTerms.MaxAnnualRate = ptr(0.0)
	assertNoOffer(t, selectOK(t, s, in))
	for _, c := range []struct{ maximum, want float64 }{{1, 1}, {100, 90}} {
		in.RequestedTerms.MaxAnnualRate = &c.maximum
		r = selectOK(t, s, in)
		if r.Terms == nil || r.Terms.AnnualRate != c.want {
			t.Fatalf("maximum=%g result=%+v", c.maximum, r)
		}
	}
	p.Optimizer.RateStep = .0003
	in.RequestedTerms.MaxAnnualRate = ptr(.1)
	r = selectOK(t, selector(t, p), in)
	if r.Terms == nil || r.Terms.AnnualRate != .09 {
		t.Fatalf("configured three-basis-point step ignored: %+v", r)
	}
	for _, c := range []struct {
		maximum, step int
		want          []int
	}{{0, 25, []int{0}}, {100, 25, []int{0, 25, 50, 75, 100}}, {101, 25, []int{0, 25, 50, 75, 100, 101}}, {1, 25, []int{0, 1}}} {
		if got := rateGrid(c.maximum, c.step); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("grid=%v want %v", got, c.want)
		}
	}
}

func TestCashFlowPrefixesAndTermHorizon(t *testing.T) {
	s := selector(t, policy(t))
	for _, cfads := range []float64{-1, 0} {
		in := simpleInput()
		in.AnnualCFADSRUB = cfads
		assertNoOffer(t, selectOK(t, s, in))
	}
	for _, debt := range []float64{750000, 750001} {
		in := simpleInput()
		in.FutureDebtPaymentsRUB[0] = debt
		assertNoOffer(t, selectOK(t, s, in))
	}
	in := simpleInput()
	in.RequestedTerms.MinAcceptableAmountRUB = ptr(int64(1))
	in.FutureDebtPaymentsRUB[2] = 750000
	r := selectOK(t, s, in)
	if r.Terms == nil || r.Terms.TermMonths > 2 {
		t.Fatalf("worst month ignored: %+v", r)
	}
	in = simpleInput()
	before := selectOK(t, s, in)
	in.FutureDebtPaymentsRUB[24] = 1e8
	after := selectOK(t, s, in)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("payment outside term horizon affected selection")
	}
	in = simpleInput()
	in.RequestedTerms.TermMonths = ptr(60)
	in.RequestedTerms.MaxAcceptableTermMonths = ptr(100)
	r = selectOK(t, s, in)
	if r.Terms == nil || r.Terms.TermMonths > 36 {
		t.Fatalf("policy horizon ignored: %+v", r)
	}
	p := policy(t)
	p.Optimizer.MaxTermMonths = 2
	s = selector(t, p)
	r = selectOK(t, s, in)
	if r.Terms == nil || r.Terms.TermMonths > 2 {
		t.Fatalf("configured horizon ignored: %+v", r)
	}
}

func TestInvalidInputAndCalculationFailure(t *testing.T) {
	s := selector(t, policy(t))
	for _, c := range []struct {
		name   string
		change func(*Input)
	}{
		{"amount missing", func(i *Input) { i.RequestedTerms.AmountRUB = nil }}, {"need missing", func(i *Input) { i.Need.OwnContributionRUB = nil }},
		{"amount zero", func(i *Input) { i.RequestedTerms.AmountRUB = ptr(int64(0)) }}, {"minimum exceeds request", func(i *Input) { i.RequestedTerms.MinAcceptableAmountRUB = ptr(int64(9000000)) }},
		{"term zero", func(i *Input) { i.RequestedTerms.TermMonths = ptr(0) }}, {"term contradiction", func(i *Input) { i.RequestedTerms.MaxAcceptableTermMonths = ptr(1) }},
		{"negative need", func(i *Input) { i.Need.ConfirmedCostRUB = ptr(int64(-1)) }}, {"negative own", func(i *Input) { i.Need.OwnContributionRUB = ptr(int64(-1)) }},
		{"rate negative", func(i *Input) { i.RequestedTerms.MaxAnnualRate = ptr(-1.0) }}, {"rate above 100", func(i *Input) { i.RequestedTerms.MaxAnnualRate = ptr(100.01) }}, {"rate excess precision", func(i *Input) { i.RequestedTerms.MaxAnnualRate = ptr(24.001) }},
		{"NaN CFADS", func(i *Input) { i.AnnualCFADSRUB = math.NaN() }}, {"infinite CFADS", func(i *Input) { i.AnnualCFADSRUB = math.Inf(1) }},
		{"negative debt", func(i *Input) { i.FutureDebtPaymentsRUB[35] = -1 }}, {"NaN debt", func(i *Input) { i.FutureDebtPaymentsRUB[35] = math.NaN() }}, {"infinite debt", func(i *Input) { i.FutureDebtPaymentsRUB[35] = math.Inf(1) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := simpleInput()
			c.change(&in)
			if _, err := s.Select(in); !errors.Is(err, economics.ErrInvalidInput) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	for _, s := range []*Selector{nil, {}} {
		if _, err := s.Select(simpleInput()); !errors.Is(err, economics.ErrInvalidInput) {
			t.Fatalf("zero selector: %v", err)
		}
	}
	p := policy(t)
	p.Optimizer.AmountStepRUB = 0
	if _, err := New(p); !errors.Is(err, economics.ErrInvalidInput) {
		t.Fatal(err)
	}
	in := simpleInput()
	in.RequestedTerms.AmountRUB = ptr(int64(math.MaxInt64))
	in.RequestedTerms.MinAcceptableAmountRUB = ptr(int64(math.MaxInt64))
	in.Need.ConfirmedCostRUB = ptr(int64(math.MaxInt64))
	in.Need.OwnContributionRUB = ptr(int64(0))
	in.AnnualCFADSRUB = 1e30
	if _, err := s.Select(in); !errors.Is(err, economics.ErrAmountOverflow) {
		t.Fatalf("overflow must not become manual review: %v", err)
	}
}
