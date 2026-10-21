# WebSocket

```
GET ws://localhost:8080/api/v1/ws?token=<token из /auth/mock-login>
```

Один сокет на вкладку. Сервер сам шлёт события по всем компаниям пользователя (для `UNDERWRITER` — плюс события очереди back-office), отдельная подписка не нужна.

**Принцип:** событие — это подсказка «перечитай», а не данные. Источник правды — REST. Поэтому потеря события не страшна: после reconnect фронт просто перечитывает открытые экраны (`GET /applications/{id}/status`).

## Конверт

```json
{
  "id": "evt_01JB9...",
  "type": "application.status_changed",
  "at": "2026-10-21T16:40:00Z",
  "data": { }
}
```

`id` монотонный (ULID). При переподключении можно передать `?token=...&last_event_id=evt_...`, и сервер дошлёт пропущенное из `application_events`. Это опционально: без него фронт всё равно перечитывает REST.

## Server → client

| type | data | Что делает фронт |
|---|---|---|
| `application.status_changed` | `{application_id, from, to, version, client_stage}` | перечитать `/status`; если `to` = OFFERED — `/offers` |
| `application.updated` | `{application_id, version}` | правки черновика из другой вкладки или от другого представителя → перечитать заявку |
| `info_request.created` | `{application_id, info_request_id}` | показать бейдж «нужно уточнить» |
| `document.recognized` | `{application_id, document_id, state}` | обновить карточку документа (распознанные поля) |
| `document.signatures_verified` | `{application_id, document_id, verdict, policy_satisfied}` | показать бейдж подписи; полный отчёт — `GET .../signature-report` |
| `offers.issued` | `{application_id, count, kind}` | `kind=RETENTION` — показать retention-оффер |
| `signing.requested` | `{application_id, required_signer_id}` | приходит **директору**: задача «подписать договор» |
| `signing.completed` | `{application_id}` | экран «кредит выдан» |
| `backoffice.queue_changed` | `{application_id, status}` | только `UNDERWRITER`: обновить очередь |

Наружу никогда не уходят события по отдельным источникам (`source.*`): клиент не должен видеть, где мы собираем данные. Подстатусы источников доступны только через `GET /backoffice/applications/{id}`.

## Client → server

| type | data | |
|---|---|---|
| `ping` | `{}` | сервер отвечает `pong`. Также раз в 30 с шлётся WS ping frame |

## Реализация на Go (заметка для бэка)

* `LISTEN/NOTIFY` в Postgres: транзакция перехода FSM делает `pg_notify('app_events', <event_id>)`, а WS-хаб в каждом инстансе API слушает канал и раздаёт событие подключённым пользователям компании. Так работает при нескольких репликах без Redis.
* Воркеры pgqueue меняют статус через тот же FSM-сервис, поэтому события возникают одинаково, кто бы ни сделал переход.
