# Career Workflow Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add durable Career Agent runs, review preparations, coherent vacancy read models and a minimal review workspace without changing HH write authorization.

**Architecture:** Keep `internal/runtime/migrations` as the embedded migration authority and synchronize the root mirror. Add typed workflow entities and narrow storage ports under the existing `internal/careeragent` and `internal/ports` boundaries, with JSON and PostgreSQL adapters. Build snapshots and queue ordering as projections over existing canonical repositories; extend the current dashboard API/UI and approval artifact additively.

**Tech Stack:** Go 1.25, standard library, pgx v5, existing JSON atomic-file stores, PostgreSQL migrations, embedded vanilla JavaScript dashboard.

**Spec:** `docs/superpowers/specs/2026-09-22-career-workflow-phase-2-design.md`

## Global Constraints

- `HH_DRY_RUN=true` must block every HH write.
- AI output is advisory and never authorization.
- Unknown critical state is `REVIEW_REQUIRED`.
- `internal/runtime/migrations` is the embedded PostgreSQL migration authority.
- Root `migrations/` remains a byte-for-byte compatibility mirror.
- Do not create canonical duplicate vacancy, application, conversation or candidate truth.
- Do not store cookies, tokens, raw prompts or unrelated private messages.
- Preserve existing CLI flags, environment variables, JSON envelopes, application states and HH transports.
- No generic `agents/` framework, microservices or frontend rewrite.

## Review Focus

- A root-only or embedded-only migration must fail the contract test; Task 1 adds the mirror test.
- Malformed workflow JSON/evidence must fail closed instead of becoming an empty success; Task 2 tests it.
- Repeated run items and preparations must upsert idempotently; Task 2 and Task 3 test duplicates.
- Interrupted runs must never become successful; Task 2 tests recovery.
- Ambiguous but plausible routing must receive bounded advisory analysis while remaining review-only; Task 4 tests it.
- Approval must bind to the exact preparation and cover-letter hash; Task 5 tests stale and changed artifacts.
- Queue order must reflect safety/freshness/application state rather than publication time alone; Task 6 tests ordering.
- Dashboard failures must be read-only and redacted; Task 7 tests API responses and JavaScript syntax.

---

### Task 1: Reconcile migration authorities

**Files:**
- Create: `docs/MIGRATION_AUTHORITY.md`
- Create: `internal/runtime/migration_authority_test.go`
- Create: `migrations/000006_application_attempts.down.sql`
- Create: `migrations/000006_application_attempts.up.sql`
- Create: `migrations/000007_application_attempt_reconciliation.down.sql`
- Create: `migrations/000007_application_attempt_reconciliation.up.sql`
- Create: `migrations/000008_legacy_auto_chat_attempts.down.sql`
- Create: `migrations/000008_legacy_auto_chat_attempts.up.sql`
- Create: `migrations/000009_auto_chat_reconciliation.down.sql`
- Create: `migrations/000009_auto_chat_reconciliation.up.sql`
- Create: `migrations/000010_candidate_semantic_contract.down.sql`
- Create: `migrations/000010_candidate_semantic_contract.up.sql`
- Create: `migrations/000011_vacancy_freshness_review.down.sql`
- Create: `migrations/000011_vacancy_freshness_review.up.sql`
- Create: `migrations/000012_vacancy_provider_enrichment.down.sql`
- Create: `migrations/000012_vacancy_provider_enrichment.up.sql`
- Create: `migrations/000013_vacancy_fingerprint_versions.down.sql`
- Create: `migrations/000013_vacancy_fingerprint_versions.up.sql`
- Create: `migrations/000014_application_attempt_manual_verification.down.sql`
- Create: `migrations/000014_application_attempt_manual_verification.up.sql`
- Test: `internal/runtime/migration_authority_test.go`

**Interfaces:**
- Consumes the existing embedded runner in `internal/runtime/postgres.go`.
- Produces a checked-in root mirror and a deterministic authority contract.

