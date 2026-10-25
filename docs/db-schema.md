# Схема БД

PostgreSQL 16. Одна БД на API, воркеры pgqueue и WS-хаб, поэтому переход FSM, запись в журнал и постановка следующей таски выполняются в одной транзакции ([fsm.md](fsm.md)).

16 таблиц. ERD — [db-schema.dbml](db-schema.dbml): вставьте файл целиком на [dbdiagram.io](https://dbdiagram.io/d). Таблицы раскрашены по доменам. Диаграмма и справочник ниже содержат одни и те же колонки. Источник правды — миграции в [db/migrations](../db/migrations), оба файла обновляются вместе с ними.

Проверка синтаксиса диаграммы:

```bash
npx -p @dbml/cli@3.14.1 dbml2sql docs/db-schema.dbml --postgres > /dev/null
```

## Принципы

- **Колонка — только если по ней фильтруем, джойним или её охраняет FSM.** Всё, что пишется и читается целиком (анкета, расчёт скоринга, отчёт проверки ЭП), лежит в `jsonb`. Структура jsonb совпадает со схемами [openapi.yaml](../api/openapi.yaml), валидация и связи с таблицами — на Go-структурах.
- **История не перезаписывается.** `application_events`, `source_snapshots`, `raw_collections` и `signature_reports` пишутся только вставкой. Решения хранятся строкой на каждый раунд рассмотрения.
- PK — `text` вида `<prefix>_<ULID>` (`app_01JB8Z…`), как в API. Исключения — `source_runs` и `outbox`: наружу без id, PK `bigint identity`.
- Деньги — `bigint` в рублях, ставки — `numeric`, время — `timestamptz`.
- PG-enum не заводим: перечисления хранятся в `text` с `CHECK` на значения одноимённой схемы OpenAPI. Новое значение не требует `ALTER TYPE`.
- Имена в БД и API различаются в одном месте: таблица `companies` — это `Applicant` в API, `company_id` — это `applicant_id`.

## Справочник таблиц

Колонка «Null»: пусто — `NOT NULL`, ✔ — допускает `NULL`.

### users

Пользователи кабинета и back-office. Вход мок-логином, паролей не храним.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `usr_<ULID>` |
| `full_name` | text | | |
| `role` | text | | `CLIENT`, `UNDERWRITER` |
| `inn` | text | ✔ | ИНН ФЛ. Сверяется с сертификатом КЭП (`SIGNER_AUTHORITY`) |
| `persona` | text | ✔ | что демонстрирует пользователь (`/auth/test-users`) |
| `created_at` | timestamptz | | |

Индексы: уникальный `(inn) where role = 'CLIENT' and inn is not null` — клиент с тем же ИНН ФЛ один (регистрация в data-collector его находит).

### companies

Компания или ИП, одна строка на ИНН. Вторая заявка той же компании переиспользует строку и свежие снапшоты.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `apl_<ULID>` |
| `inn` | text | | UK. 10 цифр для ЮЛ, 12 для ИП |
| `type` | text | | `LEGAL_ENTITY`, `SOLE_PROPRIETOR` |
| `name` | text | | краткое наименование |
| `ogrn` | text | ✔ | ОГРН или ОГРНИП |
| `director_inn` | text | ✔ | ИНН действующего руководителя. По нему выбирается подписант договора и проверяется `SIGNER_AUTHORITY` |
| `registry_status` | text | ✔ | `ACTIVE`, `LIQUIDATING`, `LIQUIDATED`, `REORGANIZING`, `BANKRUPTCY` |
| `registry` | jsonb | ✔ | остальные реквизиты из последнего снапшота ЕГРЮЛ/ЕГРИП: полное наименование, КПП, адрес, ОКВЭД, дата регистрации, ФИО директора (поля `CounterpartyPrefill`) |
| `created_at` | timestamptz | | |

Полный профиль компании (`ApplicantFull` в досье back-office) не хранится: он собирается из свежих `source_snapshots` при чтении.

### company_members

Кто от имени какой компании действует (`Me.companies`).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `user_id` | text | | PK, FK → `users` |
| `company_id` | text | | PK, FK → `companies` |
| `role` | text | | `OWNER` (собственник), `DIRECTOR` (генеральный директор), `CFO` / `ACCOUNTANT` (финансовый директор или бухгалтер), `FINANCE_EMPLOYEE` (отвечает за финансирование), `BANKING_EMPLOYEE` (работает с банковскими продуктами), `AUTHORIZED_EMPLOYEE` (иной уполномоченный сотрудник) |
| `can_sign` | boolean | | `true` только у действующего директора ЮЛ или у самого ИП |

### applications

Заявка. Ввод клиента целиком лежит в `form`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `app_<ULID>` |
| `number` | text | | UK, `A-2026-000123` (sequence) |
| `company_id` | text | | FK → `companies` |
| `product_code` | text | ✔ | код продукта из `products.yaml` |
| `created_by` | text | | FK → `users` |
| `status` | text | | `ApplicationStatus`, default `DRAFT` |
| `version` | int | | default 1. Optimistic lock: `ETag`/`If-Match` и guard перехода FSM |
| `amount` | bigint | ✔ | запрошенная сумма. Дублирует `form.requested.amount` для очереди и фильтров |
| `term_months` | int | ✔ | то же для срока |
| `form` | jsonb | | default `'{}'`. `ApplicationInput`: цель, заявленные финансы, долги, залоги, поручители, согласия. До submit может быть неполной; обязательность полей проверяется по `required_fields` продукта |
| `assignee_id` | text | ✔ | FK → `users`. Андеррайтер в MANUAL_REVIEW |
| `deadline_at` | timestamptz | ✔ | TTL текущего ожидания (ответ на вопрос или оффер). По нему таймер переводит заявку в EXPIRED |
| `submitted_at` | timestamptz | ✔ | от него считается SLA ручного рассмотрения |
| `created_at` | timestamptz | | |
| `updated_at` | timestamptz | | |

Индексы: `(company_id, created_at desc)`; `(status, submitted_at)`; `(deadline_at) where deadline_at is not null`.

Если заявка создана копированием прошлой, id исходной (`Application.prefilled_from`) пишется в `payload` события создания.

### application_events

Журнал заявки: таймлайн для клиента, аудит для back-office и дозагрузка WS по `last_event_id` ([websocket.md](websocket.md)). Только вставка.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `evt_<ULID>`, монотонный |
| `application_id` | text | | FK → `applications` |
| `type` | text | | `TimelineEvent.type` (`STATUS_CHANGED`, `OFFERS_DECLINED`, `DISBURSED`, …) и внутренние (`ASSIGNED`, `SOURCE_REFRESHED`, …) |
| `from_status` | text | ✔ | |
| `to_status` | text | ✔ | |
| `actor_id` | text | ✔ | FK → `users`. `NULL` — система или таймер |
| `message` | text | ✔ | клиентский текст. `NULL` — событие не показывается клиенту |
| `payload` | jsonb | | default `'{}'`. Детали: событие FSM, версия заявки, причина отказа от оффера, параметры выдачи |
| `created_at` | timestamptz | | |

Индекс: `(application_id, id)`.

### info_requests

Нейтральный вопрос клиенту, источник расхождения в тексте не раскрывается.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `ir_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `status` | text | | `OPEN`, `ANSWERED`, `CANCELLED` |
| `required` | boolean | | блокирует ли переход `WAITING_FOR_INFO → ENRICHING` |
| `question` | jsonb | | поля `InfoRequest`: `title`, `description`, `target_field`, `input`, `documents`, плюс `finding_code` — какое расхождение породило вопрос |
| `answer` | jsonb | ✔ | `InfoRequestAnswer`: `value`, `comment`, `answered_by` |
| `created_at` | timestamptz | | |
| `answered_at` | timestamptz | ✔ | |

Кто создал вопрос — в `application_events`. Документы ответа — в `documents.info_request_id`.

### source_snapshots

Ответ внешнего источника по компании. Только вставка: это и кэш по ИНН (вторая заявка берёт свежий снапшот без похода наружу), и доказательная база.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `snap_<ULID>` |
| `company_id` | text | | FK → `companies` |
| `source` | text | | `SourceCode` |
| `mode` | text | | `LIVE`, `MOCK` |
| `data` | jsonb | | нормализованный ответ ([data-collection.md](data-collection.md), §3) |
| `raw_sha256` | text | | хэш сырого ответа |
| `raw_storage_key` | text | | где лежит сырой ответ |
| `fetched_at` | timestamptz | | |
| `expires_at` | timestamptz | ✔ | `fetched_at` + TTL источника. `NULL` — между заявками не переиспользуется (`BKI`) |

Индекс: `(company_id, source, fetched_at desc)`.

### source_runs

Состояние источника в рамках заявки (`SourceResult`): одна строка на пару (заявка, источник). Повторное обогащение сбрасывает состояние строки, но снапшоты остаются, а каждый запуск пишется в журнал.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `application_id` | text | | FK → `applications`; UK `(application_id, source)` |
| `source` | text | | `SourceCode` |
| `state` | text | | `PENDING`, `RUNNING`, `DONE`, `FAILED`, `SKIPPED` |
| `mode` | text | ✔ | `LIVE`, `MOCK`, `CACHE` |
| `attempts` | smallint | | default 0 |
| `snapshot_id` | text | ✔ | FK → `source_snapshots`, какой снапшот использован |
| `error` | text | ✔ | ошибка или причина пропуска («нет залога недвижимости») |
| `finished_at` | timestamptz | ✔ | |

Критичность источника (`CRITICAL`/`NORMAL`) берётся из конфига, а не из БД.

### collection_tasks

Задача на сбор данных data-collector: строка на анкету, принятую `POST /api/v1/applications/{id}/questionnaire`. Создаётся одной транзакцией с отправкой заявки и событием в `outbox`. Выполняет её worker по событию из Kafka: `PENDING` → `RUNNING` → `COMPLETED` (заявка — `ENRICHING` → `VERIFYING`), результат — строка `raw_collections`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `tsk_<ULID>` |
| `application_id` | text | ✔ | FK → `applications`. `NULL` — задачи, созданные до заявок |
| `questionnaire_id` | text | | `qst_<ULID>`; анкета лежит в `applications.form` |
| `inn` | text | | ИНН компании или ИП, `CHECK` 10 или 12 цифр |
| `document_ids` | text[] | | документы (`documents.id`), пришедшие с анкетой |
| `status` | text | | `PENDING`, `RUNNING`, `COMPLETED`, `FAILED` |
| `request` | jsonb | | входные данные сбора без токенов, как `raw_collections.request` |
| `secrets` | jsonb | ✔ | токены маркетплейсов и ОФД для worker'а. `NULL` — токенов нет или задача завершена |
| `skipped_sources` | text[] | | системы, для которых в анкете нет данных (согласия, токена, залога, банков): не опрашиваются. Заполняется при создании |
| `attempts` | smallint | | сколько раз worker брал задачу; кончились попытки — `FAILED` |
| `error` | text | ✔ | почему `FAILED` |
| `created_at` | timestamptz | | |
| `started_at` | timestamptz | ✔ | последний захват worker'ом |
| `finished_at` | timestamptz | ✔ | |
| `updated_at` | timestamptz | | |

Индексы: `(questionnaire_id)`, `(status, created_at)`, `(application_id)`.

### outbox

Исходящие события для Kafka (transactional outbox). Пишутся в одной транзакции с данными, в брокер их переносит outbox-publisher data-collector. Строки блокируются `FOR UPDATE SKIP LOCKED`, поэтому несколько publisher'ов не публикуют одно событие дважды. Доставка at-least-once.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity: порядок публикации |
| `topic` | text | | топик Kafka |
| `key` | text | | ключ сообщения: id задачи |
| `payload` | jsonb | | тело сообщения: `event`, `task_id`, `questionnaire_id`, `inn`, `created_at`. Входных данных и токенов нет |
| `attempts` | int | | неудачные попытки публикации |
| `last_error` | text | ✔ | почему не удалась последняя попытка |
| `created_at` | timestamptz | | |
| `published_at` | timestamptz | ✔ | `NULL` — ещё не опубликовано |

Индексы: `(topic, id) WHERE published_at IS NULL` — очередь неопубликованного.

### raw_collections

Сырые ответы всех внешних систем (моков) по одному сбору данных: строка на выполненную задачу `collection_tasks`, её пишет worker data-collector. Только вставка. Колонка на систему, в ней — ответы её эндпоинтов как есть, по ключам запросов (ключи — в [data-collector/README.md](../data-collector/README.md)). `NULL` в колонке системы — данных нет: система пропущена или не ответила, причина — в `sources`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `col_<ULID>` |
| `task_id` | text | | FK → `collection_tasks`, UK: один результат на задачу |
| `questionnaire_id` | text | | `qst_<ULID>`. Без FK: анкеты пока хранятся в памяти data-collector |
| `inn` | text | | ИНН компании или ИП, `CHECK` 10 или 12 цифр |
| `director_inn` | text | ✔ | ИНН руководителя, `CHECK` 12 цифр |
| `request` | jsonb | | входные данные сбора: руководитель, согласия, банки, залог. Токены не хранятся |
| `sources` | jsonb | | по системе: `state` (`DONE`, `PARTIAL`, `FAILED`, `SKIPPED`), `reason`, все HTTP-запросы со статусом и временем |
| `started_at`, `finished_at` | timestamptz | | |
| `core_banking` | jsonb | ✔ | АБС банка |
| `fns` | jsonb | ✔ | ЕГРЮЛ, «Прозрачный бизнес», ГИР БО, блокировки, устав, реестр МСП, дисквалификация |
| `bki` | jsonb | ✔ | отчёты БКИ по компании и руководителю |
| `compliance` | jsonb | ✔ | ЗСК и санкционные списки |
| `fssp` | jsonb | ✔ | исполнительные производства |
| `kad` | jsonb | ✔ | арбитражные дела |
| `fedresurs` | jsonb | ✔ | Федресурс |
| `m4d` | jsonb | ✔ | МЧД |
| `tax_reports` | jsonb | ✔ | налоговая отчётность |
| `pledge_registry` | jsonb | ✔ | реестр залогов |
| `egrn` | jsonb | ✔ | выписки ЕГРН |
| `gosuslugi` | jsonb | ✔ | «Цифровой профиль» руководителя |
| `open_banking` | jsonb | ✔ | счета в других банках |
| `marketplaces` | jsonb | ✔ | Wildberries, Ozon |
| `ofd` | jsonb | ✔ | онлайн-кассы |
| `zakupki` | jsonb | ✔ | госконтракты и РНП |
| `signals` | jsonb | ✔ | проверки, товарные знаки, новости |
| `cbr` | jsonb | ✔ | ключевая ставка: SOAP-ответ ЦБ строкой |
| `hh` | jsonb | ✔ | hh.ru |
| `created_at` | timestamptz | | |

Индексы: `(inn, created_at desc)`, `(questionnaire_id)`, уникальный `(task_id)`.

Это временная форма хранения: сбор пишет всё одной строкой, а нормализованные `source_snapshots` и `source_runs` по заявке появятся вместе с FSM. jsonb хранит содержимое ответов без изменений, но не исходный текст: порядок ключей и пробелы нормализуются.

### decisions

Одна строка на раунд рассмотрения. VERIFYING создаёт строку и пишет расхождения, SCORING — балл, DECIDING — итог. После WAITING_FOR_INFO начинается новый раунд и создаётся новая строка. Андеррайтер в MANUAL_REVIEW обновляет `outcome` и `decided_by` текущей строки, а итог движка остаётся в `auto_outcome`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `dcs_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `grade` | text | ✔ | `A` … `E` |
| `score` | numeric | ✔ | итоговый балл |
| `max_amount` | bigint | ✔ | максимальная сумма по расчёту |
| `auto_outcome` | text | ✔ | итог движка: `APPROVED`, `APPROVED_WITH_CHANGES`, `REJECTED`, `MANUAL_REVIEW`, `NEED_INFO` |
| `outcome` | text | ✔ | итог раунда, те же значения. Для клиента схлопывается в `DecisionOutcome` |
| `decided_by` | text | ✔ | FK → `users`. `NULL` — решение принято автоматически |
| `details` | jsonb | | `findings[]` (`Finding`), `score` (`ScoreResult`: модель, факторы, стоп-факторы, расчёт лимита, экспертные поправки), `engine` (`EngineDecision`: сработавшие правила, причины ручного рассмотрения) |
| `client_reasons` | jsonb | ✔ | то, что видит клиент (`ClientDecision`): причины, рекомендации, `reapply_after` |
| `comment` | text | ✔ | внутренний комментарий андеррайтера |
| `created_at` | timestamptz | | |

### offers

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `ofr_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `kind` | text | | `REQUESTED`, `ALTERNATIVE`, `RETENTION` |
| `status` | text | | `ACTIVE`, `ACCEPTED`, `WITHDRAWN`, `EXPIRED` |
| `amount` | bigint | | |
| `term_months` | int | | |
| `rate` | numeric(5,2) | | % годовых |
| `monthly_payment` | bigint | | |
| `terms` | jsonb | | остальные поля `Offer`: ПСК, полная сумма выплат, требуемый залог и поручительство, условия, отличие от запрошенного, `recommended` |
| `valid_until` | timestamptz | | |

Продукт оффера — продукт заявки. Принятие и отказ (с причиной) пишутся в `application_events`. Retention-оффер уже был, если у заявки есть оффер с `kind = 'RETENTION'`.

### documents

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `doc_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `type` | text | | `DocumentType`. Договор и график платежей — тоже документы (`LOAN_AGREEMENT_DRAFT`, генерирует банк) |
| `file_name` | text | | |
| `mime_type` | text | | |
| `size_bytes` | bigint | | |
| `sha256` | text | | хэш содержимого. Для договора сравнивается с хэшем в подписи (`LOAN_CONTRACT`) |
| `storage_key` | text | | путь в файловом хранилище |
| `uploaded_by` | text | ✔ | FK → `users`. `NULL` — документ сгенерирован банком |
| `info_request_id` | text | ✔ | FK → `info_requests` |
| `signed_document_id` | text | ✔ | FK → `documents`. Для открепленной подписи `.sig` — какой документ подписан |
| `recognition` | jsonb | ✔ | `Document.recognition`: состояние, распознанный тип, извлечённые поля, уверенность |
| `signature_state` | text | | `NOT_SIGNED`, `PENDING`, `DONE`, `FAILED` — состояние проверки ЭП (`Document.signatures.state`). Worker подписи ставит `DONE` (или `FAILED`, если файла нет) документу и файлу его подписи вместе с отчётом |
| `signature_verdict` | text | ✔ | вердикт последнего отчёта ЭП, для списков. Остальная сводка (`policy`, `policy_satisfied`, число подписей) берётся из последнего `signature_reports` |
| `uploaded_at` | timestamptz | | |
| `deleted_at` | timestamptz | ✔ | soft delete |

### signature_reports

Отчёты проверки электронной подписи. Только вставка: каждая проверка, включая повторную из back-office и `POST /signatures/verify` без заявки, — отдельная строка для аудита. Порядок проверки, политики и состав отчёта — в [e-signature.md](e-signature.md).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `sigr_<ULID>` |
| `document_id` | text | ✔ | FK → `documents`. `NULL` — проверка файла без заявки |
| `verdict` | text | | `VALID`, `VALID_WITH_WARNINGS`, `INVALID`, `INDETERMINATE` |
| `policy` | text | | `GENERIC`, `POWER_OF_ATTORNEY`, `FINANCIAL_STATEMENTS`, `LOAN_CONTRACT` |
| `policy_satisfied` | boolean | | |
| `trust_mode` | text | | `MOCK`, `LIVE` |
| `report` | jsonb | | `SignatureReport` целиком: каждая подпись, сертификат, отзыв, полномочия и МЧД, результаты всех 14 проверок. У worker'а подписи — ещё этапы с их проверками, профиль и его ограничения |
| `requested_by` | text | ✔ | FK → `users` |
| `verified_at` | timestamptz | | |
| `signature_document_id` | text | ✔ | FK → `documents`: файл открепленной подписи. `NULL` — подпись внутри документа |
| `document_sha256` | text | ✔ | sha256 проверенных байтов документа, посчитан до проверки, — версия документа |
| `signature_sha256` | text | ✔ | sha256 файла подписи |
| `profile` | text | ✔ | профиль проверки: `TEST` (тестовые корни и моки реестров, не УКЭП). `NULL` — отчёт не от worker'а подписи |
| `integrity_status`, `format_status`, `certificate_status`, `identity_status`, `authority_status` | text | ✔ | статус этапа: `VERIFIED`, `INVALID`, `INDETERMINATE`, `NOT_REQUIRED` — целостность, формат и охват, сертификат, личность подписанта, полномочия |
| `decision` | text | ✔ | итог по правилам банка, те же значения |
| `failure_reason` | text | ✔ | почему не `VERIFIED`: причина первого непройденного этапа |
| `idempotency_key` | text | ✔ | UK. Один автоматический отчёт на версию документа и подписи; `NULL` — ручная проверка |

Индекс: `(document_id, verified_at desc)`. У отчёта с `profile` заполнены `document_sha256`, все статусы этапов и `decision` (CHECK).

### signings

Подписание договора (`SigningSession`). После `sign.failed` заявка возвращается в OFFERED, и при повторном принятии создаётся новая строка.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `sgn_<ULID>` |
| `application_id` | text | | FK → `applications`; partial UK `(application_id) where status = 'AWAITING_SIGNER'` |
| `offer_id` | text | | FK → `offers` |
| `signer_id` | text | | FK → `users`, директор компании |
| `method` | text | ✔ | `SIMPLE_CODE`, `QUALIFIED_SIGNATURE`. Выбирается на `signing/start` |
| `status` | text | | `AWAITING_SIGNER`, `SIGNED`, `FAILED`, `CANCELLED` |
| `code` | text | ✔ | код ПЭП, только мок-режим (показывается в UI) |
| `attempts_left` | smallint | | |
| `signed_at` | timestamptz | ✔ | |
| `created_at` | timestamptz | | |

Для КЭП отчёт проверки — `signature_reports` документа-договора. Выдача кредита имитируется: переход в COMPLETED и событие `DISBURSED` с суммой и маской счёта в `payload`.

## Вне схемы

- **Каталог продуктов** — `products.yaml` (как `scorecard-sme-v1.yaml`): статичный, в рантайме не меняется, отдаётся через `GET /products`.
- **pgqueue.** Таблицы очереди создаёт и обслуживает библиотека.
- **Файлы.** Содержимое документов и сырые ответы источников лежат в файловом хранилище, в БД только `storage_key` и `sha256`.
- **Сессии.** Токен мок-логина — подписанный JWT, таблицы сессий нет.
- **Тестовый УЦ и реестр МЧД** (mock-режим) — фикстуры контейнера `sigverify`.
