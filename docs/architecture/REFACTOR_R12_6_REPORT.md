# Before

R12.6 started from the post-R12.5 worktree. The previous stages had already
extracted the substantial runtime owners; the remaining root package was a
mixture of process composition, legacy API shapes, dashboard/CLI adapters,
read/write compatibility, and persistence projection glue.

Root responsibilities before this stage:

- executable entrypoint, configuration conversion, logging and lifecycle;
- HH requester/read compatibility and local read projections;
- CLI and dashboard routing;
- JSON/PostgreSQL repository selection and transaction composition;
- compatibility façades for extracted AI, candidate, career, application,
  inbox, auto-chat and HH write use cases.

Hotspots audited:

- `main.go`, `dashboard_server.go`, `dashboard_sync.go`;
- `hh_write_gateway.go`, `hh_read_sync.go`, `hh_sync_cli.go`;
- `candidate_learning_facade.go`, `ai_reply_orchestrator.go`;
- `career_monitor.go`, `reconciliation.go`, `application_processing.go`.

`main.go` was 3,295 lines at the R12.6 baseline. The root production package
was 113 files and 30,961 lines. The earlier recorded comparisons are R0:
approximately 41k root production lines and 5,183 `main.go` lines; R12.0:
30,136 root production lines and 3,924 `main.go` lines.

`main.go` audit:

- entrypoint and exit-code selection: composition;
- configuration loading and legacy conversion: composition/compatibility;
- logger and process lifecycle: infrastructure;
- HH/AI/read/write construction: composition;
- recurring task registration: process lifecycle/composition;
- profile, HH and dashboard command dispatch: thin CLI/dashboard adapters;
- legacy HH response types and request helpers: compatibility/read adapter glue.

Dashboard audit:

- `writeAPI` parses request bodies, validates route-local fields, delegates
  HH actions to `HHWriteGateway`, delegates AI preparation to
  `AIReplyOrchestrator`, and persists local notification/draft/knowledge
  projections. It does not issue raw HH mutation requests or duplicate an
  extracted external workflow.
- `executeSync` retains targeted/all non-inbox compatibility sync and local
  projection refresh. Inbox refresh terminates in `internal/usecase/inboxrefresh`.
- background refresh queue/coalescing is bounded dashboard infrastructure,
  not a business workflow.

CLI audit:

- `runHHCommand` branches are syntax adaptation, typed service construction,
  one-shot use-case invocation, or output mapping;
- profile commands remain explicit profile-management commands;
- candidate knowledge commands use the candidate-learning boundary where
  appropriate;
- run-once still invokes the existing iteration directly and never waits for
  a scheduler.

# Classification

| Symbol/file | Before | After | Substantial business logic |
|---|---|---|---|
| `main.go` | mixed composition and compatibility | composition, lifecycle, adapters, legacy HH projection | No |
| `NewAIReplyOrchestrator` | dynamic compatibility construction | legacy wrapper over typed construction | No |
| `NewHHReadSyncService` | dynamic compatibility construction | legacy wrapper over typed construction | No |
| `NewHHWriteGateway` | dynamic compatibility construction | legacy wrapper over typed construction | No |
| `NewApplicationStore` | dynamic compatibility construction | retained compatibility wrapper; typed path used by production wiring | No |
| `ApplyVacancies` | batch loop plus application policy boundary | batch order/counters/events delegated to `applicationprocessing` and `applicationsubmission` | No; compatibility coordination only |
| `AutoRespondChats` | legacy façade | thin delegate to `autochatorchestration` | No |
| `CandidateKnowledgeAcquisitionService` | legacy façade and typed translation | thin candidate-learning façade and storage translation | No; mutation translation only |
| `AIReplyOrchestrator` | root orchestration façade | typed construction plus thin adapters to R10 leaves/workflows | No |
| `CareerMonitor` | monitor lifecycle and iteration boundary | scheduler lifecycle plus `careeriteration` delegation | No |
| `CareerDataReconciler` | local relationship repair and persistence | retained narrow local repair compatibility use case | No; no HH, AI, or scheduler policy |
| `HHReadSyncService` | read-sync façade and projections | typed construction around `hhreadsync` plus persistence compatibility | No; import policy is in `hhreadsync` |
| `DashboardServer.writeAPI` | route dispatch and local projections | route adapter; external actions terminate in established owners | No duplicate external workflow |
| `DashboardServer.executeSync` | sync dispatch and projection refresh | inbox delegated; non-inbox compatibility retained | No duplicate inbox workflow |
| `runHHCommand` | CLI dispatch | CLI adapter and output mapping | No |
| `internal/platform/scheduler` | scheduler extracted in R12.5 | unchanged authoritative timer primitive | No |

