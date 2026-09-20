# RESET-API-3B Manual Review Approval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit, local-only `hh-api approval review` command for safe AI-uncertain manual-review pilots while preserving the automatic MATCH path and every fresh API application safety gate.

**Architecture:** Keep approval conversion in `internal/runtime` beside the existing export path. Add a strict manual-pilot validator and a provenance-rich `APIApplicationApproval` representation; route both automatic and manual artifacts through the existing controlled `hh-api apply` flow. Approval review performs no HH requests, while apply performs the same fresh GET-only provider preflight and durable write gateway flow for both bases.

**Tech Stack:** Go standard library, existing runtime command parser, `crypto/rand` through the repository UUID helper, private atomic JSON artifacts, existing `httptest.Server` fixtures, application-attempt store, write gateway, and targeted reconciliation.

**Spec:** `docs/superpowers/specs/2026-09-21-reset-api-3b-manual-review-approval-design.md`

## Global Constraints

- Manual approval accepts only `MANUAL_REVIEW_BEFORE_SEND` + `REVIEW_REQUIRED` + populated AI assessment with `AIRecommendation == UNCERTAIN`.
- Manual approval rejects non-empty source pilot nonces; it requires `NonceUsedAt == nil` and generates a new nonce only after all validation succeeds.
- Manual approval does not require pilot-bound selected-resume suitability or API `AVAILABLE`; apply must prove those from fresh GET-only state.
- Manual approval must first verify source `ContentHash` is non-empty and equals the exact source pilot letter hash, before any `--letter-file` override is considered.
- Empty reviewed letters are allowed only when the source pilot has `CoverLetterRequired != nil && *CoverLetterRequired == false`; fresh API apply re-checks the provider letter requirement.
- Manual approval never converts `REVIEW_REQUIRED` to `MATCH` and never overrides hard missing, hard unknown, duplicate, test, inactive, or unknown provider state.
- `--letter-file` binds the exact file bytes as the approved letter; omitted input preserves the pilot letter exactly; no AI rewrite occurs.
- Automatic `READY_FOR_EXPLICIT_SEND + MATCH` export and validation remain backward-compatible.
- Apply preserves API-only mutation, fresh preflight, duplicate/suitability/availability/active/can-apply/no-test gates, one mutation per invocation, durable locks, run/day limits, no redirects, no retries after ambiguity, reconciliation, CAPTCHA/manual challenge handling, and one-time nonce use.
- Live nonce consumption is protected by an exclusive cross-process lock on the approval artifact or deterministic adjacent lock file; the consumer re-reads and revalidates the exact artifact while holding the lock, persists `NonceUsedAt` atomically, then releases the lock before continuing toward attempt reservation.
- All tests use local fixtures; no implementation or verification step may issue a real HH POST.

## Review Focus

- A manual pilot without a source nonce must load successfully while an automatic pilot still requires its existing nonce; test both loader modes in Task 1.
- A manual pilot with `AIRecommendation == DO_NOT_APPLY`, missing AI score, or missing recommendation must fail closed; test the table-driven validator in Task 1.
- Provider suitability and API availability must not be required during local approval but must block at fresh apply time; test local approval and fresh `httptest.Server` apply separately in Tasks 1 and 3.
- Exact source pilot content must pass its original hash before an override; exact reviewed letter bytes must survive approval and a post-approval mutation must fail by hash; test non-ASCII/trailing-byte content and empty-letter Policy B in Task 2 and apply validation in Task 3.
- Manual approval must issue a nonce only after validation and consume it before a live mutation without consuming it during dry-run; test construction, reuse rejection, two-process contention, crash-safe consumed state, and dry-run behavior in Task 3.

---

### Task 1: Manual pilot loading, strict eligibility, and approval artifact provenance

**Files:**
- Modify: `internal/runtime/career_agent_pilot.go:loadPilotArtifact` and adjacent private loader helpers.
- Modify: `internal/runtime/hh_api_application.go:APIApplicationApproval` fields and approval constants.
- Modify: `internal/runtime/hh_api_approval_export.go` with manual validation/construction helpers shared by the command.
- Test: `internal/runtime/hh_api_approval_export_test.go`.

