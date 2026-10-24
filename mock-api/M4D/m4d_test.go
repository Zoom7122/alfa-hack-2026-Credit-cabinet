package m4d_test

import (
	"net/http"
	"testing"

	m4d "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/M4D"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/m4d/v1", m4d.Register)

type poa struct {
	Number     string  `json:"number"`
	Status     string  `json:"status"`
	ValidFrom  string  `json:"valid_from"`
	ValidUntil string  `json:"valid_until"`
	RevokedAt  *string `json:"revoked_at"`
	Principal  struct {
		INN string `json:"inn"`
	} `json:"principal"`
	Representative struct {
		INN string `json:"inn"`
	} `json:"representative"`
	Powers []struct {
		Code string `json:"code"`
		Text string `json:"text"`
	} `json:"powers"`
}

const (
	active  = "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37"
	expired = "3c9e7d10-55a2-4f61-8b0e-2a7c4f9d6b12"
	revoked = "a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945"
)

func TestGet(t *testing.T) {
	want := map[string]string{active: "ACTIVE", expired: "EXPIRED", revoked: "REVOKED"}
	for number, status := range want {
		p := mt.Decode[poa](t, mt.Get(router, "/m4d/v1/poa/"+number), http.StatusOK)
		if p.Number != number || p.Status != status || p.Principal.INN != mt.InnA {
			t.Errorf("%s: %+v", number, p)
		}
	}
	// главбух может подать заявку, но не подписать кредитный договор
	p := mt.Decode[poa](t, mt.Get(router, "/m4d/v1/poa/"+active), http.StatusOK)
	for _, pw := range p.Powers {
		if pw.Code == "LOAN_CONTRACT_SIGN" {
			t.Error("active POA must not allow LOAN_CONTRACT_SIGN")
		}
	}
	if code := mt.ErrorCode(t, mt.Get(router, "/m4d/v1/poa/00000000-0000-0000-0000-000000000000"), http.StatusNotFound); code != mockkit.CodeNotFound {
		t.Errorf("code %s", code)
	}
}

func TestSearch(t *testing.T) {
	type list struct {
		Items []poa `json:"items"`
	}
	all := mt.Decode[list](t, mt.Get(router, "/m4d/v1/poa?principal_inn="+mt.InnA), http.StatusOK)
	if len(all.Items) != 3 {
		t.Fatalf("A: %d POAs", len(all.Items))
	}
	lit := mt.Decode[list](t, mt.Get(router, "/m4d/v1/poa?principal_inn="+mt.InnA+"&representative_inn=779900010333"), http.StatusOK)
	if len(lit.Items) != 2 || lit.Items[0].Number != active {
		t.Fatalf("Litvinova: %+v", lit.Items)
	}
	if b := mt.Decode[list](t, mt.Get(router, "/m4d/v1/poa?principal_inn="+mt.InnB), http.StatusOK); b.Items == nil || len(b.Items) != 0 {
		t.Errorf("B: %+v", b)
	}
	mt.ErrorCode(t, mt.Get(router, "/m4d/v1/poa"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/m4d/v1/poa?principal_inn="+mt.InnA+"&representative_inn=1"), http.StatusBadRequest)
}

// Статус в seed согласован с датами на «сегодня» мока.
func TestSeedStatuses(t *testing.T) {
	for _, f := range mt.SeedFiles(t, "seed/*.json") {
		p := mt.ReadSeed[poa](t, f)
		want := "ACTIVE"
		switch {
		case p.RevokedAt != nil:
			want = "REVOKED"
		case p.ValidUntil < mockkit.AsOf:
			want = "EXPIRED"
		}
		if p.Status != want || p.ValidFrom > mockkit.AsOf {
			t.Errorf("%s: status %s, want %s", f, p.Status, want)
		}
		for _, pw := range p.Powers {
			if pw.Text == "" {
				t.Errorf("%s: power %s without text", f, pw.Code)
			}
		}
	}
}
