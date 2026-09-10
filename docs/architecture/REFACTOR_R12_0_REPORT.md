# R12.0 — Career / Runtime Orchestration Audit

## Executive summary

Status: **AUDIT COMPLETE. R12.0 is an audit-only stage. No production code was changed and no live HH write was performed.**

The R0 monolith has materially decomposed. The repository now exposes 42 Go
packages, including typed R1–R11 usecases, ports, and adapters. The root
`package main` is still the runtime composition and compatibility boundary,
but it is no longer the owner of the generic AI, HH write transport, or most
leaf business algorithms.

The remaining R12 work is orchestration work, not another generic extraction:

- recurring runtime loops still mix scheduler cadence with business workflows;
- `CareerMonitor`, dashboard sync/projection, and reconciliation each perform
  multi-store local choreography in the root package;
- legacy vacancy application and auto-chat remain substantial unattended
  workflows in root code;
- candidate acquisition still composes clarification, interpretation, proposal,
  and mutation in root code;
- CLI and dashboard handlers remain thick composition/orchestration edges;
- JSON/Postgres selection and persistence choreography are intentionally
  compatible but split across root facades and adapters.

No critical architecture blocker was found in the existing HH write boundary:
all observed live mutation paths terminate in the typed write capability and
R11 gateway/adapter safety rules. The high-risk workflows themselves remain
high risk because they can initiate unattended external actions.

Recommended next order:

1. R12.1 — characterize and isolate one read-only Career monitor iteration;
2. R12.2 — isolate conversation/inbox/follow-up orchestration;
3. R12.3 — isolate candidate-learning orchestration;
4. R12.4 — isolate the vacancy/application pipeline;
5. R12.5 — make recurring schedulers invoke explicit iteration usecases;
6. R12.6 — reduce residual CLI/dashboard/composition and compatibility debt.

This report deliberately does not implement any of those stages.

## Progress since R0

The supplied R0 baseline was one root package with approximately 84
production Go files, approximately 41k production LOC, a 5,183-line
`main.go`, and a broad root-level business surface. Current inventory:

| Measure | R0 baseline | R12.0 observed state |
|---|---:|---:|
| Go packages | 1 root package | 42 packages from `go list ./...` |
| Root production Go files | approximately 84 | 103 |
| Root production LOC | approximately 41k | 30,136 |
| All production Go LOC | not separately recorded | 54,376 |
| `main.go` | 5,183 lines | 3,924 lines |
| Root production functions | monolith count was used at R0 | 1,225 across current root files |
| Root production type declarations | monolith count was used at R0 | 483 across current root files |

The function/type comparison is not apples-to-apples: current root counts are
package-wide after splitting files, while R0 described the monolithic file.
The meaningful change is ownership: R1–R11 introduced importable usecases,
ports, and adapters with no reverse dependency on the root package.

Pre-audit working-tree state was preserved. The tree already contained a large
uncommitted R1–R11 change set and staged documentation renames:

- `git diff --stat`: 85 files, 3,468 insertions and 14,821 deletions;
- `git diff --cached --stat`: 14 documentation renames;
- a tracked `hh-ai-responder.test` binary was already deleted;
- multiple prior R reports and compatibility files were already untracked.

These pre-existing changes are not attributed to R12.0. The only intended R12
artifact is this report.

## Runtime entry points

The primary runtime entry point is `main` → `run` → `executeInvocation` in
`main.go`. The command families are:

