# Executive summary

R14: **COMPLETE**

Reason: the final audit found no R14 blocker. Automatic application and legacy
auto-chat writes reserve a durable local safety record before a possible HH
mutation; unresolved records remain blocking across restart and scheduler
runs. Controlled chat retains its R11 action/nonce reservation boundary.
Reconciliation is read-only with positive-only evidence semantics, inspection
and notifications are non-authoritative, and no automatic HH write retry or
operator retry/reset bypass exists.

This was an audit-only stage. No production code was changed. Existing
worktree changes were preserved. LIVE HH WRITES = 0.

Pre-audit recorded 61 Go packages and the canonical executable
`cmd/hh-ai-responder`. Supported persistence modes are JSON and PostgreSQL.
JSON reliability files are `application_attempts.json`,
`autochat_attempts.json`, and `notification_events.json`; controlled chat
uses `hh_write_actions.json` and `hh_write_events.json`. Embedded PostgreSQL
migrations currently end at `000009_auto_chat_reconciliation`.

# Stage status

| Stage | Status |
|---|---|
| R14.0 | COMPLETE |
| R14.1 | COMPLETE |
| R14.2 | COMPLETE |
| R14.3 | COMPLETE |
| R14.4 | COMPLETE |
| R14.5 | AUDIT COMPLETE |
| R14.5a | COMPLETE |
| R14.5b | COMPLETE |
| R14.5c | SKIPPED BY DESIGN |
| R14.5d | COMPLETE |
| R14.Final | COMPLETE |

# Reliability goals

The closure criteria pass:

- possible automatic application dispatch is preceded by fresh vacancy/test
  proof and durable reservation;
- possible auto-chat dispatch is preceded by exact trigger identity and
  durable reservation;
- the same unresolved application, action, or auto-chat trigger cannot be
  automatically dispatched again after restart or a later scheduler run;
- ambiguous provider dispatch consumes the per-run application dispatch
  budget;
- absence of provider evidence never downgrades a possibly-dispatched record
  to `NOT_SENT`;
- reconciliation, inspection, and notification code have no HH write
  capability;
- notification lifecycle operations are advisory only;
- JSON and PostgreSQL implement the same safety states and conflict keys;
- R10/R11/R12/R13 boundaries remain intact.

# Mutation inventory

| Stage | New authority/capability | Safety purpose | Writes HH? |
|---|---|---|---:|
| R14.1 | Durable automatic application attempt state machine and atomic reservation | Prevent cross-run replay and retain uncertainty | No; wraps the existing executor |
| R14.2 | Positive-only application reconciliation | Confirm target response without claiming local causality | No |
| R14.3 | Early application attempt gate | Stop blocked vacancies before vacancy AI, cover-letter AI, test AI, or mutation | No |
| R14.4 | Durable legacy auto-chat attempt state, exact trigger identity, stable reply request key | Prevent same-trigger replay and concurrent duplicate mutation | No; uses the existing write boundary |
| R14.5a | Bounded application/auto-chat inspection read models | Make safety state inspectable without mutation | No |
| R14.5b | Operator reconciliation for application and auto-chat reply | Persist fresh provider evidence locally | No; HH reads only |
| R14.5d | Advisory reliability notification projector | Surface blocking incidents without authority over them | No; local NotificationStore only |

# Final side-effect matrix

| Mutation | Durable identity | Reservation before dispatch | Cross-run replay block | Reconciliation | Operator view | Notifications |
|---|---|---|---|---|---|---|
| Controlled chat | ActionID, nonce, destination, approved text | Yes, R11 action store | Yes | Exact provider-ID/read-only reconciliation | Clear | Advisory |
| Automatic application | AttemptID, vacancy, resume, CreatedAt | Yes, atomic attempt store | Yes | Positive application evidence | Clear | Advisory |
| Legacy auto-chat reply | AttemptID, conversation, exact trigger, action, CreatedAt, stable RequestKey | Yes, atomic trigger reservation | Yes | Exact outgoing ID or target reply evidence | Clear | Advisory |
| Legacy auto-chat leave | AttemptID, conversation, exact trigger, action, CreatedAt | Yes, atomic trigger reservation | Yes | Positive leave confirmation unsupported | Partial | Advisory |
| Resume touch | Scheduler operation | No attempt state | Semantically repeatable | Not required | Existing operational logs | No reliability authority |
| Job-search status | Scheduler operation | No attempt state | Semantically repeatable | Not required | Existing operational logs | No reliability authority |