**Interfaces:**
- Preserve `loadPilotArtifact(path) (PilotArtifact, error)` for existing automatic export and pilot-send callers; it continues to require the automatic pilot nonce.
- Add a private manual loader path that returns the decoded artifact plus the exact source bytes and permits an empty source nonce, while still rejecting malformed JSON, wrong version, missing vacancy/content identity, and invalid letter state.
- Add strict manual conversion helpers with these responsibilities:
  - `validateManualPilotArtifact(artifact PilotArtifact, reviewedLetter string, now time.Time) (string, error)` validates source content integrity before any override, status, decision, AI score/recommendation, hard blocker lists, pilot-bound preflight facts, identity, freshness, source nonce state, and Policy B letter safety, returning the normalized provider resume ID.
  - `buildManualAPIApplicationApproval(artifact PilotArtifact, providerResumeID, reviewedLetter, pilotHash, nonce string, approvedAt time.Time) APIApplicationApproval` copies provenance and exact reviewed material without changing the final decision.
- Extend `APIApplicationApproval` with optional backward-compatible fields: `ApprovalBasis`, `OperatorApproved`, `OperatorApprovalTimestamp`, `OriginalAIScore`, `OriginalAIRecommendation`, `OriginalAIRecommendationReasons`, `OriginalFinalDecision`, and `PilotArtifactHash`. Existing automatic artifacts may leave these fields at zero values.

- [ ] **Step 1: Write failing tests for nonce policy and strict manual eligibility.**

Add a table-driven test fixture with a fresh manual pilot containing:

```go
func manualPilotFixture(now time.Time) PilotArtifact {
	falseValue := false
	trueValue := true
	score := 82
	return PilotArtifact{
		Version:          pilotArtifactVersion,
		Status:           pilotManualReviewStatus,
		VacancyID:        42,
		SelectedResumeID: "hh-resume-provider-id-resume-provider-7",
		AIScore:          &score,
		AIRecommendation: "UNCERTAIN",
		AIReasons:        []string{"provider eligibility needs operator review"},
		FinalDecision:    "REVIEW_REQUIRED",
		HardMissing:      []string{},
		HardUnknown:      []string{},
		Preflight: PilotPreflightSnapshot{
			ObservedAt: now,
			Active: &trueValue, AlreadyResponded: &falseValue,
			AlreadyRespondedValue: "NO", CanApply: &trueValue,
			TestRequired: &falseValue, CoverLetterRequired: &falseValue,
		},
		CoverLetter:    "Здравствуйте! Готов обсудить интеграции и поддержку API.\n",
		ContentHash:    contentHash("Здравствуйте! Готов обсудить интеграции и поддержку API.\n"),
		PreviewFreshAt: now,
	}
}
```

Cover these individual cases and assert an error for each: `BLOCKED`, `REJECT`, `MATCH`, hard missing, hard unknown, `DO_NOT_APPLY`, nil AI score, empty AI recommendation, required test, unknown test, duplicate, unknown duplicate, inactive vacancy, unknown `CanApply`, false `CanApply`, missing/ambiguous provider resume, stale preview, stale preflight, empty source `ContentHash`, mismatched source `ContentHash`, non-empty source nonce, and non-nil `NonceUsedAt`. Add success assertions showing that missing selected-resume suitability and missing API availability do not fail local manual validation, and that an empty source letter succeeds only with known `CoverLetterRequired=false`.

- [ ] **Step 2: Run the focused tests and verify the new behavior fails.**

Run:

```bash
go test ./internal/runtime -run 'TestManualPilot|TestLoadPilotArtifact' -count=1
```

Expected: compile/test failure because the manual loader, strict validator, and provenance fields do not yet exist.

- [ ] **Step 3: Implement the loader policy and artifact fields.**

