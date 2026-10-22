# Mock API: первые 7 сервисов

> Все ИНН, названия, суммы и номера в примерах **случайные**.
> Примеры построены вокруг одной компании, чтобы поля сходились между сервисами:
> **ООО «СЕВЕРНЫЙ ВЕТЕР»**, ИНН `7799000012`, ОГРН `1217700412345`, КПП `779901001`, ОКВЭД 46.39 (оптовая торговля продуктами).
> Директор и единственный учредитель **Ковалёв Игорь Сергеевич**, ИНН `779900010139`.

## Общие правила для всех моков

| Правило | Значение |
|---|---|
| Сервер | один mock-сервер, у каждого сервиса свой префикс: `/core-banking`, `/fns`, `/bki`, `/compliance`, `/fssp`, `/kad`, `/fedresurs` |
| Как выбирается ответ | по ИНН из seed-файлов (`mock-api/<Система>/seed/<inn>.json`). Неизвестный ИНН: `404` для карточек, пустой список для поиска |
| Суммы | рубли, number с 2 знаками. Исключение: ГИР БО в **тыс. руб.**, как в реальности |
| Даты | ISO `YYYY-MM-DD`. Исключение: ответы ФНС в реальном формате (`14.03.2021`) |
| Ошибка | `{"error": {"code": "CONSENT_REQUIRED", "message": "..."}}` |
| Сбои для демо | заголовок `X-Mock-Fault: timeout \| 500 \| 503 \| captcha`, имитирует отказ источника |
| Трассировка | `X-Request-Id` возвращается эхом |

---

## 1. Core Banking (АБС банка)

Внутренние данные о клиенте: счета, обороты, транзакции, кредиты у нас, ограничения по счетам.

| Метод | Путь | Что отдаёт |
|---|---|---|
| GET | `/core-banking/v1/clients/{inn}` | профиль клиента, счета, продукты |
| GET | `/core-banking/v1/clients/{inn}/turnover?months=12` | обороты по месяцам |
| GET | `/core-banking/v1/clients/{inn}/transactions?from=&to=&page=&size=` | транзакции |
| GET | `/core-banking/v1/clients/{inn}/loans` | кредиты в нашем банке |
| GET | `/core-banking/v1/clients/{inn}/restrictions` | картотека, блокировки, аресты по нашим счетам |

### `GET /clients/{inn}`
```json
{
  "client_id": "CL-0048213",
  "inn": "7799000012",
  "kpp": "779901001",
  "ogrn": "1217700412345",
  "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"",
  "client_type": "LEGAL",
  "segment": "MICRO",
  "client_since": "2022-03-14",
  "accounts": [
    { "account_number": "40702810900000012345", "currency": "RUB", "opened_at": "2022-03-14", "status": "ACTIVE" },
    { "account_number": "40702840200000004411", "currency": "USD", "opened_at": "2024-05-20", "status": "ACTIVE" }
  ],
  "products": {
    "acquiring": true,
    "payroll": true,
    "payroll_employees": 14,
    "corporate_cards": 2
  }
}
```
`client_type`: `LEGAL` | `IP`. `segment`: `MICRO` | `SMALL` | `MEDIUM`.

### `GET /clients/{inn}/turnover?months=12`
```json
{
  "inn": "7799000012",
  "period_from": "2025-10",
  "period_to": "2026-09",
  "months": [
    { "month": "2026-09", "credit_turnover": 4812350.00, "debit_turnover": 4590120.40, "acquiring_turnover": 1203400.00,
      "avg_balance": 612300.15, "min_balance": 48210.00, "zero_balance_days": 0, "incoming_tx_count": 87, "outgoing_tx_count": 142 },
    { "month": "2026-08", "credit_turnover": 4377920.00, "debit_turnover": 4402115.75, "acquiring_turnover": 1118750.00,
      "avg_balance": 534880.90, "min_balance": 21004.10, "zero_balance_days": 0, "incoming_tx_count": 81, "outgoing_tx_count": 133 },
    { "month": "2026-07", "credit_turnover": 3954100.00, "debit_turnover": 3870442.30, "acquiring_turnover": 987300.00,
      "avg_balance": 498215.40, "min_balance": 15320.00, "zero_balance_days": 1, "incoming_tx_count": 76, "outgoing_tx_count": 129 }
  ]
}
```
В примере 3 месяца, мок отдаёт все 12. У нового клиента (сценарий D) месяцев меньше, `period_from` и `period_to` считаются по тем, что есть.

