# Before

Residual root write surface consisted of the legacy `HHAIResponder` methods,
the root `HHWriteGateway` composition façade, and compatibility adapters around
the extracted write port. The concrete HH mutation transport was already in
`internal/adapters/hh/write`.

This pass found and removed four clearly dead write-specific helpers: the old
fresh-state helper, an unused vacancy transport shim, an unused legacy writer
wrapper, and unused status/error event mapping helpers. It also removed the
obsolete `controlledWriteOnly` guard that prevented legacy auto-chat,
application, leave, resume-touch, and job-status calls from reaching their
typed gateway path.

The remaining root surface is intentional orchestration and compatibility:
approval/draft persistence, local business validation, fresh-read adaptation,
result projection, and post-attempt local projection. It does not construct
provider mutation requests.

## Compatibility wrappers

| Symbol | Classification | Why retained | Authoritative delegate |
|---|---|---|---|
| `SendChatMessage` / `LeaveChat` | ROOT COMPATIBILITY | Preserve legacy responder API and auto-chat control flow | `hhwritegateway.Service` → narrow port |
| `SendResponse` / `ApplyVacancy` / `ApplyVacancyWithTest` | ROOT ORCHESTRATION | Preserve application and atomic-test workflow API | `hhwritegateway.Service` → `VacancyResponseWriter` |
| `TouchResume` | ROOT COMPATIBILITY | Preserve maintenance loop API | `hhwritegateway.Service` → `ResumeWriter` |
| `SetActiveJobSearchStatus` | ROOT COMPATIBILITY | Preserve maintenance loop API | `hhwritegateway.Service` → `JobSearchStatusWriter` |
| `hhWriteAdapter` | PERSISTENCE/COMPOSITION ADAPTER | Resolve existing runtime URLs, HTTP client, and XSRF token | `internal/adapters/hh/write.Client` |
| `freshChatPreflightService` / `hhWritePreflightChatReader` | READ-ONLY COMPATIBILITY ADAPTER | Translate existing HH read interfaces | `internal/usecase/hhwritepreflight` |
| `reconcileDeliveryAttempt` / `hhWriteChatDeliveryReader` | READ-ONLY COMPATIBILITY ADAPTER | Translate existing targeted-read interfaces | `internal/usecase/hhwritereconcile` |
| `contentHash` / `conversationVersion` | COMPATIBILITY DELEGATE | Preserve root helper names | `internal/usecase/writeapproval` |

Future cleanup can remove these wrappers when root composition is migrated; no
such migration is part of R11.5.

## Duplicate algorithms

* Approval content hashing, conversation-version hashing, nonce issuance, and
  approval invalidation are owned by `writeapproval`. Root hash helpers only
  delegate.
* Fresh remote chat comparison is owned by `hhwritepreflight`. Root retains
  only local workflow/business checks and read adaptation.
* Provider delivery matching is owned by `hhwritereconcile`; both immediate
  post-send readback and later `ReconcileDelivery` use the same matcher.
* Reservation, nonce consumption, write policy, outcome mapping, and one-shot
  dispatch are owned by `hhwritegateway.Service`.
* No duplicate raw HH mutation transport, delivery matcher, approval algorithm,
  or automatic HH-write retry remains.

## Raw HTTP

All five mutation endpoint constructions appear only in
`internal/adapters/hh/write` at runtime. The root `hh_write_transport.go`
contains only the offline Chatik preview/validation contract and its endpoint
string; it does not dispatch HTTP.

## Retry

HH write retry is absent. The remaining retry loops are limited to safe HH GET
transport, LLM/provider transport, semantic generation, and read-only
reconciliation scheduling. Network uncertainty, 409, 5xx, malformed success
evidence, and post-write persistence failures remain ambiguous/manual-review
states and never invoke the writer again.

# Final mutation inventory

| Operation | Port | Transport | Higher workflow | Preflight | Reconciliation |
|---|---|---|---|---|---|
| Chat message | `ChatMessageWriter` | `internal/adapters/hh/write.Client.SendChatMessage` | Controlled draft/action or legacy auto-chat | Local approval + targeted chat read where controlled | Exact provider message ID for controlled action |
| Chat leave | `ChatLeaveWriter` | `Client.LeaveChat` | Explicit legacy auto-chat leave | Operation policy only | None |
| Vacancy response | `VacancyResponseWriter` | `Client.SubmitVacancyResponse` | Application workflow | Fresh vacancy response state | None |
| Vacancy response + test | `VacancyResponseWriter` | Same atomic form POST | Test fetch/answer validation + application workflow | Fresh test metadata and vacancy state | None |
| Resume touch | `ResumeWriter` | `Client.TouchResume` | Maintenance loop | Maintenance flags only | None |
| Job-search status | `JobSearchStatusWriter` | `Client.SetJobSearchStatus` | Maintenance loop | Maintenance flags only | None |

There is no separate test-submission capability or separate resume-update
writer.

# Final write ownership