# Cleanup performed

Moved:

- No broad file or package move was needed. The R12 workflow owners were
  already in their dedicated `internal/usecase/*` packages.

Added/centralized:

- `NewAIReplyOrchestratorWithOptions` and
  `AIReplyOrchestratorOptions`;
- `NewHHReadSyncServiceWithOptions` and
  `HHReadSyncServiceOptions`;
- `NewHHWriteGatewayWithOptions`;
- production composition now uses the typed paths for dashboard AI/read/write
  wiring and monitor read-sync wiring;
- production application-store construction uses its existing typed
  `NewApplicationStoreWithDependencies` path.

Removed:

- No compatibility symbol was deleted. The remaining variadic constructors
  have test/integration or source-compatibility callers and are now explicit
  legacy paths.

Retained:

- JSON/PostgreSQL backend selection in root composition;
- `HHRequester` safe-read rate/concurrency scheduling;
- dashboard queue/coalescing mechanics;
- process-lock heartbeat;
- local reconciliation and projection choreography;
- legacy constructor names and root response shapes.

Why:

The typed construction paths reduce dynamic dependency discovery without
breaking existing tests or integrations. The retained code is either
composition, local projection/persistence compatibility, or a thin adapter;
none is a second owner of an HH write, AI decision, candidate mutation policy,
or extracted runtime workflow.

# main.go

Responsibilities after:

- entrypoint, command selection and exit codes;
- configuration/environment boundary and legacy config conversion;
- logger, HTTP/read requester and process lifecycle infrastructure;
- `HHAIResponder` compatibility façade and unavoidable HH read projections;
- construction of typed services and legacy CLI/API adapters.

The substantial workflow methods are no longer implemented in `main.go`:
`AutoRespondChats` delegates to auto-chat orchestration, application
submission delegates through application processing/submission and the R11
gateway, and recurring behavior delegates to the scheduler primitive.

LOC: 3,295 lines at this stage. R12.6 intentionally does not start the
post-R12 `cmd/hh-ai-responder` migration.

# Dashboard

`writeAPI`: retained as a route adapter because it owns request parsing,
status-code mapping, local draft/notification/knowledge projection updates,
and calls to established facades. HH sends use `HHWriteGateway`; AI
preparation uses `AIReplyOrchestrator`; candidate updates use the candidate
mutation/learning boundary. Dashboard paths and JSON responses are unchanged.

`executeSync`: `inbox` terminates in `inboxrefresh.Service`. `all`,
`vacancies`, `applications`, `conversations`, and `conversation:<id>` retain
the existing read-sync compatibility path and local projection refresh.

Queue/coalescing: retained as dashboard infrastructure. It bounds concurrent
targets, coalesces duplicate inbox work, tracks async status, and does not
make business decisions.

Business algorithms remaining: none duplicated from inbox refresh, HH write,
AI, candidate learning, or scheduler owners.

# CLI

`runHHCommand`: syntax parsing, composition, typed façade invocation, and
JSON/output mapping. No direct multi-step application/chat workflow remains
in its command branches.

Profile/candidate: explicit profile bootstrap/sync/interactive commands keep
their profile-management semantics. Candidate learning proposal lifecycle is
owned by `candidatelearningorchestration` and canonical mutation boundaries.

Business algorithms remaining: no duplicated extracted workflow.

# Compatibility facades

AI: typed orchestrator construction is explicit in production. The root
orchestrator delegates employer reply, follow-up, cover-letter,
application-answer, and candidate interpretation work to typed R10/R12
services. Application-answer residual is limited to application-context
assembly, candidate-context resolution, and persistence of the resulting
clarification/draft.

