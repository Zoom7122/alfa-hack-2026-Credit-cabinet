package ofd_test

import (
	"net/http"
	"testing"

	ofd "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/OFD"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/ofd/v1", ofd.Register)

var token = []string{"X-OFD-Token", "test-token"}

func TestKKTAndReceipts(t *testing.T) {
	kkt := map[string]int{mt.InnA: 2, mt.InnB: 0, mt.InnC: 0, mt.InnD: 0, mt.InnE: 0, mt.Unknown: 0}
	for inn, n := range kkt {
		r := mt.Decode[struct {
			KKT []any `json:"kkt"`
		}](t, mt.Get(router, "/ofd/v1/organizations/"+inn+"/kkt", token...), http.StatusOK)
		if len(r.KKT) != n || r.KKT == nil {
			t.Errorf("%s: %d kkt", inn, len(r.KKT))
		}
	}
	type months struct {
		Months []struct {
			Month        string  `json:"month"`
			RevenueTotal float64 `json:"revenue_total"`
			RevenueCard  float64 `json:"revenue_card"`
			RevenueCash  float64 `json:"revenue_cash"`
		} `json:"months"`
	}
	a := mt.Decode[months](t, mt.Get(router, "/ofd/v1/organizations/"+mt.InnA+"/receipts/monthly", token...), http.StatusOK)
	if len(a.Months) != 12 || a.Months[0].Month != "2026-09" || a.Months[0].RevenueCard != 1203400 {
		t.Fatalf("A: %+v", a.Months[:1])
	}
	for _, m := range a.Months {
		if m.RevenueCard+m.RevenueCash != m.RevenueTotal {
			t.Errorf("%s: card + cash != total", m.Month)
		}
	}
	if two := mt.Decode[months](t, mt.Get(router, "/ofd/v1/organizations/"+mt.InnA+"/receipts/monthly?months=2", token...),
		http.StatusOK); len(two.Months) != 2 {
		t.Errorf("months=2: %d", len(two.Months))
	}
	if b := mt.Decode[months](t, mt.Get(router, "/ofd/v1/organizations/"+mt.InnB+"/receipts/monthly", token...), http.StatusOK); b.Months == nil || len(b.Months) != 0 {
		t.Errorf("B: %+v", b)
	}
}

func TestErrors(t *testing.T) {
	if code := mt.ErrorCode(t, mt.Get(router, "/ofd/v1/organizations/"+mt.InnA+"/kkt"), http.StatusUnauthorized); code != "INVALID_OFD_TOKEN" {
		t.Errorf("code %s", code)
	}
	mt.ErrorCode(t, mt.Get(router, "/ofd/v1/organizations/12/kkt", token...), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/ofd/v1/organizations/"+mt.InnA+"/receipts/monthly?months=13", token...), http.StatusBadRequest)
}
