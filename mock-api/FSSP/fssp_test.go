package fssp_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	fssp "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/FSSP"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/fssp/v1", fssp.Register)

type summary struct {
	ActiveCount           int     `json:"active_count"`
	ActiveAmount          float64 `json:"active_amount"`
	FinishedCount         int     `json:"finished_count"`
	FinishedNoAssetsCount int     `json:"finished_no_assets_count"`
}

type response struct {
	Debtor struct {
		Type string `json:"type"`
		INN  any    `json:"inn"`
	} `json:"debtor"`
	Summary summary `json:"summary"`
	Items   []struct {
		Status          string  `json:"status"`
		AmountRemaining float64 `json:"amount_remaining"`
		FinishReason    *string `json:"finish_reason_code"`
	} `json:"items"`
}

func get(t *testing.T, q url.Values) response {
	t.Helper()
	return mt.Decode[response](t, mt.Get(router, "/fssp/v1/proceedings?"+q.Encode()), http.StatusOK)
}

func TestByINN(t *testing.T) {
	active := map[string]int{mt.InnA: 1, mt.InnB: 0, mt.InnC: 3, mt.InnD: 0, mt.InnE: 0}
	for inn, n := range active {
		r := get(t, url.Values{"inn": {inn}})
		if r.Debtor.INN != inn || r.Summary.ActiveCount != n {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	if c := get(t, url.Values{"inn": {mt.InnC}}); c.Summary.ActiveAmount != 900000 || c.Summary.FinishedNoAssetsCount != 1 {
		t.Errorf("C summary: %+v", c.Summary)
	}
	if d := get(t, url.Values{"inn": {mt.InnD}}); d.Debtor.Type != "IP" {
		t.Errorf("D debtor type %s", d.Debtor.Type)
	}
	u := get(t, url.Values{"inn": {mt.Unknown}})
	if u.Items == nil || len(u.Items) != 0 || u.Summary.ActiveCount != 0 {
		t.Errorf("unknown: %+v", u)
	}
	mt.ErrorCode(t, mt.Get(router, "/fssp/v1/proceedings?inn=12"), http.StatusBadRequest)
}

func TestByName(t *testing.T) {
	// «Артем» без «ё», отчество не передано
	r := get(t, url.Values{"last_name": {"Белов"}, "first_name": {"Артем"}, "birth_date": {"1988-11-03"}, "region": {"77"}})
	if r.Debtor.INN != mt.DirC || r.Summary.ActiveCount != 1 {
		t.Fatalf("Belov: %+v", r)
	}
	// ИП ищется и как физлицо
	if r := get(t, url.Values{"last_name": {"Смирнова"}, "first_name": {"Анна"}, "middle_name": {"Викторовна"}}); r.Debtor.INN != mt.InnD {
		t.Fatalf("Smirnova: %+v", r)
	}
	for _, q := range []url.Values{
		{"last_name": {"Белов"}, "first_name": {"Артём"}, "birth_date": {"1970-01-01"}},
		{"last_name": {"Белов"}, "first_name": {"Артём"}, "region": {"50"}},
		{"last_name": {"Белов"}, "first_name": {"Артём"}, "middle_name": {"Петрович"}},
	} {
		if r := get(t, q); r.Debtor.INN != nil || len(r.Items) != 0 {
			t.Errorf("%v must not match: %+v", q, r)
		}
	}
	for _, q := range []string{"", "last_name=Белов", "last_name=Белов&first_name=Артём&birth_date=1988"} {
		if code := mt.ErrorCode(t, mt.Get(router, "/fssp/v1/proceedings?"+q), http.StatusBadRequest); code != mockkit.CodeBadRequest {
			t.Errorf("%q: code %s", q, code)
		}
	}
}

func TestSeedSummaries(t *testing.T) {
	files, _ := filepath.Glob("seed/*.json")
	if len(files) == 0 {
		t.Fatal("no seeds")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var r response
		if err := json.Unmarshal(body, &r); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var want summary
		for _, it := range r.Items {
			switch it.Status {
			case "ACTIVE":
				want.ActiveCount++
				want.ActiveAmount += it.AmountRemaining
			case "FINISHED":
				want.FinishedCount++
				if it.FinishReason != nil && (*it.FinishReason == "46-1-3" || *it.FinishReason == "46-1-4") {
					want.FinishedNoAssetsCount++
				}
			default:
				t.Errorf("%s: status %q", f, it.Status)
			}
		}
		if r.Summary != want {
			t.Errorf("%s: summary %+v, want %+v", f, r.Summary, want)
		}
	}
}
