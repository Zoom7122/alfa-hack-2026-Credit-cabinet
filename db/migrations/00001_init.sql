-- Начальная схема: 13 таблиц из docs/db-schema.dbml и docs/db-schema.md.
-- Перечисления — text + CHECK со значениями одноимённых схем api/openapi.yaml.

-- +goose Up

-- ------------------------------------------------------------ пользователи и компании

CREATE TABLE users (
    id         text PRIMARY KEY,                       -- usr_<ULID>
    full_name  text        NOT NULL,
    role       text        NOT NULL CHECK (role IN ('CLIENT', 'UNDERWRITER')),
    inn        text,                                   -- ИНН ФЛ, для проверки подписанта
    persona    text,                                   -- описание для /auth/test-users
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE companies (
    id              text PRIMARY KEY,                  -- apl_<ULID>; в API — Applicant
    inn             text        NOT NULL UNIQUE CHECK (inn ~ '^([0-9]{10}|[0-9]{12})$'),
    type            text        NOT NULL CHECK (type IN ('LEGAL_ENTITY', 'SOLE_PROPRIETOR')),
    name            text        NOT NULL,
    ogrn            text,
    director_inn    text,
    registry_status text CHECK (registry_status IN ('ACTIVE', 'LIQUIDATING', 'LIQUIDATED', 'REORGANIZING', 'BANKRUPTCY')),
    registry        jsonb,                             -- реквизиты из ЕГРЮЛ/ЕГРИП (CounterpartyPrefill)
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE company_members (
    user_id    text    NOT NULL REFERENCES users (id),
    company_id text    NOT NULL REFERENCES companies (id),
    role       text    NOT NULL CHECK (role IN ('DIRECTOR', 'OWNER', 'CFO', 'ACCOUNTANT', 'AUTHORIZED_EMPLOYEE')),
    can_sign   boolean NOT NULL DEFAULT false,
    PRIMARY KEY (user_id, company_id)
);

-- ------------------------------------------------------------ заявка

CREATE SEQUENCE application_number_seq;               -- для number вида A-2026-000123

CREATE TABLE applications (
    id           text PRIMARY KEY,                     -- app_<ULID>
    number       text        NOT NULL UNIQUE,
    company_id   text        NOT NULL REFERENCES companies (id),
    product_code text,                                 -- код из products.yaml
    created_by   text        NOT NULL REFERENCES users (id),
    status       text        NOT NULL DEFAULT 'DRAFT' CHECK (status IN (
                     'DRAFT', 'SUBMITTED', 'ENRICHING', 'VERIFYING', 'WAITING_FOR_INFO', 'SCORING', 'DECIDING',
                     'MANUAL_REVIEW', 'OFFERED', 'SIGNING', 'COMPLETED', 'REJECTED', 'DECLINED', 'CANCELLED', 'EXPIRED')),
    version      int         NOT NULL DEFAULT 1,       -- optimistic lock
    amount       bigint CHECK (amount > 0),
    term_months  int CHECK (term_months > 0),
    form         jsonb       NOT NULL DEFAULT '{}',    -- ApplicationInput
    assignee_id  text REFERENCES users (id),
    deadline_at  timestamptz,
    submitted_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX applications_company_created_idx ON applications (company_id, created_at DESC);
CREATE INDEX applications_status_submitted_idx ON applications (status, submitted_at);
CREATE INDEX applications_deadline_idx ON applications (deadline_at) WHERE deadline_at IS NOT NULL;

-- Журнал заявки. Только вставка.
CREATE TABLE application_events (
    id             text PRIMARY KEY,                   -- evt_<ULID>, монотонный
    application_id text        NOT NULL REFERENCES applications (id),
    type           text        NOT NULL,
    from_status    text,
    to_status      text,
    actor_id       text REFERENCES users (id),         -- NULL — система
    message        text,                               -- NULL — событие не для клиента
    payload        jsonb       NOT NULL DEFAULT '{}',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX application_events_application_idx ON application_events (application_id, id);

CREATE TABLE info_requests (
    id             text PRIMARY KEY,                   -- ir_<ULID>
    application_id text        NOT NULL REFERENCES applications (id),
    status         text        NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'ANSWERED', 'CANCELLED')),
    required       boolean     NOT NULL,
    question       jsonb       NOT NULL,
    answer         jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    answered_at    timestamptz
);

CREATE INDEX info_requests_application_idx ON info_requests (application_id);

-- ------------------------------------------------------------ обогащение и решение

-- Ответы внешних источников. Только вставка.
CREATE TABLE source_snapshots (
    id              text PRIMARY KEY,                  -- snap_<ULID>
    company_id      text        NOT NULL REFERENCES companies (id),
    source          text        NOT NULL CHECK (source IN (
                        'EGRUL', 'EGRIP', 'PB_NALOG', 'GIRBO', 'BKI', 'FSSP', 'FEDRESURS', 'KAD', 'RNP', 'EGRN', 'BANK_INTERNAL')),
    mode            text        NOT NULL CHECK (mode IN ('LIVE', 'MOCK')),
    data            jsonb       NOT NULL,
    raw_sha256      text        NOT NULL,
    raw_storage_key text        NOT NULL,
    fetched_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz                        -- NULL — между заявками не переиспользуется
);

CREATE INDEX source_snapshots_lookup_idx ON source_snapshots (company_id, source, fetched_at DESC);

CREATE TABLE source_runs (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id text     NOT NULL REFERENCES applications (id),
    source         text     NOT NULL CHECK (source IN (
                       'EGRUL', 'EGRIP', 'PB_NALOG', 'GIRBO', 'BKI', 'FSSP', 'FEDRESURS', 'KAD', 'RNP', 'EGRN', 'BANK_INTERNAL')),
    state          text     NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING', 'RUNNING', 'DONE', 'FAILED', 'SKIPPED')),
    mode           text CHECK (mode IN ('LIVE', 'MOCK', 'CACHE')),
    attempts       smallint NOT NULL DEFAULT 0,
    snapshot_id    text REFERENCES source_snapshots (id),
    error          text,
    finished_at    timestamptz,
    UNIQUE (application_id, source)
);

-- Строка на раунд рассмотрения.
CREATE TABLE decisions (
    id             text PRIMARY KEY,                   -- dcs_<ULID>
    application_id text        NOT NULL REFERENCES applications (id),
    grade          text CHECK (grade IN ('A', 'B', 'C', 'D', 'E')),
    score          numeric,
    max_amount     bigint,
    auto_outcome   text CHECK (auto_outcome IN ('APPROVED', 'APPROVED_WITH_CHANGES', 'REJECTED', 'MANUAL_REVIEW', 'NEED_INFO')),
    outcome        text CHECK (outcome IN ('APPROVED', 'APPROVED_WITH_CHANGES', 'REJECTED', 'MANUAL_REVIEW', 'NEED_INFO')),
    decided_by     text REFERENCES users (id),         -- NULL — автоматически
    details        jsonb       NOT NULL DEFAULT '{}',
    client_reasons jsonb,
    comment        text,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX decisions_application_idx ON decisions (application_id, created_at);

CREATE TABLE offers (
    id              text PRIMARY KEY,                  -- ofr_<ULID>
    application_id  text         NOT NULL REFERENCES applications (id),
    kind            text         NOT NULL CHECK (kind IN ('REQUESTED', 'ALTERNATIVE', 'RETENTION')),
    status          text         NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ACCEPTED', 'WITHDRAWN', 'EXPIRED')),
    amount          bigint       NOT NULL CHECK (amount > 0),
    term_months     int          NOT NULL CHECK (term_months > 0),
    rate            numeric(5,2) NOT NULL,
    monthly_payment bigint       NOT NULL,
    terms           jsonb        NOT NULL DEFAULT '{}',
    valid_until     timestamptz  NOT NULL
);

CREATE INDEX offers_application_idx ON offers (application_id);

-- ------------------------------------------------------------ документы и подписание

CREATE TABLE documents (
    id                 text PRIMARY KEY,               -- doc_<ULID>
    application_id     text        NOT NULL REFERENCES applications (id),
    type               text        NOT NULL CHECK (type IN (
                           'FINANCIAL_STATEMENTS', 'TAX_RETURN', 'BANK_STATEMENT', 'MANAGEMENT_REPORT', 'CHARTER',
                           'POWER_OF_ATTORNEY', 'DIRECTOR_PASSPORT', 'COLLATERAL_TITLE', 'EGRN_EXTRACT', 'LEASE_AGREEMENT',
                           'CONTRACT_WITH_CUSTOMER', 'LOAN_AGREEMENT_DRAFT', 'SIGNATURE', 'OTHER')),
    file_name          text        NOT NULL,
    mime_type          text        NOT NULL,
    size_bytes         bigint      NOT NULL CHECK (size_bytes >= 0),
    sha256             text        NOT NULL,
    storage_key        text        NOT NULL,
    uploaded_by        text REFERENCES users (id),     -- NULL — сгенерирован банком
    info_request_id    text REFERENCES info_requests (id),
    signed_document_id text REFERENCES documents (id), -- для открепленной .sig
    recognition        jsonb,
    signature_state    text        NOT NULL DEFAULT 'NOT_SIGNED' CHECK (signature_state IN ('NOT_SIGNED', 'PENDING', 'DONE', 'FAILED')),
    signature_verdict  text CHECK (signature_verdict IN ('VALID', 'VALID_WITH_WARNINGS', 'INVALID', 'INDETERMINATE')),
    uploaded_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz                     -- soft delete
);

CREATE INDEX documents_application_idx ON documents (application_id) WHERE deleted_at IS NULL;

-- Отчёты проверки ЭП. Только вставка.
CREATE TABLE signature_reports (
    id               text PRIMARY KEY,                 -- sigr_<ULID>
    document_id      text REFERENCES documents (id),   -- NULL — POST /signatures/verify
    verdict          text        NOT NULL CHECK (verdict IN ('VALID', 'VALID_WITH_WARNINGS', 'INVALID', 'INDETERMINATE')),
    policy           text        NOT NULL CHECK (policy IN ('GENERIC', 'POWER_OF_ATTORNEY', 'FINANCIAL_STATEMENTS', 'LOAN_CONTRACT')),
    policy_satisfied boolean     NOT NULL,
    trust_mode       text        NOT NULL CHECK (trust_mode IN ('MOCK', 'LIVE')),
    report           jsonb       NOT NULL,
    requested_by     text REFERENCES users (id),
    verified_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX signature_reports_document_idx ON signature_reports (document_id, verified_at DESC);

CREATE TABLE signings (
    id             text PRIMARY KEY,                   -- sgn_<ULID>
    application_id text        NOT NULL REFERENCES applications (id),
    offer_id       text        NOT NULL REFERENCES offers (id),
    signer_id      text        NOT NULL REFERENCES users (id),
    method         text CHECK (method IN ('SIMPLE_CODE', 'QUALIFIED_SIGNATURE')),
    status         text        NOT NULL DEFAULT 'AWAITING_SIGNER' CHECK (status IN ('AWAITING_SIGNER', 'SIGNED', 'FAILED', 'CANCELLED')),
    code           text,                               -- только mock
    attempts_left  smallint    NOT NULL,
    signed_at      timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Не больше одной активной сессии подписания на заявку.
CREATE UNIQUE INDEX signings_active_uidx ON signings (application_id) WHERE status = 'AWAITING_SIGNER';

-- +goose Down

DROP TABLE signings;
DROP TABLE signature_reports;
DROP TABLE documents;
DROP TABLE offers;
DROP TABLE decisions;
DROP TABLE source_runs;
DROP TABLE source_snapshots;
DROP TABLE info_requests;
DROP TABLE application_events;
DROP TABLE applications;
DROP SEQUENCE application_number_seq;
DROP TABLE company_members;
DROP TABLE companies;
DROP TABLE users;
