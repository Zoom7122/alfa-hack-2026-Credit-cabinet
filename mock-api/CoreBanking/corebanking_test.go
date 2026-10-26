package corebanking_test

import (
	"encoding/json"
	"net/http"
	"testing"

	corebanking "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/CoreBanking"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

const base = "/core-banking/v1/clients/"

var router = mt.Router("/core-banking/v1", corebanking.Register)

var endpoints = []string{"", "/turnover", "/transactions", "/loans", "/restrictions"}

type txPage struct {
	Total int `json:"total"`
	Items []struct {
		ID        string `json:"id"`
		Date      string `json:"date"`
		Operation string `json:"operation_code"`
	} `json:"items"`
}

type turnover struct {
	PeriodFrom string `json:"period_from"`
	PeriodTo   string `json:"period_to"`
	Months     []struct {
		Month string `json:"month"`
	} `json:"months"`
}

func TestAllScenarios(t *testing.T) {
	for _, inn := range mt.Companies {
		for _, ep := range endpoints {
			w := mt.Get(router, base+inn+ep)
			if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
				t.Errorf("%s%s: status %d, body %.200s", inn, ep, w.Code, w.Body)
			}
		}
		cl := mt.Decode[struct {
			INN string `json:"inn"`
		}](t, mt.Get(router, base+inn), http.StatusOK)
		if cl.INN != inn {
			t.Errorf("client inn = %s, want %s", cl.INN, inn)
		}
	}
}

func TestCabinetCompaniesHaveOwnIdentityAndFinancialHistory(t *testing.T) {
	for _, inn := range []string{"7701234567", "7724567890", "7709876501", "2901122334", "5001928374", "7811223344", "771234567890", "780198765432"} {
		cl := mt.Decode[struct {
			INN string `json:"inn"`
		}](t, mt.Get(router, base+inn), http.StatusOK)
		if cl.INN != inn {
			t.Fatalf("cabinet identity: got %q want %q", cl.INN, inn)
		}
		history := mt.Decode[turnover](t, mt.Get(router, base+inn+"/turnover"), http.StatusOK)
		if len(history.Months) == 0 {
			t.Fatalf("cabinet company %s has no financial history", inn)
		}
	}
}

func TestUnknownAndInvalidINN(t *testing.T) {
	for _, ep := range endpoints {
		if code := mt.ErrorCode(t, mt.Get(router, base+mt.Unknown+ep), http.StatusNotFound); code != mockkit.CodeNotFound {
			t.Errorf("%s: code %s", ep, code)
		}
		if code := mt.ErrorCode(t, mt.Get(router, base+"123"+ep), http.StatusBadRequest); code != mockkit.CodeInvalidINN {
			t.Errorf("%s: code %s", ep, code)
		}
	}
}

func TestTurnover(t *testing.T) {
	full := mt.Decode[turnover](t, mt.Get(router, base+mt.InnA+"/turnover"), http.StatusOK)
	if len(full.Months) != 12 || full.PeriodFrom != "2025-10" || full.PeriodTo != "2026-09" {
		t.Fatalf("A default: %d months %s..%s", len(full.Months), full.PeriodFrom, full.PeriodTo)
	}
	last3 := mt.Decode[turnover](t, mt.Get(router, base+mt.InnA+"/turnover?months=3"), http.StatusOK)
	if len(last3.Months) != 3 || last3.PeriodFrom != "2026-07" || last3.Months[0].Month != "2026-09" {
		t.Fatalf("A months=3: %+v", last3)
	}
	// ИП открыл счёт 2 месяца назад: больше двух месяцев не будет
	d := mt.Decode[turnover](t, mt.Get(router, base+mt.InnD+"/turnover?months=12"), http.StatusOK)
	if len(d.Months) != 2 || d.PeriodFrom != "2026-08" || d.PeriodTo != "2026-09" {
		t.Fatalf("D: %+v", d)
	}
	for _, q := range []string{"0", "13", "x"} {
		mt.ErrorCode(t, mt.Get(router, base+mt.InnA+"/turnover?months="+q), http.StatusBadRequest)
	}
}

func TestTransactions(t *testing.T) {
	sep := mt.Decode[txPage](t, mt.Get(router, base+mt.InnA+"/transactions?from=2026-09-01&to=2026-09-30&size=500"),
		http.StatusOK)
	if sep.Total == 0 || sep.Total != len(sep.Items) {
		t.Fatalf("total %d, items %d", sep.Total, len(sep.Items))
	}
	ids := map[string]bool{}
	for i, it := range sep.Items {
		if it.Date < "2026-09-01" || it.Date > "2026-09-30" {
			t.Errorf("date %s out of range", it.Date)
		}
		if i > 0 && it.Date > sep.Items[i-1].Date {
			t.Errorf("not sorted newest first at %d", i)
		}
		ids[it.ID] = true
	}
	// примеры из docs/mock-apis.md
	for _, id := range []string{"TX-20260915-000123", "TX-20260925-000311", "TX-20260912-000207",
		"TX-20260905-000118", "TX-20260910-000176", "TX-20260903-000044", "TX-20260920-000255"} {
		if !ids[id] {
			t.Errorf("doc example %s not found", id)
		}
	}

	all := mt.Decode[txPage](t, mt.Get(router, base+mt.InnA+"/transactions?size=500"), http.StatusOK)
	p2 := mt.Decode[txPage](t, mt.Get(router, base+mt.InnA+"/transactions?size=10&page=2"), http.StatusOK)
	if p2.Total != all.Total || len(p2.Items) != 10 || p2.Items[0].ID != all.Items[10].ID {
		t.Fatalf("page 2 mismatch: total %d/%d", p2.Total, all.Total)
	}
	past := mt.Decode[txPage](t, mt.Get(router, base+mt.InnA+"/transactions?page=1000"), http.StatusOK)
	if past.Items == nil || len(past.Items) != 0 {
		t.Fatalf("page beyond end: %+v", past.Items)
	}

	for _, q := range []string{"size=0", "size=501", "page=0", "from=2026-13-01", "from=2026-09-30&to=2026-09-01"} {
		mt.ErrorCode(t, mt.Get(router, base+mt.InnA+"/transactions?"+q), http.StatusBadRequest)
	}
}

// У C есть принудительные списания (инкассовые поручения, код 06), в картотеке — неоплаченное.
func TestScenarioC(t *testing.T) {
	all := mt.Decode[txPage](t, mt.Get(router, base+mt.InnC+"/transactions?size=500"), http.StatusOK)
	incasso := 0
	for _, it := range all.Items {
		if it.Operation == "06" {
			incasso++
		}
	}
	if incasso == 0 {
		t.Error("C: no operation_code 06 in transactions")
	}
	r := mt.Decode[struct {
		CardFile struct {
			HasUnpaidClaims bool `json:"has_unpaid_claims"`
		} `json:"card_file"`
		AccountBlocks []any `json:"account_blocks"`
		Arrests       []any `json:"arrests"`
	}](t, mt.Get(router, base+mt.InnC+"/restrictions"), http.StatusOK)
	if !r.CardFile.HasUnpaidClaims || len(r.AccountBlocks) == 0 || len(r.Arrests) == 0 {
		t.Fatalf("C restrictions: %+v", r)
	}
}
