# Executive summary

R13: **COMPLETE**

The composition-root migration is closed: the only production entrypoint is
`cmd/hh-ai-responder`, the root package is gone, the production graph is
singular, and the five dynamic constructor paths are gone. Docker support was
explicitly removed from the project; the stale `Dockerfile` was deleted rather
than repaired, and the app-only Compose configuration was removed. Docker is
therefore no longer part of the supported production/build contract or R13
acceptance. No production-code changes were made and no live HH writes
occurred.

# Stage status

R12.Final: COMPLETE (prior stage report; no ownership regression found)

R13.1: COMPLETE

R13.2a: COMPLETE

R13.2b: COMPLETE

R13.3: COMPLETE

R13.Final: COMPLETE — Docker support was removed by explicit project decision;
all remaining audited R13 criteria passed.

# Canonical executable

Path: `cmd/hh-ai-responder/main.go`

Canonical entrypoint count: **1** production `package main` / `func main`.

Root executable: **NONE**. Root production Go files: **0**. The only other
`package main` declaration is the executable's test package.

# Composition graph

```text
cmd/hh-ai-responder/main.go
        |
        v
internal/bootstrap.Run
        |
        v
internal/runtime.NewHandlers
        |
        v
internal/runtime command adapters and NewHHAIResponder graph
        |
        v
R10/R11/R12 typed usecases and platform services
        |
        v
domain + ports
        |
        v
adapters
```

## Package direction

cmd -> root: **NONE**

bootstrap -> root: **NONE**

runtime -> root: **NONE**

internal -> root: **NONE**

The only production import of `internal/runtime` is from the canonical cmd
package. The runtime test import is test-only. No reverse import from domain,
ports, usecases, or adapters into runtime was found.

## Runtime graph

Independent production graphs: **1**. `bootstrap.Run` and
`runtime.NewHandlers` have one production caller/graph. Per-command calls to
`NewHHAIResponder` are command adapters inside that graph, not independent
executables or startup algorithms. No second scheduler, dashboard, or
handler composition graph was found.

## Runtime ownership

| Concern | Authoritative owner | Runtime role | Duplicate |
| --- | --- | --- | --- |
| Process setup, parse, config, signals, exit mapping | `internal/bootstrap` | Delegated from cmd | None |
| Composition and legacy translation | `internal/runtime` | Compatibility/composition | None |
| Career iteration | `usecase/careeriteration` | Typed adapter | None |
| Employer reply | `usecase/employerreply` | Typed AI façade | None |
| Controlled employer workflow | `usecase/employerreplyworkflow` | Context/store adapter | None |
| Follow-up | `usecase/followupdraft`, `usecase/followuporchestration` | Context/store adapter | None |
| Inbox refresh | `usecase/inboxrefresh` | HH/local projection adapter | None |
| Legacy auto-chat | `usecase/autochatreply`, `usecase/autochatorchestration` | Legacy source/action adapter | None |
| Candidate learning | `usecase/candidateinterpretation`, `candidatelearningorchestration` | Candidate-store adapter | None |
| Application processing | `usecase/applicationprocessing` | Legacy input/output adapter | None |
| Application submission | `usecase/applicationsubmission` | Write executor adapter | None |
| HH write policy/gateway | `usecase/hhwritegateway` | Gateway compatibility façade | None |
| Timer primitive | `internal/platform/scheduler` | Registration only | None |
| Dashboard | `internal/runtime` dashboard adapter | HTTP/API compatibility | None |
| Persistence | JSON/Postgres adapters | Runtime composition | None |

Substantial authoritative business workflows in runtime: **NONE**. The large
runtime package remains composition, CLI, dashboard, persistence, and
compatibility debt rather than an R13 ownership blocker.

# Constructor audit

Typed production construction:

- `NewAIReplyOrchestratorWithOptions`;
- `NewHHReadSyncServiceWithOptions` and the explicit repository variant;
- `NewHHWriteGatewayWithOptions`;
- `NewApplicationStoreWithDependencies` plus the path-only store constructor;
- `NewCareerDataReconcilerWithRepositories`.

Dynamic dependency-discovery constructors: **NONE**. The five removed
`...any` constructor entrypoints have no production references. Remaining
`any`/`interface{}` uses are DTO/protocol decoding, compatibility values, or
explicit interface parameters; none infer or discover a dependency graph.

Remaining typed variadics:

