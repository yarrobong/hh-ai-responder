# Validation Stage 18 — First HH Transport Probe Ready

Проверено 7 сентября 2026 года. Реальный `POST /chatik/api/send` не выполнялся;
подготовка остановлена непосредственно перед Send.

Целевой approved action: `hh-action-79317f5c5806ad82acb1cfac6e0587cd`.

## Fresh read-only sync

Выполнен точечный sync:

```text
hh sync conversation 5599440665
fetched=1, created=0, updated=0, unchanged=1, skipped=0, errors=0
```

После sync проверено:

- компания: `Сбер2B`;
- destination: `chat.id=5599440665`;
- negotiation/topic ID `5547831362` не используется как destination;
- последнее сообщение работодателя осталось `hh-message-15361510747`;
- reply policy: `REPLY_REQUIRED`;
- conversation: `SAFE_FOR_MANUAL_REPLY`;
- salary fact: `ANSWERABLE`;
- relevant knowledge hash: `4b8786c9278153d388ee9362e48a0197cb46c001bb55fd05dc0776d3f72b6574`;
- approved action не stale.

## Sanitized request preview

Preview выполнен offline, без network request:

- method: `POST`;
- endpoint: `https://chatik.hh.ru/chatik/api/send`;
- chatId: `5599440665`;
- Content-Type: `application/json`;
- idempotencyKey: `present` (значение скрыто);
- exact text:

  `Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить.`

Request validation: `VALID`.

## Safety preflight

Fresh safety preflight выполнен read-only и завершился:

```text
Safety: READY
Request validation: VALID
Write capability: BLOCKED_BY_DRY_RUN
send_request_performed: false
```

## Handoff

Env не изменялся. Для live режима пользователь должен вручную установить:

```env
HH_DRY_RUN=false
HH_WRITE_ENABLED=true
HH_MAX_WRITES_PER_RUN=1
```

После restart нужно снова выполнить fresh sync, safety preflight и request
validation. Dashboard должен показать только активную кнопку `Send to HH` при
состояниях `READY`, `VALID`, `ENABLED`. Кнопка Send не нажималась.

Transport lifecycle и sanitized response capture подготовлены; automatic retry
отсутствует. После пользовательского Send следующий шаг — только read-only
reconciliation.