| Concern | Authoritative owner | Root role |
|---|---|---|
| Approval evidence | `internal/usecase/writeapproval` | Snapshot conversion and persistence |
| Local stale validation | `writeapproval.Service.ValidateForSend` | Supplies current snapshots |
| Nonce issuance | `writeapproval.Service.Create` | Persists returned approval |
| Fresh chat preflight | `internal/usecase/hhwritepreflight` | Read adapter and workflow orchestration |
| Fresh vacancy preflight | `hhwritepreflight.Service` plus existing vacancy read parser | Root converts legacy vacancy state and applies workflow policy |
| Fresh test metadata comparison | `hhwritepreflight` test contract | Root fetches and translates current metadata |
| Write enabled/dry-run | `internal/usecase/hhwritegateway.Service` | Supplies configured options |
| Action reservation/replay | `hhwritegateway.Service` + semantic action store | Root persistence adapter |
| Nonce consumption | `hhwritegateway.Service` + `ActionStore.Reserve` | Durable store translation |
| Transport outcome mapping | `internal/adapters/hh/write` | Root only translates compatibility result types |
| Chat HTTP mutation | `internal/adapters/hh/write.Client` | No raw HTTP |
| Leave HTTP mutation | `internal/adapters/hh/write.Client` | No raw HTTP |
| Vacancy/test HTTP mutation | `internal/adapters/hh/write.Client` | No raw HTTP |
| Resume touch HTTP mutation | `internal/adapters/hh/write.Client` | No raw HTTP |
| Job-status HTTP mutation | `internal/adapters/hh/write.Client` | No raw HTTP |
| Delivery matching | `internal/usecase/hhwritereconcile` | Persistence of transition intent |
| Reconciliation state decision | `hhwritereconcile.Service` | Applies transition to local store |

# Final operation flows

Controlled chat:

`proposal → draft → writeapproval → local validation → fresh targeted chat
preflight → hhwritegateway → ChatMessageWriter → write adapter → targeted read
→ hhwritereconcile → local projection`

Legacy auto-chat:

`AI proposal/result → root auto-chat policy → root compatibility method →
hhwritegateway direct chat operation → ChatMessageWriter → write adapter`

It intentionally does not require AIDraft approval under the current legacy
contract, but still has write-enabled, dry-run, narrow-port, and one-attempt
guards.

Chat leave:

`explicit leave decision → root compatibility method → hhwritegateway policy →
ChatLeaveWriter → write adapter`

Vacancy response:

`application workflow → fresh vacancy preflight → root typed request →
hhwritegateway policy → VacancyResponseWriter → write adapter`

Vacancy + atomic test:

`fresh test metadata/read → strict answer validation → one
VacancyResponseRequest containing response and test answers → one adapter POST`

Resume touch:

`maintenance flag → root compatibility method → hhwritegateway policy →
ResumeWriter → write adapter`

Job-search status:

`maintenance flag → root compatibility method → hhwritegateway policy →
JobSearchStatusWriter → write adapter`

The maintenance flows intentionally do not add approval, nonce, or delivery
reconciliation. Dry-run and disabled policy stop before any concrete writer.

# Raw HTTP audit

Mutation endpoints:

* `/chatik/api/send`
* `/chatik/api/leave`
* `/applicant/vacancy_response/popup`
* `/applicant/resumes/touch`
* `/profile/shards/user_statuses/job_search_status`

Runtime owner: `internal/adapters/hh/write`.

Root mutation HTTP: NONE.

Generic mutation escape hatch: NONE. The public compatibility method
`HHRequester.Do` now rejects every method except GET and HEAD. Its transport
retry remains read-only. The concrete write adapter keeps its low-level `do`
helper private.

# Approval

Owner: `internal/usecase/writeapproval`.

Root algorithm: NONE. Root converts domain values to typed snapshots and
persists the returned approval/invalidation decisions.

Compatibility: `contentHash` and `conversationVersion` delegate to the
authoritative package. Candidate/relevant-knowledge hashes elsewhere in the
repository are unrelated domain fingerprints, not duplicate approval content
hashes.

# Preflight

Owner: `internal/usecase/hhwritepreflight`, with the existing vacancy HTML
parser retained as a read/value adapter for the legacy application workflow.

Root algorithm: no duplicate remote chat comparison. Root `preflightLocked`
performs local approval validation and application/conversation business policy
after the delegated fresh targeted read.

Fresh Send preflight: YES for controlled actions, including dry-run. A terminal
action is handled as local-only and is never reopened by a fresh read.

`freshState` had no callers and was removed; the remaining `freshChatPreflight`
reader is a targeted read adapter and owns no “can send?” or “was delivered?”
business semantics.

# Gateway

Owner: `internal/usecase/hhwritegateway`.

Write enabled: every root legacy write service receives the responder’s
configured `HH_WRITE_ENABLED` value; controlled dashboard/CLI services use the
same typed option.

Dry run: blocks before a concrete writer; controlled Send may still perform
read-only fresh preflight.

Nonce: issued by `writeapproval`, durably reserved/consumed before controlled
dispatch by the gateway action store.

