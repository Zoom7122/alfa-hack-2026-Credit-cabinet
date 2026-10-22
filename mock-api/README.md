# mock-api

Эмулятор внешних систем для локальной разработки и демо. Отдельный Go-модуль (gin), с остальными модулями общается только по HTTP.

Каждая система — своя папка с моками внутри:

| Папка | Префикс | Что эмулирует |
|---|---|---|
| [Bank/](Bank/) | `/bank/v1` | внутренние системы банка (источники 1–3 в [docs/data-sources.md](../docs/data-sources.md)) |

Моки — JSON-файлы в папке системы, встроены в бинарник. Чтобы поменять ответ, правим JSON и пересобираем.

## Bank

На любой ИНН из 10 или 12 цифр отдаётся один и тот же ответ, на другой — `400`.

| Эндпоинт | Мок | Что внутри |
|---|---|---|
| `GET /bank/v1/companies/{inn}/accounts` | `accounts.json` | счета, остатки, транзакции (формат Open Banking) |
| `GET /bank/v1/companies/{inn}/credits` | `credits.json` | кредиты, долг, просрочки |
| `GET /bank/v1/companies/{inn}/acquiring` | `acquiring.json` | терминалы, оборот по картам, СБП |
| `GET /bank/v1/companies/{inn}/salary-project` | `salary_project.json` | зарплатный проект: реестры, ФОТ |
| `GET /bank/v1/companies/{inn}/card-index` | `card_index.json` | картотека: неоплаченные требования, блокировки, аресты |

Моки — единственный источник тестовых данных банка. `data-collector` копий не держит: его контрактный тест ходит в запущенный mock-api (`make test-integration` в корне после `make up`) и падает, если в моке появилось поле, которого нет в `data-collector/internal/domain/bank.go`.

## Запуск

```bash
go run ./cmd/mock-api       # HTTP_ADDR по умолчанию :8090
curl -s localhost:8090/bank/v1/companies/7701234567/accounts
```

В docker-compose поднимается вместе со всем стеком (`make up` в корне).
