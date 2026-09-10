# R11.2 — HH Write Gateway / Safety Policy Boundary

## Before

The R11.1 narrow capabilities and one-attempt HH transport already existed in
`internal/ports/hhwrite` and `internal/adapters/hh/write`. The root
`HHWriteGateway.Send` still combined reusable execution policy with approval,
preflight, durable action state, transport, local projection, and delivery
readback.

The actual durable chat action lifecycle is:

`pending → approved → sending → sent | failed | delivery_uncertain`

Other terminal/manual states are `cancelled`, `stale`, `sent_unconfirmed`,
`manual_review`, and `delivery_confirmed`. Approval creates a one-shot send
nonce; it is durably consumed before transport dispatch. For chat it is also
the provider idempotency key, but it is not treated as a provider guarantee.

Approval validation, approval hashes, draft and RelevantKnowledge freshness,
fresh HH preflight, delivery readback, and reconciliation remain root
responsibilities.

## Operation policy matrix

| Operation | Enabled | Dry-run | Explicit intent | Nonce | Approval | Preflight | Durable action |
|---|---|---|---|---|---|---|---|
| Chat message | Yes | Before capability | Approved dashboard/CLI send or explicit legacy call | Controlled action only | Controlled action only, root | Controlled action only, root | Controlled action only |
| Chat leave | Yes | Before capability | Explicit leave call | No | No | No | No |
| Vacancy response / atomic test | Yes | Before capability | Explicit application call | No | No gateway approval; root application checks remain | Root fresh vacancy/application checks | No |
| Resume touch | Yes | Before capability | Explicit call plus `autoTouch` | No | No | No | No |
| Job-search status | Yes | Before capability | Explicit call plus `autoJobStatus` | No | No | No | No |

Resume touch and job-search status are intentionally not forced through
application/chat approval rules. Root flags and controlled-mode checks remain
defense-in-depth compatibility checks.

## Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `HH_WRITE_ENABLED` | Root config/gateway | Typed service option and root projection | Moved as input; no env read in usecase |
| `HH_DRY_RUN` | Root wrappers/gateway | Typed service option and root preview compatibility | Moved as input; no capability call |
| Explicit action | Root dashboard/CLI/workflow | Root workflow | Retained; no background execution |
| Nonce validation/consumption | Root `Send` | Service plus semantic `ActionStore` | Moved; reservation remains before dispatch |
| Action reservation/state mapping | Root `Send` | Service plus root storage facade | Moved; no concrete store import |
| Approval and approval hash | Root | Root | Not moved |
| Fresh preflight | Root | Root | Not moved |
| Draft/knowledge staleness | Root | Root | Not moved |
| HH transport | R11.1 adapter | R11.1 adapter | Unchanged |
| Outcome mapping | Root compatibility path | Service | Moved to neutral gateway result/state |
| Audit decision | Root send path | Service through `AuditSink` | Moved; persistence stays outside |
| Delivery readback | Root | Root | Not moved |
| Reconciliation | Root | Root | Not moved |

## HH write gateway usecase

Package: `internal/usecase/hhwritegateway`.

Constructor:

```go
hhwritegateway.NewService(
    hhwritegateway.Dependencies{...},
    hhwritegateway.Options{...},
)
```

The typed dependencies are the five existing narrow `hhwrite` writers plus
semantic `ActionStore` and `AuditSink` ports. The service exposes
`SendChatMessage`, `LeaveChat`, `SubmitVacancyResponse`, `TouchResume`, and
`SetJobSearchStatus`. It contains no `any`, reflection, generic raw writer,
concrete HH adapter, concrete JSON/Postgres adapter, or environment access.

## State model and ordering

Controlled chat execution is ordered as follows:

1. Load the action and reject terminal/replayed actions.
2. Apply enabled, dry-run, nonce, context, and limit gates.
3. Atomically persist `sending` and the consumed nonce.
4. Persist `send_started` audit intent.
5. Invoke exactly one matching capability.
6. Persist neutral transport outcome/evidence and terminal intent.

