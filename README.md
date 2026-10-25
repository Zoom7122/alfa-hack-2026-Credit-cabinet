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
| kafka | `localhost:${KAFKA_PORT}` (9094) | брокер сообщений (KRaft, один узел); внутри compose — `kafka:9092` |
| kafka-ui | http://localhost:8086 | топики, сообщения и лаг consumer group в браузере |
| mock-api | http://localhost:8090 | эмулятор внешних систем: АБС банка, ФНС, БКИ, Compliance, ФССП, КАД, Федресурс, МЧД, налоговая отчётность, залоги, ЕГРН, Госуслуги, мультибанкинг, маркетплейсы, ОФД, закупки, сигналы, ЦБ, hh.ru; [mock-api/](mock-api/) |
| dozzle | http://localhost:8888 | логи всех контейнеров в браузере |

Порты и доступы к БД — в `.env`.

## Документация

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
