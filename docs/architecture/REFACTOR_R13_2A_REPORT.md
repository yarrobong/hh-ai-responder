# Refactor R13.2a — Importable Runtime / Compatibility Layer

Status: **COMPLETE**

Scope: move the production handler graph out of root `package main` while
preserving the existing executable, CLI contract, runtime behavior, and HH
write safety. `cmd/hh-ai-responder/main.go` was intentionally not created.

LIVE HH WRITES: **0**

## Pre-change audit

The pre-change worktree already contained the R13.1 bootstrap boundary and
extensive unrelated user changes. Those changes were preserved. The measured
baseline from the R13.1 audit was:

| Metric | Before R13.2a |
|---|---:|
| Root production Go files | 113 (`main.go` plus root compatibility files) |
| Root production Go LOC | 30,898 |
| `main.go` LOC | 3,232 |
| `go list ./...` package count | 50 |

The root handler graph was:

```text
root main
└─ run
   └─ bootstrap.Run
      └─ rootBootstrapHandlers
         ├─ runDefaultConfig
         │  ├─ NewLogger
         │  └─ NewHHAIResponder
         │     ├─ Config compatibility conversion
         │     ├─ BuildCandidatePersistence / JSON candidate stores
         │     ├─ buildVacancySearchProfiles
         │     ├─ NewMemoryPersistentJar
         │     ├─ NewHHRequester
         │     ├─ NewAIClient
         │     ├─ LoadProfileData / GetCurrentResume / GetResumeFacts
         │     └─ candidate profile merge and persistence
         ├─ runCandidateCommand
         │  ├─ candidate migration/status/semantic adapters
         │  └─ PostgreSQL, candidate and semantic compatibility adapters
         ├─ runStorageCommand
         │  ├─ career migration plan/source/destination
         │  └─ PostgreSQL repositories and migration adapter
         ├─ runReconcileCommand / runMonitorCommand
         │  ├─ local stores and repository selection
         │  ├─ HHReadSyncService / HHAIResponderReadClient
         │  ├─ CareerDataReconciler
         │  └─ notification and monitor lifecycle
         ├─ runAuditCommand
         │  └─ local career snapshot, consistency and audit projection
         ├─ runDashboardCommand
         │  ├─ dashboard option/lifecycle/server construction
         │  ├─ HHReadSyncService and inbox refresh
         │  ├─ AIReplyOrchestrator
         │  └─ HHWriteGateway behind approval, preflight and dry-run gates
         ├─ runHHCommand
         │  ├─ HH read/sync/workflow CLI adapters
         │  ├─ HHWriteGateway and write-status/preflight adapters
         │  └─ career, conversation, application and pilot projections
         └─ runProfileCommand / runKnowledgeCommandWithConfig
            ├─ candidate profile and communication adapters
            └─ canonical candidate knowledge and PostgreSQL adapters
```

All nodes below the `rootBootstrapHandlers` boundary were root-package
production code. The recursive dependency set was the root compatibility
runtime, including `Config`, `HHAIResponder`, `HHRequester`, `AIClient`,
`Logger`, `MemoryPersistentJar`, JSON/Postgres composition, dashboard
construction, CLI adapters, and the R10/R11/R12 compatibility façades.

## Handler inventory

| Handler | Root entry function before move | Runtime types/composition required | Side effects |
|---|---|---|---|
| `run` | `runDefaultConfig` | `Config`, logger, `HHAIResponder`, HH/AI clients, cookie jar, candidate persistence | HH reads; configured workflow may reach existing guarded HH writes; run-once/recurring lifecycle |
| `candidate` | `runCandidateCommand` | candidate migration/status/semantic adapters, PostgreSQL composition | local/PostgreSQL reads; explicit migration or semantic command writes where already authorized |
| `storage` | `runStorageCommand` | career migration source/destination, PostgreSQL repositories | local reads; PostgreSQL migration/data writes only with explicit `--apply` |
| `reconcile` | `runReconcileCommand` | local stores, career repositories, vacancy analyzer, candidate knowledge | local reads and reconciliation output; mutation remains controlled by existing dry-run flag |
| `monitor` | `runMonitorCommand` | HH read client, sync service, reconciler, notification engine, monitor lifecycle | HH reads and local notification/state persistence; permanently read-only for HH |
| `audit` | `runAuditCommand` | local career snapshot, consistency resolver, audit report | local reads and JSON report output |
| `web` | `runDashboardCommand` | dashboard server/lifecycle, read sync, AI reply orchestration, write gateway | local HTTP server and local dashboard actions; HH writes remain approval/preflight/dry-run gated |
| `hh` | `runHHCommand` | HH read/sync/workflow adapters, repositories, write gateway and pilots | HH reads; any HH mutation remains in existing explicit write gateway paths |
| `profile` | `runProfileCommand` / `runKnowledgeCommandWithConfig` | JSON/canonical candidate profile and knowledge adapters | candidate profile/knowledge local or PostgreSQL writes according to existing command semantics; no implicit HH write |

## R13.2a result

The complete root production implementation was moved as one cohesive
compatibility package to `internal/runtime`. Its package-private tests moved
with it, preserving white-box coverage. The package now exposes one composition
entrypoint:

```go
handlers := runtime.NewHandlers()
code := bootstrap.Run(ctx, args, bootstrap.Env{Handlers: handlers})
```

The root executable is now only the process adapter:

```text
root main.go
└─ internal/bootstrap.Run
   └─ internal/runtime.NewHandlers
```

There is no production import of root `package main` from `internal/runtime`.
The new external-package test verifies that all nine handlers are constructible
through the importable API. Embedded communication, dashboard, and migration
resources were colocated with `internal/runtime`; their contents were kept
byte-for-byte identical.

## Final metrics

| Metric | After R13.2a |
|---|---:|
| Root production Go files | 1 |
| Root `main.go` LOC | 18 |
| `internal/runtime` production Go files | 114 |
| `internal/runtime` production Go LOC | 30,893 |
| `internal/runtime` tests | 65 |
| `go list ./...` package count | 51 |

## Verification

- `gofmt -w .` — pass
- `go test -count=1 ./...` — pass
- `go vet ./...` — pass
- `go build ./...` — pass
- `go test -race ./...` — pass
- `git diff --check` — pass
- no `cmd/hh-ai-responder/main.go` created
- no live HH write performed