Replay: accepted, sent-unconfirmed, ambiguous, persistence-uncertain, stranded
`sending`, and manual-review actions are not sendable again.

Automatic retry: NO.

# Transport

Owner: `internal/adapters/hh/write`.

Dispatch per invocation: 1.

409: AMBIGUOUS.

5xx: AMBIGUOUS.

Network uncertainty: AMBIGUOUS.

Malformed success evidence: AMBIGUOUS.

429: REJECTED at transport level and never automatically resent.

Retry: NONE.

# Reconciliation

Owner: `internal/usecase/hhwritereconcile`.

Positive evidence: exact provider message ID in the exact target conversation,
with known-wrong sender/direction rejected.

No match: NOT FAILURE PROOF.

Read failure: NOT FAILURE PROOF.

Write capability: NONE. Reconciliation performs targeted reads and returns
transition intents; root applies local persistence.

# Capability containment

AI: NO WRITE. All eight audited R10 usecases have no HH write-port,
gateway, adapter, approval, or nonce dependency.

HH read sync: NO WRITE.

Preflight: READ ONLY.

Reconciliation: READ ONLY.

Gateway: NARROW WRITE PORTS.

Adapter: ACTUAL HH MUTATION.

# Replay / ambiguity safety

Accepted: terminal/accepted projection; no second writer call.

Sent unconfirmed: read-only reconciliation only; no resend.

Ambiguous: delivery uncertainty/manual review; no resend and no new key.

Persistence uncertain: remains manual/reconciliation state; no nonce reset or
automatic send.

Stranded sending: restart/reload does not make it sendable; only
reconciliation/manual handling is allowed.

Rejected/not-sent: transport outcome is preserved. A definite non-delivery
result is not treated as permission to reuse the same action/nonce; a new
explicit action/reapproval is required by the existing workflow.

Can auto-resend: NO.

# Compatibility debt

The root façades listed above remain to avoid unrelated CLI/dashboard and
composition migration. They contain no provider request construction and all
live-capable calls enter the typed gateway or its narrow writer ports.

# Dead code removed

Removed only helpers with no production or test callers after the R11
extraction:

* root `freshState`;
* root `HHAIResponderWriteClient` wrapper;
* root unused HH status/error mapping helpers;
* unused root vacancy transport shim;
* unused root transport-error event population helper.

# Retry audit

HH write retry: NONE.

Safe GET retry: retained in the HH read transport for 429/read scheduling.

LLM/semantic retry: retained and unrelated to HH mutation.

Scheduled read-only reconciliation: retained; it cannot invoke a writer.

# Tests

Focused R11.1–R11.4 suites pass, including write-port/adapter outcome tests,
gateway replay and persistence-failure tests, approval staleness tests,
fresh-preflight tests, and reconciliation exact-ID/no-match tests.

The added regression verifies legacy resume-touch and job-status paths make zero
requests when `HH_WRITE_ENABLED` is false and exactly one request each when an
enabled `httptest` transport is used. The read requester regression verifies a
POST cannot reach the network through the shared public read API.

# Dependencies

* `writeapproval` depends only on its value types and standard library.
* `hhwritepreflight` depends on approval evidence types, never write ports.
* `hhwritegateway` depends on `internal/ports/hhwrite` only for mutation
  capabilities.
* `hhwritereconcile` has no write-port, write-adapter, or gateway dependency.
* `hhreadsync` and `internal/adapters/hh/read` have no write capability.
* `internal/adapters/hh/write` is the sole provider mutation implementation.

# Behavior

HH endpoint contracts, approval evidence, RelevantKnowledgeHash, targeted
preflight, nonce issuance/consumption, write-enabled policy, dry-run behavior,
one-attempt dispatch, 409/5xx/network ambiguity, exact-ID reconciliation,
dashboard/CLI shapes, AI behavior, and storage formats remain unchanged except
for the required containment fixes described above.

# Verification

gofmt: PASS

go test -count=1 ./...: PASS

go test -race ./...: PASS

go vet ./...: PASS

go build ./...: PASS

git diff --check: PASS

node --check web/app.js: PASS

Focused R11 and R10 suites: PASS

Raw mutation HTTP audit: PASS

Generic mutation API audit: PASS

Capability dependency audit: PASS

Duplicate approval/preflight/gateway/reconciliation audit: PASS

Docker: SKIPPED — Docker CLI is installed, but the daemon is unavailable.

LIVE HH WRITES: 0.

# R11 final status

R11.1 Transport: COMPLETE

R11.1a Outcome hardening: COMPLETE

R11.2 Gateway: COMPLETE

R11.3a Approval: COMPLETE

R11.3b Fresh preflight: COMPLETE

R11.4 Reconciliation: COMPLETE

R11.5 Residual cleanup: COMPLETE

Raw HH mutation bypasses: NONE

Automatic HH mutation retries: NONE

Substantial root write algorithms: NONE; root retains documented
orchestration/compatibility and local projection logic.

R11: COMPLETE

# Next architecture phase

READY FOR NEXT ARCHITECTURE STAGE.

Do not start it as part of R11.5.