| Entry point | Primary classification | Substantial root workflow |
|---|---|---:|
| `main` / `run` / `executeInvocation` | CLI ADAPTER / COMPOSITION | NO |
| `runDefault` → `HHAIResponder.Run` | COMPOSITION → ORCHESTRATION | YES, delegated |
| `runHHCommand` | CLI ADAPTER | YES, mixed adapter/composition |
| `runMonitorCommand` | CLI ADAPTER / COMPOSITION | NO; starts monitor |
| `runReconcileCommand` | CLI ADAPTER / COMPOSITION | NO; calls reconciler |
| `runDashboardCommand` | CLI ADAPTER / COMPOSITION | NO; starts server |
| `loadDashboard` | COMPOSITION | NO |
| `DashboardServer.ServeHTTP` | DASHBOARD HANDLER | NO; routing/security shell |
| `DashboardServer.readAPI` | DASHBOARD HANDLER | NO; read projection |
| `DashboardServer.writeAPI` | DASHBOARD HANDLER | YES |
| `CareerMonitor.Run` | SCHEDULER / ORCHESTRATION | YES, mixed |
| `CareerMonitor.RunOnce` | ORCHESTRATION | YES |
| `CareerDataReconciler.ReconcileContext` | ORCHESTRATION / PERSISTENCE | YES |
| `HHReadSyncService.SyncAll` | ORCHESTRATION | YES |
| `HHReadSyncService.commitReadBatch` | PERSISTENCE / ORCHESTRATION | YES |
| `DashboardServer.executeSync` | ORCHESTRATION | YES |
| `HHAIResponder.ApplyVacancies` | ORCHESTRATION | YES |
| `HHAIResponder.AutoRespondChats` | ORCHESTRATION | YES |
| `AIReplyOrchestrator.PrepareEmployerReply` | ORCHESTRATION | YES |
| `AIReplyOrchestrator.PrepareFollowUp` | ORCHESTRATION | YES |
| `CandidateKnowledgeAcquisitionService` workflow | ORCHESTRATION | YES |

The exact classification vocabulary used in this audit is:
`DOMAIN`, `USECASE`, `ORCHESTRATION`, `SCHEDULER`, `COMPOSITION`, `ADAPTER`,
`PERSISTENCE`, `DASHBOARD HANDLER`, `CLI ADAPTER`, `COMPATIBILITY`,
`TEST ONLY`, and `DEAD`.

## Periodic/scheduled work

| Scheduler | Trigger | Cadence | Iteration | HH write |
|---|---|---:|---|---:|
| `CareerMonitor.Run` | CLI `monitor` startup | default 15m | `RunOnce` | No |
| Dashboard background refresh | dashboard startup when enabled | `MonitorInterval`, default 15m | `queueBackgroundInboxRefresh` → `executeSync("inbox")` | No |
| `HHAIResponder` auto-touch loop | `AutoTouch` at default runtime | 4h after completion | `TouchResume` | Yes-capable |
| `HHAIResponder` job-status loop | `AutoJobStatus` | 24h after completion | `SetActiveJobSearchStatus` | Yes-capable |
| `HHAIResponder` auto-apply loop | `AutoApply` | 12h in code; nearby comment says 24h | `ApplyVacancies` | Yes-capable |
| `HHAIResponder` auto-chat loop | `AutoChat` and chat mode | 15m after completion | `AutoRespondChats` | Yes-capable |
| monitor process-lock heartbeat | monitor command startup | 1m | lock liveness only | No |
| HH read request scheduler | read adapter construction | rate-limit/concurrency control | GET/HEAD transport admission | No |

The main loops are completion-based rather than fixed wall-clock schedules:
an iteration error is logged and the next timer is still armed. There is no
business scheduler package and no production `time.Sleep`; pagination loops and
LLM/provider retries are not schedulers.

The auto-apply comment/code cadence mismatch is a compatibility/documentation
finding, not changed in R12.0. Any future scheduler extraction must preserve
actual cadence unless an explicit behavior change is approved.

## Substantial workflow inventory

