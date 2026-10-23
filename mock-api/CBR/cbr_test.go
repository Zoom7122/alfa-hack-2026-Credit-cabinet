package cbr_test

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cbr "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/CBR"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/cbr", cbr.Register)

const path = "/cbr/DailyInfoWebServ/DailyInfo.asmx"

func soap(from, to string) string {
	return `<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>` +
		`<KeyRate xmlns="http://web.cbr.ru/"><fromDate>` + from + `</fromDate><ToDate>` + to + `</ToDate></KeyRate>` +
		`</soap:Body></soap:Envelope>`
}

func call(body, action string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	if action != "" {
		req.Header.Set("SOAPAction", action)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

type kr struct {
	DT   string `xml:"DT"`
	Rate string `xml:"Rate"`
}

func rates(t *testing.T, w *httptest.ResponseRecorder) []kr {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var env struct {
		Rows []kr `xml:"Body>KeyRateResponse>KeyRateResult>diffgram>KeyRate>KR"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	return env.Rows
}

func TestKeyRate(t *testing.T) {
	rows := rates(t, call(soap("2026-09-01", "2026-10-22"), `"http://web.cbr.ru/KeyRate"`))
	byDate := map[string]string{}
	for _, r := range rows {
		byDate[r.DT[:10]] = r.Rate
	}
	want := map[string]string{"2026-10-22": "15.00", "2026-10-20": "15.00", "2026-10-19": "15.50", "2026-09-15": "15.50",
		"2026-09-14": "16.00", "2026-09-01": "16.00"}
	for d, rate := range want {
		if byDate[d] != rate {
			t.Errorf("%s: rate %q, want %s", d, byDate[d], rate)
		}
	}
	if _, ok := byDate["2026-10-18"]; ok {
		t.Error("weekends must be skipped")
	}
	if rows[0].DT[:10] != "2026-10-22" {
		t.Errorf("newest first expected, got %s", rows[0].DT)
	}
	// дата с временем, без SOAPAction
	if r := rates(t, call(soap("2026-10-20T00:00:00", "2026-10-20T00:00:00"), "")); len(r) != 1 || r[0].Rate != "15.00" {
		t.Errorf("single day: %+v", r)
	}
}

func TestFaults(t *testing.T) {
	for _, tc := range []struct{ body, action string }{
		{"not xml", ""},
		{soap("2026-10-22", "2026-09-01"), ""},
		{soap("x", "2026-09-01"), ""},
		{soap("2026-09-01", "2026-10-22"), `"http://web.cbr.ru/GetCursOnDate"`},
	} {
		w := call(tc.body, tc.action)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "<soap:Fault>") {
			t.Errorf("%q: status %d", tc.body, w.Code)
		}
	}
}
