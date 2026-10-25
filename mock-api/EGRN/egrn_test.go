package egrn_test

import (
	"net/http"
	"testing"

	egrn "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/EGRN"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/egrn/v1", egrn.Register)

type extract struct {
	Object struct {
		CadastralNumber string  `json:"cadastral_number"`
		Type            string  `json:"type"`
		CadastralValue  float64 `json:"cadastral_value"`
	} `json:"object"`
	Rights []struct {
		Holder struct {
			INN string `json:"inn"`
		} `json:"holder"`
	} `json:"rights"`
	Encumbrances []struct {
		Type string `json:"type"`
	} `json:"encumbrances"`
}

func post(t *testing.T, number string, status int) extract {
	t.Helper()
	return mt.Decode[extract](t, mt.PostJSON(t, router, "/egrn/v1/extracts",
		map[string]any{"cadastral_number": number, "purpose": "COLLATERAL_ASSESSMENT"}), status)
}

func TestExtracts(t *testing.T) {
	// склад B — в ипотеке
	w := post(t, "50:21:0110203:845", http.StatusOK)
	if w.Object.Type != "BUILDING" || w.Rights[0].Holder.INN != mt.InnB || len(w.Encumbrances) != 1 ||
		w.Encumbrances[0].Type != "MORTGAGE" {
		t.Errorf("warehouse: %+v", w)
	}
	// участок директора B — свободен, но владелец не компания
	l := post(t, "50:21:0110203:412", http.StatusOK)
	if l.Object.Type != "LAND" || l.Rights[0].Holder.INN != mt.DirB || len(l.Encumbrances) != 0 {
		t.Errorf("land: %+v", l)
	}
	if code := mt.ErrorCode(t, mt.PostJSON(t, router, "/egrn/v1/extracts", map[string]any{"cadastral_number": "50:21:0110203:1"}),
		http.StatusNotFound); code != "OBJECT_NOT_FOUND" {
		t.Errorf("code %s", code)
	}
	for _, b := range []any{"{", map[string]any{"cadastral_number": "склад"}} {
		mt.ErrorCode(t, mt.PostJSON(t, router, "/egrn/v1/extracts", b), http.StatusBadRequest)
	}
}
