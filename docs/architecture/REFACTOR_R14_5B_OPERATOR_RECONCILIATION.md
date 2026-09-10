# Executive summary

R14.5b: COMPLETE

Operator improvement: operators can explicitly check a durable controlled-chat
action, application attempt, or legacy auto-chat attempt against bounded HH
read evidence. The command persists only local evidence/confirmation state.
It never retries, resends, creates an attempt, sends a chat message, leaves a
conversation, consumes a send nonce, or releases a blocking record.

# Capability model

HH reads: targeted controlled-chat history, fresh application preflight and
bounded application evidence, and targeted auto-chat conversation history.

HH writes: NONE. The reconciliation use cases receive narrow read and local
evidence-writer interfaces. They do not receive an HH write gateway, runtime
requester, vacancy response writer, chat writer, leave writer, resume writer,
job-search writer, or generic mutation requester.

AI: NONE.

# Controlled chat reconciliation

Existing owner: `internal/usecase/hhwritereconcile`, called through the
existing `HHWriteGateway.ReconcileDelivery` boundary and the operator adapter
in `internal/runtime/operator_controlled_reconciliation.go`.

Operator exposure: the existing `POST /api/actions/{id}/reconcile` route. No
second matching algorithm was introduced. The operator adapter persists only
the existing local action transition.

Evidence: targeted conversation history, exact conversation identity, exact
persisted provider message ID, and the existing allowed action-state rules.
Invalid states return the existing not-applicable behavior.

# Application reconciliation

Existing owner: `internal/usecase/applicationreconciliation.Service` from
R14.2. The dashboard and CLI construct this same service; they do not copy its
precedence, fresh-read, conflict, or target-confirmation logic.

Eligible states: `SENDING`, `DELIVERY_UNCERTAIN`, `ACCEPTED`, and the existing
`TARGET_RESPONSE_CONFIRMED` idempotent representation.

Evidence: one fresh vacancy preflight plus bounded vacancy-wide application
evidence, using the existing R14.2 precedence and conflict rules. Exact local
attempt causality is not claimed where HH exposes only vacancy-wide evidence.

Absence behavior: missing negotiation evidence, an applicable fresh page, a
failed provider read, and partial evidence never become `NOT_SENT`,
`REJECTED`, or replayable. The previous blocking state remains blocking.

# Auto-chat reconciliation

Implemented: PARTIAL. Reply reconciliation is implemented. Leave
reconciliation intentionally returns `UNSUPPORTED`: the inspected HH read
contract has no strong explicit leave/inactive/membership evidence. Conversation
absence or continued visibility is not treated as leave evidence.

Reply evidence is anchored to the exact persisted `ConversationID` and
`TriggerMessageID`. The provider history contract carries stable message ID,
sender, direction, and timestamp. Ordering is established only from provider
timestamps; slice order is not trusted. One bounded targeted history read is
performed.

Text fallback: NONE. No generated-text, substring, normalized-text, hash,
semantic, or LLM matching is used.

# Auto-chat causality

Exact outgoing evidence: when `ProviderOutgoingMessageID` exists, the exact
ID must occur after the exact employer trigger and before any later employer
trigger. A different candidate ID is `CONFLICTING`; it does not fall back to
weaker evidence and does not replace the persisted identity.

Target response evidence: when no exact outgoing ID is persisted, a candidate
message ordered after the exact trigger and before a later employer trigger
confirms the target response. Multiple candidate messages are allowed.

Attempt causality: target-response evidence is reported as “target reply
confirmed”; it does not claim that the automatic AttemptID sent that response.
This preserves the manual-message interference distinction.

# Operator API

Routes:

- `POST /api/reliability/application-attempts/{id}/reconcile`
- `POST /api/reliability/autochat-attempts/{id}/reconcile`
- existing `POST /api/actions/{id}/reconcile` for controlled chat

Methods: application and auto-chat routes dispatch to their workflow-specific
services. Controlled chat dispatches to the existing delivery reconciliation
algorithm. GET on the new reconcile routes is rejected with `405`; GET read
models remain inspection-only.

Security: the existing dashboard protections remain in force: loopback/local
host, same-host Origin, cross-site `Sec-Fetch-Site` rejection,
`X-Career-Agent: local`, and JSON content type for POST. POST means a local
operator command that performs HH reads and local evidence persistence; it does
not mean an HH provider write and does not require a send nonce.

# CLI

Commands:

- `hh reliability applications reconcile <attempt-id> [--json]`
- `hh reliability autochat reconcile <attempt-id> [--json]`

Controlled chat has no duplicate CLI command; its existing operator/dashboard
reconciliation boundary is reused.

