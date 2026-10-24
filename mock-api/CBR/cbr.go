// Package cbr эмулирует SOAP-сервис ЦБ DailyInfoWebServ: метод KeyRate
// (ключевая ставка по дням). Конверт и элементы как у настоящего сервиса
// (упрощённо, без XSD-схемы в diffgram), поэтому переход на живой
// источник — смена base URL.
//
// Данные — seed/key_rate.json: даты изменения ставки.
package cbr

import (
	"embed"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

//go:embed seed
var seedFS embed.FS

type change struct {
	From string `json:"from"`
	Rate string `json:"rate"`
}

type envelope struct {
	Body struct {
		KeyRate *struct {
			FromDate string `xml:"fromDate"`
			ToDate   string `xml:"ToDate"`
		} `xml:"KeyRate"`
	} `xml:"Body"`
}

type handler struct{ changes []change } // по возрастанию даты

// Register вешает эндпоинт на группу (обычно /cbr).
func Register(rg *gin.RouterGroup) {
	body, err := seedFS.ReadFile("seed/key_rate.json")
	if err != nil {
		panic("cbr: missing seed/key_rate.json")
	}
	var s struct {
		Changes []change `json:"changes"`
	}
	mockkit.MustDecode("key_rate.json", body, &s)
	sort.Slice(s.Changes, func(i, j int) bool { return s.Changes[i].From < s.Changes[j].From })
	rg.POST("/DailyInfoWebServ/DailyInfo.asmx", (&handler{s.Changes}).dailyInfo)
}

func (h *handler) dailyInfo(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		fault(c, "soap:Client", "cannot read request")
		return
	}
	var env envelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		fault(c, "soap:Client", "invalid SOAP envelope")
		return
	}
	action := strings.Trim(c.GetHeader("SOAPAction"), `"`)
	if env.Body.KeyRate == nil || (action != "" && action != "http://web.cbr.ru/KeyRate") {
		fault(c, "soap:Client", "only KeyRate is supported by the mock")
		return
	}
	from, err1 := parseDate(env.Body.KeyRate.FromDate)
	to, err2 := parseDate(env.Body.KeyRate.ToDate)
	if err1 != nil || err2 != nil || from.After(to) {
		fault(c, "soap:Client", "fromDate and ToDate must be dates, fromDate <= ToDate")
		return
	}

	var rows strings.Builder
	for d := to; !d.Before(from); d = d.AddDate(0, 0, -1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		rate, ok := h.rateOn(d.Format(time.DateOnly))
		if !ok {
			continue
		}
		fmt.Fprintf(&rows, "<KR><DT>%sT00:00:00+03:00</DT><Rate>%s</Rate></KR>", d.Format(time.DateOnly), rate)
	}
	c.Data(http.StatusOK, "text/xml; charset=utf-8", []byte(xml.Header+
		`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>`+
		`<KeyRateResponse xmlns="http://web.cbr.ru/"><KeyRateResult>`+
		`<diffgr:diffgram xmlns:diffgr="urn:schemas-microsoft-com:xml-diffgram-v1">`+
		`<KeyRate xmlns="">`+rows.String()+`</KeyRate>`+
		`</diffgr:diffgram></KeyRateResult></KeyRateResponse></soap:Body></soap:Envelope>`))
}

// rateOn: ставка, действовавшая в этот день.
func (h *handler) rateOn(day string) (string, bool) {
	i := sort.Search(len(h.changes), func(i int) bool { return h.changes[i].From > day })
	if i == 0 {
		return "", false
	}
	return h.changes[i-1].Rate, true
}

// parseDate понимает "2026-09-01" и "2026-09-01T00:00:00".
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) > 10 {
		s = s[:10]
	}
	return time.Parse(time.DateOnly, s)
}

func fault(c *gin.Context, code, msg string) {
	var esc strings.Builder
	_ = xml.EscapeText(&esc, []byte(msg))
	c.Data(http.StatusInternalServerError, "text/xml; charset=utf-8", []byte(xml.Header+
		`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body><soap:Fault>`+
		`<faultcode>`+code+`</faultcode><faultstring>`+esc.String()+`</faultstring>`+
		`</soap:Fault></soap:Body></soap:Envelope>`))
}
