package bank

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

type Error struct {
	Status int
	Code   string
	Fields []string
}

func (e *Error) Error() string { return fmt.Sprintf("bank %s: %v", e.Code, e.Fields) }
func envelope(policy, application string, b contracts.Borrower) error {
	var fields []string
	if strings.TrimSpace(policy) == "" {
		fields = append(fields, "policy_id")
	}
	if strings.TrimSpace(application) == "" {
		fields = append(fields, "application_id")
	}
	if strings.TrimSpace(b.ID) == "" {
		fields = append(fields, "borrower.id")
	}
	if b.Type == "" {
		fields = append(fields, "borrower.type")
	}
	if len(fields) > 0 {
		return &Error{Status: 400, Code: "MISSING_ENVELOPE", Fields: fields}
	}
	return nil
}
func validatePayload(b contracts.Borrower, c contracts.CreditRequest, score contracts.ScoreResult) error {
	invalid := func(field string) error { return &Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{field}} }
	if b.Type != contracts.BorrowerTypeSoleProprietor && b.Type != contracts.BorrowerTypeLegalEntity {
		return invalid("borrower.type")
	}
	if b.INN == nil || strings.TrimSpace(*b.INN) == "" || b.Name == nil || strings.TrimSpace(*b.Name) == "" {
		return invalid("borrower")
	}
	if c.Purpose == nil || strings.TrimSpace(*c.Purpose) == "" || c.AmountRUB == nil || c.TermMonths == nil || c.MaxAnnualRate == nil || c.MinAcceptableAmountRUB == nil || c.MaxAcceptableTermMonths == nil {
		return invalid("requested_terms")
	}
	if *c.AmountRUB <= 0 || *c.MinAcceptableAmountRUB <= 0 || *c.MinAcceptableAmountRUB > *c.AmountRUB || *c.TermMonths < 1 || *c.MaxAcceptableTermMonths < *c.TermMonths {
		return invalid("requested_terms")
	}
	if _, err := economics.BasisPointsFromPercent(*c.MaxAnnualRate); err != nil {
		return invalid("requested_terms.max_annual_rate")
	}
	if score.Model == "" || score.Total < 0 || score.Total > 1000 || !finite(score.RawTotal) || score.RawTotal < 0 || score.RawTotal > 1000 {
		return invalid("score")
	}
	switch score.Grade {
	case contracts.GradeA, contracts.GradeB, contracts.GradeC, contracts.GradeD:
	default:
		return invalid("score.grade")
	}
	return nil
}
func validateTerms(t contracts.LoanTerms) error {
	if t.AmountRUB <= 0 || t.TermMonths < 1 || t.TermMonths > 36 {
		return &Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{"terms"}}
	}
	if _, err := economics.BasisPointsFromPercent(t.AnnualRate); err != nil {
		return &Error{Status: 422, Code: "INVALID_VALUE", Fields: []string{"terms.annual_rate"}}
	}
	return nil
}

// Deep copies prevent callers and notification adapters from mutating replay
// payloads, nested criteria, event fields or previous responses.
func copyJSON[T any](v T) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}
