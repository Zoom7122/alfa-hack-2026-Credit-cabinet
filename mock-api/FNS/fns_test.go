package fns_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	fns "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/FNS"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/fns", fns.Register, mockkit.Middleware(time.Second, fns.Captcha))

type egrulRow struct {
	I string `json:"i"`
	O string `json:"o"`
	K string `json:"k"`
	E string `json:"e"`
}

func egrul(t *testing.T, query string) []egrulRow {
	t.Helper()
	tok := mt.Decode[struct {
		T               string `json:"t"`
		CaptchaRequired bool   `json:"captchaRequired"`
	}](t, mt.PostForm(router, "/fns/egrul/", url.Values{"query": {query}}), http.StatusOK)
	if tok.T == "" || tok.CaptchaRequired {
		t.Fatalf("token: %+v", tok)
	}
	return mt.Decode[struct {
		Rows []egrulRow `json:"rows"`
	}](t, mt.Get(router, "/fns/egrul/search-result/"+tok.T), http.StatusOK).Rows
}

func TestEGRUL(t *testing.T) {
	for _, inn := range mt.Companies {
		rows := egrul(t, inn)
		if len(rows) != 1 || rows[0].I != inn || rows[0].E != "" {
			t.Fatalf("%s: %+v", inn, rows)
		}
		wantKind := "ul"
		if inn == mt.InnD {
			wantKind = "fl"
		}
		if rows[0].K != wantKind {
			t.Errorf("%s: k = %s, want %s", inn, rows[0].K, wantKind)
		}
	}
	if rows := egrul(t, mt.Unknown); rows == nil || len(rows) != 0 {
		t.Fatalf("unknown: %+v", rows)
	}
	mt.ErrorCode(t, mt.PostForm(router, "/fns/egrul/", url.Values{}), http.StatusBadRequest)
}

func TestCabinetRegistryIdentity(t *testing.T) {
	for _, inn := range []string{"7701234567", "7724567890", "7709876501", "2901122334", "5001928374", "7811223344", "771234567890", "780198765432"} {
		rows := egrul(t, inn)
		if len(rows) != 1 || rows[0].I != inn {
			t.Fatalf("cabinet registry %s: %+v", inn, rows)
		}
	}
}

type pbSearch struct {
	UL *struct {
		RowCount int `json:"rowCount"`
		Data     []struct {
			Token string `json:"token"`
		} `json:"data"`
	} `json:"ul"`
	IP *struct {
		RowCount int `json:"rowCount"`
		Data     []struct {
			Token string `json:"token"`
		} `json:"data"`
	} `json:"ip"`
}

func TestTransparentBusiness(t *testing.T) {
	for _, inn := range mt.Companies {
		s := mt.Decode[pbSearch](t, mt.PostForm(router, "/fns/pb/search-proc.json", url.Values{"query": {inn}}),
			http.StatusOK)
		sec := s.UL
		if inn == mt.InnD {
			sec = s.IP
		}
		if sec == nil || sec.RowCount != 1 {
			t.Fatalf("%s: search %+v", inn, s)
		}
		token := sec.Data[0].Token

		req := mt.Decode[struct {
			Token string `json:"token"`
			ID    string `json:"id"`
		}](t, mt.PostForm(router, "/fns/pb/company-proc.json",
			url.Values{"method": {"get-request"}, "token": {token}}), http.StatusOK)

		card := mt.Decode[struct {
			Vyp struct {
				INN  string `json:"ИНН"`
				PrZd int    `json:"pr_zd"`
				Sul  int    `json:"sulst_ex"`
			} `json:"vyp"`
			Liquidated bool `json:"liquidated"`
		}](t, mt.PostForm(router, "/fns/pb/company-proc.json",
			url.Values{"method": {"get-response"}, "token": {req.Token}, "id": {req.ID}}), http.StatusOK)
		if card.Vyp.INN != inn || card.Vyp.Sul != 10 || card.Liquidated {
			t.Errorf("%s: card %+v", inn, card)
		}
		if wantDebt := inn == mt.InnC; (card.Vyp.PrZd == 1) != wantDebt {
			t.Errorf("%s: pr_zd = %d", inn, card.Vyp.PrZd)
		}
	}

	empty := mt.Decode[pbSearch](t, mt.PostForm(router, "/fns/pb/search-proc.json", url.Values{"query": {mt.Unknown}}),
		http.StatusOK)
	if empty.UL == nil || empty.UL.RowCount != 0 {
		t.Fatalf("unknown: %+v", empty)
	}
	mt.ErrorCode(t, mt.PostForm(router, "/fns/pb/company-proc.json",
		url.Values{"method": {"get-request"}, "token": {"MOCK-PB-" + mt.Unknown}}), http.StatusNotFound)
	mt.ErrorCode(t, mt.PostForm(router, "/fns/pb/company-proc.json",
		url.Values{"method": {"get-response"}, "token": {"MOCK-PB-" + mt.InnA}, "id": {"other"}}), http.StatusNotFound)
	mt.ErrorCode(t, mt.PostForm(router, "/fns/pb/company-proc.json", url.Values{"method": {"x"}}), http.StatusBadRequest)
}