| Workflow/symbol | Classification | Root workflow? | External effects | Decision |
|---|---|---:|---|---|
| `ApplyVacancies` | ORCHESTRATION | YES | HH reads, AI, possible HH application/test write | R12.4 candidate |
| `AutoRespondChats` | ORCHESTRATION | YES | HH reads, AI, possible chat send/leave | R12.2 candidate; preserve separate legacy semantics |
| `CareerMonitor.RunOnce` | ORCHESTRATION | YES | HH reads; local state/notification writes | R12.1 first |
| `CareerMonitor.Run` | SCHEDULER | YES, mixed | cadence around monitor iteration | R12.5 after iteration boundary |
| `CareerDataReconciler.ReconcileContext` | ORCHESTRATION | YES | local relation/projection persistence | narrow repair usecase candidate |
| `HHReadSyncService.SyncAll` | ORCHESTRATION | YES | HH reads and local persistence | sync facade remains root compatibility |
| `commitReadBatch` | PERSISTENCE | YES | JSON staging or Postgres transaction | repository boundary candidate |
| `executeSync` | ORCHESTRATION | YES | HH read, local projections, AI drafts, notifications | R12.2 candidate |
| `writeAPI` | DASHBOARD HANDLER | YES | local mutation, AI draft, approved HH gateway action | thin handler candidate |
| `AIReplyOrchestrator` preparation methods | ORCHESTRATION | YES | AI plus draft/clarification persistence | split by workflow, not generic service |
| candidate acquisition service | ORCHESTRATION | YES | candidate clarification/proposal/mutation | R12.3 candidate |
| `FollowUpEngine.Evaluate` | DOMAIN | NO | none | already narrow/pure |
| `VacancyAnalyzer` and `vacancyDecision` | DOMAIN | NO | none | deterministic authority remains |
| `employerreply.Service` | USECASE | NO | proposed result only | extracted leaf |
| `autochatreply.Service` | USECASE | NO | proposed result only | extracted leaf |
| `hhwritegateway.Service` | USECASE | NO | one controlled HH mutation | R11 safety boundary |
| `internal/adapters/hh/read` | ADAPTER | NO | read transport only | extracted |
| `internal/adapters/hh/write` | ADAPTER | NO | write transport only | extracted |
| `internal/adapters/storage/json` | PERSISTENCE | NO | local file persistence | extracted |
| `internal/adapters/storage/postgres` | PERSISTENCE | NO | database persistence | extracted |
| root `AIClient` compatibility methods | COMPATIBILITY | NO | provider delegation | retained source compatibility |
| root request/provider fixtures | TEST ONLY | NO | none | no runtime ownership |

No current production symbol in the audited live paths was classified `DEAD`
solely because a direct caller was not found. Callable compatibility workflows
remain live until an explicit removal decision and regression evidence exist.

## CareerMonitor

`CareerMonitor` in `career_monitor.go` is a durable local monitor with a
read-only HH boundary. Its constructor receives `HHReadSyncService`,
`CareerDataReconciler`, notification engine, application/conversation stores,
state path, interval, and quiet-hours configuration.

`RunOnce` currently performs this sequence:

1. validate sync/reconciler/notification dependencies;
2. call `RefreshInbox` for lightweight HH read synchronization;
3. record sync result and, after repeated failures, create a local sync
   problem notification;
4. call `Reconcile(false)` for local relation/projection repair;
5. resolve conversation states and persist local state;
6. load vacancies, applications, conversations, timelines, and clarifications;
7. build a `CareerSnapshot` and calculate local notifications;
8. save monitor state and notification state.

`Run` owns the infinite timer loop and swallows ordinary iteration errors until
the next cadence. `RunOnce` is the useful future application boundary;
`Run` should eventually be a scheduler wrapper that calls it.

Safety: no HH mutation is reachable from this monitor. The monitor's local
state/notification saves are still side effects and need explicit error
visibility. Several snapshot reads currently ignore errors, which can make an
incomplete snapshot look successful after the sync step succeeds.

Recommended R12.1 contract: a detached, testable `CareerIteration` result
containing sync evidence, reconciliation outcome, resolved state changes,
snapshot, and notification projection. Keep cadence, quiet hours, and state
file compatibility at the root until characterization tests cover behavior.

## CareerDataReconciler

`CareerDataReconciler` in `reconciliation.go` is local relation repair, not HH
delivery reconciliation. It:

- lists applications and conversations;
- normalizes vacancy completeness;
- links conversations to applications by strong identifiers;
- creates partial applications from negotiation identifiers;
- enriches vacancies from applications;
- synchronizes partial/completeness flags;
- optionally backfills deterministic match data through `VacancyAnalyzer`;
- appends idempotent local events;
- saves local stores when not dry-run.

For matching Postgres repositories and non-dry-run operation,
`ReconcileContext` wraps the repair in a `CareerTx`. JSON mode performs
multiple file-store operations and is not one cross-file transaction.

Classification: `Reconcile` is a compatibility facade; `ReconcileContext` is
`ORCHESTRATION`; store operations and transaction selection are `PERSISTENCE`.
The future owner should be a narrow relation-repair usecase/package, not
`hhwritereconcile`, whose purpose is post-attempt HH delivery evidence.

## Vacancy lifecycle