### `GET /clients/{inn}/transactions?from=2025-10-01&to=2026-09-30&page=1&size=500`
```json
{
  "inn": "7799000012",
  "page": 1, "size": 500, "total": 1834,
  "items": [
    {
      "id": "TX-20260915-000123", "date": "2026-09-15", "direction": "CREDIT", "amount": 356000.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7801123456", "kpp": "780101001", "name": "ООО \"ЛЕНТА-ОПТ\"", "bik": "044030653", "account": "40702810555000098765" },
      "purpose": "Оплата по счету №418 от 08.09.2026 за поставку товара. В т.ч. НДС 20% 59333.33"
    },
    {
      "id": "TX-20260925-000311", "date": "2026-09-25", "direction": "DEBIT", "amount": 187430.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7727406020", "kpp": "770801001", "name": "Казначейство России (ФНС России)", "bik": "024501901", "account": "03100643000000018500" },
      "purpose": "Единый налоговый платеж"
    },
    {
      "id": "TX-20260912-000207", "date": "2026-09-12", "direction": "DEBIT", "amount": 118400.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7744001234", "kpp": "775001001", "name": "ПАО \"БАНК ВОСТОК\"", "bik": "044525777", "account": "47422810100000000123" },
      "purpose": "Погашение основного долга и процентов по кредитному договору №КД-2025/0712 от 12.03.2025"
    },
    {
      "id": "TX-20260905-000118", "date": "2026-09-05", "direction": "DEBIT", "amount": 64900.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7718654321", "kpp": "771801001", "name": "ООО \"ЛИЗИНГ-ПРО\"", "bik": "044525225", "account": "40701810400000007788" },
      "purpose": "Лизинговый платеж по договору лизинга №ЛД-3391 от 01.02.2026 за сентябрь 2026"
    },
    {
      "id": "TX-20260910-000176", "date": "2026-09-10", "direction": "DEBIT", "amount": 742500.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7728168971", "kpp": "770801001", "name": "АО \"АЛЬФА-БАНК\" (зарплатный проект)", "bik": "044525999", "account": "30232810400000000917" },
      "purpose": "Перечисление заработной платы за август 2026 по реестру №9"
    },
    {
      "id": "TX-20260903-000044", "date": "2026-09-03", "direction": "CREDIT", "amount": 500000.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "779900010139", "kpp": null, "name": "Ковалёв Игорь Сергеевич", "bik": "044525974", "account": "40817810100004561234" },
      "purpose": "Предоставление процентного займа учредителем по договору №3 от 01.09.2026"
    },
    {
      "id": "TX-20260920-000255", "date": "2026-09-20", "direction": "DEBIT", "amount": 300000.00, "currency": "RUB",
      "operation_code": "01",
      "counterparty": { "inn": "7799000012", "kpp": "779901001", "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"", "bik": "044525225", "account": "40702810738000123456" },
      "purpose": "Перевод собственных средств"
    }
  ]
}
```
- `operation_code`: `01` платёжное поручение, `02` платёжное требование, `06` **инкассовое поручение** (принудительное списание), `16` платёжный ордер.
- `direction`: `CREDIT` (поступление) | `DEBIT` (списание).
- Назначения платежей нарочно «живые»: по ним сбор данных находит налоги, кредиты и лизинг в других банках, ФОТ, займы учредителя и переводы самому себе.
- В моке лежат только ключевые операции, 10–20 в месяц, поэтому `total` меньше, чем в примере. С оборотами из `/turnover` их сумма не сходится.

### `GET /clients/{inn}/loans`
```json
{
  "inn": "7799000012",
  "items": [
    {
      "contract_id": "KD-0048213-01", "product": "OVERDRAFT", "currency": "RUB",
      "limit": 1500000.00, "outstanding": 420000.00, "rate": 21.5, "monthly_payment": null,
      "opened_at": "2025-06-01", "maturity_date": "2026-12-01", "status": "ACTIVE",
      "current_overdue_days": 0, "current_overdue_amount": 0.00,
      "max_overdue_days_24m": 3, "overdue_episodes_30p_24m": 0, "restructured": false
    },
    {
      "contract_id": "KD-0048213-02", "product": "TERM_LOAN", "currency": "RUB",
      "limit": 3000000.00, "outstanding": 1875000.00, "rate": 19.9, "monthly_payment": 96300.00,
      "opened_at": "2024-11-15", "maturity_date": "2027-11-15", "status": "ACTIVE",
      "current_overdue_days": 0, "current_overdue_amount": 0.00,
      "max_overdue_days_24m": 0, "overdue_episodes_30p_24m": 0, "restructured": false
    }
  ]
}
```
`product`: `TERM_LOAN` | `CREDIT_LINE` | `OVERDRAFT` | `GUARANTEE`. `status`: `ACTIVE` | `CLOSED` | `IN_DEFAULT`.

