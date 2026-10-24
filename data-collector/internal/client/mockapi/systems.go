package mockapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Пути и параметры — как в mock-api (docs/mock-api-guide.md). Ключи в put —
// имена полей внутри колонки системы в raw_collections.

var msk = time.FixedZone("MSK", 3*60*60)

// coreBanking — АБС: клиент, обороты за 12 месяцев, все транзакции, наши кредиты, картотека.
func (c *Client) coreBanking(ctx context.Context, r *run, in domain.CollectInput) {
	base := "/core-banking/v1/clients/" + url.PathEscape(in.INN)
	for _, ep := range []struct {
		key, path string
		q         url.Values
	}{
		{"client", base, nil},
		{"turnover", base + "/turnover", url.Values{"months": {"12"}}},
		{"loans", base + "/loans", nil},
		{"restrictions", base + "/restrictions", nil},
	} {
		raw, _ := r.get(ctx, ep.key, ep.path, ep.q, nil)
		r.put(raw, ep.key)
	}
	const size = 500
	r.put(pages(size, func(page int) (json.RawMessage, bool) {
		return r.get(ctx, "transactions", base+"/transactions",
			url.Values{"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(size)}}, nil)
	}), "transactions")
}

// fns — ЕГРЮЛ/ЕГРИП, «Прозрачный бизнес», ГИР БО, блокировки счетов, устав, реестр МСП, дисквалификация.
func (c *Client) fns(ctx context.Context, r *run, in domain.CollectInput) {
	inn := in.INN

	// ЕГРЮЛ: токен поиска → строки выписки
	if raw, ok := r.postForm(ctx, "egrul_search", "/fns/egrul/", url.Values{"query": {inn}}); ok {
		var tok struct {
			T               string `json:"t"`
			CaptchaRequired bool   `json:"captchaRequired"`
		}
		_ = json.Unmarshal(raw, &tok)
		switch {
		case tok.CaptchaRequired:
			r.failLast("ЕГРЮЛ требует капчу")
		case tok.T != "":
			res, _ := r.get(ctx, "egrul", "/fns/egrul/search-result/"+url.PathEscape(tok.T), nil, nil)
			r.put(res, "egrul")
		}
	}

	// «Прозрачный бизнес»: поиск → токен → id запроса → карточка
	if raw, ok := r.postForm(ctx, "pb_search", "/fns/pb/search-proc.json", url.Values{"query": {inn}}); ok {
		r.put(raw, "pb_search")
		if token := pbToken(raw); token == "" {
			r.skip("«Прозрачный бизнес»: компания не найдена")
		} else if req, ok := r.postForm(ctx, "pb_request", "/fns/pb/company-proc.json",
			url.Values{"method": {"get-request"}, "token": {token}}); ok {
			var id struct {
				Token string `json:"token"`
				ID    string `json:"id"`
			}
			_ = json.Unmarshal(req, &id)
			card, _ := r.postForm(ctx, "pb", "/fns/pb/company-proc.json",
				url.Values{"method": {"get-response"}, "token": {id.Token}, "id": {id.ID}})
			r.put(card, "pb")
		}
	}

	// ГИР БО: поиск → бухотчётность по id (у ИП её нет — поиск пустой)
	if raw, ok := r.get(ctx, "bo_search", "/fns/bo/advanced-search/organizations/search", url.Values{"query": {inn}}, nil); ok {
		r.put(raw, "bo_search")
		if id := boID(raw); id != 0 {
			bfo, _ := r.get(ctx, "bo_bfo", fmt.Sprintf("/fns/bo/nbo/organizations/%d/bfo/", id), nil, nil)
			r.put(bfo, "bo_bfo")
		}
	}

	// блокировки по нашему банку и по банкам, где у клиента ещё есть счета
	for _, bik := range uniq(append([]string{c.ownBIK}, in.OtherBankBIKs...)...) {
		raw, _ := r.postForm(ctx, "account_blocks", "/fns/bi/bi2-proc.json", url.Values{"inn": {inn}, "bik": {bik}})
		r.put(raw, "account_blocks", bik)
	}

	if len(inn) == 10 { // у ИП устава нет
		raw, _ := r.get(ctx, "charter", "/fns/puchdoc/v1/"+url.PathEscape(inn), nil, nil)
		r.put(raw, "charter")
	}
	raw, _ := r.get(ctx, "rmsp", "/fns/rmsp/v1/"+url.PathEscape(inn), nil, nil)
	r.put(raw, "rmsp")

	if in.Director == nil {
		r.skip("дисквалификация: нет данных руководителя")
		return
	}
	raw, _ = r.get(ctx, "disqualified", "/fns/disqualified/v1", url.Values{"inn": {in.Director.INN}}, nil)
	r.put(raw, "disqualified")
}

// bki — отчёты бюро по компании и по руководителю; без согласия запрос не отправляется.
func (c *Client) bki(ctx context.Context, r *run, in domain.CollectInput) {
	if in.BKICompanyConsent == nil {
		r.skip("отчёт по компании: нет согласия consents.bki_company")
	} else {
		raw, _ := r.postJSON(ctx, "legal", "/bki/v1/reports", map[string]any{
			"subject_type": "LEGAL", "inn": in.INN, "consent": consentBody(in.BKICompanyConsent),
			"purpose": "LOAN_APPLICATION", "application_id": r.requestID,
		}, nil)
		r.put(raw, "legal")
	}

	switch {
	case in.Director == nil:
		r.skip("отчёт по руководителю: нет данных руководителя")
	case in.BKIPersonConsent == nil:
		r.skip("отчёт по руководителю: нет согласия consents.bki_person")
	default:
		raw, _ := r.postJSON(ctx, "person", "/bki/v1/reports", map[string]any{
			"subject_type": "PERSON", "person": personBody(in.Director), "consent": consentBody(in.BKIPersonConsent),
			"purpose": "LOAN_APPLICATION", "application_id": r.requestID,
		}, nil)
		r.put(raw, "person")
	}
}

// compliance — уровень риска ЗСК и проверка компании и людей по санкционным спискам.
func (c *Client) compliance(ctx context.Context, r *run, in domain.CollectInput) {
	raw, _ := r.get(ctx, "zsk", "/compliance/v1/zsk/"+url.PathEscape(in.INN), nil, nil)
	r.put(raw, "zsk")

	subjects := []map[string]any{{"ref": "company", "type": "LEGAL", "inn": in.INN}}
	if d := in.Director; d != nil {
		subjects = append(subjects, map[string]any{"ref": "director", "type": "PERSON", "inn": d.INN,
			"full_name": d.FullName(), "birth_date": d.BirthDate})
	}
	if in.RepresentativeINN != "" {
		subjects = append(subjects, map[string]any{"ref": "representative", "type": "PERSON", "inn": in.RepresentativeINN})
	}
	raw, _ = r.postJSON(ctx, "screening", "/compliance/v1/screening", map[string]any{
		"lists":    []string{"RFM_TERRORISM_EXTREMISM", "RFM_FREEZE", "OFAC_SDN", "UK", "EU"},
		"subjects": subjects,
	}, nil)
	r.put(raw, "screening")
}

// fssp — исполнительные производства компании (по ИНН) и руководителя (по ФИО и дате рождения).
func (c *Client) fssp(ctx context.Context, r *run, in domain.CollectInput) {
	raw, _ := r.get(ctx, "company", "/fssp/v1/proceedings", url.Values{"inn": {in.INN}}, nil)
	r.put(raw, "company")

	d := in.Director
	if d == nil {
		r.skip("производства руководителя: нет данных руководителя")
		return
	}
	q := url.Values{"last_name": {d.LastName}, "first_name": {d.FirstName}, "birth_date": {d.BirthDate}}
	if d.MiddleName != "" {
		q.Set("middle_name", d.MiddleName)
	}
	raw, _ = r.get(ctx, "director", "/fssp/v1/proceedings", q, nil)
	r.put(raw, "director")
}

// kad — все арбитражные дела компании, постранично.
func (c *Client) kad(ctx context.Context, r *run, in domain.CollectInput) {
	const size = 100
	r.put(pages(size, func(page int) (json.RawMessage, bool) {
		return r.postJSON(ctx, "cases", "/kad/v1/cases/search",
			map[string]any{"inn": in.INN, "role": "ANY", "page": page, "size": size}, nil)
	}), "cases")
}

// fedresurs — сообщения о компании и банкротство руководителя как физлица.
func (c *Client) fedresurs(ctx context.Context, r *run, in domain.CollectInput) {
	raw, _ := r.get(ctx, "company_messages", "/fedresurs/v1/companies/"+url.PathEscape(in.INN)+"/messages", nil, nil)
	r.put(raw, "company_messages")

	if in.Director == nil {
		r.skip("банкротство руководителя: нет данных руководителя")
		return
	}
	raw, _ = r.get(ctx, "director_bankruptcy", "/fedresurs/v1/persons/"+url.PathEscape(in.Director.INN)+"/bankruptcy", nil, nil)
	r.put(raw, "director_bankruptcy")
}

// m4d — доверенности компании (на представителя, если он указан) и сама МЧД по номеру.
func (c *Client) m4d(ctx context.Context, r *run, in domain.CollectInput) {
	q := url.Values{"principal_inn": {in.INN}}
	if in.RepresentativeINN != "" {
		q.Set("representative_inn", in.RepresentativeINN)
	}
	raw, _ := r.get(ctx, "search", "/m4d/v1/poa", q, nil)
	r.put(raw, "search")

	if in.POANumber == "" {
		r.skip("МЧД по номеру: номер не передан (заявку подаёт руководитель)")
		return
	}
	raw, _ = r.get(ctx, "poa", "/m4d/v1/poa/"+url.PathEscape(in.POANumber), nil, nil)
	r.put(raw, "poa")
}

// taxReports — налоговая отчётность по согласию: создать запрос → забрать результат.
// Запрашиваются только документы, на которые есть право в согласии.
func (c *Client) taxReports(ctx context.Context, r *run, in domain.CollectInput) {
	cs := in.TaxConsent
	var docs []string
	if slices.Contains(cs.Scope, domain.TaxScopeDeclarations) {
		docs = append(docs, "PROFIT", "VAT", "USN", "NPD_INCOME")
	}
	if slices.Contains(cs.Scope, domain.TaxScopeENSBalance) {
		docs = append(docs, "ENS_BALANCE")
	}
	year := c.now().Year()
	created, ok := r.postJSON(ctx, "request", "/tax-reports/v1/requests", map[string]any{
		"inn": in.INN, "application_id": r.requestID, "consent": consentBody(cs), "documents": docs,
		"periods": []string{strconv.Itoa(year - 2), strconv.Itoa(year - 1), strconv.Itoa(year)},
	}, nil)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(created, &req)
	raw, _ := r.get(ctx, "report", "/tax-reports/v1/requests/"+url.PathEscape(req.RequestID), nil, nil)
	r.put(raw, "report")
}

// pledgeRegistry — что уже заложила компания и свободны ли предложенные транспорт и техника.
func (c *Client) pledgeRegistry(ctx context.Context, r *run, in domain.CollectInput) {
	const path = "/pledge-registry/v1/notifications"
	raw, _ := r.get(ctx, "by_pledgor", path, url.Values{"pledgor_inn": {in.INN}}, nil)
	r.put(raw, "by_pledgor")
	for _, vin := range in.Collateral.VINs {
		raw, _ := r.get(ctx, "by_vin", path, url.Values{"vin": {vin}}, nil)
		r.put(raw, "by_vin", vin)
	}
	for _, serial := range in.Collateral.Serials {
		raw, _ := r.get(ctx, "by_serial", path, url.Values{"serial": {serial}}, nil)
		r.put(raw, "by_serial", serial)
	}
}

// egrn — выписки по каждому объекту недвижимости, предложенному в залог.
func (c *Client) egrn(ctx context.Context, r *run, in domain.CollectInput) {
	for _, num := range in.Collateral.CadastralNumbers {
		raw, _ := r.postJSON(ctx, "extract", "/egrn/v1/extracts", map[string]any{
			"cadastral_number": num, "purpose": "COLLATERAL_ASSESSMENT", "application_id": r.requestID,
		}, nil)
		r.put(raw, num)
	}
}

// gosuslugi — «Цифровой профиль» руководителя: запрос → результат (мок одобряет сразу).
func (c *Client) gosuslugi(ctx context.Context, r *run, in domain.CollectInput) {
	created, ok := r.postJSON(ctx, "request", "/gosuslugi/v1/profile-requests", map[string]any{
		"person":         personBody(in.Director),
		"scopes":         []string{"PASSPORT", "INN", "SNILS", "ADDRESS", "INCOME", "EMPLOYMENT", "NPD_STATUS"},
		"purpose":        "LOAN_APPLICATION",
		"application_id": r.requestID,
	}, nil)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(created, &req)
	raw, _ := r.get(ctx, "profile", "/gosuslugi/v1/profile-requests/"+url.PathEscape(req.RequestID), nil, nil)
	r.put(raw, "director")
}

// openBanking — по каждому банку из анкеты: согласие → счета → остатки и транзакции за 12 месяцев.
func (c *Client) openBanking(ctx context.Context, r *run, in domain.CollectInput) {
	now := c.now().In(msk)
	from := time.Date(now.Year(), now.Month()-12, 1, 0, 0, 0, 0, msk)
	for _, bik := range in.OtherBankBIKs {
		h := http.Header{"X-Customer-Inn": {in.INN}, "X-Bank-Bic": {bik}}
		created, ok := r.postJSON(ctx, "consent", "/open-banking/v1/account-consents", map[string]any{"Data": map[string]any{
			"permissions":             []string{"ReadAccountsBasic", "ReadAccountsDetail", "ReadBalances", "ReadTransactionsDetail"},
			"expirationDateTime":      now.AddDate(0, 3, 0).Format(time.RFC3339),
			"transactionFromDateTime": from.Format(time.RFC3339),
			"transactionToDateTime":   now.Format(time.RFC3339),
		}}, h)
		if ok {
			var cs struct {
				Data struct {
					ConsentID string `json:"consentId"`
				} `json:"Data"`
			}
			_ = json.Unmarshal(created, &cs)
			raw, _ := r.get(ctx, "consent_status", "/open-banking/v1/account-consents/"+url.PathEscape(cs.Data.ConsentID), nil, h)
			r.put(raw, bik, "consent")
		}

		accounts, ok := r.get(ctx, "accounts", "/open-banking/v1/accounts", nil, h)
		r.put(accounts, bik, "accounts")
		if !ok {
			continue
		}
		var list struct {
			Data struct {
				Account []struct {
					AccountID string `json:"accountId"`
				} `json:"Account"`
			} `json:"Data"`
		}
		_ = json.Unmarshal(accounts, &list)
		for _, a := range list.Data.Account {
			base := "/open-banking/v1/accounts/" + url.PathEscape(a.AccountID)
			raw, _ := r.get(ctx, "balances", base+"/balances", nil, h)
			r.put(raw, bik, "balances", a.AccountID)
			raw, _ = r.get(ctx, "transactions", base+"/transactions", url.Values{
				"fromBookingDateTime": {from.Format(time.RFC3339)}, "toBookingDateTime": {now.Format(time.RFC3339)},
			}, h)
			r.put(raw, bik, "transactions", a.AccountID)
		}
	}
}

// marketplaces — профиль, продажи за 36 месяцев и выплаты на каждой площадке, где есть токен.
func (c *Client) marketplaces(ctx context.Context, r *run, in domain.CollectInput) {
	for _, mp := range sortedKeys(in.MarketplaceTokens) {
		h := http.Header{"X-Seller-Token": {in.MarketplaceTokens[mp]}}
		base := "/marketplaces/v1/" + url.PathEscape(mp) + "/sellers/" + url.PathEscape(in.INN)
		raw, _ := r.get(ctx, "profile", base, nil, h)
		r.put(raw, mp, "profile")
		raw, _ = r.get(ctx, "sales", base+"/sales/monthly", url.Values{"months": {"36"}}, h)
		r.put(raw, mp, "sales")
		raw, _ = r.get(ctx, "payouts", base+"/payouts", nil, h)
		r.put(raw, mp, "payouts")
	}
}

// ofd — кассы и выручка по чекам за 12 месяцев.
func (c *Client) ofd(ctx context.Context, r *run, in domain.CollectInput) {
	h := http.Header{"X-OFD-Token": {in.OFDToken}}
	base := "/ofd/v1/organizations/" + url.PathEscape(in.INN)
	raw, _ := r.get(ctx, "kkt", base+"/kkt", nil, h)
	r.put(raw, "kkt")
	raw, _ = r.get(ctx, "receipts", base+"/receipts/monthly", url.Values{"months": {"12"}}, h)
	r.put(raw, "receipts")
}

// zakupki — госконтракты, где компания поставщик, и реестр недобросовестных поставщиков.
func (c *Client) zakupki(ctx context.Context, r *run, in domain.CollectInput) {
	raw, _ := r.get(ctx, "contracts", "/zakupki/v1/contracts", url.Values{"supplier_inn": {in.INN}}, nil)
	r.put(raw, "contracts")
	raw, _ = r.get(ctx, "rnp", "/zakupki/v1/rnp", url.Values{"inn": {in.INN}}, nil)
	r.put(raw, "rnp")
}

// signals — госпроверки, товарные знаки, новости.
func (c *Client) signals(ctx context.Context, r *run, in domain.CollectInput) {
	for _, kind := range []string{"inspections", "trademarks", "news"} {
		raw, _ := r.get(ctx, kind, "/signals/v1/"+url.PathEscape(in.INN)+"/"+kind, nil, nil)
		r.put(raw, kind)
	}
}

// cbr — ключевая ставка за последние 30 дней. Ответ SOAP (XML) кладётся строкой как есть.
func (c *Client) cbr(ctx context.Context, r *run, _ domain.CollectInput) {
	to := c.now().In(msk)
	from := to.AddDate(0, 0, -30)
	envelope := `<?xml version="1.0" encoding="utf-8"?>` +
		`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"><soap:Body>` +
		`<KeyRate xmlns="http://web.cbr.ru/"><fromDate>` + from.Format(time.DateOnly) + `</fromDate>` +
		`<ToDate>` + to.Format(time.DateOnly) + `</ToDate></KeyRate></soap:Body></soap:Envelope>`
	body, ok := r.do(ctx, "key_rate", http.MethodPost, "/cbr/DailyInfoWebServ/DailyInfo.asmx", nil,
		http.Header{"SOAPAction": {`"http://web.cbr.ru/KeyRate"`}}, []byte(envelope), "text/xml; charset=utf-8")
	if ok {
		r.put(string(body), "key_rate")
	}
}

// hh — работодатель на hh.ru по названию компании и его вакансии.
// В поиске бывают тёзки, поэтому вакансии берём только у точного совпадения названия.
func (c *Client) hh(ctx context.Context, r *run, name string) {
	if name == "" {
		r.skip("нет названия компании: ЕГРЮЛ не ответил")
		return
	}
	raw, ok := r.get(ctx, "employers", "/hh/employers", url.Values{"text": {name}}, nil)
	r.put(raw, "employers")
	if !ok {
		return
	}
	id := matchEmployer(raw, name)
	if id == "" {
		r.skip("на hh.ru нет работодателя «" + name + "»")
		return
	}
	raw, _ = r.get(ctx, "vacancies", "/hh/vacancies", url.Values{"employer_id": {id}}, nil)
	r.put(raw, "vacancies")
}

// pages собирает страницы, пока не наберётся total из ответа. nil — первая страница не пришла.
func pages(size int, fetch func(page int) (json.RawMessage, bool)) []json.RawMessage {
	var out []json.RawMessage
	for page := 1; page <= maxPages; page++ {
		raw, ok := fetch(page)
		if !ok {
			break
		}
		out = append(out, raw)
		var p struct {
			Total int `json:"total"`
		}
		if json.Unmarshal(raw, &p) != nil || page*size >= p.Total {
			break
		}
	}
	return out
}

func consentBody(cs *domain.Consent) map[string]any {
	m := map[string]any{"id": cs.ID, "valid_until": cs.ValidUntil}
	if cs.SignedAt != "" {
		m["signed_at"] = cs.SignedAt
	}
	if len(cs.Scope) > 0 {
		m["scope"] = cs.Scope
	}
	return m
}

func personBody(p *domain.Person) map[string]any {
	return map[string]any{"inn": p.INN, "last_name": p.LastName, "first_name": p.FirstName,
		"middle_name": p.MiddleName, "birth_date": p.BirthDate}
}

// pbToken достаёт токен карточки из поиска «Прозрачного бизнеса» (юрлица — в "ul", ИП — в "ip").
func pbToken(raw json.RawMessage) string {
	var s map[string]struct {
		Data []struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	for _, section := range []string{"ul", "ip"} {
		if d := s[section].Data; len(d) > 0 {
			return d[0].Token
		}
	}
	return ""
}

func boID(raw json.RawMessage) int64 {
	var s struct {
		Content []struct {
			ID int64 `json:"id"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &s) != nil || len(s.Content) == 0 {
		return 0
	}
	return s.Content[0].ID
}

// companyName — название компании для поиска на hh.ru из ответа ЕГРЮЛ в данных ФНС:
// ООО "СЕВЕРНЫЙ ВЕТЕР" → СЕВЕРНЫЙ ВЕТЕР; у ИП — ФИО.
func companyName(fnsData json.RawMessage) string {
	var d struct {
		EGRUL struct {
			Rows []struct {
				C string `json:"c"`
				N string `json:"n"`
			} `json:"rows"`
		} `json:"egrul"`
	}
	if len(fnsData) == 0 || json.Unmarshal(fnsData, &d) != nil || len(d.EGRUL.Rows) == 0 {
		return ""
	}
	name := d.EGRUL.Rows[0].C
	if name == "" {
		name = d.EGRUL.Rows[0].N
	}
	name = strings.NewReplacer("«", `"`, "»", `"`).Replace(name)
	if i, j := strings.Index(name, `"`), strings.LastIndex(name, `"`); i >= 0 && j > i {
		return strings.TrimSpace(name[i+1 : j])
	}
	return strings.TrimSpace(strings.TrimPrefix(name, "Индивидуальный предприниматель"))
}

// CompanyCard — реквизиты компании из первой строки выписки ЕГРЮЛ/ЕГРИП в данных ФНС.
// false — ЕГРЮЛ не ответил или компании там нет.
func (*Client) CompanyCard(fnsData json.RawMessage) (domain.CompanyCard, bool) {
	var d struct {
		EGRUL struct {
			Rows []struct {
				C  string `json:"c"`  // короткое имя
				N  string `json:"n"`  // полное имя
				I  string `json:"i"`  // ИНН
				O  string `json:"o"`  // ОГРН / ОГРНИП
				P  string `json:"p"`  // КПП
				R  string `json:"r"`  // дата регистрации ДД.ММ.ГГГГ
				G  string `json:"g"`  // «ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: ФИО»
				K  string `json:"k"`  // ul — юрлицо, fl — ИП
				RN string `json:"rn"` // регион
			} `json:"rows"`
		} `json:"egrul"`
	}
	if len(fnsData) == 0 || json.Unmarshal(fnsData, &d) != nil || len(d.EGRUL.Rows) == 0 || d.EGRUL.Rows[0].I == "" {
		return domain.CompanyCard{}, false
	}
	row := d.EGRUL.Rows[0]
	card := domain.CompanyCard{INN: row.I, Type: domain.CompanyLegalEntity, OGRN: row.O, KPP: row.P,
		FullName: row.N, ShortName: row.C, Region: row.RN}
	if row.K == "fl" {
		card.Type = domain.CompanySoleProprietor
	}
	if t, err := time.Parse("02.01.2006", row.R); err == nil {
		card.RegistrationDate = t.Format(time.DateOnly)
	}
	if _, name, ok := strings.Cut(row.G, ":"); ok {
		card.DirectorName = strings.TrimSpace(name)
	}
	return card, true
}

// matchEmployer — id работодателя с тем же названием, что у компании; "" — совпадения нет.
func matchEmployer(raw json.RawMessage, name string) string {
	var s struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	for _, e := range s.Items {
		if normName(e.Name) == normName(name) {
			return e.ID
		}
	}
	return ""
}

func normName(s string) string {
	s = strings.NewReplacer("Ё", "Е", `"`, " ", "«", " ", "»", " ").Replace(strings.ToUpper(s))
	return strings.Join(strings.Fields(s), " ")
}