The observed lifecycle is:

`HH search read → deterministic filters → description read → deterministic
salary/exclusion checks → AI analysis → deterministic authoritative decision →
fresh read-only preflight → cover-letter/test preparation → typed HH gateway →
local result/event projection`.

`HHAIResponder.ApplyVacancies` owns the complete legacy automatic pipeline.
It loads already-responded state, applies user limits and archive/response
filters, reads vacancy detail, builds canonical candidate context, calls typed
`vacancyanalysis.Service` through the compatibility surface, and derives the
final decision in Go. Unknown hard requirements create candidate questions or
proposals; AI cannot convert unknown into a safe match.

Cover letters use typed `coverletter.Service`. Tests use typed `testanswer`
with strict complete-answer and fresh-metadata validation. The atomic test
submission remains one vacancy-response mutation. Dry-run and write-enabled
gates remain in the write path; this audit performed zero such writes.

The important R12 finding is ownership, not a safety bypass: `ApplyVacancies`
is still a substantial unattended root workflow and is the highest-risk
candidate for an explicit application-processing boundary.

## Application lifecycle

Applications are primarily imported/projection data. `hhreadsync` maps HH
applications, preserves local match/notes/follow-up state, imports partial
vacancies where needed, and links conversations. `CareerDataReconciler` repairs
relations and may create a partial application from a negotiation identifier.

The automatic application pipeline does not create a local application before
the HH response; confirmed application metadata is subsequently imported and
projected. Duplicate control therefore uses HH/preflight evidence,
`already_responded_state`, external vacancy/application identity, and the
existing write limits.

`SendResponse`, `ApplyVacancy`, and `ApplyVacancyWithTest` are root
`COMPATIBILITY`/adapter methods. The substantial orchestration is
`ApplyVacancies`, not those mechanical capability facades. Manual controlled
actions use the R11 approval/preflight/gateway boundary. Automatic vacancy
submission remains a separate legacy path and must not be silently merged with
manual approved chat actions.

## Conversation lifecycle

The normal controlled conversation path is:

`HH read/import → relation/state projection → conversation context → policy and
candidate-context resolution → typed employer-reply/follow-up/application
answer usecase → local draft or clarification → explicit approval → fresh
preflight → one-shot HH write gateway → targeted delivery readback → local
projection`.

`AIReplyOrchestrator.PrepareEmployerReply` is still root orchestration around
the extracted `employerreply.Service`. It loads context, delegates generation,
and persists either a draft or candidate clarification. It owns no raw prompt
algorithm or HH transport.

`AutoRespondChats` is a distinct legacy workflow. It filters awaiting chats,
reads bounded live history, enforces state/history gates, calls
`autochatreply.Service`, and may send or leave without the AIDraft approval
path. It remains write-gated, dry-run-aware, and routed through the typed HH
writer, but is an unattended high-risk workflow. Its semantics should remain
separate from controlled employer reply during extraction.

## Follow-up lifecycle

`FollowUpEngine.Evaluate` is deterministic domain policy. It checks timing,
history, limits, candidate reply, terminal states, dismissal, and pending
clarifications.

`AIReplyOrchestrator.PrepareFollowUp` loads current application/conversation
state, evaluates eligibility, reuses a fresh matching generated draft by
`followUpFingerprint`, calls `followupdraft.Service`, and persists a draft or
clarification. It never sends. A fresh input is read before persistence.

Dashboard follow-up actions create/dismiss local artifacts. A send is only
possible through an explicit approved action and the HH write gateway, which
records follow-up state/events after the attempt. No independent automatic
follow-up sender or cadence was found.

Future ownership should be a narrow `followuporchestration` boundary, not a
combined conversation god-service.

## Candidate acquisition lifecycle

`CandidateKnowledgeAcquisitionService` remains a substantial root workflow.
It composes:

`gap detection → deterministic clarification request → typed interpretation →
candidate answer validation → proposal/choice → explicit mutation policy →
clarification/proposal persistence`.

The typed packages are appropriately narrower:

- `candidateacquisition` owns deterministic gap/filter/classification policy;
- `candidateinterpretation` owns structured AI interpretation only;
- `candidatemutation` owns policy and atomic candidate mutation;
- JSON/Postgres adapters own persistence.