### `GET /clients/{inn}/restrictions`
Чистый клиент:
```json
{
  "inn": "7799000012",
  "checked_at": "2026-10-22T10:15:00Z",
  "card_file": { "has_unpaid_claims": false, "total_amount": 0.00, "items": [] },
  "account_blocks": [],
  "arrests": []
}
```
Проблемный клиент (сценарий C):
```json
{
  "inn": "7799000037",
  "checked_at": "2026-10-22T10:15:00Z",
  "card_file": {
    "has_unpaid_claims": true, "total_amount": 412870.00,
    "items": [ { "document": "Инкассовое поручение №5521", "date": "2026-10-01", "claimant": "ИФНС России №99 по г. Москве", "amount": 412870.00 } ]
  },
  "account_blocks": [
    { "source": "FNS", "decision_number": "12873", "decision_date": "2026-09-30", "authority": "ИФНС России №99 по г. Москве", "type": "FULL", "amount": null }
  ],
  "arrests": [
    { "source": "FSSP", "document": "Постановление №77099/26/551207", "date": "2026-10-05", "amount": 234500.00 }
  ]
}
```
`type`: `FULL` (все операции) | `PARTIAL` (в пределах `amount`).

---

## 2. ФНС (ЕГРЮЛ, «Прозрачный бизнес», ГИР БО, блокировки)

**Пути и поля повторяют реальные сервисы ФНС.** Тогда один и тот же адаптер парсит и мок, и живой ответ, а переключение между ними — это смена base URL (`https://egrul.nalog.ru` → `http://mock:8090/fns/egrul`). Двухшаговые запросы (сначала токен, потом результат) мок отрабатывает мгновенно.

| Метод | Путь | Реальный аналог |
|---|---|---|
| POST | `/fns/egrul/` (form `query=<ИНН>`) | `egrul.nalog.ru/` |
| GET | `/fns/egrul/search-result/{t}` | `egrul.nalog.ru/search-result/{t}` |
| POST | `/fns/pb/search-proc.json` | `pb.nalog.ru/search-proc.json` |
| POST | `/fns/pb/company-proc.json` | `pb.nalog.ru/company-proc.json` |
| GET | `/fns/bo/advanced-search/organizations/search?query=<ИНН>` | `bo.nalog.gov.ru/...` |
| GET | `/fns/bo/nbo/organizations/{id}/bfo/` | `bo.nalog.gov.ru/nbo/...` |
| POST | `/fns/bi/bi2-proc.json` (form `inn`, `bik`) | `service.nalog.ru/bi2-proc.json` (формат свой) |

### ЕГРЮЛ: `POST /fns/egrul/` → `GET /fns/egrul/search-result/{t}`
```json
{ "t": "MOCK-T-7799000012", "captchaRequired": false }
```
```json
{
  "rows": [
    {
      "c": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"",
      "n": "ОБЩЕСТВО С ОГРАНИЧЕННОЙ ОТВЕТСТВЕННОСТЬЮ \"СЕВЕРНЫЙ ВЕТЕР\"",
      "i": "7799000012",
      "o": "1217700412345",
      "p": "779901001",
      "r": "14.03.2021",
      "g": "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич",
      "k": "ul",
      "rn": "Г.Москва",
      "t": "MOCK-VYP-7799000012"
    }
  ]
}
```
- У ИП: `"k": "fl"`, `"n": "Индивидуальный предприниматель Смирнова Анна Викторовна"`, `"i"` из 12 цифр, `"o"` — ОГРНИП из 15 цифр.
- У ликвидированной компании добавляется `"e": "15.09.2026"` (дата прекращения).

### «Прозрачный бизнес»: `POST /fns/pb/company-proc.json` (`method=get-response`)
```json
{
  "vyp": {
    "ИНН": "7799000012",
    "ОГРН": "1217700412345",
    "КПП": "779901001",
    "НаимЮЛСокр": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"",
    "ДатаРег": "2021-03-14",
    "КодОКВЭД": "46.39",
    "НаимОКВЭД": "Торговля оптовая неспециализированная пищевыми продуктами, напитками и табачными изделиями",
    "СумКап": "10000",
    "Адрес": "125319,МОСКВА ГОРОД,,,,УЛИЦА ЧЕРНЯХОВСКОГО,9,,ПОМЕЩ. 4",
    "sulst_ex": 10,
    "sulst_name_ex": "Действующая организация",
    "invalid": 0,
    "pr_zd": 0,
    "pr_otch": 0,
    "rsmpcategory": 1,
    "masruk": [ { "inn": "779900010139", "name": "КОВАЛЁВ ИГОРЬ СЕРГЕЕВИЧ", "position": "ГЕНЕРАЛЬНЫЙ ДИРЕКТОР", "cnt": "1" } ]
  },
  "masaddress": [],
  "vestnik": [],
  "liquidated": false,
  "okved2exs": [
    "46.39 - Торговля оптовая неспециализированная пищевыми продуктами, напитками и табачными изделиями",
    "49.41 - Деятельность автомобильного грузового транспорта"
  ]
}
```
| Поле | Хорошо | Плохо |
|---|---|---|
| `sulst_ex` | `10` действующая | другое значение → стоп |
| `invalid` | `0` | `1` недостоверность → стоп |
| `pr_zd` | `0` | `1` долг по налогам |
| `pr_otch` | `0` | `1` не сдаёт отчётность → стоп |
| `masruk[].cnt` | `1` | `>5` номинальный директор |
| `masaddress` | `[]` | 10+ компаний → массовый адрес |
| `liquidated` | `false` | `true` → стоп |
| `rsmpcategory` | 1 микро, 2 малое, 3 среднее (договорённость мока) | |

