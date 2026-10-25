package bank

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func findCheck(t *testing.T, checks []contracts.CheckResult, code string) contracts.CheckResult {
	t.Helper()
	for _, c := range checks {
		if c.Code == code {
			return c
		}
	}
	t.Fatalf("missing check %s", code)
	return contracts.CheckResult{}
}
func project(t *testing.T, p *Projector, b contracts.BankState, terms contracts.LoanTerms) Projection {
	t.Helper()
	r, err := p.Project(b, "application", "borrower", terms)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInitialNormsAndEveryInclusiveBoundary(t *testing.T) {
	state := initial(t)
	p := policy(t)
	values := norms(state, p.LargeExposureThreshold)
	for code, want := range map[string]float64{"N1_1": .08125, "N1_2": .1, "N1_0": .125, "N2": .3, "N3": .625, "N4": .75, "N6": .2, "N7": .48, "N25": .1, "N12": .15} {
		closeNumber(t, code, values[code], &want, 1e-12)
	}
	f := deal(t)
	original, err := NewProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	baseline := project(t, original, state, f.Request.Terms)
	for _, code := range normOrder {
		t.Run(code, func(t *testing.T) {
			value := *findCheck(t, baseline.Checks, code).After
			current := policy(t)
			limit := current.NormLimits[code]
			limit.Value = value
			current.NormLimits[code] = limit
			projector, err := NewProjector(current)
			if err != nil {
				t.Fatal(err)
			}
			equal := project(t, projector, state, f.Request.Terms)
			if !findCheck(t, equal.Checks, code).Passed {
				t.Fatal("equal threshold failed")
			}
			if limit.Operator == ">=" {
				limit.Value = math.Nextafter(value, math.Inf(1))
			} else {
				limit.Value = math.Nextafter(value, math.Inf(-1))
			}
			current.NormLimits[code] = limit
			projector, err = NewProjector(current)
			if err != nil {
				t.Fatal(err)
			}
			beyond := project(t, projector, state, f.Request.Terms)
			if findCheck(t, beyond.Checks, code).Passed || beyond.Issued {
				t.Fatal("one representable step beyond threshold passed")
			}
		})
	}
}

func TestGroupsRelatedRisksAndStrictLargeThreshold(t *testing.T) {
	f := deal(t)
	p := policy(t)
	b := initial(t)
	b.Groups = map[string]float64{"at": 50000000, "above": 50000001, "smaller": 40000000}
	b.RelatedGroups = []string{}
	values := norms(b, .05)
	closeNumber(t, "N7 strict >", values["N7"], ptr(.050000001), 1e-15)
	closeNumber(t, "N25 empty", values["N25"], ptr(0.0), 0)
	b = initial(t)
	b.BorrowerGroups[f.Request.Borrower.ID] = "related"
	projector, err := NewProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	after, err := projector.Project(b, f.Request.ApplicationID, f.Request.Borrower.ID, f.Request.Terms)
	if err != nil {
		t.Fatal(err)
	}
	closeNumber(t, "related group", ptr(after.State.Groups["related"]), ptr(102850000.0), 0)
	closeNumber(t, "N25", findCheck(t, after.Checks, "N25").After, ptr(102850000.0/999850000), 1e-15)
	if _, ok := after.State.Groups[f.Request.Borrower.ID]; ok {
		t.Fatal("mapped borrower created a second group")
	}
	if b.Groups["related"] != 100000000 {
		t.Fatal("projection mutated input groups")
	}
	b.RelatedGroups = []string{"g1", "related"}
	values = norms(b, .05)
	closeNumber(t, "maximum related", values["N25"], ptr(.2), 0)
}

func TestUpfrontCostReserveAndTermBoundaries(t *testing.T) {
	f := deal(t)
	p := policy(t)
	p.Economics.UpfrontCostRUB = 10000
	projector, err := NewProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	r := project(t, projector, f.Initial, f.Request.Terms)
	closeNumber(t, "cash", ptr(r.State.Cash), ptr(6990000.0), 0)
	closeNumber(t, "K0", ptr(r.State.K0), ptr(999840000.0), 0)
	closeNumber(t, "reserves", ptr(r.State.Reserves), ptr(50150000.0), 0)
	closeNumber(t, "RWA", ptr(r.State.RWA10), ptr(8002850000.0), 0)
	// No second liquidity charge for reserve; no second capital-allocation charge.
	for _, term := range []int{12, 13, 24, 25} {
		terms := f.Request.Terms
		terms.TermMonths = term
		r = project(t, projector, f.Initial, terms)
		if term <= 12 && r.State.Krd != f.Initial.Krd || term > 12 && r.State.Krd <= f.Initial.Krd {
			t.Fatalf("long-term exposure at %d", term)
		}
	}
	p.Economics.QualityCategory = 3
	projector, err = NewProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	r = project(t, projector, f.Initial, f.Request.Terms)
	if !findCheck(t, r.Checks, "QUALITY").Passed || !r.Issued {
		t.Fatal("quality equality failed")
	}
	p.Economics.QualityCategory = 4
	projector, err = NewProjector(p)
	if err != nil {
		t.Fatal(err)
	}
	r = project(t, projector, f.Initial, f.Request.Terms)
	if findCheck(t, r.Checks, "QUALITY").Passed || r.Issued {
		t.Fatal("poor quality issued")
	}
	b := initial(t)
	b.CashMin = 7000000
	p = policy(t)
	projector, _ = NewProjector(p)
	r = project(t, projector, b, f.Request.Terms)
	if !findCheck(t, r.Checks, "CASH_MIN").Passed || !r.Issued {
		t.Fatal("cash equality failed")
	}
	b.CashMin = math.Nextafter(7000000, math.Inf(1))
	r = project(t, projector, b, f.Request.Terms)
	if findCheck(t, r.Checks, "CASH_MIN").Passed || r.Issued {
		t.Fatal("cash minimum ignored")
	}
}

func TestInvalidProjectedStatesAreBankProblems(t *testing.T) {
	f := deal(t)
	for _, c := range []struct {
		name      string
		change    func(*contracts.BankState)
		nullCodes []string
	}{
		{"zero capital and long-term base", func(b *contracts.BankState) { b.K0 = 150000; b.Funding4 = 0 }, []string{"N4", "N6", "N7", "N25", "N12"}},
		{"negative capital", func(b *contracts.BankState) { b.K0 = 100000 }, []string{"N6", "N7", "N25", "N12"}},
		{"negative basic capital", func(b *contracts.BankState) { b.K1 = 100000 }, nil},
		{"negative liquid assets", func(b *contracts.BankState) { b.Cash = 1000000; b.Lam = 1000000; b.Lat = 1000000 }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := initial(t)
			c.change(&b)
			s := service(t, policy(t), b, nil)
			r, err := s.Check(context.Background(), f.Request)
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict != contracts.BankVerdictBankReview || findCheck(t, r.Checks, "STATE_VALID").Passed {
				t.Fatal(r)
			}
			for _, code := range c.nullCodes {
				c := findCheck(t, r.Checks, code)
				if c.After != nil || c.Passed {
					t.Errorf("%s should be failed and null", code)
				}
			}
			compareState(t, s.Snapshot().State, b)
			if _, err = json.Marshal(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	b := initial(t)
	b.K1 = 1e308
	b.Ka = 1e308
	b.K0 = 1e308
	b.RWA11 = 1e308
	b.RWA12 = 1e308
	b.RWA10 = 1e308
	b.Cash = 1e20
	b.Lam = 1e20
	b.Lat = 1e20
	p := policy(t)
	p.Economics.ReserveRate = 0
	p.Economics.RiskWeight = 1e290
	p.Economics.CapitalAllocation = 0
	s := service(t, p, b, nil)
	r := f.Request
	r.Terms.AmountRUB = 1000000000000000000
	r.Terms.TermMonths = 1
	r.RequestedTerms.AmountRUB = ptr(r.Terms.AmountRUB)
	r.RequestedTerms.MinAcceptableAmountRUB = ptr(r.Terms.AmountRUB)
	response, err := s.Check(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if response.Verdict != contracts.BankVerdictBankReview || findCheck(t, response.Checks, "N1_0").After != nil {
		t.Fatal("nonfinite projected base was accepted")
	}
	if _, err = json.Marshal(response); err != nil {
		t.Fatal(err)
	}
	compareState(t, s.Snapshot().State, b)
}