The root service creates clarification requests, submits answers, manages
proposal confirmation/rejection/dismissal, and bridges legacy stores. Dashboard
knowledge routes call it or the mutation boundary. CLI currently exposes
proposal listing/confirmation/rejection and profile synchronization, not the
full answer workflow.

Candidate mutation is a high-risk local truth change. The workflow correctly
preserves unknown/proposal states rather than inventing facts. No automatic
candidate-mutation-to-semantic-reindex workflow was observed; semantic
reindex is an explicit CLI operation. R12.3 must not silently add that side
effect.

## Notifications

Notifications are local-only. `CandidateNotificationEngine.Calculate` derives
notifications from a career snapshot; `NotificationStore` persists them;
dashboard projections and `CareerMonitor` expose lifecycle state. Delivery is
currently JSON/dashboard/console-facing, with no Telegram, email, or other
external notification adapter.

Delivery-uncertain HH events are converted into local critical notifications;
there is no resend loop. Fingerprints/cooldowns and lifecycle status prevent
ordinary duplicate presentation. Some notification-save and projection-read
errors are ignored by monitor/dashboard paths; this is a reliability debt,
not a write-safety bypass.

Classification: engine is `ORCHESTRATION`/local business projection, store is
`PERSISTENCE`, dashboard rendering is `DASHBOARD HANDLER`.

## Dashboard audit

`ServeHTTP` provides loopback/origin/content-type/CSRF-style request checks and
routes GET/POST operations. `readAPI` is mostly a local read-model projection.
The `/notifications` read currently refreshes and may save local notification
state, so it is not strictly side-effect-free despite being an HTTP GET.

`writeAPI` is a 359-line dashboard handler and a clear R12 hotspot. It parses
requests and directly sequences:

- notification lifecycle changes;
- follow-up draft/dismiss operations;
- employer-reply draft generation and persistence;
- draft edit/reject/approve/feedback;
- action preflight/reconcile/send/cancel;
- clarification answers and candidate knowledge mutations.

It does not construct raw HH requests or bypass the gateway. The issue is
thick orchestration in a handler, which makes response/error semantics and
local transaction choreography harder to test.

`executeSync` is a separate substantial workflow: HH read sync, reload local
files, inbox projection, follow-up/workflow refresh, notification refresh,
daily-state persistence, and background draft scheduling. It is a first-class
R12.2 extraction candidate.

`startBackgroundInboxRefresh` adds a ticker and coalescing queue. The ticker
should eventually call an explicit iteration service and remain ignorant of
business steps.

## CLI audit

`internal/cli.Parse` is syntax/routing only and is correctly classified
`CLI ADAPTER`. Root command handlers are thicker:

- `runHHCommand` mixes command dispatch, read sync, local projections, draft
  preparation, and compatibility output;
- `runMonitorCommand` is composition plus process-lock lifecycle and starts
  `CareerMonitor` in read-only configuration;
- `runReconcileCommand` is composition around `CareerDataReconciler`;
- `runDashboardCommand` is server startup/composition;
- candidate/profile commands bridge knowledge and semantic storage.

`runHHCommand` is the largest CLI hotspot, but it is not the first extraction
target because the underlying career/conversation iterations should be made
explicit first. CLI compatibility flags and JSON output must remain stable.

## Root persistence choreography

JSON sync choreography in `HHReadSyncService.commitReadBatch` clones/stages
vacancy/application/conversation stores, acquires ordered file locks, imports
typed batches, performs deterministic derived matching, writes atomic files,
and adopts compatibility mirrors. It protects against staged-file conflicts,
but it is not one cross-store transaction.

Postgres sync choreography in `commitPostgresReadBatch` performs the career
triple import inside one database transaction. Candidate knowledge storage is
separately composed and may have its own transaction boundary.

The gateway choreography is distinct and established by R11:

`approval/action state → fresh preflight → durable nonce reservation → one HH
transport attempt → accepted/ambiguous evidence → local action/audit/message/
follow-up projection → targeted delivery reconciliation`.

R12 should not mix HH transport choreography with local career projection
choreography. The latter needs narrower repository/usecase boundaries while
preserving JSON compatibility and Postgres transaction behavior.

## JSON/Postgres branching

