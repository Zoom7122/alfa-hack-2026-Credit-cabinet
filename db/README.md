# db

Общая схема PostgreSQL для всех модулей кабинета. Отдельный Go-модуль: миграции Goose и команда `migrate`, которая их накатывает и завершается. Сервисы схему не трогают — они стартуют после `migrate`.

Начальная схема (`00001_init.sql`) перенесена из [docs/db-schema.dbml](../docs/db-schema.dbml) и [docs/db-schema.md](../docs/db-schema.md): 13 таблиц, перечисления — `text` + `CHECK` со значениями из `api/openapi.yaml`. `00002_raw_collections.sql` добавляет `raw_collections` — сырые ответы внешних систем, которые собирает data-collector. `00003_collection_tasks.sql` — очередь сбора data-collector: задачи `collection_tasks`, исходящие события `outbox` (transactional outbox → Kafka) и ссылка `raw_collections.task_id` на задачу. С этого момента источник правды — миграции; ERD и справочник обновляются вместе с ними.

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

## Правила миграций

- Применённую миграцию не редактируем — пишем новую.
- У каждой миграции есть `-- +goose Down`.
- Файлы встраиваются в бинарник (`migrations/embed.go`), отдельно их копировать не нужно.
