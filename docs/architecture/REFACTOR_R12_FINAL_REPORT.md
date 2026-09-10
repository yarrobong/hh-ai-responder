# Executive summary

R12:
COMPLETE

Reason:

The final closure audit found explicit runtime ownership for the R10–R12
workflows. Root `package main` remains large, but its remaining responsibilities
are composition, compatibility conversion, CLI/dashboard routing, process
lifecycle, local persistence/projection, and bounded local repair. No second
authoritative owner was found for the extracted workflows.

The automatic application and legacy auto-chat paths terminate in the R11
gateway and the narrow HH write ports. Controlled conversation preparation
cannot send. Candidate-learning interpretation produces proposals and requires
explicit confirmation before canonical mutation. Unknown critical state fails
closed. Automatic HH write retry is absent.

This audit made no production-code changes. LIVE HH WRITES = 0.

The worktree was already dirty at audit start. Existing modifications,
renames, deletions, and untracked files were preserved.

# Stage status

R12.0:
COMPLETE

R12.1:
COMPLETE

R12.2a:
COMPLETE

R12.2b:
COMPLETE

R12.2c:
COMPLETE

R12.3:
COMPLETE

R12.4a:
COMPLETE

R12.4b:
COMPLETE

R12.5:
COMPLETE

R12.6:
COMPLETE

# Pre-audit inventory

The current inventory, measured from the repository rather than copied from
R12.6, is:

| Metric | Current value |
|---|---:|
| `go list ./...` package count | 49 |
| Root production Go files | 113 |
| Root production Go LOC | 30,961 |
| `main.go` LOC | 3,295 |
| Largest root file | `main.go` — 3,295 LOC |
| Largest root function | `DashboardServer.writeAPI` — approximately 364 LOC |

Largest root production files are `main.go` (3,295),
`hh_write_gateway.go` (1,446), `dashboard_server.go` (1,288),
`candidate_mutation.go` (998), `hh_read_sync.go` (909),
`ai_reply_orchestrator.go` (796), `hh_reply_pilot.go` (778),
`career_migration.go` (776), `hh_sync_cli.go` (764), and
`dashboard_views.go` (738).

The pre-audit `git status --short` showed extensive existing user worktree
changes, including a deleted `hh-ai-responder.test` binary and untracked R10–R12
files. No existing worktree change was reverted or reformatted in this audit.

# Authoritative ownership

| Concern | Authoritative owner | Root role | Duplicate owner? |
|---|---|---|---|
| Career iteration | `internal/usecase/careeriteration` | `CareerMonitor` lifecycle and type adapters | NONE |
| Career local repair | `CareerDataReconciler` | local repair and persistence compatibility | NONE |
| Employer reply preparation | `internal/usecase/employerreplyworkflow` plus `employerreply` | AI façade and draft/clarification projection | NONE |
| Follow-up preparation | `internal/usecase/followuporchestration` plus `followupdraft` | compatibility façade and local draft projection | NONE |
| Application answer generation | `internal/usecase/applicationanswer` | context adaptation and draft/clarification persistence | NONE |
| Inbox refresh | `internal/usecase/inboxrefresh` | dashboard trigger, projection, and response mapping | NONE |
| Legacy auto-chat | `internal/usecase/autochatorchestration` plus `autochatreply` | legacy source/action/audit adapters | NONE |
| Candidate learning | `internal/usecase/candidatelearningorchestration` | legacy model and mutation capability translation | NONE |
| Candidate mutation | `internal/usecase/candidatemutation` | explicit profile/import compatibility adapters | NONE |
| Application preparation | `internal/usecase/applicationprocessing` | batch order, counters, event projection, policy injection | NONE |
| Application submission | `internal/usecase/applicationsubmission` then R11 gateway | legacy result mapping | NONE |
| Vacancy analysis | `internal/usecase/vacancyanalysis` plus the single deterministic application policy boundary | root supplies existing configured policy values | NONE |
| Cover letter | `internal/usecase/coverletter` | typed leaf construction and legacy result mapping | NONE |
| Test answering | `internal/usecase/testanswer` | typed leaf construction and legacy result mapping | NONE |
| Scheduling | `internal/platform/scheduler` | task registration and feature gates | NONE |
| HH read sync | `internal/usecase/hhreadsync` | read façade, backend commit, and legacy projections | NONE |
| HH write gateway | `internal/usecase/hhwritegateway` | root action-store compatibility façade | NONE |
| HH transport | `internal/adapters/hh/write` | supplies configured adapter dependencies | NONE |
| HH write preflight | `internal/usecase/hhwritepreflight` plus existing gateway boundary | typed state conversion | NONE |
| HH delivery reconciliation | `internal/usecase/hhwritereconcile` | lifecycle/persistence conversion | NONE |
| Notifications | `CandidateNotificationEngine` | local notification calculation and persistence | NONE |
| Conversation policy | `internal/usecase/conversationpolicy` | legacy state-model conversion | NONE |