The four safety-sensitive automatic/controlled categories have durable replay
protection. Resume touch and job-search status are intentionally best-effort
maintenance operations.

# Controlled chat

Identity: `ActionID` plus a persisted send nonce and approved destination/text.

Reservation: draft and approval are preparation only. Sending requires the
approved action, nonce, fresh preflight, and a durable sending reservation
before the single writer call. An audit-persistence failure after reservation
does not release the action.

Trace: draft -> approval -> ActionID/nonce -> fresh preflight -> sending
reservation -> one HH mutation -> durable outcome -> exact-ID read-only
reconciliation -> operator view -> advisory notification.

Ambiguity: accepted, failed-after-transport, manual-review, and delivery
uncertainty are not sendable again. Missing provider ID prevents delivery from
being claimed. Absence of provider evidence is not proof of non-delivery.

Reconciliation: `internal/usecase/hhwritereconcile` is read-only and uses the
exact destination/provider identity. It may confirm delivery but cannot send,
create a new action, consume a nonce, or release a blocked action.

Operator visibility: the dashboard exposes inspection and “Run read-only
reconciliation”. Notification dismissal does not release the action. The
separate “Approve again” draft flow creates a new explicitly approved action;
it is not a retry/reset of an unresolved action.

# Automatic applications

Identity: immutable `AttemptID`, vacancy ID, recorded resume ID, and CreatedAt.
The local AttemptID is correlation only; it is not asserted to be a provider
idempotency key.

Reservation: the automatic path is:

`ApplyVacancies` -> early attempt gate -> normal preparation -> fresh vacancy
applicability/preflight -> fresh test metadata proof when applicable ->
`applicationattempt.Executor.Reserve` -> existing application executor ->
R11 HH write gateway -> `internal/adapters/hh/write`.

The reservation is immediately before the possible response dispatch and
cannot be bypassed by the normal automatic executor. Reservation failure means
provider dispatch = 0.

Early gate: a blocking application attempt is checked before vacancy
description/analysis and before cover-letter or test-answer AI. For a blocking
target, vacancy-analysis AI = 0, cover-letter AI = 0, test-answer AI = 0, and
HH mutation = 0. R14.1 reservation remains necessary for TOCTOU and concurrent
races.

Dispatch budget: `maxApplicationsPerRun` counts `TransportTried`, not only
successful `Applied` results. Accepted, rejected after dispatch, 409, 429,
5xx, network ambiguity, and malformed success responses after transport all
consume budget. Reservation/preflight failure, existing blocking attempts,
dry-run, write-disabled, and conclusive pre-dispatch `NOT_SENT` do not. Thus N
ambiguous dispatches prevent dispatch N+1.

Ambiguity: states are `SENDING`, `ACCEPTED`, `REJECTED`, `NOT_SENT`,
`DELIVERY_UNCERTAIN`, and `TARGET_RESPONSE_CONFIRMED`. `SENDING`, `ACCEPTED`,
`DELIVERY_UNCERTAIN`, and `TARGET_RESPONSE_CONFIRMED` block. Only `REJECTED`
and conclusive pre-dispatch `NOT_SENT` are replayable, and only as a later new
logical attempt. There is no TTL release and no unresolved-to-`NOT_SENT`
transition based on evidence absence.

If final outcome persistence fails after executor invocation, the durable
`SENDING` residue remains, the caller receives persistence uncertainty, and the
next automatic run dispatches zero times.

Reconciliation: owner is `internal/usecase/applicationreconciliation`.
Fresh preflight and bounded application reads provide evidence. Positive
target evidence may produce `TARGET_RESPONSE_CONFIRMED`; absence, unavailable
reads, and conflicts remain unresolved. Reconciliation does not claim that the
exact AttemptID caused the provider response and has no HH writer capability.

Operator visibility: bounded inspection exposes AttemptID, vacancy/resume
identity, state/classification, timestamps, provider IDs/evidence, and the
causality limitation. Operator reconciliation is a POST local-evidence
operation backed by HH reads, never an application retry.

