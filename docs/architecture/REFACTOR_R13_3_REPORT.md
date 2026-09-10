# Before

Canonical executable:

```text
cmd/hh-ai-responder/main.go
```

The root executable was still an 18-line compatibility wrapper calling the
same `bootstrap.Run → runtime.NewHandlers` graph. No active production,
development, or test caller required `go run .` or `go build .`; Docker,
release, README, and canonical tests already targeted `./cmd/hh-ai-responder`.

Compatibility constructors before cleanup:

| Constructor | Typed alternative | Production callers | Test callers | Compatibility callers | Action |
| --- | --- | ---: | ---: | ---: | --- |
| `NewAIReplyOrchestrator(...any)` | `NewAIReplyOrchestratorWithOptions` | 0 | several runtime tests | 0 | remove and migrate tests |
| `NewHHReadSyncService(...any)` | `NewHHReadSyncServiceWithOptions` | `hh_sync_cli`, sync staging | sync/performance/runtime tests | 0 | migrate all callers and remove |
| `NewHHWriteGateway(...any)` | `NewHHWriteGatewayWithOptions` | 0 | one gateway fixture | 0 | migrate fixture and remove |
| `NewApplicationStore(...any)` | explicit store / `NewApplicationStoreWithDependencies` | no dynamic use | storage/runtime tests | 0 | make constructor path-only |
| `NewCareerDataReconciler(...any)` | `NewCareerDataReconcilerWithRepositories` | monitor path | reconciliation/performance tests | 0 | migrate all callers and remove |

The internal `buildHHReadSyncService(...any)` helper was also converted to
explicit analyzer, candidate, clarification, draft, and state-path inputs.

Initial metrics for this stage were 52 Go packages, one root production file
(18 LOC), `cmd` main 19 LOC, bootstrap 179 LOC, and 114 production runtime
files / 30,893 LOC.

Legacy aliases and wrappers were used by the runtime package itself and its
tests. They were not removed solely because their names came from the former
root package.

# Compatibility audit

| Surface | Callers | Classification | Action |
| --- | --- | --- | --- |
| Root `main.go` | no active callers; old commands only in historical reports | dead migration executable | removed |
| `go run .`, `go build .`, `go install .` | no active repository callers | historical/dead | active docs already use `cmd`; historical reports untouched |
| Variadic AI/read/write/application/reconciliation constructors | tests plus two internal construction paths | migration/test compatibility | migrated to typed paths and removed |
| `NewApplicationStoreWithDependencies` | runtime store loading and tests | active internal API | retained; dependencies are explicit |
| Typed `...string` / typed retriever constructors | active runtime and tests | active typed API | retained; no dynamic dependency discovery |
| Root-era candidate, conversation, vacancy, application, HH DTO aliases | runtime composition, adapters, tests | active compatibility façade | retained |
| `DraftStore` / `NewDraftStore` | no callers after inventory | dead alias/forwarder | removed |
| `ClarificationStore` / `NewClarificationStore` | no runtime callers; canonical JSON adapter remains | dead alias/forwarder in runtime | removed |
| `HHWriteActionStore` / `NewHHWriteActionStore` | no callers | dead alias/forwarder | removed |

# Root executable decision

Decision: **REMOVE**.

Evidence:

- Dockerfile and release workflow build `./cmd/hh-ai-responder`.
- README build/rebuild commands use `./cmd/hh-ai-responder`.
- launchers preserve the deployed `hh-ai-responder` binary name and do not
  require a Go package at repository root.
- the previous parity test was the only active root executable consumer; it was
  replaced by canonical safe-behavior coverage.
- `go list -deps ./cmd/hh-ai-responder`, `./internal/bootstrap`, and
  `./internal/runtime` contain no repository-root package dependency.

Canonical executable:

```text
cmd/hh-ai-responder
```

# Entrypoints after

```text
cmd/hh-ai-responder/main.go
    ↓
internal/bootstrap.Run
    ↓
internal/runtime.NewHandlers
    ↓
R10/R11/R12 use cases and adapters
    ↓
raw HH mutations only through internal/adapters/hh/write
```

There is one production entrypoint, one runtime graph, and no root production
`package main`. The `cmd` test file uses `package main` only because it tests
the executable package; it is not a second entrypoint.

# Constructor cleanup

Typed production constructors:

- `NewAIReplyOrchestratorWithOptions`;
- `NewHHReadSyncServiceWithOptions` and the typed repository variant;
- `NewHHWriteGatewayWithOptions`;
- `NewApplicationStore` plus explicit `NewApplicationStoreWithDependencies`;
- `NewCareerDataReconcilerWithRepositories`.

Removed variadic constructors:

- `NewAIReplyOrchestrator(...any)`;
- `NewHHReadSyncService(...any)`;
- `NewHHWriteGateway(...any)`;
- `NewApplicationStore(...any)`;
- `NewCareerDataReconciler(...any)`.

Retained variadic constructors:

- `NewVacancyStore(...string)`;
- `NewNotificationStore(...string)`;
- `NewConversationContextBuilder(...CandidateSemanticRetriever)`;
- `NewApplicationContextBuilder(...CandidateSemanticRetriever)`.

