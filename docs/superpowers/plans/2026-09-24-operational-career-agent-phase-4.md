# Operational Career Agent Phase 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Ship one durable, read-only `career-agent daily` workflow used by CLI, dashboard, and scheduler, with PostgreSQL parity, deterministic attention queue, idempotency, partial-failure telemetry, and safety coverage.

**Architecture:** Add one typed application service over the existing vacancy and communication use cases. Keep domain truth in current vacancy/application/conversation/candidate stores and derive `AttentionItem` from those records plus preparations and communication work items. Add only additive PostgreSQL parity storage for drafts/clarifications where existing concrete JSON dependencies cannot safely be generalized.

**Tech Stack:** Go 1.25, standard library, existing pgx/PostgreSQL adapters, existing JSON stores, existing scheduler, embedded vanilla JavaScript dashboard, `httptest` fixtures.

**Spec:** `docs/superpowers/specs/2026-09-24-operational-career-agent-phase-4-design.md`

## Global Constraints

- Existing Phase 1–3 canonical state machines remain authoritative.
- Daily orchestration receives read/sync services only; `HHWriteGateway` is outside the service.
- `HH_DRY_RUN=true` remains an unconditional write block.
- Unknown or critical incomplete state is `REVIEW_REQUIRED`.
- Employer messages remain untrusted input and AI never authorizes writes or authoritative priority.
- Existing dirty/untracked user files are preserved and excluded from Phase 4 commits unless required.
- Additive paired migrations only; JSON compatibility and startup behavior remain intact.

## Review Focus

- A configured write-enabled process must still produce `HHWrites=0` from daily and never construct an HH writer.
- A second CLI/UI/scheduler trigger while a daily run is running must return the durable existing run rather than overlap.
- A repeated input fingerprint must not duplicate preparations, drafts, clarifications, or communication items.
- One malformed vacancy or failed AI/provider item must result in `PARTIAL_SUCCESS` with surviving successful artifacts.
- An offer, suspicious message, or unknown candidate fact must remain the highest-priority manual attention item and never become an automatic action.

### Task 1: Typed daily result and non-overlapping orchestration core

**Files:**
- Create: `internal/careeragent/daily.go`
- Create: `internal/careeragent/daily_test.go`
- Create: `internal/runtime/daily_career_agent.go`
- Create: `internal/runtime/daily_career_agent_test.go`
- Modify: `internal/ports/career_workflow.go`

**Interfaces:**
- Produce `careeragent.DailyCareerAgentRun`, `careeragent.DailyCareerAgentSummary`, and `runtime.DailyCareerAgentService`.
- `DailyCareerAgentService.Run(context.Context, time.Time) (careeragent.DailyCareerAgentRun, error)` is the shared application entrypoint.
- Dependencies expose only read/sync vacancy, communication, attention projection, and `ports.CareerWorkflowStore` operations.

- [ ] Write failing tests for stable status/result mapping, partial item errors, deterministic run fingerprint, and no overlapping second invocation.
- [ ] Run `go test ./internal/careeragent ./internal/runtime -run 'TestDailyCareerAgent|TestDailyRun' -count=1` and observe the missing typed API failure.
- [ ] Implement typed summary/status contracts, a process mutex, durable running-run lookup, and redacted failure aggregation.
- [ ] Make the core persist `AgentRun`/`AgentRunItem` through existing upsert contracts without any HH writer dependency.
- [ ] Run focused tests and commit `feat: add daily career agent orchestration core`.

### Task 2: Vacancy and communication adapters over existing use cases

**Files:**
- Create: `internal/runtime/daily_career_agent_adapters.go`
- Modify: `internal/runtime/career_agent_command.go`
- Modify: `internal/runtime/daily_communication_run.go`
- Modify: focused runtime tests

**Interfaces:**
- Adapt the existing read-only vacancy Career Agent report and `RunDailyCommunication` logic into the service from Task 1.
- Preserve existing canonical lifecycle and preparation upserts.
- Return stage counters and redacted per-item failures to the daily core.

- [ ] Add failing integration tests for fresh vacancy, ambiguous vacancy, unknown fact, employer reply, interview, test, offer, rejection, and follow-up.
- [ ] Run the focused integration test and confirm the unified service is not yet wired.
- [ ] Implement adapters that call the existing vacancy pipeline in shadow/read-only mode and the communication refresh/classification path without passing write capability.
- [ ] Ensure AI failures are counted and isolated while deterministic safety decisions remain authoritative.
- [ ] Run `go test ./internal/runtime -run 'TestDailyCareerAgent.*(Fresh|Ambiguous|Communication|Partial)' -count=1` and commit `feat: compose vacancy and communication daily stages`.

