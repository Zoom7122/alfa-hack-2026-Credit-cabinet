# Схема БД

PostgreSQL 16. Одна БД на API, воркеры pgqueue и WS-хаб, поэтому переход FSM, запись в журнал и постановка следующей таски выполняются в одной транзакции ([fsm.md](fsm.md)).

Как читать документ:

1. **ERD** — [db-schema.dbml](db-schema.dbml) для dbdiagram.io. Широкие группы колонок там свёрнуты в одну строку типа `group`: например, `"fin_* (7)" group` означает 7 колонок с префиксом `fin_`.
2. **Справочник таблиц** ниже — все колонки без сокращений. Если диаграмма и справочник расходятся, прав справочник. Когда появятся миграции, источником правды станут они, а оба файла обновляются вместе с ними.

Соглашения:

- PK бизнес-сущностей — `text` вида `<prefix>_<ULID>` (`app_01JB8Z…`), как в API. У дочерних строк, которые наружу без id не отдаются, PK — `bigint generated always as identity`.
- Деньги хранятся как `bigint` в рублях (в API тоже целые рубли), ставки — `numeric`.
- Время хранится в `timestamptz`, у каждой таблицы есть `created_at` (default `now()`), у изменяемых ещё `updated_at`.
- Значения enum-типов совпадают с одноимёнными схемами в [openapi.yaml](../api/openapi.yaml). Список — в [разделе Enum-типы](#enum-типы).
- `jsonb` используем только для данных переменной формы: нормализованные ответы источников, распознанные поля, отчёты движка. Всё, по чему фильтруем или что валидируем, лежит в обычных колонках.

## ERD

Диаграмма лежит в [db-schema.dbml](db-schema.dbml). Чтобы посмотреть, вставьте файл целиком на [dbdiagram.io](https://dbdiagram.io/d). Таблицы раскрашены по доменам (`TableGroup`): пользователи и каталог, заявка, обогащение и решение, документы и подписание. Свёрнутые колонки раскрываются в подсказке при наведении (`note`).

Проверка синтаксиса:

```bash
npx -p @dbml/cli@3.14.1 dbml2sql docs/db-schema.dbml --postgres > /dev/null
```

SQL, который выдаёт `dbml2sql`, не годится как миграция: в нём остаются свёрнутые колонки типа `group`.

## Справочник таблиц

Колонка «Null»: пусто — `NOT NULL`, ✔ — допускает `NULL`.

### users

Пользователи кабинета и back-office. Вход мок-логином (`POST /auth/mock-login`), паролей не храним.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `usr_<ULID>` |
| `full_name` | text | | ФИО |
| `roles` | user_role[] | | `CLIENT`, `UNDERWRITER` |
| `inn_individual` | text | ✔ | ИНН ФЛ. Сверяется с OID 1.2.643.3.131.1.1 сертификата (`SIGNER_AUTHORITY`) |
| `snils` | text | ✔ | СНИЛС, для той же сверки. Наружу отдаётся только маской |
| `persona` | text | ✔ | Что демонстрирует пользователь (для `/auth/test-users`) |
| `is_test` | boolean | | default `true`. Показывать ли в `/auth/test-users` |
| `created_at` | timestamptz | | |

### applicants

Компания или ИП. Одна строка на ИНН: вторая заявка той же компании переиспользует строку и свежие снапшоты.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `apl_<ULID>` |
| `inn` | text | | UK. 10 цифр для ЮЛ, 12 для ИП |
| `applicant_type` | applicant_type | | `LEGAL_ENTITY` / `SOLE_PROPRIETOR` |
| `ogrn` | text | ✔ | ОГРН или ОГРНИП |
| `kpp` | text | ✔ | только ЮЛ |
| `full_name` | text | | |
| `short_name` | text | ✔ | |
| `registration_date` | date | ✔ | |
| `legal_address_full` | text | ✔ | |
| `region_code` | text | ✔ | `'77'` |
| `postal_code` | text | ✔ | |
| `okved_main_code` | text | ✔ | `'47.11'` |
| `okved_main_title` | text | ✔ | |
| `director_name` | text | ✔ | действующий руководитель по ЕГРЮЛ |
| `director_inn` | text | ✔ | ИНН ФЛ руководителя, для `SIGNER_AUTHORITY` и подбора `required_signer` |
| `registry_status` | registry_status | ✔ | `ACTIVE`, `LIQUIDATING`, … |
| `profile` | jsonb | ✔ | нормализованный профиль `ApplicantFull` (registry, tax, risk_flags, financial_statements, credit_history, enforcement, litigation, bank_relationship, collateral_checks), собранный из свежих снапшотов. Это кэш для досье: заново собирается на VERIFYING |
| `profile_built_at` | timestamptz | ✔ | |
| `created_at` | timestamptz | | |
| `updated_at` | timestamptz | | |

Реквизиты (`ogrn` … `registry_status`) денормализованы из последнего снапшота `EGRUL`/`EGRIP` и обновляются при каждом новом снапшоте.

### user_companies

Кто от имени какой компании действует (`Me.companies`, `CompanyAccess`).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `user_id` | text | | PK, FK → `users` |
| `applicant_id` | text | | PK, FK → `applicants` |
| `role` | representative_role | | `DIRECTOR`, `OWNER`, `CFO`, `ACCOUNTANT`, `AUTHORIZED_EMPLOYEE` |
| `can_sign` | boolean | | `true` только у действующего директора ЮЛ или у самого ИП |
| `poa_id` | text | ✔ | FK → `powers_of_attorney`. Основание полномочий, если роль ≠ `DIRECTOR` |
| `created_at` | timestamptz | | |

### products

Каталог кредитных продуктов. Заполняется сидами.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `code` | text | | PK, `WORKING_CAPITAL` |
| `name` | text | | |
| `description` | text | ✔ | |
| `applicant_types` | applicant_type[] | | кому доступен |
| `purposes` | purpose_code[] | | допустимые цели |
| `amount_min` | bigint | | ₽ |
| `amount_max` | bigint | | ₽, он же `PRODUCT_LIMIT` в `max_amount` |
| `term_min_months` | int | | |
| `term_max_months` | int | | |
| `rate_annual_from` | numeric(5,2) | | % годовых |
| `revenue_k` | numeric(4,2) | | `k_product` в формуле `revenue_cap` ([data-collection.md](data-collection.md)) |
| `collateral_required_above` | bigint | ✔ | с какой суммы нужен залог; `NULL` — не нужен |
| `collateral_accepted_types` | collateral_type[] | | default `'{}'` |
| `guarantee_required` | boolean | | |
| `required_fields` | text[] | | dot-path полей `ApplicationInput`, обязательных для submit |
| `required_documents` | jsonb | | `[{type, condition}]` |
| `is_active` | boolean | | default `true` |
| `created_at` | timestamptz | | |
| `updated_at` | timestamptz | | |

### applications

Заявка. Поля ввода клиента лежат плоско: до submit все они nullable, обязательность определяет `products.required_fields`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `app_<ULID>` |
| `number` | text | | UK, `A-2026-000123` (sequence `application_number_seq`) |
| `version` | int | | default 1. Optimistic lock: `ETag` / `If-Match` и guard перехода FSM |
| `status` | application_status | | default `DRAFT` |
| `applicant_id` | text | | FK → `applicants` |
| `product_code` | text | ✔ | FK → `products` |
| `created_by` | text | | FK → `users` |
| `prefilled_from` | text | ✔ | FK → `applications`. Из какой заявки подставлены данные |
| `requested_amount` | bigint | ✔ | ₽ |
| `requested_term_months` | int | ✔ | |
| `purpose_code` | purpose_code | ✔ | |
| `purpose_description` | text | ✔ | до 2000 символов |
| `fin_annual_revenue` | bigint | ✔ | заявленная выручка за последний полный год |
| `fin_annual_revenue_year` | smallint | ✔ | |
| `fin_net_profit` | bigint | ✔ | может быть < 0 |
| `fin_avg_monthly_turnover` | bigint | ✔ | средние поступления в месяц за 6 мес. |
| `fin_avg_monthly_expenses` | bigint | ✔ | |
| `fin_employees_count` | int | ✔ | |
| `fin_main_bank` | text | ✔ | |
| `has_no_debts` | boolean | ✔ | явное «долгов нет». `NULL` — не заполнено |
| `consent_personal_data` | boolean | | default `false`. 152-ФЗ |
| `consent_credit_bureau` | boolean | | default `false`. 218-ФЗ, без него submit заблокирован |
| `consents_accepted_at` | timestamptz | ✔ | |
| `submitted_at` | timestamptz | ✔ | |
| `deadline_at` | timestamptz | ✔ | TTL текущего ожидания (WAITING_FOR_INFO или оффер). Таймер-воркер переводит заявку в EXPIRED |
| `assignee_id` | text | ✔ | FK → `users`. Андеррайтер в MANUAL_REVIEW |
| `sla_due_at` | timestamptz | ✔ | SLA ручного рассмотрения |
| `created_at` | timestamptz | | |
| `updated_at` | timestamptz | | |

Индексы: `(applicant_id, created_at desc)` — список заявок компании; `(status, submitted_at)` where `status = 'MANUAL_REVIEW'` — очередь back-office; `(deadline_at)` where `deadline_at is not null` — таймеры.

### application_debts

`ApplicationInput.debts[]`. При `PATCH` массив заменяется целиком (delete + insert).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `application_id` | text | | FK → `applications`, on delete cascade |
| `position` | smallint | | порядок в массиве |
| `kind` | debt_kind | | `LOAN`, `CREDIT_LINE`, `OVERDRAFT`, `LEASING`, `GUARANTEE`, `OTHER` |
| `lender` | text | | |
| `original_amount` | bigint | ✔ | |
| `outstanding` | bigint | | остаток долга |
| `monthly_payment` | bigint | | |
| `maturity_date` | date | ✔ | |
| `has_overdue` | boolean | ✔ | |

### application_collateral

`ApplicationInput.collateral[]`, заменяется целиком.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `application_id` | text | | FK → `applications`, on delete cascade |
| `position` | smallint | | |
| `type` | collateral_type | | |
| `description` | text | ✔ | |
| `estimated_value` | bigint | | ₽ |
| `owner` | collateral_owner | ✔ | `APPLICANT`, `FOUNDER`, `THIRD_PARTY` |
| `cadastral_number` | text | ✔ | для `REAL_ESTATE`, проверяется по ЕГРН |
| `vin` | text | ✔ | для `VEHICLE` |
| `year` | smallint | ✔ | |

### application_guarantors

`ApplicationInput.guarantors[]`, заменяется целиком.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `application_id` | text | | FK → `applications`, on delete cascade |
| `position` | smallint | | |
| `type` | guarantor_type | | `INDIVIDUAL`, `LEGAL_ENTITY` |
| `inn` | text | | |
| `name` | text | | |
| `relation` | guarantor_relation | ✔ | `FOUNDER`, `DIRECTOR`, `SPOUSE_OF_OWNER`, `AFFILIATED_COMPANY`, `OTHER` |
| `birth_date` | date | ✔ | только `INDIVIDUAL` |
| `monthly_income` | bigint | ✔ | только `INDIVIDUAL` |

### application_events

Журнал заявки: таймлайн для клиента, аудит для back-office и источник дозагрузки WS по `last_event_id` ([websocket.md](websocket.md)). Только вставка.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `evt_<ULID>`, монотонный |
| `application_id` | text | | FK → `applications` |
| `type` | text | | `TimelineEvent.type` (`STATUS_CHANGED`, `INFO_REQUESTED`, …) и внутренние типы (`SOURCE_REFRESHED`, `ASSIGNED`, `DOCUMENT_RECOGNIZED`, …) |
| `fsm_event` | text | ✔ | событие FSM из таблицы переходов: `submit`, `verify.passed`, `uw.approve` |
| `from_status` | application_status | ✔ | |
| `to_status` | application_status | ✔ | |
| `version` | int | | `applications.version` после события |
| `actor_kind` | actor_kind | | `CLIENT`, `SYSTEM`, `EMPLOYEE` |
| `actor_id` | text | ✔ | FK → `users` |
| `message` | text | ✔ | клиентский текст без внутренних деталей |
| `client_visible` | boolean | | попадает ли в `GET /timeline` и клиентский WS |
| `payload` | jsonb | | default `'{}'`. Внутренние детали: id оффера, источник, причина |
| `created_at` | timestamptz | | |

Индекс: `(application_id, id)`.

### info_requests

Нейтральный вопрос клиенту. Источник расхождения в тексте не раскрывается.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `ir_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `state` | info_request_state | | `OPEN`, `ANSWERED`, `CANCELLED` |
| `required` | boolean | | блокирует ли переход `WAITING_FOR_INFO → ENRICHING` |
| `title` | text | | |
| `description` | text | ✔ | |
| `target_field` | text | ✔ | dot-path в заявке, который обновится ответом |
| `input_type` | info_input_type | | `NONE`, `TEXT`, `NUMBER`, `MONEY`, `DATE`, `BOOLEAN`, `CHOICE` |
| `input_options` | jsonb | ✔ | `[{value, label}]` для `CHOICE` |
| `input_min` | numeric | ✔ | |
| `input_max` | numeric | ✔ | |
| `docs_required` | boolean | | default `false` |
| `docs_accepted_types` | document_type[] | | default `'{}'` |
| `docs_hint` | text | ✔ | |
| `origin` | text | | `SYSTEM` (verify / decide) или `EMPLOYEE` |
| `created_by` | text | ✔ | FK → `users`, для `EMPLOYEE` |
| `finding_id` | text | ✔ | FK → `findings`, какое расхождение породило вопрос |
| `answer_value` | jsonb | ✔ | string / number / boolean |
| `answer_comment` | text | ✔ | |
| `answered_by` | text | ✔ | FK → `users` |
| `answered_at` | timestamptz | ✔ | |
| `created_at` | timestamptz | | |
| `due_at` | timestamptz | ✔ | |

Документы ответа лежат в `documents.info_request_id`.

### source_snapshots

Ответ внешнего источника по ИНН. Это одновременно кэш (вторая заявка берёт снапшот без похода наружу) и доказательная база.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `snap_<ULID>` |
| `applicant_id` | text | | FK → `applicants` |
| `source_code` | source_code | | |
| `mode` | text | | `LIVE` / `MOCK` |
| `source_url` | text | ✔ | |
| `raw_sha256` | text | | хэш сырого ответа |
| `raw_storage_key` | text | | где лежит сырой ответ (файловое хранилище) |
| `normalized` | jsonb | | нормализованный ответ (поля — [data-collection.md](data-collection.md), §3) |
| `fetched_at` | timestamptz | | |
| `expires_at` | timestamptz | ✔ | `fetched_at` + TTL источника. `NULL` — не переиспользуется между заявками (`BKI`) |

Индекс: `(applicant_id, source_code, fetched_at desc)`. Поле `SourceSnapshot.used_in_applications` в API вычисляется через `source_runs.snapshot_id`.

### source_runs

Состояние источника в рамках заявки (`SourceResult`). Одна строка на пару (заявка, источник). Повторное обогащение после WAITING_FOR_INFO и `POST .../refresh` сбрасывают строку, а не создают новую. История остаётся в `application_events`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `application_id` | text | | FK → `applications`; UK `(application_id, source_code)` |
| `source_code` | source_code | | |
| `criticality` | text | | `CRITICAL` / `NORMAL` на момент запуска |
| `state` | source_state | | `PENDING`, `RUNNING`, `DONE`, `FAILED`, `SKIPPED` |
| `mode` | text | ✔ | `LIVE`, `MOCK`, `CACHE` |
| `attempts` | smallint | | default 0 |
| `snapshot_id` | text | ✔ | FK → `source_snapshots` |
| `error` | text | ✔ | |
| `skip_reason` | text | ✔ | «Нет залога недвижимости» |
| `started_at` | timestamptz | ✔ | |
| `finished_at` | timestamptz | ✔ | |
| `updated_at` | timestamptz | | |

### findings

Расхождения «заявлено ↔ найдено» и результаты проверок на VERIFYING.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `fnd_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `code` | text | | `REVENUE_MISMATCH`, `UNDECLARED_DEBT`, `NO_POA`, … |
| `severity` | finding_severity | | `INFO`, `WARN`, `STOP` |
| `field` | text | ✔ | dot-path заявки |
| `declared_value` | jsonb | ✔ | |
| `found_value` | jsonb | ✔ | |
| `source_code` | source_code | ✔ | |
| `material` | boolean | | влияет ли на решение или сумму |
| `message` | text | | внутренний текст для back-office |
| `resolved_at` | timestamptz | ✔ | закрыто ответом клиента или подписанным документом |
| `created_at` | timestamptz | | |

### score_results

Результат скоринга. Хранится история: после WAITING_FOR_INFO и экспертных поправок появляется новая строка.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `scr_<ULID>` |
| `application_id` | text | | FK → `applications`; partial UK `(application_id) where is_current` |
| `is_current` | boolean | | |
| `model_version` | text | | `scorecard-sme-v1` |
| `engine` | score_engine | | `SCORECARD`, `ML`, `HYBRID` |
| `total` | numeric | | |
| `grade` | score_grade | | `A` … `E` |
| `pd` | numeric(6,5) | ✔ | вероятность дефолта, если подключена ML-модель |
| `stop_factors` | text[] | | default `'{}'` |
| `max_amount` | bigint | ✔ | |
| `max_amount_limited_by` | text | ✔ | `PRODUCT_LIMIT`, `REVENUE`, `DEBT_SERVICE`, `COLLATERAL` |
| `max_amount_calc` | jsonb | ✔ | промежуточные значения формулы (`free_cf`, `dscr_cap`, …) |
| `computed_at` | timestamptz | | |

### score_factors

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `score_result_id` | text | | FK → `score_results`, on delete cascade; UK `(score_result_id, code)` |
| `code` | text | | `COMPANY_AGE` |
| `title` | text | | |
| `value` | jsonb | ✔ | значение признака |
| `bucket` | text | ✔ | `'3–5 лет'` |
| `points` | numeric | | |
| `source` | text | | `DECLARED`, `<SourceCode>`, `DERIVED`, `EXPERT` |
| `degraded` | boolean | | default `false`. Источник недоступен, взято значение по умолчанию |
| `comment` | text | ✔ | для `EXPERT` |
| `added_by` | text | ✔ | FK → `users`, для `EXPERT` |

Экспертные поправки андеррайтера (`BoDecisionInput.expert_adjustments`) хранятся как факторы с `source = 'EXPERT'` в новом `score_results`.

### decisions

Решения движка и андеррайтера. Клиенту показывается последнее решение с непустым `client_outcome` (`ClientDecision`).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `dcs_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `kind` | decision_kind | | `ENGINE` / `UNDERWRITER` |
| `outcome` | engine_outcome | | `APPROVED`, `APPROVED_WITH_CHANGES`, `REJECTED`, `MANUAL_REVIEW`, `NEED_INFO` |
| `score_result_id` | text | ✔ | FK → `score_results` |
| `rules_fired` | jsonb | | default `'[]'`, `[{code, effect, detail}]` |
| `manual_review_reasons` | text[] | | default `'{}'` |
| `client_outcome` | decision_outcome | ✔ | `APPROVED`, `APPROVED_WITH_CHANGES`, `REJECTED`. `NULL` для промежуточных (`MANUAL_REVIEW`, `NEED_INFO`) |
| `client_reasons` | jsonb | | default `'[]'`, `[{code, title, description}]` |
| `recommendations` | text[] | | default `'{}'` |
| `reapply_after` | date | ✔ | |
| `decided_by` | text | ✔ | FK → `users`, для `UNDERWRITER` |
| `internal_comment` | text | ✔ | до 4000 символов |
| `created_at` | timestamptz | | это `decided_at` |

### offers

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `ofr_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `decision_id` | text | | FK → `decisions` |
| `kind` | offer_kind | | `REQUESTED`, `ALTERNATIVE`, `RETENTION` |
| `state` | offer_state | | `ACTIVE`, `ACCEPTED`, `WITHDRAWN`, `EXPIRED` |
| `recommended` | boolean | | default `false` |
| `product_code` | text | | FK → `products` |
| `amount` | bigint | | |
| `term_months` | int | | |
| `rate_annual` | numeric(5,2) | | |
| `monthly_payment` | bigint | | |
| `total_payment` | bigint | ✔ | |
| `full_cost_rate` | numeric(6,3) | ✔ | ПСК |
| `collateral_required` | collateral_type[] | | default `'{}'` |
| `guarantee_required` | boolean | | default `false` |
| `conditions` | text[] | | default `'{}'` |
| `difference_from_requested` | text | ✔ | |
| `valid_until` | timestamptz | | |
| `accepted_at` | timestamptz | ✔ | |
| `accepted_by` | text | ✔ | FK → `users` |
| `declined_at` | timestamptz | ✔ | `offers/decline` проставляет всем `ACTIVE` офферам и переводит их в `WITHDRAWN` |
| `decline_reason` | decline_reason | ✔ | |
| `decline_comment` | text | ✔ | |
| `created_at` | timestamptz | | |

Retention-оффер уже был, если у заявки есть строка с `kind = 'RETENTION'`.

### documents

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `doc_<ULID>` |
| `application_id` | text | | FK → `applications` |
| `type` | document_type | | |
| `file_name` | text | | |
| `mime_type` | text | | |
| `size_bytes` | bigint | | |
| `sha256` | text | | хэш содержимого. Для договора сравнивается с хэшем в подписи (`LOAN_CONTRACT`) |
| `storage_key` | text | | путь в файловом хранилище |
| `origin` | document_origin | | `CLIENT_UPLOAD`, `GENERATED` |
| `uploaded_by` | text | ✔ | FK → `users`. `NULL` для `GENERATED` |
| `info_request_id` | text | ✔ | FK → `info_requests` |
| `signed_document_id` | text | ✔ | FK → `documents`. Для `SIGNATURE` (открепленная `.sig`) указывает, какой документ подписан |
| `signature_state` | text | | `NOT_SIGNED`, `PENDING`, `DONE`, `FAILED` |
| `latest_signature_report_id` | text | ✔ | FK → `signature_reports`, источник сводки `Document.signatures` |
| `recognition_state` | text | | `NOT_APPLICABLE`, `PENDING`, `PROCESSING`, `DONE`, `FAILED` |
| `recognition_detected_type` | document_type | ✔ | |
| `recognition_extracted` | jsonb | ✔ | `{"revenue_2025": 48200000, …}` |
| `recognition_confidence` | numeric(4,3) | ✔ | 0…1 |
| `recognized_at` | timestamptz | ✔ | |
| `uploaded_at` | timestamptz | | |
| `deleted_at` | timestamptz | ✔ | soft delete (`DELETE …/documents/{id}`) |

### signature_reports

Отчёт проверки ЭП (`SignatureReport`). Для `POST /signatures/verify` без заявки `document_id = NULL`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `sigr_<ULID>` |
| `document_id` | text | ✔ | FK → `documents` |
| `file_name` | text | | |
| `document_sha256` | text | | |
| `policy` | signature_policy | | |
| `verdict` | signature_verdict | | вердикт документа: худший из обязательных подписей |
| `policy_satisfied` | boolean | | |
| `policy_required_signers` | jsonb | | `[{role, satisfied, signature_index}]` |
| `policy_message` | text | ✔ | |
| `trust_mode` | text | | `MOCK` / `LIVE` |
| `requested_by` | text | ✔ | FK → `users` |
| `verified_at` | timestamptz | | |

### signatures

Одна подпись внутри документа (`SignatureInfo`).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | bigint | | PK, identity |
| `report_id` | text | | FK → `signature_reports`, on delete cascade; UK `(report_id, idx)` |
| `idx` | smallint | | порядковый номер подписи в документе |
| `format` | text | | `CADES_BES`, `CADES_T`, `CADES_X_LONG_TYPE_1`, `PADES`, `XADES`, `XMLDSIG`, `UNKNOWN` |
| `container` | text | ✔ | `DETACHED`, `ATTACHED`, `PDF_EMBEDDED`, `XML_ENVELOPED`, `XML_DETACHED` |
| `kind` | text | | `QUALIFIED`, `UNQUALIFIED`, `UNKNOWN` |
| `verdict` | signature_verdict | | |
| `signing_time` | timestamptz | ✔ | |
| `signing_time_source` | text | | `TIMESTAMP`, `SIGNED_ATTRIBUTE`, `NONE` |
| `tsa_name` | text | ✔ | |
| `signer_kind` | text | | `LEGAL_ENTITY_HEAD`, `LEGAL_ENTITY_EMPLOYEE`, `SOLE_PROPRIETOR`, `INDIVIDUAL` |
| `signer_full_name` | text | | |
| `signer_position` | text | ✔ | |
| `signer_inn_individual` | text | ✔ | OID 1.2.643.3.131.1.1 |
| `signer_snils_masked` | text | ✔ | `***-***-123 45` |
| `signer_org_name` | text | ✔ | |
| `signer_org_inn` | text | ✔ | OID 1.2.643.100.4 |
| `signer_org_ogrn` | text | ✔ | OID 1.2.643.100.1 / .5 |
| `cert_serial_number` | text | | |
| `cert_thumbprint` | text | | SHA-1, hex |
| `cert_valid_from` | timestamptz | | |
| `cert_valid_to` | timestamptz | | |
| `cert_issuer_name` | text | | |
| `cert_issuer_inn` | text | ✔ | |
| `cert_issuer_ogrn` | text | ✔ | |
| `cert_issuer_accredited` | boolean | ✔ | аккредитован Минцифры на момент подписи |
| `cert_subject_sign_tool` | text | ✔ | OID 1.2.643.100.111 |
| `cert_issuer_sign_tool` | text | ✔ | OID 1.2.643.100.112 |
| `cert_policies` | text[] | | default `'{}'` |
| `cert_key_algorithm` | text | | `GOST_2012_256`, `GOST_2012_512`, `RSA`, `ECDSA`, `OTHER` |
| `revocation_status` | text | | `GOOD`, `REVOKED`, `UNKNOWN` |
| `revocation_method` | text | | `OCSP`, `CRL`, `EMBEDDED`, `NONE` |
| `revoked_at` | timestamptz | ✔ | |
| `revocation_checked_at` | timestamptz | ✔ | |
| `authority_basis` | text | | `REGISTRY_HEAD`, `SOLE_PROPRIETOR`, `POWER_OF_ATTORNEY`, `NONE` |
| `authority_matches_applicant` | boolean | ✔ | `NULL` — проверка без заявки |
| `poa_id` | text | ✔ | FK → `powers_of_attorney` |

### signature_checks

Чек-лист проверки одной подписи ([e-signature.md](e-signature.md)).

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `signature_id` | bigint | | PK, FK → `signatures`, on delete cascade |
| `code` | signature_check_code | | PK |
| `status` | text | | `PASSED`, `FAILED`, `WARNING`, `SKIPPED` |
| `message` | text | ✔ | |
| `details` | jsonb | ✔ | |

### powers_of_attorney

Машиночитаемые доверенности (МЧД). В режиме `mock` эта таблица вместе с сидами работает как реестр ФНС: `registry_status` берётся отсюда.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `poa_<ULID>` |
| `number` | text | | UK, единый регистрационный номер (GUID) |
| `format_version` | text | ✔ | `'5.02'` |
| `principal_name` | text | | доверитель |
| `principal_inn` | text | | |
| `principal_ogrn` | text | ✔ | |
| `representative_full_name` | text | | |
| `representative_inn_individual` | text | ✔ | |
| `powers` | text[] | | default `'{}'` |
| `valid_from` | date | | |
| `valid_to` | date | | |
| `registry_status` | text | | `VALID`, `REVOKED`, `EXPIRED`, `NOT_FOUND`, `UNCHECKED` |
| `principal_signature_verdict` | signature_verdict | ✔ | |
| `document_id` | text | ✔ | FK → `documents`, если МЧД загружена файлом |
| `checked_at` | timestamptz | ✔ | |
| `created_at` | timestamptz | | |

### signing_sessions

Сессия подписания договора (`SigningSession`). На заявку может приходиться несколько сессий (после `sign.failed` → OFFERED → снова accept), активная — не больше одной.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `sgn_<ULID>` |
| `application_id` | text | | FK → `applications`; partial UK `(application_id) where state = 'AWAITING_SIGNER'` |
| `offer_id` | text | | FK → `offers` |
| `state` | signing_state | | `AWAITING_SIGNER`, `SIGNED`, `FAILED`, `CANCELLED` |
| `method` | signing_method | ✔ | выбирается на `signing/start` |
| `required_signer_id` | text | | FK → `users`, директор компании |
| `contract_document_id` | text | | FK → `documents`, сгенерированный договор |
| `schedule_document_id` | text | ✔ | FK → `documents`, график платежей |
| `demo_code` | text | ✔ | только мок-режим: код показывается в UI |
| `attempts_left` | smallint | | |
| `signature_report_id` | text | ✔ | FK → `signature_reports`, для `QUALIFIED_SIGNATURE` |
| `failed_reason` | text | ✔ | |
| `signed_by` | text | ✔ | FK → `users` |
| `signed_at` | timestamptz | ✔ | |
| `created_at` | timestamptz | | |
| `updated_at` | timestamptz | | |

### disbursements

Имитация выдачи кредита, создаётся в той же транзакции, что и `SIGNING → COMPLETED`.

| Колонка | Тип | Null | Описание |
|---|---|---|---|
| `id` | text | | PK, `dsb_<ULID>` |
| `application_id` | text | | FK → `applications`, UK |
| `signing_session_id` | text | | FK → `signing_sessions`, UK |
| `amount` | bigint | | |
| `account_masked` | text | | `40702•••••••••••1234` |
| `executed_at` | timestamptz | | |

## Enum-типы

Значения PG-enum совпадают со схемами в `openapi.yaml`:

| PG-тип | Схема OpenAPI |
|---|---|
| `user_role` | `UserRole` |
| `representative_role` | `RepresentativeRole` |
| `applicant_type` | `ApplicantType` |
| `registry_status` | `CounterpartyPrefill.registry_status` |
| `purpose_code` | `PurposeCode` |
| `collateral_type` | `CollateralType` |
| `collateral_owner` | `CollateralItem.owner` |
| `debt_kind` | `ExistingDebt.kind` |
| `guarantor_type`, `guarantor_relation` | `Guarantor.type`, `Guarantor.relation` |
| `application_status` | `ApplicationStatus` |
| `actor_kind` | `TimelineEvent.actor_kind` |
| `info_request_state` | `InfoRequestState` |
| `info_input_type` | `InfoRequest.input.type` |
| `document_type` | `DocumentType` |
| `document_origin` | `Document.origin` |
| `source_code` | `SourceCode` |
| `source_state` | `SourceResult.state` |
| `finding_severity` | `Finding.severity` |
| `score_engine`, `score_grade` | `ScoreResult.engine`, `ScoreResult.grade` |
| `decision_kind` | `ENGINE`, `UNDERWRITER` (только в БД) |
| `engine_outcome` | `EngineDecision.outcome` |
| `decision_outcome` | `DecisionOutcome` |
| `offer_kind`, `offer_state` | `Offer.kind`, `Offer.state` |
| `decline_reason` | `DeclineReason` |
| `signing_state`, `signing_method` | `SigningSession.state`, `SigningMethod` |
| `signature_policy`, `signature_verdict`, `signature_check_code` | `SignaturePolicy`, `SignatureVerdict`, `SignatureCheckCode` |

Редкие и часто меняющиеся перечисления (форматы подписи, `signature_state`, `recognition_state`, `trust_mode`, …) хранятся как `text` с `CHECK`, чтобы новое значение не требовало `ALTER TYPE`.

## Вне схемы

- **pgqueue.** Таблицы очереди (`enrich.fanout`, `enrich.source`, `verify`, `score`, `decide`, `signature.verify`, таймеры TTL) создаёт и обслуживает библиотека, на ERD их нет.
- **Файлы.** Содержимое документов и сырые ответы источников лежат в файловом хранилище, в БД только `storage_key` и `sha256`.
- **Сессии.** Токен мок-логина — подписанный JWT, таблицы сессий нет.
- **Тестовый УЦ.** Ключи, сертификаты и CRL тестового УЦ — файлы контейнера `sigverify`, не БД.
