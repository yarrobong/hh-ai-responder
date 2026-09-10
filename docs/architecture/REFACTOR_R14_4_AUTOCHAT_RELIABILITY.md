# Executive summary

R14.4: COMPLETE

Primary safety improvement: legacy AUTO chat now has a durable logical action
keyed by the HH conversation and the exact provider employer-message ID. A
blocking record is read before reply AI and is atomically reserved again
immediately before one HH mutation. `SENDING`, accepted and ambiguous outcomes
survive restart and block automatic replay. HH provider exactly-once is not
claimed.

# Before

The legacy path reconstructed an awaiting chat on every 15-minute iteration,
generated a fresh UUID for each send, and relied on an in-memory chat-only
ignore list. If HH history lagged after a send, the same employer trigger could
reach AI and HH POST again.

# Trigger identity

Conversation: the HH conversation ID, represented by the existing numeric chat
ID and persisted as `conversation_id`.

Employer trigger: the latest awaiting employer message, identified by HH's
numeric provider message ID and persisted as `trigger_message_id`.

Provider message ID availability: the read adapter already exposes
`rawMessage.ID`; the legacy source now carries it through list/history
normalization. Missing or non-positive IDs are represented as unavailable.

Text fallback: NONE. Live AUTO writing fails closed when the exact trigger ID
is unavailable. Text, generated reply, and timestamp are never durable write
identity.

# Attempt model

AttemptID: local durable correlation ID generated once at reservation time.

Provider request key: for REPLY, one generated key is persisted in
`request_key` before dispatch and reused for the single dispatch. It is passed
to HH as the existing `idempotencyKey` field. LEAVE has no provider request-key
field in the current HH API.

States: `SENDING`, `ACCEPTED`, `REJECTED`, `NOT_SENT`,
`DELIVERY_UNCERTAIN`, `TARGET_REPLY_CONFIRMED`, and
`TARGET_LEAVE_CONFIRMED`. Blocking states are `SENDING`, `ACCEPTED`,
`DELIVERY_UNCERTAIN`, and either target-confirmed state. `REJECTED` and
`NOT_SENT` are replayable only as a later separately identified attempt; there
is no same-run retry.

Conflict key: `conversation_id + trigger_message_id`, intentionally without
action type. A racing REPLY and LEAVE cannot both mutate the same trigger.

# Blocking policy

The early gate runs only for live AUTO mode. It treats a durable store error,
store initialization error, missing trigger ID, or existing blocking record as
a no-write condition. A blocking record is checked before `autochatreply` AI.
The final store `Reserve` is the concurrency authority; the early read is only
an optimization.

# Sequence

```text
15m scheduler
  -> awaiting chats
  -> current history and exact employer message ID
  -> early durable blocking gate (before AI)
  -> existing auto-chat policy / AI proposal
  -> final atomic durable Reserve(SENDING)
  -> one existing HH SendChatMessage or LeaveChat
  -> RecordOutcome
```

Early gate

Implemented: YES. Same conversation + trigger in a blocking state produces
AI = 0 and HH mutation = 0. A new provider message ID is an independent key.

Final reservation

Implemented: YES. JSON uses the private process/file lock and reloads the file
inside the lock. PostgreSQL uses a partial unique index over blocking states
and `INSERT ... ON CONFLICT DO NOTHING`.

Send outcome

Accepted maps to `ACCEPTED` and is not resent. The persisted request key is
not regenerated.

Persistence uncertainty

If `RecordOutcome` fails after the writer returns, the original `SENDING`
reservation remains conservatively blocking. The caller receives uncertainty;
there is no retry.

Crash / restart

`SENDING` is blocking. A process crash before or after transport therefore
does not release the trigger and the next scheduler iteration performs no AI
or HH mutation.

Reconciliation

Implemented: NO. The current legacy read model and leave operation do not
provide enough evidence to safely infer automatic delivery for an ambiguous
attempt without adding a broader reconciliation workflow. Uncertain states
remain blocking. No polling scheduler, TTL release, or automatic reset was
added.

Limitations:

- An exact outgoing provider ID is stored when HH returns one, but no new
  legacy reconciliation operation was introduced in this stage.
