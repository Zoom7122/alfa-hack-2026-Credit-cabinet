# data-collector

Модуль сбора данных о заёмщике для передачи в скоринг. Независимый Go-модуль: свой `go.mod`, ничего не импортирует из остального репозитория.

Стек: Go, gin, pgx, Kafka ([IBM/sarama](https://github.com/IBM/sarama)).
- **Анкеты** пока хранятся в памяти процесса (заглушка за интерфейсом `service.Repository`).
- **Задачи на сбор**, **outbox** и **сырые ответы внешних систем** — в Postgres: таблицы `collection_tasks`, `outbox`, `raw_collections`. Схема — в [db/](../db/), миграции `00002_raw_collections.sql` и `00003_collection_tasks.sql`.

## Как идёт сбор

Сбор асинхронный, по схеме transactional outbox:

```
POST /questionnaires      ──tx──▶ collection_tasks (PENDING) + outbox (событие collection.requested)
data-collector-publisher  ──────▶ outbox → Kafka, топик data-collector.collection-tasks
data-collector-worker     ◀────── Kafka: задача RUNNING → опрос всех моков → raw_collections, задача COMPLETED
GET /collections/{id}     ──────▶ состояние задачи и итог по каждой системе
```

- Задача и событие пишутся **одной транзакцией**: событие не теряется и не уходит без задачи.
- В Kafka уходит только id задачи (+ ИНН и анкета). Входные данные worker берёт из `collection_tasks`. Токены маркетплейсов и ОФД лежат в `collection_tasks.secrets`, пока задача не завершена, потом стираются.
- Доставка **at-least-once**, worker к повторам готов. Задачу берёт только `PENDING` или `RUNNING`, а результат пишется один раз: `raw_collections.task_id` уникален.
- Worker упал посреди сбора — оффсет не закоммичен, сообщение придёт снова, `attempts` вырастет. Задачу, которую брали уже `WORKER_MAX_ATTEMPTS` раз, worker переводит в `FAILED`.
- Kafka недоступна — API продолжает принимать анкеты, события копятся в `outbox`, worker раз в 2 с пробует войти в группу. Когда брокер вернётся, всё дойдёт само.
- Системы, для которых в анкете нет данных (согласия, токена, залога, банков, руководителя), **не опрашиваются**. Их список — в `skipped_sources` сразу при создании задачи. В `raw_collections` их колонка `NULL`, в `sources` — `SKIPPED` с причиной.

Статусы задачи: `PENDING` (ждёт worker) → `RUNNING` (системы опрашиваются) → `COMPLETED` (сбор сохранён) или `FAILED` (кончились попытки или сбор упал, причина в `error`).

## Бинари

Один модуль и один Docker-образ, три бинаря:

| Бинарь | Пакет | Что делает |
|---|---|---|
| `data-collector` | `cmd/data-collector` | HTTP API: анкеты, состояние сбора, `/check-mock` |
| `data-collector-publisher` | `cmd/outbox-publisher` | переносит события из `outbox` в Kafka. Несколько реплик безопасны: события блокируются `FOR UPDATE SKIP LOCKED` |
| `data-collector-worker` | `cmd/worker` | читает топик в consumer group `data-collector-worker`, опрашивает системы, пишет результат. Реплики делят партиции топика |

Топик создают publisher и worker при старте (`KAFKA_TOPIC_PARTITIONS` партиций). Новая группа читает топик с начала, так что задачи, созданные до первого запуска worker'а, не теряются.

## Слои

| Слой | Пакет | Что делает |
|---|---|---|
| Приём запросов | `internal/transport/http` | все эндпоинты — в `router.go`; разбор запросов, ответы и ошибки (RFC 9457, `application/problem+json`) |
| Бизнес-логика | `internal/service` | `QuestionnaireService` — правила анкеты, после сохранения ставит сбор в очередь; `CollectionService` — проверка данных для сбора, постановка задачи (`Enqueue`) и её состояние (`Get`); `InternalDataService` — старый опрос систем банка для `/check-mock` |
| Сбор | `internal/worker` | обработка сообщения о задаче: `RUNNING` → опрос систем → `COMPLETED` |
| Outbox | `internal/outbox` | publisher: пачка событий из `outbox` → брокер |
| Kafka | `internal/kafka` | sarama: настройки клиента, создание топика, синхронный продюсер, цикл consumer group с ручным коммитом оффсетов |
| Хранилище | `internal/repository/postgres` | `collection_tasks` + `outbox` + `raw_collections` (pgx) |
| | `internal/repository/memory` | анкеты; задачи на сбор для тестов |
| Внешние системы | `internal/client/mockapi` | HTTP-клиент ко всем 19 системам эмулятора [mock-api](../mock-api/): какие эндпоинты вызвать и куда положить ответ |
| | `internal/client/bank` | старый клиент `/bank/v1` для `/check-mock` |

Сущности — `internal/domain`, события — `internal/events`. Сборка зависимостей — `cmd/*/main.go`.

## Запуск

```bash
make run                # API, HTTP_ADDR по умолчанию :8081
make run-publisher      # outbox → Kafka
make run-worker         # Kafka → сбор → raw_collections
make test               # юнит-тесты, внешние сервисы не нужны
make test-integration   # против живых mock-api, Postgres и Kafka: из корня — `make up && make test-integration`
make swagger            # перегенерировать docs/ после правки аннотаций
```

Всем трём бинарям нужен `DATABASE_URL` (Postgres со схемой из модуля db).

| Переменная | Кому | По умолчанию | Что это |
|---|---|---|---|
| `DATABASE_URL` | всем | — | Postgres: задачи на сбор, outbox, `raw_collections`. Обязательна |
| `KAFKA_TOPIC` | всем | `data-collector.collection-tasks` | топик задач на сбор |
| `HTTP_ADDR` | API | `:8081` | адрес HTTP-сервера |
| `BANK_API_URL` | API | `http://localhost:8090/bank/v1` | корень старого API банка (`/check-mock`) |
| `BANK_API_TIMEOUT` | API | `5s` | таймаут одного запроса к старому API банка |
| `KAFKA_BROKERS` | publisher, worker | `localhost:9094` | брокеры через запятую |
| `KAFKA_TOPIC_PARTITIONS` | publisher, worker | `3` | партиций у топика, если его надо создать |
| `PUBLISHER_POLL_INTERVAL` | publisher | `500ms` | пауза, когда в outbox пусто. При ошибках подряд пауза удваивается, но не дольше 5 с |
| `PUBLISHER_BATCH_SIZE` | publisher | `100` | событий за один круг |
| `PUBLISHER_PUBLISH_TIMEOUT` | publisher | `10s` | на отправку пачки в Kafka; не успели — события остаются в outbox |
| `KAFKA_GROUP` | worker | `data-collector-worker` | consumer group |
| `WORKER_CONCURRENCY` | worker | `4` | сколько задач выполняется одновременно |
| `WORKER_MAX_ATTEMPTS` | worker | `3` | сколько раз брать задачу, прежде чем перевести её в `FAILED` |
| `MOCK_API_URL` | worker | `http://localhost:8090` | корень эмулятора внешних систем |
| `MOCK_API_TIMEOUT` | worker | `5s` | таймаут одного запроса к внешней системе |
| `COLLECT_TIMEOUT` | worker | `8s` | таймаут всего сбора по задаче |
| `OWN_BANK_BIK` | worker | `044525999` | БИК нашего банка: по нему проверяются блокировки ФНС |

В docker-compose всё нужное уже задано.

## API

Swagger UI: http://localhost:8081/swagger/index.html (спецификация — `/swagger/doc.json`).

Спецификацию генерирует [swaggo](https://github.com/swaggo/swag) из аннотаций `@Summary`, `@Param`, `@Router` и т. д. над хендлерами и из `@title` в `main.go`. Результат лежит в `docs/` и коммитится, поэтому для сборки swag не нужен. Поменял ручку или DTO — запусти `make swagger` и закоммить `docs/` вместе с кодом.

### `POST /api/v1/questionnaires` — анкета и задача на сбор данных о компании

Ручка делает три вещи:
1. Проверяет анкету и данные для сбора. Ошибки обеих проверок возвращаются сразу, одним ответом.
2. Сохраняет анкету.
3. Одной транзакцией создаёт задачу на сбор (`PENDING`) и событие о ней в `outbox`, затем отвечает `202`. В заголовке `Location` — ссылка на задачу.

Системы опрашивает worker. Они идут параллельно, кроме hh.ru: ему нужно название компании из ЕГРЮЛ. Состояние сбора — `GET /api/v1/collections/{id}`.

Готовые тела запросов для сценариев A–E — в [examples/](examples/):

```bash
curl -si -X POST localhost:8081/api/v1/questionnaires \
  -H 'Content-Type: application/json' -d @examples/scenario-a.json   # 202, Location: /api/v1/collections/tsk_…
curl -s localhost:8081/api/v1/collections/tsk_…                       # PENDING → RUNNING → COMPLETED
```

#### Тело запроса

Обязательны только `age`, `marital_status`, `children_count` и `inn`. Без остальных полей часть систем не опрашивается: они сразу перечислены в `skipped_sources`, а после сбора у них `SKIPPED` и в `reason` написано, чего не хватило.

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

Токены передаются внешним системам в заголовках. В `raw_collections`, журнал запросов, Kafka и ответ API они не попадают. Пока задача не завершена, они лежат в `collection_tasks.secrets`, потом стираются.

#### Ответ `202`

`Location: /api/v1/collections/tsk_01M5PTAWESP9MRX01JRKSGXX6Z`

```json
{
  "id": "qst_01M5PTAWESYK3Z987GRPZRM2J1",
  "age": 42, "marital_status": "MARRIED", "children_count": 2,
  "created_at": "2026-10-24T11:22:01Z",
  "collection": {
    "id": "tsk_01M5PTAWESP9MRX01JRKSGXX6Z",
    "questionnaire_id": "qst_01M5PTAWESYK3Z987GRPZRM2J1",
    "inn": "7799000012",
    "status": "PENDING",
    "attempts": 0,
    "skipped_sources": ["EGRN", "MARKETPLACES"],
    "created_at": "2026-10-24T11:22:01Z"
  }
}
```

`skipped_sources` — системы, для которых в анкете нет данных: они не опрашиваются. Для полной анкеты список пустой.

Ошибки:
- `400` — битый JSON, нет обязательного поля или неверный тип;
- `422` — нарушено правило анкеты или неверный формат данных для сбора (ИНН, БИК, VIN, кадастровый номер, даты, номер МЧД).

Формат ошибки — `Problem` с `code: VALIDATION_FAILED` и списком `errors[]`, например `{"field": "other_bank_biks[0]", ...}`.

### `GET /api/v1/collections/{id}` — состояние сбора

`id` — id задачи (`tsk_…`) из ответа POST или заголовка `Location`. Пока задача `PENDING` или `RUNNING`, в ответе только статус. У `COMPLETED` есть `raw_collection_id` и `sources` — итог по каждой системе:

```json
{
  "id": "tsk_01M5PTAWESP9MRX01JRKSGXX6Z",
  "questionnaire_id": "qst_01M5PTAWESYK3Z987GRPZRM2J1",
  "inn": "7799000012",
  "status": "COMPLETED",
  "attempts": 1,
  "skipped_sources": ["EGRN", "MARKETPLACES"],
  "created_at": "2026-10-24T11:22:01Z",
  "started_at": "2026-10-24T11:22:01Z",
  "finished_at": "2026-10-24T11:22:01Z",
  "raw_collection_id": "col_01M5PTAWNHA63H1ETMENDGX0VS",
  "sources": [
    { "source": "CORE_BANKING", "state": "DONE", "calls": 5, "failed_calls": 0, "duration_ms": 6 },
    { "source": "EGRN", "state": "SKIPPED", "reason": "нет недвижимости в залог collateral.cadastral_numbers",
      "calls": 0, "failed_calls": 0, "duration_ms": 0 }
  ]
}
```

Значения `state` у системы:
- `DONE` — все запросы к системе успешны;
- `PARTIAL` — часть запросов не удалась;
- `FAILED` — ни одного успешного ответа;
- `SKIPPED` — не хватило входных данных.

Отказ системы — не сбой задачи: она всё равно `COMPLETED`, в логе worker'а предупреждение. У `FAILED`-задачи причина — в `error`.

Ошибки: `404` (`code: NOT_FOUND`) — задачи нет.

#### Таблица `collection_tasks`

Одна строка — одна задача на сбор по анкете:
- `id` (`tsk_…`), `questionnaire_id`, `inn`;
- `status` — `PENDING`, `RUNNING`, `COMPLETED`, `FAILED`; `attempts` — сколько раз worker брал задачу; `error` — почему `FAILED`;
- `request` — входные данные без токенов, как `raw_collections.request`;
- `secrets` — токены маркетплейсов и ОФД для worker'а; после завершения `NULL`;
- `skipped_sources` — системы без входных данных (`text[]`), заполняется при создании;
- `created_at`, `started_at`, `finished_at`, `updated_at`.

#### Таблица `outbox`

События для Kafka: `topic`, `key` (id задачи), `payload` (`{"event": "collection.requested", "task_id", "questionnaire_id", "inn", "created_at"}`). Пока событие не опубликовано, `published_at` — `NULL`. При неудачной публикации растёт `attempts`, в `last_error` — причина.

#### Таблица `raw_collections`

Одна строка — один сбор по задаче, только вставка. Служебные колонки:
- `id` (`col_…`), `task_id` (уникален: один результат на задачу), `questionnaire_id`, `inn`, `director_inn`;
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

-- очередь: что ждёт worker, что упало
SELECT id, status, attempts, error, skipped_sources, created_at
FROM collection_tasks WHERE status <> 'COMPLETED' ORDER BY created_at;

-- события, которые publisher ещё не доставил в Kafka
SELECT id, key, attempts, last_error, created_at FROM outbox WHERE published_at IS NULL ORDER BY id;
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

## Финансовые документы

Загрузка оригиналов, проверка подписи через адаптер и извлечение КУДиР/ГИРБО выполняются теми же API/publisher/worker. Контракт, настройки и реальные тестовые образцы описаны в [financial-documents.md](../docs/financial-documents.md).
