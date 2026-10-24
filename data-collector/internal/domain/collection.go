package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Сбор данных о компании из всех внешних систем (локально и в демо — эмулятор mock-api).
// Ответы систем хранятся как есть (raw JSON), в признаки скоринга их переводит следующий шаг.

// System — внешняя система, которую опрашиваем. Одна система — одна колонка
// в таблице raw_collections.
type System string

const (
	SystemCoreBanking    System = "CORE_BANKING"    // АБС банка: клиент, обороты, транзакции, наши кредиты, картотека
	SystemFNS            System = "FNS"             // ЕГРЮЛ, «Прозрачный бизнес», ГИР БО, блокировки, устав, реестр МСП, дисквалификация
	SystemBKI            System = "BKI"             // кредитная история компании и руководителя
	SystemCompliance     System = "COMPLIANCE"      // ЗСК ЦБ и санкционные списки
	SystemFSSP           System = "FSSP"            // исполнительные производства
	SystemKAD            System = "KAD"             // арбитражные дела
	SystemFedresurs      System = "FEDRESURS"       // банкротство, лизинг, ликвидация
	SystemM4D            System = "M4D"             // машиночитаемые доверенности
	SystemTaxReports     System = "TAX_REPORTS"     // налоговая отчётность по согласию
	SystemPledgeRegistry System = "PLEDGE_REGISTRY" // реестр залогов движимого имущества
	SystemEGRN           System = "EGRN"            // выписки ЕГРН по недвижимости в залог
	SystemGosuslugi      System = "GOSUSLUGI"       // «Цифровой профиль» руководителя
	SystemOpenBanking    System = "OPEN_BANKING"    // счета в других банках
	SystemMarketplaces   System = "MARKETPLACES"    // Wildberries, Ozon
	SystemOFD            System = "OFD"             // онлайн-кассы
	SystemZakupki        System = "ZAKUPKI"         // госконтракты и РНП
	SystemSignals        System = "SIGNALS"         // госпроверки, товарные знаки, новости
	SystemCBR            System = "CBR"             // ключевая ставка ЦБ
	SystemHH             System = "HH"              // работодатель и вакансии на hh.ru
)

// Systems — все системы в порядке колонок таблицы raw_collections.
var Systems = []System{
	SystemCoreBanking, SystemFNS, SystemBKI, SystemCompliance, SystemFSSP, SystemKAD, SystemFedresurs,
	SystemM4D, SystemTaxReports, SystemPledgeRegistry, SystemEGRN, SystemGosuslugi, SystemOpenBanking,
	SystemMarketplaces, SystemOFD, SystemZakupki, SystemSignals, SystemCBR, SystemHH,
}

// CollectState — итог опроса одной системы.
type CollectState string

const (
	CollectDone    CollectState = "DONE"    // все запросы к системе успешны
	CollectPartial CollectState = "PARTIAL" // часть запросов не удалась
	CollectFailed  CollectState = "FAILED"  // ни одного успешного ответа
	CollectSkipped CollectState = "SKIPPED" // не хватило входных данных: нет согласия, токена, залога
)

// Marketplaces, по которым умеем собирать данные продавца.
const (
	MarketplaceWildberries = "wildberries"
	MarketplaceOzon        = "ozon"
)

// Права в согласии на налоговую отчётность.
const (
	TaxScopeDeclarations = "DECLARATIONS" // декларации: прибыль, НДС, УСН, доходы НПД
	TaxScopeENSBalance   = "ENS_BALANCE"  // сальдо единого налогового счёта
)

// Person — руководитель компании или сам ИП как физлицо.
type Person struct {
	INN        string
	LastName   string
	FirstName  string
	MiddleName string
	BirthDate  string // YYYY-MM-DD
}

// FullName — «Фамилия Имя Отчество» (отчество может отсутствовать).
func (p Person) FullName() string {
	return strings.Join(strings.Fields(p.LastName+" "+p.FirstName+" "+p.MiddleName), " ")
}

// Consent — согласие субъекта: на запрос в БКИ или на получение налоговой отчётности.
type Consent struct {
	ID         string
	SignedAt   string   // RFC 3339, необязательно
	ValidUntil string   // YYYY-MM-DD
	Scope      []string // права; нужны только налоговому согласию
}

