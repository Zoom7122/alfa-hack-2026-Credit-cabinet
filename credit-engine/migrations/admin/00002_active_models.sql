-- +goose Up
CREATE TABLE credit_engine_admin.active_models (
    borrower_type text PRIMARY KEY CHECK (borrower_type IN ('SOLE_PROPRIETOR', 'LEGAL_ENTITY')),
    model_version text NOT NULL REFERENCES credit_engine_admin.model_versions(model_version),
    revision bigint NOT NULL CHECK (revision > 0),
    changed_by text NOT NULL,
    reason text NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE credit_engine_admin.active_models;