# Legacy auto-chat

Trigger identity: live durable auto-chat writes require the exact pair
`ConversationID + TriggerMessageID`. There is no text hash, normalized text,
semantic match, LLM identity, or timestamp-only fallback for write
authorization. M1 and a later employer M2 have independent trigger identities;
an unresolved M1 does not block M2.

Reservation: the 15-minute scheduler reads history, verifies the exact
trigger, applies the early durable gate, performs reply AI only when allowed,
then atomically reserves immediately before the mutation. Leave follows the
same exact-trigger reservation boundary without an AI reply step.

Request key: a reply reservation persists one stable request key derived from
the durable attempt. Restart reuses the stored key for correlation; it does
not generate a new key. The key does not authorize a resend. Persistence
uncertainty remains blocking.

Replay safety: same M1 on a later scheduler run yields AI = 0 and reply/leave
send = 0. Two workers racing the same conversation/trigger have at most one
external mutation attempt because JSON locking and the PostgreSQL partial
unique index serialize the reservation.

States: `SENDING`, `ACCEPTED`, `REJECTED`, `NOT_SENT`,
`DELIVERY_UNCERTAIN`, `TARGET_REPLY_CONFIRMED`, and
`TARGET_LEAVE_CONFIRMED`. The same blocking/replayable distinction is
authoritative for both actions.

Reply reconciliation: exact conversation history is examined using the exact
trigger, message order/timestamps, and exact outgoing provider ID when
available. A candidate's manual reply after M1 can confirm a target response,
but the result explicitly does not claim that the automatic attempt delivered
it without exact outgoing evidence. No text matching is used.

Leave reconciliation: currently `UNSUPPORTED` unless a strong provider
contract is available. Unsupported reconciliation does not release the leave
attempt. Durable replay protection is therefore YES; positive provider leave
confirmation is UNSUPPORTED. This is an evidence limitation, not a duplicate
replay vulnerability.

# Maintenance writes

Resume touch: **INTENTIONAL BEST-EFFORT**. It is low-consequence and
semantically repeatable, runs on the normal four-hour completion-based cadence,
and has no immediate automatic retry loop. An attempt state machine would add
complexity without material safety benefit.

Job status: **INTENTIONAL BEST-EFFORT / semantically repeatable**. It runs on
the normal 24-hour completion-based cadence and has no immediate retry loop.

# Crash / restart guarantees

| Crash point | Result |
|---|---|
| Before application reservation | No provider dispatch; later iteration may try a fresh logical attempt after normal gates |
| After `SENDING` reservation | Record remains blocking; automatic restart cannot dispatch it again |
| While transport is in flight | Record remains `SENDING` or becomes uncertainty; no automatic resend |
| After provider effect, before outcome save | Durable reservation remains blocking; provider effect is treated as possibly real |
| After outcome save | State is durable and replay policy is applied; blocking states remain blocking |

The same guarantee applies to auto-chat attempts and controlled actions. For
controlled chat, failure after transport and before local projection produces
an unresolved durable action requiring reconciliation. No event or notification
failure changes the authority state.

# Concurrency guarantees

Application conflicts are vacancy-wide and deliberately include all blocking
states, including positive target confirmation. Auto-chat conflicts are
`conversation_id + trigger_message_id` and deliberately exclude action type,
so REPLY and LEAVE cannot race into two mutations for one employer trigger.

JSON uses a private file lock, reload-under-lock, validation, and atomic
replacement. PostgreSQL uses transaction boundaries, row locking for outcome
and reconciliation updates, and unique partial indexes for active conflicts.
Both backends provide one reservation winner.

# Provider evidence model

Can prove target response exists: **YES**, from fresh `AlreadyResponded`,
fresh negotiation/application evidence, stable provider IDs, and available
resume identity/timestamps where the provider exposes them.

Can prove exact local AttemptID caused it: **NO**. AttemptID is local
correlation and is not reconstructed as provider causality.

Can prove post-dispatch non-delivery: **NO**. Missing negotiation/history or an
unavailable read is unresolved, not `NOT_SENT`.

Conflicting evidence is fail-closed. Provider exactly-once is not established.

# Absence of evidence