HH read: `HHReadSyncService` is a typed root façade around `hhreadsync`; root
retains legacy model conversion, state persistence, and JSON/Postgres commit
composition.

HH write: `HHWriteGateway` remains the root compatibility façade, with typed
construction and all runtime mutation transport delegated through
`internal/adapters/hh/write` and the R11 gateway use case.

Candidate: candidate-learning construction and mutation translation are
explicit compatibility adapters; canonical mutation policy remains in the
candidate mutation boundary.

Career: `CareerMonitor` owns process lifecycle/state compatibility and calls
`careeriteration`; `CareerDataReconciler` remains narrow local relation repair.

# CareerDataReconciler

Classification: narrow local vacancy/application/conversation relationship
repair plus persistence projection. It has no HH read, HH write, AI, or
scheduler dependency.

Extracted: NO.

Reason: the remaining implementation is a bounded local repair operation
already isolated from runtime workflows and still coupled to the root legacy
repository interfaces and PostgreSQL transaction compatibility. It is not a
second career iteration owner. Moving it in R12.6 would require a new domain
port/model translation without reducing an actual duplicate owner.

# Application answer residual

Classification: thin root adapter around `internal/usecase/applicationanswer`.

Action: retained. It validates the question, builds the existing application
context, resolves vacancy-scoped candidate context, calls the typed leaf, and
persists only the resulting clarification or draft. No raw AI protocol or
application-answer policy remains in root.

# HHReadSyncService

Classification: persistence/read-model compatibility façade.

Persistence choreography: root selects JSON/PostgreSQL composition and keeps
the existing atomic file or PostgreSQL transaction semantics. The typed
`hhreadsync.Service` owns read-sync import policy. No generic UnitOfWork was
introduced.

# JSON/Postgres

Composition: root chooses the configured backend and builds the matching
repositories/transaction bundle.

Business branching: NONE in extracted use cases. Concrete storage adapter
imports are absent from `internal/usecase` and other domain/use-case packages.

# Dead code removed

| Symbol | Evidence | Removed |
|---|---|---|
| None | Remaining compatibility constructors and wrappers have tests or production/integration callers | No |

# Authoritative ownership

| Concern | Owner | Root role |
|---|---|---|
| Career iteration | `internal/usecase/careeriteration` | monitor façade/lifecycle |
| Employer reply | `internal/usecase/employerreplyworkflow` | AI/dashboard adapter |
| Follow-up | `internal/usecase/followuporchestration` | AI/dashboard adapter |
| Inbox refresh | `internal/usecase/inboxrefresh` | dashboard trigger/projection |
| Auto-chat | `internal/usecase/autochatorchestration` | compatibility entrypoint |
| Candidate learning | `internal/usecase/candidatelearningorchestration` | storage/model translation |
| Application preparation | `internal/usecase/applicationprocessing` | batch coordination/events |
| Application submission | `internal/usecase/applicationsubmission` + R11 gateway | legacy result mapping |
| Scheduling | `internal/platform/scheduler` | task registration |
| AI leaves | R10 typed leaf use cases | provider compatibility construction |
| HH read sync | `internal/usecase/hhreadsync` | read adapter and persistence projection |
| HH write | `internal/usecase/hhwritegateway` + `internal/adapters/hh/write` | compatibility façade |
| Delivery reconciliation | `internal/usecase/hhwritereconcile` | gateway projection/response mapping |

# Root substantial workflows

NONE.

The only retained algorithmic root boundary is the narrow local
`CareerDataReconciler` relation repair, which is intentionally classified as
local repair/projection rather than a substantial external career workflow.
`ApplyVacancies` remains a compatibility batch coordinator for ordering,
limits, counters and event projection; application policy and submission are
owned by extracted use cases.

# Raw HH write audit

Root mutation HTTP: NONE.

Runtime mutation owner: `internal/adapters/hh/write`, reached through the R11
typed gateway boundary.

# HH write call graph

- auto-apply → `applicationprocessing` → `applicationsubmission` → R11
  `hhwritegateway` → `internal/adapters/hh/write`;