func TestAccountingReports(t *testing.T) {
	type search struct {
		Content []struct {
			ID  int64  `json:"id"`
			INN string `json:"inn"`
		} `json:"content"`
		TotalElements int `json:"totalElements"`
	}
	for _, inn := range mt.Companies {
		s := mt.Decode[search](t, mt.Get(router, "/fns/bo/advanced-search/organizations/search?query="+inn), http.StatusOK)
		if inn == mt.InnD { // ИП не сдают бухотчётность
			if s.TotalElements != 0 || len(s.Content) != 0 {
				t.Errorf("D: %+v", s)
			}
			continue
		}
		if s.TotalElements != 1 || s.Content[0].INN != inn {
			t.Fatalf("%s: %+v", inn, s)
		}
		reports := mt.Decode[[]struct {
			Period  string `json:"period"`
			Actives int    `json:"actives"`
		}](t, mt.Get(router, "/fns/bo/nbo/organizations/"+strconv.FormatInt(s.Content[0].ID, 10)+"/bfo/"),
			http.StatusOK)
		if len(reports) == 0 || reports[0].Period != "2025" {
			t.Errorf("%s: reports %+v", inn, reports)
		}
	}
	mt.ErrorCode(t, mt.Get(router, "/fns/bo/nbo/organizations/1/bfo/"), http.StatusNotFound)
	mt.ErrorCode(t, mt.Get(router, "/fns/bo/nbo/organizations/x/bfo/"), http.StatusBadRequest)
}

func TestAccountBlocks(t *testing.T) {
	type blocks struct {
		INN       string `json:"inn"`
		Decisions []struct {
			Number string `json:"number"`
		} `json:"decisions"`
	}
	get := func(inn, bik string) blocks {
		return mt.Decode[blocks](t, mt.PostForm(router, "/fns/bi/bi2-proc.json", url.Values{"inn": {inn}, "bik": {bik}}),
			http.StatusOK)
	}
	if b := get(mt.InnC, "044525999"); len(b.Decisions) != 1 || b.Decisions[0].Number != "12873" {
		t.Fatalf("C: %+v", b)
	}
	for _, c := range [][2]string{{mt.InnC, "044525225"}, {mt.InnA, "044525999"}, {mt.Unknown, "044525999"}} {
		if b := get(c[0], c[1]); b.Decisions == nil || len(b.Decisions) != 0 {
			t.Errorf("%v: %+v", c, b)
		}
	}
	mt.ErrorCode(t, mt.PostForm(router, "/fns/bi/bi2-proc.json", url.Values{"inn": {"1"}, "bik": {"044525999"}}),
		http.StatusBadRequest)
	mt.ErrorCode(t, mt.PostForm(router, "/fns/bi/bi2-proc.json", url.Values{"inn": {mt.InnA}, "bik": {"1"}}),
		http.StatusBadRequest)
}

