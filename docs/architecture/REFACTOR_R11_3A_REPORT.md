# R11.3a — Approval + Draft/Staleness Boundary

## Before

The controlled chat flow was composed by the root `HHWriteGateway`:

`draft -> root approval -> root local checks -> root fresh HH preflight -> hhwritegateway -> HH write capability -> root projection/reconciliation`

Approval was required only for controlled AI employer-reply and follow-up chat
actions. Chat leave, vacancy response (including the atomic vacancy test),
resume touch, and job-search status were explicit operations with their own
write gates. Legacy auto-chat did not use the `AIDraft` approval path.

`ApproveDraft` created an `ApprovedHHAction`, froze raw draft text and its
SHA-256, captured conversation and scoped Candidate Knowledge evidence, and
issued the existing `hh-action-...` and `hh-send-...` random IDs. The root
also invalidated prior local approvals after draft edits/regeneration and
checked content, conversation, and knowledge freshness during preflight.

The persisted draft state values are `generated`, `approved`, `rejected`,
`superseded`, and `sent`. The durable HH action state is separate:
`pending`, `approved`, `sending`, `sent`, `sent_unconfirmed`, `failed`,
`cancelled`, `stale`, `delivery_uncertain`, `manual_review`, and
`delivery_confirmed`.

Relevant Knowledge uses the existing R8 scoped `RelevantKnowledgeHash`.
Semantic retrieval is relevance-only and is not used as approval authority.
`followUpFingerprint` remains follow-up workflow metadata because it was not
part of the persisted approved action freshness contract.

## Approval policy matrix

| Draft/action type | Approval required | Hash binding | Knowledge binding | Nonce |
|---|---:|---|---|---|
| AI employer reply / controlled chat message | Yes | Raw draft text SHA-256; conversation version; latest delivered message | Scoped `RelevantKnowledgeHash` where available | One-shot `hh-send-...` |
| AI follow-up / controlled chat message | Yes | Raw draft text SHA-256; conversation version; latest delivered message | Scoped `RelevantKnowledgeHash` where available | One-shot `hh-send-...` |
| Chat leave | No | None | None | None |
| Vacancy response / application answer / atomic test | No gateway approval | Root application/preflight state | Root application semantics | None |
| Resume touch | No | None | None | None |
| Job-search status | No | None | None | None |
| Legacy auto-chat | Existing legacy path | Not forced into `AIDraft` approval | Existing legacy path | Existing legacy path |

## Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| Raw content hash | Root `HHWriteGateway` | `internal/usecase/writeapproval.ContentHash` | Moved; algorithm unchanged |
| Conversation version | Root `HHWriteGateway` | `writeapproval.ConversationVersion` through a root value adapter | Moved; persisted JSON shape unchanged |
| Approval creation and evidence | Root `ApproveDraft` | `writeapproval.Service.Create` plus root persistence adapter | Moved; root still persists `ApprovedHHAction` |
| Active duplicate/reapproval checks | Root `ApproveDraft` | `writeapproval.Service.Create` | Moved; current action history preserved |
| Draft edit invalidation decision | Root edit loop | `writeapproval.InvalidateDraft` | Moved; root applies and audits result |
| Regeneration invalidation decision | Root loop | `writeapproval.InvalidateConversation` | Moved; root applies and audits result |
| Local draft/conversation/knowledge freshness | Root `preflightLocked` | `writeapproval.Service.ValidateForSend` | Moved; root remains orchestration boundary |
| Scoped knowledge snapshot construction | R8 Candidate Knowledge owner | R8/root candidate semantics | Remains; approval package compares the supplied hash only |
| Nonce issuance | Root `ApproveDraft` | `writeapproval.Service` with injectable ID generator | Moved; secure random format unchanged |
| Nonce reservation/consumption | `hhwritegateway` composition/service | `internal/usecase/hhwritegateway` | Unchanged |
| Fresh HH preflight | Root `freshState` / `preflightLocked` | Root | Not moved; R11.3b inventory only |
| HH transport | R11.1 adapter | R11.1 adapter | Unchanged |
| Delivery readback/reconciliation | Root | Root | Not moved; R11.4 remains separate |
| Approval audit persistence | Root `HHWriteAuditStore` | Root adapter after semantic decision | Persistence remains outside usecase |

