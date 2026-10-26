// Package config loads and validates startup settings and financial configuration.
package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type LookupEnv func(string) (string, bool)

type Scoring struct {
	ModelsDatabaseURL string `json:"-"`
	Addr              string
	BankURL           string
	StatementsURL     string
	FactsURL          string
	PolicyFile        string
	BankTimeout       time.Duration
	StatementsTimeout time.Duration
	FactsTimeout      time.Duration
	WriteTimeout      time.Duration
	EvaluationDate    *time.Time
}

type Bank struct {
	Addr             string
	AdminURL         string
	AdminEventsToken string `json:"-"`
	PolicyFile       string
	StateFile        string
	AdminTimeout     time.Duration
	WriteTimeout     time.Duration
}

func LoadScoring(env LookupEnv) (Scoring, error) {
	cfg := Scoring{
		ModelsDatabaseURL: value(env, "SCORING_MODELS_DATABASE_URL", ""),
		Addr:              value(env, "SCORING_ADDR", ":8082"),
		BankURL:           value(env, "BANK_URL", "http://localhost:8083"),
		StatementsURL:     value(env, "STATEMENTS_URL", ""),
		FactsURL:          value(env, "FACTS_URL", ""),
		PolicyFile:        value(env, "POLICY_FILE", "config/policy.json"),
	}
	var err error
	if cfg.BankTimeout, err = duration(env, "BANK_TIMEOUT", 4*time.Second); err != nil {
		return cfg, err
	}
	if cfg.StatementsTimeout, err = duration(env, "STATEMENTS_TIMEOUT", 2*time.Second); err != nil {
		return cfg, err
	}
	if cfg.FactsTimeout, err = duration(env, "FACTS_TIMEOUT", 2*time.Second); err != nil {
		return cfg, err
	}
	if cfg.WriteTimeout, err = duration(env, "HTTP_WRITE_TIMEOUT", 15*time.Second); err != nil {
		return cfg, err
	}
	if raw, ok := env("DEMO_EVALUATION_DATE"); ok {
		date, parseErr := time.Parse(time.DateOnly, raw)
		if parseErr != nil {
			return cfg, fmt.Errorf("DEMO_EVALUATION_DATE must use YYYY-MM-DD: %w", parseErr)
		}
		cfg.EvaluationDate = &date
	}
	return cfg, cfg.Validate()
}

func LoadBank(env LookupEnv) (Bank, error) {
	cfg := Bank{
		Addr:             value(env, "BANK_ADDR", ":8083"),
		AdminURL:         value(env, "ADMIN_URL", ""),
		AdminEventsToken: value(env, "ADMIN_EVENTS_TOKEN", ""),
		PolicyFile:       value(env, "POLICY_FILE", "config/policy.json"),
		StateFile:        value(env, "BANK_STATE_FILE", "config/bank.default.json"),
	}
	var err error
	if cfg.AdminTimeout, err = duration(env, "ADMIN_TIMEOUT", 2*time.Second); err != nil {
		return cfg, err
	}
	if cfg.WriteTimeout, err = duration(env, "HTTP_WRITE_TIMEOUT", 15*time.Second); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c Scoring) Validate() error {
	if err := address("SCORING_ADDR", c.Addr); err != nil {
		return err
	}
	if err := baseURL("BANK_URL", c.BankURL); err != nil {
		return err
	}
	if err := baseURL("STATEMENTS_URL", c.StatementsURL); err != nil {
		return err
	}
	if c.FactsURL != "" {
		if err := baseURL("FACTS_URL", c.FactsURL); err != nil {
			return err
		}
		if c.FactsTimeout <= 0 {
			return fmt.Errorf("FACTS_TIMEOUT must be positive")
		}
	}
	if strings.TrimSpace(c.PolicyFile) == "" {
		return fmt.Errorf("POLICY_FILE must not be empty")
	}
	if c.BankTimeout <= 0 || c.StatementsTimeout <= 0 {
		return fmt.Errorf("outgoing HTTP timeouts must be positive")
	}
	if c.WriteTimeout <= c.BankTimeout || c.WriteTimeout-c.BankTimeout <= c.StatementsTimeout {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT must exceed STATEMENTS_TIMEOUT + BANK_TIMEOUT")
	}
	if c.FactsURL != "" && c.WriteTimeout-c.BankTimeout-c.StatementsTimeout <= c.FactsTimeout {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT must exceed FACTS_TIMEOUT + STATEMENTS_TIMEOUT + BANK_TIMEOUT")
	}
	return nil
}

func (c Bank) Validate() error {
	if c.AdminEventsToken != "" && !ValidServiceToken(c.AdminEventsToken) {
		return fmt.Errorf("ADMIN_EVENTS_TOKEN must contain 32..256 visible ASCII characters without spaces")
	}
	if err := address("BANK_ADDR", c.Addr); err != nil {
		return err
	}
	if err := baseURL("ADMIN_URL", c.AdminURL); err != nil {
		return err
	}
	if strings.TrimSpace(c.PolicyFile) == "" || strings.TrimSpace(c.StateFile) == "" {
		return fmt.Errorf("POLICY_FILE and BANK_STATE_FILE must not be empty")
	}
	if c.AdminTimeout <= 0 {
		return fmt.Errorf("ADMIN_TIMEOUT must be positive")
	}
	if c.WriteTimeout <= c.AdminTimeout {
		return fmt.Errorf("HTTP_WRITE_TIMEOUT must exceed ADMIN_TIMEOUT")
	}
	return nil
}

func value(env LookupEnv, key, fallback string) string {
	if v, ok := env(key); ok {
		return v
	}
	return fallback
}

func duration(env LookupEnv, key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := env(key)
	if !ok {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration (for example 2s)", key)
	}
	return d, nil
}

func address(key, raw string) error {
	_, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s must use host:port", key)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s must contain a port from 1 to 65535", key)
	}
	return nil
}

func baseURL(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Opaque != "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) base URL without credentials, query or fragment", key)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%s has an invalid port", key)
		}
	}
	return nil
}