// Капча у ФНС — как у реального egrul: 200 и captchaRequired=true.
func TestCaptcha(t *testing.T) {
	r := mt.Decode[struct {
		CaptchaRequired bool `json:"captchaRequired"`
	}](t, mt.PostForm(router, "/fns/egrul/", url.Values{"query": {mt.InnA}}, "X-Mock-Fault", "captcha"), http.StatusOK)
	if !r.CaptchaRequired {
		t.Fatal("captchaRequired = false")
	}
}

func TestCharter(t *testing.T) {
	type charter struct {
		INN  string `json:"inn"`
		Head struct {
			Restrictions []struct {
				Type   string  `json:"type"`
				Amount float64 `json:"amount"`
			} `json:"restrictions"`
		} `json:"head"`
		Major struct {
			Pct       float64 `json:"threshold_pct_of_assets"`
			Assets    float64 `json:"assets_at_last_reporting_date"`
			Threshold float64 `json:"threshold_amount"`
		} `json:"major_transaction"`
		Participants []struct {
			SharePct float64 `json:"share_pct"`
		} `json:"participants"`
	}
	for _, inn := range []string{mt.InnA, mt.InnB, mt.InnC, mt.InnE} {
		ch := mt.Decode[charter](t, mt.Get(router, "/fns/puchdoc/v1/"+inn), http.StatusOK)
		var shares float64
		for _, p := range ch.Participants {
			shares += p.SharePct
		}
		if ch.INN != inn || shares != 100 || ch.Major.Threshold != ch.Major.Assets*ch.Major.Pct/100 {
			t.Errorf("%s: %+v", inn, ch)
		}
	}
	if a := mt.Decode[charter](t, mt.Get(router, "/fns/puchdoc/v1/"+mt.InnA), http.StatusOK); a.Major.Threshold != 5357500 {
		t.Errorf("A threshold %.2f, doc says 5 357 500", a.Major.Threshold)
	}
	if e := mt.Decode[charter](t, mt.Get(router, "/fns/puchdoc/v1/"+mt.InnE), http.StatusOK); len(e.Head.Restrictions) != 1 ||
		e.Head.Restrictions[0].Type != "LOAN_LIMIT" {
		t.Errorf("E restrictions: %+v", e.Head.Restrictions)
	}
	mt.ErrorCode(t, mt.Get(router, "/fns/puchdoc/v1/"+mt.InnD), http.StatusNotFound) // у ИП устава нет
	mt.ErrorCode(t, mt.Get(router, "/fns/puchdoc/v1/"+mt.Unknown), http.StatusNotFound)
}

func TestRMSPAndDisqualified(t *testing.T) {
	type rmsp struct {
		InRegistry bool    `json:"in_registry"`
		Category   *string `json:"category"`
	}
	want := map[string]string{mt.InnA: "MICRO", mt.InnB: "SMALL", mt.InnC: "MICRO", mt.InnD: "MICRO", mt.InnE: "SMALL"}
	for inn, cat := range want {
		r := mt.Decode[rmsp](t, mt.Get(router, "/fns/rmsp/v1/"+inn), http.StatusOK)
		if !r.InRegistry || r.Category == nil || *r.Category != cat {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	if u := mt.Decode[rmsp](t, mt.Get(router, "/fns/rmsp/v1/"+mt.Unknown), http.StatusOK); u.InRegistry || u.Category != nil {
		t.Errorf("unknown: %+v", u)
	}

	type disq struct {
		Listed  bool  `json:"listed"`
		Entries []any `json:"entries"`
	}
	for _, inn := range append(append([]string{}, mt.Directors...), mt.Unknown+"00") {
		if r := mt.Decode[disq](t, mt.Get(router, "/fns/disqualified/v1?inn="+inn), http.StatusOK); r.Listed || r.Entries == nil {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	if r := mt.Decode[disq](t, mt.Get(router, "/fns/disqualified/v1?inn="+mt.Laptev), http.StatusOK); !r.Listed || len(r.Entries) != 1 {
		t.Errorf("Laptev: %+v", r)
	}
	mt.ErrorCode(t, mt.Get(router, "/fns/disqualified/v1?inn="+mt.InnA), http.StatusBadRequest) // нужен ИНН физлица
}
