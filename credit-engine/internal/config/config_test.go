package config_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func lookup(values map[string]string) config.LookupEnv {
	return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
}

func TestScoringSettings(t *testing.T) {
	cfg, err := config.LoadScoring(lookup(map[string]string{"STATEMENTS_URL": "http://statements:8080"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8082" || cfg.BankURL != "http://localhost:8083" || cfg.PolicyFile != "config/policy.json" || cfg.EvaluationDate != nil {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.BankTimeout != 4*time.Second || cfg.StatementsTimeout != 2*time.Second || cfg.WriteTimeout != 15*time.Second {
		t.Fatalf("unexpected timeout budgets: %+v", cfg)
	}
	cfg, err = config.LoadScoring(lookup(map[string]string{
		"SCORING_ADDR": "127.0.0.1:9002", "BANK_URL": "https://bank.example/base", "STATEMENTS_URL": "https://statements.example",
		"POLICY_FILE": "custom-policy.json", "BANK_TIMEOUT": "5s", "STATEMENTS_TIMEOUT": "3s", "HTTP_WRITE_TIMEOUT": "20s", "DEMO_EVALUATION_DATE": "2026-10-23",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9002" || cfg.PolicyFile != "custom-policy.json" || cfg.BankTimeout != 5*time.Second || cfg.StatementsTimeout != 3*time.Second || cfg.WriteTimeout != 20*time.Second || cfg.EvaluationDate.Format(time.DateOnly) != "2026-10-23" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestBankSettings(t *testing.T) {
	cfg, err := config.LoadBank(lookup(map[string]string{"ADMIN_URL": "http://admin:8080"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8083" || cfg.StateFile != "config/bank.default.json" || cfg.PolicyFile != "config/policy.json" || cfg.AdminTimeout != 2*time.Second || cfg.WriteTimeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	cfg, err = config.LoadBank(lookup(map[string]string{"ADMIN_URL": "https://admin.example", "BANK_ADDR": "[::1]:9003", "BANK_STATE_FILE": "state.json", "POLICY_FILE": "policy.json", "ADMIN_TIMEOUT": "1s", "HTTP_WRITE_TIMEOUT": "10s"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "[::1]:9003" || cfg.StateFile != "state.json" || cfg.PolicyFile != "policy.json" || cfg.AdminTimeout != time.Second || cfg.WriteTimeout != 10*time.Second {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"empty statements URL", "STATEMENTS_URL", ""}, {"relative URL", "BANK_URL", "/bank"},
		{"unsupported scheme", "STATEMENTS_URL", "ftp://statements"}, {"URL credentials", "BANK_URL", "http://user:secret@bank"},
		{"URL query", "BANK_URL", "http://bank?token=secret"}, {"URL fragment", "BANK_URL", "http://bank/#fragment"},
		{"URL port", "BANK_URL", "http://bank:65536"}, {"missing port", "SCORING_ADDR", "localhost"},
		{"zero port", "SCORING_ADDR", ":0"}, {"oversized port", "SCORING_ADDR", ":65536"},
		{"empty policy", "POLICY_FILE", ""}, {"zero timeout", "BANK_TIMEOUT", "0s"},
		{"negative timeout", "STATEMENTS_TIMEOUT", "-1s"}, {"bad duration", "BANK_TIMEOUT", "two seconds"},
		{"insufficient response budget", "HTTP_WRITE_TIMEOUT", "6s"}, {"bad date", "DEMO_EVALUATION_DATE", "2026-02-30"},
		{"wrong date format", "DEMO_EVALUATION_DATE", "23.10.2026"}, {"empty date", "DEMO_EVALUATION_DATE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.LoadScoring(lookup(map[string]string{"STATEMENTS_URL": "http://statements", tc.key: tc.value}))
			if err == nil {
				t.Fatal("expected startup error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error exposes URL credentials/query")
			}
		})
	}
	for _, values := range []map[string]string{
		{}, {"ADMIN_URL": "http://admin", "ADMIN_TIMEOUT": "0s"}, {"ADMIN_URL": "http://admin", "HTTP_WRITE_TIMEOUT": "2s"},
		{"ADMIN_URL": "http://admin", "BANK_STATE_FILE": ""}, {"ADMIN_URL": "http://admin", "BANK_ADDR": "bad"},
	} {
		if _, err := config.LoadBank(lookup(values)); err == nil {
			t.Fatalf("expected error for %v", values)
		}
	}
}

func writeConfig(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func changedConfig(t *testing.T, source string, change func(map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return writeConfig(t, data)
}

func TestLoadPolicy(t *testing.T) {
	p, err := config.LoadPolicy("../../config/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "credit-mvp-v2" || p.Optimizer.RateStep != 0.0025 || p.Economics.FTPAnnual != 0.15 || len(p.NormLimits) != 10 {
		t.Fatalf("policy changed: %+v", p)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing id", func(d map[string]any) { delete(d, "id") }},
		{"null economics", func(d map[string]any) { d["economics"] = nil }},
		{"missing zero cost", func(d map[string]any) { delete(d["economics"].(map[string]any), "upfront_cost_rub") }},
		{"invalid probability", func(d map[string]any) { d["economics"].(map[string]any)["pd_annual"] = 1.01 }},
		{"negative cost", func(d map[string]any) { d["economics"].(map[string]any)["upfront_cost_rub"] = -1 }},
		{"invalid category", func(d map[string]any) { d["economics"].(map[string]any)["quality_category"] = 0 }},
		{"invalid max category", func(d map[string]any) { d["max_quality_category"] = 6 }},
		{"zero amount step", func(d map[string]any) { d["optimizer"].(map[string]any)["amount_step_rub"] = 0 }},
		{"fractional basis point", func(d map[string]any) { d["optimizer"].(map[string]any)["rate_step"] = 0.00015 }},
		{"invalid horizon", func(d map[string]any) { d["optimizer"].(map[string]any)["max_term_months"] = 37 }},
		{"invalid profit retention", func(d map[string]any) { d["optimizer"].(map[string]any)["profit_retention"] = 0 }},
		{"invalid dsr", func(d map[string]any) { d["optimizer"].(map[string]any)["dsr_cap"] = 1.01 }},
		{"invalid threshold", func(d map[string]any) { d["large_exposure_threshold"] = 0 }},
		{"missing norm", func(d map[string]any) { delete(d["norm_limits"].(map[string]any), "N6") }},
		{"missing norm limit", func(d map[string]any) { delete(d["norm_limits"].(map[string]any)["N6"].(map[string]any), "value") }},
		{"null norm", func(d map[string]any) { d["norm_limits"].(map[string]any)["N6"] = nil }},
		{"wrong operator", func(d map[string]any) { d["norm_limits"].(map[string]any)["N6"].(map[string]any)["operator"] = ">=" }},
		{"unknown field", func(d map[string]any) { d["typo"] = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := changedConfig(t, "../../config/policy.json", tc.change)
			if _, err := config.LoadPolicy(path); err == nil {
				t.Fatal("expected invalid policy")
			}
		})
	}
	p.Economics.PDAnnual = math.NaN()
	if p.Validate() == nil {
		t.Fatal("NaN must fail validation")
	}
	for _, raw := range []string{"null", "{}", "[]", "{", "{} {}"} {
		if _, err := config.LoadPolicy(writeConfig(t, []byte(raw))); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if _, err := config.LoadPolicy(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file must fail")
	}
}

func TestLoadBankStates(t *testing.T) {
	for _, name := range []string{"bank.default.json", "bank.capital_boundary.json", "bank.liquidity_boundary.json"} {
		if _, err := config.LoadBankState(filepath.Join("../../config", name)); err != nil {
			t.Fatal(err)
		}
	}
	path := changedConfig(t, "../../config/bank.default.json", func(d map[string]any) {
		d["issuance_enabled"] = false
		d["Cash"] = 0
		d["K1"] = 0
		d["Lam"] = 0
	})
	state, err := config.LoadBankState(path)
	if err != nil {
		t.Fatalf("business breaches must not block startup: %v", err)
	}
	if state.IssuanceEnabled {
		t.Fatal("explicit false was lost")
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing cash", func(d map[string]any) { delete(d, "Cash") }},
		{"missing issuance flag", func(d map[string]any) { delete(d, "issuance_enabled") }},
		{"null capital", func(d map[string]any) { d["K0"] = nil }},
		{"zero capital base", func(d map[string]any) { d["K0"] = 0 }},
		{"negative assets", func(d map[string]any) { d["Lam"] = -1 }},
		{"zero denominator", func(d map[string]any) { d["RWA10"] = 0 }},
		{"null groups", func(d map[string]any) { d["groups"] = nil }},
		{"null group risk", func(d map[string]any) { d["groups"].(map[string]any)["g1"] = nil }},
		{"negative risk", func(d map[string]any) { d["groups"].(map[string]any)["g1"] = -1 }},
		{"null related group", func(d map[string]any) { d["related_groups"] = []any{nil} }},
		{"unknown related group", func(d map[string]any) { d["related_groups"] = []string{"missing"} }},
		{"duplicate related group", func(d map[string]any) { d["related_groups"] = []string{"related", "related"} }},
		{"unknown borrower group", func(d map[string]any) { d["borrower_groups"] = map[string]string{"borrower": "missing"} }},
		{"duplicate issuance", func(d map[string]any) { d["issued_application_ids"] = []string{"same", "same"} }},
		{"empty issuance", func(d map[string]any) { d["issued_application_ids"] = []string{""} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := changedConfig(t, "../../config/bank.default.json", tc.change)
			if _, err := config.LoadBankState(path); err == nil {
				t.Fatal("expected invalid bank state")
			}
		})
	}
	state.K0 = math.Inf(1)
	if config.ValidateBankState(state) == nil {
		t.Fatal("infinite capital must fail validation")
	}
}
