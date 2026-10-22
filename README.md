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
| data-collector | http://localhost:8081 | сбор данных для скоринга, [data-collector/](data-collector/); Swagger UI — http://localhost:8081/swagger/index.html |
| mock-api | http://localhost:8090 | эмулятор внешних систем: АБС банка, ФНС, БКИ, Compliance, ФССП, КАД, Федресурс; [mock-api/](mock-api/) |
| dozzle | http://localhost:8888 | логи всех контейнеров в браузере |

Порты и доступы к БД — в `.env`.

## Документация

- [api/openapi.yaml](api/openapi.yaml) — REST-контракт для фронтенда и back-office (OpenAPI 3.1)
- [docs/fsm.md](docs/fsm.md) — FSM заявки: диаграмма, таблица переходов, клиентские этапы
- [docs/db-schema.md](docs/db-schema.md) — схема БД: справочник всех таблиц и колонок; ERD — [docs/db-schema.dbml](docs/db-schema.dbml) для dbdiagram.io
- [db/](db/) — миграции схемы (Goose) и команда `migrate`; `docker compose up migrate`
- [docs/websocket.md](docs/websocket.md) — WebSocket-события
- [docs/e-signature.md](docs/e-signature.md) — проверка электронных подписей (КЭП, МЧД), политики, демо-сценарии
- [docs/data-collection.md](docs/data-collection.md) — какие данные собираем, источники, скоринговая карта ЮЛ/ИП
- [docs/mock-apis.md](docs/mock-apis.md) — контракты моков внешних систем и тестовые сценарии A–E

Проверка контракта:

```bash
npx @redocly/cli@1.25.0 lint api/openapi.yaml
```
