-- +goose Up

CREATE TABLE credit_engine_admin.model_drafts (
    draft_id uuid PRIMARY KEY,
    model_id text NOT NULL,
    borrower_type text NOT NULL CHECK (borrower_type IN ('SOLE_PROPRIETOR', 'LEGAL_ENTITY')),
    title text NOT NULL,
    current_revision bigint NOT NULL CHECK (current_revision > 0),
    created_by text NOT NULL CHECK (length(created_by) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

-- Canonical JSON is text so PostgreSQL JSONB normalization cannot change
-- exact numeric tokens or the definition SHA256. New parameters need no DDL.
CREATE TABLE credit_engine_admin.model_revisions (
    draft_id uuid NOT NULL REFERENCES credit_engine_admin.model_drafts(draft_id),
    revision bigint NOT NULL CHECK (revision > 0),
    definition_json text NOT NULL CHECK (jsonb_typeof(definition_json::jsonb) = 'object'),
    definition_sha256 text NOT NULL CHECK (definition_sha256 ~ '^[a-f0-9]{64}$'),
    author text NOT NULL CHECK (length(author) > 0),
    reason text NOT NULL,
    changes jsonb NOT NULL CHECK (jsonb_typeof(changes) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (draft_id, revision)
);

ALTER TABLE credit_engine_admin.model_drafts ADD CONSTRAINT model_drafts_current_revision_fk
    FOREIGN KEY (draft_id, current_revision)
    REFERENCES credit_engine_admin.model_revisions(draft_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE credit_engine_admin.model_versions (
    model_version text PRIMARY KEY CHECK (length(model_version) BETWEEN 1 AND 128),
    model_id text NOT NULL CHECK (length(model_id) > 0),
    borrower_type text NOT NULL CHECK (borrower_type IN ('SOLE_PROPRIETOR', 'LEGAL_ENTITY')),
    definition_json text NOT NULL CHECK (jsonb_typeof(definition_json::jsonb) = 'object'),
    definition_sha256 text NOT NULL CHECK (definition_sha256 ~ '^[a-f0-9]{64}$'),
    uses_facts boolean NOT NULL,
    facts_schema_version text NOT NULL,
    facts_schema_sha256 text NOT NULL,
    created_by text NOT NULL CHECK (length(created_by) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((uses_facts AND length(facts_schema_version) > 0 AND facts_schema_sha256 ~ '^[a-f0-9]{64}$')
        OR (NOT uses_facts AND facts_schema_sha256 = '')),
    CHECK ((definition_json::jsonb ->> 'version') IS NOT DISTINCT FROM model_version)
);

CREATE TABLE credit_engine_admin.prepared_releases (
    release_version text PRIMARY KEY CHECK (length(release_version) BETWEEN 1 AND 128),
    manifest_json text NOT NULL CHECK (jsonb_typeof(manifest_json::jsonb) = 'object'),
    manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^[a-f0-9]{64}$'),
    archive bytea NOT NULL CHECK (octet_length(archive) > 0),
    archive_sha256 text NOT NULL CHECK (archive_sha256 ~ '^[a-f0-9]{64}$'),
    changes_markdown text NOT NULL,
    base_release_version text,
    base_manifest_sha256 text,
    notes text NOT NULL,
    created_by text NOT NULL CHECK (length(created_by) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((base_release_version IS NULL AND base_manifest_sha256 IS NULL)
        OR (base_release_version IS NOT NULL AND length(base_release_version) > 0
            AND base_manifest_sha256 IS NOT NULL AND base_manifest_sha256 ~ '^[a-f0-9]{64}$')),
    CHECK ((manifest_json::jsonb ->> 'version') IS NOT DISTINCT FROM release_version)
);

CREATE TABLE credit_engine_admin.change_history (
    history_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entity_kind text NOT NULL CHECK (entity_kind IN ('DRAFT', 'MODEL', 'RELEASE')),
    entity_id text NOT NULL CHECK (length(entity_id) > 0),
    action text NOT NULL CHECK (length(action) > 0),
    revision bigint CHECK (revision > 0),
    author text NOT NULL CHECK (length(author) > 0),
    reason text NOT NULL,
    details jsonb NOT NULL CHECK (jsonb_typeof(details) = 'object'),
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_engine_admin.credit_events (
    event_id text PRIMARY KEY CHECK (length(event_id) > 0),
    application_id text NOT NULL CHECK (length(application_id) > 0),
    kind text NOT NULL CHECK (kind IN ('LOAN_ISSUED', 'BORROWER_REVIEW', 'BANK_REVIEW')),
    borrower_type text NOT NULL CHECK (borrower_type IN ('SOLE_PROPRIETOR', 'LEGAL_ENTITY')),
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    payload_json text NOT NULL CHECK (jsonb_typeof(payload_json::jsonb) = 'object'),
    payload_sha256 text NOT NULL CHECK (payload_sha256 ~ '^[a-f0-9]{64}$'),
    CHECK ((payload_json::jsonb ->> 'event_id') IS NOT DISTINCT FROM event_id),
    CHECK ((payload_json::jsonb ->> 'application_id') IS NOT DISTINCT FROM application_id),
    CHECK ((payload_json::jsonb ->> 'kind') IS NOT DISTINCT FROM kind),
    CHECK ((payload_json::jsonb -> 'borrower' ->> 'type') IS NOT DISTINCT FROM borrower_type)
);

CREATE TABLE credit_engine_admin.admin_sessions (
    token_sha256 text PRIMARY KEY CHECK (token_sha256 ~ '^[a-f0-9]{64}$'),
    username text NOT NULL CHECK (length(username) > 0),
    csrf_sha256 text NOT NULL CHECK (csrf_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX model_drafts_updated_idx ON credit_engine_admin.model_drafts(updated_at DESC, draft_id);
CREATE INDEX change_history_entity_idx ON credit_engine_admin.change_history(entity_kind, entity_id, history_id DESC);
CREATE INDEX credit_events_time_idx ON credit_engine_admin.credit_events(occurred_at DESC, event_id);
CREATE INDEX credit_events_kind_idx ON credit_engine_admin.credit_events(kind, occurred_at DESC, event_id);
CREATE INDEX credit_events_application_idx ON credit_engine_admin.credit_events(application_id, occurred_at DESC, event_id);
CREATE INDEX admin_sessions_expiry_idx ON credit_engine_admin.admin_sessions(expires_at);

-- +goose StatementBegin
CREATE FUNCTION credit_engine_admin.reject_immutable_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION USING ERRCODE = '55000', MESSAGE = 'immutable administrative record';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER immutable_revision BEFORE UPDATE OR DELETE ON credit_engine_admin.model_revisions
    FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_immutable_mutation();
CREATE TRIGGER immutable_model BEFORE UPDATE OR DELETE ON credit_engine_admin.model_versions
    FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_immutable_mutation();
CREATE TRIGGER immutable_release BEFORE UPDATE OR DELETE ON credit_engine_admin.prepared_releases
    FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_immutable_mutation();
CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON credit_engine_admin.change_history
    FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_immutable_mutation();
CREATE TRIGGER immutable_event BEFORE UPDATE OR DELETE ON credit_engine_admin.credit_events
    FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.reject_immutable_mutation();

-- +goose Down
ALTER TABLE credit_engine_admin.model_drafts DROP CONSTRAINT model_drafts_current_revision_fk;
DROP TABLE credit_engine_admin.admin_sessions;
DROP TABLE credit_engine_admin.credit_events;
DROP TABLE credit_engine_admin.change_history;
DROP TABLE credit_engine_admin.prepared_releases;
DROP TABLE credit_engine_admin.model_versions;
DROP TABLE credit_engine_admin.model_revisions;
DROP TABLE credit_engine_admin.model_drafts;
DROP FUNCTION credit_engine_admin.reject_immutable_mutation();
-- Keep this service's schema and version table for Goose's rollback record.