- auto-chat/manual chat → R11 `hhwritegateway` → write adapter;
- manual application/test → application submission/R11 gateway;
- resume touch → R11 gateway;
- job-search status → R11 gateway.

# Retry audit

HH write retry: NONE. A later scheduler iteration is a new workflow run, not
an immediate retry of the same write action.

# AI audit

Direct root business `CompletionProvider`: NONE. Root provider assertions and
`legacyCompletionProvider` are compatibility bridges; policy and structured
AI behavior terminate in typed R10/R12 use cases.

# Candidate mutation audit

Root direct canonical mutation algorithms: NONE. Root candidate paths adapt
legacy stores and commands to the canonical mutation boundary.

# Timer audit

- business recurring loops: `internal/platform/scheduler`;
- CareerMonitor cadence: scheduler loop;
- dashboard refresh trigger: scheduler ticker plus dashboard coalescing;
- process-lock heartbeat: monitor command infrastructure;
- HH read rate/concurrency: HH read requester/adapter;
- provider/use-case retry timers: provider/use-case boundaries.

No duplicate business completion timer remains in root.

# Dependency audit

- `go list ./...`: resolves all packages;
- no internal package imports the root package;
- no extracted use case imports JSON/Postgres or HH concrete adapters;
- no extracted use case imports scheduler business behavior;
- HH write transport remains isolated under `internal/adapters/hh/write`.

# Progress

R0: approximately 41k root production LOC; `main.go` 5,183 lines.

R12.0: 30,136 root production LOC; `main.go` 3,924 lines.

R12.6: 30,961 root production LOC; 113 root production Go files;
`main.go` 3,295 lines. The LOC increase from the R12.0 recorded number is
not treated as an architectural regression; this stage prioritizes explicit
composition and ownership over line-count reduction.

Largest root production files after R12.6:

1. `main.go` — 3,295
2. `hh_write_gateway.go` — 1,446
3. `dashboard_server.go` — 1,288
4. `candidate_mutation.go` — 998
5. `hh_read_sync.go` — 909
6. `ai_reply_orchestrator.go` — 796
7. `hh_reply_pilot.go` — 778
8. `career_migration.go` — 776
9. `hh_sync_cli.go` — 764
10. `dashboard_views.go` — 738

Largest root functions by approximate source size:

1. `DashboardServer.writeAPI` — 364 lines
2. `runHHCommand` — 351 lines
3. `HHWriteGateway.Send` — 238 lines
4. `NewHHAIResponder` — 191 lines
5. `BuildCareerAuditReport` — 184 lines
6. `runInteractiveProfile` — 183 lines
7. `evaluateConversationEligibility` — 174 lines
8. `ApplyVacancies` — 152 lines
9. `HHReadSyncService.commitReadBatch` — 146 lines
10. `CareerDataReconciler.reconcileContext` — 145 lines

# Compatibility debt remaining

- legacy variadic constructors remain for source compatibility and tests;
  production composition uses typed options for the audited boundaries;
- root HH response/request models remain until the post-R12 executable
  composition migration;
- local JSON/Postgres transaction/projection glue remains in root;
- dashboard write dispatch and non-inbox sync compatibility remain root
  adapters;
- `CareerDataReconciler` remains a narrow local repair boundary.

No raw transport, duplicated AI policy, duplicated candidate mutation policy,
or duplicated scheduler workflow is counted as remaining debt.

# Tests

Characterization and boundary tests remain for scheduler, career iteration,
employer reply, follow-up, inbox refresh, auto-chat, candidate learning,
application processing/submission, R10 leaves, R11 gateway/preflight/
reconcile/write adapter, dashboard, CLI, CareerMonitor, ApplyVacancies,
AutoRespondChats, candidate learning and HH read sync.

# Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED — `docker build -t hh-ai-responder:r12-6 .` could not connect
to `unix:///var/run/docker.sock` because the Docker daemon is unavailable.

LIVE HH WRITES: 0

# R12 status

R12.6: COMPLETE

# Ready

READY FOR R12 FINAL CLOSURE AUDIT

Do not begin the final closure audit in this stage.