- `NewVacancyStore(...string)` — typed path convenience;
- `NewNotificationStore(...string)` — typed path convenience;
- `NewConversationContextBuilder(...CandidateSemanticRetriever)` — typed
  optional capability;
- `NewApplicationContextBuilder(...CandidateSemanticRetriever)` — typed
  optional capability.

Dynamic dependency discovery: **0**.

# R10 AI safety

Direct business bypass in cmd: **NONE**.

Direct business bypass in bootstrap: **NONE**.

Direct business bypass in runtime: **NONE**. Runtime's
`legacyCompletionProvider`, model-name checks, and façade calls are
compatibility bridges. Prompt construction, structured parsing, retry policy,
and business decisions terminate in typed leaves:
`vacancyanalysis`, `coverletter`, `testanswer`, `employerreply`,
`followupdraft`, `autochatreply`, `candidateinterpretation`, and
`applicationanswer`.

# R11 HH write safety

Raw mutation owner: `internal/adapters/hh/write`.

Mutation endpoints:

- `POST /applicant/vacancy_response/popup` — vacancy response/atomic test;
- `POST /chatik/api/send` — chat send;
- `POST /chatik/api/leave` — chat leave;
- `POST /applicant/resumes/touch` — resume touch;
- `POST /profile/shards/user_statuses/job_search_status` — job-search status.

cmd raw mutation HTTP: **NONE**.

bootstrap raw mutation HTTP: **NONE**.

runtime raw mutation HTTP: **NONE**. Runtime's generic `HHRequester.Do`
rejects every method other than GET/HEAD; its `buildRequest` helper is used
only by read paths. The runtime POST-shaped preview object is not an HTTP
transport.

Write bypass: **NONE**. Live calls terminate at
`workflow -> hhwritegateway -> narrow hhwrite port -> internal/adapters/hh/write`.

HH write retry: **NONE**. Read 429 retry, LLM retry, normal scheduler cadence,
and the next workflow iteration are separate. A write transport is one-shot:
409, 5xx, network uncertainty, and malformed success evidence are ambiguous;
429 is rejected. No same-attempt automatic retry is present.

# R12 workflow safety

Application: `applicationprocessing` prepares a typed result; the runtime
adapter delegates submission choreography to `applicationsubmission`.

Controlled conversation: `employerreplyworkflow` and
`followuporchestration` prepare local drafts/clarifications. Sending requires
explicit approval, fresh preflight, and the write gateway. Preparation cannot
send.

Legacy auto-chat: remains a separate `autochatorchestration` path. It may
send only in auto mode through the gateway and does not use controlled AI-draft
approval.

Candidate learning: `candidateinterpretation` produces proposals;
`candidatelearningorchestration` requires explicit confirmation. No proposal
auto-confirmation or direct AI canonical mutation was found.

Scheduler: `internal/platform/scheduler` has no HH, AI, candidate, storage
business, or workflow capability. Runtime only registers typed tasks.

Duplicate R12 workflow owner: **NONE**.

# Application submission safety

The audited chain is:

```text
applicationprocessing
  -> PreparedApplication
  -> applicationsubmission
  -> fresh vacancy proof
  -> fresh test metadata comparison when applicable
  -> hhwrite gateway/executor
  -> one atomic mutation
```

`PreparedApplication` is not write authorization. Fresh vacancy preflight is
required for live submission; fresh test metadata is required when a test is
present. No task/option remapping or AI re-answer occurs during submission.
Each `Submit` has at most one mutation attempt.

Application ambiguity residual: same-run replay and immediate retry are
absent. Cross-run durable ambiguity identity remains absent and is classified
as known reliability/safety residual, not an R13 migration regression.

# Candidate truth

The candidate path is `gap -> clarification -> answer -> candidate
interpretation -> proposal -> explicit confirmation -> candidate mutation`.
AI-created unknowns/proposals remain unresolved until the explicit candidate
operation. Direct canonical AI mutation: **NONE**. Proposal auto-confirm:
**NONE**. Unknown-to-confirmed without explicit action: **NONE**. There is one
runtime compatibility façade over the candidate mutation boundary and no root
duplicate policy.

Semantic reindex remains an explicit/pre-existing optional post-commit hook.
The R12.3 invariant was preserved: candidate-learning confirmation did not
introduce a new hidden semantic reindex behavior.

# Storage boundaries

