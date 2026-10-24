# Frontend — умный кредитный кабинет

React 19 + TypeScript + Vite, UI-кит Альфа-Банка `@alfalab/core-components`, стили — обычный CSS по БЭМ на токенах кита (`@alfalab/core-components-vars`).

## Запуск

```bash
npm ci
npm run dev        # http://localhost:5173
```

Бэкенда пока нет: запросы к `/api/v1` перехватывает [MSW](https://mswjs.io) (`src/mocks`), ответы — по контракту `../api/openapi.yaml`.
Чтобы ходить в настоящий API, скопируйте `.env.example` в `.env` и поставьте `VITE_API_MOCK=false` — dev-сервер проксирует `/api` на `API_PROXY_TARGET`. `VITE_API_BASE_URL` меняет базу REST (по умолчанию `/api/v1`).

Изменения на моках (черновики, привязанные компании, документы) хранятся в `localStorage` браузера под ключом `credit-cabinet:mock-db:v2` и переживают перезагрузку. Чтобы вернуть исходные данные, удалите этот ключ в DevTools.

Версия Node — в `.nvmrc` (`nvm use`), зависимости закреплены точно (`.npmrc`: `save-exact`).

## Docker

Образ собирается в два этапа: Node собирает статику, nginx её раздаёт и проксирует `/api` (включая WebSocket) на `API_UPSTREAM`.

```bash
make front                         # из корня репозитория: собрать и поднять только фронт
# или вместе со всеми сервисами
make up
```

Фронт откроется на http://localhost:3000. Настройки — в корневом `.env`:

| Переменная | По умолчанию | Что делает |
|---|---|---|
| `FRONTEND_PORT` | `3000` | порт на хосте |
| `FRONTEND_API_MOCK` | `true` | `true` — данные отдаёт MSW прямо в браузере, бэкенд не нужен; `false` — запросы идут в API. Значение вшивается при сборке; `make up` и `make front` пересобирают образ сами |
| `FRONTEND_API_UPSTREAM` | `http://api:8080` | адрес API в сети compose: схема, хост и порт **без пути** |

nginx стартует и без поднятого API: имя upstream резолвится при каждом запросе через DNS Docker (`NGINX_RESOLVER=127.0.0.11`; вне Docker поменяйте).
Заголовки безопасности лежат в `nginx/snippets/security-headers.conf` и подключаются в каждый `location`.

Моки работают только в secure context (`localhost` или https): Service Worker на `http://<ip>` браузер не запустит, и приложение пойдёт в настоящий API.
После переключения с моков на API на том же адресе старый worker можно снять в DevTools → Application → Service Workers → Unregister.

## E2E-тесты

[Playwright](https://playwright.dev), тесты лежат в `e2e/`:

| Файл | Что проверяет |
|---|---|
| `login.spec.ts` | список тестовых пользователей, вкладки ролей, выбор стрелками, вход, сессия после перезагрузки |
| `dashboard.spec.ts` | задачи «Ждут вашего действия», фильтр заявок, статусы по FSM, карточка компании, выход, нет вложенных интерактивных элементов |
| `auth.spec.ts` | гарды по ролям (гость, клиент, андеррайтер), ошибки `/me`: 500 без петли редиректов, 401 со сбросом токена |
| `products.spec.ts` | каталог по задаче, «уже есть заявка» (409), пересчёт платежа, стрелки в radiogroup, сумма вне диапазона |
| `wizard.spec.ts` | создание черновика (своя и новая компания по ИНН, 404, ликвидация, таймаут реестра, 409), предзаполнение, автосохранение и его сбои (5xx, 422 по полю, 412, досохранение после перезагрузки), «Сохранить и выйти», сумма/срок/цель, «нет долгов», поручитель с неверным ИНН, загрузка нескольких файлов и тип документа, отправка (согласия, `CONSENT_REQUIRED`, `DRAFT_INCOMPLETE`) |

```bash
npx playwright install chromium   # один раз: браузер для тестов
npm run test:e2e                  # сам поднимет dev-сервер на моках
npm run test:e2e:ui               # интерактивный режим
E2E_BASE_URL=http://localhost:3000 npm run test:e2e   # против контейнера (make front)
```

Тесты опираются на синтетические данные из `src/mocks/data.ts`: если меняете моки, поправьте ожидания.

Ответы с ошибкой мок отдаёт по флагу в `localStorage` — так тесты проверяют сбои, не трогая обработчики. Пример: `credit-cabinet:mock-fail` = `[{"method":"PATCH","path":"/applications/","status":500,"times":1}]`. Необязательные поля: `code` (код Problem) и `field` (поле в `errors`). Хелперы для тестов — `setMockFailures` и `clearMockFailures` в `e2e/helpers.ts`.

## Структура

| Папка | Что внутри |
|---|---|
| `src/api` | `schema.d.ts` (генерируется из OpenAPI), клиент `openapi-fetch`, хуки TanStack Query, сессия мок-логина |
| `src/mocks` | сиды (`data.ts`), изменяемое хранилище в `localStorage` (`store.ts`), MSW-обработчики |
| `src/app` | роутинг, гард `RequireAuth` (роль + 401), хуки сессии `useAuth` |
| `src/pages` | страницы; каждый БЭМ-блок — свой `.tsx` + `.css` (например `LoginPage/components/PersonaCard`). Анкета — `WizardPage`: шаги в `steps/`, автосохранение в `useDraft.ts` (PATCH merge-patch + If-Match, очередь запросов, повтор при 412) |
| `src/shared` | общие компоненты (шапка, логотип, сумма+срок со слайдерами, платёж, карточка формы) и утилиты (форматирование, словари, merge-patch, Idempotency-Key) |

## Команды

- `npm run gen:api` — перегенерировать типы после изменения `api/openapi.yaml`
- `npm run typecheck`, `npm run lint`, `npm run build`
- `npm run test:e2e` — e2e-тесты
