# alfa-hack-2026-Credit-cabinet

Умный кредитный кабинет предпринимателя (Альфа-Хак 2026).

## Документация

- [api/openapi.yaml](api/openapi.yaml) — REST-контракт для фронтенда и back-office (OpenAPI 3.1)
- [docs/fsm.md](docs/fsm.md) — FSM заявки: диаграмма, таблица переходов, клиентские этапы
- [docs/websocket.md](docs/websocket.md) — WebSocket-события
- [docs/e-signature.md](docs/e-signature.md) — проверка электронных подписей (КЭП, МЧД), политики, демо-сценарии
- [docs/data-collection.md](docs/data-collection.md) — какие данные собираем, источники, скоринговая карта ЮЛ/ИП

Проверка контракта:

```bash
npx @redocly/cli@1.25.0 lint api/openapi.yaml
```
