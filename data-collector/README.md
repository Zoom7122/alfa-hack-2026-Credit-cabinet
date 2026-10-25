# data-collector

Модуль сбора данных о заёмщике для передачи в скоринг. Независимый Go-модуль: свой `go.mod`, ничего не импортирует из остального репозитория.

Стек: Go, gin, pgx, Kafka ([IBM/sarama](https://github.com/IBM/sarama)).
- **Заявитель, компания и заявка** — в Postgres: `users`, `companies`, `company_members`, `applications`, журнал `application_events`.
- **Анкета** — в `applications.form`; **документы** — строки `documents`, сами файлы — на диске (в docker — volume `documents`).
- **Задачи на сбор**, **outbox** и **сырые ответы внешних систем** — `collection_tasks`, `outbox`, `raw_collections`.

Схема — в [db/](../db/), миграции `00001`–`00004`.

## Как идёт работа

Два шага у клиента, остальное асинхронно (transactional outbox):

```
POST /applicants                      ──tx──▶ users + companies + company_members + applications (DRAFT)
POST /applications/{id}/questionnaire ──────▶ файлы → staging на диске → проверка пар .sig → /data/documents/{app}/{doc}.{ext}
                                      ──tx──▶ documents + applications (SUBMITTED, анкета, сумма, срок)
                                              + collection_tasks + outbox: collection.requested, documents.submitted
data-collector-publisher              ──────▶ outbox → Kafka: data-collector.collection-tasks, data-collector.documents
data-collector-worker                 ◀────── задача RUNNING, заявка ENRICHING → опрос всех моков → raw_collections,
                                              задача COMPLETED, заявка VERIFYING, реквизиты компании из ЕГРЮЛ
будущие worker'ы подписи и OCR        ◀────── data-collector.documents (сейчас без потребителей)
GET /applications/{id}, /collections/{id} ──▶ статус заявки и итог сбора по каждой системе
```

- Регистрация, отправка анкеты и завершение сбора — каждое **одной транзакцией**: событие не теряется и не уходит без данных.
- В Kafka уходят только id (+ ИНН, анкета, ссылки на файлы). Входные данные worker берёт из `collection_tasks`. Токены маркетплейсов и ОФД лежат в `collection_tasks.secrets`, пока задача не завершена, потом стираются.
- Доставка **at-least-once**, worker к повторам готов. Задачу берёт только `PENDING` или `RUNNING`, а результат пишется один раз: `raw_collections.task_id` уникален.
- Worker упал посреди сбора — оффсет не закоммичен, сообщение придёт снова, `attempts` вырастет. Задачу, которую брали уже `WORKER_MAX_ATTEMPTS` раз, worker переводит в `FAILED`.
- Kafka недоступна — API продолжает принимать анкеты, события копятся в `outbox`, worker раз в 2 с пробует войти в группу. Когда брокер вернётся, всё дойдёт само.
- Системы, для которых в анкете нет данных (согласия, токена, залога, банков, руководителя), **не опрашиваются**. Их список — в `skipped_sources` сразу при создании задачи. В `raw_collections` их колонка `NULL`, в `sources` — `SKIPPED` с причиной.

Статусы заявки (FSM из [docs/fsm.md](../docs/fsm.md), модуль ставит первые четыре): `DRAFT` (после регистрации) → `SUBMITTED` (анкета отправлена) → `ENRICHING` (worker взял задачу) → `VERIFYING` (данные собраны). Каждый переход — строка в `application_events`. Если сбор не удался, заявка остаётся в `ENRICHING`, в журнале — `enrich.failed`.

Статусы задачи на сбор: `PENDING` → `RUNNING` → `COMPLETED` или `FAILED` (причина в `error`).

## Бинари

Один модуль и один Docker-образ, три бинаря:

| Бинарь | Пакет | Что делает |
|---|---|---|
| `data-collector` | `cmd/data-collector` | HTTP API: регистрация, анкета с файлами, заявки, состояние сбора, `/check-mock` |
| `data-collector-publisher` | `cmd/outbox-publisher` | переносит события из `outbox` в Kafka (оба топика). Несколько реплик безопасны: события блокируются `FOR UPDATE SKIP LOCKED` |
| `data-collector-worker` | `cmd/worker` | читает топик задач в consumer group `data-collector-worker`, опрашивает системы, пишет результат. Реплики делят партиции топика |

Топики создают publisher и worker при старте (`KAFKA_TOPIC_PARTITIONS` партиций). Новая группа читает топик с начала, так что задачи, созданные до первого запуска worker'а, не теряются.

## Слои

| Слой | Пакет | Что делает |
|---|---|---|
| Приём запросов | `internal/transport/http` | все эндпоинты — в `router.go`; разбор JSON и multipart, ответы и ошибки (RFC 9457, `application/problem+json`) |
| Бизнес-логика | `internal/service` | `ApplicationService` — регистрация заявителя и чтение заявки; `QuestionnaireService` — правила анкеты и файлов, отправка заявки; `CollectionService` — проверка данных для сбора, задача на сбор и её состояние; `InternalDataService` — старый опрос систем банка для `/check-mock` |
| Сбор | `internal/worker` | обработка сообщения о задаче: `RUNNING` → опрос систем → `COMPLETED`, реквизиты компании |
| Outbox | `internal/outbox` | publisher: пачка событий из `outbox` → брокер |
| Kafka | `internal/kafka` | sarama: настройки клиента, создание топика, синхронный продюсер, цикл consumer group с ручным коммитом оффсетов |
| Файлы | `internal/storage` | файлы документов на диске: приём потоком во временную папку, перенос на место, откат |
| Хранилище | `internal/repository/postgres` | заявки с компанией, пользователем и документами; задачи, outbox, `raw_collections` (pgx) |
| | `internal/repository/memory` | то же в памяти — для тестов |
| Внешние системы | `internal/client/mockapi` | HTTP-клиент ко всем 19 системам эмулятора [mock-api](../mock-api/): какие эндпоинты вызвать, куда положить ответ, реквизиты из ЕГРЮЛ |
| | `internal/client/bank` | старый клиент `/bank/v1` для `/check-mock` |

Сущности — `internal/domain`, события — `internal/events`. Сборка зависимостей — `cmd/*/main.go`.

## Запуск

```bash
make run                # API, HTTP_ADDR по умолчанию :8081
make run-publisher      # outbox → Kafka
make run-worker         # Kafka → сбор → raw_collections
make test               # юнит-тесты, внешние сервисы не нужны
make test-integration   # против живых mock-api, Postgres и Kafka: из корня — `make up && make test-integration`
make swagger            # перегенерировать docs/ после правки аннотаций или examples/
```

Всем трём бинарям нужен `DATABASE_URL` (Postgres со схемой из модуля db).

| Переменная | Кому | По умолчанию | Что это |
|---|---|---|---|
| `DATABASE_URL` | всем | — | Postgres: заявки, задачи на сбор, outbox, `raw_collections`. Обязательна |
| `KAFKA_TOPIC` | всем | `data-collector.collection-tasks` | топик задач на сбор |
| `KAFKA_DOCUMENTS_TOPIC` | API, publisher | `data-collector.documents` | топик событий о документах |
| `HTTP_ADDR` | API | `:8081` | адрес HTTP-сервера |
| `DOCUMENTS_DIR` | API | `data/documents` (в образе — `/data/documents`) | куда класть файлы документов |
| `MAX_FILE_SIZE_MB` | API | `20` | лимит одного файла |
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

В docker-compose всё нужное уже задано. Файлы лежат в volume `documents`: образ distroless работает от `nonroot` (uid 65532), папка в образе принадлежит ему, и новый volume берёт этого владельца. Посмотреть файлы (в distroless нет shell):

```bash
docker run --rm -v alfa-hack-2026-credit-cabinet_documents:/d postgres:16-alpine ls -R /d
```

## API

Swagger UI: http://localhost:8081/swagger/index.html (спецификация — `/swagger/doc.json`). Запросы готовы к отправке:
1. `POST /applicants` → Try it out → Execute: в теле — заявитель сценария A, в ответе id черновика `app_…`;
2. `POST /applications/{id}/questionnaire` → Try it out → id заявки: в `questionnaire` уже подставлена анкета сценария A (без файлов), файлы — `files` → Add file item, по полю на файл (документ и его `.sig`) → Execute → `202`.

Примеры сценариев A–E (регистрация и анкета) — в описаниях этих ручек.

Спецификацию генерирует [swaggo](https://github.com/swaggo/swag) из аннотаций `@Summary`, `@Param`, `@Router` и т. д. над хендлерами и из `@title` в `main.go`. Примеры в описаниях `make swagger` собирает из `examples/` в `docs/markdown/` (`tools/swaggerexamples`), анкета в поле `questionnaire` — `extensions(x-example=…)` в аннотации; тест `TestSwagger*` сверяет их с `examples/`. Результат лежит в `docs/` и коммитится, поэтому для сборки swag не нужен. Поменял ручку, DTO или пример — запусти `make swagger` и закоммить `docs/` вместе с кодом.

Готовые запросы для сценариев A–E — в [examples/](examples/): `scenario-X.applicant.json` (регистрация) и `scenario-X.json` (анкета); файлы для сценария A — в `examples/files/`.

```bash
curl -s -X POST localhost:8081/api/v1/applicants -H 'Content-Type: application/json' \
  -d @examples/scenario-a.applicant.json                          # 201, id заявки app_…
curl -si -X POST localhost:8081/api/v1/applications/app_…/questionnaire \
  -F 'questionnaire=<examples/scenario-a.json' \
  -F files=@examples/files/balance.pdf -F files=@examples/files/balance.pdf.sig   # 202, Location: /api/v1/collections/tsk_…
curl -s localhost:8081/api/v1/applications/app_…                 # SUBMITTED → ENRICHING → VERIFYING
curl -s localhost:8081/api/v1/collections/tsk_…                  # PENDING → RUNNING → COMPLETED
```

### `POST /api/v1/applicants` — кто оформляет кредит и за какую компанию

Одной транзакцией создаёт или находит:
- **пользователя** (`users`, роль `CLIENT`): с тем же ИНН физлица — тот же пользователь, ФИО обновляется; без ИНН — новый;
- **компанию** (`companies`): с тем же ИНН — та же, вместе с собранными данными (`already_known: true`); новой тип ставится по ИНН, имя — из запроса или «ИНН …» до сбора, после сбора — из ЕГРЮЛ;
- **связь** (`company_members`): роль и `can_sign`;
- **черновик заявки** (`applications`, `DRAFT`, номер `A-2026-000123`): если у этого пользователя по этой компании черновик уже есть — возвращается он.

```json
{ "representative": { "full_name": "Литвинова Марина Олеговна", "inn": "779900010333", "role": "ACCOUNTANT" },
  "company": { "inn": "7799000012", "name": "ООО «Северный ветер»" } }
```

Обязательны `representative.full_name`, `representative.role`, `company.inn`. Роли (`role`) — пункты формы:

| Значение | Кто | `can_sign` |
|---|---|---|
| `OWNER` | собственник бизнеса | у ИП — да, у ЮЛ — по доверенности |
| `DIRECTOR` | генеральный директор | да |
| `CFO` / `ACCOUNTANT` | финансовый директор или бухгалтер | по доверенности |
| `FINANCE_EMPLOYEE` | сотрудник, отвечающий за финансирование | по доверенности |
| `BANKING_EMPLOYEE` | сотрудник, работающий с банковскими продуктами | по доверенности |
| `AUTHORIZED_EMPLOYEE` | иной сотрудник, уполномоченный оформлять заявку от имени компании | по доверенности |

Ответ `201` — заявка (как в `GET /applications/{id}`, плюс `applicant.already_known`), `Location: /api/v1/applications/{id}`. Ошибки: `400` — битый JSON, нет обязательного поля; `422` — неверный ИНН (компании — 10 или 12 цифр, физлица — 12) или роль.

### `POST /api/v1/applications/{id}/questionnaire` — анкета и документы

`multipart/form-data`: часть `questionnaire` — JSON анкеты, части `files` — файлы. Анкету без файлов можно отправить и как `application/json`. По одной заявке анкету принимают один раз.

Одной транзакцией: строки `documents`, анкета в `applications.form`, продукт, сумма и срок в `applications`, заявка `DRAFT → SUBMITTED` (событие `application.submitted`), задача на сбор со ссылками `application_id` и `document_ids`, события в `outbox` — о задаче и о документах. Ответ `202`, `Location: /api/v1/collections/{task_id}`.

#### Анкета

ИНН компании берётся из заявки. Обязательны только `age`, `marital_status`, `children_count`. Без остальных полей часть систем не опрашивается: они сразу перечислены в `skipped_sources`, а после сбора у них `SKIPPED` и в `reason` написано, чего не хватило.

| Поле | Что это | Без него |
|---|---|---|
| `age`, `marital_status`, `children_count` | анкета заёмщика: возраст 18–100, `SINGLE`/`MARRIED`/`DIVORCED`/`DIVORCED_MULTIPLE`, детей 0–20 | `400` |
| `product_code`, `requested.amount`, `requested.term_months`, `purpose.code`, `purpose.description` | продукт, сумма (₽) и срок, цель кредита (`WORKING_CAPITAL`, `EQUIPMENT`, `VEHICLE`, `REAL_ESTATE`, `REFINANCING`, `BUSINESS_EXPANSION`, `TENDER`, `OTHER`) — пишутся в заявку | заявка без суммы и срока |
| `documents` | `[{"file_name": "balance.pdf", "type": "FINANCIAL_STATEMENTS"}]` — тип документа по имени файла (`DocumentType` из api/openapi.yaml) | тип `OTHER`: позже его определит распознавание |
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

Токены передаются внешним системам в заголовках. В `applications.form`, `raw_collections`, журнал запросов, Kafka и ответ API они не попадают. Пока задача не завершена, они лежат в `collection_tasks.secrets`, потом стираются.

#### Файлы

- До **10 файлов**, считая подписи; каждый до **20 МБ** (`MAX_FILE_SIZE_MB`), всё тело — до 10 × лимит + 2 МБ.
- Форматы: `pdf`, `docx`, `xlsx`, `jpg`/`jpeg`, `png`, `xml`; подписи — `sig`, `p7s`. Имена в запросе уникальны без учёта регистра, путь из имени отбрасывается.
- Файл приходит **целым** или **парой «документ + открепленная подпись»**: подпись `X.sig` (`X.p7s`) относится к документу `X` (`balance.pdf` + `balance.pdf.sig`); если такого нет, а у `X` нет расширения, — к единственному документу с той же основой (`balance.pdf` + `balance.sig`).
- Подпись без пары принимается как **присоединённая** (документ внутри контейнера, см. [docs/e-signature.md](../docs/e-signature.md)).
- `422`: больше 10 файлов, недопустимый формат, пустой файл, файл больше лимита, два файла с одним именем, две подписи к одному документу, подпись к подписи, неоднозначная подпись (`a.pdf` + `a.xlsx` + `a.sig`), тип в `documents` для чужого файла.

Документы для будущих worker'ов: у документа с открепленной подписью, у самой подписи и у присоединённой подписи `signature_state = PENDING`, у остальных — `NOT_SIGNED` (встроенные подписи PDF/XML worker подписи найдёт сам). Распознавание: `recognition.state = PENDING` у pdf, docx, xlsx, jpg, png; `NOT_APPLICABLE` у подписей и xml. Подпись — строка `type = SIGNATURE` с `signed_document_id` → документ.

Файлы пишутся потоком во временную папку `DOCUMENTS_DIR/.staging`, после проверки переносятся (`rename` на том же диске) в `DOCUMENTS_DIR/{application_id}/{document_id}.{ext}` — это `documents.storage_key`. Не прошла проверка или транзакция — файлы запроса удаляются.

#### Ответ `202`

```json
{
  "application": { "id": "app_…", "number": "A-2026-000005", "status": "SUBMITTED", "applicant": { … }, "representative": { … },
    "product_code": "WORKING_CAPITAL", "requested": { "amount": 5000000, "term_months": 24 },
    "documents": [
      { "id": "doc_…1", "type": "FINANCIAL_STATEMENTS", "file_name": "balance.pdf", "mime_type": "application/pdf",
        "size_bytes": 218, "sha256": "…", "signature_state": "PENDING", "recognition_state": "PENDING" },
      { "id": "doc_…2", "type": "SIGNATURE", "file_name": "balance.pdf.sig", "signed_document_id": "doc_…1",
        "signature_state": "PENDING", "recognition_state": "NOT_APPLICABLE", … }
    ], "collection_task_id": "tsk_…", "submitted_at": "…" },
  "questionnaire": { "id": "qst_…", "age": 42, "marital_status": "MARRIED", "children_count": 2, "created_at": "…" },
  "collection": { "id": "tsk_…", "application_id": "app_…", "inn": "7799000012", "status": "PENDING",
    "skipped_sources": ["EGRN", "MARKETPLACES"], "document_ids": ["doc_…1", "doc_…2"], … }
}
```

Ошибки:
- `400` — битый JSON или multipart, нет части `questionnaire` или обязательного поля, неверный тип;
- `404` — заявки нет;
- `409` (`INVALID_TRANSITION`) — анкету по заявке уже отправили;
- `413` (`PAYLOAD_TOO_LARGE`) — тело больше лимита;
- `415` (`UNSUPPORTED_MEDIA_TYPE`) — не `multipart/form-data` и не `application/json`;
- `422` — нарушено правило анкеты, данных для сбора (ИНН, БИК, VIN, кадастровый номер, даты, номер МЧД), заявки (сумма, срок, цель) или файлов. Все нарушения — одним ответом, например `{"field": "files[2]", "code": "second_signature"}`.

### `GET /api/v1/applications/{id}` — заявка

Номер, статус, компания (после сбора — имя и ОГРН из ЕГРЮЛ), представитель с ролью и `can_sign`, продукт, сумма и срок, цель, документы и `collection_task_id` — последняя задача на сбор. Ошибки: `404` — заявки нет.

### `GET /api/v1/collections/{id}` — состояние сбора

`id` — id задачи (`tsk_…`) из ответа анкеты или заголовка `Location`. Пока задача `PENDING` или `RUNNING`, в ответе только статус. У `COMPLETED` есть `raw_collection_id` и `sources` — итог по каждой системе:

```json
{
  "id": "tsk_01M5PTAWESP9MRX01JRKSGXX6Z",
  "application_id": "app_01M5QCPZAEN4MRS7MJ0X4KG6A3",
  "questionnaire_id": "qst_01M5PTAWESYK3Z987GRPZRM2J1",
  "inn": "7799000012",
  "status": "COMPLETED",
  "attempts": 1,
  "skipped_sources": ["EGRN", "MARKETPLACES"],
  "document_ids": ["doc_…1", "doc_…2"],
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

### События в Kafka

| Топик | Событие | Ключ | Тело | Кто читает |
|---|---|---|---|---|
| `data-collector.collection-tasks` | `collection.requested` | id задачи | `event`, `task_id`, `application_id`, `questionnaire_id`, `inn`, `created_at` | worker сбора |
| `data-collector.documents` | `documents.submitted` | id заявки | `event`, `application_id`, `company_inn`, `documents[]` (`id`, `type`, `file_name`, `mime_type`, `size_bytes`, `sha256`, `storage_key`, `signed_document_id`, `signature_state`, `recognition_state`), `created_at` | будущие worker'ы проверки подписи и OCR |

Будущим worker'ам подписи и OCR — свои consumer group на `data-collector.documents`. Файлы они читают из того же volume (read-only) по `storage_key`, результат пишут в `documents.signature_state`, `signature_reports`, `documents.recognition`. Сейчас эти worker'ы не реализованы.

### Таблицы

`users`, `companies`, `company_members`, `applications`, `application_events`, `documents` — общая схема из [docs/db-schema.md](../docs/db-schema.md). Что в них пишет модуль:
- `users` — представитель: `full_name`, `inn`, `role = CLIENT`;
- `companies` — `inn`, `type`, `name`; после сбора — `name` и `ogrn` из ЕГРЮЛ, `director_inn` из анкеты, `registry` — реквизиты ЕГРЮЛ (поля `CounterpartyPrefill`);
- `company_members` — `role`, `can_sign`;
- `applications` — номер, статус, `form` (анкета заёмщика с данными для сбора без токенов, продукт, сумма, срок, цель), `product_code`, `amount`, `term_months`, `submitted_at`;
- `application_events` — `application.created`, `application.submitted`, `enrich.started`, `enrich.completed`, `enrich.failed`;
- `documents` — файлы анкеты: имя, MIME, размер, sha256, `storage_key`, `signed_document_id`, `signature_state`, `recognition`.

#### Таблица `collection_tasks`

Одна строка — одна задача на сбор по анкете:
- `id` (`tsk_…`), `application_id`, `questionnaire_id`, `inn`;
- `document_ids` — документы, пришедшие с анкетой;
- `status` — `PENDING`, `RUNNING`, `COMPLETED`, `FAILED`; `attempts` — сколько раз worker брал задачу; `error` — почему `FAILED`;
- `request` — входные данные без токенов, как `raw_collections.request`;
- `secrets` — токены маркетплейсов и ОФД для worker'а; после завершения `NULL`;
- `skipped_sources` — системы без входных данных (`text[]`), заполняется при создании;
- `created_at`, `started_at`, `finished_at`, `updated_at`.

#### Таблица `outbox`

События для Kafka: `topic`, `key`, `payload` (тела — в таблице событий выше). Пока событие не опубликовано, `published_at` — `NULL`. При неудачной публикации растёт `attempts`, в `last_error` — причина.

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

-- заявка: статус, компания, представитель и журнал
SELECT a.number, a.status, c.name, u.full_name, m.role, m.can_sign,
       (SELECT array_agg(type ORDER BY id) FROM application_events e WHERE e.application_id = a.id) AS events
FROM applications a JOIN companies c ON c.id = a.company_id JOIN users u ON u.id = a.created_by
JOIN company_members m ON m.user_id = u.id AND m.company_id = c.id
ORDER BY a.created_at DESC LIMIT 5;

-- документы, ждущие проверки подписи
SELECT application_id, file_name, storage_key FROM documents WHERE signature_state = 'PENDING' AND signed_document_id IS NULL;

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