The root application policy provider is a compatibility implementation of the
single `applicationprocessing.Policy` capability. It contains existing
deterministic filters and configured thresholds, but does not create a second
batch, AI, submission, or transport workflow. The use-case service remains the
only runtime application-processing sequence.

## Runtime owner call graph audit

| Owner | Production caller(s) | Substantive workflow owned | Side effects |
|---|---|---|---|
| `careeriteration` | `CareerMonitor.RunOnce` | one career read/reconcile/notification iteration | HH reads; local relation, state, and notification writes |
| `employerreplyworkflow` | `AIReplyOrchestrator.PrepareEmployerReply`, dashboard/CLI | employer context gate, reply preparation, draft/clarification choice | AI; local draft/clarification writes; no HH write |
| `followuporchestration` | `AIReplyOrchestrator.PrepareFollowUp`, dashboard | follow-up eligibility and draft/clarification preparation | AI; local draft/clarification writes; no HH write |
| `inboxrefresh` | `DashboardServer.executeSync`, synchronous/background dashboard sync | one inbox refresh and projection sequence | HH reads; local reload/projection/notification/daily-state writes; no HH write |
| `autochatorchestration` | `AutoRespondChats`, recurring scheduler | legacy history gate, reply/leave choice, action orchestration | HH reads; AI; local audit; HH chat send/leave only through gateway |
| `candidatelearningorchestration` | `CandidateKnowledgeAcquisitionService`, dashboard/CLI adapters | gap, answer, interpretation, proposal, confirmation lifecycle | AI; local clarification/proposal/canonical mutation through ports; no HH write |
| `applicationprocessing` | `HHAIResponder.ApplyVacancies` | one-vacancy read, analysis, policy, cover-letter/test preparation | HH reads; AI; local review/clarification/preparation data; no HH write |
| `applicationsubmission` | automatic batch compatibility path and manual application compatibility path | fresh vacancy/test proof and one submission attempt | fresh HH reads; one HH write through executor/gateway; local result mapping |
| `scheduler` | root task registration, `CareerMonitor`, dashboard ticker | timer and completion-based recurrence only | timers/goroutines; no HH, AI, storage, or business-policy side effects |
| `hhreadsync` | root `HHReadSyncService`, inbox and career adapters | normalized HH read/import policy | HH reads and supplied import ports; no HH write |
| `hhwritegateway` | controlled actions, auto-chat, application executor, maintenance façades | write capability gating, one-attempt dispatch, outcome mapping | narrow HH write port; audit/action lifecycle persistence |
| `hhwritereconcile` | gateway delivery reconciliation | read-only interpretation of an already-attempted write | targeted HH read; local lifecycle transition; never HH write |

For every row, the root caller is an adapter, façade, batch coordinator, or
process boundary. No root caller duplicates the substantive sequence listed in
the third column.

# Root workflow closure

SUBSTANTIAL ROOT BUSINESS WORKFLOWS:
NONE

