package events

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
)

func nonempty(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsRune(value, 0)
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// Validate follows MS2's existing fields/rate units and documented event shapes.
// It deliberately does not reconstruct the model or recalculate the decision.
func Validate(e contracts.AdminEvent) error {
	bad := func(path string) error { return invalid(422, "INVALID_EVENT_VALUE", path) }
	if path, ok := validStrings(reflect.ValueOf(e), ""); !ok {
		return bad(path)
	}
	if !nonempty(e.EventID) || !nonempty(e.ApplicationID) {
		return bad("/event_id")
	}
	switch e.Kind {
	case contracts.EventTypeLoanIssued, contracts.EventTypeBorrowerReview, contracts.EventTypeBankReview:
	default:
		return bad("/kind")
	}
	if e.EventID != e.ApplicationID+":"+string(e.Kind) {
		return bad("/event_id")
	}
	occurred, err := time.Parse(time.RFC3339, e.OccurredAt)
	if err != nil {
		return bad("/occurred_at")
	}
	_, offset := occurred.Zone()
	if offset != 0 {
		return bad("/occurred_at")
	}
	if !nonempty(e.Borrower.ID) || e.Borrower.INN == nil || !nonempty(*e.Borrower.INN) || e.Borrower.Name == nil || !nonempty(*e.Borrower.Name) {
		return bad("/borrower")
	}
	if e.Borrower.Type != contracts.BorrowerTypeSoleProprietor && e.Borrower.Type != contracts.BorrowerTypeLegalEntity {
		return bad("/borrower/type")
	}
	c := e.RequestedTerms
	if c.Purpose == nil || !nonempty(*c.Purpose) || c.AmountRUB == nil || c.TermMonths == nil || c.MaxAnnualRate == nil || c.MinAcceptableAmountRUB == nil || c.MaxAcceptableTermMonths == nil {
		return bad("/requested_terms")
	}
	if *c.AmountRUB <= 0 || *c.MinAcceptableAmountRUB <= 0 || *c.MinAcceptableAmountRUB > *c.AmountRUB || *c.TermMonths < 1 || *c.MaxAcceptableTermMonths < *c.TermMonths {
		return bad("/requested_terms")
	}
	if _, err := economics.BasisPointsFromPercent(*c.MaxAnnualRate); err != nil {
		return bad("/requested_terms/max_annual_rate")
	}
	if !nonempty(e.Score.Model) || e.Score.Total < 0 || e.Score.Total > 1000 || !finite(e.Score.RawTotal) || e.Score.RawTotal < 0 || e.Score.RawTotal > 1000 {
		return bad("/score")
	}
	switch e.Score.Grade {
	case contracts.GradeA, contracts.GradeB, contracts.GradeC, contracts.GradeD:
	default:
		return bad("/score/grade")
	}
	for _, criterion := range e.Score.Economics.Criteria {
		if err := criterion.ValidateValue(); err != nil {
			return bad("/score/economics/criteria")
		}
	}
	if e.Terms != nil {
		if e.Terms.AmountRUB <= 0 || e.Terms.TermMonths < 1 || e.Terms.TermMonths > 36 {
			return bad("/terms")
		}
		if _, err := economics.BasisPointsFromPercent(e.Terms.AnnualRate); err != nil {
			return bad("/terms/annual_rate")
		}
	}
	for _, reason := range e.Reasons {
		if !nonempty(reason.Code) || !nonempty(reason.Message) {
			return bad("/reasons")
		}
	}
	switch e.Kind {
	case contracts.EventTypeLoanIssued:
		if e.LoanID == nil || !nonempty(*e.LoanID) || e.Terms == nil || e.Selection == nil {
			return bad("/loan_id")
		}
	case contracts.EventTypeBankReview:
		if e.LoanID != nil || e.Terms == nil || e.Selection == nil || len(e.Problems) == 0 {
			return bad("/problems")
		}
	case contracts.EventTypeBorrowerReview:
		if e.LoanID != nil || e.Terms != nil || e.Selection != nil || len(e.Reasons) == 0 {
			return bad("/reasons")
		}
	}
	return nil
}

// PostgreSQL's checked JSON representation cannot store U+0000. Reject invalid
// text before a storage error, including text inside nested criterion details.
func validStrings(value reflect.Value, path string) (string, bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", true
		}
		return validStrings(value.Elem(), path)
	}
	switch value.Kind() {
	case reflect.String:
		if !utf8.ValidString(value.String()) || strings.ContainsRune(value.String(), 0) {
			return path, false
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			if invalid, ok := validStrings(value.Field(i), pointer(path, name)); !ok {
				return invalid, false
			}
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if invalid, ok := validStrings(value.Index(i), fmt.Sprint(path, "/", i)); !ok {
				return invalid, false
			}
		}
	}
	return "", true
}
