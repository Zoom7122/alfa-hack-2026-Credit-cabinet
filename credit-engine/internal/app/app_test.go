package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func environment(values map[string]string) config.LookupEnv {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

func TestScoringDependencies(t *testing.T) {
	cfg, err := config.LoadScoring(environment(map[string]string{"STATEMENTS_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json"}))
	if err != nil {
		t.Fatal(err)
	}
	fixed := clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	a, err := app.NewScoring(cfg, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if a.Optimizer == nil {
		t.Fatal("scoring startup must initialize offer selection")
	}
	if a.Clock.Now() != fixed.Now() || a.BankHTTP.Timeout != 4*time.Second || a.StatementsHTTP.Timeout != 2*time.Second || a.Server.WriteTimeout != 15*time.Second || a.Server.Addr != ":8082" {
		t.Fatalf("incorrect scoring dependencies: %+v", a)
	}
	date := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	cfg.EvaluationDate = &date
	a, err = app.NewScoring(cfg, fixed)
	if err != nil {
		t.Fatal(err)
	}
	if a.Clock.Now() != date {
		t.Fatal("demo date must control evaluation clock")
	}
	cfg.EvaluationDate = nil
	a, err = app.NewScoring(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Clock.(clock.System); !ok {
		t.Fatal("default clock must use real UTC time")
	}
}

func TestBankDependenciesAndIndependentSnapshots(t *testing.T) {
	cfg, err := config.LoadBank(environment(map[string]string{"ADMIN_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json", "BANK_STATE_FILE": "../../config/bank.default.json"}))
	if err != nil {
		t.Fatal(err)
	}
	fixed := clock.Fixed{Time: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	a, err := app.NewBank(cfg, fixed)
	if err != nil {
		t.Fatal(err)
	}
	b, err := app.NewBank(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Operations == nil || b.Operations == nil {
		t.Fatal("bank operations must be initialized")
	}
	a.InitialState.Groups["g1"] = 1
	if a.Operations.Snapshot().State.Groups["g1"] != 200000000 {
		t.Fatal("public initial snapshot aliases bank operations")
	}
	if b.InitialState.Groups["g1"] != 200000000 || b.InitialState.Cash != 10000000 {
		t.Fatal("startup snapshots must be independently loaded")
	}
	if a.Clock.Now() != fixed.Now() || a.AdminHTTP.Timeout != 2*time.Second || a.Server.WriteTimeout != 15*time.Second || a.Server.Addr != ":8083" {
		t.Fatalf("incorrect bank dependencies: %+v", a)
	}
	if _, ok := b.Clock.(clock.System); !ok {
		t.Fatal("default bank clock must use real UTC time")
	}
}

func TestStartupRejectsInvalidDependencies(t *testing.T) {
	scoring, err := config.LoadScoring(environment(map[string]string{"STATEMENTS_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json"}))
	if err != nil {
		t.Fatal(err)
	}
	bank, err := config.LoadBank(environment(map[string]string{"ADMIN_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json", "BANK_STATE_FILE": "../../config/bank.default.json"}))
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	scoring.PolicyFile = missing
	if _, err := app.NewScoring(scoring, nil); err == nil {
		t.Fatal("missing policy must block scoring startup")
	}
	bank.StateFile = missing
	if _, err := app.NewBank(bank, nil); err == nil {
		t.Fatal("missing state must block bank startup")
	}
	bank.PolicyFile = missing
	if _, err := app.NewBank(bank, nil); err == nil {
		t.Fatal("missing policy must block bank startup")
	}
	scoring.BankTimeout = 0
	if _, err := app.NewScoring(scoring, nil); err == nil {
		t.Fatal("invalid settings must block startup")
	}
}

func TestServicesUseTheSameEconomicsWithoutBankState(t *testing.T) {
	scoringConfig, err := config.LoadScoring(environment(map[string]string{
		"STATEMENTS_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	bankConfig, err := config.LoadBank(environment(map[string]string{
		"ADMIN_URL": "http://unused.invalid", "POLICY_FILE": "../../config/policy.json",
		"BANK_STATE_FILE": "../../config/bank.capital_boundary.json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	scoring, err := app.NewScoring(scoringConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	bank, err := app.NewBank(bankConfig, nil)
	if err != nil {
		t.Fatal(err)
	}
	terms := contracts.LoanTerms{AmountRUB: 3000000, TermMonths: 24, AnnualRate: 24}
	scoringResult, err := scoring.Economics.Evaluate(terms)
	if err != nil {
		t.Fatal(err)
	}
	bankResult, err := bank.Economics.Evaluate(terms)
	if err != nil {
		t.Fatal(err)
	}
	if scoringResult.NPVKopecks != 11894554 || scoringResult.NPVKopecks != bankResult.NPVKopecks || scoringResult.Schedule.MonthlyPaymentRUB != bankResult.Schedule.MonthlyPaymentRUB {
		t.Fatal("MS1 and MS2 must use identical economics regardless of the bank snapshot")
	}
	if bank.InitialState.Cash != 10000000 || len(bank.InitialState.IssuedApplicationIds) != 0 {
		t.Fatal("economic evaluation must not issue a loan or change bank state")
	}
}
