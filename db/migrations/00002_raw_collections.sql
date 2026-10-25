-- Сырые ответы всех внешних систем (моков) по одному сбору данных.
-- Строка — один сбор по анкете (data-collector, POST /api/v1/questionnaires).
-- Колонка — одна система: в ней ответы её эндпоинтов как есть, по ключам запросов.
-- NULL в колонке системы — данных нет (пропущена или не ответила); почему — в sources.

-- +goose Up

CREATE TABLE raw_collections (
    id               text PRIMARY KEY,                     -- col_<ULID>
    questionnaire_id text        NOT NULL,                 -- qst_<ULID>; анкеты пока в памяти data-collector, поэтому без FK
    inn              text        NOT NULL CHECK (inn ~ '^([0-9]{10}|[0-9]{12})$'),
    director_inn     text CHECK (director_inn ~ '^[0-9]{12}$'),
    request          jsonb       NOT NULL,                 -- входные данные сбора; токены не храним
    sources          jsonb       NOT NULL,                 -- по системе: state, reason, все запросы со статусами
    started_at       timestamptz NOT NULL,
    finished_at      timestamptz NOT NULL,

    core_banking     jsonb,  -- /core-banking/v1: client, turnover, transactions (страницы), loans, restrictions
    fns              jsonb,  -- /fns: egrul, pb_search, pb, bo_search, bo_bfo, account_blocks{БИК}, charter, rmsp, disqualified
    bki              jsonb,  -- /bki/v1: legal, person
    compliance       jsonb,  -- /compliance/v1: zsk, screening
    fssp             jsonb,  -- /fssp/v1: company, director
    kad              jsonb,  -- /kad/v1: cases (страницы)
    fedresurs        jsonb,  -- /fedresurs/v1: company_messages, director_bankruptcy
    m4d              jsonb,  -- /m4d/v1: search, poa
    tax_reports      jsonb,  -- /tax-reports/v1: report
    pledge_registry  jsonb,  -- /pledge-registry/v1: by_pledgor, by_vin{VIN}, by_serial{номер}
    egrn             jsonb,  -- /egrn/v1: {кадастровый номер: выписка}
    gosuslugi        jsonb,  -- /gosuslugi/v1: director
    open_banking     jsonb,  -- /open-banking/v1: {БИК: consent, accounts, balances{счёт}, transactions{счёт}}
    marketplaces     jsonb,  -- /marketplaces/v1: {wildberries|ozon: profile, sales, payouts}
    ofd              jsonb,  -- /ofd/v1: kkt, receipts
    zakupki          jsonb,  -- /zakupki/v1: contracts, rnp
    signals          jsonb,  -- /signals/v1: inspections, trademarks, news
    cbr              jsonb,  -- /cbr: key_rate — SOAP-ответ ЦБ строкой (XML)
    hh               jsonb,  -- /hh: employers, vacancies

    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX raw_collections_inn_idx ON raw_collections (inn, created_at DESC);
CREATE INDEX raw_collections_questionnaire_idx ON raw_collections (questionnaire_id);

-- +goose Down

DROP TABLE raw_collections;