### ГИР БО: поиск → отчётность (суммы в **тыс. руб.**)
`GET /fns/bo/advanced-search/organizations/search?query=7799000012`
```json
{
  "content": [
    { "id": 90000123, "inn": "7799000012", "shortName": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"", "ogrn": "1217700412345",
      "okved2": "46.39", "statusCode": "ACTIVE",
      "bfo": { "period": "2025", "gainSum": 58420, "knd": "0710099" } }
  ],
  "totalElements": 1
}
```
`GET /fns/bo/nbo/organizations/90000123/bfo/`
```json
[
  {
    "period": "2025",
    "actives": 21430,
    "gainSum": 58420,
    "typeCorrections": [ { "correction": {
      "balance": {
        "current1100": 3120, "current1200": 18310, "current1230": 9870, "current1250": 2140,
        "current1300": 7650, "current1410": 2400, "current1510": 1500, "current1520": 9880, "current1600": 21430,
        "previous1300": 4920, "previous1600": 17260
      },
      "financialResult": {
        "current2110": 58420, "previous2110": 46310,
        "current2120": 51200, "current2200": 4380, "current2330": 610,
        "current2300": 3420, "current2410": 690, "current2400": 2730, "previous2400": 1980
      },
      "fundsMovement": { "current4100": 3150, "previous4100": 2210 }
    } } ]
  },
  { "period": "2024", "actives": 17260, "gainSum": 46310, "typeCorrections": [ { "correction": { "...": "аналогично" } } ] }
]
```
Строки расходов (`2120`, `2330`, `2410`) хранятся **положительными** числами, прибыль (`2200`, `2300`, `2400`) — со знаком.

### Блокировки счетов: `POST /fns/bi/bi2-proc.json` (`inn=7799000012&bik=044525999`)
Реальный формат ответа неизвестен, поэтому формат свой.
```json
{ "inn": "7799000012", "bik": "044525999", "checked_at": "2026-10-22T10:15:03", "decisions": [] }
```
Сценарий C:
```json
{
  "inn": "7799000037", "bik": "044525999", "checked_at": "2026-10-22T10:15:03",
  "decisions": [
    { "number": "12873", "date": "2026-09-30", "tax_authority_code": "7799",
      "tax_authority_name": "ИФНС России №99 по г. Москве", "type": "FULL", "amount": null }
  ]
}
```

---

## 3. БКИ (бюро кредитных историй)

Один мок с ответом «как НБКИ». Запрашивается **дважды**: по компании и по собственнику (директору, поручителю).

| Метод | Путь |
|---|---|
| POST | `/bki/v1/reports` |

