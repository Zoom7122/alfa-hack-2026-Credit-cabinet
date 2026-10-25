package marketplaces_test

import (
	"net/http"
	"testing"

	marketplaces "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Marketplaces"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/marketplaces/v1", marketplaces.Register)

var token = []string{"X-Seller-Token", "test-token"}

type month struct {
	Month         string  `json:"month"`
	GrossSales    float64 `json:"gross_sales"`
	ReturnsAmount float64 `json:"returns_amount"`
	Commission    float64 `json:"commission"`
	Logistics     float64 `json:"logistics"`
	Payout        float64 `json:"payout"`
}

type payout struct {
	Date        string  `json:"date"`
	Amount      float64 `json:"amount"`
	Destination struct {
		Type    string `json:"type"`
		Account string `json:"account"`
	} `json:"destination"`
}

func TestSeller(t *testing.T) {
	for _, mp := range []string{"wildberries", "ozon"} {
		p := mt.Decode[struct {
			INN         string `json:"inn"`
			Marketplace string `json:"marketplace"`
		}](t, mt.Get(router, "/marketplaces/v1/"+mp+"/sellers/"+mt.InnD, token...), http.StatusOK)
		if p.INN != mt.InnD || p.Marketplace != mp {
			t.Errorf("%s: %+v", mp, p)
		}
	}
	m := mt.Decode[struct {
		Months []month `json:"months"`
	}](t, mt.Get(router, "/marketplaces/v1/wildberries/sellers/"+mt.InnD+"/sales/monthly", token...), http.StatusOK)
	if len(m.Months) != 24 || m.Months[0].Month != "2026-09" || m.Months[0].Payout != 251350 {
		t.Fatalf("WB monthly: %d months, first %+v", len(m.Months), m.Months[0])
	}
	all := mt.Decode[struct {
		Months []month `json:"months"`
	}](t, mt.Get(router, "/marketplaces/v1/wildberries/sellers/"+mt.InnD+"/sales/monthly?months=36", token...), http.StatusOK)
	if last := all.Months[len(all.Months)-1].Month; last != "2024-03" {
		t.Errorf("WB history starts %s, want 2024-03", last)
	}

	p := mt.Decode[struct {
		Items []payout `json:"items"`
	}](t, mt.Get(router, "/marketplaces/v1/wildberries/sellers/"+mt.InnD+"/payouts?from=2026-08-01&to=2026-09-30", token...),
		http.StatusOK)
	if len(p.Items) != 8 || p.Items[0].Destination.Type != "CARD" || p.Items[7].Destination.Type != "ACCOUNT" {
		t.Errorf("payouts: %+v", p.Items)
	}
}

func TestErrors(t *testing.T) {
	base := "/marketplaces/v1/wildberries/sellers/" + mt.InnD
	if code := mt.ErrorCode(t, mt.Get(router, base), http.StatusUnauthorized); code != "INVALID_SELLER_TOKEN" {
		t.Errorf("no token: %s", code)
	}
	mt.ErrorCode(t, mt.Get(router, base, "X-Seller-Token", "invalid"), http.StatusUnauthorized)
	if code := mt.ErrorCode(t, mt.Get(router, "/marketplaces/v1/ozon/sellers/"+mt.InnA, token...), http.StatusNotFound); code != "SELLER_NOT_FOUND" {
		t.Errorf("not a seller: %s", code)
	}
	mt.ErrorCode(t, mt.Get(router, "/marketplaces/v1/avito/sellers/"+mt.InnD, token...), http.StatusNotFound)
	mt.ErrorCode(t, mt.Get(router, base+"/sales/monthly?months=37", token...), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, base+"/payouts?from=01.08.2026", token...), http.StatusBadRequest)
}

// Выплата за месяц = продажи − возвраты − комиссия − логистика = сумма выплат этого месяца.
func TestSeedConsistency(t *testing.T) {
	s := mt.ReadSeed[map[string]struct {
		Months  []month  `json:"months"`
		Payouts []payout `json:"payouts"`
	}](t, "seed/"+mt.InnD+".json")
	for mp, sel := range s {
		paid := map[string]float64{}
		for _, p := range sel.Payouts {
			paid[p.Date[:7]] += p.Amount
		}
		for _, m := range sel.Months {
			if d := m.GrossSales - m.ReturnsAmount - m.Commission - m.Logistics - m.Payout; d > 0.005 || d < -0.005 {
				t.Errorf("%s %s: payout %.2f does not add up", mp, m.Month, m.Payout)
			}
			if d := paid[m.Month] - m.Payout; d > 0.005 || d < -0.005 {
				t.Errorf("%s %s: payouts %.2f, monthly payout %.2f", mp, m.Month, paid[m.Month], m.Payout)
			}
		}
	}
}