Refactor the existing private loader into a raw-reading helper that decodes bounded JSON with `DisallowUnknownFields`, retains the exact source bytes for hashing, and accepts a `requireNonce` boolean. Keep the existing `loadPilotArtifact` wrapper on `requireNonce=true`; add the manual wrapper on `requireNonce=false`. Keep automatic behavior unchanged, including its non-empty source nonce requirement.

Add the JSON fields with `omitempty` where needed so old automatic artifacts still decode and validate. Define the literal basis constant `OPERATOR_MANUAL_REVIEW`. Keep `FinalDecision` as the original decision and set `OriginalFinalDecision` only on manual artifacts. Use the existing `contentHash` helper for cover-letter content and SHA-256 of the exact pilot JSON bytes for `PilotArtifactHash`.

- [ ] **Step 4: Implement strict manual validation and construction.**

Require:

```text
status == MANUAL_REVIEW_BEFORE_SEND
final_decision == REVIEW_REQUIRED
AIScore != nil
AIRecommendation == UNCERTAIN
HardMissing empty
HardUnknown empty
Preflight.ObservedAt fresh
Preflight.Active != nil && *Active == true
Preflight.AlreadyResponded != nil && *AlreadyResponded == false
Preflight.AlreadyRespondedValue == NO
Preflight.CanApply != nil && *CanApply == true
Preflight.TestRequired != nil && *TestRequired == false
Nonce == ""
NonceUsedAt == nil
```

Require a trusted provider resume ID through the existing `pilotProviderResumeID` function, require a positive vacancy ID, require preview/preflight freshness within `apiApplicationApprovalMaxAge`, and first require a non-empty source `ContentHash` equal to `contentHash(artifact.CoverLetter)`. For a non-empty reviewed letter, run `validatePilotCoverLetter`; for an empty reviewed letter, require `artifact.Preflight.CoverLetterRequired != nil && *artifact.Preflight.CoverLetterRequired == false` and do not call the validator. Do not inspect or require `SelectedResumeSuitable` or `Available` in this function.

After validation returns successfully, call the existing cryptographically secure `generateUUIDv4` from the command layer and pass that fresh value to the constructor. The constructor must set `Status` to `MANUAL_REVIEW_BEFORE_SEND`, `FinalDecision` and `OriginalFinalDecision` to `REVIEW_REQUIRED`, `ApprovalBasis` to `OPERATOR_MANUAL_REVIEW`, `OperatorApproved` to `true`, the injected approval timestamp, all original AI fields, the exact reviewed letter, recomputed content hash, source pilot hash, selected/provider resume IDs, and the new nonce. It must never copy `artifact.Nonce`.

- [ ] **Step 5: Run focused tests and commit the artifact foundation.**

Run:

```bash
go test ./internal/runtime -run 'TestManualPilot|TestLoadPilotArtifact|TestPilotArtifactToAPIApplicationApproval' -count=1
```

Expected: PASS, including automatic export regression coverage. Commit:

```bash
git add internal/runtime/career_agent_pilot.go internal/runtime/hh_api_application.go internal/runtime/hh_api_approval_export.go internal/runtime/hh_api_approval_export_test.go
git commit -m "feat: validate manual review pilot artifacts"
```

### Task 2: Explicit `hh-api approval review` command and exact letter binding

**Files:**
- Modify: `internal/cli/parse.go` and `internal/cli/parse_test.go`.
- Modify: `internal/runtime/hh_api_command.go` to pass command dependencies.
- Modify: `internal/runtime/hh_api_approval_export.go` for command parsing/execution.
- Modify: `internal/runtime/cli_help.go`.
- Modify: `README.md` and `docs/architecture/CLI_CONTRACT.md`.
- Test: `internal/runtime/hh_api_approval_export_test.go` and `internal/runtime/hh_api_command_test.go`.

