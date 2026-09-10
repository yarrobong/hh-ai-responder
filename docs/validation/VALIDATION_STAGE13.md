# Этап 13 — controlled production pilot readiness

Проверено 6 сентября 2026 года. Реальные HH write-actions не выполнялись.

## Readiness snapshot

Команда `hh write-status` показала:

| Показатель | Значение |
| --- | --- |
| HH write enabled | `false` |
| Dry run | `true` |
| Gateway | `BLOCKED_BY_DRY_RUN` |
| Legacy writes | `BLOCKED_BY_GATEWAY` |
| CareerMonitor | `READ_ONLY` |
| AIReplyOrchestrator | `NO_WRITE_CAPABILITY` |
| Pending approved actions | `0` |
| Лимит за процесс | `1` |
| Лимит за UTC-день | `5` |

Локальный безопасный отчёт `hh eligible` обработал 261 conversation:

| Класс | Количество |
| --- | ---: |
| `SAFE_FOR_MANUAL_REPLY` | 0 |
| `BLOCKED` | 151 |
| `MANUAL_REVIEW` | 110 |

Поэтому первый production send сейчас не разрешён: подходящего локально
подтверждённого сценария нет.

## Controlled flow

Для ручного пилота пользователь сам, в отдельном окружении, задаёт:

```env
HH_DRY_RUN=false
HH_WRITE_ENABLED=true
HH_MAX_WRITES_PER_RUN=1
HH_MAX_WRITES_PER_DAY=5
```

Затем в Dashboard выполняется только следующий порядок:

1. открыть Inbox;
2. выбрать реального работодателя;
3. Generate Draft;
4. проверить или Edit текст;
5. Approve exact text;
6. Run fresh preflight и убедиться в `READY_TO_SEND`;
7. нажать Send to HH.

Перед Send Dashboard показывает компанию, вакансию, HH destination, последнее
сообщение работодателя, exact approved text, source draft, использованные факты,
warnings и timestamp последнего preflight. Reload и double-click не создают
вторую попытку: action nonce одноразовый, а отправленное действие не sendable.

После HTTP success выполняется только read-only delivery check. Результат —
`delivery_confirmed` либо `sent_unconfirmed`; неоднозначный транспортный ответ
получает `delivery_uncertain`. Ни один из этих статусов не запускает retry.

Follow-up на этом этапе не выполнялся. Его можно проверять только отдельным
ручным циклом после появления подходящего сценария.

Проверки: `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`,
`node --check web/app.js`, `git diff --check`.
