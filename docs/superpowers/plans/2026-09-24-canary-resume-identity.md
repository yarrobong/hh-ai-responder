# Canary Resume Identity and Durable Preparation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make current selected resumes persist and validate through one canonical identity path, while preserving stale-resume blocking and ensuring pilot previews create durable preparation before any approval artifact can be exported.

**Architecture:** Keep `careeragent.ResumeProfile.ID` as the internal routing identity and add explicit runtime helpers that project a selected `ResumeItem` into internal ID, provider ID, and optional version fingerprint. Application requests use the provider-facing identity required by the selected transport; durable preparations store the internal identity plus provider identity and version fingerprint. Pilot preview persistence is local-only and happens before the artifact is saved; preparation is never approval.

**Tech Stack:** Go, existing `careeragent`, `applicationprocessing`, `applicationsubmission`, JSON/Postgres workflow stores, `httptest` and deterministic unit fixtures.

**Spec:** User-provided Canary Readiness continuation request in the task conversation.

## Global Constraints

- Do not modify the original dirty checkout or `.hh-browser-profile*` paths.
- Work only in the clean `fix/canary-preparation-blockers` worktree.
- Keep `HH_DRY_RUN=true` and all pilot/readiness verification read-only; perform no HH writes.
- Preserve internal resume identity, provider resume ID, and version/fingerprint as separate concepts.
- Unknown critical state remains `REVIEW_REQUIRED`; stale or conflicting identity fails closed.
- Preserve canonical cover-letter content hashing and existing approval/nonce gates.
- Do not change unrelated classification, candidate facts, or integrator KnowledgeRequest behavior.

## Review Focus

- API provider ID and browser hash must not be compared as the same namespace — runtime regression test in `internal/runtime/resume_identity_test.go`.
- A route that still points to resume A must not silently submit or persist resume B — stale provider identity test in `internal/runtime/resume_identity_test.go`.
- Optional invalid cover letters remain safely omitted with the canonical empty hash — existing cover-letter and preparation tests plus pilot persistence test.
- Pilot `READY_FOR_EXPLICIT_SEND` must have a durable preparation before artifact save — pilot persistence integration test.
- Restart/reload and repeated persistence must preserve identity and avoid duplicates — JSON repository regression tests.

---

### Task 1: Capture the failing identity path

**Files:**
- Create: `internal/runtime/resume_identity_test.go`
- Modify: `internal/runtime/career_agent_pipeline_test.go` only if a shared fixture is needed.

**Interfaces:**
- Consume existing `ResumeItem`, `careeragent.ResumeProfile`, `applicationprocessing.Request`, `applicationsubmission.Input`, and `persistCareerAgentPreparation` behavior.
- Produce executable evidence for the API namespace mismatch and the stale A/B cases.

- [ ] **Step 1: Add a red test for API identity propagation.** Build a responder with `transportAPI`, `ResumeItem{ProviderID: "280551431", Hash: "hh-resume-a89"}`, `resumeIdentifier: "280551431"`, and assert the application request/preparation uses provider ID for the provider-facing identity while durable metadata keeps both identities.
- [ ] **Step 2: Run the focused test and confirm it fails because the current request uses `ResumeItem.Hash` and submission compares against `resumeHash`.
- [ ] **Step 3: Add red tests for same resume, internal/provider mismatch, unavailable replacement B, changed version fingerprint, wrong-resume cover letter, restart reload, and idempotent preparation semantics.
- [ ] **Step 4: Run only the new focused tests and record the exact failing values without logging secrets or raw private response bodies.
- [ ] **Step 5: Commit the diagnostic/regression tests as `test: cover canonical resume identity failures`.

### Task 2: Implement canonical resume identity projection

**Files:**
- Modify: `internal/careeragent/model.go` or a focused adjacent domain file for stable internal identity normalization.
- Modify: `internal/runtime/career_agent_runtime.go` for selected resume identity projection.
- Modify: `internal/runtime/application_processing_compat.go` for provider-facing prepared request identity.
- Modify: `internal/runtime/application_submission_compat.go` for current identity binding.
- Modify: `internal/careeragent/workflow.go` and storage adapters only if an additive fingerprint field is needed.
- Test: `internal/runtime/resume_identity_test.go`, existing router/workflow tests.

**Interfaces:**
- Consume `ResumeProfile.ID`, `ProviderID`, `Hash`, `HHID`, and `ResumeItem`.
- Produce one internal identity, one provider identity, and an optional version fingerprint. A provider ID is preferred for provider identity; a content/hash fingerprint is never used as the sole stable identity.