**Interfaces:**
- Accept `hh-api approval review --pilot <path> --out <path> [--letter-file <path>]` and reject missing/duplicate values, unknown flags, extra positionals, and `export` syntax changes.
- Change `runHHAPIApprovalCommand` to receive `HHAPICommandDeps` so approval timestamps use the injected `Now` seam; it must not construct an HH client.
- Reuse private atomic output through `platform.WritePrivateFileAtomic` with mode `0600` and sanitized output that contains no paths, provider IDs, letter text, or secrets.

- [ ] **Step 1: Write failing parser and command tests.**

Add parser cases for valid review syntax with and without `--letter-file`, equals-form flags, duplicate/missing values, unknown flags, and extra positionals. Add command tests that write a manual pilot JSON with an empty source nonce, run review with an injected timestamp, and assert:

```go
approval.FinalDecision == "REVIEW_REQUIRED"
approval.OriginalFinalDecision == "REVIEW_REQUIRED"
approval.ApprovalBasis == "OPERATOR_MANUAL_REVIEW"
approval.OperatorApproved
approval.Nonce != ""
approval.Nonce != pilot.Nonce
approval.ContentHash == contentHash(approval.CoverLetter)
approval.OriginalAIRecommendation == "UNCERTAIN"
approval.PilotArtifactHash == sha256OfExactPilotBytes
```

Assert the source pilot file is unchanged, the output mode is `0600`, no HTTP server is contacted, and command output is sanitized.

Add a `--letter-file` test whose content includes Unicode and a trailing newline. Assert source pilot integrity is checked before the override, `approval.CoverLetter == string(letterBytes)`, `approval.ContentHash == contentHash(string(letterBytes))`, and `approval.PilotArtifactHash` remains the SHA-256 of the exact original pilot JSON bytes. Mutate the source pilot and override file after approval and assert the stored approval remains unchanged. Add Policy B cases for an empty source letter with known `CoverLetterRequired=false` (success), `true` (failure), and unknown (failure), plus a non-empty override against a source pilot whose letter is empty but known not required.

- [ ] **Step 2: Run focused parser/command tests and verify failure.**

Run:

```bash
go test ./internal/cli ./internal/runtime -run 'TestParse.*Approval|TestHHAPIApprovalReview' -count=1
```

Expected: failure because `review` is not yet accepted and the runtime dispatch still only supports `export`.

- [ ] **Step 3: Add CLI validation and dispatch.**

Extend `validateHHAPIApprovalArgs` to accept `export` and `review`. Keep export’s exact two required flags. For review, require exactly one `--pilot` and `--out`, permit at most one `--letter-file`, and reject every other token. Update the command help string and `README.md`/`CLI_CONTRACT.md` to document review as local/read-only and apply as the only controlled mutation command.

- [ ] **Step 4: Implement local review execution.**

Parse the review arguments, load the manual pilot with the nonce-permitting manual loader, compute the SHA-256 pilot hash from exact source bytes, verify the source `ContentHash` against the source pilot letter before reading or applying `--letter-file`, select either the exact pilot letter or exact `os.ReadFile` bytes from `--letter-file`, validate Policy B, validate the pilot, generate the fresh nonce only after all validation passes, construct the approval, marshal it, and atomically write the private artifact. Use `deps.Now` or `time.Now().UTC()` and do not call `newHHAPIClient`, `ReadResumes`, or any other HH dependency.

On success print only a sanitized line such as:

```text
API_MANUAL_APPROVAL_CREATED vacancy_id=<numeric> approval_basis=OPERATOR_MANUAL_REVIEW
```

Do not print the nonce, letter, source/output paths, complete provider identity, AI reasons, or candidate context.

- [ ] **Step 5: Run focused tests and commit the command.**

Run:

```bash
gofmt -w internal/cli/parse.go internal/cli/parse_test.go internal/runtime/hh_api_command.go internal/runtime/hh_api_approval_export.go internal/runtime/hh_api_approval_export_test.go internal/runtime/hh_api_command_test.go internal/runtime/cli_help.go
go test ./internal/cli ./internal/runtime -run 'TestParse.*Approval|TestHHAPIApprovalReview|TestPilotArtifactToAPIApplicationApproval' -count=1
```