The final search found no post-dispatch equivalent of “not found means not
sent”, “empty history means failed”, “no negotiation means retry”, or “timeout
releases the attempt”. Provider read absence, read unavailability, persistence
failure, and notification failure all preserve the blocking safety residue.

# Exactly-once semantics

Provider exactly-once: **NOT ESTABLISHED**.

Local replay prevention: durable reservations, immutable local identities,
blocking state predicates, exact auto-chat trigger identity, stable reply
request-key correlation, atomic JSON/SQL conflict enforcement, and no
automatic write retry.

The implementation claims local replay prevention and bounded positive
reconciliation, not guaranteed provider delivery or provider idempotency.

# Operator model

Inspection: `hh reliability applications` and `hh reliability autochat` are
bounded, read-only local inspection commands. Dashboard GET reliability routes
are side-effect free. Default/max list bounds are 20/100 with deterministic
ordering; there is no unbounded `ListAll` path.

Reconciliation: CLI and dashboard reconciliation perform fresh HH reads and
local evidence persistence only. GET does not reconcile. Dashboard POST routes
retain local/security checks. Provider HH mutation calls during reconciliation:
0.

Notifications: reliability notifications are advisory projections. They expose
blocking reason, target, AttemptID, and evidence limitations. Request keys are
masked. Notification store failure does not change attempt/action state and
does not cause a write retry.

Unsafe controls: **NONE** for unresolved reliability records. No Retry,
Resend, Clear, Reset, Force, Apply again, Reply again, or Mark-not-sent
control exists in the reliability dashboard or reliability CLI. Existing
manual application/controlled-reply workflows remain separate workflows with
fresh gates and fresh identities.

R14.5c remains **SKIPPED BY DESIGN**: no demonstrated requirement needs an
`ACKNOWLEDGE ATTEMPT` or `MARK_REVIEWED ATTEMPT` domain mutation. Notification
lifecycle and inspection provide attention tracking without modifying safety
state.

# Notification authority

`ApplicationAttemptStore`, `AutoChatAttemptStore`, and the controlled action
store remain the safety authorities. `NotificationStore` is advisory only.

Dismiss, resolve, snooze, seen, and open operations mutate only notification
lifecycle data. They do not change attempt/action state, reconcile, create a
new attempt, consume a nonce, call HH, or release a replay block. Notification
persistence failure has no recursive notification loop and cannot authorize a
write.

Repeated application observations over 12 hours and auto-chat observations
over 15 minutes use bounded incident identity based on AttemptID/category (or
stable store-health identity). A new M2 trigger creates a new incident.

# JSON / PostgreSQL parity

JSON and PostgreSQL have materially equivalent safety semantics:

- application active conflict: vacancy-wide blocking states;
- auto-chat active conflict: conversation + exact trigger blocking states;
- reservation before dispatch;
- the same state transition and reconciliation rules;
- bounded deterministic inspection;
- immutable identity and no TTL deletion;
- corruption/transaction failure is an error, never silently treated as an
  empty authority.

JSON operations lock, reload, validate, and atomically write. PostgreSQL uses
transactions, row locks, checks, unique partial indexes, and bounded queries.
No `DATABASE_URL` is present in this environment, so live PostgreSQL
integration was not run; repository tests and SQL/static audits are green.

# Migrations

The embedded migration set is sorted and applied transactionally through
`internal/runtime/postgres.go`. Current reliability migrations are:

- `000006_application_attempts`;
- `000007_application_attempt_reconciliation`;
- `000008_legacy_auto_chat_attempts`;
- `000009_auto_chat_reconciliation`.

All embedded up migrations have matching down migrations. The migration
sequence was inspected without editing historical migrations. Docker is not
supported and is not required.

# HH write safety

Raw mutation owner: `internal/adapters/hh/write`

Write bypass: **NONE**. Runtime requesters are read-only for HH HTTP reads;
LLM/embedding POSTs are not HH mutations. The runtime HH write descriptor is a
projection, not a second HTTP transport.

HH WRITE RETRY: **NONE**.

The adapter performs one transport call. Network ambiguity, 409, 5xx, and
malformed success responses preserve uncertainty; 429 is rejected according
to the existing adapter contract but still counts as `TransportTried` for the
application dispatch budget. Read-only HH 429 retry and AI/provider retries
are separate concerns and occur before any HH mutation executor.

