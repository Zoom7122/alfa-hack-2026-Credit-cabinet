-- Durable dispatch, scoring replay and bank effects. No HTTP call holds a row lock.
-- +goose Up
CREATE TABLE scoring_jobs (
 id text PRIMARY KEY,
 application_id text NOT NULL REFERENCES applications(id),
 round integer NOT NULL CHECK(round > 0),
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','RUNNING','RETRY_WAIT','COMPLETED','FAILED')),
 snapshot jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 owner_token text,
 response jsonb,
 last_error text,
 created_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 UNIQUE(application_id, round)
);
CREATE UNIQUE INDEX scoring_jobs_active_idx ON scoring_jobs(application_id)
 WHERE status IN ('PENDING','RUNNING','RETRY_WAIT');
CREATE INDEX scoring_jobs_due_idx ON scoring_jobs(next_attempt_at, created_at)
 WHERE status IN ('PENDING','RUNNING','RETRY_WAIT');
ALTER TABLE decisions ADD COLUMN scoring_job_id text UNIQUE REFERENCES scoring_jobs(id);
CREATE TABLE credit_event_inbox (
 event_id text PRIMARY KEY,
 application_id text NOT NULL REFERENCES applications(id),
 received_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO credit_event_inbox(event_id,application_id)
 SELECT DISTINCT ON (payload->>'event_id') payload->>'event_id', application_id
 FROM application_events WHERE type LIKE 'credit_engine.%' AND payload->>'event_id' IS NOT NULL
 ORDER BY payload->>'event_id',created_at ON CONFLICT DO NOTHING;

CREATE SCHEMA credit_engine_runtime;
CREATE TABLE credit_engine_runtime.scoring_runs (
 idempotency_key text PRIMARY KEY,
 request_hash text NOT NULL,
 prepared jsonb,
 response jsonb,
 owner_token text,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_engine_runtime.bank_state (
 id text PRIMARY KEY,
 policy_id text NOT NULL,
 state jsonb NOT NULL
);
CREATE TABLE credit_engine_runtime.bank_operations (
 operation_id text PRIMARY KEY,
 application_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('CHECK','MANUAL')),
 request_hash text NOT NULL,
 request jsonb NOT NULL,
 response jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE credit_engine_runtime.bank_loans (
 application_id text PRIMARY KEY,
 operation_id text NOT NULL UNIQUE REFERENCES credit_engine_runtime.bank_operations(operation_id),
 loan jsonb NOT NULL
);
CREATE TABLE credit_engine_runtime.bank_outbox (
 event_id text PRIMARY KEY,
 operation_id text NOT NULL REFERENCES credit_engine_runtime.bank_operations(operation_id),
 payload jsonb NOT NULL,
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 owner_token text,
 delivered_at timestamptz
);
CREATE INDEX bank_outbox_due_idx ON credit_engine_runtime.bank_outbox(next_attempt_at)
 WHERE delivered_at IS NULL;

-- +goose Down
DROP SCHEMA credit_engine_runtime CASCADE;
DROP TABLE credit_event_inbox;
ALTER TABLE decisions DROP COLUMN scoring_job_id;
DROP TABLE scoring_jobs;
