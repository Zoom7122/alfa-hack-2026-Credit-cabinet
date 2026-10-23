package signals_test

import (
	"net/http"
	"testing"

	signals "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Signals"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/signals/v1", signals.Register)

type items struct {
	INN   string `json:"inn"`
	Items []struct {
		ID        string `json:"id"`
		Sentiment string `json:"sentiment"`
	} `json:"items"`
}

func get(t *testing.T, path string) items {
	t.Helper()
	return mt.Decode[items](t, mt.Get(router, "/signals/v1/"+path), http.StatusOK)
}

func TestSignals(t *testing.T) {
	counts := map[string][3]int{ // проверки, товарные знаки, новости
		mt.InnA: {0, 1, 1}, mt.InnB: {0, 0, 0}, mt.InnC: {0, 0, 2}, mt.InnD: {0, 0, 0}, mt.InnE: {1, 0, 1}, mt.Unknown: {0, 0, 0},
	}
	for inn, n := range counts {
		for i, kind := range []string{"inspections", "trademarks", "news"} {
			r := get(t, inn+"/"+kind)
			if r.INN != inn || r.Items == nil || len(r.Items) != n[i] {
				t.Errorf("%s %s: %d items, want %d", inn, kind, len(r.Items), n[i])
			}
		}
	}
	c := get(t, mt.InnC+"/news?from=2026-07-01")
	if len(c.Items) != 1 || c.Items[0].ID != "NEWS-8812" || c.Items[0].Sentiment != "NEGATIVE" {
		t.Errorf("C recent news: %+v", c)
	}
	mt.ErrorCode(t, mt.Get(router, "/signals/v1/"+mt.InnC+"/news?from=x"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/signals/v1/12/news"), http.StatusBadRequest)
}
