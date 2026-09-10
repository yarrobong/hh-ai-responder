# Refactor R13.2b — Executable Entry Point Cutover

Status: **COMPLETE**

Scope: make `cmd/hh-ai-responder` the canonical production executable while
retaining the root executable as a mechanically equivalent compatibility
wrapper. No runtime redesign was performed.

LIVE HH WRITES: **0**

## Before

Canonical entrypoint: root `main.go`.

Root compatibility entrypoint: not applicable; root was the production
entrypoint.

Build references found in the pre-change audit:

| Reference | Current target | Changed? | Reason |
| --- | --- | --- | --- |
| `Dockerfile` | `go build ... .` | Yes | Production container now builds `./cmd/hh-ai-responder`. |
| `.github/workflows/release.yml` | release build of `.` | Yes | Release artifacts now use the canonical executable. |
| `README.md` | `go build .` and embedded-resource rebuild | Yes | Instructions now use `./cmd/hh-ai-responder`. |
| `start.sh`, `start.ps1` | historical binary `hh-ai-responder` | No | Binary name and launcher contract remain unchanged. |
| `docker-compose.yml` | image build only | No | No executable target was specified there. |
| historical reports | historical commands | No | Reports remain historically accurate. |

## New cmd entrypoint

Path: `cmd/hh-ai-responder/main.go`

LOC: 19, including the package comment.

Imports: standard library, `internal/bootstrap`, and `internal/runtime` only.

Responsibilities: process setup only — base context, process args, standard
streams, handler construction, and process exit code.

The file contains no configuration interpretation, business AI construction,
HH construction, storage construction, scheduler registration, dashboard
composition, vacancy policy, candidate policy, CLI dispatch, or write gate.

## Entrypoint graph

```text
cmd/hh-ai-responder/main.go
    ↓
internal/bootstrap.Run
    ↓
internal/runtime.NewHandlers
    ↓
R10/R11/R12 owners, use cases, and adapters
    ↓
raw HH mutations only through internal/adapters/hh/write
```

Legacy compatibility path:

```text
root main.go
    ↓
internal/bootstrap.Run
    ↓
internal/runtime.NewHandlers
    ↓
the same production graph
```

## Root compatibility

Retained: **YES**.

Reason: root `main.go` is a temporary 18-line process wrapper. Keeping it
preserves `go run .`, `go build .`, and existing local workflows without
maintaining a second runtime graph. R13.3 may remove or demote it after a
separate compatibility review.

## Canonical build

Command:

```text
go build ./cmd/hh-ai-responder
```

Binary: `hh-ai-responder` when built from the repository root; deployment
binary name remains unchanged.

## Docker

Before: Docker built the root package (`.`).

After: Docker copies `cmd/` and builds:

```text
go build -trimpath -buildvcs=false -ldflags="-s -w" -o /usr/local/bin/hh-ai-responder ./cmd/hh-ai-responder
```

Runtime image, user, entrypoint, working directory, environment contract, and
binary name are unchanged. Embedded resources remain under
`internal/runtime` and are included by the existing
`COPY internal ./internal` step.

Docker verification: **SKIPPED** because the Docker daemon was unavailable:
`Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?`

## CLI parity

`cmd/hh-ai-responder/main_test.go` runs the root and canonical wrappers for
`--help` and an unknown command, comparing exit code, stdout, and stderr.
The parity test passed. Existing bootstrap/runtime tests remain in place,
including the external importability test for `runtime.NewHandlers()`.

No command syntax, aliases, working-directory resolution, or signal ownership
changed. Signal context creation remains in `internal/bootstrap`; both
process wrappers pass a normal `context.Background()`.

## Exit-code parity

The automated parity test passed for help and unknown invocation. Both wrappers
use the same `bootstrap.Run` exit-code mapping.

## Stdout/stderr parity

The automated parity test passed with exact stdout/stderr comparison for the
safe cases above. No startup banner or stream swap was introduced.

## Working directory / assets

Both wrappers leave working-directory behavior to `bootstrap.Run`, which
derives configuration paths from the caller's current directory. No source
location is used for application data paths.

Embedded resources remain colocated with `internal/runtime`:

```text
internal/runtime/candidate_communication.md
internal/runtime/web/{index.html,app.js,styles.css}
internal/runtime/migrations/*.sql
```

## Runtime graph

Single production graph: **YES**.

`cmd` has no root-package dependency. `internal/bootstrap` and
`internal/runtime` also have no root-package dependency according to
`go list -deps` audits.

## Dependency audit

```text
cmd -> root:       NONE
bootstrap -> root: NONE
runtime -> root:   NONE
```

The root package is present only as the compatibility executable and is not in
the canonical command's dependency closure.

## Safety

Raw HH mutation owner: `internal/adapters/hh/write`.

`cmd` raw write: **NONE**.

`bootstrap` raw write: **NONE**.

`runtime` direct raw mutation transport: **NONE**; compatibility methods
delegate through the write port/gateway. Runtime request builders used by
read paths and offline write previews remain unchanged.

Root raw write: **NONE**.

HH write bypass: **NONE**.

HH write retry: **NONE**. The one-attempt HH mutation transport remains the
existing adapter.

`HH_DRY_RUN`, `HH_WRITE_ENABLED`, write gates, candidate confirmation
rules, preflight rules, and test validation were not changed.

LIVE HH WRITES: **0**.

## Scheduler

Cadences: **UNCHANGED**.

- auto-apply: 12h;
- auto-chat: 15m;
- resume touch: 4h;
- job-search status: 24h;
- career monitor default: 15m.

The cmd entrypoint constructs handlers once and registers no additional
scheduler.

## Metrics

| Metric | Result |
| --- | ---: |
| Root `main.go` | 18 LOC |
| Cmd `main.go` | 19 LOC |
| `internal/bootstrap` production code | 179 LOC |
| `internal/runtime` production code | 30,893 LOC |
| `internal/runtime` production files | 114 |
| `go list ./...` packages | 52 |

## R13.3 inventory

### REMOVE R13.3

- temporary root `main.go` compatibility wrapper after a compatibility
  decision;
- remaining root-only executable assumptions;
- legacy compatibility constructors and aliases after caller inventory.

### KEEP PUBLIC COMPATIBILITY

- historical `go run .` / `go build .` path during the compatibility window;
- historical binary name `hh-ai-responder`;
- existing CLI flags, environment variables, cookies support, and JSON event
  compatibility.

### RUNTIME DECOMPOSITION DEBT

- the 114-file, approximately 30.9k LOC `internal/runtime` package;
- legacy root-named concepts and adapters retained inside that package;
- variadic/compatibility constructors intentionally not redesigned here.

### POST-R13 RELIABILITY DEBT

- rerun the Docker build in an environment with an available Docker daemon;
- continue focused reliability work only under a later, separately scoped
  stage.

## Verification

| Check | Result |
| --- | --- |
| `gofmt -l .` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build .` | PASS; root compatibility retained |
| `go build ./cmd/hh-ai-responder` | PASS |
| canonical `go run ./cmd/hh-ai-responder --help` | PASS |
| temporary canonical binary `--help` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| Docker build | SKIPPED; daemon unavailable |
| generated binary in repository | NONE |
| live HH writes | 0 |

Focused coverage included `internal/bootstrap`, `internal/runtime`, the
external handler importability test, scheduler/runtime safety suites, and the
new root/cmd executable parity test.

## R13 status

R13.2b: **COMPLETE**.

## Ready

R13.3 — Legacy Root / Compatibility Cleanup: **READY**.

Do not begin it as part of R13.2b.
