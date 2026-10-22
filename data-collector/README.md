# data-collector

Модуль сбора данных о заёмщике для передачи в скоринг. Независимый Go-модуль: свой `go.mod`, ничего не импортирует из остального репозитория.

Стек: Go, gin. Хранилище пока in-memory заглушка; Postgres (pgx + Jet, миграции Goose в `migrations/`) подключается следующим шагом через тот же интерфейс `service.Repository`.

## Слои

| Слой | Пакет | Что делает |
|---|---|---|
| Приём запросов | `internal/transport/http` | разбор JSON, ответы и ошибки (RFC 9457, `application/problem+json`) |
| Бизнес-логика | `internal/service` | правила анкеты, присвоение `id`, вызов хранилища |
| Хранилище | `internal/repository/memory` | заглушка в памяти; позже `internal/repository/postgres` |

Сущности — `internal/domain`. Сборка зависимостей — `cmd/data-collector/main.go`.

## Запуск

```bash
make run          # HTTP_ADDR по умолчанию :8081
make test
```

## API

`POST /api/v1/questionnaires`

```json
{ "age": 34, "marital_status": "MARRIED", "children_count": 2 }
```

| Поле | Тип | Правило |
|---|---|---|
| `age` | int | 18–100, полных лет |
| `marital_status` | string | `SINGLE`, `MARRIED`, `DIVORCED`, `DIVORCED_MULTIPLE` (разведены 2 и более раз) |
| `children_count` | int | 0–20, дети и/или иждивенцы |

Ответы: `201` с анкетой и `id` вида `qst_<ULID>`; `400` — битый JSON, нет поля или неверный тип; `422` — нарушено правило из таблицы. Ошибки — `Problem` с `code: VALIDATION_FAILED` и списком `errors[]`.

```bash
curl -i -X POST localhost:8081/api/v1/questionnaires \
  -H 'Content-Type: application/json' \
  -d '{"age":34,"marital_status":"MARRIED","children_count":2}'
```

`GET /healthz` — проверка, что сервис поднят.
