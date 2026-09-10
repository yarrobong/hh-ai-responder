# Before

ReconcileDelivery:

`HHWriteGateway.ReconcileDelivery` loaded a local action, performed a targeted
`ReadConversationState` read, searched `LastMessageID` and `MessageIDs` for the
stored provider message ID, and persisted `delivery_confirmed` on a match. A
missing message or read failure returned an error without enabling a resend.

Post-Send readback:

`HHWriteGateway.Send` repeated the same provider-ID search after an accepted
transport response. It mapped a missing or failed read to
`sent_unconfirmed`.

Career reconciliation:

`CareerDataReconciler` repairs local vacancy/application/conversation
relationships and projections. It does not own HH write delivery evidence and
has no HH write capability.

States:

The write gateway uses `sending`, `sent`, `sent_unconfirmed`,
`delivery_uncertain`, `manual_review`, and `delivery_confirmed`. Transport
accepted is persisted as `sent` before the targeted readback; ambiguous
transport is persisted as `delivery_uncertain`. The consumed nonce is never
reopened.

Matching:

The current positive evidence was an exact provider message ID in the targeted
conversation. No text fallback, semantic matching, polling, or approximate
matching existed.

Retry/resend:

There was no automatic HH write retry. Repeated `Send` calls remained blocked by
the persisted action/nonce state.

# Reconciliation policy matrix

| Operation | Reconciled | Evidence | Positive confirmation | No-match meaning |
|---|---|---|---|---|
| Chat message | Yes | Targeted conversation and exact provider message ID | `delivery_confirmed` | Accepted: `sent_unconfirmed`; ambiguous: `delivery_uncertain`/manual state |
| Chat leave | No current contract | None | None | No reconciliation read |
| Vacancy response / atomic test | No current contract | None | None | No reconciliation read |
| Resume touch | No current contract | None | None | No reconciliation read |
| Job-search status | No current contract | None | None | No reconciliation read |

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| Provider message matching | `HHWriteGateway.Send` and `ReconcileDelivery` | `internal/usecase/hhwritereconcile` | Extracted one authoritative deterministic matcher |
| State transition intent | Root gateway branches | `internal/usecase/hhwritereconcile` | Extracted; root persists the returned intent |
| Action/audit persistence | Root stores | Root compatibility adapter | Kept outside the use case; no migration |
| Career local relation repair | `CareerDataReconciler` | `CareerDataReconciler` | Unchanged; not HH delivery reconciliation |
| Scheduling | `CareerMonitor` | `CareerMonitor` | Unchanged |

# HH write reconciliation usecase

Package:

`internal/usecase/hhwritereconcile`

Service:

`Service.Reconcile(context.Context, AttemptEvidence) (Result, error)` performs
one bounded targeted read and returns a typed decision plus transition intent.
Confirmed actions and non-reconcilable operations are handled without a read.

Dependencies:

Only the consumer-owned `ChatDeliveryReader` and an optional clock. The package
does not import HH write ports, the write adapter, `hhwritegateway`, concrete
storage, or an AI provider.

Input:

Detached `AttemptEvidence`: action/operation identity, target conversation,
provider message ID, attempt context, transport outcome, and existing action
state. It contains no writer, store, approval, nonce-issuing, or gateway
capability.

Output:

`Result` contains `confirmed`, `not_confirmed`, `uncertain`,
`remote_unavailable`, `manual_review`, or `not_applicable`; deterministic match
strength; observed evidence; and a safe `TransitionIntent`.

# Chat delivery evidence

Provider ID:

An exact provider message ID is authoritative when supplied. A different ID
with similar text cannot confirm the action.

Conversation:

The targeted read must return the requested provider conversation identity. An
empty or different conversation identity is a `conflict`, never confirmation.

Sender:

When the full conversation reader exposes sender/direction, known employer,
recruiter, system, or incoming messages cannot confirm a candidate send.

Timestamp/order:

The existing algorithm did not use timestamps or order because it had no text
fallback. Attempt time is carried in the detached input for future/provider
contract use, but no weaker matching was introduced.

Text fallback:

None. The current reconciliation contract has no safe text fallback, including
for duplicate text.

Match authority:

`EXACT_PROVIDER_ID` is the only positive match produced by this stage. A legacy
state-only reader is adapted to the same exact-ID algorithm for compatibility
without inventing sender/direction metadata; full targeted records are
preferred when available and validate those fields when present.

# Transport outcome interaction

Accepted + found:

Return `confirmed` and transition `sent`/`sent_unconfirmed` to
`delivery_confirmed`.

Accepted + no match:

Return `not_confirmed` and keep/transition to `sent_unconfirmed`. This is not a
write failure and is not retryable.

Accepted + read failure:

Return `remote_unavailable` and keep/transition to `sent_unconfirmed`.

Ambiguous + found:

Return `confirmed` and transition `delivery_uncertain` to
`delivery_confirmed`; no resend occurs.

