# Executive summary

R14.5a: COMPLETE

Operator improvement: durable automatic application and legacy auto-chat
attempts are now visible through bounded, read-only read models. The operator
can see the exact local AttemptID, target identity, technical state, clear
Russian-facing meaning, provider identifiers/evidence when persisted, and
timestamps. Store corruption/unavailability is reported as an error, never as
an empty list.

No retry, resend, reset, reconciliation action, notification, scheduler, TTL,
mutation route, or persistence migration was added.

# Before

Application visibility: durable records existed and blocked unsafe replay, but
the dashboard had no attempt listing/detail and the CLI had no durable attempt
inspection command.

Auto-chat visibility: durable trigger attempts existed and preserved M1/M2
identity, but they were not available in an operator read model.

# Read model architecture

`internal/usecase/reliabilityinspection` owns two workflow-specific models:
`ApplicationAttemptReadModel` and `AutoChatAttemptReadModel`. It owns limit and
filter validation, state classification, human labels, and safe presentation
mapping.

The use case depends only on separate read ports in
`internal/ports/applicationattempt` and `internal/ports/autochatattempt`.
Dashboard and CLI composition inject readers, not stores with write methods.
No HH or AI capability is reachable from the inspection package.

# Application attempt model

Fields: `attempt_id`, `vacancy_id`, `resume_id`, `state`, `classification`,
`display_label`, `reason`, `created_at`, `updated_at`, provider status/error,
provider application/negotiation IDs, evidence kind/source/strength,
`observed_at`, provider response time, and a causality limitation note when a
target response is confirmed.

Classification: `SENDING` and `DELIVERY_UNCERTAIN` are `UNRESOLVED`;
`ACCEPTED` is `ACCEPTED`; `REJECTED` is `REPLAYABLE`; `NOT_SENT` is
`NOT_SENT`; `TARGET_RESPONSE_CONFIRMED` is `CONFIRMED`. Unknown future values
map to `OTHER` without changing domain state.

Labels include “Ожидает проверки: исход отправки неизвестен”, “Возможно
отправлено; повторная отправка запрещена”, and “Не отправлено: запрос к HH не
выполнялся”.

# Auto-chat attempt model

Fields: `attempt_id`, `conversation_id`, exact `trigger_message_id`,
`action_type`, `state`, `classification`, `display_label`, timestamps,
masked provider request key, outgoing provider message ID, provider status,
error class, and causality limitation note.

Classification: the corresponding `SENDING`, `ACCEPTED`, `REJECTED`,
`NOT_SENT`, `DELIVERY_UNCERTAIN`, `TARGET_REPLY_CONFIRMED`, and
`TARGET_LEAVE_CONFIRMED` states map to the same presentation categories as
their workflow semantics. Distinct trigger IDs remain distinct records.

# Store query capabilities

Application: bounded `List(ReadQuery)` with optional state and vacancy filters,
plus `GetByID`.

Auto-chat: bounded `List(ReadQuery)` with optional state, conversation, and
action filters, plus `GetByID`.

Both JSON and PostgreSQL adapters use the same logical filters and deterministic
ordering: `UpdatedAt DESC`, then `AttemptID ASC`.

# Bounds / pagination

The default limit is 20 and the maximum is 100. The CLI defaults to
needs-attention records and supports bounded recent history with `--all` or
`--recent`; these do not mean unbounded history. Dashboard query parameters
accept `limit`, `state`, `needs_attention`, and workflow-specific identity
filters. Cursor pagination was not needed for this bounded stage.

# JSON implementation

JSON reads reload and validate the file under the existing private-file locking
convention. Corrupt JSON, invalid records, duplicate identities, or read
errors are returned. Reads do not write attempt records and do not treat a
missing file as corruption; a missing file is a valid empty store.

# PostgreSQL implementation

PostgreSQL uses parameterized bounded `SELECT` queries with the same filters,
state semantics, ordering, and limit cap. No migration or index was added for
R14.5a. The inspection builders deliberately do not apply migrations.

# Dashboard

Routes:

- `GET /api/reliability/application-attempts`
- `GET /api/reliability/application-attempts/{id}`
- `GET /api/reliability/autochat-attempts`
- `GET /api/reliability/autochat-attempts/{id}`

Views: a compact `/reliability` page shows separate application and legacy
auto-chat “Требуют внимания” tables, with links to read-only detail pages.
Attempt state is displayed separately from any HH application/conversation
status. Request keys are masked and full message history/prompts are omitted.