`BuildCareerRepositories` selects a complete JSON or complete Postgres set of
career repositories. Typed usecases depend on ports, not concrete backends.
The root still owns backend selection, compatibility mirrors, and some
Postgres type assertions needed to open `CareerTx`.

Assessment: the architecture direction is healthy; the duplication is
composition/persistence debt rather than duplicated business policy. A future
repository unit-of-work port could reduce root assertions, but it must not
weaken JSON atomic-file guarantees or silently change transaction scope.

## Retry audit

| Layer | Observed policy | Finding |
|---|---|---|
| HH writes | no transport retry in write adapter/gateway | REQUIRED safety invariant preserved |
| HH write next cadence | main loops retry a failed task on a later new iteration | new workflow invocation, not replay of the same approved action |
| controlled chat identity | durable action ID + one-shot `SendNonce` | replay rejected; new approval creates independent action/nonce |
| HH reads | GET 429 retries up to two additional attempts with backoff/`Retry-After` | safe-read retry is adapter-owned |
| LLM transport | OpenAI adapter attempts configured provider requests | provider boundary |
| LLM semantic | typed usecases retry malformed/invalid business output | usecase-owned and bounded |
| legacy auto-chat | one business generation attempt; provider retry only | preserves legacy semantics |
| notifications | no delivery retry | local recalculation/fingerprint only |
| dashboard sync | coalescing/singleflight, not retry | failures surface per operation and next refresh may run |
| monitor | error recorded/logged, next cadence continues | no same-action HH replay |

No critical same-approved-action HH write retry was found. The remaining risk
is operational: unattended loops will attempt later new work after an error,
so future workflow identity and per-run audit must remain explicit.

## Side-effect matrix

| Workflow | HH read | AI | Local write | HH write | Approval |
|---|---:|---:|---:|---:|---:|
| `CareerMonitor.RunOnce` | yes | no | yes | no | n/a |
| `CareerDataReconciler` | no | no | yes | no | n/a |
| `HHReadSyncService` | yes | no | yes | no | n/a |
| dashboard `executeSync` | yes | optional draft AI | yes | no | n/a |
| controlled employer reply | targeted/read context | yes | draft/clarification | only after gateway send | yes |
| follow-up preparation | local/read projection | yes | draft/clarification | no | later send requires yes |
| candidate acquisition | local context | optional interpretation | candidate truth/proposal | no | explicit candidate confirmation for mutation paths |
| `ApplyVacancies` | yes | yes | events/state | yes-capable | legacy automatic path, no AIDraft approval |
| `AutoRespondChats` | yes | yes | events/state | yes-capable send/leave | legacy automatic path |
| manual HH action gateway | fresh read | no | audit/projection | yes | explicit approved action |
| notification projection | local | no | notification store | no | n/a |

`HH_DRY_RUN=true` must continue to block every HH write row, including legacy
auto-apply, auto-chat, touch, and job-status loops.

## Risk matrix

| Area | Risk | Level | Reason |
|---|---|---:|---|
| `ApplyVacancies` | unattended application/test mutation | HIGH | candidate truth, AI letter/test, and external submission in one root workflow |
| `AutoRespondChats` | unattended chat send/leave | HIGH | AI-generated external communication and discard side effect |
| recurring loops invoking writes | repeated unattended external actions | HIGH | cadence continues after ordinary errors |
| manual gateway send | external mutation | HIGH | explicit approval and fresh preflight reduce trigger risk but do not remove effect |
| candidate acquisition | candidate-truth mutation | HIGH | interpretation/proposal/mutation can alter future employer-safe claims |
| `CareerMonitor` | incomplete local audit | MEDIUM | HH reads and local writes; ignored snapshot errors |
| dashboard `executeSync` | projection/draft side effects | MEDIUM | background AI/local writes after read sync |
| `CareerDataReconciler` | cross-store consistency | MEDIUM | JSON lacks one cross-file transaction |
| `HHReadSyncService` | import consistency | MEDIUM | JSON staging is careful but branch-specific |
| `AIReplyOrchestrator` | stale or duplicated local drafts | MEDIUM | fingerprints exist; persistence remains root |
| follow-up preparation | stale eligibility/draft | MEDIUM | fresh input/fingerprint checks exist; send is separate |
| typed pure analyzers | wrong classification | LOW | deterministic/no external mutation |
| typed AI leaf usecases | invalid generated result | LOW/MEDIUM | bounded validation/retry and no direct side effect |
| adapters/ports | boundary regression | LOW | current dependency direction is clean |