Expected: PASS with zero HTTP requests from the review command. Commit:

```bash
git add internal/cli/parse.go internal/cli/parse_test.go internal/runtime/hh_api_command.go internal/runtime/hh_api_approval_export.go internal/runtime/hh_api_approval_export_test.go internal/runtime/hh_api_command_test.go internal/runtime/cli_help.go README.md docs/architecture/CLI_CONTRACT.md
git commit -m "feat: add explicit manual API approval command"
```

### Task 3: Manual apply basis, explicit fresh gates, nonce consumption, and dry-run coverage

**Files:**
- Modify: `internal/runtime/hh_api_application.go`.
- Test: `internal/runtime/hh_api_application_test.go` and `internal/runtime/hh_api_command_test.go`.

**Interfaces:**
- `validateAPIApplicationApproval` accepts either the existing automatic basis or the manual basis; it rejects all other combinations.
- Add a focused preflight validator used by `runHHAPIApply` that requires fresh authoritative API evidence for duplicate `NO`, selected resume suitability, complete suitability scan, `Available`, active state, standard applicant path, `CanApply`, known `TestPresent=false`, and cover-letter rules.
- Add a private cross-process locked nonce-consumption helper that marks `NonceUsedAt` before a live submission and after all fresh preflight gates, but never during dry-run. A second live attempt using the same file, including a separate process, must fail before any HH POST.

- [ ] **Step 1: Write failing approval-validation tests.**

Extend `validAPIApplicationApproval` tests with a valid manual artifact and assert both automatic and manual artifacts pass. Add table-driven failures for manual wrong status, `MATCH` final decision, wrong basis, `operator_approved=false`, missing operator timestamp, missing original AI score, non-`UNCERTAIN` original recommendation, missing pilot hash, missing nonce, used nonce, stale approval, wrong vacancy, wrong provider resume, invalid letter, and changed letter/hash. Keep the existing automatic cases unchanged and assert an automatic artifact with no manual fields still passes.

- [ ] **Step 2: Run the focused validator tests and verify failure.**

Run:

```bash
go test ./internal/runtime -run 'TestValidateAPIApplicationApproval' -count=1
```

Expected: the new manual success case fails because current validation only accepts `READY_FOR_EXPLICIT_SEND + MATCH`.

- [ ] **Step 3: Implement two explicit validation bases.**

In `validateAPIApplicationApproval`, retain the current exact identity, nonce, content hash, letter safety, and 30-minute freshness checks. Replace the single status/decision condition with two named branches:

```go
automatic := approval.Status == "READY_FOR_EXPLICIT_SEND" && approval.FinalDecision == "MATCH" && approval.ApprovalBasis == "" && !approval.OperatorApproved
manual := approval.Status == pilotManualReviewStatus && approval.FinalDecision == "REVIEW_REQUIRED" && approval.ApprovalBasis == manualApprovalBasis && approval.OperatorApproved
```

Require the manual provenance fields, timestamp not in the future, original decision `REVIEW_REQUIRED`, score present, original recommendation `UNCERTAIN`, and non-empty pilot hash only in the manual branch. Preserve automatic zero-value compatibility. Keep `NonceUsedAt != nil` and empty nonce blocking for both branches.

- [ ] **Step 4: Write failing fresh-preflight and nonce tests.**

Using the existing local API fixture, add manual-approval apply cases where fresh GET state independently blocks for duplicate, unsuitable/unknown selected resume, unavailable/unknown application state, inactive/unknown active state, `CanApply=false/unknown`, and `TestPresent=true/unknown`; assert zero POSTs for every case. Add a success dry-run case asserting `WOULD_APPLY` and zero POSTs.

Add a direct nonce-consumption test that writes a valid approval artifact, invokes the consumption helper once, reloads it and asserts `NonceUsedAt != nil`, then invokes it again and asserts `errAPIApplicationApprovalNonce` with no second mutation opportunity. Add two competing live-style consumers synchronized on a start barrier and assert exactly one succeeds, the other observes the consumed nonce, and both preserve the exact vacancy/resume/content identity checks. Assert dry-run leaves `NonceUsedAt` nil. Add a crash-style test that leaves the persisted consumed artifact in place before the simulated continuation and proves a later consumer cannot restore or reuse the nonce.

