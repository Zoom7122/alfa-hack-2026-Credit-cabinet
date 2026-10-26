package decision

import "encoding/json"

// InputSnapshot freezes all inputs used by a dispatch round. Missing values stay nullable.
type InputSnapshot struct {
	ID             string               `json:"id"`
	EvaluationDate string               `json:"evaluation_date"`
	SourceStates   json.RawMessage      `json:"source_states"`
	CollectionID   string               `json:"collection_id"`
	Statements     *Statements          `json:"statements"`
	Documents      json.RawMessage      `json:"documents"`
	Indicators     []FinancialIndicator `json:"indicators"`
	Diagnostics    []InputDiagnostic    `json:"diagnostics"`
}
type FinancialIndicator struct {
	Name       string          `json:"name"`
	Period     string          `json:"period"`
	PeriodKind string          `json:"period_kind"`
	Amount     string          `json:"amount"` // exact decimal RUB
	Basis      string          `json:"basis"`
	DocumentID string          `json:"document_id"`
	Generation int             `json:"generation"`
	Sources    json.RawMessage `json:"sources"`
}
type InputDiagnostic struct {
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Fields   []string        `json:"fields"`
	Conflict bool            `json:"conflict"`
	Values   json.RawMessage `json:"values,omitempty"`
}