- [ ] **Step 1: Write the failing mirror contract test.** Enumerate `migrations/*.sql` and `internal/runtime/migrations/*.sql`, require identical relative names, byte content, numeric ordering, and every `up` file to have a paired `down` file.
- [ ] **Step 2: Run the focused test to verify RED.** Run `go test ./internal/runtime -run TestMigrationAuthority -count=1`; it must fail because root versions 6–14 are absent.
- [ ] **Step 3: Copy the existing embedded versions 6–14 into the root mirror.** Do not edit their SQL contents and do not change versions 1–5.
- [ ] **Step 4: Add `docs/MIGRATION_AUTHORITY.md`.** State that `internal/runtime/migrations` is embedded/authoritative, root `migrations` is a synchronized compatibility mirror, new migrations must update both, and existing `schema_migrations` rows remain valid.
- [ ] **Step 5: Run the contract and existing migration tests.** Run `go test ./internal/runtime -run 'TestMigrationAuthority|Test.*Migration' -count=1`; expect PASS.
- [ ] **Step 6: Commit the isolated migration reconciliation.** Use `git add` only for the mirror files, contract test and documentation.

### Task 2: Add durable workflow domain types and repositories

**Files:**
- Create: `internal/careeragent/workflow.go`
- Create: `internal/careeragent/workflow_test.go`
- Create: `internal/ports/career_workflow.go`
- Create: `internal/adapters/storage/json/career_workflow.go`
- Create: `internal/adapters/storage/json/career_workflow_test.go`
- Create: `internal/adapters/storage/postgres/career_workflow.go`
- Create: `internal/adapters/storage/postgres/career_workflow_test.go`
- Create: `internal/runtime/migrations/000015_career_workflow.down.sql`
- Create: `internal/runtime/migrations/000015_career_workflow.up.sql`
- Create: `migrations/000015_career_workflow.down.sql`
- Create: `migrations/000015_career_workflow.up.sql`
- Modify: `internal/runtime/postgres.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/flags.go`
- Modify: `internal/config/defaults.go`
- Modify: `example.env`
- Modify: `README.md`

**Interfaces:**
- Consumes the existing `careeragent.AgentRun`, canonical vacancy IDs and candidate version semantics.
- Produces `AgentRunItem`, `ApplicationPreparation`, `RunQuery`, `PreparationQuery` and a narrow `ports.CareerWorkflowStore`.

- [ ] **Step 1: Write domain validation tests.** Cover valid/invalid run status, required terminal timestamps, redacted error fields, item identity, preparation content hash, candidate snapshot identity and stale status.
- [ ] **Step 2: Run `go test ./internal/careeragent -run 'Test.*Workflow|Test.*Preparation' -count=1` and verify RED.** The new types and validators must be absent.
- [ ] **Step 3: Extend `AgentRun` additively.** Preserve existing JSON fields and add run type, result code, summary, error code/summary and created timestamp only where needed; keep `Finish` fail-closed and `RedactAgentError` as the only stored error path.
- [ ] **Step 4: Implement preparation and item validation.** Hash exact cover-letter content with SHA-256, reject mismatches, and expose a deterministic input fingerprint without storing raw prompts.
- [ ] **Step 5: Write the SQL migration.** Add `agent_runs`, `agent_run_items` and `application_preparations` with primary keys, unique `(run_id, vacancy_id)` item identity, unique preparation input identity, JSONB evidence fields, timestamps and checks for non-empty status values. Do not alter canonical tables.
- [ ] **Step 6: Run the migration SQL contract test to verify RED, then add version 15 to both authorities.** Require additive tables, paired rollback and no secret/prompt columns.
- [ ] **Step 7: Implement the JSON store.** Use a versioned private atomic file, strict decoding, redacted validation, stable upsert keys, deterministic list ordering and explicit malformed-file errors. Persist runs/items/preparations together so a retry cannot create duplicates.
- [ ] **Step 8: Implement the PostgreSQL store.** Use parameterized queries, transactions for each upsert, `ON CONFLICT` on the stable keys, strict JSON decoding into typed values, and a recovery update that changes stale `running` rows to `failed` with `INTERRUPTED`.
- [ ] **Step 9: Add repository tests.** Cover JSON duplicate upsert, malformed JSON/evidence, interrupted recovery, deterministic ordering and PostgreSQL query behavior with the existing database opt-in convention.
- [ ] **Step 10: Add configuration wiring.** Add a workflow-store path with a default beside `CareerAgentResultPath`, preserve CLI-over-env precedence, validate empty/invalid paths, document `HH_CAREER_AGENT_WORKFLOW` and keep JSON default behavior unchanged.
- [ ] **Step 11: Run focused tests.** Run `go test ./internal/careeragent ./internal/adapters/storage/json ./internal/adapters/storage/postgres ./internal/runtime -run 'Test.*Workflow|Test.*Migration' -count=1`.

