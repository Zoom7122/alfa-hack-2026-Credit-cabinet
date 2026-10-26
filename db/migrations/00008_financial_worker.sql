-- +goose Up
ALTER TABLE financial_document_jobs DROP CONSTRAINT financial_document_jobs_status_check;
UPDATE financial_document_jobs SET status='COMPLETED' WHERE status='DONE';
ALTER TABLE financial_document_jobs ADD CONSTRAINT financial_document_jobs_status_check
 CHECK(status IN ('PENDING','RUNNING','COMPLETED','FAILED'));
ALTER TABLE financial_document_jobs
 ADD COLUMN attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN error text,
 ADD COLUMN started_at timestamptz,
 ADD COLUMN finished_at timestamptz,
 ADD COLUMN lease_until timestamptz;

-- +goose Down
ALTER TABLE financial_document_jobs DROP CONSTRAINT financial_document_jobs_status_check;
UPDATE financial_document_jobs SET status=CASE WHEN status='COMPLETED' THEN 'DONE' ELSE 'PENDING' END;
ALTER TABLE financial_document_jobs ADD CONSTRAINT financial_document_jobs_status_check CHECK(status IN ('PENDING','DONE'));
ALTER TABLE financial_document_jobs DROP COLUMN attempts, DROP COLUMN error,
 DROP COLUMN started_at, DROP COLUMN finished_at, DROP COLUMN lease_until;