Add a changed-letter-after-approval integration test that edits only the approval JSON letter while retaining the old hash and asserts apply fails before any preflight POST; add a fresh-preflight blocking test that proves approval creation itself succeeds locally but apply still blocks from provider state.

- [ ] **Step 5: Run the new tests and verify failure.**

Run:

```bash
go test ./internal/runtime -run 'TestHHAPIApply|TestConsumeAPIApplicationApprovalNonce|TestValidateAPIApplicationApproval' -count=1
```

Expected: manual apply validation, explicit preflight cases, and nonce-consumption tests fail against the current implementation.

- [ ] **Step 6: Implement explicit API preflight gates and durable nonce use.**

Add a helper immediately after `apiVacancyPreflightWithSource` returns. It must reject unknown as well as false states with sanitized errors, require `preflight.Available`, `preflight.CanApplyKnown && preflight.CanApply`, `preflight.SelectedResumeSuitableKnown && preflight.SelectedResumeSuitable`, `preflight.SuitableResumesScanComplete`, `preflight.TestPresentKnown && !preflight.TestPresent`, `preflight.ArchivedKnown && !preflight.Archived`, a known active standard vacancy type, no `ResponseIdentifierPresent`, and a duplicate evidence value exactly equal to `AlreadyRespondedNo`. Keep cover-letter required/allowed checks after these gates.

For non-dry-run apply, acquire an exclusive cross-process lock by creating a deterministic adjacent lock file with `os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)`; an existing lock fails closed and is never overwritten. While holding it, re-read the approval from disk, verify its exact nonce is the one that passed validation, verify `VacancyID`, provider resume identity, and content hash still match the validated artifact, require `NonceUsedAt == nil`, set `NonceUsedAt = &now`, and persist through the existing private atomic-file mechanism. Close and remove the lock only after persistence succeeds or fails. Continue toward attempt reservation and the existing one-attempt submission service only after successful persistence. If persistence fails, return before constructing/sending the writer. If transport becomes ambiguous or the process crashes after persistence, the consumed nonce remains durable and no retry or automatic restoration is allowed. Do not mark or rewrite the approval file on dry-run.

Do not remove or bypass the existing application-attempt store, audit counter, one-run gateway cap, redirect-free writer, or targeted reconciliation. The manual branch only changes artifact eligibility.

- [ ] **Step 7: Run focused apply tests and commit the safety integration.**

Run:

```bash
gofmt -w internal/runtime/hh_api_application.go internal/runtime/hh_api_application_test.go internal/runtime/hh_api_command_test.go
go test ./internal/runtime -run 'TestHHAPIApply|TestConsumeAPIApplicationApprovalNonce|TestValidateAPIApplicationApproval' -count=1
```

Expected: PASS, with all fresh provider blockers issuing zero POSTs and the manual dry-run emitting `WOULD_APPLY`. Commit:

```bash
git add internal/runtime/hh_api_application.go internal/runtime/hh_api_application_test.go internal/runtime/hh_api_command_test.go
git commit -m "feat: gate manual API approvals through fresh preflight"
```

### Task 4: End-to-end round trip, documentation consistency, and full verification

**Files:**
- Modify: `internal/runtime/hh_api_approval_export_test.go` and `internal/runtime/hh_api_command_test.go` for the complete pilot-to-review-to-dry-run path.
- Modify: `README.md`, `docs/architecture/CLI_CONTRACT.md`, and the validation report only after evidence exists.
- Create: `docs/validation/VALIDATION_RESET_API_3B.md` after all commands pass.