Terminal call graphs:

1. Controlled chat: dashboard send -> controlled action/nonce gateway -> R11
   reservation -> HH write gateway -> `internal/adapters/hh/write.Client.SendChatMessage`.
2. Legacy auto-chat reply: 15m scheduler -> exact-trigger orchestration ->
   auto-chat reservation -> stored-request-key action adapter -> HH write
   gateway -> `Client.SendChatMessage`.
3. Legacy auto-chat leave: 15m scheduler -> exact-trigger orchestration ->
   auto-chat reservation -> HH write gateway -> `Client.LeaveChat`.
4. Automatic application: 12h scheduler -> early gate -> application
   preparation/submission -> application reservation -> existing application
   executor -> HH write gateway -> `Client.SubmitVacancyResponse`.
5. Resume touch: 4h scheduler -> maintenance operation -> HH write gateway ->
   `Client.TouchResume`.
6. Job-search status: 24h scheduler -> maintenance operation -> HH write
   gateway -> `Client.SetJobSearchStatus`.

No R14 reconciliation, inspection, notification projector, or persistence
read model terminates in an HH writer.

# Capability matrix

| Package | HH read | HH write | AI | Local read | Local write |
|---|---:|---:|---:|---:|---:|
| `internal/applicationattempt` | No | No | No | No | No |
| `internal/usecase/applicationattempt` | No | No | No | Through Store port | Through Store port |
| `internal/usecase/applicationattemptpolicy` | No | No | No | Through reader port | No |
| `internal/usecase/applicationreconciliation` | Through read-only EvidenceReader | No | No | Yes | Evidence only |
| `internal/autochatattempt` | No | No | No | No | No |
| `internal/usecase/autochatorchestration` | Through read-only history/trigger ports | Existing action port only; no raw HH capability | Existing reply preparation only | Through ports | Attempt reservation/outcome |
| `internal/usecase/autochatreconciliation` | Through read-only history reader | No | No | Yes | Evidence only |
| `internal/usecase/reliabilityinspection` | No | No | No | Reader ports | No |
| `internal/usecase/reliabilitynotifications` | No | No | No | No | NotificationStore only |
| JSON/PG attempt adapters | No | No | No | Store implementation | Store implementation |

Direct import audit found no HH write, HH writer-port, LLM, or AI dependency
in the reconciliation, inspection, notification, or attempt-domain packages.

# R10/R11/R12/R13 regression

R10: no reliability/reconciliation/inspection/notification package invokes
AI. AI retries remain confined to existing AI/provider boundaries and cannot
lead to a second HH write after provider dispatch.

R11: raw HH mutation remains in one write adapter; one-shot mutation transport,
fresh preflight, ambiguity preservation, and dry-run/write-disabled gates are
intact.

R12: authoritative owners remain application processing/submission,
auto-chat orchestration, scheduler, and controlled reply workflow. R14 adds
gates and safety wrappers without duplicating those workflows.

R13: the only production executable is `cmd/hh-ai-responder`; there is no root
executable. Docker remains unsupported/not applicable.

# Scheduler

Completion-based cadences are unchanged:

- automatic applications: 12h;
- auto-chat: 15m;
- resume touch: 4h;
- job-search status: 24h;
- career iteration: default 15m.

There is no reconciliation retry scheduler and no ambiguity-release scheduler.

# Risk matrix after R14

| Workflow | Duplicate-write risk before R14 | After R14 | Residual |
|---|---|---|---|
| Automatic application | High across restart/concurrency | Low; durable vacancy reservation | Provider exactly-once and exact local causality are not established |
| Controlled chat | Medium before R11 closure | Low; durable action/nonce block | Provider exactly-once and notification delivery are not guaranteed |
| Legacy auto-chat reply | High for repeated trigger/restart | Low; exact-trigger reservation and stable key | Target-only reply evidence may not prove automatic causality |
| Chat leave | Medium | Low for replay; reservation is durable | Positive leave reconciliation unsupported |
| Resume touch | Low and repeatable | Low; intentional best-effort | Provider ambiguity may be observed only operationally |
| Job status | Low and repeatable | Low; intentional best-effort | No attempt-level delivery evidence |

