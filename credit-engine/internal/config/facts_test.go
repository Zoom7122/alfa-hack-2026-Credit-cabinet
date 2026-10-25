package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func TestFactsSettingsOptionalURLAndResponseBudget(t *testing.T) {
	base := map[string]string{"STATEMENTS_URL": "http://statements", "HTTP_WRITE_TIMEOUT": "7s"}
	cfg, err := config.LoadScoring(lookup(base))
	if err != nil || cfg.FactsURL != "" || cfg.FactsTimeout != 2*time.Second {
		t.Fatal(cfg, err)
	}
	base["FACTS_URL"] = "https://facts.example/base"
	if _, err := config.LoadScoring(lookup(base)); err == nil {
		t.Fatal("insufficient facts budget accepted")
	}
	base["HTTP_WRITE_TIMEOUT"] = "9s"
	cfg, err = config.LoadScoring(lookup(base))
	if err != nil || cfg.FactsURL != base["FACTS_URL"] {
		t.Fatal(cfg, err)
	}
	base["FACTS_TIMEOUT"] = "3s"
	if _, err := config.LoadScoring(lookup(base)); err == nil {
		t.Fatal("equal combined budget accepted")
	}
	base["HTTP_WRITE_TIMEOUT"] = "10s"
	cfg, err = config.LoadScoring(lookup(base))
	if err != nil || cfg.FactsTimeout != 3*time.Second {
		t.Fatal(cfg, err)
	}
	for _, test := range []struct{ key, value string }{{"FACTS_URL", "http://user:secret@facts"}, {"FACTS_URL", "http://facts?secret=1"}, {"FACTS_URL", "http://facts?"}, {"FACTS_URL", "/relative"}, {"FACTS_URL", "http://facts:65536"}, {"FACTS_URL", " "}, {"FACTS_TIMEOUT", "0s"}, {"FACTS_TIMEOUT", "-1s"}, {"FACTS_TIMEOUT", "bad"}} {
		values := map[string]string{"STATEMENTS_URL": "http://statements", test.key: test.value}
		_, err := config.LoadScoring(lookup(values))
		if err == nil {
			t.Fatal("invalid facts setting accepted", test.key)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("setting error leaked secret")
		}
	}
}
