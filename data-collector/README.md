# data-collector

Модуль сбора данных о заёмщике для передачи в скоринг. Независимый Go-модуль: свой `go.mod`, ничего не импортирует из остального репозитория.

Стек: Go, gin. Хранилище анкет пока in-memory заглушка; Postgres (pgx + Jet, схема в [db/](../db/)) подключается следующим шагом через тот же интерфейс `service.Repository`.

## Слои

| Слой | Пакет | Что делает |
|---|---|---|
| Приём запросов | `internal/transport/http` | все эндпоинты — в `router.go`; разбор запросов, ответы и ошибки (RFC 9457, `application/problem+json`) |
| Бизнес-логика | `internal/service` | `QuestionnaireService` — правила анкеты; `InternalDataService` — параллельный опрос систем банка |
| Хранилище | `internal/repository/memory` | анкеты; заглушка в памяти, позже `internal/repository/postgres` |
| Внешние системы | `internal/client/bank` | HTTP-клиент к внутренним системам банка (локально — эмулятор [mock-api](../mock-api/)) |

Сущности — `internal/domain`. Сборка зависимостей — `cmd/data-collector/main.go`.

`InternalDataService` хранилища не получает: только запрашивает данные и отдаёт их.

## Запуск

```bash
make run                # HTTP_ADDR по умолчанию :8081
make test               # юнит-тесты, внешние сервисы не нужны
make test-integration   # контракт с банком: нужен запущенный mock-api
make swagger            # перегенерировать docs/ после правки аннотаций
```

| Переменная | По умолчанию | Что это |
|---|---|---|
| `HTTP_ADDR` | `:8081` | адрес HTTP-сервера |
| `BANK_API_URL` | `http://localhost:8090/bank/v1` | корень API банка (эмулятор mock-api) |
| `BANK_API_TIMEOUT` | `5s` | таймаут одного запроса к банку |

## API

Swagger UI: http://localhost:8081/swagger/index.html (спецификация — `/swagger/doc.json`).

Спецификацию генерирует [swaggo](https://github.com/swaggo/swag) из аннотаций `@Summary`, `@Param`, `@Router` и т. д. над хендлерами и из `@title` в `main.go`. Результат лежит в `docs/` и коммитится, поэтому для сборки swag не нужен. Поменял ручку или DTO — запусти `make swagger` и закоммить `docs/` вместе с кодом.

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

`GET /api/v1/check-mock?inn=7701234567`

Параллельно опрашивает пять систем банка и возвращает их ответы как есть: `accounts` (расчётный счёт), `credits` (кредиты), `acquiring` (эквайринг), `salary_project` (зарплатный проект), `card_index` (картотека). В `sources` — итог по каждой системе: `DONE` или `FAILED` с текстом ошибки и временем ответа. Если система не ответила, её секция `null`, а ответ всё равно `200`.

Ошибки: `400` — нет `inn`; `422` — `inn` не из 10 или 12 цифр.

```bash
curl -s 'localhost:8081/api/v1/check-mock?inn=7701234567'
```

`GET /healthz` — проверка, что сервис поднят.