| Root symbol | Classification | Closure finding |
|---|---|---|
| `ApplyVacancies` | COMPOSITION / COMPATIBILITY / READ MODEL / PROJECTION | Owns search ordering, counters, stop conditions, event projection, and legacy result mapping; preparation and submission are delegated. |
| `AutoRespondChats` | COMPATIBILITY | Thin façade over `autochatorchestration`. |
| `CareerMonitor.Run` | PROCESS LIFECYCLE | Loads state and invokes the scheduler around `RunOnce`. |
| `CareerMonitor.RunOnce` | COMPATIBILITY / ADAPTER | Converts root stores and state to `careeriteration`. |
| `CandidateKnowledgeAcquisitionService` | COMPATIBILITY / PERSISTENCE ADAPTER | Adapts legacy candidate stores and canonical mutation capabilities to candidate-learning orchestration. |
| `AIReplyOrchestrator` | COMPATIBILITY BRIDGE / PERSISTENCE ADAPTER | Constructs typed leaves, maps context, and persists drafts/clarifications; it does not own raw AI policy. |
| `PrepareEmployerReply` | COMPATIBILITY BRIDGE | Delegates to `employerreplyworkflow`. Preparation cannot send. |
| `PrepareFollowUp` | COMPATIBILITY BRIDGE | Delegates to `followuporchestration`; it produces a draft or clarification only. |
| `PrepareApplicationAnswer` | COMPATIBILITY / ADAPTER | Loads/adapts application and Candidate context, invokes `applicationanswer`, and persists the returned draft/clarification. |
| `DashboardServer.executeSync` | DASHBOARD ADAPTER / READ MODEL / PROJECTION | Routes inbox to `inboxrefresh`; retained non-inbox compatibility sync and local projection choreography do not duplicate inbox policy. |
| `DashboardServer.writeAPI` | DASHBOARD ADAPTER | Parses/mappings and delegates to established owners; raw HH write and direct CompletionProvider business calls are absent. |
| `runHHCommand` | CLI ADAPTER | Parses, constructs, invokes typed services, and maps output. |
| `CareerDataReconciler` | LOCAL REPAIR / PERSISTENCE ADAPTER | Repairs local vacancy/application/conversation relations and projections only. |
| `HHReadSyncService` | READ MODEL / PROJECTION / PERSISTENCE ADAPTER | Wraps `hhreadsync`, maps legacy models, and commits through the selected backend. |
| `HHWriteGateway` | COMPATIBILITY / PERSISTENCE ADAPTER | Retains legacy action/audit state and maps into the typed R11 gateway; transport is not implemented here. |

The root symbols above are not counted as substantial workflows because each
terminates in a typed owner or is limited to local composition/projection.

# Application flow

The production automatic application trace is:

```text
scheduler Loop (immediate, then 12h after completion)
  -> HHAIResponder.ApplyVacancies (batch order, limits, counters, events)
  -> applicationprocessing.Service.Prepare (one-vacancy preparation)
  -> vacancyanalysis / coverletter / testanswer typed leaves
  -> applicationsubmission.Service.Submit (fresh read-only proof)
  -> rootApplicationExecutor compatibility mapping
  -> hhwritegateway.Service.SubmitVacancyResponse
  -> narrow hhwrite.VacancyResponseWriter
  -> internal/adapters/hh/write.Client
```

Root owns only batch ordering, limits/counters, event projection, and legacy
compatibility mapping. It does not duplicate vacancy-analysis AI sequencing,
Candidate-context truth rules, cover-letter logic, test-answer validation,
fresh submission proof, or write transport policy. The atomic vacancy response
and optional test remain one HH mutation.

Manual `ApplyVacancy`, `ApplyVacancyWithTest`, and `SendResponse` are retained
compatibility APIs. Their test-answer path calls the typed `testanswer` service
through `SolveTests`, and their final mutation still reaches the R11 gateway;
they are not a second automatic `ApplyVacancies` loop.

# Conversation flows

Controlled employer reply:

```text
caller -> employerreplyworkflow -> employerreply leaf
       -> draft or clarification persistence
       -> explicit approval -> fresh preflight
       -> hhwritegateway -> write port -> HH write adapter
       -> optional read-only delivery reconciliation
```

Follow-up uses the same shape through `followuporchestration` and
`followupdraft`. Preparation cannot send, and explicit approval is required
before the write path.

Legacy auto-chat is separate:

```text
scheduler Loop (immediate, then 15m after completion)
  -> AutoRespondChats façade
  -> autochatorchestration
  -> autochatreply leaf
  -> legacy action adapter
  -> R11 hhwritegateway -> HH write adapter
```

History gates, leave/reply orchestration, ambiguity handling, and no-retry
semantics are in the auto-chat owner and gateway. Legacy auto-chat does not
share the controlled employer-reply approval workflow.

# Candidate learning

The verified trace is:

```text
gap -> clarification -> candidate answer
    -> candidateinterpretation
    -> proposal or structured choice
    -> explicit confirmation
    -> candidatemutation
```

AI interpretation cannot directly confirm canonical truth. Proposal state is
distinct from confirmed truth; unknown remains unknown. Canonical mutation is
performed by the candidate-mutation boundary or an explicitly configured
profile/import compatibility adapter. No R12 change introduced a hidden
semantic reindex from candidate-learning confirmation.

The existing optional post-commit semantic-index hook is separately wired in
the candidate backend. It is not part of the candidate-learning orchestration
contract and was not newly introduced by R12.

# Career / inbox