## Remaining root hotspots

Current largest root production files include `main.go` (3,924 LOC),
`hh_write_gateway.go` (1,438), `dashboard_server.go` (1,271),
`candidate_mutation.go` (998), `hh_read_sync.go` (864),
`ai_reply_orchestrator.go` (793), `hh_reply_pilot.go` (778),
`career_migration.go` (776), and `hh_sync_cli.go` (757).

Largest approximate root functions include:

| Function | Approx. lines | Classification |
|---|---:|---|
| `ApplyVacancies` | 405 | ORCHESTRATION |
| `DashboardServer.writeAPI` | 359 | DASHBOARD HANDLER |
| `runHHCommand` | 347 | CLI ADAPTER |
| `HHWriteGateway.Send` | 237 | COMPATIBILITY / ORCHESTRATION |
| `DashboardServer.analytics` | 221 | DASHBOARD HANDLER projection |
| `NewHHAIResponder` | 190 | COMPOSITION |
| `BuildCareerAuditReport` | 183 | ORCHESTRATION/report projection |
| `runInteractiveProfile` | 182 | CLI ADAPTER |
| `runProfileBootstrap` | 179 | CLI ADAPTER / COMPOSITION |
| `evaluateConversationEligibility` | 173 | USECASE/DOMAIN boundary |
| `LoadProfileData` | 154 | COMPOSITION / persistence glue |
| `commitReadBatch` | 145 | PERSISTENCE |
| `reconcileContext` | 144 | ORCHESTRATION / PERSISTENCE |

The principal architectural hotspot is not file size alone: it is the root
sequence that combines trigger, context loading, local state, AI result, and
side-effect policy.

## Compatibility debt

The following compatibility debt is intentional and must be reduced
incrementally:

- root `HHReadSyncService` facade translating legacy stores to typed
  `hhreadsync` ports;
- root AI compatibility facades retained for source compatibility;
- root write compatibility wrappers terminating at the typed R11 gateway;
- dynamic/legacy constructor shapes in composition code;
- duplicate root local projections for JSON and Postgres modes;
- root type assertions for Postgres transaction selection;
- CLI/dashboard handlers that directly sequence multiple usecases;
- legacy automatic apply/auto-chat workflows outside the controlled draft
  approval model;
- root career snapshot/state/notification composition spread across monitor,
  dashboard, audit, and sync code.

These are not evidence of a broken port boundary. They are the remaining
runtime-orchestration and composition surface identified for R12.

## Current architecture graph

```text
CLI commands ───────────────┐
Dashboard HTTP/ticker ──────┤
CareerMonitor timer ────────┤
HHAIResponder task timers ──┘
              ↓
root package main
  orchestration + compatibility + composition + local persistence choreography
  ApplyVacancies / AutoRespondChats
  CareerMonitor / Dashboard.executeSync / Dashboard.writeAPI
  AIReplyOrchestrator / CandidateKnowledgeAcquisitionService
  HHReadSyncService / CareerDataReconciler
              ↓
typed domain + R1–R11 usecases
  application, candidate, conversation, vacancy
  hhreadsync, vacancyanalysis, employerreply, autochatreply,
  applicationanswer, coverletter, followupdraft, candidate acquisition,
  writeapproval, hhwritegateway, hhwritepreflight, hhwritereconcile
              ↓ ports
internal/ports/*
              ↓ adapters
HH read | HH write | LLM | JSON | Postgres | platform
```

Safety graph:

```text
AI workflow → typed usecase → CompletionProvider → LLM adapter
read workflow → hhreadsync/read ports → HH read adapter
manual write → approval → fresh preflight → hhwritegateway → write ports → adapter
legacy auto write → root compatibility gate → typed write gateway/adapter
```

Observed raw HH mutation bypasses: **NONE**.

## Recommended target graph

```text
CLI / Dashboard / recurring scheduler
              ↓
narrow iteration/orchestration usecases
              ↓
existing domain + R1–R11 leaf usecases
              ↓
consumer-owned ports / repository contracts
              ↓
HH read/write, LLM, JSON/Postgres adapters
```

