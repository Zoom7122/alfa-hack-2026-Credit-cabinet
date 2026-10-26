package decision

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/testdocument"
)

func TestTestPDFAttachmentCannotConfirmScoringEvidence(t *testing.T) {
	for _, soleProprietor := range []bool{false, true} {
		in := legalInput()
		if soleProprietor {
			in.Application.Company.Type = domain.CompanySoleProprietor
		}
		in.Scoring = nil
		in.Systems = nil
		baseline, baseFacts := Build(in)
		for _, typ := range []domain.DocumentType{domain.DocFinancialStatements, domain.DocTaxReturn, domain.DocBankStatement, domain.DocContractWithCustomer} {
			in.Application.Documents = []domain.Document{{ID: "test-pdf", Type: typ, SHA256: testdocument.SHA256}}
			got, facts := Build(in)
			if !reflect.DeepEqual(got, baseline) || !reflect.DeepEqual(facts, baseFacts) {
				t.Fatalf("test PDF affects evidence for %s/%s", in.Application.Company.Type, typ)
			}
			if len(in.Application.Documents) != 1 {
				t.Fatal("scoring mutated the application's attachment list")
			}
		}
	}
}

func TestDocumentIncomeChangesScoringEvidence(t *testing.T) {
	in := legalInput()
	in.Application.Company.Type = domain.CompanySoleProprietor
	in.DocumentIndicators = []DocumentIndicator{
		{DocumentID: "new", Name: "usn_taxable_income", Period: "2025", PeriodKind: "YEAR", AmountRUB: 4_000_000},
		{DocumentID: "old", Name: "usn_taxable_income", Period: "2025", PeriodKind: "YEAR", AmountRUB: 9_000_000},
		{DocumentID: "previous", Name: "usn_taxable_income", Period: "2024", PeriodKind: "YEAR", AmountRUB: 3_000_000},
		{Name: "usn_taxable_income", Period: "2026", PeriodKind: "YEAR", AmountRUB: 99_000_000},
	}
	r, facts := Build(in)
	if facts.DeclaredRevenue != 4_000_000 || facts.DeclaredSource != "DOCUMENT_KUDIR" || len(facts.DocumentIndicators) != 2 {
		t.Fatalf("document provenance: %+v", facts)
	}
	if *r.Evidence.ConfirmedReceiptsRUB != 4_000_000 || *r.Evidence.UnexplainedDifferenceRUB != 8_000_000 ||
		*r.IPEconomicData.PreviousComparableReceiptsRUB != 3_000_000 || *r.Scoring.IP.IncomeEvidence != "VERIFIED" {
		t.Fatalf("document income did not affect scoring: %+v %+v", r.Evidence, r.IPEconomicData)
	}
	if r.FinancialStatements != nil {
		t.Fatal("IP must not send accounting statements")
	}
	// A verified zero must not fall back to a more favorable external revenue.
	in.DocumentIndicators[0].AmountRUB = 0
	r, facts = Build(in)
	if facts.DeclaredRevenue != 0 || *r.Evidence.ConfirmedReceiptsRUB != 0 || *r.Evidence.UnexplainedDifferenceRUB != 12_000_000 {
		t.Fatal("zero income was lost")
	}
}

func TestDocumentStatementsOverlayInRubles(t *testing.T) {
	in := legalInput()
	in.DocumentIndicators = []DocumentIndicator{
		{DocumentID: "pdf", Name: "accounting_revenue", Period: "2025", PeriodKind: "YEAR", AmountRUB: 2_000_000},
		{DocumentID: "pdf", Name: "net_profit", Period: "2025", PeriodKind: "YEAR", AmountRUB: -5_000},
		{DocumentID: "pdf", Name: "equity", Period: "2025", PeriodKind: "AS_OF", AmountRUB: -100_000},
		{DocumentID: "pdf", Name: "total_assets", Period: "2025", PeriodKind: "AS_OF", AmountRUB: 9_000_000},
		{DocumentID: "pdf", Name: "long_term_liabilities", Period: "2025", PeriodKind: "AS_OF", AmountRUB: 4_000_000},
		{DocumentID: "pdf", Name: "short_term_liabilities", Period: "2025", PeriodKind: "AS_OF", AmountRUB: 5_100_000},
		{Name: "accounting_revenue", Period: "2024", PeriodKind: "AS_OF", AmountRUB: 99_000_000},
		{DocumentID: "older-pdf", Name: "accounting_revenue", Period: "2025", PeriodKind: "YEAR", AmountRUB: 99_000_000},
	}
	r, facts := Build(in)
	if r.FinancialStatements == nil || len(facts.DocumentIndicators) != 6 || facts.DeclaredSource != "DOCUMENT_GIRBO" {
		t.Fatalf("statements not sent: %+v", facts)
	}
	last := r.FinancialStatements.AnnualReports[1]
	if *last.Balance.Unit != "RUB" || *last.ProfitAndLoss.Unit != "RUB" ||
		*last.ProfitAndLoss.Revenue != 2_000_000 || *last.ProfitAndLoss.NetProfit != -5_000 ||
		*last.Balance.Equity != -100_000 || *last.Balance.TotalAssets != 9_000_000 ||
		*last.Balance.LongTermLiabilities != 4_000_000 || *last.Balance.CurrentLiabilities != 5_100_000 {
		t.Fatalf("wrong normalization: %+v %+v", last.Balance, last.ProfitAndLoss)
	}
	base, _ := StatementsFromFNS(in.Systems[domain.SystemFNS], in.Application.Company.ID, []int{2024, 2025})
	if *last.Balance.CashAndEquivalents != *base.AnnualReports[1].Balance.CashAndEquivalents*1000 {
		t.Fatal("external fields were not converted to rubles")
	}
	if *r.Evidence.ConfirmedReceiptsRUB != 2_400_000 || *r.Scoring.LegalEntity.AuditorOpinion != "NOT_REQUIRED" {
		t.Fatal("document revenue used wrong units in evidence or audit threshold")
	}
	encoded, _ := json.Marshal(r)
	var wire struct {
		FinancialStatements *Statements `json:"financial_statements"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil || wire.FinancialStatements == nil || wire.FinancialStatements.BorrowerID != r.Borrower.ID {
		t.Fatal("application-scoped statements missing from HTTP request")
	}
}

func TestPartialDocumentDoesNotInventStatementRows(t *testing.T) {
	in := legalInput()
	delete(in.Systems, domain.SystemFNS)
	in.DocumentIndicators = []DocumentIndicator{{Name: "accounting_revenue", Period: "2025", PeriodKind: "YEAR", AmountRUB: 1000}}
	r, _ := Build(in)
	last := r.FinancialStatements.AnnualReports[0]
	if last.Balance.TotalAssets != nil || last.ProfitAndLoss.CostOfSales != nil || r.Scoring.LegalEntity.AuditorOpinion != nil {
		t.Fatal("missing document rows became zero or established an audit opinion")
	}
}

func TestWholeRubles(t *testing.T) {
	for _, s := range []string{"1.01", "1/1", "1e3", "NaN", "9223372036854775808", "-9223372036854775809", ""} {
		if _, ok := WholeRubles(s); ok {
			t.Fatalf("invalid or fractional amount admitted: %q", s)
		}
	}
	for s, want := range map[string]int64{"0.00": 0, "12000000.00": 12_000_000, "-5000.00": -5000, "9223372036854775807": math.MaxInt64} {
		if got, ok := WholeRubles(s); !ok || got != want {
			t.Fatalf("%s: %d %v", s, got, ok)
		}
	}
}
