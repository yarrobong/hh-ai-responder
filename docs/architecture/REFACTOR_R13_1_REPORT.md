# Before

Entrypoint:

```text
root main.go
  -> run(args)
  -> internal/cli.Parse
  -> root executeInvocation
  -> root config conversion and command dispatch
  -> root signal context
  -> root NewHHAIResponder / dashboard / CLI construction
  -> root Run or command handler
  -> os.Exit
```

`main.go` was both the executable entrypoint and the owner of the root
compatibility/runtime types. A future `cmd/hh-ai-responder` package could not
reuse its startup path without importing `package main`.

Startup trace and modes:

| Mode | Configuration | Runtime path | Exit behavior |
|---|---|---|---|
| default / `run` | leading application flags, env, dotenv | signal-aware `HHAIResponder.Run` | `0` on normal completion, `1` on runtime construction failure |
| `--help` / `help` | leading flags | config help only | `0` for help, `2` for other config errors |
| `candidate` | env/dotenv; command flags remain candidate-owned | candidate command adapter | `0` or `1` |
| `storage` | env/dotenv; migration flags remain storage-owned | storage migration adapter | `0` or `1` |
| `reconcile` | leading and command flags | local reconciliation adapter | `0` or `1` |
| `monitor` | leading and command flags | permanently read-only monitor adapter | `0` or `1` |
| `audit` | leading and command flags | local audit adapter | `0` or `1` |
| `web` / `dashboard` | leading application flags; dashboard flags remain dashboard-owned | dashboard construction and server lifecycle | `0` or `1` |
| `hh` | leading application flags; HH flags remain HH-owned | HH sync/workflow CLI adapter | `0` or `1` |
| `profile` | env/dotenv; profile flags remain profile-owned | profile/knowledge adapter | `0` or `2` |

Exit behavior is unchanged. Unknown invocation and configuration failures are
reported to stderr with code `2`. Command/runtime failures retain their
existing handler-selected code. The default runtime receives a context derived
from the caller context and `SIGINT`/`SIGTERM`; cancellation causes the
existing responder lifecycle to finish without a new retry or scheduler path.

Output behavior is unchanged at the process boundary. Bootstrap owns only
error reporting for parse/config failures. Command handlers continue to own
human-readable output, CLI JSON/event output, logger construction, and their
existing stdout/stderr destinations. The root list-resumes path now writes to
the supplied stdout writer, which is identical to `os.Stdout` in the
executable and makes the boundary testable.

Root-only dependencies found during the audit:

- `HHAIResponder`, `HHRequester`, `AIClient`, `Logger`, persistent cookie jar,
  legacy response models, and their concrete HH/AI construction;
- root JSON/Postgres compatibility composition and local projection wiring;
- dashboard server dependency construction;
- root CLI command adapters (`runHHCommand`, profile, candidate, storage,
  reconciliation, monitor, audit, and dashboard);
- legacy facades such as `ApplyVacancies`, `AutoRespondChats`,
  `HHReadSyncService`, `HHWriteGateway`, and `AIReplyOrchestrator`.

These are compatibility/composition dependencies, not new business workflow
owners. R12 typed usecases and the narrow HH write adapter remain their
authoritative owners.

# Classification

| main/root responsibility | Classification | R13.1 action |
|---|---|---|
| `main`, `run`, process exit mapping | ENTRYPOINT / PROCESS LIFECYCLE | Root `main` is now a thin call to `bootstrap.Run`; `run` is a compatibility wrapper. |
| CLI syntax classification | CLI DISPATCH | Reused through `internal/cli` from bootstrap. |
| typed config loading and env boundary | CONFIG | Bootstrap calls `internal/config.Load`; no config keys or precedence changed. |
| legacy config conversion | COMPATIBILITY TYPE | Retained in root for R13.2 because root runtime types still consume it. |
| signal-aware context | PROCESS LIFECYCLE | Moved to bootstrap; injectable seam added for deterministic tests. |
| root logger construction | COMPOSITION | Retained in root adapter; format, level, destination, and prefixes unchanged. |
| HH read construction | HH READ ADAPTER / COMPOSITION | Retained behind root compatibility construction; no read capability widened. |
| HH write construction | HH WRITE ADAPTER / COMPOSITION | Retained behind existing gateway chain; bootstrap constructs no mutation request. |
| AI provider construction | AI ADAPTER / COMPOSITION | Retained behind root compatibility construction and typed provider path. |
| storage selection and persistence | LOCAL PROJECTION / COMPOSITION | Retained in root compatibility adapters; JSON/Postgres semantics unchanged. |
| candidate services | COMPATIBILITY ADAPTER | Existing candidate acquisition/interpretation/mutation/learning owners unchanged. |
| application services | COMPATIBILITY ADAPTER | Existing analysis, cover-letter, test-answer, processing, and submission owners unchanged. |
| conversation services | COMPATIBILITY ADAPTER | Controlled and legacy paths remain separate. |
| career/notification/monitor services | COMPATIBILITY ADAPTER / LIFECYCLE | Existing career iteration and monitor owners unchanged. |
| scheduler registration | PROCESS LIFECYCLE | Cadence constants and completion-based loops unchanged. |
| dashboard handlers | CLI/DASHBOARD ADAPTER | Handler logic was not refactored; only routing moved behind explicit bootstrap wiring. |
| vacancy matching, candidate policy, prompt logic | BUSINESS WORKFLOW | NONE introduced in bootstrap. Existing authoritative usecases remain owners. |
| test fakes and fixtures | TEST SUPPORT | Not moved. |