Ambiguous + no match:

Return `uncertain` and preserve `delivery_uncertain` or existing manual-review
state.

Ambiguous + read failure:

Return `remote_unavailable` and preserve uncertainty.

Rejected:

No delivery reconciliation is run for the failed/non-reconcilable action.

Not sent:

No delivery reconciliation is run.

# Crash / persistence windows

Stranded sending:

`sending` is accepted as a read-only reconciliation input. Positive exact
evidence can upgrade it to `delivery_confirmed`; missing/error evidence moves it
to `manual_review`. It never returns to `approved` and never calls a writer.

Persistence uncertain:

The existing gateway surfaces post-transport persistence uncertainty as a
manual-review-safe state. Later reconciliation uses only durable attempt
identity that is actually present; it does not synthesize missing provider
identity or nonce state.

Can resend automatically:

NO

# State transitions

sent:

Fresh exact evidence upgrades to `delivery_confirmed`; otherwise it becomes or
remains `sent_unconfirmed`.

sent_unconfirmed:

Repeated read-only reconciliation can later upgrade it to
`delivery_confirmed`; absence/error preserves the unconfirmed boundary.

delivery_uncertain:

Exact evidence upgrades it to `delivery_confirmed`; absence/error preserves
uncertainty.

delivery_confirmed:

Terminal and idempotent; no reader or writer is needed.

manual_review:

Positive evidence can confirm it. Otherwise it remains manual review.

# No-match semantics

Proof of failure:

NO. Not found, parse/read failure, eventual consistency, and unknown provider
state do not prove non-delivery.

Safe to resend:

NO. Existing gateway replay protection remains authoritative.

Future reconciliation:

An existing manual/scheduled caller may invoke another bounded read later. No
polling loop was added to the use case.

# Read failure semantics

Proof of non-delivery:

NO.

State:

Accepted attempts remain/transition to `sent_unconfirmed`; ambiguous and
stranded attempts remain uncertain/manual. The read error is returned to the
caller, with no write capability involved.

# Immediate readback

Owner:

`internal/usecase/hhwritereconcile.Service`

Send duplicate matcher:

NONE. Immediate post-accepted readback and manual `ReconcileDelivery` call the
same use case.

# Manual/career reconciliation

Owner:

The root gateway remains the persistence/orchestration adapter;
`hhwritereconcile` owns delivery evidence and decision semantics. Career local
projection reconciliation remains in `CareerDataReconciler`.

Scheduling:

UNCHANGED

Write capability:

NONE

# Persistence

Decision:

Use case.

State/audit persistence:

Root compatibility code persists the returned transition and existing audit
events. No concrete store is imported by the use case and no schema migration
was added.

Persistence failure after positive evidence:

The root manual reconciliation result retains confirmed status and provider ID
while returning the local persistence error. A later attempt is
reconciliation-only, never a send retry.

# Capability containment

HH writes:

NO

Gateway:

NO

Approval:

NO

AI:

NO

# Retry audit

Write retry:

NONE

Read transport retry:

Only existing adapter-level GET behavior remains in scope; this use case does
not add transport retry or polling.

Scheduled reconciliation:

Existing scheduling/composition remains unchanged.

# R11.5 inventory

Remaining root write compatibility:

The root `HHWriteGateway` still owns approval, preflight, transport composition,
local conversation projection, action persistence, and dashboard result mapping.

Duplicate gateway helpers:

No duplicate delivery matcher remains. `freshState` remains the preflight read
helper and is not a reconciliation decision owner.

Duplicate approval helpers:

None changed.

Duplicate preflight helpers:

None changed; preflight and reconciliation retain separate business decisions.

Duplicate reconciliation helpers:

None intended. `hhWriteChatDeliveryReader` is only a root compatibility adapter
for the semantic reader.

Raw write HTTP:

Unchanged in `internal/adapters/hh/write`.

Ready cleanup items:

Residual root compatibility and storage choreography can be addressed in R11.5
only. No scheduler, dashboard, or storage redesign is included here.

# Tests

The new service tests cover exact provider-ID confirmation, accepted and
ambiguous no-match preservation, wrong conversation/sender rejection, read
failure uncertainty, confirmed idempotence, and cancellation without a read or
confirmation. Existing gateway tests continue to assert one writer call and
read-only reconciliation.

# Dependencies

`go list -deps ./internal/usecase/hhwritereconcile` contains no HH write port,
HH write adapter, write gateway, or completion provider dependency.

# Behavior

Transport classification, approval, fresh preflight, dry-run enforcement,
nonce consumption, dashboard shapes, local career reconciliation, and HH read
sync behavior remain unchanged. The only new reconcilable operation is the
already-existing controlled chat-message delivery path.

# Verification

gofmt:

PASS

go test:

PASS — `go test ./...`

race:

PASS — `go test -race ./...`
