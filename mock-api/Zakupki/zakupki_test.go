package zakupki_test

import (
	"net/http"
	"testing"
	"time"

	zakupki "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Zakupki"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/zakupki/v1", zakupki.Register)

type summary struct {
	ActiveCount       int     `json:"active_count"`
	ActiveAmount      float64 `json:"active_amount"`
	ActiveUnpaid      float64 `json:"active_unpaid_amount"`
	CompletedCount36  int     `json:"completed_count_36m"`
	CompletedAmount36 float64 `json:"completed_amount_36m"`
	TerminatedCount36 int     `json:"terminated_count_36m"`
	Penalties36       float64 `json:"penalties_amount_36m"`
}

type contract struct {
	RegNumber  string  `json:"reg_number"`
	Price      float64 `json:"price"`
	SignedAt   string  `json:"signed_at"`
	Status     string  `json:"status"`
	PaidAmount float64 `json:"paid_amount"`
	Penalties  []struct {
		Amount float64 `json:"amount"`
	} `json:"penalties"`
}

type contracts struct {
	Summary summary    `json:"summary"`
	Items   []contract `json:"items"`
}

func TestContracts(t *testing.T) {
	e := mt.Decode[contracts](t, mt.Get(router, "/zakupki/v1/contracts?supplier_inn="+mt.InnE+"&date_from=2023-10-01"), http.StatusOK)
	want := summary{1, 4920000, 2050000, 3, 6100000, 0, 0}
	if e.Summary != want || len(e.Items) != 4 {
		t.Fatalf("E: %+v, %d items", e.Summary, len(e.Items))
	}
	if recent := mt.Decode[contracts](t, mt.Get(router, "/zakupki/v1/contracts?supplier_inn="+mt.InnE+"&date_from=2025-01-01"),
		http.StatusOK); len(recent.Items) != 2 || recent.Summary != want {
		t.Errorf("E from 2025: %+v", recent)
	}
	c := mt.Decode[contracts](t, mt.Get(router, "/zakupki/v1/contracts?supplier_inn="+mt.InnC), http.StatusOK)
	if len(c.Items) != 1 || c.Items[0].Status != "TERMINATED" || c.Summary.TerminatedCount36 != 1 {
		t.Errorf("C: %+v", c)
	}
	if a := mt.Decode[contracts](t, mt.Get(router, "/zakupki/v1/contracts?supplier_inn="+mt.InnA), http.StatusOK); a.Items == nil || len(a.Items) != 0 {
		t.Errorf("A: %+v", a)
	}
	mt.ErrorCode(t, mt.Get(router, "/zakupki/v1/contracts"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/zakupki/v1/contracts?supplier_inn="+mt.InnE+"&date_from=x"), http.StatusBadRequest)
}

func TestRNP(t *testing.T) {
	type rnp struct {
		Listed  bool  `json:"listed"`
		Entries []any `json:"entries"`
	}
	for _, inn := range append(append([]string{}, mt.Companies...), mt.Unknown) {
		r := mt.Decode[rnp](t, mt.Get(router, "/zakupki/v1/rnp?inn="+inn), http.StatusOK)
		if r.Listed != (inn == mt.InnC) || r.Entries == nil || r.Listed != (len(r.Entries) > 0) {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	mt.ErrorCode(t, mt.Get(router, "/zakupki/v1/rnp?inn=1"), http.StatusBadRequest)
}

func TestSeedSummaries(t *testing.T) {
	asOf, _ := time.Parse(time.DateOnly, mockkit.AsOf)
	since := asOf.AddDate(0, 0, -3*365).Format(time.DateOnly)
	for _, f := range mt.SeedFiles(t, "seed/*.json") {
		s := mt.ReadSeed[struct {
			Summary   summary    `json:"summary"`
			Contracts []contract `json:"contracts"`
		}](t, f)
		var want summary
		for _, c := range s.Contracts {
			if c.Status == "EXECUTING" {
				want.ActiveCount++
				want.ActiveAmount += c.Price
				want.ActiveUnpaid += c.Price - c.PaidAmount
			}
			if c.SignedAt < since {
				continue
			}
			switch c.Status {
			case "COMPLETED":
				want.CompletedCount36++
				want.CompletedAmount36 += c.Price
			case "TERMINATED":
				want.TerminatedCount36++
			}
			for _, p := range c.Penalties {
				want.Penalties36 += p.Amount
			}
		}
		if s.Summary != want {
			t.Errorf("%s: summary %+v, want %+v", f, s.Summary, want)
		}
	}
}
