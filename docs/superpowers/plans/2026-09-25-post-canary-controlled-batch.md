# Post-Canary Controlled Batch Applications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the first reconciled canary across PostgreSQL projections, suppress already-applied preparation attention, and add a safe explicit `hh-api apply-batch` command capped at three separately approved applications.

**Architecture:** Preserve the existing application attempt, preparation, application, conversation, reconciliation, and `HHWriteGateway` authorities. Add a pure attention actionability predicate and refactor the existing single API apply path around one reusable controlled-application executor; batch orchestration supplies explicit approvals, fresh per-item preflight, sequential execution, and stop rules without introducing a second write path or application state machine.

**Tech Stack:** Go, existing `internal/runtime` orchestration, `internal/usecase/applicationsubmission`, `internal/usecase/hhwritegateway`, PostgreSQL and JSON repository contracts, `httptest.Server`, Go race detector.

**Spec:** `docs/superpowers/specs/2026-09-25-post-canary-controlled-batch-design.md`

## Global Constraints

- The canonical production storage for this workflow is PostgreSQL, selected by the current production configuration.
- `HHWriteGateway` remains the only HH mutation boundary.
- Daily Career Agent and scheduler paths remain read-only and never receive batch-write capability.
- A batch accepts only explicit approval file paths, from one through three items.
- Every item is freshly preflighted immediately before its possible POST and is reconciled before the next item.
- A pre-send block may skip to the next explicitly approved item; transport, delivery, or persistence uncertainty stops the run without retry.
- `HH_DRY_RUN=true` performs validation and produces an execution plan with zero HH writes.
- This implementation performs no live batch POST.
- The original dirty checkout `/Users/Yaroslav/Documents/dev/hh-ai-responder` remains untouched.

## Review Focus

1. An approval that is valid at batch start becomes duplicate or stale before item execution; the item must block before transport and leave its nonce reusable.
2. A provider call returns an uncertain outcome after transport; the batch must stop and never execute later approvals.
3. Two concurrent invocations use the same approval; at most one may consume the nonce and reach transport.
4. PostgreSQL and JSON reload normalize preparation/application data differently; actionability and binding must remain deterministic without migrating historical rows.
5. A valid ready preparation has no confirmed application; it must remain visible in `application_ready` rather than being over-suppressed.

---

### Task 1: Add deterministic post-canary attention actionability

**Files:**
- Modify: `internal/runtime/attention_queue.go`
- Create: `internal/runtime/attention_queue_test.go`
- Modify: `internal/runtime/dashboard_career_agent_test.go` only if an existing fixture is the smallest integration coverage

**Interfaces:**
- Consumes: `dashboardSnapshot.applications`, `dashboardSnapshot.preparations`, and existing `application.JobApplication` status/source fields.
- Produces: `preparationAttentionActionable(preparation careeragent.ApplicationPreparation, applications []JobApplication) bool`, used by `DashboardServer.buildAttentionQueue`.

- [ ] **Step 1: Write the failing tests**

Add table-driven tests for the pure helper and queue projection:

```go
func TestPreparationAttentionIsSuppressedAfterConfirmedApplication(t *testing.T) {
	preparation := readyPreparationForVacancy(137609053)
	applications := []JobApplication{{
		ID: "app-1", VacancyID: 137609053, Source: ApplicationSourceHH,
		Status: ApplicationApplied, ExternalID: "hh-negotiation-1",
	}}
	if preparationAttentionActionable(preparation, applications) {
		t.Fatal("confirmed application left preparation actionable")
	}
}

func TestUnappliedReadyPreparationRemainsActionable(t *testing.T) {
	preparation := readyPreparationForVacancy(137609053)
	if !preparationAttentionActionable(preparation, nil) {
		t.Fatal("unapplied ready preparation was suppressed")
	}
}
```

Define `readyPreparationForVacancy` in the test file with a valid candidate identity, `VacancyID` set from its argument, `Status=careeragent.PreparationStatusReady`, non-zero `CreatedAt`/`UpdatedAt`, and a valid `InputFingerprint`; keep all fixture values synthetic.

Also cover an application with a lower non-terminal status, a different vacancy, and a confirmed conversation-linked application. The expected rule is exact `VacancyID` plus application status at least `StatusApplied`; no preparation mutation occurs.

- [ ] **Step 2: Run the focused tests to verify RED**

Run: `go test ./internal/runtime -run 'Test(PreparationAttention|UnappliedReadyPreparation)' -count=1`

Expected: FAIL because the helper is not defined and the current queue always emits every ready preparation.

- [ ] **Step 3: Implement the minimal suppression predicate**

Implement `preparationAttentionActionable` as a pure scan over applications. In `buildAttentionQueue`, keep the existing `PreparationStatusReady` check, then skip only when the helper returns false. Do not change preparation persistence, status, timestamps, or historical workflow rows.

