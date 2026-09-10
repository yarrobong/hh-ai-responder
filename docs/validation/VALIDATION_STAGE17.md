# Validation Stage 17 — HH Write Transport Compatibility Audit

Проверено 7 сентября 2026 года. Второй реальный HH write не выполнялся.
Команда `hh action request-preview` построила request offline; safety preflight
выполнил только read-only HH чтение.

## Историческая попытка

В локальном audit сохранён фактический action ID
`hh-action-557ad50a956e89ceedb1bd6028bf4caa`. Он отличается от ID, указанного
в исходном контексте (`...b1cf...`), поэтому ниже не смешиваются две записи.

Сохранено:

- HTTP status: `400`;
- результат: `failed`;
- external message ID: отсутствует;
- локальное outgoing HH-сообщение: не создано;
- retry: не выполнялся.

Исторические endpoint, method, request content type, payload и response body в
audit не были записаны. По legacy path и сохранённому action request
реконструируется так:

- endpoint: `https://chatik.hh.ru/chatik/api/send`;
- method: `POST`;
- request content type: `application/json`;
- destination: `chat_id=5599440665`;
- observed negotiation/topic ID: `5547831362` (not sent to this endpoint);
- payload fields: `chatId`, `text`, `idempotencyKey`;
- safe request header names: `Accept`, `Content-Type`, `Referer`, `X-Requested-With`,
  `X-Xsrftoken` plus the standard browser headers;
- response content type/body: **не сохранены**.

В сохранённом HH read fixture `5599440665` — это `chat.id` типа
`NEGOTIATION`; связанный `NEGOTIATION_TOPIC` имеет отдельный ID `5547831362`.
Следовательно, read conversation destination не был ошибочно принят за topic
ID: write adapter использует именно numeric chat ID, как legacy send path.

Точная причина старого 400 неизвестна: достоверна только категория
`bad_request`. Нельзя доказательно выбрать между payload validation, stale
state, identifier validation и другим HH validation error без response body.

## Контракт и защита

Новый `HHWriteRequestPreview` строит тот же request shape без network I/O.
`ValidateHHWriteRequest` до `HHRequester.Do` проверяет endpoint, method,
destination type/ID, обязательные payload fields, UTF-8 и content type.
Failed transport responses теперь сохраняют только bounded sanitized body,
response content type и безопасные request metadata. Cookies, Authorization,
XSRF values и session identifiers не сохраняются.

## Текущий approved action

Для `hh-action-79317f5c5806ad82acb1cfac6e0587cd` выполнено:

- Safety preflight: `READY`;
- request validation: `VALID`;
- write capability: `BLOCKED_BY_DRY_RUN`;
- send request performed: `false`.

Request preview:

- `POST https://chatik.hh.ru/chatik/api/send`;
- destination type: `chat_id`;
- destination ID: `5599440665`;
- content type: `application/json`;
- payload:

  ```json
  {
    "chatId": 5599440665,
    "text": "Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить.",
    "idempotencyKey": "<approved action nonce>"
  }
  ```

- headers: only safe header names/presence are shown by the CLI.

Lifecycle remains explicit: `approved → preflight_passed → send_started →
transport_response → delivery_confirmed`. `send_started` is counted as a write
attempt, not as a sent message. Metrics expose `write_attempts_total` while
retaining `manual_writes_total` as a backward-compatible attempts alias.
