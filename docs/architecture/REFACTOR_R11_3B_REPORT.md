# R11.3b — Fresh Targeted Preflight Boundary

## Before

The controlled approved-chat flow was composed by the root `HHWriteGateway`:

`action -> local writeapproval validation -> root freshState/preflightLocked -> hhwritegateway -> HH write adapter`.

`PreflightAction`, the live `Send` fallback, dashboard preflight, and the CLI
preflight all entered that root path. The root comparison covered the HH
conversation external ID, latest relevant message ID, terminal state, reply
requirement, warnings, and local-history completeness. Local draft,
conversation, and Candidate Knowledge freshness was already owned by
`writeapproval` from R11.3a.

Chat leave, resume touch, and job-search status are maintenance mutations with
no existing targeted preflight and remain GET-free. Vacancy response and its
atomic test submission have a separate legacy root application flow. Its
provider HTML/embedded-state reader remains root compatibility code; its
deterministic availability decision is now delegated to the new typed
preflight use case. No test remapping or new resume selection was added.

## Preflight policy matrix

| Action | Fresh read required | Reads | Remote stale conditions |
|---|---:|---|---|
| Controlled chat message | Yes | One targeted conversation state read | Destination mismatch, changed latest message, candidate reply/new employer message, terminal state, non-reply-required state, warnings, incomplete history, read failure |
| Vacancy response / atomic test | Existing application flow | Existing vacancy-response GET and embedded state | Archived, already responded, unavailable, unknown critical state, test applicability change when supplied |
| Chat leave | No existing check | None added | N/A |
| Resume touch | No existing check | None added | N/A |
| Job-search status | No existing check | None added | N/A |

## Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `PreflightAction` | Root gateway | Root orchestration + `hhwritepreflight.Service` | Kept public shape; remote proof delegated |
| Send fallback preflight | Root `Send` / `preflightLocked` | Root orchestration + `hhwritepreflight.Service` | Always fresh, including dry-run |
| `freshState` | Root targeted read helper | Root read/value adapter | Retained only for delivery readback and reconciliation compatibility |
| Chat remote comparison | Root `preflightLocked` | `hhwritepreflight.Service.PreflightChatMessage` | Moved |
| Vacancy response decision | Root `vacancyPreflightDecision` | `hhwritepreflight.Service.PreflightVacancyResponse` | Moved; provider parser remains legacy root adapter |
| Dashboard POST preflight | Dashboard -> root gateway | Dashboard -> root gateway -> use case | Shape unchanged |
| CLI action preflight | CLI -> root gateway | CLI -> root gateway -> use case | Shape unchanged |
| Test answering | `testanswer` plus existing application flow | `hhwritepreflight.CompareTestMetadata` for the final fresh snapshot | Structural AI validation remains; provider task/option identity is rechecked before response |
| Delivery reconciliation | Root gateway/career reconciliation | Root | Outside R11.3b |

## HH write preflight usecase

Package: `internal/usecase/hhwritepreflight`

Service: `hhwritepreflight.Service`

Dependencies:

- `ChatStateReader`, a narrow read-only targeted chat capability;
- `VacancyResponseStateReader`, a narrow typed vacancy-response capability;
- an injectable clock for evidence timestamps.

Input:

- `writeapproval.AuthorizationEvidence` already returned by successful local
  validation;
- typed operation and target identity;
- the local message-count baseline where the current contract requires it.

Output:

- typed `Result` / `VacancyResult` with `Status` values such as `passed`,
  `stale`, `blocked`, `manual_review`, and `remote_unavailable`;
- short-lived observation `Evidence` containing operation, target, observed
  identity/state/message ID, count, and timestamp;
- reasons and the underlying read error where applicable.

There is no `PreflightOK` authorization API, write capability, nonce, store,
AI call, mutation result, or delivery state in the package.

## Read capabilities

Ports are package-local semantic interfaces. The chat composition adapter maps
the existing `HHConversationPreflightReader` into `ChatStateReader`; the
existing R9 HH read adapter remains the concrete transport owner. The vacancy
provider parser is still the legacy root read adapter because it parses HH
HTML/embedded state and was deliberately deferred by R9; only its typed
decision policy moved into the use case.

The package imports neither `internal/ports/hhwrite`,
`internal/usecase/hhwritegateway`, `internal/adapters/hh/write`, root
`HHAIResponder`, nor `net/http`.

## Chat preflight

Fresh evidence includes the HH external conversation ID, latest relevant
message ID, state, reply requirement, message count, warnings, and provider
message IDs. Approval for employer message M1 therefore fails when a fresh
read shows candidate message C1 or newer employer message M2. Closed or
rejected conversations fail before the gateway, and non-reply-required state,
warnings, incomplete history, destination mismatch, and read errors fail
closed. Message content hashing and provider button/action validation were not
invented; the existing contract is identity/state based.

## Vacancy response preflight