- [ ] **Step 4: Run focused and existing attention tests**

Run: `go test ./internal/runtime -run 'Test(PreparationAttention|DashboardCareerAgent|Attention)' -count=1`

Expected: PASS, with no change to unrelated attention categories.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/attention_queue.go internal/runtime/attention_queue_test.go internal/runtime/dashboard_career_agent_test.go
git commit -m "fix: suppress applied preparations from attention"
```

### Task 2: Refactor one controlled API application into a reusable executor

**Files:**
- Modify: `internal/runtime/hh_api_application.go`
- Modify: `internal/runtime/hh_api_command.go`
- Modify: `internal/runtime/hh_api_application_test.go`
- Modify: `internal/runtime/hh_api_command_test.go`

**Interfaces:**
- Consumes: `APIApplicationApproval`, `HHAPICommandDeps`, `VacancyPreflight`, `applicationattempt.Store`, and existing `reconcileControlledAPIApplication`.
- Produces: an internal `controlledAPIApplicationService` that owns one configured `hhwritegateway.Service` and exposes `Execute(ctx context.Context, approvalPath string, now time.Time) (controlledAPIApplicationResult, error)`; the existing single `hh-api apply` command becomes a thin parser/composition wrapper around it.

- [ ] **Step 1: Write failing reuse and limit tests**

Add a fake-provider test proving a service can execute two safe submissions sequentially through one gateway and that a gateway configured with `MaxWritesPerRun=3` accepts three but rejects a fourth before provider transport. Add a regression test that single-apply dry-run still performs zero transport calls and never consumes the approval nonce.

- [ ] **Step 2: Run focused tests to verify RED**

Run: `go test ./internal/runtime -run 'Test(ControlledAPIApplication|HHAPIApply)' -count=1`

Expected: FAIL because the reusable service and shared gateway composition do not exist.

- [ ] **Step 3: Extract common controlled execution**

Move only the shared logic from `runHHAPIApply` into the service: load/validate approval, validate preparation binding, fresh `apiVacancyPreflightWithSource`, validate `validateControlledAPIApplicationPreflight`, cover-letter requirement, optional dry-run preview, nonce consumption only immediately before transport, application submission through `applicationsubmission.Service`, and exact reconciliation. The service must return whether transport was attempted, the application result class, final reconciliation outcome, attempt ID, and error classification so batch stop rules do not infer state from strings.

Keep single apply’s `MaxWritesPerRun=1` option. The service constructor must accept the already-created gateway/store/client dependencies so the batch can share one gateway; it must not create a direct HH HTTP writer outside the existing `apiApplicationExecutor` and gateway boundary.

- [ ] **Step 4: Run existing API application tests**

Run: `go test ./internal/runtime -run 'Test(HHAPI|ReconcileControlledAPIApplication|PreparationApproval)' -count=1`

Expected: PASS with unchanged single-apply output and exactly the existing dry-run/write semantics.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/hh_api_application.go internal/runtime/hh_api_command.go internal/runtime/hh_api_application_test.go internal/runtime/hh_api_command_test.go
git commit -m "refactor: share controlled application execution"
```

### Task 3: Add explicit bounded batch domain and argument validation