`CareerMonitor` delegates one iteration to `careeriteration`. Its root
responsibilities are state-file lifecycle, root store adapters, and
notification/projection compatibility. `CareerDataReconciler` is limited to
local vacancy/application/conversation relation repair, projection repair, and
local persistence. It has no HH write, AI, or scheduler dependency and is not
`hhwritereconcile`.

Dashboard inbox refresh delegates the substantive sequence to
`inboxrefresh.Service`: HH read, local reload, projection, notifications, and
daily-state persistence. Background refresh uses a wait-first ticker and
bounded/coalesced work. Draft generation after inbox projection is local/AI
work and performs no HH write.

# Scheduler

Current ownership and cadence:

| Task | Cadence | Semantics |
|---|---|---|
| Automatic application | 12h | completion-based loop |
| Legacy auto-chat | 15m | completion-based loop |
| Resume touch | 4h | completion-based loop |
| Job-search status | 24h | completion-based loop |
| Career monitor | default 15m | completion-based loop |
| Dashboard inbox refresh | configured ticker | wait-first trigger plus existing coalescing |

The scheduler package has no HH, AI, storage, or business-policy dependency.
Run-once commands invoke the workflow directly and bypass scheduler waits. No
overlap, immediate retry, or changed first-run behavior was introduced.

# HH read

`HHReadSyncService` is a root compatibility façade around
`internal/usecase/hhreadsync`. The use case owns read/import policy and uses
HH read ports. Root owns legacy JSON model conversion, backend selection,
atomic JSON/Postgres commit choreography, and read-model projection. Root does
not duplicate the import policy.

# HH write safety

Raw mutation owner:

`internal/adapters/hh/write` is the only production owner of HH mutation HTTP
construction. The exact live mutation inventory remains:

- chat send: `/chatik/api/send`;
- chat leave: `/chatik/api/leave`;
- atomic vacancy response/test: `/applicant/vacancy_response/popup`;
- resume touch: `/applicant/resumes/touch`;
- job-search status: `/profile/shards/user_statuses/job_search_status`.

Root raw mutation HTTP:
NONE

Generic arbitrary mutation escape hatch:
NONE

All live-capable paths terminate as follows:

| Path | Terminal chain |
|---|---|
| Controlled chat send | approval/preflight -> `hhwritegateway` -> narrow chat port -> write adapter |
| Legacy auto-chat send/leave | `autochatorchestration` action adapter -> `hhwritegateway` -> write adapter |
| Automatic application | `applicationsubmission` -> root executor -> `hhwritegateway` -> vacancy port -> write adapter |
| Manual application/test | root compatibility API -> `hhwritegateway` -> vacancy port -> write adapter |
| Resume touch | scheduler/root façade -> `hhwritegateway` -> resume port -> write adapter |
| Job-search status | scheduler/root façade -> `hhwritegateway` -> status port -> write adapter |

WRITE BYPASS = NONE.

Automatic write retry:

HH WRITE RETRY = NONE.

409, 5xx, network uncertainty, malformed success evidence, and missing success
identity remain ambiguous where required. 429 remains rejected. Later
scheduler execution is a new workflow iteration, not same-attempt retry.

Application cross-run ambiguity remains a known residual: no new durable
cross-run attempt identity was added, so an independent later iteration still
relies on current HH state and existing response evidence. Same-run replay and
immediate retry are absent.

# AI ownership

DIRECT ROOT BUSINESS AI:
NONE

Substantial AI workflows terminate in typed owners:

| Workflow | Typed owner |
|---|---|
| Vacancy analysis | `internal/usecase/vacancyanalysis` |
| Cover letter | `internal/usecase/coverletter` |
| Test answering | `internal/usecase/testanswer` |
| Employer reply | `internal/usecase/employerreply` / `employerreplyworkflow` |
| Follow-up draft | `internal/usecase/followupdraft` / `followuporchestration` |
| Legacy auto-chat reply | `internal/usecase/autochatreply` / `autochatorchestration` |
| Candidate interpretation | `internal/usecase/candidateinterpretation` / `candidatelearningorchestration` |
| Application answer | `internal/usecase/applicationanswer` |

Root `AIClient`, provider conversion, legacy prompt/result shapes, and
`SolveTests` are compatibility bridges around typed leaves. No root prompt
business algorithm or direct business CompletionProvider bypass was found.

# Candidate truth ownership

