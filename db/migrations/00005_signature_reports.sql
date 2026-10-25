-- Отчёты worker'а проверки ЭП (data-collector-signature-worker) в signature_reports:
--   какой файл подписи проверялся и хеши проверенных байтов (версия документа), профиль проверки,
--   статус каждого этапа, итоговое решение по правилам банка и общая причина отказа.
-- Повторная доставка события о документах не создаёт второй отчёт: у автоматической проверки
-- есть ключ идемпотентности (профиль + документ и подпись с их sha256).
-- Старые колонки verdict, policy, policy_satisfied и trust_mode остаются: worker их тоже заполняет.

-- +goose Up

ALTER TABLE signature_reports
    ADD COLUMN signature_document_id text REFERENCES documents (id), -- файл открепленной подписи; NULL — подпись внутри документа
    ADD COLUMN document_sha256       text,                           -- sha256 проверенных байтов документа, посчитан до проверки
    ADD COLUMN signature_sha256      text,                           -- sha256 файла подписи
    ADD COLUMN profile               text CHECK (profile IN ('TEST')), -- профиль проверки; TEST — тестовые корни, не УКЭП
    ADD COLUMN integrity_status      text CHECK (integrity_status IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN format_status         text CHECK (format_status IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN certificate_status    text CHECK (certificate_status IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN identity_status       text CHECK (identity_status IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN authority_status      text CHECK (authority_status IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN decision              text CHECK (decision IN ('VERIFIED', 'INVALID', 'INDETERMINATE', 'NOT_REQUIRED')),
    ADD COLUMN failure_reason        text,                           -- почему не VERIFIED: причина первого непройденного этапа
    ADD COLUMN idempotency_key       text,                           -- NULL — проверка не из worker'а (ручная, POST /signatures/verify)
    -- отчёт с профилем — это отчёт worker'а: у него заполнены все этапы и хеш документа
    ADD CONSTRAINT signature_reports_stages_chk CHECK (profile IS NULL OR (
        document_sha256 IS NOT NULL AND integrity_status IS NOT NULL AND format_status IS NOT NULL AND
        certificate_status IS NOT NULL AND identity_status IS NOT NULL AND authority_status IS NOT NULL AND
        decision IS NOT NULL));

-- Один автоматический отчёт на версию документа и подписи (NULL ключи не конфликтуют).
CREATE UNIQUE INDEX signature_reports_idempotency_uidx ON signature_reports (idempotency_key);

-- +goose Down

DROP INDEX signature_reports_idempotency_uidx;
ALTER TABLE signature_reports
    DROP CONSTRAINT signature_reports_stages_chk,
    DROP COLUMN idempotency_key,
    DROP COLUMN failure_reason,
    DROP COLUMN decision,
    DROP COLUMN authority_status,
    DROP COLUMN identity_status,
    DROP COLUMN certificate_status,
    DROP COLUMN format_status,
    DROP COLUMN integrity_status,
    DROP COLUMN profile,
    DROP COLUMN signature_sha256,
    DROP COLUMN document_sha256,
    DROP COLUMN signature_document_id;