**Files:**
- Create: `internal/runtime/hh_api_batch.go`
- Create: `internal/runtime/hh_api_batch_test.go`
- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/parse_test.go`
- Modify: `internal/runtime/cli_help.go`

**Interfaces:**
- Consumes: `controlledAPIApplicationService.Execute`, `APIApplicationApproval`, and explicit approval paths.
- Produces: `batchMaxApplications = 3`, `BatchRunStatus`, `BatchItemStatus`, `BatchApplicationRun`, `parseHHAPIApplyBatchArgs(args []string) ([]string, error)`, and `runHHAPIApplyBatch(ctx, args, cfg, stdout, stderr, deps) error`.

- [ ] **Step 1: Write failing parser and stop-rule tests**

Add tests for:

```go
func TestParseHHAPIApplyBatchArgsRequiresOneToThreeExplicitFiles(t *testing.T) {}
func TestParseHHAPIApplyBatchArgsRejectsDuplicatePathAndMoreThanThree(t *testing.T) {}
func TestBatchStopsAfterUncertainTransport(t *testing.T) {}
func TestBatchContinuesAfterPreSendBlock(t *testing.T) {}
```

The tests must assert provider-call order and count, not just returned status. Four paths and duplicate vacancy IDs must fail before any provider or gateway call. A pre-send block on item two may allow item three; an uncertain item two must leave item three uncalled.

- [ ] **Step 2: Run batch tests to verify RED**

Run: `go test ./internal/runtime ./internal/cli -run 'Test(ParseHHAPIApplyBatch|Batch)' -count=1`

Expected: FAIL because the batch command, parser, statuses, and executor are absent.

- [ ] **Step 3: Implement typed batch orchestration**

Parse repeated `--approval-file` flags only. Require one through three values, reject normalized duplicate paths, load and validate every approval identity before the first write, and reject duplicate `VacancyID` values before constructing the execution loop. Do not discover files from directories or globs.

Construct one API client, one attempt store, one audit sink, one reconciliation reader, and one `hhwritegateway.Service` with `WriteEnabled=cfg.HHWriteEnabled`, `DryRun=cfg.DryRun`, `MaxWritesPerRun=3`, and `MaxWritesPerDay=cfg.HHMaxWritesPerDay`. For each item call the shared executor in input order. Mark pre-transport blocks as item results and continue; stop with `STOPPED_UNCERTAIN` after any transport-attempted outcome that is not confirmed. In dry-run, emit the complete plan and keep actual writes at zero.

- [ ] **Step 4: Integrate CLI parsing and help**

Recognize `apply-batch` under `hh-api`, route it through `runHHAPICommandWithDeps`, and document the repeated explicit flag in help. Preserve all existing `apply` argument validation and command behavior.

- [ ] **Step 5: Run focused tests to verify GREEN**

Run: `go test ./internal/runtime ./internal/cli -run 'Test(ParseHHAPIApplyBatch|Batch|HHAPIApply)' -count=1`

Expected: PASS; the fake provider observes sequential calls, at most three writes, reconciliation before the next item, and no call after uncertainty.

- [ ] **Step 6: Commit**

```bash
git add internal/runtime/hh_api_batch.go internal/runtime/hh_api_batch_test.go internal/runtime/hh_api_application.go internal/runtime/hh_api_application_test.go internal/runtime/hh_api_command.go internal/cli/parse.go internal/cli/parse_test.go internal/runtime/cli_help.go
git commit -m "feat: add bounded controlled application batch"
```

### Task 4: Complete batch safety and durability tests

**Files:**
- Modify: `internal/runtime/hh_api_batch_test.go`
- Modify: `internal/runtime/hh_api_command_test.go`
- Modify: `internal/runtime/hh_api_approval_export_test.go`
- Modify: `internal/runtime/hh_read_sync_test.go`
- Modify: `internal/runtime/dashboard_career_agent_test.go`
- Modify: `internal/runtime/application_preparation_test.go` if needed for consumed/replay binding coverage

**Interfaces:**
- Consumes: the Task 1 attention predicate and Task 3 batch executor.
- Produces: regression coverage for reload, sync idempotency, conversation relation, duplicate/nonce replay, concurrent same-batch execution, and dry-run zero-write behavior.

- [ ] **Step 1: Add failing tests for remaining review-focus inputs**

Add tests that:

1. reload a confirmed attempt and its reconciliation evidence from the repository;
2. run application sync twice and assert one `JobApplication` by external HH response identity;
3. retain an exact `ApplicationID`/`ConversationID`/`HHConversationID` relation and accept no conversation;
4. reject a consumed approval nonce before the fake provider is called;
5. run two concurrent batch attempts with one approval and assert one provider call;
6. assert a stale/duplicate item is `BLOCKED_PRE_SEND` or `ALREADY_APPLIED_PRE_SEND` while a later safe item may proceed;
7. assert `HH_DRY_RUN=true` produces `WOULD_ATTEMPT` output and zero provider calls.

- [ ] **Step 2: Run the new tests to verify RED**

Run: `go test ./internal/runtime -run 'Test(.*Reload|.*Sync.*Idempot|.*Conversation|.*Nonce|.*Concurrent|.*DryRun|.*Batch)' -count=1`

Expected: the newly specified regression cases fail against the incomplete behavior.

- [ ] **Step 3: Implement only the missing guards or test seams**

Use existing stores and typed errors. Do not add a second application table, migration, retry loop, or fake conversation creator. If a concurrency failure is found, fix the shared nonce/gateway reservation boundary rather than adding a batch-local mutex that cannot protect another process.

- [ ] **Step 4: Run the focused regression suite**

Run: `go test ./internal/runtime -run 'Test(.*Reload|.*Sync.*Idempot|.*Conversation|.*Nonce|.*Concurrent|.*DryRun|.*Batch)' -count=1`

Expected: PASS with provider call counts and persisted identities matching the assertions.

- [ ] **Step 5: Commit**

```bash
git add internal/runtime/hh_api_batch_test.go internal/runtime/hh_api_command_test.go internal/runtime/hh_api_approval_export_test.go internal/runtime/hh_read_sync_test.go internal/runtime/dashboard_career_agent_test.go internal/runtime/application_preparation_test.go
git commit -m "test: cover post-canary and batch safety"
```

### Task 5: Run canonical PostgreSQL Phase A validation and write the report

**Files:**
- Create: `docs/validation/VALIDATION_POST_CANARY_2026-09-25.md`
- No production code changes unless a failing Phase A assertion has a preceding RED test in Tasks 1 or 4

**Interfaces:**
- Consumes: the canonical PostgreSQL stores, existing HH read-sync commands, reliability CLI, Career Agent daily read-only flow, and the new attention predicate.
- Produces: a redacted operational validation report; no HH write capability.

- [ ] **Step 1: Capture the canonical backend and baseline**

From the isolated branch, record `git status`, `git rev-parse HEAD`, configured backend without printing `DATABASE_URL`, and the existing fingerprint-fix commit. Confirm the original dirty checkout is unchanged.

- [ ] **Step 2: Inspect exactly one canary attempt**

Run the PostgreSQL-backed read-only command:

```sh
go run ./cmd/hh-ai-responder hh reliability applications --all --vacancy-id 137609053 --json
```

Assert one attempt for vacancy `137609053`, resume/provider identity `b29ec17dff103a8bc60039ed1f356c62486c37`, transport attempted, success class, confirmed reconciliation, and no second provider dispatch attempt. If more than one real attempt exists, record `POST_CANARY_VALIDATION_FAILED` and stop Phase B.

- [ ] **Step 3: Run application sync twice**

With browser/API reads configured and HH writes disabled, run `hh sync applications` twice against PostgreSQL. Compare canonical application counts and external IDs. Then run `hh sync conversations` once; accept no conversation as valid and never synthesize one.

- [ ] **Step 4: Refresh Communication Agent read-only**

Run `go run ./cmd/hh-ai-responder career-agent daily --json` with the verified browser read transport, `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, auto-apply/auto-chat/resume-touch/job-status disabled, and PostgreSQL selected. Assert no automatic reply, interview, test, offer, or employer-message write. Any imported employer message is classified as untrusted review work only.

