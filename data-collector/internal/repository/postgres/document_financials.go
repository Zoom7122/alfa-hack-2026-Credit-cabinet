package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Read the persisted approval contract without coupling the repository to the
// financial worker. Raw extraction is deliberately absent from this DTO.
type documentFinancialResult struct {
	ID             string `json:"id"`
	DocumentID     string `json:"document_id"`
	Generation     int
	DocumentSHA256 string `json:"document_sha256"`
	Signature      struct {
		DocumentSHA256  string `json:"document_sha256"`
		Policy, Verdict string
		TrustMode       string                   `json:"trust_mode"`
		PolicyResult    struct{ Satisfied bool } `json:"policy_result"`
	}
	ScoringIndicators []struct {
		Name, Period, Amount string
		PeriodKind           string `json:"period_kind"`
		Eligible             bool
		DataEligible         bool `json:"data_eligible"`
		Sources              []map[string]any
	} `json:"scoring_indicators"`
}

// Only the current, completed generation is visible. Reverification, deletion
// and another application can never resurrect a previously approved result.
const selectDocumentFinancialsSQL = `
SELECT d.id, d.sha256, d.financial_generation, j.applicant, r.result
FROM documents d
JOIN financial_document_jobs j ON j.document_id = d.id AND j.generation = d.financial_generation AND j.status = 'COMPLETED'
JOIN financial_document_results r ON r.job_id = j.id AND r.document_id = d.id AND r.generation = d.financial_generation AND r.original_sha256 = d.sha256
WHERE d.application_id = $1 AND d.deleted_at IS NULL AND d.type IN ('TAX_RETURN', 'FINANCIAL_STATEMENTS')
  AND d.signature_state = 'DONE' AND d.signature_verdict IN ('VALID', 'VALID_WITH_WARNINGS')
ORDER BY d.uploaded_at DESC, d.id DESC`

func (r *DecisionRepository) documentFinancials(ctx context.Context, app domain.Application) ([]decision.DocumentIndicator, error) {
	rows, err := r.pool.Query(ctx, selectDocumentFinancialsSQL, app.ID)
	if err != nil {
		return nil, fmt.Errorf("select document financials: %w", err)
	}
	defer rows.Close()
	out := []decision.DocumentIndicator{}
	for rows.Next() {
		var id, sha string
		var generation int
		var applicantJSON, resultJSON []byte
		if err := rows.Scan(&id, &sha, &generation, &applicantJSON, &resultJSON); err != nil {
			return nil, err
		}
		var applicant struct {
			Type      string `json:"applicant_type"`
			INN       string
			TaxRegime string `json:"tax_regime"`
			LegalForm string `json:"legal_form"`
		}
		var result documentFinancialResult
		if err := json.Unmarshal(applicantJSON, &applicant); err != nil {
			return nil, fmt.Errorf("decode financial applicant: %w", err)
		}
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return nil, fmt.Errorf("decode financial result: %w", err)
		}
		policy := "GIRBO_FNS"
		contextSupported := applicant.Type == "LEGAL_ENTITY" && applicant.LegalForm == "OOO"
		if applicant.Type == "SOLE_PROPRIETOR" {
			policy, contextSupported = "KUDIR_IP", applicant.TaxRegime == "USN_INCOME"
		}
		if !contextSupported || applicant.INN != app.Company.INN || applicant.Type != string(app.Company.Type) ||
			result.DocumentID != id || result.Generation != generation || result.DocumentSHA256 != sha ||
			result.Signature.DocumentSHA256 != sha || result.Signature.Policy != policy ||
			!result.Signature.PolicyResult.Satisfied || result.Signature.TrustMode != "LIVE" ||
			(result.Signature.Verdict != "VALID" && result.Signature.Verdict != "VALID_WITH_WARNINGS") {
			continue
		}
		for _, indicator := range result.ScoringIndicators {
			amount, ok := decision.WholeRubles(indicator.Amount)
			if !ok || !indicator.Eligible || !indicator.DataEligible {
				continue
			}
			out = append(out, decision.DocumentIndicator{DocumentID: id, ResultID: result.ID, Generation: generation,
				Name: indicator.Name, Period: indicator.Period, PeriodKind: indicator.PeriodKind, AmountRUB: amount, Sources: indicator.Sources})
		}
	}
	return out, rows.Err()
}
