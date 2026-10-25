package taxreports_test

import (
	"net/http"
	"testing"

	taxreports "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/TaxReports"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/tax-reports/v1", taxreports.Register)

var allDocs = []string{"PROFIT", "VAT", "USN", "NPD_INCOME", "ENS_BALANCE"}

type doc struct {
	Type    string             `json:"type"`
	Period  string             `json:"period"`
	Reason  string             `json:"reason"`
	DueDate string             `json:"due_date"`
	Data    map[string]float64 `json:"data"`
}

type result struct {
	RequestID    string `json:"request_id"`
	Status       string `json:"status"`
	INN          string `json:"inn"`
	TaxpayerType string `json:"taxpayer_type"`
	TaxRegime    struct {
		Current string `json:"current"`
	} `json:"tax_regime"`
	Documents    []doc `json:"documents"`
	NotAvailable []doc `json:"not_available"`
}

func body(inn string, docs []string, periods []string) map[string]any {
	return map[string]any{"inn": inn, "application_id": "APP-1", "documents": docs, "periods": periods,
		"consent": map[string]any{"id": "TAX-CNS-1", "valid_until": "2027-04-21", "scope": []string{"DECLARATIONS", "ENS_BALANCE"}}}
}

func fetch(t *testing.T, req map[string]any) result {
	t.Helper()
	created := mt.Decode[result](t, mt.PostJSON(t, router, "/tax-reports/v1/requests", req), http.StatusAccepted)
	if created.Status != "PROCESSING" || created.RequestID == "" {
		t.Fatalf("create: %+v", created)
	}
	r := mt.Decode[result](t, mt.Get(router, "/tax-reports/v1/requests/"+created.RequestID), http.StatusOK)
	if r.Status != "READY" || r.RequestID != created.RequestID {
		t.Fatalf("result: %+v", r)
	}
	return r
}

func find(docs []doc, typ, period string) *doc {
	for i := range docs {
		if docs[i].Type == typ && (period == "" || docs[i].Period == period) {
			return &docs[i]
		}
	}
	return nil
}

func TestAllScenarios(t *testing.T) {
	regimes := map[string]string{mt.InnA: "OSN", mt.InnB: "OSN", mt.InnC: "OSN", mt.InnD: "USN_INCOME", mt.InnE: "OSN"}
	for inn, regime := range regimes {
		r := fetch(t, body(inn, allDocs, nil))
		if r.INN != inn || r.TaxRegime.Current != regime || find(r.Documents, "ENS_BALANCE", "") == nil {
			t.Errorf("%s: %+v", inn, r)
		}
	}
}

func TestScenarioA(t *testing.T) {
	r := fetch(t, body(mt.InnA, allDocs, []string{"2025", "2026"}))
	p := find(r.Documents, "PROFIT", "2025")
	if p == nil || p.Data["revenue_from_sales"] != 58420000 || p.Data["tax_amount"] != 690000 {
		t.Fatalf("PROFIT 2025: %+v", p)
	}
	if find(r.Documents, "PROFIT", "2024") != nil {
		t.Error("period filter: PROFIT 2024 must not be returned")
	}
	if q3 := find(r.Documents, "VAT", "2026-Q3"); q3 == nil || q3.Data["vat_payable"] != 337600 {
		t.Errorf("VAT Q3: %+v", q3)
	}
	if na := find(r.NotAvailable, "USN", ""); na == nil || na.Reason != "NOT_APPLICABLE" {
		t.Errorf("USN: %+v", r.NotAvailable)
	}
}

func TestScenarioCAndD(t *testing.T) {
	c := fetch(t, body(mt.InnC, []string{"ENS_BALANCE"}, nil))
	if ens := find(c.Documents, "ENS_BALANCE", ""); ens == nil || ens.Data["balance"] != -412870 || ens.Data["debt"]+ens.Data["penalties"] != 412870 {
		t.Errorf("C ENS: %+v", c.Documents)
	}
	d := fetch(t, body(mt.InnD, allDocs, []string{"2024", "2025", "2026"}))
	if d.TaxpayerType != "IP" || len(d.Documents) != 4 {
		t.Errorf("D: %+v", d)
	}
	if usn := find(d.NotAvailable, "USN", "2026"); usn == nil || usn.Reason != "NOT_DUE" || usn.DueDate != "2027-04-25" {
		t.Errorf("D USN: %+v", d.NotAvailable)
	}
	for _, period := range []string{"2024", "2025", "2026-01-01..2026-07-27"} {
		n := find(d.Documents, "NPD_INCOME", period)
		if n == nil || n.Data["tax_paid"] != n.Data["income"]*0.06 {
			t.Errorf("D NPD %s: %+v", period, n)
		}
	}
}

func TestErrors(t *testing.T) {
	noConsent := body(mt.InnA, allDocs, nil)
	delete(noConsent, "consent")
	expired := body(mt.InnA, allDocs, nil)
	expired["consent"] = map[string]any{"id": "x", "valid_until": "2026-01-01", "scope": []string{"DECLARATIONS"}}
	narrow := body(mt.InnA, []string{"ENS_BALANCE"}, nil)
	narrow["consent"] = map[string]any{"id": "x", "valid_until": "2027-01-01", "scope": []string{"DECLARATIONS"}}

	cases := []struct {
		body   any
		status int
		code   string
	}{
		{noConsent, http.StatusForbidden, mockkit.CodeConsentRequired},
		{expired, http.StatusForbidden, mockkit.CodeConsentExpired},
		{narrow, http.StatusForbidden, "CONSENT_SCOPE"},
		{body(mt.InnA, []string{"SALARY"}, nil), http.StatusBadRequest, mockkit.CodeBadRequest},
		{body(mt.InnA, nil, nil), http.StatusBadRequest, mockkit.CodeBadRequest},
		{body("1", allDocs, nil), http.StatusBadRequest, mockkit.CodeInvalidINN},
		{body(mt.Unknown, allDocs, nil), http.StatusNotFound, mockkit.CodeNotFound},
		{"{", http.StatusBadRequest, mockkit.CodeBadRequest},
	}
	for i, tc := range cases {
		if code := mt.ErrorCode(t, mt.PostJSON(t, router, "/tax-reports/v1/requests", tc.body), tc.status); code != tc.code {
			t.Errorf("#%d: code %s, want %s", i, code, tc.code)
		}
	}
	if code := mt.ErrorCode(t, mt.Get(router, "/tax-reports/v1/requests/TXR-000000"), http.StatusNotFound); code != "REQUEST_NOT_FOUND" {
		t.Errorf("code %s", code)
	}
}
