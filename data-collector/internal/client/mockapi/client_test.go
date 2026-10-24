package mockapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// fakeMock отвечает на многошаговые запросы как mock-api, на остальные — {"path": …}.
// failPrefix — пути, на которые отвечает 404, как на неизвестный ИНН.
type fakeMock struct {
	failPrefix string

	mu       sync.Mutex
	requests []*http.Request
}

func (f *fakeMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.ParseForm()
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()

	reply := func(status int, v string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, v)
	}
	p := r.URL.Path
	if f.failPrefix != "" && strings.HasPrefix(p, f.failPrefix) {
		reply(http.StatusNotFound, `{"error":{"code":"NOT_FOUND","message":"client not found"}}`)
		return
	}
	switch {
	case p == "/fns/egrul/":
		reply(200, `{"t":"T1","captchaRequired":false}`)
	case p == "/fns/egrul/search-result/T1":
		reply(200, `{"rows":[{"c":"ООО \"СЕВЕРНЫЙ ВЕТЕР\"","i":"7799000012"}]}`)
	case p == "/fns/pb/search-proc.json":
		reply(200, `{"ul":{"rowCount":1,"data":[{"token":"PB1"}]}}`)
	case p == "/fns/pb/company-proc.json" && r.PostForm.Get("method") == "get-request":
		reply(200, `{"token":"PB1","id":"PB1"}`)
	case p == "/fns/pb/company-proc.json":
		reply(200, `{"vyp":{"ИНН":"7799000012"}}`)
	case p == "/fns/bo/advanced-search/organizations/search":
		reply(200, `{"content":[{"id":42}],"totalElements":1}`)
	case strings.HasSuffix(p, "/transactions") && strings.HasPrefix(p, "/core-banking/"):
		reply(200, `{"page":`+r.URL.Query().Get("page")+`,"size":500,"total":700,"items":[]}`)
	case p == "/kad/v1/cases/search":
		reply(200, `{"total":1,"items":[]}`)
	case p == "/tax-reports/v1/requests":
		reply(http.StatusAccepted, `{"request_id":"TXR-1","status":"PROCESSING"}`)
	case p == "/gosuslugi/v1/profile-requests":
		reply(http.StatusAccepted, `{"request_id":"DP-1","status":"WAITING_USER_CONSENT"}`)
	case p == "/open-banking/v1/account-consents":
		reply(http.StatusCreated, `{"Data":{"consentId":"aac-1","status":"AwaitingAuthorisation"}}`)
	case p == "/open-banking/v1/accounts":
		reply(200, `{"Data":{"Account":[{"accountId":"acc-1"},{"accountId":"acc-2"}]}}`)
	case p == "/cbr/DailyInfoWebServ/DailyInfo.asmx":
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<soap:Envelope><KR><Rate>15.00</Rate></KR></soap:Envelope>`)
	case p == "/hh/employers":
		reply(200, `{"found":2,"items":[{"id":"9","name":"СЕВЕРНЫЙ ВЕТЕР ГРУПП"},{"id":"7","name":"Северный ветер"}]}`)
	default:
		reply(200, `{"path":"`+p+`"}`)
	}
}

func (f *fakeMock) find(path string) []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*http.Request
	for _, r := range f.requests {
		if r.URL.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func newTestClient(t *testing.T, f *fakeMock) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := New(srv.URL, 2*time.Second, "044525999")
	c.now = func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }
	return c
}

func fullInput() domain.CollectInput {
	consent := &domain.Consent{ID: "CNS-1", ValidUntil: "2027-04-21"}
	return domain.CollectInput{
		INN: "7799000012",
		Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", MiddleName: "Сергеевич",
			BirthDate: "1984-07-19"},
		BKICompanyConsent: consent,
		BKIPersonConsent:  consent,
		TaxConsent:        &domain.Consent{ID: "TAX-1", ValidUntil: "2027-04-21", Scope: []string{domain.TaxScopeENSBalance}},
		OtherBankBIKs:     []string{"044525225"},
		MarketplaceTokens: map[string]string{"wildberries": "secret-wb"},
		OFDToken:          "secret-ofd",
		Collateral: domain.Collateral{CadastralNumbers: []string{"50:21:0110203:845"}, VINs: []string{"XTC652005N1498213"},
			Serials: []string{"JCB3CX2023118457"}},
		POANumber:         "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
		RepresentativeINN: "779900010333",
	}
}

func bySystem(results []domain.SystemResult) map[domain.System]domain.SystemResult {
	m := map[domain.System]domain.SystemResult{}
	for _, r := range results {
		m[r.System] = r
	}
	return m
}

func decode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("data is not a JSON object: %v: %s", err, raw)
	}
	return m
}

func TestCollectAllFullInput(t *testing.T) {
	f := &fakeMock{}
	results := newTestClient(t, f).CollectAll(context.Background(), "col_test", fullInput())

	if len(results) != len(domain.Systems) {
		t.Fatalf("%d results, want %d", len(results), len(domain.Systems))
	}
	for i, res := range results {
		if res.System != domain.Systems[i] {
			t.Fatalf("result %d is %s, want %s (order of domain.Systems)", i, res.System, domain.Systems[i])
		}
		if res.State != domain.CollectDone || len(res.Data) == 0 {
			t.Errorf("%s: state %s, reason %q", res.System, res.State, res.Reason)
		}
	}
	r := bySystem(results)

	fns := decode(t, r[domain.SystemFNS].Data)
	for _, key := range []string{"egrul", "pb_search", "pb", "bo_search", "bo_bfo", "charter", "rmsp", "disqualified"} {
		if fns[key] == nil {
			t.Errorf("fns.%s is missing", key)
		}
	}
	if blocks, _ := fns["account_blocks"].(map[string]any); len(blocks) != 2 || blocks["044525999"] == nil || blocks["044525225"] == nil {
		t.Errorf("fns.account_blocks = %v, want own and other bank", fns["account_blocks"])
	}

	// транзакции: total 700 при size 500 → две страницы
	if pages, _ := decode(t, r[domain.SystemCoreBanking].Data)["transactions"].([]any); len(pages) != 2 {
		t.Errorf("core_banking.transactions = %v, want 2 pages", pages)
	}

	ob := decode(t, r[domain.SystemOpenBanking].Data)["044525225"].(map[string]any)
	for _, key := range []string{"balances", "transactions"} {
		if accs, _ := ob[key].(map[string]any); len(accs) != 2 {
			t.Errorf("open_banking.044525225.%s = %v, want both accounts", key, ob[key])
		}
	}

	if xml, _ := decode(t, r[domain.SystemCBR].Data)["key_rate"].(string); !strings.HasPrefix(xml, "<soap:Envelope>") {
		t.Errorf("cbr.key_rate = %q, want raw XML string", xml)
	}

	// hh: поиск по названию из ЕГРЮЛ, вакансии — у точного совпадения, не у тёзки
	if emp := f.find("/hh/employers"); len(emp) != 1 || emp[0].URL.Query().Get("text") != "СЕВЕРНЫЙ ВЕТЕР" {
		t.Errorf("hh search: %v", emp)
	}
	if vac := f.find("/hh/vacancies"); len(vac) != 1 || vac[0].URL.Query().Get("employer_id") != "7" {
		t.Errorf("hh vacancies: %v", vac)
	}

	// налоговая отчётность: только документы из прав согласия
	tax := f.find("/tax-reports/v1/requests")[0]
	var taxBody struct {
		Documents []string `json:"documents"`
		Periods   []string `json:"periods"`
	}
	_ = json.NewDecoder(tax.Body).Decode(&taxBody)
	if strings.Join(taxBody.Documents, ",") != "ENS_BALANCE" || strings.Join(taxBody.Periods, ",") != "2024,2025,2026" {
		t.Errorf("tax request = %+v", taxBody)
	}
}

// Токены уходят только в заголовках: в журнале запросов (и в БД) их нет.
func TestTokensOnlyInHeaders(t *testing.T) {
	f := &fakeMock{}
	results := newTestClient(t, f).CollectAll(context.Background(), "col_test", fullInput())

	for _, res := range results {
		for _, call := range res.Calls {
			if strings.Contains(call.Path, "secret") {
				t.Errorf("%s: token in path %s", res.System, call.Path)
			}
		}
	}
	if got := f.find("/marketplaces/v1/wildberries/sellers/7799000012")[0].Header.Get("X-Seller-Token"); got != "secret-wb" {
		t.Errorf("X-Seller-Token = %q", got)
	}
	if got := f.find("/ofd/v1/organizations/7799000012/kkt")[0].Header.Get("X-OFD-Token"); got != "secret-ofd" {
		t.Errorf("X-OFD-Token = %q", got)
	}
	for _, r := range f.requests {
		if r.Header.Get("X-Request-Id") != "col_test" {
			t.Fatalf("%s %s without X-Request-Id", r.Method, r.URL.Path)
		}
	}
}

func TestMinimalInputSkips(t *testing.T) {
	results := bySystem(newTestClient(t, &fakeMock{}).CollectAll(context.Background(), "col_test",
		domain.CollectInput{INN: "7799000012"}))

	for _, sys := range []domain.System{domain.SystemBKI, domain.SystemTaxReports, domain.SystemEGRN, domain.SystemGosuslugi,
		domain.SystemOpenBanking, domain.SystemMarketplaces, domain.SystemOFD} {
		res := results[sys]
		if res.State != domain.CollectSkipped || res.Reason == "" || len(res.Calls) != 0 || res.Data != nil {
			t.Errorf("%s: %+v", sys, res)
		}
	}
	// без руководителя данные физлица пропускаются, но компания собирается
	fssp := results[domain.SystemFSSP]
	if fssp.State != domain.CollectDone || !strings.Contains(fssp.Reason, "руководител") || len(fssp.Calls) != 1 {
		t.Errorf("fssp: %+v", fssp)
	}
}

func TestFailedAndPartial(t *testing.T) {
	// у АБС клиента нет: все 5 запросов 404 → FAILED, данных нет
	results := bySystem(newTestClient(t, &fakeMock{failPrefix: "/core-banking/"}).CollectAll(context.Background(), "col_test",
		domain.CollectInput{INN: "7799000012"}))
	cb := results[domain.SystemCoreBanking]
	if cb.State != domain.CollectFailed || cb.Data != nil || cb.FailedCalls() != 5 ||
		!strings.Contains(cb.Reason, "client: 404 NOT_FOUND: client not found") {
		t.Fatalf("core banking: %+v", cb)
	}
	if cb.Calls[0].Status != http.StatusNotFound {
		t.Errorf("status = %d", cb.Calls[0].Status)
	}

	// у ФНС не отвечает только устав → PARTIAL, остальное в данных, на месте устава null
	results = bySystem(newTestClient(t, &fakeMock{failPrefix: "/fns/puchdoc/"}).CollectAll(context.Background(), "col_test",
		domain.CollectInput{INN: "7799000012"}))
	fns := results[domain.SystemFNS]
	if fns.State != domain.CollectPartial || !strings.Contains(fns.Reason, "charter: 404") {
		t.Fatalf("fns: %+v", fns)
	}
	data := decode(t, fns.Data)
	if v, ok := data["charter"]; !ok || v != nil || data["egrul"] == nil {
		t.Errorf("fns data: charter=%v (present %v), egrul=%v", v, ok, data["egrul"])
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	res := New(srv.URL, time.Second, "044525999").CollectAll(context.Background(), "col_test", domain.CollectInput{INN: "7799000012"})

	for _, r := range res {
		if r.State != domain.CollectFailed && r.State != domain.CollectSkipped {
			t.Errorf("%s: state %s", r.System, r.State)
		}
	}
	cb := bySystem(res)[domain.SystemCoreBanking]
	if cb.Calls[0].Status != 0 || cb.Calls[0].Error == "" {
		t.Errorf("call = %+v", cb.Calls[0])
	}
	// без ЕГРЮЛ нет названия — hh пропускается
	if hh := bySystem(res)[domain.SystemHH]; hh.State != domain.CollectSkipped {
		t.Errorf("hh: %+v", hh)
	}
}

func TestCompanyName(t *testing.T) {
	cases := map[string]string{
		`{"egrul":{"rows":[{"c":"ООО \"СЕВЕРНЫЙ ВЕТЕР\"","n":"ОБЩЕСТВО …"}]}}`:                 "СЕВЕРНЫЙ ВЕТЕР",
		`{"egrul":{"rows":[{"c":"ООО «ТЕХНО-СЕРВИС»"}]}}`:                                      "ТЕХНО-СЕРВИС",
		`{"egrul":{"rows":[{"n":"Индивидуальный предприниматель Смирнова Анна Викторовна"}]}}`: "Смирнова Анна Викторовна",
		`{"egrul":{"rows":[]}}`: "",
		`{}`:                    "",
	}
	for in, want := range cases {
		if got := companyName(json.RawMessage(in)); got != want {
			t.Errorf("companyName(%s) = %q, want %q", in, got, want)
		}
	}
	if got := companyName(nil); got != "" {
		t.Errorf("companyName(nil) = %q", got)
	}
}

func TestCompanyCard(t *testing.T) {
	var c Client
	card, ok := c.CompanyCard(json.RawMessage(`{"egrul":{"rows":[{"c":"ООО \"СЕВЕРНЫЙ ВЕТЕР\"",
		"n":"ОБЩЕСТВО С ОГРАНИЧЕННОЙ ОТВЕТСТВЕННОСТЬЮ \"СЕВЕРНЫЙ ВЕТЕР\"","i":"7799000012","o":"1217700412345",
		"p":"779901001","r":"14.03.2021","g":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич","k":"ul","rn":"Г.Москва"}]}}`))
	want := domain.CompanyCard{INN: "7799000012", Type: domain.CompanyLegalEntity, OGRN: "1217700412345", KPP: "779901001",
		FullName: `ОБЩЕСТВО С ОГРАНИЧЕННОЙ ОТВЕТСТВЕННОСТЬЮ "СЕВЕРНЫЙ ВЕТЕР"`, ShortName: `ООО "СЕВЕРНЫЙ ВЕТЕР"`,
		RegistrationDate: "2021-03-14", DirectorName: "Ковалёв Игорь Сергеевич", Region: "Г.Москва"}
	if !ok || card != want {
		t.Fatalf("card = %+v (ok %v)", card, ok)
	}

	// ИП: короткого имени и руководителя нет, имя — полное
	card, ok = c.CompanyCard(json.RawMessage(`{"egrul":{"rows":[{"n":"Индивидуальный предприниматель Смирнова Анна Викторовна",
		"i":"779900010202","o":"326774600012345","r":"28.07.2026","k":"fl"}]}}`))
	if !ok || card.Type != domain.CompanySoleProprietor || card.Name() != "Индивидуальный предприниматель Смирнова Анна Викторовна" ||
		card.DirectorName != "" {
		t.Fatalf("ip card = %+v (ok %v)", card, ok)
	}

	for _, raw := range []string{``, `{}`, `{"egrul":{"rows":[]}}`, `{"egrul":null}`} {
		if _, ok := c.CompanyCard(json.RawMessage(raw)); ok {
			t.Errorf("CompanyCard(%q) must be false", raw)
		}
	}
}
