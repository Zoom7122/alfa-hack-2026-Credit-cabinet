-- Financial processing uses existing documents, signature_reports and outbox.
-- +goose Up
ALTER TABLE documents ADD COLUMN financial_generation integer NOT NULL DEFAULT 0;
ALTER TABLE signature_reports DROP CONSTRAINT signature_reports_policy_check;
ALTER TABLE signature_reports ADD CONSTRAINT signature_reports_policy_check
 CHECK (policy IN ('GENERIC','POWER_OF_ATTORNEY','FINANCIAL_STATEMENTS','LOAN_CONTRACT','KUDIR_IP','GIRBO_FNS'));

CREATE TABLE financial_document_jobs (
 id text PRIMARY KEY,
 document_id text NOT NULL REFERENCES documents(id),
 generation integer NOT NULL,
 applicant jsonb NOT NULL,
 signature_ids jsonb NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','DONE')),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(document_id,generation)
);
CREATE TABLE financial_document_results (
 id text PRIMARY KEY,
 job_id text NOT NULL UNIQUE REFERENCES financial_document_jobs(id),
 document_id text NOT NULL REFERENCES documents(id),
 generation integer NOT NULL,
 signature_report_id text NOT NULL REFERENCES signature_reports(id),
 original_sha256 text NOT NULL,
 content_storage_key text,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(document_id,generation)
);
CREATE INDEX financial_results_document_idx ON financial_document_results(document_id,generation DESC);

-- +goose Down
DROP TABLE financial_document_results;
DROP TABLE financial_document_jobs;
ALTER TABLE documents DROP COLUMN financial_generation;
-- Keep the extended policy constraint: historical signature reports remain valid audit records.