These remaining variadics are typed value/path conveniences, not dynamic
dependency discovery or production escape hatches. Dynamic compatibility
constructors: **5 before, 0 after**.

# Alias / wrapper cleanup

Removed:

- root `main.go` compatibility executable;
- three unused runtime alias/forwarder pairs: draft store, clarification
  store, and HH write action store;
- the five unused/dynamic constructor entrypoints listed above.

Retained:

- aliases that are actively used by runtime composition, JSON/PostgreSQL
  adapters, CLI/dashboard code, or tests;
- legacy DTO and JSON-facing names where removing them could affect persisted
  or emitted shapes;
- `NewApplicationStoreWithDependencies`, because active store loading still
  needs those explicit collaborators.

# Build / release references

Active production references use `./cmd/hh-ai-responder` in Dockerfile,
`.github/workflows/release.yml`, and README. `start.sh`, `start.ps1`, and the
deployment contract continue to use the binary name `hh-ai-responder`.
Historical architecture/validation reports were not edited.

# CLI / output compatibility

No command, flag, environment key, precedence rule, binary name, JSON event
name, JSON field, status value, draft/action shape, candidate proposal shape,
or dashboard output contract was changed. The former root/cmd parity test was
replaced with canonical `--help` and unknown-invocation behavior checks;
bootstrap tests remain authoritative for parsing, configuration errors, exit
mapping, signal context, run-once routing, and working-directory behavior.

# Runtime ownership

Duplicate authoritative workflows: **NONE**.

No broad `internal/runtime` decomposition was performed. Runtime remains 114
production files; this stage only removed compatibility construction surfaces
and three unused aliases/forwarders.

# Safety

Raw HH mutation owner:

```text
internal/adapters/hh/write
```

Write bypass: **NONE**.

HH write retry: **NONE**. The remaining `POST` method literal in runtime is a
request-preview representation; runtime does not perform the mutation HTTP
call. Network mutation remains in the one-attempt HH write adapter.

Direct business AI bypass: **NONE**.

Candidate mutation bypass: **NONE**.

LIVE HH WRITES: **0**.

# Scheduler

Cadences unchanged:

- auto-apply: 12h;
- auto-chat: 15m;
- resume touch: 4h;
- job-search status: 24h;
- career monitor default: 15m.

# Runtime decomposition debt

Inventory only; no code changes were made for these future boundaries:

| Future boundary | Approximate current surface | Main dependencies |
| --- | --- | --- |
| runtime composition/startup | ~8 files | bootstrap, config, use cases, adapters |
| HH read/sync compatibility | ~9 files | HH read port, read adapter, JSON/PostgreSQL repositories |
| dashboard HTTP/views/lifecycle | ~6 files | runtime stores, HH read/write façades, embedded web assets |
| persistence façades | ~15 files | storage adapters, ports, JSON/PostgreSQL |
| CLI and operational adapters | ~10 files | config, scheduler, storage, sync, monitor |
| legacy domain projections/facades | remaining runtime surface | candidate, conversation, vacancy, application use cases |

These are post-R13 architecture candidates, not R13.3 work.

# Reliability debt

Kept separate from this cleanup:

- cross-run application ambiguity;
- partial/best-effort projection writes;
- Docker daemon availability for local verification;
- any other previously documented reliability issues.

No persistence schema, transaction, scheduler, HH, AI, candidate, or business
workflow redesign was introduced.

# Metrics

| Metric | Before | After |
| --- | ---: | ---: |
| Go packages | 52 | 51 |
| Root production files | 1 | 0 |
| Root production LOC | 18 | 0 / not applicable |
| `cmd/hh-ai-responder/main.go` LOC | 19 | 19 |
| Bootstrap production LOC | 179 | 179 |
| `internal/runtime` production files | 114 | 114 |
| `internal/runtime` production LOC | 30,893 | 30,739 |
| Dynamic variadic constructors | 5 | 0 |
| Compatibility alias/forwarder pairs removed | 0 | 3 |
| Compatibility root executable | 1 | 0 |

# Remaining compatibility debt

The runtime package still exposes actively used root-era aliases, DTO names,
read/write façades, and explicit dependency constructors. They remain because
repository callers, adapters, tests, or JSON/persistence compatibility still
use them. No hypothetical external import compatibility was used as a reason:
`internal/runtime` is an internal package.

The larger 114-file runtime package and its legacy names remain scheduled for a
future architecture phase; R13.3 deliberately does not decompose or broadly
rename it.

# R13.Final readiness

Composition migration complete enough for final closure audit: **YES**.

# Verification

| Check | Result |
| --- | --- |
| `gofmt -l .` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS; generated binary removed afterward |
| root build | intentionally no longer applicable after root removal |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `go run ./cmd/hh-ai-responder --help` | PASS |
| Docker | SKIPPED: Docker CLI is installed, but daemon connection to `unix:///var/run/docker.sock` failed with `Cannot connect to the Docker daemon ... Is the docker daemon running?` |
| generated/binary artifacts | NONE |
| live HH writes | 0 |

# R13 status

R13.3: **COMPLETE**.

# Ready

R13.Final — Composition Root Migration Closure Audit
READY

Do not begin it as part of R13.3.