### Task 3: Wire workflow storage without HH capability

**Files:**
- Modify: `internal/runtime/runtime.go`
- Modify: `internal/runtime/postgres.go`
- Modify: `internal/runtime/career_agent_command.go`
- Modify: `internal/runtime/career_agent_command_test.go`
- Modify: `internal/runtime/career_agent_accounting_test.go`
- Modify: `internal/runtime/career_agent_events.go`
- Modify: `internal/runtime/career_agent_pipeline_test.go`
- Modify: `internal/runtime/application_processing.go`
- Modify: `internal/runtime/application_processing_compat.go`
- Create: `internal/runtime/career_workflow_compat.go`

**Interfaces:**
- Consumes `ports.CareerWorkflowStore` and existing `CareerAgentRunReport` events.
- Produces durable run lifecycle and per-vacancy accounting while leaving `HHWriteGateway` and `submitPreparedApplication` unchanged.

- [ ] **Step 1: Write a failing command test.** Start a shadow run with a fake workflow store, assert `StartRun` occurs before processing, every terminal vacancy has one item, and a failed run is persisted when processing returns an error.
- [ ] **Step 2: Run the focused command test and verify RED.** Run `go test ./internal/runtime -run TestCareerAgent.*Durable -count=1`.
- [ ] **Step 3: Add an optional workflow store dependency to `HHAIResponder` and command composition.** Select JSON or PostgreSQL using the already selected backend; do not instantiate a second HH client or writer.
- [ ] **Step 4: Start and finish the run around the existing processing loop.** On context cancellation, setup failure or storage failure, finish with `FAILED`/`PARTIAL` and a redacted error; never call `Finish(SUCCESS)` for an incomplete run.
- [ ] **Step 5: Persist each `CareerAgentVacancyResult` as an `AgentRunItem`.** Use `(run ID, vacancy ID)` as the idempotency key and store only bounded evidence/decision fields.
- [ ] **Step 6: Persist a preparation immediately after safe local preparation succeeds and before any existing submission method is reached.** The store write is telemetry/artifact persistence only and cannot change the submission decision.
- [ ] **Step 7: Add interruption recovery at startup/read boundary.** Recover stale running runs before listing them for the dashboard.
- [ ] **Step 8: Add idempotency and dry-run regression tests.** Assert two identical shadow runs do not duplicate run items or preparations, `HH_DRY_RUN=true` still blocks every writer, and no approval/nonce is created by telemetry.

### Task 4: Fix ambiguous resume routing and bounded AI analysis

**Files:**
- Modify: `internal/careeragent/model.go`
- Modify: `internal/careeragent/router_role_family.go`
- Modify: `internal/careeragent/resume_route.go`
- Create: `internal/careeragent/advisory_route_test.go`
- Modify: `internal/runtime/application_processing.go`
- Modify: `internal/runtime/career_agent_pilot.go`
- Modify: `internal/runtime/career_agent_pilot_search_test.go`
- Modify: `internal/runtime/career_agent_pipeline_test.go`
- Create: `internal/runtime/career_agent_route_regression_test.go`

**Interfaces:**
- Consumes existing deterministic `RouteDecision`, `ResumeProfile`, `AlternativeScores` and candidate activation paths.
- Produces an advisory-only candidate selection for plausible ambiguity; canonical route status remains `REVIEW_REQUIRED`.

- [ ] **Step 1: Write failing route regression tests.** Cover Technical Support, Python/Django Backend, Automation/Integration, unrelated vacancy, ambiguous supported role families, and unknown hard requirement. Assert ambiguous plausible cases call AI once but do not become `MATCH` or create a write action.
- [ ] **Step 2: Run the focused tests and verify RED.** Run `go test ./internal/runtime -run 'Test.*Route|Test.*Ambiguous|Test.*PilotSearch' -count=1`; the current route gate must show zero AI calls for ambiguity.
- [ ] **Step 3: Add a deterministic advisory-candidate helper.** Select only an enabled candidate from `AlternativeScores` with no hard blockers and a supported provider identity; return no candidate for low evidence, out-of-scope or no-suitable routes.
- [ ] **Step 4: Update the application-processing route gate.** Allow bounded AI for `ROUTE_AMBIGUOUS` when the helper returns a candidate; keep `FinalDecision=REVIEW_REQUIRED`, do not increment write-ready state, and preserve existing canary restrictions.
- [ ] **Step 5: Update pilot search.** Activate the advisory candidate for analysis only, preserve route evidence and review reason, and ensure explicit operator resume/provider validation still runs before any preparation.
- [ ] **Step 6: Run the route regression and existing pilot suites.** Run `go test ./internal/careeragent ./internal/runtime -run 'Test.*Route|Test.*CareerAgentPilot|Test.*PilotSearch' -count=1`.