// Collateral — предлагаемый залог.
type Collateral struct {
	CadastralNumbers []string // недвижимость → выписки ЕГРН
	VINs             []string // транспорт → реестр залогов
	Serials          []string // техника и оборудование (заводской номер) → реестр залогов
}

// CollectInput — всё, что нужно, чтобы опросить внешние системы по компании.
// Обязателен только ИНН: без остального часть систем пропускается (SKIPPED).
type CollectInput struct {
	INN               string            // компания или ИП
	Director          *Person           // руководитель; у ИП — сам предприниматель
	BKICompanyConsent *Consent          // согласие компании на запрос в БКИ
	BKIPersonConsent  *Consent          // согласие руководителя на запрос в БКИ
	TaxConsent        *Consent          // согласие на налоговую отчётность (ст. 102 НК РФ)
	OtherBankBIKs     []string          // банки, где у клиента есть счета (мультибанкинг)
	MarketplaceTokens map[string]string // wildberries, ozon → токен продавца
	OFDToken          string            // токен оператора фискальных данных
	Collateral        Collateral
	POANumber         string // номер МЧД, если заявку подаёт представитель
	RepresentativeINN string // ИНН представителя по МЧД
}

// SkippedSystems — системы, которые не по чему опрашивать: во входных данных нет согласия,
// токена, залога, банков или руководителя. Такие системы не опрашиваются вовсе: их колонка
// в raw_collections остаётся NULL, в sources — SKIPPED с причиной. Значение — причина.
// Пропуски части запросов внутри системы (БКИ по руководителю, дисквалификация) и hh.ru,
// которому нужно название из ЕГРЮЛ, решаются при опросе.
func SkippedSystems(in CollectInput) map[System]string {
	skipped := map[System]string{}
	if in.BKICompanyConsent == nil {
		switch {
		case in.Director == nil:
			skipped[SystemBKI] = "отчёт по компании: нет согласия consents.bki_company; отчёт по руководителю: нет данных руководителя"
		case in.BKIPersonConsent == nil:
			skipped[SystemBKI] = "отчёт по компании: нет согласия consents.bki_company; отчёт по руководителю: нет согласия consents.bki_person"
		}
	}
	if in.TaxConsent == nil {
		skipped[SystemTaxReports] = "нет согласия на налоговую отчётность consents.tax"
	}
	if len(in.Collateral.CadastralNumbers) == 0 {
		skipped[SystemEGRN] = "нет недвижимости в залог collateral.cadastral_numbers"
	}
	if in.Director == nil {
		skipped[SystemGosuslugi] = "нет данных руководителя"
	}
	if len(in.OtherBankBIKs) == 0 {
		skipped[SystemOpenBanking] = "нет счетов в других банках other_bank_biks"
	}
	if len(in.MarketplaceTokens) == 0 {
		skipped[SystemMarketplaces] = "нет токенов продавца marketplace_tokens"
	}
	if in.OFDToken == "" {
		skipped[SystemOFD] = "нет токена ОФД ofd_token"
	}
	return skipped
}

// Call — один HTTP-запрос к системе.
type Call struct {
	Key      string // что запрашивали: client, turnover, egrul…
	Method   string
	Path     string // путь с query; токены передаются заголовками и сюда не попадают
	Status   int    // HTTP-статус; 0 — ответа нет
	Duration time.Duration
	Error    string // пусто, если запрос успешен
}

// OK сообщает, что запрос успешен.
func (c Call) OK() bool { return c.Error == "" }

// SystemResult — что получили от одной системы.
type SystemResult struct {
	System   System
	State    CollectState
	Reason   string // почему SKIPPED, FAILED или PARTIAL; что пропущено внутри системы
	Duration time.Duration
	Calls    []Call
	Data     json.RawMessage // сырые ответы по ключам запросов; nil — данных нет
}

// FailedCalls — сколько запросов не удалось.
func (r SystemResult) FailedCalls() int {
	n := 0
	for _, c := range r.Calls {
		if !c.OK() {
			n++
		}
	}
	return n
}

// RawCollection — один сбор данных по анкете: строка таблицы raw_collections.
type RawCollection struct {
	ID              string // col_<ULID>
	TaskID          string // задача, результатом которой стал сбор
	QuestionnaireID string
	Input           CollectInput
	Systems         []SystemResult // в порядке Systems
	StartedAt       time.Time
	FinishedAt      time.Time
}