Usecase packages import no concrete JSON or Postgres storage adapters. Runtime
composition may know both backends. No `UnitOfWork`, schema, transaction, or
storage behavior change was introduced by R13.

# HH read boundary

HH reads remain separate from mutation. `HHRequester.Do` is GET/HEAD-only,
with the existing safe read rate/concurrency scheduler and read-only 429
retry. No generic mutation method was reintroduced.

# Scheduler and cadence

The timer primitive remains `internal/platform/scheduler`. Runtime wiring
preserves:

- auto-apply: 12h after completion;
- auto-chat: 15m after completion;
- resume touch: 4h after completion;
- job-search status: 24h after completion;
- career monitor: default 15m after completion;
- dashboard inbox: existing wait-first ticker/coalescing behavior.

Run-once directly invokes each allowed workflow once, does not register the
recurring loop, and preserves exit/error behavior. Cadence change: **NONE**.

# CLI / process contract

Bootstrap owns invocation parsing, config loading, signal-aware context,
routing, and exit-code mapping. Cmd contains only process setup:
`context.Background`, `os.Args`, stdio, `runtime.NewHandlers`,
`bootstrap.Run`, and `os.Exit`.

The parser exposes the existing top-level commands: run/default, candidate,
storage, reconcile, monitor, audit, web/dashboard, hh, and profile. No alias or
flag change was found. Signal ownership is bootstrap-only; cmd adds no second
`NotifyContext`. Working-directory defaults are resolved from the caller's
working directory, not a source directory.

Monitor remains read-only for HH. Storage migration apply remains explicit and
does not apply by default.

# Dashboard

`web/dashboard` routes through the same runtime handler graph. Embedded API
paths and JSON responses are retained; inbox refresh remains wait-first and
coalesced. Dashboard HH writes remain gateway-gated and dashboard AI remains
typed. Embedded resources are present and valid:

- `internal/runtime/candidate_communication.md`;
- `internal/runtime/web/index.html`, `app.js`, `styles.css`;
- `internal/runtime/migrations/*.sql`.

Resource tests and runtime embed declarations passed.

# Build / release

Release: `.github/workflows/release.yml` builds every matrix artifact from
`./cmd/hh-ai-responder` with the expected `hh-ai-responder-{GOOS}-{GOARCH}`
names and extensions. No active release build targets the removed root.

README: active build instructions target `go build ./cmd/hh-ai-responder`.
Historical reports and the governance `AGENTS.md` verification command are
not active release/build launchers.

Launchers: `start.sh` and `start.ps1` preserve the `hh-ai-responder` binary
contract and caller argument forwarding.

## Deployment contract

Canonical executable: `cmd/hh-ai-responder`

Canonical build: `go build ./cmd/hh-ai-responder`

Canonical deployment artifact: the Go binary built from `./cmd/hh-ai-responder`

Binary: `hh-ai-responder`

Docker support: **REMOVED / NOT SUPPORTED**

Container verification: **NOT APPLICABLE**

# Side-effect matrix

| Workflow | HH read | AI | Local writes | Candidate truth | HH write | Scheduler |
| --- | --- | --- | --- | --- | --- | --- |
| Career iteration | Yes | No | Sync/reconcile/notifications | Read-only | No | Career cadence |
| Employer reply | Yes/context | Yes | Draft/clarification | Read-only; clarification on unknown | No until controlled approval | Caller-controlled |
| Follow-up | Fresh context/preflight when sent | Yes for draft | Draft/clarification | Read-only; explicit clarification | Approval + gateway only | Caller-controlled |
| Inbox refresh | Yes | No | Local projection/state | Read-only | No | Dashboard ticker/coalescing |
| Legacy auto-chat | Yes | Yes | Audit/local state | Read-only | Auto mode via gateway | 15m loop |
| Candidate learning | No | Interpretation only | Clarification/proposal/events | Explicit confirmation required | No | No |
| Application processing | Yes | Analysis/letter/test | Review/events | Read-only context | No | Auto-apply caller |
| Application submission | Fresh vacancy/test reads | No | Gateway audit/result | No mutation | One atomic attempt | Auto-apply or manual |
| Resume touch | No | No | Audit/result | No | Gateway only | 4h loop |
| Job status | No | No | Audit/result | No | Gateway only | 24h loop |

Capability containment remains identical to R12.Final.

# Remaining composition debt

- 114-file / 30,739-LOC `internal/runtime` package;
- legacy root-era type and DTO names;
- runtime persistence, CLI, dashboard, and compatibility colocation;
- explicit compatibility façade construction;