The nonce is never restored after rejection, ambiguity, or persistence failure.
Accepted and ambiguous actions therefore cannot be resent with the same
action/nonce. Reconciliation is read-only and remains outside this package.

## Transport mapping

| Transport result | Gateway result/state | Automatic retry |
|---|---|---|
| `OutcomeAccepted` | `accepted` / `sent` | No |
| `OutcomeRejected` | `rejected` / `failed` | No |
| `OutcomeNotSent` | `not_sent` / `failed` | No hidden retry |
| `OutcomeAmbiguous` | `delivery_uncertain` / `delivery_uncertain` | No; reconcile/manual review |
| HTTP 409 | `OutcomeAmbiguous` | No |
| HTTP 5xx | `OutcomeAmbiguous` | No |
| HTTP 429 | `OutcomeRejected` | No; recovery requires a new explicit action |

The R11.1 adapter remains the source of provider evidence and HTTP
classification. The gateway does not rebuild requests or generate a new chat
idempotency key after ambiguity.

## Failure windows

- Store failure before reservation: zero capability calls; fail closed.
- Audit failure after reservation but before transport: manual review when
  possible; zero capability calls.
- Transport followed by final action/audit persistence failure: result is
  `persistence_uncertain`, reconciliation is required, and resend is forbidden.
- A crash after reservation before dispatch, or after dispatch before final
  persistence, remains an intentionally unresolved window. The persisted
  consumed nonce and attempted state prevent unsafe replay.

## Approval / preflight / staleness boundary

Approval owner: root existing workflow.

Fresh preflight owner: root existing workflow.

Staleness owner: root existing workflow, including `RelevantKnowledgeHash`,
draft fingerprint, and application/conversation freshness.

Moved in R11.2: none of these checks. Existing preflight or stale approval
failures still prevent capability invocation.

## Capability containment

- R10 AI usecases end at proposals, drafts, assessments, interpretations, or
  test answers and have no `hhwrite` or gateway import.
- `hhreadsync` and HH read adapters have no write capability or gateway import.
- The gateway imports only `internal/ports/hhwrite`, semantic local types, and
  the standard library.
- Root composition supplies concrete adapters through narrow interfaces.

## Root compatibility

`HHWriteGateway` remains the compatibility/composition facade. It continues to
own approval, preflight, request-preview validation, local conversation state,
delivery confirmation, reconciliation, dashboard projections, and legacy
result translation. Its approved live chat path delegates execution to the
new service.

The legacy operations `SendChatMessage`, `LeaveChat`, `SendResponse`,
`ApplyVacancy`, `ApplyVacancyWithTest`, `TouchResume`, and
`SetActiveJobSearchStatus` retain their public signatures and operation flags;
their explicit capability calls now pass through the typed service. Test
submission remains atomic in `VacancyResponseRequest.Test`.

## Tests

Focused tests cover disabled/dry-run zero-capability behavior, accepted and
ambiguous replay lockout, pre-transport store failure, post-transport
persistence failure, concurrent reservation, and maintenance operations
without application approval state. Existing root, R10, read-sync, R11.1
transport, and port tests remain green.

## Verification

- `gofmt -w .`: PASS
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- Focused gateway/port/adapter tests: PASS
- Dependency/import and retry audits: PASS
- Docker build: SKIPPED; Docker CLI is present but the daemon is unavailable

LIVE HH WRITES: **0**.

## Ready for R11.3

**READY**. The reusable gateway core has one importable owner; capabilities are
invoked at most once; ambiguity blocks replay; disabled/dry-run gates stop
before capability invocation; durable reservation and nonce semantics are
fail-closed; and approval, fresh preflight, staleness, and reconciliation
remain higher-level boundaries.

R11.3 may extract approval, fresh-preflight, and staleness workflow boundaries.
R11.4 may address reconciliation. Neither is started by this change.