### Task 5: Bind preparations to exact approval artifacts

**Files:**
- Modify: `internal/runtime/hh_api_application.go`
- Modify: `internal/runtime/hh_api_approval_export.go`
- Modify: `internal/runtime/hh_api_application_test.go`
- Modify: `internal/runtime/career_agent_pilot.go`
- Modify: `internal/runtime/career_agent_command.go`
- Create: `internal/runtime/application_preparation_compat.go`
- Create: `internal/runtime/application_preparation_test.go`

**Interfaces:**
- Consumes `careeragent.ApplicationPreparation` and existing `APIApplicationApproval` validation/nonce contracts.
- Produces optional `PreparationID`/`PreparationHash` approval provenance while accepting old approval JSON without those fields.

- [ ] **Step 1: Write failing approval tests.** Require exact preparation ID/hash for new preparation-backed approvals; reject stale preparation, changed letter hash, changed candidate version and wrong vacancy; accept a legacy approval without preparation fields under the old validation path.
- [ ] **Step 2: Run `go test ./internal/runtime -run 'Test.*PreparationApproval|TestValidateAPIApplicationApproval' -count=1` and verify RED.**
- [ ] **Step 3: Add optional approval provenance fields and validation.** Load the referenced preparation through the local workflow store, compare exact artifact hash and selected resume identity, and reject stale/unknown artifacts before nonce consumption.
- [ ] **Step 4: Attach preparation identity when converting a pilot/application preparation to approval.** Do not alter existing nonce, fresh preflight, reservation or reconciliation checks.
- [ ] **Step 5: Test dry-run and compatibility.** Confirm artifact persistence cannot create approval, approval cannot bypass fresh preflight, and old approval fixtures still pass.

### Task 6: Build vacancy snapshot and ordered review queue

**Files:**
- Create: `internal/careeragent/review_workspace.go`
- Create: `internal/careeragent/review_workspace_test.go`
- Create: `internal/runtime/career_review_read_model.go`
- Create: `internal/runtime/career_review_read_model_test.go`
- Modify: `internal/runtime/dashboard_read_model.go`
- Modify: `internal/runtime/dashboard_views.go`

**Interfaces:**
- Consumes canonical vacancy/application/conversation repositories, vacancy freshness/review state, workflow store, and existing Candidate Knowledge/clarification stores.
- Produces `VacancyReviewSnapshot`, `ReviewQueueItem`, queue ordering and safe next-action text.

- [ ] **Step 1: Write domain projection tests.** Assert snapshot fields reference canonical IDs, route state, preparation state, knowledge requests, application/conversation state and next action without creating a second vacancy truth.
- [ ] **Step 2: Write queue ordering tests.** Use fixtures for clarification-needed, ready review, strong match, AI review, new, dismissed/rejected and closed; assert safety/freshness/application state outrank publication timestamps and ordering is deterministic by vacancy ID tie-breaker.
- [ ] **Step 3: Run focused tests and verify RED.** Run `go test ./internal/careeragent ./internal/runtime -run 'Test.*Snapshot|Test.*ReviewQueue' -count=1`.
- [ ] **Step 4: Implement the pure queue rank and snapshot builder.** Treat malformed optional evidence as a review/read error; do not silently downgrade it to an empty snapshot.
- [ ] **Step 5: Add dashboard loading helpers.** Load workflow data once per request and reuse it for overview, queue and workspace projections; preserve existing dashboard snapshot behavior when workflow storage is unavailable by returning an explicit unavailable diagnostic.
- [ ] **Step 6: Run focused projection tests plus existing dashboard tests.** Run `go test ./internal/careeragent ./internal/runtime -run 'Test.*Snapshot|Test.*ReviewQueue|TestDashboard' -count=1`.