- A manually sent message must not be attributed to the automatic attempt.
- Absence of a message in a later read is not treated as proof of failed send.

New employer message

M2 with a distinct HH provider message ID does not match M1's durable record
and may enter normal policy/AI flow.

Review / auto / dry-run modes

Only live AUTO mode reserves. REVIEW can still generate the existing proposal
and creates no live attempt. Dry-run creates no live attempt and no HH write.
`HH_WRITE_ENABLED=false` creates no live attempt and no HH write.

Chat leave

LEAVE uses the same durable trigger reservation and conflict key. Accepted
leave is blocking; ambiguous leave becomes `DELIVERY_UNCERTAIN`; rejected or
definitively not-sent leave is recorded without an immediate retry. Current HH
leave has no documented idempotency key or exact delivery evidence.

Resume touch

Classification: BEST-EFFORT INTENTIONAL

Reason: touching the same resume is semantically repeatable and has low
duplicate impact. No durable attempt machinery was added.

Job-search status

Classification: BEST-EFFORT INTENTIONAL

Reason: setting the same `looking_for_offers` status every 24 hours is
semantically repeatable and has low duplicate impact. No durable attempt
machinery was added.

Storage

JSON: `autochat_attempts.json`, version 1, private atomic writes and a
cross-process lock. Corruption/read failure is returned, not treated as an
empty store; live AUTO fails closed.

Postgres: `legacy_auto_chat_attempts` and the narrow embedded migration
`000008_legacy_auto_chat_attempts`. The partial unique index provides parity
for concurrent blocking reservations. PostgreSQL mode does not fall back to
JSON. Live PostgreSQL integration was not run because `DATABASE_URL` was not
configured.

Concurrent workers

Two workers may both pass the early read, but only one final reservation can
win. Focused tests verify one provider write at most; race verification covers
the store and orchestration.

Controlled chat compatibility

UNCHANGED. `ApprovedHHAction`, `SendNonce`, controlled `ActionStore`, and exact
ID reconciliation were not modified.

Application compatibility

UNCHANGED. `applicationattempt`, application reconciliation, application
gating, and per-run application dispatch behavior were not modified.

Provider semantics

Provider idempotency: NOT ESTABLISHED. HH accepts an `idempotencyKey` request
field in the current adapter, but this repository has no HH contract proving
server-side idempotency semantics for it.

Exactly-once: NO. The local durable trigger gate prevents automatic replay for
the same trigger, but provider exactly-once and delivery exactly-once are not
proven.

Retry safety

HH WRITE RETRY: NONE

The 15-minute cadence remains unchanged and there is no retry timer, resend
button, automatic reset, or retry queue. The existing legacy compatibility
writer still does not receive configured `HHMaxWritesPerRun`/
`HHMaxWritesPerDay` values; that pre-existing direct-write limit discrepancy
is reported here and was not broadly redesigned in R14.4. Durable trigger
protection does not add sends.

# Risk after R14.4

Automatic application: strong durable protection (unchanged from R14.3).

Controlled chat: strong durable protection (unchanged).

Legacy auto-chat: HIGH -> LOW / substantially reduced through durable
trigger/action reservation; provider exactly-once remains unproven.

Leave: MEDIUM -> reduced through the same durable trigger protection; exact
provider leave delivery remains unproven.

Touch: LOW, intentional best-effort.

Status: LOW, intentional best-effort.

# Verification

Focused auto-chat domain, JSON store, orchestration, runtime, controlled-chat
and application regression tests passed. The focused suite includes blocking
before AI, new trigger, ambiguous send, crash residue, outcome persistence
failure, reserve failure, dry-run, review, write-disabled, missing ID, leave,
restart, corruption, and concurrent reservation cases.

gofmt: PASS

gofmt -l: PASS

go test -count=1 ./...: PASS

race: PASS

vet: PASS

build: PASS

canonical build: PASS

diff: PASS

node: PASS

Docker: NOT APPLICABLE

Postgres live integration: NOT RUN (`DATABASE_URL` not configured)

LIVE HH WRITES: 0

# R14 status

R14.4: COMPLETE

# Ready

R14.5 — Reliability Observability / Operator Resolution Audit: READY

Do not begin R14.5 in this stage.