### Запрос: компания
```json
{
  "subject_type": "LEGAL",
  "inn": "7799000012",
  "ogrn": "1217700412345",
  "consent": { "id": "CNS-2026-000781", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21" },
  "purpose": "LOAN_APPLICATION",
  "application_id": "APP-000154"
}
```
### Запрос: физлицо
```json
{
  "subject_type": "PERSON",
  "person": { "last_name": "Ковалёв", "first_name": "Игорь", "middle_name": "Сергеевич",
              "birth_date": "1984-07-19", "inn": "779900010139", "passport": "4519 128734" },
  "consent": { "id": "CNS-2026-000782", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21" },
  "purpose": "LOAN_APPLICATION",
  "application_id": "APP-000154"
}
```
### Ответ
```json
{
  "report_id": "NBKI-RPT-8F3A21",
  "bureau": "NBKI",
  "generated_at": "2026-10-22T10:15:07Z",
  "subject": { "type": "LEGAL", "inn": "7799000012", "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"" },
  "has_history": true,
  "score": { "value": 712, "min": 300, "max": 850, "grade": "B", "model": "MOCK-SME-v1" },
  "summary": {
    "active_accounts": 3,
    "closed_accounts": 5,
    "total_limit": 7000000.00,
    "total_outstanding": 3690000.00,
    "total_monthly_payment": 214700.00,
    "current_overdue_amount": 0.00,
    "max_overdue_days_ever": 17,
    "overdue_30p_24m": 0,
    "overdue_60p_24m": 0,
    "overdue_90p_24m": 0,
    "restructured_count": 0,
    "inquiries_7d": 1,
    "inquiries_30d": 2,
    "inquiries_90d": 4
  },
  "accounts": [
    {
      "account_id": "ACC-1", "creditor_type": "BANK", "creditor_name": "ПАО \"БАНК ВОСТОК\"",
      "product_type": "TERM_LOAN", "role": "BORROWER", "currency": "RUB",
      "opened_at": "2025-03-12", "planned_close_at": "2027-03-12",
      "limit": 2500000.00, "outstanding": 1395000.00, "monthly_payment": 118400.00,
      "status": "ACTIVE", "current_overdue_days": 0, "current_overdue_amount": 0.00,
      "payment_history_24m": "000000000000010000000000", "collateral": false
    },
    {
      "account_id": "ACC-2", "creditor_type": "BANK", "creditor_name": "АО \"АЛЬФА-БАНК\"",
      "product_type": "TERM_LOAN", "role": "BORROWER", "currency": "RUB",
      "opened_at": "2024-11-15", "planned_close_at": "2027-11-15",
      "limit": 3000000.00, "outstanding": 1875000.00, "monthly_payment": 96300.00,
      "status": "ACTIVE", "current_overdue_days": 0, "current_overdue_amount": 0.00,
      "payment_history_24m": "000000000000000000000000", "collateral": false
    },
    {
      "account_id": "ACC-3", "creditor_type": "BANK", "creditor_name": "АО \"АЛЬФА-БАНК\"",
      "product_type": "OVERDRAFT", "role": "BORROWER", "currency": "RUB",
      "opened_at": "2025-06-01", "planned_close_at": "2026-12-01",
      "limit": 1500000.00, "outstanding": 420000.00, "monthly_payment": null,
      "status": "ACTIVE", "current_overdue_days": 0, "current_overdue_amount": 0.00,
      "payment_history_24m": "00000000000000000-------", "collateral": false
    }
  ],
  "inquiries": [
    { "date": "2026-10-21", "creditor_type": "BANK", "purpose": "LOAN_APPLICATION" },
    { "date": "2026-10-02", "creditor_type": "MFO", "purpose": "LOAN_APPLICATION" }
  ]
}
```
- `payment_history_24m`: первый символ — последний месяц. `0` без просрочки, `1` 1–29 дн., `2` 30–59, `3` 60–89, `4` 90+, `-` нет данных.
- `product_type`: `TERM_LOAN` | `CREDIT_LINE` | `OVERDRAFT` | `GUARANTEE` | `CREDIT_CARD`. `role`: `BORROWER` | `GUARANTOR`.
- Кредиты нашего банка тоже видны в бюро (`АЛЬФА-БАНК`). При расчёте нагрузки их **нельзя считать дважды** вместе с Core Banking.

### Ошибки и пустая история
- Нет или просрочено согласие → `403 {"error":{"code":"CONSENT_REQUIRED"}}` (или `CONSENT_EXPIRED`).
- Истории нет → `200` с `"has_history": false`, `score: null`, пустыми `accounts`. Это не ошибка.

---

## 4. Compliance (ЗСК ЦБ, Росфинмониторинг, санкции)

| Метод | Путь | Что |
|---|---|---|
| GET | `/compliance/v1/zsk/{inn}` | уровень риска платформы ЦБ «Знай своего клиента» |
| POST | `/compliance/v1/screening` | проверка по перечням Росфинмониторинга и санкционным спискам |

### `GET /compliance/v1/zsk/7799000012`
```json
{ "inn": "7799000012", "risk_level": "LOW", "assigned_at": "2026-07-01", "reason_codes": [] }
```
Сценарий E (жёлтый):
```json
{ "inn": "7799000044", "risk_level": "MEDIUM", "assigned_at": "2026-09-14", "reason_codes": ["TRANSIT_OPERATIONS", "HIGH_CASH_WITHDRAWAL"] }
```
`risk_level`: `LOW` (зелёный) | `MEDIUM` (жёлтый) → ручная проверка | `HIGH` (красный) → стоп.