These are structural/deployment issues, not reasons to refactor runtime during
R13.Final.

# Remaining reliability debt

- cross-run application delivery ambiguity has no durable identity;
- best-effort projection persistence and partial read/projection failure
  behavior remain documented residuals;

# Metrics

| Metric | R13.Final measurement |
| --- | ---: |
| `go list ./...` package count | 51 |
| Root production Go files | 0 |
| Root production LOC | 0 |
| `cmd/hh-ai-responder/main.go` LOC | 19 |
| `internal/bootstrap` production LOC | 179 |
| `internal/runtime` production files | 114 |
| `internal/runtime` production LOC | 30,739 |
| Dynamic dependency-discovery constructors | 0 (5 before R13.3) |

Largest runtime production files:

1. `runtime.go` — 3,168 LOC;
2. `hh_write_gateway.go` — 1,399 LOC;
3. `dashboard_server.go` — 1,288 LOC;
4. `candidate_mutation.go` — 998 LOC;
5. `hh_read_sync.go` — 874 LOC.

Largest runtime functions (audit inventory, approximate source spans):

1. `dashboard_server.go:writeAPI` — 362 LOC, dashboard/compatibility;
2. `hh_sync_cli.go:runHHCommand` — 351 LOC, CLI adapter;
3. `hh_write_gateway.go:Send` — 236 LOC, compatibility/gateway;
4. `dashboard_views.go:analytics` — 221 LOC, dashboard/read model;
5. `runtime.go:NewHHAIResponder` — 189 LOC, composition/provider/storage;
6. `career_audit.go:BuildCareerAuditReport` — 182 LOC, read model;
7. `candidate_profile.go:runProfileBootstrap` — 179 LOC, CLI/local repair;
8. `conversation_eligibility.go:evaluateConversationEligibility` — 172 LOC,
   compatibility/read policy;
9. `candidate_profile.go:runInteractiveProfile` — 165 LOC, CLI/local repair;
10. `runtime.go:LoadProfileData` — 154 LOC, HH read adapter.

No hotspot was classified as a duplicated R12 authoritative business owner.

# Verification

`gofmt -l .`: **PASS**

`go test -count=1 ./...`: **PASS**

`go test -race ./...`: **PASS**

`go vet ./...`: **PASS**

`go build ./...`: **PASS**

Canonical cmd build: **PASS**

Focused bootstrap/runtime/scheduler/R10/R11/R12 package tests:
**PASS**

Safe smoke:

- `go run ./cmd/hh-ai-responder --help`: **PASS** (Go runner reports 0);
- temporary canonical binary `--help`: **PASS**, exit 0;
- temporary canonical binary unknown command: **PASS**, exit 2;
- temporary canonical binary malformed duration/config: **PASS**, exit 2.

`git diff --check`: **PASS**

`node --check web/app.js`: **PASS**

Docker: **NOT APPLICABLE** — support was intentionally removed from the
project and is not part of R13 acceptance or verification.

LIVE HH WRITES: **0**

# R13 final decision

R13: **COMPLETE**

Canonical production entrypoints: **1**

Independent runtime graphs: **1**

Dynamic production dependency discovery: **NONE**

Raw HH mutation bypass: **NONE**

Automatic HH mutation retries: **NONE**

Direct business AI bypass: **NONE**

Candidate mutation bypass: **NONE**

Duplicate R12 workflow ownership: **NONE**

Docker blocker: **REMOVED BY EXPLICIT DE-SCOPING OF DOCKER SUPPORT**

LIVE HH WRITES: **0**

# Next architecture phase

This report stops at R13.Final. No R14 work is authorized by this stage. The
previously identified next architecture phase remains **B. Reliability /
Delivery Semantics** as future work, because cross-run application ambiguity
and partial projection failure semantics carry greater safety impact than the
maintainability benefit of immediately splitting `internal/runtime`.

Alternatives, ranked after B:

1. **A. Runtime Package Decomposition** — highest maintainability leverage,
   but high change risk and no immediate safety gain;
2. **C. Persistence Boundary Extraction** — useful if JSON/Postgres coupling
   becomes the dominant dependency leverage, but currently less urgent than
   delivery semantics.

No R14 work, runtime split, reliability fix, HH behavior change, AI behavior
change, candidate behavior change, scheduler change, or persistence redesign
was performed.
