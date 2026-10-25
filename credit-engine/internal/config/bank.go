package config

import (
	"fmt"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func LoadBankState(path string) (contracts.BankState, error) {
	var state contracts.BankState
	if err := loadJSON(path, &state); err != nil {
		return state, err
	}
	if err := ValidateBankState(state); err != nil {
		return state, fmt.Errorf("bank state %q: %w", path, err)
	}
	return state, nil
}

// Existing norm breaches and an issuance block are business conditions, not
// malformed startup data. Ratios and issuance decisions belong to internal/bank.
func ValidateBankState(s contracts.BankState) error {
	for _, item := range []struct {
		name  string
		value float64
	}{
		{"K1", s.K1}, {"Ka", s.Ka}, {"K0", s.K0}, {"RWA11", s.RWA11}, {"RWA12", s.RWA12}, {"RWA10", s.RWA10},
		{"Lam", s.Lam}, {"Lat", s.Lat}, {"D2", s.D2}, {"D3", s.D3}, {"Krd", s.Krd}, {"Funding4", s.Funding4},
		{"Invest", s.Invest}, {"Cash", s.Cash}, {"CashMin", s.CashMin}, {"Reserves", s.Reserves},
	} {
		if !finite(item.value) || item.value < 0 {
			return fmt.Errorf("%s must be finite and nonnegative", item.name)
		}
	}
	if s.K0 <= 0 || s.RWA11 <= 0 || s.RWA12 <= 0 || s.RWA10 <= 0 || s.D2 <= 0 || s.D3 <= 0 || !finite(s.K0+s.Funding4) {
		return fmt.Errorf("capital and norm denominator bases must be positive and finite")
	}
	if s.Groups == nil || s.RelatedGroups == nil || s.BorrowerGroups == nil || s.IssuedApplicationIds == nil {
		return fmt.Errorf("group registries and issued_application_ids must not be null")
	}
	for group, risk := range s.Groups {
		if strings.TrimSpace(group) == "" || !finite(risk) || risk < 0 {
			return fmt.Errorf("groups must have nonempty IDs and finite nonnegative risks")
		}
	}
	seenGroups := make(map[string]bool)
	for _, group := range s.RelatedGroups {
		if _, ok := s.Groups[group]; !ok || seenGroups[group] {
			return fmt.Errorf("related_groups must reference distinct existing groups")
		}
		seenGroups[group] = true
	}
	for borrower, group := range s.BorrowerGroups {
		if _, ok := s.Groups[group]; !ok || strings.TrimSpace(borrower) == "" {
			return fmt.Errorf("borrower_groups must map nonempty borrowers to existing groups")
		}
	}
	seenIDs := make(map[string]bool)
	for _, id := range s.IssuedApplicationIds {
		if strings.TrimSpace(id) == "" || seenIDs[id] {
			return fmt.Errorf("issued_application_ids must contain distinct nonempty IDs")
		}
		seenIDs[id] = true
	}
	return nil
}
