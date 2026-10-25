-- Асинхронный сбор данных по анкете (data-collector), transactional outbox:
--   POST /api/v1/questionnaires одной транзакцией пишет задачу в collection_tasks и событие в outbox;
--   outbox-publisher публикует событие в Kafka; worker забирает его, опрашивает все внешние системы (моки)
--   и пишет результат строкой raw_collections со ссылкой на задачу.
-- Статусы задачи: PENDING → RUNNING → COMPLETED, либо FAILED (кончились попытки или сбой сбора).

-- +goose Up

CREATE TABLE collection_tasks (
    id               text PRIMARY KEY,                     -- tsk_<ULID>
    questionnaire_id text        NOT NULL,                 -- qst_<ULID>; анкеты пока в памяти data-collector, поэтому без FK
    inn              text        NOT NULL CHECK (inn ~ '^([0-9]{10}|[0-9]{12})$'),
    status           text        NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'RUNNING', 'COMPLETED', 'FAILED')),
    request          jsonb       NOT NULL,                 -- входные данные сбора без токенов (формат raw_collections.request)
    secrets          jsonb,                                -- токены маркетплейсов и ОФД для worker; NULL после завершения
    skipped_sources  text[]      NOT NULL DEFAULT '{}',    -- системы без входных данных (согласия, токена, залога): не опрашиваются
    attempts         smallint    NOT NULL DEFAULT 0,       -- сколько раз worker брал задачу
    error            text,                                 -- почему FAILED
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,                          -- последний захват worker'ом
    finished_at      timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX collection_tasks_questionnaire_idx ON collection_tasks (questionnaire_id);
CREATE INDEX collection_tasks_status_idx ON collection_tasks (status, created_at);

-- Исходящие события. Пишутся в одной транзакции с данными, в Kafka их переносит outbox-publisher.
CREATE TABLE outbox (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic        text        NOT NULL,                     -- топик Kafka
    key          text        NOT NULL,                     -- ключ сообщения: id задачи
    payload      jsonb       NOT NULL,                     -- тело сообщения; токенов и входных данных в нём нет
    attempts     int         NOT NULL DEFAULT 0,           -- неудачные попытки публикации
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz                               -- NULL — ещё не опубликовано
);

CREATE INDEX outbox_unpublished_idx ON outbox (topic, id) WHERE published_at IS NULL;

-- Каждый сбор — результат задачи. Сборам, сделанным до очереди, заводим завершённые задачи.
ALTER TABLE raw_collections ADD COLUMN task_id text REFERENCES collection_tasks (id);

INSERT INTO collection_tasks (id, questionnaire_id, inn, status, request, skipped_sources, attempts,
                              created_at, started_at, finished_at, updated_at)
SELECT 'tsk_' || substr(rc.id, 5), rc.questionnaire_id, rc.inn, 'COMPLETED', rc.request,
       ARRAY(SELECT s ->> 'source' FROM jsonb_array_elements(rc.sources) AS s WHERE s ->> 'state' = 'SKIPPED'),
       1, rc.created_at, rc.started_at, rc.finished_at, rc.created_at
FROM raw_collections rc;

UPDATE raw_collections SET task_id = 'tsk_' || substr(id, 5);

ALTER TABLE raw_collections ALTER COLUMN task_id SET NOT NULL;

-- Один результат на задачу: повторно доставленное сообщение не запишет сбор дважды.
CREATE UNIQUE INDEX raw_collections_task_uidx ON raw_collections (task_id);

-- +goose Down

DROP INDEX raw_collections_task_uidx;
ALTER TABLE raw_collections DROP COLUMN task_id;
DROP TABLE outbox;
DROP TABLE collection_tasks;
