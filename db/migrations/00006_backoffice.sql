-- Рабочее место андеррайтера (data-collector, /api/v1/backoffice): повтор решения с тем же
-- Idempotency-Key не применяется второй раз; у вопроса клиенту — срок ответа.

-- +goose Up

CREATE TABLE decision_requests (
    idempotency_key text PRIMARY KEY,
    application_id  text        NOT NULL REFERENCES applications (id),
    user_id         text        NOT NULL REFERENCES users (id),
    action          text        NOT NULL CHECK (action IN ('APPROVE', 'REJECT', 'REQUEST_INFO')),
    created_at      timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE info_requests ADD COLUMN due_at timestamptz;

CREATE INDEX applications_assignee_idx ON applications (assignee_id) WHERE assignee_id IS NOT NULL;

-- +goose Down

DROP INDEX applications_assignee_idx;
ALTER TABLE info_requests DROP COLUMN due_at;
DROP TABLE decision_requests;