**Interfaces:**
- The round trip is `career-agent pilot --vacancy 137532422` → `hh-api approval review` → `hh-api apply ... --approval-file ...` with `HH_TRANSPORT=api`, `HH_DRY_RUN=true`, and `HH_WRITE_ENABLED=false` for the final apply.
- The local end-to-end fixture must count every HTTP method and fail on any POST; the real-provider validation must use GET-only preflight and dry-run only.

- [ ] **Step 1: Write the failing round-trip regression test.**

Create a real-shaped manual `PilotArtifact` JSON with no source nonce and a source letter hash that passes integrity validation, run the review command, load the output approval, then run apply against an `httptest.Server` returning active/open, duplicate `NO`, suitable selected resume, `CanApply=true`, `has_test=false`, and `AVAILABLE`. Assert the output retains `REVIEW_REQUIRED`, `OPERATOR_MANUAL_REVIEW`, original AI fields, exact letter/hash, and a nonce different from the empty source value; assert apply emits `WOULD_APPLY`, leaves the approval nonce unconsumed, and the server observed zero POSTs.

- [ ] **Step 2: Run the round-trip test and verify it fails.**

Run:

```bash
go test ./internal/runtime -run 'TestHHAPIManualApprovalRoundTrip' -count=1
```

Expected: failure until all command, artifact, and apply integration work is present.

- [ ] **Step 3: Implement only the minimum round-trip wiring and update docs.**

Keep the test on the existing command seams. Update examples to show explicit manual review and the existing automatic export separately. State that review is local/read-only, apply is explicit, and no real application was sent. Do not add any force/override flag or change the existing automatic command example.

- [ ] **Step 4: Run the full required verification suite.**

Run exactly:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
```

Expected: every command exits 0; tests use only local fixtures and no real HH POST is observed.

- [ ] **Step 5: Perform GET/dry-run validation for vacancy 137532422.**

Use an explicitly configured, read-only environment and run the pilot, manual approval, and API dry-run commands. Do not set live write enablement and do not invoke `career-agent pilot send`.

```bash
HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder career-agent pilot --vacancy 137532422

HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder hh-api approval review \
  --pilot career_agent_pilot.json \
  --out reset-api-3b-approval.json

HH_TRANSPORT=api HH_DRY_RUN=true HH_WRITE_ENABLED=false \
go run ./cmd/hh-ai-responder hh-api apply 137532422 \
  --resume-id <provider-resume-id-from-the-approved-artifact> \
  --approval-file reset-api-3b-approval.json
```

The final report must use the actual selected resume argument from the generated approval artifact, redact it in narrative output, record the GET-only preflight result, and state `Application POST: 0`. If the pilot or fresh preflight is not eligible, report the fail-closed result and do not manufacture an approval or bypass a gate.

- [ ] **Step 6: Write the validation report and record the final SHA.**

Create `docs/validation/VALIDATION_RESET_API_3B.md` with the actual command outputs summarized without private candidate context, the full verification command results, the dry-run result, and an explicit real-POST count of zero. Run `git status --short`, `git diff --stat <baseline>..HEAD`, and `git rev-parse HEAD`; report the final SHA and changed files. Do not send or retry a real application.

## Plan self-review

- Spec coverage: command syntax, strict AI-uncertain eligibility, no source nonce requirement, fresh nonce issuance, exact letter binding, provenance/audit fields, automatic compatibility, explicit apply bases, fresh API gates, nonce consumption, dry-run, zero-POST tests, docs, and vacancy `137532422` validation are assigned to Tasks 1–4.
- Placeholder scan: no unfinished or unspecified implementation step is used; each task names files, functions, test behavior, commands, and expected result.
- Type consistency: the manual validator returns the normalized provider resume ID; the constructor receives that ID, exact reviewed letter, pilot hash, fresh nonce, and approval timestamp; apply consumes the resulting `APIApplicationApproval` without requiring pilot-only API facts.
- Review focus coverage: loader nonce policy and AI uncertainty are tested in Task 1; provider-state deferral and exact letter mutation in Tasks 2–3; nonce issuance/consumption and dry-run in Task 3; full zero-POST round trip in Task 4.