### Task 3: Draft and clarification storage interfaces plus JSON parity

**Files:**
- Create: `internal/ports/candidate_workflow.go`
- Modify: `internal/adapters/storage/json/candidate_acquisition.go`
- Modify: `internal/runtime/ai_reply_orchestrator.go`
- Modify: `internal/runtime/candidate_acquisition_compat.go`
- Modify: `internal/runtime/ai_reply_orchestrator.go`, `internal/runtime/conversation_workflow_compat.go`, `internal/runtime/hh_write_gateway.go`, `internal/runtime/hh_read_sync.go`, `internal/runtime/hh_reply_pilot.go`, `internal/runtime/career_monitor.go`, and their existing tests

**Interfaces:**
- Define narrow consumer-owned interfaces for draft and clarification CRUD/upsert operations while preserving JSON constructors and file format.
- Add deterministic upsert methods keyed by draft input fingerprint and clarification gap/relation identity.
- Keep proposal/confirmation state and freshness metadata intact.

- [ ] Add failing JSON restart and duplicate tests for drafts and clarifications, including hashes, proposal IDs, answers, and relation metadata.
- [ ] Run focused JSON tests and verify duplicate records are currently possible through the new service path.
- [ ] Implement interfaces and adapters without changing existing JSON schema/version compatibility.
- [ ] Refactor only consumers that need backend polymorphism; retain compatibility aliases where tests and public runtime contracts depend on them.
- [ ] Run storage, orchestrator, and candidate acquisition tests and commit `refactor: introduce workflow storage contracts`.

### Task 4: Additive PostgreSQL parity and migration contracts

**Files:**
- Create: `migrations/000017_candidate_workflow_storage.up.sql`
- Create: `migrations/000017_candidate_workflow_storage.down.sql`
- Create: `internal/runtime/migrations/000017_candidate_workflow_storage.up.sql`
- Create: `internal/runtime/migrations/000017_candidate_workflow_storage.down.sql`
- Create: `internal/adapters/storage/postgres/candidate_workflow.go`
- Create: `internal/adapters/storage/postgres/candidate_workflow_test.go`
- Modify: `internal/runtime/migration_contract_test.go`
- Modify: `internal/runtime/career_workflow_parity_test.go`

**Interfaces:**
- Implement the Task 3 draft/clarification interfaces using the selected pgx pool/transaction boundary.
- Use additive tables and deterministic unique constraints; do not create a queue table or duplicate application/conversation truth.

- [ ] Write failing migration contract and repository parity tests for restart, replacement, idempotent create, and full metadata round trips.
- [ ] Run focused tests and observe missing migration/repository behavior.
- [ ] Add paired additive schema with safe down migration and synchronized embedded copy.
- [ ] Implement validated pgx upserts and reads with redacted JSONB fields for structured metadata.
- [ ] Run PostgreSQL adapter and migration tests, then commit `feat: add PostgreSQL parity for workflow drafts`.

### Task 5: Deterministic AttentionItem read model

**Files:**
- Create: `internal/careeragent/attention.go`
- Create: `internal/careeragent/attention_test.go`
- Create: `internal/runtime/attention_queue.go`
- Create: `internal/runtime/attention_queue_test.go`
- Modify: `internal/runtime/dashboard_read_model.go`

**Interfaces:**
- Produce `careeragent.AttentionItem` and `AttentionQueue` builder functions over existing authoritative read models.
- Priority order is deterministic and stable for equal priority (`updated_at`, then ID).

- [ ] Write failing tests for all required item types, priority order, stable IDs, and no duplicate follow-up item per interval.
- [ ] Run focused tests and verify the new read model is absent.
- [ ] Implement derived projection from review queue, preparations, inbox communication items, drafts, clarifications, and follow-ups.
- [ ] Return queue after restart by recomputing from persisted sources; do not persist a second queue truth table.
- [ ] Run focused dashboard/read-model tests and commit `feat: add unified career attention queue`.

### Task 6: CLI daily command and stable output