Canonical Candidate writes terminate in `candidatemutation`-backed paths.
Explicit profile-management and HH-resume import commands remain allowed
compatibility adapters. AI direct Candidate mutation is NONE;
candidate-learning direct raw canonical mutation is NONE; root duplicate
mutation policy is NONE. Candidate safety tests preserve exact experience
values, unknown state, restricted claims, and the prohibition on promoting AI
inference to confirmed truth.

# Semantic indexing

R12 preserves the R12.3 invariant: candidate-learning confirmation does not
newly trigger hidden semantic reindexing. Existing explicit semantic indexing
and the pre-existing optional post-commit hook remain separately observable.
No behavior change was made in this audit.

# Storage / backend boundaries

Extracted use cases do not branch JSON versus Postgres and do not import
concrete storage adapters. Root/backend adapters select the configured backend,
perform legacy model translation, and preserve atomic file/Postgres behavior.
No generic UnitOfWork was introduced.

The dependency audit found:

- no internal package imports the root package;
- root imports internal packages as expected;
- no use-case package imports `internal/adapters/storage/json`;
- no use-case package imports `internal/adapters/storage/postgres`;
- no use-case package imports `internal/adapters/hh/read` or
  `internal/adapters/hh/write`;
- no import cycle.

# Compatibility constructor audit

Production audited paths use typed constructors/options for AI orchestration,
HH read sync, HH write gateway, application stores, and dashboard wiring.
Legacy variadic constructors remain for tests, integration/source compatibility,
and legacy callers. No dynamic constructor remains the authoritative
production dependency-discovery path for an audited workflow.

# Dead / bypass surface audit

The retained legacy symbols `SendResponse`, `ApplyVacancy`,
`ApplyVacancyWithTest`, `SolveTests`, and variadic constructors are used as
compatibility APIs or tests. They do not own raw HH mutation transport and do
not create a second automatic application/auto-chat loop. No independent
write-capable bypass was found.

BYPASS CAPABILITY:
NONE

# Timer search

| Occurrence family | Classification |
|---|---|
| `internal/platform/scheduler` `Loop` timers | PLATFORM SCHEDULER |
| Dashboard `Ticker` and `hh_sync_cli` process ticker | DASHBOARD COALESCING / PROCESS HEARTBEAT |
| Root/adapter HH read wait timers | HH SAFE READ RATE LIMIT |
| OpenAI provider timer | LLM/PROVIDER RETRY |
| Vacancy analysis, test answer, candidate interpretation, application answer, employer reply, and follow-up draft timers | SEMANTIC/USECASE RETRY for malformed AI output only |

DUPLICATE BUSINESS COMPLETION LOOP = NONE.

# Final side-effect matrix

| Workflow | HH read | AI | Local writes | Candidate truth | HH write | Scheduler |
|---|---|---|---|---|---|---|
| Career iteration | inbox/read sync | no | state, relations, notifications | no | no | `careeriteration` called by root lifecycle loop |
| Employer reply | local/read context; fresh preflight only at send | typed employer reply leaf | draft/clarification | clarification/proposal boundary only | approval -> R11 gateway | caller/dashboard |
| Follow-up | local/read context; fresh preflight only at send | typed follow-up leaf | draft/clarification | clarification/proposal boundary only | approval -> R11 gateway | caller/dashboard |
| Inbox refresh | HH inbox read | optional post-refresh draft generation | projection, notifications, daily state | no | no | dashboard ticker/coalescing |
| Legacy auto-chat | awaiting chats and bounded history | typed auto-chat leaf | audit/events | no | chat send/leave through gateway | 15m completion loop |
| Candidate learning | local Candidate/clarification state | typed interpretation leaf | clarification/proposal records | explicit confirmation via candidatemutation | no | caller/dashboard/CLI |
| Application preparation | vacancy description/applicability/test reads | typed analysis, cover-letter, test-answer leaves | events/clarification projection | no canonical mutation | no | called by application batch |
| Application submission | fresh vacancy/test proof | no | result/event projection | no | one gateway attempt | automatic 12h loop or caller |
| Maintenance touch/status | optional configured read context | no | event/result projection | no | one gateway capability call | 4h / 24h loops |

# Architecture graph

