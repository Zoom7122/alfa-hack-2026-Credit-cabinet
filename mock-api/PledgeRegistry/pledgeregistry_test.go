package pledgeregistry_test

import (
	"net/http"
	"testing"

	pledgeregistry "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/PledgeRegistry"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/pledge-registry/v1", pledgeregistry.Register)

type resp struct {
	Total int `json:"total"`
	Items []struct {
		Status   string `json:"status"`
		Pledgees []struct {
			Name string `json:"name"`
		} `json:"pledgees"`
		Subjects []struct {
			Class string `json:"class"`
		} `json:"subjects"`
	} `json:"items"`
}

func get(t *testing.T, q string) resp {
	t.Helper()
	return mt.Decode[resp](t, mt.Get(router, "/pledge-registry/v1/notifications?"+q), http.StatusOK)
}

func TestByPledgor(t *testing.T) {
	total := map[string]int{mt.InnA: 0, mt.InnB: 2, mt.InnC: 1, mt.InnD: 0, mt.InnE: 0, mt.Unknown: 0}
	for inn, n := range total {
		if r := get(t, "pledgor_inn="+inn); r.Total != n || len(r.Items) != n || r.Items == nil {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	c := get(t, "pledgor_inn="+mt.InnC)
	if c.Items[0].Subjects[0].Class != "GOODS_IN_TURNOVER" || c.Items[0].Pledgees[0].Name != `ООО "МФК ТОРГФИНАНС"` {
		t.Errorf("C: %+v", c)
	}
}

func TestByVINAndSerial(t *testing.T) {
	if r := get(t, "vin=xtc652005n1498213"); r.Total != 1 || r.Items[0].Subjects[0].Class != "VEHICLE" {
		t.Errorf("vin: %+v", r)
	}
	if r := get(t, "serial=JCB3CX2023118457"); r.Total != 1 || r.Items[0].Subjects[0].Class != "EQUIPMENT" {
		t.Errorf("serial: %+v", r)
	}
	// самосвал B в лизинге (VIN из Федресурса) — в залоге не числится
	if r := get(t, "vin=XTC651153P1234567"); r.Total != 0 {
		t.Errorf("leased truck: %+v", r)
	}
	for _, q := range []string{"", "vin=X&serial=Y", "pledgor_inn=12"} {
		mt.ErrorCode(t, mt.Get(router, "/pledge-registry/v1/notifications?"+q), http.StatusBadRequest)
	}
}