**Files:**
- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/parse_test.go`
- Modify: `internal/runtime/career_agent_command.go`
- Create: `internal/runtime/daily_career_agent_cli.go`
- Create: `internal/runtime/daily_career_agent_cli_test.go`
- Modify: `internal/runtime/handlers.go` and composition helpers

**Interfaces:**
- Parse `career-agent daily`, `career-agent daily --json`, and `career-agent daily --dry-run` using existing config conventions.
- Human output must show the requested counters/result; JSON output must be a stable typed envelope.

- [ ] Add failing parser/output tests for the new subcommand and forced zero-write capability.
- [ ] Run CLI focused tests and observe unknown-subcommand/absent-output failures.
- [ ] Construct the same `DailyCareerAgentService` used by dashboard/scheduler; force read-only mode regardless of global write config.
- [ ] Render human and JSON outputs without raw provider bodies or private prompts.
- [ ] Run CLI tests and commit `feat: expose career-agent daily CLI`.

### Task 7: Dashboard Control Center and manual run API

**Files:**
- Modify: `internal/runtime/dashboard_server.go`
- Modify: `internal/runtime/dashboard_career_agent.go`
- Modify: `internal/runtime/dashboard_command.go`
- Modify: `web/app.js`, `web/index.html`, `web/styles.css`
- Modify: `internal/runtime/web/app.js`, `internal/runtime/web/index.html`, `internal/runtime/web/styles.css`
- Modify: `internal/runtime/dashboard_test.go`, `internal/runtime/dashboard_career_agent_test.go`, `internal/runtime/dashboard_inbox_overview_test.go`

**Interfaces:**
- Add read endpoints for attention, daily status, and latest summary, plus a local POST endpoint for manual daily execution.
- The POST uses the shared service, has no HH write capability, and returns durable run identity/status.

- [ ] Add failing `httptest` cases for attention ordering, running/success/partial status, refresh persistence, and duplicate-click behavior.
- [ ] Run dashboard focused tests and verify routes/UI are absent.
- [ ] Wire service into `DashboardDependencies`, add route validation and request mutex/idempotency behavior.
- [ ] Add Control Center cards and one attention queue while preserving existing routes/components/styles.
- [ ] Run dashboard tests and both `node --check` commands; commit `feat: add career agent control center`.

### Task 8: Existing scheduler integration

**Files:**
- Modify: `internal/runtime/recurring_scheduler.go`
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/config/config.go`, `internal/config/defaults.go`, `internal/config/flags.go`, `internal/config/validation.go`
- Modify: `example.env`, config/scheduler tests

**Interfaces:**
- Add default-off daily scheduler configuration using existing duration/boolean conventions.
- Scheduler invokes the same `DailyCareerAgentService.Run` and relies on its no-overlap guard.

- [ ] Add failing scheduler tests for default-off, cadence, clean shutdown, failed-run continuation, and shared-service invocation.
- [ ] Run focused scheduler/config tests and observe missing config behavior.
- [ ] Implement bounded registration on the existing scheduler loop; do not add business logic to `internal/platform/scheduler`.
- [ ] Ensure failed runs are logged/persisted and the next interval still executes.
- [ ] Run scheduler/config tests and commit `feat: schedule read-only daily career agent runs`.

### Task 9: Idempotency, partial failure, and safety regression suite

**Files:**
- Create: `internal/runtime/daily_career_agent_e2e_test.go`
- Create: `internal/runtime/daily_career_agent_safety_test.go`
- Modify: `internal/runtime/hh_write_gateway_test.go` only if regression coverage needs extension
- Modify: `internal/runtime/daily_workflow_test.go`, `internal/runtime/daily_communication_run_test.go`, `internal/runtime/career_agent_phase2_regression_test.go`, `internal/runtime/hh_write_gateway_test.go`

**Interfaces:**
- Exercise the public daily service, CLI composition, dashboard POST, and scheduler trigger against fixtures.

- [ ] Add failing duplicate-run/restart/partial-failure/zero-write tests before implementation changes.
- [ ] Run the suite and capture expected failures for each missing guarantee.
- [ ] Implement only the minimum fixes needed to make repeated runs idempotent and failure state observable.
- [ ] Prove daily, dashboard, and scheduler cannot call an HH write client; prove approval/preflight/send gates remain unchanged.
- [ ] Run all focused safety/idempotency tests and commit `test: cover operational career agent safety`.

### Task 10: Documentation and final verification

**Files:**
- Modify: `docs/CAREER_AGENT_ARCHITECTURE.md` (required Phase 4 extension; preserve existing content)
- Create: `docs/OPERATIONAL_CAREER_AGENT.md`
- Modify: `README.md` with a concise usage section only if no equivalent exists
- Modify: `example.env` for scheduler settings if Task 8 adds them

**Interfaces:**
- Document the service boundary, CLI, dashboard, scheduler, persistence, queue, troubleshooting, and safety guarantees.

- [ ] Add documentation tests/checks where existing conventions support them.
- [ ] Run `gofmt -w .`.
- [ ] Run `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./...`, `git diff --check`.
- [ ] Run `node --check web/app.js` and `node --check internal/runtime/web/app.js`.
- [ ] Review the diff and confirm unrelated dirty/untracked files, secrets, and real HH writes are absent.
- [ ] Commit `docs: document operational career agent` and record final verification evidence.
