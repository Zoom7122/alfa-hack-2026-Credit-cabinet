# data-collector

Модуль сбора данных о заёмщике для передачи в скоринг. Независимый Go-модуль: свой `go.mod`, ничего не импортирует из остального репозитория.

Стек: Go, gin, pgx.
- **Анкеты** пока хранятся в памяти процесса (заглушка за интерфейсом `service.Repository`).
- **Сырые ответы внешних систем** пишутся в Postgres, в таблицу `raw_collections`. Схема — в [db/](../db/), миграция `00002_raw_collections.sql`.

## Слои

| Слой | Пакет | Что делает |
|---|---|---|
| Приём запросов | `internal/transport/http` | все эндпоинты — в `router.go`; разбор запросов, ответы и ошибки (RFC 9457, `application/problem+json`) |
| Бизнес-логика | `internal/service` | `QuestionnaireService` — правила анкеты, после сохранения запускает сбор; `CollectionService` — сбор данных о компании из всех внешних систем; `InternalDataService` — старый опрос систем банка для `/check-mock` |
| Хранилище | `internal/repository/postgres` | `raw_collections` (pgx) |
| | `internal/repository/memory` | анкеты; сборы, если не задан `DATABASE_URL` |
| Внешние системы | `internal/client/mockapi` | HTTP-клиент ко всем 19 системам эмулятора [mock-api](../mock-api/): какие эндпоинты вызвать и куда положить ответ |
| | `internal/client/bank` | старый клиент `/bank/v1` для `/check-mock` |

Сущности — `internal/domain`. Сборка зависимостей — `cmd/data-collector/main.go`.

## Запуск

```bash
make run                # HTTP_ADDR по умолчанию :8081
make test               # юнит-тесты, внешние сервисы не нужны
make test-integration   # против живых mock-api и Postgres: из корня — `make up && make test-integration`
make swagger            # перегенерировать docs/ после правки аннотаций
```

| Переменная | По умолчанию | Что это |
|---|---|---|
| `HTTP_ADDR` | `:8081` | адрес HTTP-сервера |
| `DATABASE_URL` | — | Postgres для `raw_collections`. Не задан — сборы хранятся в памяти, в лог пишется предупреждение |
| `MOCK_API_URL` | `http://localhost:8090` | корень эмулятора внешних систем |
| `MOCK_API_TIMEOUT` | `5s` | таймаут одного запроса к внешней системе |
| `COLLECT_TIMEOUT` | `8s` | таймаут всего сбора по анкете |
| `OWN_BANK_BIK` | `044525999` | БИК нашего банка: по нему проверяются блокировки ФНС |
| `BANK_API_URL` | `http://localhost:8090/bank/v1` | корень старого API банка (`/check-mock`) |
| `BANK_API_TIMEOUT` | `5s` | таймаут одного запроса к старому API банка |

В docker-compose `DATABASE_URL` и `MOCK_API_URL` уже заданы.

## API

Swagger UI: http://localhost:8081/swagger/index.html (спецификация — `/swagger/doc.json`).