### Task 7: Integrate read-only Career Agent dashboard workspace

**Files:**
- Modify: `internal/runtime/dashboard_server.go`
- Modify: `internal/runtime/dashboard_command.go`
- Modify: `internal/runtime/dashboard_test.go`
- Modify: `web/index.html`
- Modify: `web/app.js`
- Modify: `web/styles.css`
- Create: `internal/runtime/dashboard_career_agent_test.go`

**Interfaces:**
- Consumes the read models from Task 6 and existing dashboard auth/CSRF/read-only routing.
- Produces GET-only queue, vacancy workspace and run timeline endpoints plus minimal UI panels.

- [ ] **Step 1: Write failing API tests.** Cover `GET /api/career/review-queue`, `GET /api/career/vacancies/{id}`, `GET /api/career/runs`, unsupported methods, missing workflow data and redacted run errors.
- [ ] **Step 2: Run the focused dashboard tests and verify RED.** Run `go test ./internal/runtime -run 'TestDashboardCareer|Test.*ReviewQueueAPI' -count=1`.
- [ ] **Step 3: Add route registration and read handlers.** Keep POST protection unchanged; career endpoints must never dispatch HH sync/write actions.
- [ ] **Step 4: Extend dashboard overview metrics.** Show new, analyzing, matched, review required, ready, applied, interview and run status as additive JSON fields.
- [ ] **Step 5: Add the minimal Career Agent UI.** Add a navigation entry/page or existing dashboard section with ordered queue cards, pipeline counts, selected resume/route, matched/missing requirements, preparation/letter hash status, Knowledge Requests, application/conversation state and next safe action.
- [ ] **Step 6: Add run timeline rendering.** Display started/completed/failed state, counts, stage events and already-redacted failures without exposing stored raw evidence.
- [ ] **Step 7: Run API, dashboard and JavaScript checks.** Run `go test ./internal/runtime -run 'TestDashboardCareer|TestDashboard' -count=1`, `node --check web/app.js`, and `node --check internal/runtime/web/app.js` when that path exists.

### Task 8: End-to-end daily run, parity and final verification

**Files:**
- Modify only files required by focused test failures from Tasks 1–7.
- Create: `internal/runtime/career_workflow_parity_test.go`
- Create: `internal/runtime/career_agent_phase2_regression_test.go`

**Interfaces:**
- Consumes the completed workflow store, run command, preparation artifact, snapshot, queue and dashboard contracts.
- Produces the verified Phase 2 vertical slice with no new HH write capability.

- [ ] **Step 1: Write end-to-end regression fixtures.** Use mocked HH reads and AI endpoints; assert discover → route → advisory analysis → preparation → durable run/item accounting, including one terminal outcome per processed vacancy.
- [ ] **Step 2: Add JSON/PostgreSQL parity coverage.** For the same run/items/preparation fixtures, compare normalized read models, ordering, idempotent replay and stale/recovery behavior. Keep live PostgreSQL tests opt-in through `POSTGRES_TEST_DATABASE_URL`.
- [ ] **Step 3: Run focused regression tests and verify RED before final integration.** Run `go test ./internal/runtime -run 'TestCareerAgentPhase2|TestCareerWorkflowParity' -count=1`.
- [ ] **Step 4: Implement only the minimum integration fixes.** Ensure default `career-agent run`/`autopilot` remains shadow/read-analyze-prepare, explicit canary behavior remains capped, and every processed vacancy has an accounting outcome.
- [ ] **Step 5: Run the complete required verification.** Run:
  `gofmt -w .`
  `go test ./...`
  `go test -race ./...`
  `go vet ./...`
  `go build ./...`
  `git diff --check`
  `node --check web/app.js`
  `node --check internal/runtime/web/app.js` when present.
- [ ] **Step 6: Inspect the final diff and repository status.** Confirm no secrets, cookies, tokens, generated private reports or unrelated pre-existing changes were added; verify HH write paths remain unchanged except for exact approval provenance validation.
- [ ] **Step 7: Commit implementation changes in logical commits.** Keep migration, storage, workflow, dashboard and verification changes reviewable and do not include the user’s existing Phase 1 dirty files unless a focused fix requires them.