### `POST /compliance/v1/screening`
```json
{
  "lists": ["RFM_TERRORISM_EXTREMISM", "RFM_FREEZE", "OFAC_SDN", "UK", "EU"],
  "subjects": [
    { "ref": "company", "type": "LEGAL", "inn": "7799000012", "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"" },
    { "ref": "director", "type": "PERSON", "inn": "779900010139", "full_name": "Ковалёв Игорь Сергеевич", "birth_date": "1984-07-19" }
  ]
}
```
```json
{
  "checked_at": "2026-10-22T10:15:05Z",
  "list_versions": { "RFM_TERRORISM_EXTREMISM": "2026-10-20", "OFAC_SDN": "2026-10-17", "UK": "2026-10-15", "EU": "2026-10-16" },
  "results": [
    { "ref": "company", "listed": false, "matches": [] },
    { "ref": "director", "listed": false, "matches": [] }
  ]
}
```
Совпадение (для теста стопа):
```json
{ "ref": "director", "listed": true,
  "matches": [ { "list": "RFM_TERRORISM_EXTREMISM", "entry_id": "RFM-55821", "name": "КОВАЛЕВ ИГОРЬ СЕРГЕЕВИЧ", "birth_date": "1984-07-19", "match_score": 0.97 } ] }
```

---

## 5. ФССП (исполнительные производства)

| Метод | Путь |
|---|---|
| GET | `/fssp/v1/proceedings?inn=7799000012` (юрлицо или ИП по ИНН) |
| GET | `/fssp/v1/proceedings?last_name=&first_name=&middle_name=&birth_date=&region=77` (физлицо) |

```json
{
  "debtor": { "type": "LEGAL", "inn": "7799000012", "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"" },
  "checked_at": "2026-10-22T10:15:06Z",
  "summary": { "active_count": 1, "active_amount": 48250.00, "finished_count": 2, "finished_no_assets_count": 0 },
  "items": [
    {
      "number": "148213/26/77099-ИП",
      "opened_at": "2026-08-11",
      "executive_document": "Исполнительный лист ФС №041237785 от 22.07.2026, Арбитражный суд г. Москвы, дело №А40-118734/2026",
      "subject_code": "CONTRACT_DEBT",
      "subject": "Задолженность по договору поставки",
      "amount_initial": 48250.00,
      "amount_remaining": 48250.00,
      "status": "ACTIVE",
      "finished_at": null, "finish_reason_code": null, "finish_reason": null,
      "department": "ОСП по Центральному АО №1 ГУФССП России по г. Москве",
      "bailiff": "Петрова М.А."
    },
    {
      "number": "90211/25/77099-ИП",
      "opened_at": "2025-11-03",
      "executive_document": "Постановление по делу об АП №18810077250001234567",
      "subject_code": "FINE",
      "subject": "Штраф ГИБДД",
      "amount_initial": 500.00,
      "amount_remaining": 0.00,
      "status": "FINISHED",
      "finished_at": "2025-11-20", "finish_reason_code": "47-1-1", "finish_reason": "ст. 47 ч. 1 п. 1 — фактическое исполнение",
      "department": "ОСП по Центральному АО №1 ГУФССП России по г. Москве",
      "bailiff": "Петрова М.А."
    },
    {
      "number": "51877/25/77099-ИП",
      "opened_at": "2025-05-19",
      "executive_document": "Акт налогового органа №3341 от 12.05.2025",
      "subject_code": "TAX",
      "subject": "Налоги и сборы, включая пени",
      "amount_initial": 12740.00,
      "amount_remaining": 0.00,
      "status": "FINISHED",
      "finished_at": "2025-06-02", "finish_reason_code": "47-1-1", "finish_reason": "ст. 47 ч. 1 п. 1 — фактическое исполнение",
      "department": "ОСП по Центральному АО №1 ГУФССП России по г. Москве",
      "bailiff": "Иванов Д.К."
    }
  ]
}
```
- `subject_code`: `TAX` | `CREDIT` | `CONTRACT_DEBT` | `FINE` | `SALARY` | `OTHER`.
- `finish_reason_code`: `47-1-1` исполнено (нормально); `46-1-3` / `46-1-4` **невозможно взыскать, нет имущества** (сильный негатив, учитывается в `finished_no_assets_count`).

---

## 6. КАД (арбитражные дела)

| Метод | Путь |
|---|---|
| POST | `/kad/v1/cases/search` |

```json
{ "inn": "7799000012", "role": "ANY", "date_from": "2023-10-01", "page": 1, "size": 50 }
```
`role`: `ANY` | `PLAINTIFF` | `DEFENDANT` | `THIRD_PARTY`.

