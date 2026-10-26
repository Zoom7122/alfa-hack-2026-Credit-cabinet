-- Демо-подписание договора кодом из интерфейса. Доказательство хранится отдельно
-- от отчётов криптографической проверки ЭП загруженных документов.
-- +goose Up
ALTER TABLE signings
    ADD COLUMN document_id text REFERENCES documents (id),
    ADD COLUMN document_sha256 text,
    ADD COLUMN code_expires_at timestamptz,
    ADD COLUMN disbursement jsonb;

-- +goose Down
ALTER TABLE signings
    DROP COLUMN disbursement,
    DROP COLUMN code_expires_at,
    DROP COLUMN document_sha256,
    DROP COLUMN document_id;