Read-only guarantees: the route dispatcher registers only GET for these paths;
POST receives 405. Ordinary listing uses durable local state only and does not
call HH, AI, reconciliation, or attempt mutation methods.

# CLI

Commands:

- `hh reliability applications`
- `hh reliability autochat`

R14.5b adds explicit operator commands:

- `hh reliability applications reconcile <attempt-id> [--json]`
- `hh reliability autochat reconcile <attempt-id> [--json]`

These commands perform one bounded HH read and may persist local evidence;
they have no HH writer capability and do not consume a send nonce.

Filters: `--limit N`, `--state STATE`, `--vacancy-id`, `--conversation-id`,
`--action-type REPLY|LEAVE`, `--json`, and bounded-history `--all` / `--recent`.
The default is needs-attention (`SENDING`, `DELIVERY_UNCERTAIN`, and
`ACCEPTED`). JSON includes exact technical state names and human labels.

Exit behavior: successful reads, including empty lists, return 0. Invalid
filters and store failures return errors through the existing non-zero command
handler path. No mutation command was introduced.

# Store failure visibility

Dashboard store failures return HTTP 503 with a safe availability error;
corrupt JSON is not rendered as `items: []`. Not-found detail lookups return
404. Successful responses include `store.status=AVAILABLE` and the backend
name without connection details. CLI store failures are non-zero.

# Security / secret exposure

The existing loopback and response security headers remain in force. IDs are
used as values through repository parameters; no SQL is interpolated. Cookies,
headers, tokens, environment secrets, prompts, and complete message history are
not projected. Auto-chat request keys are masked as correlation metadata.

# State vocabulary

Technical domain states remain unchanged and are emitted in `state`. The
additional `classification` and `display_label` fields are presentation-only
and are never persisted back to attempt stores. `HH application status`,
automatic attempt state, transport outcome, and reconciliation evidence remain
separate concepts in the API.

# Causality limitations

Application: `TARGET_RESPONSE_CONFIRMED` means a provider response for the
vacancy is confirmed by evidence already persisted. The detail view explicitly
does not claim that this exact AttemptID caused the response.

Auto-chat: target-reply/leave labels confirm the target action vocabulary only.
The UI does not claim that a legacy attempt definitely caused it without exact
outgoing identity evidence. M1 and M2 retain separate TriggerMessageID values.

# No-write proof

HH read: NONE required for ordinary inspection.

HH write: NONE

AI: NONE

Attempt mutations: NONE

The implementation audit found no HH adapter or LLM import in the inspection
use case or its read-only dashboard/CLI handlers. The only `Reserve` calls in
new tests are fixture setup calls, not inspection behavior.

# Regression safety

R14.1: durable application reservation and outcome behavior unchanged; focused
application-attempt tests pass.

R14.2: reconciliation service and evidence transitions unchanged; focused
reconciliation tests pass.

R14.3: blocking policy, early gate, and dispatch behavior unchanged; focused
application policy/runtime tests pass.

R14.4: exact auto-chat trigger identity, durable reservation, stable request
key, same-trigger blocking, new-trigger allowance, and no-retry behavior
unchanged; focused auto-chat tests pass.

# Tests

Added/read-model coverage verifies:

- every actual application and auto-chat state classification;
- bounded ordering, filters, and limits;
- restart visibility;
- corrupt JSON/store error visibility;
- M1/M2 trigger preservation;
- provider ID/evidence projection and request-key masking;
- dashboard GET-only API, invalid filters, store failure, and empty success;
- CLI bounded JSON, empty success, and store failure behavior.

Focused reliability, scheduler, controlled-chat/runtime, JSON, PostgreSQL
adapter, and dashboard tests pass.

# Verification

gofmt: PASS

go test: PASS (`go test -count=1 ./...`)

race: PASS (`go test -race ./...`)

vet: PASS

build: PASS (`go build ./...`)

canonical build: PASS (`go build ./cmd/hh-ai-responder`)

diff: PASS (`git diff --check`)

node: PASS (`node --check web/app.js`; embedded dashboard asset also checked)

Docker: NOT APPLICABLE

Postgres live integration: NOT RUN (`DATABASE_URL` was not configured)

LIVE HH WRITES: 0

# R14 status

R14.5a: COMPLETE

# Ready

R14.5b — Read-only Operator Reconciliation
READY

Do NOT begin it.