- [ ] **Step 1: Define a small canonical projection helper with explicit fields for internal ID, provider ID, and version fingerprint; reject a missing provider identity instead of guessing.
- [ ] **Step 2: Make application preparation requests carry `resumeIdentifierForValue(selectedResume)` so API requests use `280551431` and browser requests retain the browser hash behavior.
- [ ] **Step 3: Make submission current-state binding use the same selected/current provider-facing identity, never `r.resumeHash` unconditionally.
- [ ] **Step 4: Store durable `ApplicationPreparation.ResumeID` as the internal route identity and `ResumeProviderID` as the provider identity; preserve a version fingerprint only when the existing source supplies one.
- [ ] **Step 5: Keep stale validation strict: missing current identity, unavailable A, provider mismatch A→B, wrong route identity, or material version conflict must return the existing stale error before any executor.
- [ ] **Step 6: Run focused red tests, then the full package tests; commit as `fix: bind preparations to canonical resume identity`.

### Task 3: Persist targeted pilot preparation before approval material

**Files:**
- Modify: `internal/runtime/career_agent_pilot.go`.
- Modify: `internal/runtime/career_workflow_compat.go` or extract a narrow shared preparation builder if needed.
- Test: `internal/runtime/career_agent_pilot_test.go`, new identity/persistence tests.

**Interfaces:**
- Consume the completed pilot artifact inputs: vacancy, selected resume, route, AI result, cover-letter result, preflight, candidate snapshot.
- Produce a validated durable `ApplicationPreparation` reference (`PreparationID`, `PreparationHash`) before the pilot artifact is written. No approval or HH write is created.

- [ ] **Step 1: Add a red test asserting a ready targeted pilot with valid optional/no-letter content calls `UpsertPreparation` before artifact persistence and carries matching preparation references.
- [ ] **Step 2: Add a red test asserting a wrong selected resume cannot be persisted as a ready preparation.
- [ ] **Step 3: Build the preparation from canonical selected resume metadata and exact final cover-letter content; reuse `ApplicationPreparation.Validate`, `PreparationInputFingerprint`, and `ContentHash`.
- [ ] **Step 4: Persist only after all readiness gates pass; leave blocked/review previews without a ready preparation unless an existing manual-review path explicitly requires a review-required preparation.
- [ ] **Step 5: Verify repeated calls upsert the same canonical preparation without conflict or duplicate records.
- [ ] **Step 6: Commit as `fix: persist targeted pilot preparations before approval`.

### Task 4: Recheck candidate flows and bounded fresh search

**Files:**
- Modify only diagnostics/rendering files if required to expose identity values and preparation counters.
- Add/update deterministic fixtures under `internal/runtime/*_test.go` without storing credentials or raw HH private data.

**Interfaces:**
- Consume the fixed targeted pilot and career-agent read paths.
- Produce evidence for vacancies `137609053`, `137611139`, and `137765629`, then a bounded fresh read-only search breakdown.

- [ ] **Step 1: Re-run targeted 137609053 and record route, selected Technical Specialist resume, AI APPLY, optional-letter state, visible seniority/salary concerns, preparation persistence, and CANARY_READY result.
- [ ] **Step 2: Re-run targeted 137611139 and record Python/Django resume, safe optional omission, seniority/location concerns, preparation persistence, and policy-derived result.
- [ ] **Step 3: Re-run targeted 137765629 and record title/company, selected resume, route, AI result, before/after identity values, preparation, cover-letter state, and remaining blockers.
- [ ] **Step 4: Run a bounded fresh search with `HH_DRY_RUN=true`; collect raw hits, fresh unique, routing, deterministic/AI outcomes, letter outcomes, preparation persistence/failures, stale false positives, content-hash mismatches, KnowledgeRequests, CANARY_READY, and HH writes.
- [ ] **Step 5: Verify integrator 137471009 remains a pending user KnowledgeRequest and is not used as evidence for the resume fix.

### Task 5: Full verification and integration

**Files:**
- No additional production files unless verification exposes a directly related regression.

- [ ] **Step 1: Run `gofmt -w .`.
- [ ] **Step 2: Run `go test ./...`.
- [ ] **Step 3: Run `go test -race ./...`.
- [ ] **Step 4: Run `go vet ./...`.
- [ ] **Step 5: Run `go build ./...`.
- [ ] **Step 6: Run `git diff --check` and confirm the original dirty checkout is unchanged.
- [ ] **Step 7: Commit test-only additions separately if still uncommitted, push the feature branch, merge through a clean worktree, run the complete gate on merged `main`, and push `main` only if all gates pass. Never create approval or send an HH application.

