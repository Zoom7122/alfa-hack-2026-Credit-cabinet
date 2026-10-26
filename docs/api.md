# API текущего backend

Этот документ описывает маршруты, зарегистрированные актуальным `data-collector/cmd/data-collector/main.go`, и границы API credit-engine. Прежний проектный OpenAPI заменён. WebSocket, PATCH анкеты, отдельный submit, отказ от предложения и КЭП договора не объявляются действующими backend-операциями.

## Адреса и спецификации

| Участник | Адрес по умолчанию | Контракт |
|---|---|---|
| data-collector напрямую | `http://localhost:8081/api/v1` | [основной OpenAPI 3.0.3](../api/openapi.yaml) |
| data-collector через frontend | `http://localhost:3000/collector/api/v1` | те же разрешённые клиентские операции; префикс `/collector` срезается |
| Swagger UI data-collector | [localhost:8081/swagger/index.html](http://localhost:8081/swagger/index.html) | [Swagger JSON](../data-collector/docs/swagger.json), [YAML](../data-collector/docs/swagger.yaml); динамический `/swagger/doc.json` |
| scoring через gateway | `http://localhost:8082` | [runtime](../credit-engine/contracts/openapi.json), [метаданные моделей](../credit-engine/contracts/scoring_metadata.openapi.json) |
| admin credit-engine | `http://localhost:8084` | [административный OpenAPI](../credit-engine/contracts/admin.openapi.json) |
| mock-api | `http://localhost:8090` | [описание эмуляторов](../mock-api/README.md), [источники и запросы](mock-api-guide.md) |

Порты изменяются в `.env`. Адреса `data-collector:8081`, `credit-engine-gateway:8082`, `kafka:9092` предназначены для сети Docker. В актуальном Compose нет HTTP-сервиса `api:8080`.

Swagger 2 использует общий `basePath: /api/v1`, поэтому отдельная проверка процесса `GET /healthz` описана в OpenAPI 3 с сервером без этого префикса. OpenAPI 3 также уточняет разные JSON/multipart-тела; Swagger UI для анкеты показывает multipart с JSON-строкой `questionnaire`.

## Вход и доступ

1. `GET /auth/test-users` возвращает синтетических пользователей.
2. `POST /auth/mock-login` с `{"user_id":"usr_director_north"}` возвращает `token` и `user`.
3. Далее передавать `Authorization: Bearer mock.usr_director_north` либо полученный `token`.

Это демонстрационная авторизация, пароля и OAuth-провайдера нет. Список компаний, список заявок, `/me` и поиск по ИНН требуют входа. Back-office требует пользователя с ролью `UNDERWRITER`. Подписать договор может директор компании или сам ИП; код не показывается другим представителям.

Регистрация допускает анонимную заявку. При наличии токена представителем становится вошедший клиент; сотрудник банка зарегистрировать кредитную заявку не может. Черновик, отправка анкеты, ответы и изменение вложений проверяют автора: для заявки тестового пользователя нужен его токен. Чужой черновик не включается в список.

Часть чтений по ID в текущем MVP не требует входа: карточка заявки, задача сбора, история и результаты. У списка/карточки финансовых документов дополнительно стоит `DocumentAccess`: читать может автор, представитель компании или андеррайтер, изменять — автор; для анонимной заявки допускается анонимный путь. При неверном переданном токене middleware возвращает `401`, даже если операция допускает запрос без заголовка. В OpenAPI такие условные режимы отражены необязательной схемой bearer; точные права зависят от заявки.

Служебные `/borrowers/{id}/statements`, `/credit-events`, `/check-mock` не требуют отдельного сервисного секрета data-collector в локальном MVP. Nginx кабинета их не проксирует; они доступны напрямую на порту API.

## Полный перечень data-collector

Пути ниже относительно `/api/v1`, кроме `/healthz`.

### Вход, компании и каталог

| Метод и путь | Результат |
|---|---|
| `GET /auth/test-users` | `200`, `{items: [...]}` тестовых пользователей |
| `POST /auth/mock-login` | `200`, `{token, user}`; JSON `user_id` |
| `GET /me` | `200`, пользователь и доступные компании |
| `GET /applicants` | `200`, компании вошедшего пользователя |
| `POST /applicants` | `201`, заявка-черновик; `Location: /api/v1/applications/{id}` |
| `POST /counterparty/lookup` | `200`, реквизиты по JSON `inn`; реестр опрашивается синхронно |
| `GET /products` | `200`, продукты; фильтры `purpose`, `applicant_type` |
| `GET /products/{code}` | `200`, описание продукта |
| `POST /products/{code}/calculate` | `200`, расчёт по JSON `amount`, `term_months` |

`POST /applicants` принимает `RegisterApplicantRequest`: `representative.full_name`, `representative.role`, `company.inn` обязательны. Повторная регистрация может вернуть существующий собственный черновик компании; ID компании и ID заявки различаются.

### Заявка, черновик, сбор и решение

| Метод и путь | Результат |
|---|---|
| `GET /applications` | `200`, заявки; `company_id`, `status` через запятую, `mine=true` |
| `GET /applications/{id}` | `200`, текущая заявка, документы, реквизиты, последняя задача |
| `GET /applications/{id}/draft` | `200`, `{data, version, updated_at}` |
| `PUT /applications/{id}/draft` | `200`, сохранённый черновик; JSON `{data, version}` |
| `POST /applications/{id}/questionnaire` | `202`, задача сбора; `Location: /api/v1/collections/{id}` |
| `GET /collections/{id}` | `200`, состояние задачи, результаты/пропуски источников |
| `GET /applications/{id}/timeline` | `200`, клиентская история |
| `GET /applications/{id}/decision` | `200`, клиентский итог; до решения возможен `404` |
| `GET /applications/{id}/offers` | `200`, `{items: [...]}` предложений |
| `GET /applications/{id}/info-requests` | `200`, `{items: [...]}` вопросов банка |
| `POST /applications/{id}/info-requests/{requestId}/answer` | `200`, статус и актуальные вопросы |

`PUT /draft` принимает частично заполненные поля в `data` и текущую `version` из предыдущего ответа. Неизвестные поля не принимаются; тело ограничено 128 КиБ. Это не `PATCH merge-patch` и не универсальный `If-Match`. Несовпадение версии даёт `409`.

Отправка анкеты принимает:

- `application/json`: объект `QuestionnaireRequest`, без файлов.
- `multipart/form-data`: обязательная часть `questionnaire` со строкой JSON и повторяемые части `files`.

В JSON обязательны `age`, `marital_status`, `children_count`. `product_code`, `requested`, `purpose` задают запрос кредита. `director`, структурированные `consents`, `other_bank_biks`, `marketplace_tokens`, `ofd_token`, `collateral`, МЧД и `scoring` определяют доступные источники и данные оценки. Черновик хранит форму UI: его `consents` отличаются от структурированных согласий отправляемой анкеты, поэтому нельзя отправлять `draft.data` как готовую анкету без преобразования.

Допускается до 10 файлов, включая подписи, каждый до 20 МиБ. Типы задаются в `documents` по имени файла. Пара `balance.pdf` + `balance.pdf.sig` связывается при приёме. Повторная отправка уже отправленной анкеты — `409`; новые вопросы обслуживаются ручкой `answer`.

Ответ на вопрос банка принимает JSON `InfoRequestAnswerInput` (`value`, `comment`, `document_ids`) либо multipart: часть `answer` с тем же JSON и части `files`. Тип `value` определяется `input.type` самого вопроса: число, код варианта или строка; для `NONE` значение не нужно. Уже загруженные и проверенные файлы передаются по `document_ids`. После всех обязательных ответов возможны повторная оценка либо возврат специалисту.

### Документы и проверки

| Метод и путь | Результат |
|---|---|
| `GET /applications/{id}/documents` | `200`, список метаданных и состояний обработки |
| `POST /applications/{id}/documents` | `201`, карточка загруженного документа |
| `GET /applications/{id}/documents/{documentId}` | `200`, карточка документа |
| `DELETE /applications/{id}/documents/{documentId}` | `204`, мягкое удаление документа и связанных подписей |
| `GET /applications/{id}/documents/{documentId}/content` | `200`, исходные байты и `Content-Disposition: attachment` |
| `GET /applications/{id}/documents/{documentId}/signature-report` | `200`, `signature.Report` либо `financial.SignatureReport`; `404 REPORT_NOT_READY`, если отчёта ещё нет |
| `GET /applications/{id}/documents/{documentId}/financial-result` | `200`, результат обработки; `202 {state:"PENDING"}`, пока результата нет |
| `POST /applications/{id}/documents/{documentId}/signature-verification` | `202`, карточка после постановки повторной обработки |

Загрузка отдельного документа — multipart: обязательные `file` и `type`, до двух частей `signatures`; необязательный `info_request_id`. Для добавления только подписи указать `type=SIGNATURE` и `signed_document_id`, а `signatures` не передавать. Это отдельный формат, отличный от multipart анкеты. Прямой обработчик допускает до 20 МиБ на файл; nginx frontend ограничивает весь запрос отдельной загрузки 22 МиБ.

Изменения допускаются в `DRAFT` или `WAITING_FOR_INFO`. Удаление вне черновика требует привязки к ещё открытому вопросу банка. Повтор обработки сохраняет оригинальные байты; для финансовых документов создаётся новое поколение, для обычных — задача проверки подписи. У встроенного тестового PDF обработка намеренно пропускается, чтобы он не влиял на скоринг.

Финансовый парсер поддерживает оговорённые контексты КУДиР ИП на УСН «доходы» и отчётности ООО. Неподдерживаемый контекст не становится проверенными данными. Подробнее — [финансовые документы](financial-documents.md).

### Предложение и демонстрационное подписание

| Метод и путь | Результат |
|---|---|
| `POST /applications/{id}/offers/{offerId}/accept` | `200`, сессия подписания; переход в `SIGNING` |
| `GET /applications/{id}/signing` | `200`, сессия, подписант, договор, срок, попытки; код — только подписанту |
| `POST /applications/{id}/signing/confirm` | `200`, подтверждённая сессия; JSON `{code, method?:"SIMPLE_CODE"}` |

Код действует 10 минут, доступны три попытки. Неверный код — `422 SIGNATURE_INVALID`, истёкший — `410 CODE_EXPIRED`; исчерпание попыток/проверка истечения возвращают заявку в `OFFERED`. Успешное подтверждение переводит в `COMPLETED`, удаляет код и записывает учебную выдачу. Повтор успешного подтверждения не создаёт вторую выдачу. Договор действующего backend — демонстрационный текстовый файл `.txt`; SMS, КЭП договора и реальные деньги не используются.

### Рабочее место банка

| Метод и путь | Результат |
|---|---|
| `GET /backoffice/applications` | `200`, очередь, счётчики, курсор; фильтры `status`, `assignee`, `limit`, `cursor` |
| `GET /backoffice/applications/{id}` | `200`, досье, источники, причины, скоринг, условия; заголовок `ETag` |
| `POST /backoffice/applications/{id}/assign` | `200`, заявка назначена вошедшему андеррайтеру |
| `POST /backoffice/applications/{id}/decision` | `200`, обновлённое досье; JSON с `action` и данными решения |

Действия — `APPROVE`, `REQUEST_INFO`, `REJECT`. Для решения поддержаны необязательные `If-Match` (версия из досье, конфликт `412`) и `Idempotency-Key` (повтор без второй операции). Это поведение конкретной ручки; оно не заявляется для всех POST кабинета. Назначение чужому исполнителю и недопустимый статус дают `409`.

### Служебные операции

| Метод и путь | Назначение |
|---|---|
| `GET /borrowers/{id}/statements?years=2024,2025` | отчётность поставщика Б для credit-engine; `id` — компания `apl_…`, `years` необязателен |
| `POST /credit-events` | приём события банка; JSON `decision.CreditEvent`, обязательный `event_id`; `202 {event_id, accepted:true}` |
| `GET /check-mock?inn=7799000012` | диагностический запрос в старый `/bank/v1` mock-api |
| `GET /healthz` от корня `http://localhost:8081` | `{status:"ok"}`, доступность HTTP-процесса |

Служебные операции отсутствуют в allowlist frontend nginx. Не путать их с `/api/v1/credit-events` административного сервиса credit-engine: у админки собственная сервисная авторизация.

## Форматы и ошибки

- Идентификаторы: `usr_…` — пользователь, `apl_…` — компания, `app_…` — заявка, `tsk_…` — задача, `doc_…` — документ. ИНН не подставляется вместо ID.
- Деньги и сроки в обычных запросах кабинета — числовые поля, денежные суммы в рублях. Ставки — проценты годовых. Финансовые индикаторы парсера могут иметь `amount` как десятичную строку; исходная отчётность ФНС содержит суммы в тысячах рублей. Единицу определяет конкретная схема.
- Время API — RFC 3339, календарные даты — `YYYY-MM-DD`. Синтетическая дата общего стека — `2026-10-22`.
- `202` означает принятие/ожидание обработки. Завершение проверять чтением заявки, задачи, документа или результата.
- Ошибки транспорта кабинета обычно имеют `Content-Type: application/problem+json`: `type`, `title`, `status`, `code`, необязательные `detail` и `errors[]` с `field`, `message`, `code`.
- У inline-загрузчика документов часть ошибок (`INVALID_MULTIPART`, `INVALID_FILES`, `FILE_TOO_LARGE`, `INVALID_SIGNATURE_LINK`) возвращается как `application/json` с одним `code`. Клиент должен обрабатывать оба варианта.
- Типичные коды HTTP: `400` — формат, `401/403` — вход/права, `404` — ресурс/ещё нет результата, `409` — состояние/версия, `410` — истечение, `412` — версия решения андеррайтера, `413` — размер, `415` — тип тела, `422` — правила данных, `500/504` — внутренняя ошибка/таймаут реестра.

Клиент использует polling. Backend data-collector не регистрирует `/ws`, `/applications/{id}/status`, PATCH заявки, отдельный `/submit`, ручку отказа от предложения или обновление одного источника. Код MSW для старых демонстрационных экранов не означает наличие таких backend-ручек.

## API credit-engine

Credit-engine сохраняет собственные действующие контракты. Основной OpenAPI кабинета не смешивает три разных сервиса под одним URL.

| Контракт | Участник и доступ |
|---|---|
| [Runtime OpenAPI](../credit-engine/contracts/openapi.json) | `POST /api/v1/decisions` — scoring; `/api/v1/bank/*` — bank; отчётность/события — интерфейсы внешних участников |
| [Scoring metadata](../credit-engine/contracts/scoring_metadata.openapi.json) | `GET /api/v1/models` на scoring, версии, SHA-256 и фактический каталог |
| [Admin OpenAPI](../credit-engine/contracts/admin.openapi.json) | `/api/admin/v1/*`, административная cookie-сессия и CSRF; отдельный приём событий с сервисным токеном |
| [Scoring facts](../credit-engine/contracts/scoring_facts.openapi.json) | контракт дополнительного поставщика для моделей, которым нужны facts; не отдельный контейнер общего Compose |

С хоста gateway `localhost:8082` открывает scoring, а `/api/v1/bank/*` возвращает `404`. Внутренний listener `credit-engine-gateway:8082` маршрутизирует и scoring, и bank. Админка слушает `127.0.0.1:8084`; её тестовый логин — в корневом `.env`. Подробнее: [интеграция](../credit-engine/docs/05_integration.md), [доступ админки](../credit-engine/docs/24_admin_access.md), [применение моделей](../credit-engine/README.md).

## Воспроизведение и обновление

Примеры запросов: [requests.http](../api/requests.http), [JSON A–E](../data-collector/examples/), [полные curl-сценарии](run-scenarios.md#11-ae-через-api-готовые-команды-и-тела). HTTP-коллекция не создаёт данные автоматически: выполнять блоки последовательно и переносить полученные ID в переменные.

```bash
# npm ci в frontend выполняется один раз; нужны Go 1.26.8 и Node 24.22.x.
make api-docs
```

Порядок: Go-аннотации/DTO → `make -C data-collector swagger` → Swagger 2 → `frontend/scripts/generate-openapi.mjs` → OpenAPI 3 → `openapi-typescript` → `frontend/src/api/collector/schema.d.ts`. `--parseFuncBody` включает аннотации inline-маршрутов документов; аннотация у `questionnaireRequest` экспортирует схему JSON-варианта без вымышленного маршрута. Скрипт экспорта уточняет возможности, которые Swagger 2 не выражает одним телом.

Не править сгенерированные файлы отдельно от источников. Старый OpenAPI удалён. `frontend/src/api/schema.d.ts` содержит только внутренние статические типы старых MSW-экранов; команды генерации backend его не используют.
