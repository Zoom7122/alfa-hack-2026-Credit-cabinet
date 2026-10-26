# alfa-hack-2026-Credit-cabinet

Умный кредитный кабинет предпринимателя (Альфа-Хак 2026).

## Запуск

```bash
make up      # создаёт .env из .env.example, собирает и поднимает всё
make help    # остальные команды
```

| Сервис | Адрес | Что это |
|---|---|---|
| postgres | `localhost:${POSTGRES_PORT}` (5432) | общая БД |
| migrate | — | накатывает миграции из [db/](db/) и завершается |
| data-collector | http://localhost:8081 | сбор данных для скоринга: регистрация заявителя `POST /api/v1/applicants`, анкета с документами `POST /api/v1/applications/{id}/questionnaire` ставит задачу на сбор (outbox → Kafka), состояние — `GET /api/v1/applications/{id}`, `GET /api/v1/collections/{id}`; файлы — в volume `documents`; [data-collector/](data-collector/), Swagger UI — http://localhost:8081/swagger/index.html |
| data-collector-publisher | — | переносит события о задачах и документах из таблицы `outbox` в Kafka |
| data-collector-worker | — | читает задачи из Kafka, опрашивает все моки и пишет сырые ответы в `raw_collections` |
| data-collector-signature-worker | — | читает события о документах из Kafka, проверяет электронные подписи (CMS/CAdES и подпись внутри PDF, профиль TEST — тестовые корни, не УКЭП) и пишет отчёты в `signature_reports`; [data-collector/README.md](data-collector/README.md#проверка-подписи-data-collector-signature-worker) |
| credit-engine-scoring | http://localhost:8082 | скоринг и подбор условий, `POST /api/v1/decisions` |
| credit-engine-bank | — | состояние банка; события передаёт в data-collector |
| credit-engine-admin-migrate | — | накатывает миграции схемы `credit_engine_admin` в общей БД и завершается |
| credit-engine-admin | http://localhost:8084 | конструктор скоринга: модели, правила, пробные расчёты и подготовка релизов; доступ с localhost |
| kafka | `localhost:${KAFKA_PORT}` (9094) | брокер сообщений (KRaft, один узел); внутри compose — `kafka:9092` |
| kafka-ui | http://localhost:8086 | топики, сообщения и лаг consumer group в браузере |
| mock-api | http://localhost:8090 | эмулятор внешних систем: АБС банка, ФНС, БКИ, Compliance, ФССП, КАД, Федресурс, МЧД, налоговая отчётность, залоги, ЕГРН, Госуслуги, мультибанкинг, маркетплейсы, ОФД, закупки, сигналы, ЦБ, hh.ru; [mock-api/](mock-api/) |
| frontend | http://localhost:3000 | кабинет клиента и рабочее место банка, [frontend/](frontend/); пока работает на моках |
| dozzle | http://localhost:8888 | логи всех контейнеров в браузере |

Порты и доступы к БД — в `.env`.

### Конструктор скоринга

`make up` поднимает конструктор вместе со стеком и сначала применяет его миграции. Для уже работающего стека достаточно:

```bash
docker compose up -d --build credit-engine-admin
```

Откройте http://localhost:8084. В `.env.example` настроен локальный вход **admin / credit-admin**. Если `.env` уже был создан, перенесите в него блок настроек конструктора из `.env.example`. Данные конструктора хранятся в схеме `credit_engine_admin` существующего PostgreSQL и сохраняются в volume `pgdata`.

Порт задаёт `ADMIN_PORT`; пустой `ADMIN_PUBLIC_ORIGIN` автоматически использует `http://localhost` с этим портом. Для другого адреса задайте точный `ADMIN_PUBLIC_ORIGIN`, для HTTPS — также `ADMIN_COOKIE_SECURE=true`. Собственный bcrypt-хеш можно получить утилитой `/admin-password` из образа credit-engine; [инструкция по настройке доступа](credit-engine/docs/33_container_delivery.md).

Конструктор получает метаданные работающего движка через `ADMIN_SCORING_URL`. Подготовленный в нём релиз модели переносится в исходники credit-engine с последующей сборкой образа — [порядок выпуска](credit-engine/docs/36_release_and_integration.md).

## Документация

- [docs/backend-test-data.md](docs/backend-test-data.md) — все тестовые входные данные backend: компании A–E, готовые JSON, согласия, токены, МЧД, залоги и файлы
- [api/openapi.yaml](api/openapi.yaml) — REST-контракт для фронтенда и back-office (OpenAPI 3.1)
- [docs/fsm.md](docs/fsm.md) — FSM заявки: диаграмма, таблица переходов, клиентские этапы
- [docs/db-schema.md](docs/db-schema.md) — схема БД: справочник всех таблиц и колонок; ERD — [docs/db-schema.dbml](docs/db-schema.dbml) для dbdiagram.io
- [db/](db/) — миграции схемы (Goose) и команда `migrate`; `docker compose up migrate`
- [docs/websocket.md](docs/websocket.md) — WebSocket-события
- [docs/e-signature.md](docs/e-signature.md) — проверка электронных подписей (КЭП, МЧД), политики, демо-сценарии; что реализовано в MVP и чем профиль TEST отличается от проверки УКЭП
- [docs/data-collection.md](docs/data-collection.md) — какие данные собираем, источники, скоринговая карта ЮЛ/ИП
- [docs/mock-api-guide.md](docs/mock-api-guide.md) — что можно получить с каждого мока, как и зачем это скорингу
- [docs/mock-apis.md](docs/mock-apis.md) — контракты моков внешних систем и тестовые сценарии A–E; вторая и третья очередь — [docs/mock-apis-wave-2-3.md](docs/mock-apis-wave-2-3.md)

Проверка контракта:

```bash
npx @redocly/cli@1.25.0 lint api/openapi.yaml
```

Финансовые документы (КУДиР ИП, PDF ГИРБО ООО): [реализация, контракт валидатора и проверки](docs/financial-documents.md).

## Отправка заявок в скоринг

Очередь, снимки, масштабирование, переход на постоянное банковское состояние и проверки описаны в [docs/scoring-dispatch.md](docs/scoring-dispatch.md).