# Bootstrap boundary

Package:

`internal/bootstrap`

API:

```go
func Run(ctx context.Context, args []string, env Env) int
```

`Env` provides standard streams, working directory, environment lookup, an
injectable signal-context seam, and explicit `Handlers`. Each handler receives
one typed `Request` containing context, parsed invocation, validated
`internal/config.Config`, and I/O streams.

Responsibilities:

- parse and classify the top-level invocation;
- load typed configuration at the environment boundary;
- preserve command-specific configuration parsing exceptions;
- create the signal-aware runtime context exactly once for the default mode;
- route to an explicitly named handler;
- preserve exit-code classes and parse/config error reporting;
- provide deterministic seams for bootstrap tests.

Forbidden responsibilities:

- vacancy matching or application policy;
- conversation eligibility or reply policy;
- Candidate fact interpretation or mutation policy;
- prompt construction or AI business logic;
- HH mutation request construction;
- write retry or ambiguous-write interpretation;
- storage backend branching inside usecases;
- generic service locators, registries, containers, or `map[string]any` wiring.

`internal/bootstrap` does not import the root `package main`. It imports only
`internal/cli` and `internal/config`.

# Startup graph

```text
root main.go
    |
    v
internal/bootstrap.Run(ctx, args, Env)
    |
    +-- internal/cli.Parse
    +-- internal/config.Load
    +-- signal.NotifyContext (default runtime only)
    +-- explicit named Handler
          |
          +-- root compatibility adapters (current R13.1 boundary)
          |     +-- typed internal/usecase owners
          |     +-- typed HH read/write ports and adapters
          |     +-- typed AI provider
          |     +-- JSON/Postgres composition
          |     +-- dashboard/CLI lifecycle
          |
          +-- future cmd/hh-ai-responder handlers after R13.2 moves
```

There is one executable startup path. The old root `executeInvocation` switch
and root-owned signal setup were removed; the root wrapper now supplies named
compatibility handlers to bootstrap. `cmd/hh-ai-responder/main.go` is not
created in R13.1.

# Tests and safety

`internal/bootstrap/bootstrap_test.go` covers:

- unknown command rejection before runtime construction;
- configuration failure before runtime construction;
- run-once selection and signal-context inheritance;
- command selection and argument preservation;
- cancelled context propagation;
- working-directory-derived config defaults.

The root handler graph remains explicit and typed. The default production path
continues to use the R12 typed constructors/options, including the typed AI,
HH read, HH write gateway, and application-store paths. Legacy variadic
constructors remain compatibility debt and were not removed.

Run-once continues to call the workflow directly through the existing
responder path. Bootstrap does not register or wait on a scheduler for a
one-shot invocation. Existing cadence constants remain:

| Task | Cadence |
|---|---:|
| auto-apply | 12h |
| auto-chat | 15m |
| resume touch | 4h |
| job-search status | 24h |
| career monitor | configured/default 15m |

Dry-run and write safety are unchanged. No raw mutation HTTP was added to
bootstrap; the only raw HH mutation owner remains `internal/adapters/hh/write`.
No write retry was added. No live HH write was performed.

# R13.2 readiness and residual root symbols

Status: **R13.2 READY WITH REQUIRED MOVES**.

The new command entrypoint can import and call `internal/bootstrap.Run` without
importing the root package. To reproduce the complete current executable
behavior, R13.2 must move the explicitly listed compatibility handlers and
their root-only runtime types behind importable packages. This is a known
composition migration, not an import cycle or safety blocker.

| Root symbols/responsibilities | Classification before cmd migration |
|---|---|
| `Config`, `legacyConfigFromPackage`, config compatibility helpers | MOVE R13.2 |
| `HHAIResponder`, `NewHHAIResponder`, `HHRequester`, `AIClient`, `Logger`, cookie jar | MOVE R13.2 |
| root HH read facade and backend selection | MOVE R13.2 |
| root dashboard construction and command adapters | MOVE R13.2 |
| legacy response/request models and local projections | KEEP COMPATIBILITY during migration |
| `ApplyVacancies`, `AutoRespondChats`, `CareerMonitor`, `AIReplyOrchestrator`, `HHReadSyncService`, `HHWriteGateway` facades | KEEP COMPATIBILITY until callers migrate |
| `run`, `rootBootstrapHandlers`, `rootCommandResult` | KEEP as temporary root adapter, then remove |
| parsing/serialization helpers retained only for legacy tests or facades | TEST ONLY / KEEP COMPATIBILITY as usage dictates |
| cross-run application ambiguity and remaining variadic constructors | POST-R13 DEBT |

R13.2 should first move the root runtime coordinator and legacy configuration
shape together, then move command/dashboard adapters, then reduce `main.go` to
`os.Exit(bootstrap.Run(...))` with handlers constructed outside `package main`.
No business workflow should be reimplemented during those moves.

# Metrics

Measured after R13.1 changes:

| Metric | Value |
|---|---:|
| root production Go files | 113 |
| root production Go LOC | 30,898 |
| `main.go` LOC | 3,232 |
| `internal/bootstrap` Go LOC | 306 |
| `go list ./...` package count | 50 |

# Verification

| Check | Result |
|---|---|
| `gofmt -l .` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| Docker build | SKIPPED; Docker daemon unavailable |

The worktree contained extensive pre-existing user changes. They were
preserved. R13.1 added only the importable bootstrap boundary, its tests, the
thin root wiring, this report, and the related CLI package comment. LIVE HH
WRITES = 0.