## Write approval usecase

Package: `internal/usecase/writeapproval`

Service: `writeapproval.Service`

Dependencies: standard-library time and cryptographic randomness only; an
optional `Now` and `IDGenerator` are injected for deterministic tests.

Input: typed `CreateInput`, `DraftSnapshot`, `ConversationSnapshot`, existing
typed approval evidence, and current scoped knowledge hashes.

Output: typed `ApprovalCreated` containing `Approval` evidence and explicit
invalidation decisions; typed `ValidationResult` containing a status, reasons,
and `AuthorizationEvidence` on success.

There is no boolean trust API such as `CanSend(approved, stale, hashMatches)`.
Callers branch on `ValidationStatus` and pass the resulting evidence upward.

## Approval evidence

Bound fields remain the existing fields of `ApprovedHHAction`: draft ID,
conversation ID, action type/purpose, exact approved text, actor, approval
time, source/latest message IDs, conversation version, legacy candidate
knowledge version, scoped relevant knowledge hash, content hash, and send
nonce.

Content hash is SHA-256 over the exact raw draft text. No trimming,
normalization, or new persisted hash was introduced. Conversation version uses
the same JSON field names and message shape as the old root calculation.

Approval validity now compares the current typed snapshots against that
evidence before any fresh HH read. Draft text changes, conversation/message
changes, missing local HH identity, and relevant knowledge changes produce a
stale/invalid result. Unrelated Candidate Knowledge changes do not invalidate a
scoped approval. Manual-review and transport-terminal action states remain
non-sendable.

Behavior: **UNCHANGED**, with the additional safe ordering that local stale
evidence returns before fresh remote preflight.

## Nonce

Issued by `writeapproval.Service.Create` using the existing 16-byte
cryptographic-random format (`kind-` plus 32 hex characters).

Consumed by `internal/usecase/hhwritegateway` during durable action
reservation, before transport dispatch.

Binding: the nonce is stored in the same approved action as its draft/action
identity. Presented nonce comparison is typed and a nonce from another
approval cannot authorize the current approval.

Reapproval creates a new action ID and nonce. An old stale/terminal approval
remains history and cannot authorize the new action.

## Staleness

- Text change: stale through exact text and content-hash comparison.
- Relevant knowledge change: stale when the existing scoped
  `RelevantKnowledgeHash` differs or cannot be established.
- Metadata: only fields included in the existing conversation version or
  approved artifact remain relevant; excluded knowledge metadata does not make
  an approval stale.
- Local state: non-approved/manual-review/terminal action states fail closed;
  draft invalidation decisions preserve sent/cancelled history.
- Remote HH state: **OUTSIDE / R11.3b**. `freshState` remains in root and is
  called only after local validation succeeds.

## Concurrency and persistence

The root gateway mutex and existing store behavior remain in place. The new
usecase makes approval decisions from a supplied snapshot and does not claim a
different snapshot is approved. Existing JSON action/draft stores retain their
atomic individual-file saves, locks, and version-1 persisted shapes; no
migration or concrete storage dependency was introduced. Cross-file approval
and draft persistence remains the pre-existing root compatibility limitation,
not a new usecase responsibility.

The existing action uniqueness behavior is characterized and preserved:
another active action for the same conversation/purpose/latest employer
message is rejected; an old stale/terminal action may remain history while a
new approval is created.

## Capability containment

Can HH read: **NO**

Can HH write: **NO**

Can call gateway: **NO**

Can import concrete persistence: **NO**