```json
{
  "inn": "7799000012",
  "checked_at": "2026-10-22T10:15:06Z",
  "summary": {
    "as_defendant_count_12m": 2, "as_defendant_amount_12m": 176650.00, "as_defendant_active_amount": 128400.00,
    "as_plaintiff_count_12m": 3, "as_plaintiff_amount_12m": 2045000.00,
    "bankruptcy_as_debtor": false
  },
  "total": 5,
  "items": [
    {
      "case_number": "А40-118734/2026", "court": "АС города Москвы", "registered_at": "2026-05-14",
      "case_type": "CIVIL",
      "category": "О неисполнении или ненадлежащем исполнении обязательств по договорам поставки",
      "role": "DEFENDANT",
      "plaintiffs": [ { "name": "ООО \"АГРОСНАБ\"", "inn": "5024187365" } ],
      "defendants": [ { "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"", "inn": "7799000012" } ],
      "claim_amount": 48250.00,
      "status": "CLOSED", "outcome": "SATISFIED", "decision_date": "2026-07-22",
      "last_event": "Решение вступило в законную силу", "last_event_at": "2026-08-23", "next_hearing_at": null
    },
    {
      "case_number": "А41-77120/2026", "court": "АС Московской области", "registered_at": "2026-06-30",
      "case_type": "CIVIL",
      "category": "О взыскании задолженности по договору поставки",
      "role": "PLAINTIFF",
      "plaintiffs": [ { "name": "ООО \"СЕВЕРНЫЙ ВЕТЕР\"", "inn": "7799000012" } ],
      "defendants": [ { "name": "ООО \"ПРОДМАРКЕТ\"", "inn": "5047201188" } ],
      "claim_amount": 1240000.00,
      "status": "PENDING", "outcome": null, "decision_date": null,
      "last_event": "Назначено судебное заседание", "last_event_at": "2026-10-01", "next_hearing_at": "2026-11-12"
    }
  ]
}
```
- `case_type`: `CIVIL` | `BANKRUPTCY` | `ADMINISTRATIVE`.
- `outcome`: `SATISFIED` | `PARTIALLY_SATISFIED` | `REJECTED` | `SETTLED` | `null`.

Банкротное дело (сценарий C):
```json
{
  "case_number": "А40-245903/2026", "court": "АС города Москвы", "registered_at": "2026-10-18",
  "case_type": "BANKRUPTCY", "category": "О несостоятельности (банкротстве) организаций и граждан",
  "role": "DEBTOR",
  "plaintiffs": [ { "name": "ООО \"АГРОСНАБ\"", "inn": "5024187365" } ],
  "defendants": [ { "name": "ООО \"АЛЬТАИР\"", "inn": "7799000037" } ],
  "claim_amount": 1184000.00, "status": "PENDING", "outcome": null, "bankruptcy_stage": "PETITION_FILED",
  "last_event": "Заявление принято к производству", "last_event_at": "2026-10-20", "next_hearing_at": "2026-11-25"
}
```

---

## 7. Федресурс / ЕФРСБ

| Метод | Путь |
|---|---|
| GET | `/fedresurs/v1/companies/{inn}/messages?from=2023-10-01` |
| GET | `/fedresurs/v1/persons/{inn}/bankruptcy` (собственник или ИП как физлицо) |

### `GET /fedresurs/v1/companies/7799000012/messages`
```json
{
  "inn": "7799000012",
  "checked_at": "2026-10-22T10:15:06Z",
  "summary": {
    "bankruptcy_stage": "NONE",
    "has_bankruptcy_intention": false,
    "active_leasing_count": 2,
    "liquidation": false,
    "reorganization": false
  },
  "messages": [
    {
      "id": "FR-5C1A9E21", "type": "LEASING_CONTRACT", "published_at": "2026-02-03",
      "publisher": { "name": "ООО \"ЛИЗИНГ-ПРО\"", "inn": "7718654321" },
      "payload": {
        "contract_number": "ЛД-3391", "contract_date": "2026-02-01",
        "period_from": "2026-02-01", "period_to": "2029-01-31",
        "lessee_inn": "7799000012",
        "subject_class": "VEHICLE",
        "subject": "Грузовой фургон ГАЗель NEXT, VIN XUY27050TL0012345, 2025 г.в."
      }
    },
    {
      "id": "FR-77B0D402", "type": "LEASING_CONTRACT", "published_at": "2025-08-19",
      "publisher": { "name": "АО \"ТЕХЛИЗИНГ\"", "inn": "7705112233" },
      "payload": {
        "contract_number": "ТЛ-2025/118", "contract_date": "2025-08-15",
        "period_from": "2025-08-15", "period_to": "2027-08-14",
        "lessee_inn": "7799000012",
        "subject_class": "EQUIPMENT",
        "subject": "Холодильная камера КХН-12, 2 шт."
      }
    }
  ]
}
```
`type`: `LEASING_CONTRACT` | `LEASING_TERMINATION` | `BANKRUPTCY_INTENTION` | `BANKRUPTCY` | `LIQUIDATION_DECISION` | `REORGANIZATION` | `NET_ASSETS_DECREASE`.
Суммы лизингового платежа в Федресурсе **нет**, как и в реальности. Её берём из выписки (назначение «Лизинговый платёж по договору №ЛД-3391»).

