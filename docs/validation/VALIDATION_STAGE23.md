# Validation Stage 23 — Real UX and Third Controlled Pilot

Date: 2026-09-07 (Asia/Yekaterinburg)

Validation ran against the real local dataset with:

```text
HH_WRITE_ENABLED=false
HH_DRY_RUN=true
HH_MAX_WRITES_PER_RUN=1
HH_MAX_WRITES_PER_DAY=5
```

No HH write was executed.

## REAL UX

The real browser flow was:

```text
Overview → Inbox → ИТЛ Консалтинг conversation
```

The conversation rendered from local data first. Its targeted HH refresh ran
in the background and finished after the conversation UI was already usable.
No Full Sync Conversations was invoked and the Sync page was not required.

Measured local API timings:

| Flow | Cold | Warm |
| --- | ---: | ---: |
| Dashboard local render | 149.5 ms | 0.9 ms |
| Inbox local render | 138.7 ms | 2.1 ms |
| Conversation local render | 16.4 ms | 12.3 ms |
| Health | 1.1 ms | 1.3 ms |

The repeated browser flow reached an interactive ИТЛ conversation in about
203 ms after the Inbox link was activated. During the stale first open,
interactive local content was visible before the automatic targeted refresh
finished:

```text
Targeted display refresh start: 2026-09-07T01:27:32.190Z
Targeted display refresh finish: 2026-09-07T01:27:35.295Z
Duration: 3.104 s
HH detail requests: 1
Fetched: 1
Updated: 1
```

The refresh used the known `chat.id=5599452149` directly. The latest employer
message remained:

```text
Напишите,пожалуйста,уровень дохода вы рассматриваете?
```

No new employer or candidate message appeared.

## Scheduler and targeted reads

No Inbox background refresh was active during the pilot (`HH_BACKGROUND_INBOX_REFRESH`
is unset/false). The priority scheduler regression test passed: safety reads
are granted before queued background reads, while the global HH rate limiter
remains in force.

The fresh preflight button performed the complete targeted flow in one user
action. It did not perform a full conversation sync:

```text
Targeted conversation update: 1 HH detail request, ~201 ms
Fresh safety read: 1 HH detail request, ~1.083 s
Preflight total: ~1.090 s
```

Total targeted HH detail reads in this validation flow: 3 (one display refresh
and two reads belonging to the single preflight action). Full Sync requests: 0.

## THIRD PILOT

Company: ИТЛ Консалтинг  
Vacancy: Разработчик БД (PostgreSQL, Middle)  
Conversation: `conversation-5dc9b17e2db65f5637dba17dfffa35b9`  
Destination chat.id: `5599452149`

Current action:

```text
hh-action-a31ba56bedf1664d3cf2983d9e84448f
```

Action validation:

```text
exists: yes
status: approved
nonce persisted: yes
nonce length: 40
nonce used: no
content hash: f12ff1a551ede6b7ff093a0eea6cd195a2179cf1ecbebea20bb1567cf6d9266c
RelevantKnowledgeHash current: yes
external_message_id: absent
transport attempt: absent
```

Exact approved/outgoing text is unchanged:

```text
Рассматриваю предложения от 40–50 тыс. рублей, целевой диапазон — 50–100 тыс. рублей. Варианты выше 100 тыс. рублей тоже готов обсудить.
```

Final dry-run validation:

```text
Safety: READY_TO_SEND
Request validation: VALID
Write capability: BLOCKED_BY_DRY_RUN
HHWriteClient: NOT CALLED
transport_attempted: false
nonce consumed: false
Conversation freshness: FRESH
```

The main card selected the current approved action by lifecycle priority.
Historical failed actions remain in `Previous attempts / History (1)` and do
not replace the current action. The UI showed `READY_TO_SEND`, `VALID`,
`BLOCKED_BY_DRY_RUN`, and `Send to HH (blocked safely)`.

```text
THIRD PILOT — READY FOR USER SEND
```

The process intentionally stopped before Send. No automatic retry exists.

## LIVE PILOT READY

Live mode was not enabled by this validation. After reviewing the exact text,
the user may manually set and restart with:

```text
HH_WRITE_ENABLED=true
HH_DRY_RUN=false
HH_MAX_WRITES_PER_RUN=1
HH_MAX_WRITES_PER_DAY=5
```

After that restart, validate only the capability state and fresh targeted
preflight. Do not press Send until the user explicitly chooses to do so.

## Performance regression

```text
Dashboard / Inbox / Conversation local UX: PASS
Targeted display refresh: PASS
Fresh targeted preflight: PASS
Full Sync Conversations required: NO
Full Sync used: NO
Send → reconciliation regression: NOT RUN (user Send required)
```

## Verification

```text
gofmt -w .                 PASS
go test ./...              PASS
go test -race ./...        PASS
go vet ./...               PASS
go build ./...             PASS
git diff --check           PASS
node --check web/app.js    PASS
./start.sh web smoke       PASS
```

The startup wrappers now rely on the application's dotenv parser instead of
shell-sourcing `.env`; this preserves restart behavior for comma-separated
values such as keyword lists, forwards CLI arguments, and avoids executing
dotenv values as shell code.