The root package should retain configuration, startup, legacy API facades,
and composition only. Recommended narrow future packages are:

- `careeriteration`: one read-only monitor iteration;
- `conversationworkflow`: context → policy → draft/clarification → persist;
- `followuporchestration`: eligibility scan and draft persistence;
- `candidatelearningorchestration`: gap → interpretation → proposal/mutation;
- `applicationprocessing`: vacancy evaluation/preparation/submission;
- a scheduler wrapper only after iterations are explicit.

Do not create a broad `internal/career` or `CareerService` now. “Career” in
the current code spans projections, reconciliation, notifications, cadence,
and multiple independent workflows; a god package would reproduce the root
problem under a new name.

## Proposed R12 stages

### R12.1 — Career iteration boundary

Characterize `CareerMonitor.RunOnce`, extract a read-only iteration contract,
and leave `Run`/cadence in the root temporarily. Preserve state paths, quiet
hours, notification fingerprints, and all error behavior until tests describe
them. No HH write.

### R12.2 — Conversation/inbox orchestration

Separate dashboard post-sync projection, inbox draft scheduling, employer-reply
draft persistence, and follow-up preparation from HTTP handlers. Keep
`employerreply` and `autochatreply` as separate leaf semantics. No change to
approval or HH write gateway.

### R12.3 — Candidate-learning orchestration

Extract the root acquisition sequence around existing typed gap,
interpretation, and mutation services. Preserve proposal/confirmation gates.
Do not add automatic semantic reindex without an explicit contract and tests.

### R12.4 — Vacancy/application pipeline

Split `ApplyVacancies` into deterministic candidate selection, analysis,
preparation, test validation, and submission orchestration. Preserve legacy
automatic behavior and all existing dry-run/write gates. Treat this as a
high-risk staged extraction with no automatic retry of an attempted write.

### R12.5 — Scheduler boundary

Make recurring loops call explicit iteration functions. Keep cadence values,
completion-based timer semantics, process locks, coalescing, and config/flag
compatibility unchanged. Scheduler code must not own business policy.

### R12.6 — Residual root/composition cleanup

Thin `runHHCommand`, dashboard handlers, compatibility facades, and
JSON/Postgres selection only after the workflow boundaries are proven. Remove
dead compatibility surfaces only with an explicit compatibility decision.

## Blockers

No critical blocker prevents planning the next stage.

Open risks/gates before implementation:

1. Add characterization tests for `CareerMonitor.RunOnce` error and partial
   snapshot behavior; ignored read errors are currently an observability risk.
2. Decide whether the actual 12-hour auto-apply cadence or the nearby 24-hour
   comment is the compatibility contract; do not infer a change.
3. Define whether local JSON career reconciliation requires a multi-store
   recovery journal or whether current staged atomic files are sufficient.
4. Preserve legacy auto-chat's one-generation-attempt semantics separately from
   controlled employer reply.
5. Decide whether candidate semantic reindex is intentionally explicit or must
   become part of candidate learning; current code does not establish an
   automatic invariant.
6. Keep `HH_DRY_RUN=true` and zero-live-write tests around every future root
   extraction.

## Verification

Pre-audit inventory completed:

- `git status --short` recorded and existing changes preserved;
- `git diff` and `git diff --cached` inspected;
- `go list ./...` completed successfully;
- root/all production file and LOC inventory collected;
- largest root files/functions inventoried;
- scheduler, workflow, retry, identity, persistence, dashboard, CLI, and
  import-direction searches completed.

Final verification results:

| Command | Result |
|---|---|
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `docker build -t hh-ai-responder:r12-audit .` | NOT RUN TO COMPLETION: Docker CLI exists, but the daemon is unavailable (`Cannot connect to the Docker daemon at unix:///var/run/docker.sock`). |

All executed commands were read/build/test-only; no runtime command with live
HH credentials was part of R12.0.

## Ready

**R12.0 is ready for review.**

The report is the only R12.0 artifact. No production code, configuration
default, scheduler cadence, HH transport, or live external state was changed.
The next action, if approved, is R12.1 characterization/extraction planning;
this stage intentionally stops here.