Exit semantics: confirmed, insufficient, and idempotent/no-op results are
successful command execution with typed output. Invalid state, unsupported
leave, provider read failure, and persistence failure are reported as typed
non-zero errors according to the command path. Output includes attempt ID,
previous/new state, result, evidence kind/source, provider correlation IDs,
observed time, reason, and causality limitation. Request keys and raw HH
responses are not emitted.

# State transitions

Application: existing R14.2 transitions positive fresh evidence from an
eligible blocking state to `TARGET_RESPONSE_CONFIRMED`. Insufficient or
unavailable evidence does not release or downgrade the attempt. Repeated
confirmation is idempotent.

Auto-chat: reply evidence may transition `SENDING`, `ACCEPTED`, or
`DELIVERY_UNCERTAIN` to `TARGET_REPLY_CONFIRMED`; an already-confirmed reply
does not regress. Leave states do not transition because leave evidence is
unsupported.

Controlled: existing `hhwritereconcile` transitions only the established
eligible delivery states. No new state was added.

# Persistence

JSON: `autochat_attempts.json` stores reconciliation kind, source, provider
message ID, observed time, and causality note through the narrow
`ReconciliationWriter` interface. Application and controlled persistence use
their existing stores.

PostgreSQL: the same five auto-chat evidence fields are persisted and scanned
with the JSON equivalent.

Migration: a narrow additive `internal/runtime/migrations/000009` migration
was required for durable auto-chat evidence parity. Existing migrations were
not edited. Inspection-only builders do not apply migrations.

# Absence / unavailable evidence

Absence is not non-delivery. Application absence remains blocking. Auto-chat
absence remains `SENDING`, `ACCEPTED`, or `DELIVERY_UNCERTAIN` as applicable and
is reported as `INSUFFICIENT`. Provider read failure leaves local state
unchanged and reports `UNAVAILABLE`. Unsupported leave reports
`UNSUPPORTED` and leaves the attempt blocking.

# Persistence failure

Positive evidence followed by local persistence failure returns
`PERSISTENCE_ERROR`. No HH mutation occurs and the old blocking state remains;
the result is not converted into `NOT_SENT`, released, or replayable.

# Concurrent reconciliation

Provider reads may happen concurrently, while local dashboard dispatch remains
serialized by the existing dashboard lock. JSON uses the existing private
file lock and PostgreSQL uses a transaction. Domain transitions reject invalid
downgrades and exact provider identity replacement, so repeated or concurrent
reconciliation cannot regress confirmed state.

# No-write proof

Application reconciliation HH writes: 0

Auto-chat reconciliation HH writes: 0

Controlled reconciliation HH writes: 0

The reconciliation constructors expose only read clients and local evidence
writers. No raw HH mutation call was added; raw mutation ownership remains
`internal/adapters/hh/write`.

# Unsafe actions

Retry: NOT IMPLEMENTED

Resend: NOT IMPLEMENTED

Mark not sent: NOT IMPLEMENTED

Clear/reset: NOT IMPLEMENTED

# Regression safety

R14.1: durable application attempts and blocking/replay rules remain in the
existing application-attempt store and policy.

R14.2: operator application reconciliation calls the same
`applicationreconciliation.Service` and preserves evidence precedence,
fresh reads, conflicts, and target confirmation.

R14.3: automatic bounded reconciliation and cross-run gating are unchanged.

R14.4: exact auto-chat trigger identity, stable request key, reservation-before-
mutation, replay blocking, and no-text-fallback behavior are unchanged.

R14.5a: existing reliability list/detail read models remain the single UI
read model and refresh naturally after successful local persistence.

# Tests

Focused tests cover `autochatreconciliation`, application reconciliation,
`hhwritereconcile`, auto-chat attempts, HH read mapping, runtime reliability
routes, CLI parsing, and dashboard request-key masking. They include exact
outgoing ID, wrong outgoing ID, target-only/manual response, before-trigger,
later-trigger, absence, read failure, persistence failure, idempotence,
unsupported leave, GET-vs-POST routing, and response secret-boundary cases.

Full verification was run after the implementation and final safety fixes.

# Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

canonical build: PASS (`go build ./cmd/hh-ai-responder`)

diff: PASS

node: PASS (`node --check web/app.js`)

Docker: NOT APPLICABLE

Postgres live integration: NOT RUN — `DATABASE_URL` absent

LIVE HH WRITES: 0

# R14 status

R14.5b: COMPLETE

# Ready

R14.5c — Local Review/Audit Annotations is not started. The current durable
evidence, typed operator results, and existing inspection read models provide
the needed operational trace for this stage. Recommend skipping directly to
R14.5d — Reliability Notifications / Operator UX, or to R14.Final if
notifications and observability are already sufficient.
