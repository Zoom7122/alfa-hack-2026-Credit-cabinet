package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

type ScoringFactsBorrower struct {
	ID   string       `json:"id"`
	Type BorrowerType `json:"type"`
	INN  *string      `json:"inn"`
}
type ScoringFactsRequest struct {
	ApplicationID      string               `json:"application_id"`
	Borrower           ScoringFactsBorrower `json:"borrower"`
	ModelVersion       string               `json:"model_version"`
	FactsSchemaVersion string               `json:"facts_schema_version"`
}
type ScoringFactsMetadata struct {
	ApplicationID      string       `json:"application_id"`
	BorrowerID         string       `json:"borrower_id"`
	BorrowerType       BorrowerType `json:"borrower_type"`
	ModelVersion       string       `json:"model_version"`
	FactsSchemaVersion string       `json:"facts_schema_version"`
	FactsDate          string       `json:"facts_date"`
}
type ScoringFactsExpectation struct {
	Request ScoringFactsRequest
	Date    time.Time
}

// ScoringFactsError carries a protocol code and schema paths, never the body
// or source values. The REST client maps protocol failures to HTTP 502.
type ScoringFactsError struct {
	Code   string
	Fields []string
	Cause  error
}

func (e *ScoringFactsError) Error() string { return fmt.Sprintf("scoring facts: %s", e.Code) }
func (e *ScoringFactsError) Unwrap() error { return e.Cause }
func factsError(code string, fields ...string) error {
	return &ScoringFactsError{Code: code, Fields: append([]string{}, fields...)}
}
func factsJSONError(err error) error {
	var format *jsonstrict.Error
	fields := []string{}
	if errors.As(err, &format) {
		path := strings.TrimPrefix(format.Path, "/")
		path = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(path, "/", "."), "~1", "/"), "~0", "~")
		if path != "" {
			fields = append(fields, path)
		}
	}
	return &ScoringFactsError{Code: "INVALID_FACTS_RESPONSE", Fields: fields, Cause: err}
}

func decodeFactsMetadata(object map[string]json.RawMessage, expected *ScoringFactsExpectation, version, schema string, borrower BorrowerType) (ScoringFactsMetadata, error) {
	var result ScoringFactsMetadata
	for _, field := range []struct {
		name   string
		target *string
	}{{"application_id", &result.ApplicationID}, {"borrower_id", &result.BorrowerID}, {"model_version", &result.ModelVersion}, {"facts_schema_version", &result.FactsSchemaVersion}, {"facts_date", &result.FactsDate}} {
		value, err := jsonstrict.String(object[field.name], "/"+field.name)
		if err != nil {
			return result, factsJSONError(err)
		}
		if strings.TrimSpace(value) == "" {
			return result, factsError("INVALID_FACTS_RESPONSE", field.name)
		}
		*field.target = value
	}
	value, err := jsonstrict.String(object["borrower_type"], "/borrower_type")
	if err != nil {
		return result, factsJSONError(err)
	}
	result.BorrowerType = BorrowerType(value)
	if expected != nil {
		if expected.Date.IsZero() {
			return result, fmt.Errorf("scoring facts: explicit evaluation date is required")
		}
		request := expected.Request
		if strings.TrimSpace(request.ApplicationID) == "" || strings.TrimSpace(request.Borrower.ID) == "" {
			return result, fmt.Errorf("scoring facts: expected request IDs are required")
		}
		if result.ApplicationID != request.ApplicationID {
			return result, factsError("FACTS_ID_MISMATCH", "application_id")
		}
		if result.BorrowerID != request.Borrower.ID {
			return result, factsError("FACTS_ID_MISMATCH", "borrower_id")
		}
		if result.BorrowerType != request.Borrower.Type {
			return result, factsError("FACTS_ID_MISMATCH", "borrower_type")
		}
		if result.ModelVersion != request.ModelVersion || request.ModelVersion != version {
			return result, factsError("FACTS_VERSION_MISMATCH", "model_version")
		}
		if result.FactsSchemaVersion != request.FactsSchemaVersion || request.FactsSchemaVersion != schema {
			return result, factsError("FACTS_VERSION_MISMATCH", "facts_schema_version")
		}
	}
	if result.BorrowerType != borrower {
		return result, factsError("FACTS_ID_MISMATCH", "borrower_type")
	}
	if result.ModelVersion != version {
		return result, factsError("FACTS_VERSION_MISMATCH", "model_version")
	}
	if result.FactsSchemaVersion != schema {
		return result, factsError("FACTS_VERSION_MISMATCH", "facts_schema_version")
	}
	date, err := time.Parse("2006-01-02", result.FactsDate)
	if err != nil {
		return result, factsError("INVALID_FACTS_RESPONSE", "facts_date")
	}
	if expected != nil {
		evaluation := expected.Date.UTC()
		evaluation = time.Date(evaluation.Year(), evaluation.Month(), evaluation.Day(), 0, 0, 0, 0, time.UTC)
		if date.After(evaluation) {
			return result, factsError("INVALID_FACTS_RESPONSE", "facts_date")
		}
	}
	return result, nil
}