AI can approve: **NO**. R10 AI usecases continue to return proposals/drafts,
and no AI usecase imports `writeapproval` or creates approval evidence,
trusted authorization, or a write nonce.

## Root compatibility

`ApproveDraft` still owns the existing local context and Candidate Knowledge
snapshot acquisition, validates the draft through the existing safety path,
then delegates deterministic approval creation and nonce issuance to
`writeapproval`. It persists the returned evidence into the unchanged
`ApprovedHHAction` shape and continues to set the draft status and append the
existing approval audit event.

`EditDraft` and regeneration continue to persist stale action state and audit
events, but the invalidation selection is now returned by the pure usecase.

`PreflightAction` and `Send` continue to own fresh targeted HH reads, request
preview validation, gateway invocation, delivery readback, and local
projection. Invalid/stale local approval returns before `freshState`, so it
cannot invoke preflight or the gateway.

Remaining root preflight entry points for R11.3b:

- `HHWriteGateway.PreflightAction` in `hh_write_gateway.go`;
- `HHWriteGateway.Send` fallback preflight in `hh_write_gateway.go`;
- `HHWriteGateway.freshState` and `HHWriteGateway.preflightLocked` remote
  checks;
- dashboard `POST /api/actions/{id}/preflight`;
- CLI `runHHActionPreflight` / `hh action-preflight`.

Reconciliation remains in `HHWriteGateway.ReconcileDelivery` and the existing
career reconciler; no reconciliation code moved in R11.3a.

## Tests

Pure `writeapproval` tests cover approval creation, exact content binding,
conversation version changes, draft text staleness, relevant versus unrelated
knowledge changes, invalid/manual-review states, reapproval with a new nonce,
old approval invalidation, and terminal history preservation.

Root integration coverage verifies that editing an approved draft or changing
relevant knowledge stops before fresh HH preflight and makes zero writer calls.
Existing root coverage continues to verify exact request parity, nonce
consumption/replay protection, dry-run/write-disabled behavior, terminal
preflight short-circuiting, delivery uncertainty, and reconciliation.

## Dependencies

The new package has no imports of `hhread`, `HHAIResponder`, `net/http`, the
HH adapters, `internal/ports/hhwrite`, `internal/usecase/hhwritegateway`, or
concrete JSON/Postgres storage. Its dependency graph is:

`root typed snapshot adapter -> writeapproval -> typed approval evidence -> root fresh preflight -> hhwritegateway -> hhwrite capability`

There is no `writeapproval -> HH` or `AI -> writeapproval` edge.

## Behavior

Approval semantics: **UNCHANGED**

Approval hash/content binding: **UNCHANGED**

RelevantKnowledgeHash semantics: **UNCHANGED**

Conversation/fingerprint semantics: **UNCHANGED**; follow-up fingerprint
remains owned by follow-up workflow because it was not part of action validity.

Nonce generation/binding: **UNCHANGED**

Nonce consumption: **UNCHANGED / R11.2**

Explicit Send: **UNCHANGED**

Fresh HH preflight: **UNCHANGED / ROOT**

HH transport: **UNCHANGED**

Reconciliation: **UNCHANGED**

AI: **UNCHANGED**

Storage: **UNCHANGED**

Dashboard: **UNCHANGED**

## Verification

The final verification commands and results are recorded below.

- `gofmt -w .`: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- Focused writeapproval/gateway/port/adapter tests: PASS
- Docker: SKIPPED — Docker CLI is present, but the daemon is unavailable
- LIVE HH WRITES: **0**

## Ready for R11.3b

**READY** — local approval/staleness has one importable owner; approval cannot
send; text and relevant knowledge changes invalidate according to the current
contract; nonce issuance and R11.2 consumption remain separated; invalid
local evidence stops before HH preflight; and the remaining fresh HH preflight
paths are inventoried above.

R11.3b is not started. R11.4 reconciliation, R11.5 cleanup, dashboard/frontend
work, and command composition changes are not started.
