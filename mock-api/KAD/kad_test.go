package kad_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	kad "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/KAD"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/kad/v1", kad.Register)

type summary struct {
	DefCount12m    int     `json:"as_defendant_count_12m"`
	DefAmount12m   float64 `json:"as_defendant_amount_12m"`
	DefActive      float64 `json:"as_defendant_active_amount"`
	PlfCount12m    int     `json:"as_plaintiff_count_12m"`
	PlfAmount12m   float64 `json:"as_plaintiff_amount_12m"`
	BankruptDebtor bool    `json:"bankruptcy_as_debtor"`
}

type response struct {
	INN     string  `json:"inn"`
	Summary summary `json:"summary"`
	Total   int     `json:"total"`
	Items   []struct {
		CaseNumber   string  `json:"case_number"`
		CaseType     string  `json:"case_type"`
		Role         string  `json:"role"`
		RegisteredAt string  `json:"registered_at"`
		ClaimAmount  float64 `json:"claim_amount"`
		Status       string  `json:"status"`
	} `json:"items"`
}

func search(t *testing.T, body map[string]any) response {
	t.Helper()
	return mt.Decode[response](t, mt.PostJSON(t, router, "/kad/v1/cases/search", body), http.StatusOK)
}

func TestScenarios(t *testing.T) {
	total := map[string]int{mt.InnA: 6, mt.InnB: 0, mt.InnC: 4, mt.InnD: 0, mt.InnE: 2}
	for inn, n := range total {
		if r := search(t, map[string]any{"inn": inn}); r.INN != inn || r.Total != n || len(r.Items) != n {
			t.Errorf("%s: total %d, items %d, want %d", inn, r.Total, len(r.Items), n)
		}
	}
	if c := search(t, map[string]any{"inn": mt.InnC}); !c.Summary.BankruptDebtor || c.Items[0].CaseType != "BANKRUPTCY" {
		t.Errorf("C: %+v", c.Summary)
	}
	if e := search(t, map[string]any{"inn": mt.InnE, "role": "DEFENDANT"}); e.Total != 1 || e.Items[0].ClaimAmount != 3200000 ||
		e.Items[0].Status != "PENDING" {
		t.Errorf("E: %+v", e)
	}
}

func TestFilters(t *testing.T) {
	cases := []struct {
		body  map[string]any
		total int
	}{
		{map[string]any{"inn": mt.InnA, "date_from": "2023-10-01"}, 5}, // пример из дока
		{map[string]any{"inn": mt.InnA, "role": "PLAINTIFF"}, 4},
		{map[string]any{"inn": mt.InnA, "role": "PLAINTIFF", "date_from": "2023-10-01"}, 3},
		{map[string]any{"inn": mt.InnA, "role": "DEFENDANT"}, 2},
		{map[string]any{"inn": mt.InnA, "role": "THIRD_PARTY"}, 0},
		{map[string]any{"inn": mt.InnC, "role": "DEFENDANT"}, 4}, // должник в банкротстве — тоже ответчик
	}
	for _, tc := range cases {
		r := search(t, tc.body)
		if r.Total != tc.total {
			t.Errorf("%v: total %d, want %d", tc.body, r.Total, tc.total)
		}
		// summary не зависит от фильтров
		if full := search(t, map[string]any{"inn": tc.body["inn"]}); r.Summary != full.Summary {
			t.Errorf("%v: summary changed with filters", tc.body)
		}
	}

	p3 := search(t, map[string]any{"inn": mt.InnA, "size": 2, "page": 3})
	if p3.Total != 6 || len(p3.Items) != 2 {
		t.Errorf("page 3: %+v", p3)
	}
	if p4 := search(t, map[string]any{"inn": mt.InnA, "size": 2, "page": 4}); p4.Items == nil || len(p4.Items) != 0 {
		t.Errorf("page 4: %+v", p4)
	}
	if u := search(t, map[string]any{"inn": mt.Unknown}); u.Total != 0 || u.Items == nil || u.Summary != (summary{}) {
		t.Errorf("unknown: %+v", u)
	}
}

func TestErrors(t *testing.T) {
	for _, b := range []any{
		"{",
		map[string]any{"inn": "1"},
		map[string]any{"inn": mt.InnA, "role": "JUDGE"},
		map[string]any{"inn": mt.InnA, "date_from": "01.10.2023"},
		map[string]any{"inn": mt.InnA, "size": 101},
		map[string]any{"inn": mt.InnA, "page": -1},
	} {
		w := mt.PostJSON(t, router, "/kad/v1/cases/search", b)
		if code := mt.ErrorCode(t, w, http.StatusBadRequest); code != mockkit.CodeBadRequest && code != mockkit.CodeInvalidINN {
			t.Errorf("%v: code %s", b, code)
		}
	}
}

func TestSeedSummaries(t *testing.T) {
	asOf, _ := time.Parse(time.DateOnly, mockkit.AsOf)
	since := asOf.AddDate(0, 0, -365).Format(time.DateOnly)
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
			recent := it.RegisteredAt >= since
			switch it.Role {
			case "DEFENDANT", "DEBTOR":
				if recent {
					want.DefCount12m++
					want.DefAmount12m += it.ClaimAmount
				}
				if it.Status == "PENDING" {
					want.DefActive += it.ClaimAmount
				}
				if it.CaseType == "BANKRUPTCY" && it.Role == "DEBTOR" && it.Status == "PENDING" {
					want.BankruptDebtor = true
				}
			case "PLAINTIFF":
				if recent {
					want.PlfCount12m++
					want.PlfAmount12m += it.ClaimAmount
				}
			}
		}
		if r.Summary != want {
			t.Errorf("%s: summary %+v, want %+v", f, r.Summary, want)
		}
	}
}