Zero duplicate risk and exactly-once delivery are not claimed.

# Remaining reliability residuals

- Provider exactly-once is not established.
- Post-dispatch non-delivery cannot be proven from absent provider evidence.
- Auto-chat leave lacks positive reconciliation under the current provider
  contract.
- Target-response evidence may not establish causality to a specific local
  application/auto-chat AttemptID.
- Advisory notification persistence/delivery can fail.

These residuals are fail-closed and are not R14 blockers.

# Remaining operability debt

- Historical attempt records have no TTL deletion; future retention/archive
  work may be needed for growth.
- Live PostgreSQL integration coverage was not run without a database URL.
- A consolidated health view for reliability-store availability could improve
  operator ergonomics.

# Remaining structural debt

- `internal/runtime` remains large and contains compatibility composition.
- Root-era compatibility facades and DTO mappings remain.
- Persistence, CLI, and dashboard composition are still colocated in parts of
  the runtime.

# Metrics

- Go packages inspected: 61.
- R14 domain/port/use-case package additions audited: application attempt,
  auto-chat attempt, application attempt executor/policy/reconciliation,
  auto-chat orchestration/reconciliation, reliability inspection, and
  reliability notifications (10 package areas, plus adapters/runtime wiring).
- JSON attempt storage adapters: 2.
- PostgreSQL attempt storage adapters: 2.
- Reconciliation use cases: 2 R14-specific use cases plus existing controlled
  chat reconciliation.
- Inspection use case: 1.
- Notification projector: 1.
- New reliability migrations: 4 (`000006` through `000009`).
- Reliability dashboard route families: bounded application/auto-chat list and
  detail GETs, plus two local-evidence reconciliation POSTs.
- Reliability CLI command families: application and auto-chat inspection,
  each with a reconciliation subcommand.
- Mutation categories with durable replay protection: 4.
- Intentionally best-effort maintenance categories: 2.
- Categories supporting positive reconciliation: controlled chat, automatic
  application, and auto-chat reply (3); leave is unsupported.

# Verification

| Check | Result |
|---|---|
| `gofmt -l .` | PASS; no output |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |
| Focused R11/R12/R14 tests | PASS |
| Migration up/down pairing audit | PASS |
| Postgres live integration | NOT RUN — `DATABASE_URL` absent |
| Docker | NOT APPLICABLE |
| LIVE HH WRITES | 0 |

Focused coverage included the HH write adapter/gateway/preflight/reconcile,
application processing/submission/attempt/policy/reconciliation,
auto-chat orchestration/attempt/reconciliation, scheduler, JSON/PostgreSQL
attempt stores, reliability inspection/notifications, runtime dashboard APIs,
and reliability CLI.

# R14 final decision

R14: **COMPLETE**

Automatic application cross-run replay: **PROTECTED**

Auto-chat same-trigger replay: **PROTECTED**

Controlled chat replay: **PROTECTED**

Application ambiguity budget: **SAFE**

Reconciliation write capability: **NONE**

Absence releases uncertainty: **NO**

Notification safety authority: **NO**

Operator retry/reset bypass: **NONE**

Raw HH mutation bypass: **NONE**

Automatic HH mutation retries: **NONE**

# Next phase

Recommended: **D. Product/feature development rather than more architecture
work**.

Why: the safety-critical R14 architecture is closed, the remaining risks are
provider evidence limits and operational polish rather than an unsafe write
path, and the repository already has durable/concurrent boundaries in both
supported persistence modes. Feature work now has higher value than a broad
runtime decomposition.

Alternatives, ranked after product work:

1. **C. Operational hardening / retention / health** — highest maintainability
   leverage for long-running deployments, but not a current safety blocker.
2. **B. Persistence boundary cleanup** — useful if backend evolution or
   stronger live PostgreSQL coverage becomes a priority.
3. **A. Runtime package decomposition** — worthwhile only when a concrete
   change is being impeded by runtime concentration; current audit evidence
   does not justify it as the next safety phase.

# Ready

**R14 CLOSED.**

No fixes were implemented opportunistically. Do not start R15 or alter the
provider/retry/reset/retention architecture as part of this closure report.
