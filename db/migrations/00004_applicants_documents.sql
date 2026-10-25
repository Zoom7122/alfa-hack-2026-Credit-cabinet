-- Заявитель, компания, заявка и документы в data-collector:
--   POST /api/v1/applicants пишет users, companies, company_members и черновик applications;
--   POST /api/v1/applications/{id}/questionnaire отправляет заявку, пишет documents (файлы — на диске)
--   и ставит задачу на сбор со ссылками на заявку и документы.

-- +goose Up

-- Роли представителя по пунктам формы: к прежним — сотрудник, отвечающий за финансирование,
-- и сотрудник, работающий с банковскими продуктами.
ALTER TABLE company_members DROP CONSTRAINT company_members_role_check;
ALTER TABLE company_members ADD CONSTRAINT company_members_role_check CHECK (role IN (
    'DIRECTOR', 'OWNER', 'CFO', 'ACCOUNTANT', 'FINANCE_EMPLOYEE', 'BANKING_EMPLOYEE', 'AUTHORIZED_EMPLOYEE'));

-- Клиент с тем же ИНН физлица — тот же пользователь: повторная регистрация его находит.
CREATE UNIQUE INDEX users_client_inn_uidx ON users (inn) WHERE role = 'CLIENT' AND inn IS NOT NULL;

-- Задача на сбор знает свою заявку и документы, пришедшие с анкетой. NULL — задачи до заявок.
ALTER TABLE collection_tasks
    ADD COLUMN application_id text REFERENCES applications (id),
    ADD COLUMN document_ids   text[] NOT NULL DEFAULT '{}';

CREATE INDEX collection_tasks_application_idx ON collection_tasks (application_id);

-- +goose Down

DROP INDEX collection_tasks_application_idx;
ALTER TABLE collection_tasks DROP COLUMN document_ids, DROP COLUMN application_id;
DROP INDEX users_client_inn_uidx;
ALTER TABLE company_members DROP CONSTRAINT company_members_role_check;
ALTER TABLE company_members ADD CONSTRAINT company_members_role_check CHECK (role IN (
    'DIRECTOR', 'OWNER', 'CFO', 'ACCOUNTANT', 'AUTHORIZED_EMPLOYEE'));
