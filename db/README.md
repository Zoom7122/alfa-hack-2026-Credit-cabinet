# db

Общая схема PostgreSQL для всех модулей кабинета. Отдельный Go-модуль: миграции Goose и команда `migrate`, которая их накатывает и завершается. Сервисы схему не трогают — они стартуют после `migrate`.

Начальная схема (`00001_init.sql`) перенесена из [docs/db-schema.dbml](../docs/db-schema.dbml) и [docs/db-schema.md](../docs/db-schema.md): 13 таблиц, перечисления — `text` + `CHECK` со значениями из `api/openapi.yaml`. `00002_raw_collections.sql` добавляет `raw_collections` — сырые ответы внешних систем, которые собирает data-collector. `00003_collection_tasks.sql` — очередь сбора data-collector: задачи `collection_tasks`, исходящие события `outbox` (transactional outbox → Kafka) и ссылка `raw_collections.task_id` на задачу. `00004_applicants_documents.sql` — регистрация заявителя и документы: новые роли `company_members` (`FINANCE_EMPLOYEE`, `BANKING_EMPLOYEE`), уникальный ИНН клиента в `users`, ссылки `collection_tasks.application_id` и `document_ids`. `00005_signature_reports.sql` — отчёты worker'а проверки подписи: в `signature_reports` файл подписи, sha256 документа и подписи, профиль (`TEST`), статус каждого этапа, итог, причина отказа и уникальный ключ идемпотентности. С этого момента источник правды — миграции; ERD и справочник обновляются вместе с ними.

## Запуск

Настройки подключения — в `.env` в корне репозитория (`.env` в git не попадает):

```bash
cp .env.example .env   # POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB, POSTGRES_PORT
```

Через docker-compose из корня репозитория:

```bash
docker compose up migrate
```

Локально (`Makefile` собирает `DATABASE_URL` из `.env`, хост `localhost`, порт `POSTGRES_PORT`):

```bash
make up        # накатить все миграции
make status    # какие применены
make down      # откатить последнюю
make new name=add_questionnaires   # новый файл миграции
```

Готовый `DATABASE_URL` в окружении имеет приоритет над `.env`. Параллельные запуски безопасны: `migrate` берёт advisory lock в Postgres.

## Как подключить сервис

В `docker-compose.yml`:

```yaml
depends_on:
  migrate:
    condition: service_completed_successfully
```

Код Jet каждый модуль генерирует себе сам из БД после `make up`, только по своим таблицам, — так модули не зависят от `db` на уровне Go.

Для финансовых документов: `cd ../data-collector && make generate-financial`
с `DATABASE_URL` мигрированной БД. Генератор и список таблиц — `cmd/generate-financial`;
результат — `internal/generated/financial`. Требуются миграции `00007_financial_documents.sql`
и `00008_financial_worker.sql`, следующие за основной цепочкой миграций кабинета.

## Правила миграций

- Применённую миграцию не редактируем — пишем новую.
- У каждой миграции есть `-- +goose Down`.
- Файлы встраиваются в бинарник (`migrations/embed.go`), отдельно их копировать не нужно.