Спецификацию генерирует [swaggo](https://github.com/swaggo/swag) из аннотаций `@Summary`, `@Param`, `@Router` и т. д. над хендлерами и из `@title` в `main.go`. Результат лежит в `docs/` и коммитится, поэтому для сборки swag не нужен. Поменял ручку или DTO — запусти `make swagger` и закоммить `docs/` вместе с кодом.

### `POST /api/v1/questionnaires` — анкета и сбор данных о компании

Ручка делает четыре вещи:
1. Проверяет анкету и данные для сбора. Ошибки обеих проверок возвращаются сразу, одним ответом.
2. Сохраняет анкету.
3. Опрашивает все внешние системы через `CollectionService`. Системы опрашиваются параллельно, кроме hh.ru: он ждёт название компании из ЕГРЮЛ.
4. Пишет одну строку в `raw_collections` и отвечает `201`.

В ответе — только состояние по каждой системе, сами данные лежат в БД.

Готовые тела запросов для сценариев A–E — в [examples/](examples/):

```bash
curl -s -X POST localhost:8081/api/v1/questionnaires \
  -H 'Content-Type: application/json' -d @examples/scenario-a.json
```

#### Тело запроса

Обязательны только `age`, `marital_status`, `children_count` и `inn`. Без остальных полей часть систем пропускается (`SKIPPED`), и в `reason` написано, чего не хватило.

| Поле | Что это | Без него |
|---|---|---|
| `age`, `marital_status`, `children_count` | анкета заёмщика: возраст 18–100, `SINGLE`/`MARRIED`/`DIVORCED`/`DIVORCED_MULTIPLE`, детей 0–20 | `400` |
| `inn` | ИНН компании (10 цифр) или ИП (12) | `400` |
| `director` | руководитель: `inn` (12 цифр), `last_name`, `first_name`, `middle_name`, `birth_date` (ГГГГ-ММ-ДД). У ИП — сам предприниматель | нет данных физлица: БКИ по руководителю, ФССП, Федресурс, Госуслуги, дисквалификация, санкции по человеку |
| `consents.bki_company` | согласие компании на БКИ: `id`, `signed_at`, `valid_until` | нет отчёта БКИ по компании |
| `consents.bki_person` | согласие руководителя на БКИ (нужен `director`) | нет отчёта БКИ по руководителю |
| `consents.tax` | согласие на налоговую отчётность, плюс `scope`: `DECLARATIONS`, `ENS_BALANCE` | `TAX_REPORTS` пропущен |
| `other_bank_biks` | БИК банков, где у клиента ещё есть счета (до 10) | мультибанкинг пропущен; блокировки ФНС проверяются только по нашему банку |
| `marketplace_tokens` | `{"wildberries": "...", "ozon": "..."}` — токены продавца | маркетплейсы пропущены |
| `ofd_token` | токен ОФД | онлайн-кассы пропущены |
| `collateral.cadastral_numbers` | недвижимость в залог | ЕГРН пропущен |
| `collateral.vins`, `collateral.serials` | транспорт и техника в залог (до 20 объектов всего) | в реестре залогов только поиск по ИНН компании |
| `poa_number`, `representative_inn` | МЧД представителя, если заявку подаёт не директор | доверенности ищутся только по ИНН компании |

Токены передаются внешним системам в заголовках. В БД, журнал запросов и ответ API они не попадают.

#### Ответ `201`

```json
{
  "id": "qst_01M5M4DMX8M0AX3Y4E3CMS03J1",
  "age": 42, "marital_status": "MARRIED", "children_count": 2,
  "created_at": "2026-10-23T10:20:31Z",
  "collection": {
    "id": "col_01M5M4DMX8GFE897A8EMFKSC4D",
    "inn": "7799000012",
    "started_at": "2026-10-23T10:20:31Z", "finished_at": "2026-10-23T10:20:31Z",
    "sources": [
      { "source": "CORE_BANKING", "state": "DONE", "calls": 5, "failed_calls": 0, "duration_ms": 6 },
      { "source": "EGRN", "state": "SKIPPED", "reason": "нет недвижимости в залог collateral.cadastral_numbers",
        "calls": 0, "failed_calls": 0, "duration_ms": 0 }
    ]
  }
}
```

Значения `state`:
- `DONE` — все запросы к системе успешны;
- `PARTIAL` — часть запросов не удалась;
- `FAILED` — ни одного успешного ответа;
- `SKIPPED` — не хватило входных данных.

Отказ системы — не ошибка запроса: ответ всё равно `201`, в логе предупреждение.

Ошибки:
- `400` — битый JSON, нет обязательного поля или неверный тип;
- `422` — нарушено правило анкеты или неверный формат данных для сбора (ИНН, БИК, VIN, кадастровый номер, даты, номер МЧД).

Формат ошибки — `Problem` с `code: VALIDATION_FAILED` и списком `errors[]`, например `{"field": "other_bank_biks[0]", ...}`.

#### Таблица `raw_collections`

Одна строка — один сбор по анкете, только вставка. Служебные колонки:
- `id` (`col_…`), `questionnaire_id`, `inn`, `director_inn`;
- `request` — входные данные без токенов;
- `sources` — по каждой системе: `state`, `reason` и все HTTP-запросы со статусами и временем;
- `started_at`, `finished_at`.

Дальше по колонке `jsonb` на систему. Внутри колонки лежат ответы эндпоинтов как есть, по ключам запросов. `NULL` значит, что данных нет (система пропущена или не ответила). Неудавшийся запрос внутри системы — `null` под своим ключом.

| Колонка | Система | Ключи внутри |
|---|---|---|
| `core_banking` | АБС банка | `client`, `turnover`, `transactions` (массив страниц), `loans`, `restrictions` |
| `fns` | ФНС | `egrul`, `pb_search`, `pb`, `bo_search`, `bo_bfo`, `account_blocks.{БИК}`, `charter` (не у ИП), `rmsp`, `disqualified` |
| `bki` | БКИ | `legal`, `person` |
| `compliance` | ЗСК, санкции | `zsk`, `screening` (компания, руководитель, представитель) |
| `fssp` | ФССП | `company`, `director` |
| `kad` | арбитраж | `cases` (массив страниц) |
| `fedresurs` | Федресурс | `company_messages`, `director_bankruptcy` |
| `m4d` | МЧД | `search`, `poa` |
| `tax_reports` | налоговая отчётность | `report` |
| `pledge_registry` | реестр залогов | `by_pledgor`, `by_vin.{VIN}`, `by_serial.{номер}` |
| `egrn` | ЕГРН | `{кадастровый номер}` |
| `gosuslugi` | Госуслуги | `director` |
| `open_banking` | мультибанкинг | `{БИК}.consent`, `{БИК}.accounts`, `{БИК}.balances.{счёт}`, `{БИК}.transactions.{счёт}` |
| `marketplaces` | WB, Ozon | `{wildberries\|ozon}.profile`, `.sales`, `.payouts` |
| `ofd` | ОФД | `kkt`, `receipts` |
| `zakupki` | закупки | `contracts`, `rnp` |
| `signals` | сигналы | `inspections`, `trademarks`, `news` |
| `cbr` | ЦБ | `key_rate` — SOAP-ответ строкой (XML как есть) |
| `hh` | hh.ru | `employers`, `vacancies` (только у работодателя с тем же названием) |

Что означают данные каждой системы — в [docs/mock-api-guide.md](../docs/mock-api-guide.md).

```sql
-- последний сбор по компании: балл БКИ, ЗСК и состояние систем
SELECT bki->'legal'->'score'->>'value'    AS bki_score,
       compliance->'zsk'->>'risk_level'   AS zsk,
       jsonb_path_query_array(sources, '$[*] ? (@.state != "DONE").source') AS not_done
FROM raw_collections WHERE inn = '7799000037' ORDER BY created_at DESC LIMIT 1;
```

### `GET /api/v1/check-mock?inn=7701234567`

Старая ручка. Параллельно опрашивает пять систем старого мока банка (`/bank/v1`) и возвращает их ответы как есть:
- `accounts` — расчётный счёт;
- `credits` — кредиты;
- `acquiring` — эквайринг;
- `salary_project` — зарплатный проект;
- `card_index` — картотека.

В `sources` — итог по каждой системе: `DONE` или `FAILED` с текстом ошибки и временем ответа. Если система не ответила, её секция `null`, а ответ всё равно `200`.

Ошибки: `400` — нет `inn`; `422` — `inn` не из 10 или 12 цифр.

```bash
curl -s 'localhost:8081/api/v1/check-mock?inn=7701234567'
```

### `GET /healthz`

Проверка, что сервис поднят.
