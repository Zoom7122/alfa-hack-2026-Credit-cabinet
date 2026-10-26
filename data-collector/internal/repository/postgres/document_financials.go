package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/financial"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
)

// Only the current, completed generation is visible. Reverification, deletion
// and another application can never resurrect a previously approved result.
const selectDocumentFinancialsSQL = `
SELECT d.id, d.sha256, d.financial_generation, j.applicant, r.result
FROM documents d
JOIN financial_document_jobs j ON j.document_id = d.id AND j.generation = d.financial_generation AND j.status = 'COMPLETED'
JOIN financial_document_results r ON r.job_id = j.id AND r.document_id = d.id AND r.generation = d.financial_generation
WHERE d.application_id = $1 AND d.deleted_at IS NULL AND d.type IN ('TAX_RETURN', 'FINANCIAL_STATEMENTS')
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
		if testdocument.IsSHA256(sha) {
			continue
		}
		var applicant financial.Applicant
		var result financial.Result
		if err := json.Unmarshal(applicantJSON, &applicant); err != nil {
			return nil, fmt.Errorf("decode financial applicant: %w", err)
		}
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return nil, fmt.Errorf("decode financial result: %w", err)
		}
		if applicant.INN != app.Company.INN || applicant.Type != string(app.Company.Type) ||
			result.DocumentID != id || result.Generation != generation || result.DocumentSHA256 != sha ||
			!result.EligibleForScoring(applicant) {
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