The typed policy preserves the existing known-bit semantics for availability,
archive state, already-responded state, test presence, letter requirement,
response URL, and application availability. A confirmed existing response or
unavailable vacancy blocks duplicate submission. Unknown critical state is
manual review. The atomic test response remains one vacancy-response mutation;
the preflight package does not submit a test, remap task/option IDs, choose a
resume, or mutate local application/Candidate state.

The legacy provider read still supplies embedded test metadata to the existing
application flow. After answers are structurally validated, the flow performs
one additional targeted metadata GET immediately before the atomic response.
`CompareTestMetadata` compares test identity, start metadata, task IDs, open
task markers, and option IDs. Any change blocks the response; no old-to-new
option remapping is attempted.

## Read failures

Timeouts, cancellation, authentication failures, 429/5xx responses, and parse
failures are unavailable/system outcomes from the read capability. They do not
fall back to cached/local state in the extracted chat service and cannot reach
the gateway. Safe GET retry, if any, remains owned by the HH read adapter;
there is no preflight business retry and no write retry.

## Nonce / side effects

Preflight does not issue or consume a nonce, persist a concrete action, mutate
Candidate Knowledge, call an HH writer, or call the gateway. Repeated
`PreflightAction` calls remain read-only. The root stores the existing
diagnostic result for dashboard/CLI compatibility; this is projection state,
not a durable send grant.

LIVE HH WRITES: **0**.

## Send integration

The effective chain is:

`writeapproval.ValidateForSend -> hhwritepreflight -> hhwritegateway -> HH write adapter`.

The root still owns loading actions/drafts, local validation, request-preview
validation, lifecycle/audit projection, and post-accepted delivery readback.
Explicit `Send` always runs a fresh targeted preflight, even if a dashboard or
CLI preflight passed earlier and even in dry-run. The gateway remains the only
owner of nonce reservation and mutation dispatch. No model generation or
unrelated workflow is inserted between the final proof and gateway call.

## TOCTOU

The unavoidable external window is `fresh targeted GET -> HH mutation POST`.
It is minimized by running the read immediately before gateway dispatch. The
existing provider idempotency/evidence and R11.4 reconciliation boundary
remain the mitigations; this stage does not claim HH transactionality.

## Root compatibility

`PreflightAction`, dashboard `POST /api/actions/{id}/preflight`, and CLI
`hh action preflight <action-id>` preserve their response/CLI shapes. Invalid
local approval still returns before any fresh HH read. Remote stale state
returns before `hhwritegateway` and before the writer. `freshState` remains a
small root read adapter for delivery confirmation/reconciliation and does not
own chat preflight comparison. Vacancy response public methods and their
read-only preflight behavior remain source-compatible.

## R11.4 inventory

Reconciliation entry points are `HHWriteGateway.ReconcileDelivery`, the
post-accepted readback in `HHWriteGateway.Send`, and the existing career/read
reconciliation services. They read the targeted conversation after a
transport attempt, search for the provider message identity, and transition
`sent` to `delivery_confirmed` when found. Missing/failed readback remains
`sent_unconfirmed`; ambiguous transport remains `delivery_uncertain` or
`manual_review`. These paths and states were not changed.

## Tests

Added focused use-case coverage for unchanged chat state, candidate reply,
newer employer message, terminal state, read failure, cancellation, duplicate
vacancy response, and unknown vacancy state. Existing root coverage continues
to verify local-stale short-circuiting, no writer calls on remote stale state,
dry-run nonce preservation, dashboard/CLI read-only preflight, gateway
transport behavior, and vacancy GET-only blocking.

## Dependencies

The new dependency direction is:

`root typed read adapter -> hhwritepreflight -> typed read-only capability`.

The surrounding workflow remains:

`writeapproval -> hhwritepreflight -> hhwritegateway -> hhwrite`.

No schema or storage migration was added.

## Behavior

Local approval, HH transport classification, nonce consumption, dry-run/write
gates, maintenance write counts, AI behavior, dashboard shapes, and delivery
reconciliation remain unchanged except that explicit Send no longer treats a
previous same-process preflight as sufficient; it performs the required fresh
proof again.

## Verification

- `gofmt` check: PASS for changed Go files
- `go test -count=1 ./...`: PASS
- `go test -race ./...`: PASS
- `go vet ./...`: PASS
- `go build ./...`: PASS
- `git diff --check`: PASS
- `node --check web/app.js`: PASS
- Focused `hhwritepreflight`, `writeapproval`, and `hhwritegateway` tests: PASS
- Dependency audit: PASS; no write/gateway/HTTP/persistence imports in the use case
- Docker: SKIPPED — Docker daemon unavailable
- LIVE HH WRITES: **0**

## Ready for R11.4

**READY** for the delivery reconciliation boundary. Fresh controlled-chat remote proof has one
importable owner; local stale authorization stops before reads; remote stale
state stops before the gateway; explicit Send re-preflights; preflight has no
mutation capability and consumes no nonce; and reconciliation remains outside
this stage.