Плохие сообщения (сценарий C):
```json
[
  {
    "id": "FR-A91E3377", "type": "BANKRUPTCY_INTENTION", "published_at": "2026-10-02",
    "publisher": { "name": "ООО \"АГРОСНАБ\"", "inn": "5024187365" },
    "payload": { "debtor_inn": "7799000037", "claim_amount": 1184000.00, "planned_filing_after": "2026-10-17" }
  },
  {
    "id": "FR-C2F00915", "type": "BANKRUPTCY", "published_at": "2026-10-20",
    "publisher": { "name": "АС города Москвы", "inn": null },
    "payload": { "case_number": "А40-245903/2026", "stage": "PETITION_FILED", "decision_date": "2026-10-20", "arbitration_manager": null }
  }
]
```
`stage`: `PETITION_FILED` | `OBSERVATION` | `FINANCIAL_RECOVERY` | `EXTERNAL_MANAGEMENT` | `BANKRUPTCY_PROCEEDINGS` | `COMPLETED`.

### `GET /fedresurs/v1/persons/779900010139/bankruptcy`
```json
{ "inn": "779900010139", "full_name": "Ковалёв Игорь Сергеевич", "checked_at": "2026-10-22T10:15:06Z", "bankruptcy_stage": "NONE", "messages": [] }
```

---

## Тестовые сценарии: что отдают моки по каждому ИНН

| Сценарий | ИНН | Core Banking | ФНС | БКИ | Compliance | ФССП | КАД | Федресурс | Ожидаемый исход |
|---|---|---|---|---|---|---|---|---|---|
| **A. Здоровый** ООО «Северный ветер» | `7799000012` | ~4.5 млн/мес, стабильно, без картотеки | действует, отчётность растёт | балл 712, без просрочек | LOW, совпадений нет | 1 мелкое активное | мелкие иски, есть как истец | 2 лизинга | `OFFERED` |
| **B. Перегружен долгами** ООО «Стройресурс» | `7799000020` | ~2 млн/мес, 3 кредита в других банках по выписке, DSCR ≈ 1.0 | действует, долг/выручка высокий | 5 активных кредитов, `inquiries_30d: 6` | LOW | нет | нет | 4 лизинга | `OFFERED`, но меньше сумма или дольше срок |
| **C. Стоп-фактор** ООО «Альтаир» | `7799000037` | картотека, блокировка ФНС, арест ФССП | `pr_zd: 1` | просрочка 60+ | LOW | 3 активных на 0.9 млн | банкротное дело | `BANKRUPTCY_INTENTION` + `BANKRUPTCY` | `REJECTED` |
| **D. ИП, мало данных** ИП Смирнова А.В. | `779900010202` | счёт открыт 2 мес. назад, оборотов мало | ЕГРИП `k: fl`, в ГИР БО пусто | история физлица чистая | LOW | нет | нет | нет | `WAITING_FOR_INFO` → выписка 1С или декларация → скоринг |
| **E. Спорный** ООО «Техно-Сервис» | `7799000044` | нормальные обороты | действует | балл 640, был эпизод 30+ | **MEDIUM** | нет | иск к компании на 3.2 млн, идёт | нет | `MANUAL_REVIEW` |

Идентификаторы для сценариев B–E (A — в шапке):

| | ОГРН / ОГРНИП | Регистрация | ОКВЭД | Директор, ИНН, дата рождения |
|---|---|---|---|---|
| B | `1197700523456` | 2019-05-20 | 41.20 | Орлов Денис Павлович, `779900020136`, 1979-02-11 |
| C | `1207700634567` | 2020-08-17 | 46.90 | Белов Артём Николаевич, `779900030147`, 1988-11-03 |
| D | `326774600012345` | 2026-07-28 | 47.91 | ИП сама: Смирнова Анна Викторовна, 1993-05-27 |
| E | `1187700745678` | 2018-04-09 | 33.12 | Зайцева Ольга Михайловна, `779900040158`, 1981-09-14 |

Для теста стопа по Compliance в списках есть субъекты вне сценариев:
- ФЛ «Громов Виктор Андреевич», 1979-03-02, в `RFM_TERRORISM_EXTREMISM`;
- ЮЛ ООО «ТРАНЗИТ-ИНВЕСТ», ИНН `7799000099`, в `RFM_FREEZE` и `OFAC_SDN`.

Есть и тёзки директоров Ковалёва и Белова с другими отчеством или датой рождения. Они совпадать не должны.
