package decision

import (
	"encoding/json"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"testing"
	"time"
)

func TestSnapshotPreservesMissingAndExactAmounts(t *testing.T) {
	raw := json.RawMessage(`{"bo_bfo":[{"period":"2025","typeCorrections":[{"correction":{"balance":{"current1600":0},"financialResult":{"current2110":123.456}}}]}]}`)
	st, err := ExactStatements(raw, "borrower", []int{2024, 2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.AnnualReports) != 1 {
		t.Fatal(st)
	}
	r := st.AnnualReports[0]
	if r.Balance.TotalAssets == nil || *r.Balance.TotalAssets != 0 || r.Balance.Equity != nil || *r.ProfitAndLoss.Revenue != 123456 {
		t.Fatalf("missing/zero/unit semantics lost: %+v %+v", r.Balance, r.ProfitAndLoss)
	}
}
func TestSnapshotConflictAndHashBinding(t *testing.T) {
	in := Input{Application: domain.Application{Company: domain.Company{ID: "borrower", Type: domain.CompanyLegalEntity}}, Now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), Systems: map[domain.System]json.RawMessage{domain.SystemFNS: json.RawMessage(`{"bo_bfo":[{"period":"2025","typeCorrections":[{"correction":{"financialResult":{"current2110":100}}}]}]}`)}}
	var doc SnapshotDocument
	if err := json.Unmarshal([]byte(`{"id":"doc","sha256":"hash","generation":2,"financial_result":{"document_sha256":"hash","scoring_indicators":[{"name":"accounting_revenue","period":"2025","period_kind":"YEAR","amount":"200000.00","eligible":true,"sources":[]}]}}`), &doc); err != nil {
		t.Fatal(err)
	}
	in.DocumentIndicators = []DocumentIndicator{{Name: "accounting_revenue", Period: "2025", PeriodKind: "YEAR", AmountRUB: 200000}}
	req, _, err := BuildSnapshot(in, "job", "collection", []SnapshotDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.InputSnapshot.Diagnostics) != 1 || !req.InputSnapshot.Diagnostics[0].Conflict || req.InputSnapshot.Statements.AnnualReports[0].ProfitAndLoss.Revenue != nil {
		t.Fatal("conflicting values admitted")
	}
	if req.FinancialStatements == nil || req.FinancialStatements.AnnualReports[0].ProfitAndLoss.Revenue != nil {
		t.Fatal("legacy document overlay replaced the frozen conflict")
	}
	doc.SHA256 = "modified"
	req, _, err = BuildSnapshot(in, "job", "collection", []SnapshotDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.InputSnapshot.Indicators) != 0 || *req.InputSnapshot.Statements.AnnualReports[0].ProfitAndLoss.Revenue != 100000 {
		t.Fatal("wrong document hash admitted")
	}
}
func TestKudirIncomeRemainsSeparate(t *testing.T) {
	in := Input{Application: domain.Application{Company: domain.Company{ID: "ip", Type: domain.CompanySoleProprietor}}, Now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	var doc SnapshotDocument
	json.Unmarshal([]byte(`{"id":"doc","sha256":"hash","generation":1,"financial_result":{"document_sha256":"hash","scoring_indicators":[{"name":"usn_taxable_income","period":"2025","period_kind":"YEAR","amount":"10000.50","eligible":true,"sources":[]}]}}`), &doc)
	in.DocumentIndicators = []DocumentIndicator{{Name: "usn_taxable_income", Period: "2025", PeriodKind: "YEAR", AmountRUB: 10000}}
	req, facts, err := BuildSnapshot(in, "job", "", []SnapshotDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.InputSnapshot.Indicators) != 1 || len(req.InputSnapshot.Statements.AnnualReports) != 0 || req.Evidence != nil || facts.DeclaredSource == "DOCUMENT_KUDIR" {
		t.Fatal("taxable income was used as accounting/cash income")
	}
}

func TestSnapshotIntegrityUsesIncludedDocuments(t *testing.T) {
	in := Input{Application: domain.Application{Company: domain.Company{ID: "co", Type: domain.CompanyLegalEntity}}, Now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), ForgedDocument: true}
	req, _, err := BuildSnapshot(in, "job", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if *req.Scoring.LegalEntity.MaterialForgery {
		t.Fatal("historical report leaked into current snapshot")
	}
	invalid := "INVALID"
	req, _, err = BuildSnapshot(in, "job", "", []SnapshotDocument{{ID: "current", IntegrityStatus: &invalid}})
	if err != nil {
		t.Fatal(err)
	}
	if !*req.Scoring.LegalEntity.MaterialForgery {
		t.Fatal("confirmed current integrity failure lost")
	}
}
