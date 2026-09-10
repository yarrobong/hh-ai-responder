# Stage R2 report — platform infrastructure extraction

Date: 2026-09-08 (Asia/Yekaterinburg)

## Before

Packages:

```text
hh-ai-responder
```

There was one production Go package, `main`. Platform-like code was spread
through `local_lock.go`, `performance.go`, `ai_reply_orchestrator.go`, and
several JSON stores.

Platform utilities before:

- filesystem process locking lived in `local_lock.go`;
- generic `PerfSummary` recording and snapshots lived in `performance.go`;
- same-directory private temp-file writes were duplicated in store code and
  behind `atomicPrivateStoreWrite`;
- canonical-name memoization and `OperationPerformance` were mixed into
  `performance.go`, but are still coupled to current domain/sync behavior.

Relevant files audited:

- `local_lock.go`
- `performance.go`
- `ai_reply_orchestrator.go`
- `candidate_knowledge_store.go`
- `conversation_store.go`
- `job_application_store.go`
- `vacancy_store.go`
- `hh_read_sync.go`
- private JSON stores and their tests

Classification:

- `EXTRACT_NOW`: process lock, single-file private atomic write, generic
  duration/counter metrics;
- `DEFER`: logging, clock abstraction, multi-file candidate knowledge staging,
  legacy fixed-`.tmp` writers;
- `KEEP_DOMAIN_LOCAL`: canonical memoization and `OperationPerformance` /
  `operationMeter`.

## Extraction

Atomic file:

- old location: `atomicPrivateStoreWrite` in `ai_reply_orchestrator.go`, plus
  repeated implementations in the conversation, application, and vacancy
  stores;
- new location: `internal/platform/atomic_file.go`;
- API: `platform.WritePrivateFileAtomic(path, data, tempPattern) error`;
- behavior preserved: same-directory temp file, `0600` temp permissions,
  `0700` directory creation, write, `fsync`, close, atomic rename, and cleanup
  on failure;
- migrated callers: AI drafts, clarifications, quality log, daily refresh,
  pilot observations, career monitor state/notifications, HH action/audit
  stores, HH sync state, conversation store, application store, and vacancy
  store.

The candidate knowledge save remains a domain-specific multi-file staging
operation: it stages every collection before renaming any destination. It was
not reduced to repeated single-file calls because that would change its
failure/partial-save contract.

Process lock:

- old location: `local_lock.go`;
- new location: `internal/platform/process_lock.go`;
- API: `platform.AcquireProcessLock`, `ProcessLock.Release`, and
  `ProcessLock.Touch`;
- preserved lock path suffix, owner file, permissions, stale-lock threshold,
  duplicate-acquisition error, and release behavior;
- `withStoreLock` remains in `main` only for the existing in-process mutex plus
  platform process-lock composition.

Metrics:

- old location: generic portion of `performance.go`;
- new location: `internal/platform/metrics.go`;
- API: `platform.NewMetrics`, `Metrics.Record`, and `Metrics.Snapshot`;
- preserved `PerfSummary` JSON shape, operation-keyed counters, duration and
  item accumulation, defensive snapshots, and mutex protection;
- canonical memoization and operation-specific sync meters remain in
  `performance.go`.

Logging:

DEFERRED

Reason: the current logger is already part of broad package-main composition;
moving it would require constructor and global wiring changes unrelated to
R2.

Clock:

NOT INTRODUCED

Reason: no existing concrete clock testability problem required an abstraction.

## Dependencies

`go list ./...`:

```text
hh-ai-responder
hh-ai-responder/internal/platform
```

Platform imports:

```text
fmt
os
path/filepath
strconv
strings
sync
time
```

`internal/platform` imports no business package, repository, HH, AI,
dashboard, PostgreSQL, or third-party package.

## Compatibility wrappers

- `perfRecord` and `perfSnapshot` remain transitional package-main wrappers
  over `platform.Metrics`, so the existing dashboard/sync/test call sites keep
  their current shape. New production code should call the platform metrics
  object directly.

No process-lock or atomic-writer compatibility implementation remains.

## Tests

Moved:

- no pre-existing platform-only tests were present to move.

Added:

- `internal/platform/atomic_file_test.go`: success, replacement, `0600`
  permissions, failed rename cleanup, and concurrent complete-file writes;
- `internal/platform/process_lock_test.go`: acquisition, duplicate denial,
  release, touch, stale reclamation, and path/nil semantics;
- `internal/platform/metrics_test.go`: recording, defensive snapshots, and
  concurrent recorders.

Existing store, HH write-safety, sync, dashboard, and candidate tests remain
in `main` and exercise the migrated callers.

## Behavior

Candidate: **UNCHANGED**

Candidate Knowledge Acquisition: **UNCHANGED**

Vacancy matching: **UNCHANGED**

Conversation classification: **UNCHANGED**

AI: **UNCHANGED**

HH Read: **UNCHANGED**

HH Write: **UNCHANGED**. `hh_write_gateway.go` and
`hh_write_transport.go` received no semantic changes; only the audit/action
store writer calls use the platform primitive.

Dashboard: **UNCHANGED**

Storage schemas and filenames: **UNCHANGED**

PostgreSQL schema: **UNCHANGED**

CLI and env/flags: **UNCHANGED**

## File size / ownership

R2 moved approximately 165 lines of extracted infrastructure out of the root
implementation (excluding import and call-site rewrites) and added 163 lines
of production implementation to `internal/platform`; platform tests add 199
lines. LOC reduction was not a target.

Production files now owned by platform:

- `internal/platform/atomic_file.go`
- `internal/platform/process_lock.go`
- `internal/platform/metrics.go`

Remaining root helpers that should eventually move elsewhere:

- configuration/CLI and composition in `main.go`;
- domain-specific stores and services;
- candidate knowledge multi-file staging;
- legacy fixed-`.tmp` writers in candidate profile and already-responded
  state;
- domain-specific operation meters and normalization memoization;
- logging, deferred to R3 or a later platform slice.

## Verification

The final command results are recorded below after the full verification pass.

gofmt: **PASS**

go test ./...: **PASS**

go test -race ./...: **PASS**

go vet ./...: **PASS**

go build ./...: **PASS**

git diff --check: **PASS**

node --check web/app.js: **PASS**

Focused platform tests: **PASS** (`go test ./internal/platform/...`)

Docker: **SKIPPED** — `Cannot connect to the Docker daemon at
unix:///var/run/docker.sock. Is the docker daemon running?`

LIVE HH WRITES: **0**

## Architectural audit

- [x] platform imports no business package;
- [x] no package import cycles;
- [x] no duplicate single-file atomic writer implementation with the extracted
  fsync/random-temp contract (legacy fixed-`.tmp` variants are explicitly
  deferred);
- [x] no duplicate process lock implementation;
- [x] extracted metrics are generic;
- [x] business-specific caches/meters stayed with their current owner;
- [x] no new `...any` introduced;
- [x] no new global mutable dependency wiring;
- [x] no HH write capability added;
- [x] no behavior intentionally changed.

## Remaining platform debt

- migrate the two transitional metrics wrappers when the remaining callers
  move into their owning packages;
- decide later whether the multi-file candidate knowledge save deserves a
  typed staging primitive, without weakening its current contract;
- evaluate the legacy fixed-`.tmp` writers only with characterization tests
  for their distinct no-fsync behavior;
- logging remains coupled to package-main composition.

## Ready for R3

R3 — Config + CLI + Thin Main: **READY**

R2 stops here. No config, CLI, thin-main, candidate/vacancy/application/
conversation package, repository/ports, AI, HH adapter, or dashboard
extraction was started.