```text
CLI / Dashboard / Scheduler
          |
          v
root composition and compatibility adapters
  - batch/order/counters/events
  - legacy model conversion
  - local persistence and read projections
          |
          +--> careeriteration
          +--> employerreplyworkflow -> employerreply
          +--> followuporchestration -> followupdraft
          +--> inboxrefresh
          +--> autochatorchestration -> autochatreply
          +--> candidatelearningorchestration -> candidateinterpretation
          +--> applicationprocessing -> vacancyanalysis / coverletter / testanswer
          +--> applicationsubmission
          |
          v
domain + narrow ports
          |
          +--> hhreadsync -> HH read ports -> internal/adapters/hh/read
          +--> hhwritepreflight
          +--> hhwritegateway -> HH write ports -> internal/adapters/hh/write
          +--> hhwritereconcile (read-only delivery evidence)
          +--> candidatemutation -> storage adapters
          +--> conversationpolicy / vacancy domain
          +--> typed AI leaves -> CompletionProvider -> LLM adapter
```

Candidate truth flow remains:

```text
candidate gap -> clarification -> answer -> interpretation/proposal
              -> explicit confirmation -> candidatemutation -> canonical storage
```

# Root debt vs blocker

## Compatibility debt

- legacy root response/request models;
- legacy public application and auto-chat façade methods;
- variadic constructors retained for source compatibility;
- root `AIReplyOrchestrator`, `HHReadSyncService`, and `HHWriteGateway` façade
  types.

## Composition debt

- large `main.go` and root package;
- dashboard dispatch size;
- root backend selection and typed-service assembly;
- future move to `cmd/hh-ai-responder/main.go`.

## Persistence debt

- JSON/Postgres projection and transaction choreography remains in root adapters;
- local CareerDataReconciler remains coupled to legacy repository interfaces;
- legacy action/audit and response-model persistence remains in root.

## Observability / reliability debt

- some existing read/projection error paths are intentionally partial or
  best-effort;
- notification/projection save reliability remains a known existing concern;
- cross-run application ambiguity has no durable attempt identity.

## Known safety risk

- An independent later application iteration after an ambiguous provider result
  can only rely on fresh HH state and existing response evidence; R12 preserves
  this documented residual and does not add same-attempt replay.
- Partial/ignored read reliability paths can leave local projections stale;
  they fail closed for the relevant write preflight and do not create a write
  bypass.

## R12 blocker

NONE.

The remaining root deterministic application policy provider is a single
compatibility capability supplied to `applicationprocessing`, not a duplicate
runtime workflow. Moving it or the root façade types is composition-root work,
not required for R12 closure.

# Post-R12 target

The next architecture phase should be Composition Root Migration:

```text
cmd/hh-ai-responder/main.go
  -> explicit composition/config/lifecycle wiring
  -> existing root compatibility packages during migration
  -> unchanged internal/usecase and adapter owners
```

That phase should reorganize root implementation packages and compatibility
models without changing runtime behavior, cadence, dry-run safety, write
ownership, or JSON event compatibility. It is not started by this audit.

# Progress

R0:

Approximately 41k root production LOC, 84 production files in the original
monolithic inventory, and `main.go` at 5,183 LOC.

R12.0:

30,136 root production LOC and `main.go` at 3,924 LOC, with the major runtime
workflow ownership gaps explicitly identified.

R12.Final:

49 packages, 113 root production Go files, 30,961 root production LOC, and
`main.go` at 3,295 LOC. Raw size is not the closure criterion; explicit owner
boundaries and side-effect containment are.

# Test coverage review

Characterization/boundary coverage remains present for:

- `careeriteration`;
- `employerreplyworkflow`;
- `followuporchestration`;
- `inboxrefresh`;
- `autochatorchestration`;
- `candidatelearningorchestration`;
- `applicationprocessing`;
- `applicationsubmission`;
- scheduler;
- R10 AI leaves and provider boundaries;
- R11 write gateway, preflight, reconciliation, and adapter;
- dashboard and CLI;
- `CareerMonitor`;
- `ApplyVacancies`;
- `AutoRespondChats`;
- HH read sync.

Candidate safety tests cover exact experience preservation, unknown-state
handling, restricted claims, and AI-to-confirmed-truth rejection. Write tests
cover dry-run blocking, no automatic retry, ambiguity classification, and
read-only delivery reconciliation.

# Verification

All requested checks passed:

| Check | Result |
|---|---|
| `gofmt -l .` | PASS; no files listed |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| Focused root safety/workflow tests | PASS |
| Focused R10/R11/R12 use-case tests | PASS |
| Docker build | SKIPPED; Docker CLI is installed but the daemon is unavailable at `unix:///var/run/docker.sock` |

No test or audit command performed a live HH write. LIVE HH WRITES = 0.

# Closure decision

R12 = COMPLETE.

The repository is ready for the separately scoped post-R12 Composition Root
Migration. Do not begin that migration as part of R12.Final.