- [ ] **Step 5: Inspect attention and repeat-send state**

Inspect the Career Agent attention breakdown and PostgreSQL preparation. Assert no actionable `application_ready`, `vacancy_review`, or duplicate preparation attention for `137609053`; assert the historical preparation remains present. Verify fresh HH duplicate state is YES and offline approval nonce reuse fails before transport. Reload the same stores in a second process and repeat the assertions.

- [ ] **Step 6: Write the validation report and verdict**

Record AttemptID, application identity, conversation identity or `NONE`, reconciliation evidence, duplicate/provider negotiation evidence, sync idempotency, attention categories, nonce protection, restart results, and HH write count. Use `POST_CANARY_PASS` when no product bug is found or `POST_CANARY_FIXED_AND_PASS` when the suppression/fingerprint fixes are proven. Use `POST_CANARY_VALIDATION_FAILED` only for a failed invariant and do not proceed to batch implementation in that case.

- [ ] **Step 7: Commit**

```bash
git add docs/validation/VALIDATION_POST_CANARY_2026-09-25.md
git commit -m "docs: record post-canary validation"
```

### Task 6: Document the operator workflow and run full verification

**Files:**
- Create: `docs/CONTROLLED_BATCH_APPLICATIONS.md`
- Modify: `internal/runtime/cli_help.go` if help text needs final examples

**Interfaces:**
- Consumes: the final CLI and typed batch result from Tasks 2–4.
- Produces: operator documentation stating that approvals are human-created, daily is read-only, batch is explicit, max batch is three, gateway is the sole mutation boundary, and uncertainty stops without retry.

- [ ] **Step 1: Write documentation tests/checks**

Use a shell assertion or Go documentation contract test to verify the document includes `HHWriteGateway`, `Max batch = 3`, `HH_DRY_RUN=true`, `uncertain`, `daily`, and `no live batch POST`.

- [ ] **Step 2: Write the operator documentation**

Document approval-file creation as an explicit prior human action, exact command syntax, dry-run output, per-item preflight, sequential reconciliation, stop statuses, daily limit behavior, replay protection, and the prohibition on using daily/scheduler commands for sends. Do not include real tokens, cookies, or private payloads.

- [ ] **Step 3: Run the complete verification suite**

Run:

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

Expected: all commands pass, no live HH batch command is run, and the original dirty checkout remains unchanged.

- [ ] **Step 4: Commit**

```bash
git add docs/CONTROLLED_BATCH_APPLICATIONS.md internal/runtime/cli_help.go
git commit -m "docs: document controlled application batches"
```

## Final delivery checklist

- [ ] Run a fresh self-review of the whole branch against the spec and this plan.
- [ ] Confirm `git log` contains separate fingerprint, attention, batch, tests, validation, and docs commits.
- [ ] Confirm no live batch POST occurred and no first live batch is attempted.
- [ ] Report `POST_CANARY_PASS` or `POST_CANARY_FIXED_AND_PASS`, batch safety test results, write invariants, commit SHAs, and that the dirty checkout was unchanged.
