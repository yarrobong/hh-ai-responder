# RESET-API-3 Controlled API Application POST Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit, single-vacancy HH API application command with fail-closed preflight, durable two-scope protection, mandatory reconciliation, and a fully validated dry-run path that never sends a real HH mutation.

**Architecture:** Keep `APIHHClient` read-only at its public boundary and add one narrow API application writer behind `hhwrite.VacancyResponseWriter`. Compose that writer with the existing write gateway, application submission, durable attempt, and reconciliation use cases from a new explicit `hh-api apply` runtime command. Do not connect this path to search or bulk career-agent execution.

**Tech Stack:** Go, `net/http`, `net/url`, `encoding/json`, `httptest`, existing JSON/PostgreSQL attempt repositories, and existing HH API preflight/reconciliation sources.

**Spec:** `docs/superpowers/specs/2026-09-20-reset-api-3-controlled-application-post-design.md`

## Global constraints

- `HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_TRANSPORT=api` is valid: it performs all eligibility, preflight, approval, and artifact validation, may emit `WOULD_APPLY`, and must not invoke the mutation adapter.
- A real POST requires `HH_DRY_RUN=false` and `HH_WRITE_ENABLED=true`; the command always requires exact vacancy/resume IDs and an explicit `--approval-file`.
- Durable protection has both exact-attempt scope (`application + vacancy + resume`) and vacancy mutation-lock scope (`application + vacancy`); once a POST may have been dispatched, another resume cannot bypass the lock.
- `SUCCESS`, `ALREADY_APPLIED`, and `UNKNOWN_SEND_RESULT` each require one targeted reconciliation read before the final outcome is reported.
- Ambiguous transport results are never retried automatically. No implementation or automated test may perform a real HH POST.

## Review focus

The implementation review must specifically verify:

- dry-run reaches every read-only validation gate but never reaches the writer;
- a second resume for the same vacancy is blocked after a possible POST, including after restart;
- all three reconciliation-triggering result classes perform the targeted GET;
- stale or mismatched approval artifacts fail closed;
- timeout/reset/EOF/uncertain 5xx maps to `UNKNOWN_SEND_RESULT` without retry.

## File map

- Modify `internal/ports/hhwrite/hh_write.go` for the provider resume identifier and normalized application result class.
- Add `internal/adapters/hh/api/application_writer.go` and focused HTTP tests.
- Modify `internal/usecase/applicationsubmission` only as needed to carry trusted availability and preserve existing browser behavior.
- Add `internal/runtime/hh_api_application.go` and tests for approval validation, orchestration, dry-run, and reconciliation.
- Modify `internal/runtime/hh_api_command.go`, `internal/cli/parse.go`, `internal/runtime/cli_help.go`, and `README.md` for the explicit command.
- Extend existing `internal/usecase/applicationattempt` tests only where needed to document exact-attempt plus vacancy-lock behavior.
- Add sanitized validation evidence under `docs/validation/` after tests are actually run.

## Task 1: Extend the narrow write contract and implement the API writer

- [ ] Add a provider resume ID to `hhwrite.VacancyResponseRequest`, retaining browser `ResumeHash` compatibility.
- [ ] Add normalized classes `SUCCESS`, `ALREADY_APPLIED`, `BUSINESS_REJECTED`, `AUTH_REQUIRED`, `RATE_LIMITED`, and `UNKNOWN_SEND_RESULT` to `hhwrite`; preserve existing gateway outcome semantics.
- [ ] Add `Class` to `hhwrite.WriteResult` and document which classes are terminal versus reconciliation-triggering.
- [ ] Implement `APIApplicationWriter` in `internal/adapters/hh/api/application_writer.go` with one `POST /negotiations` operation using `vacancy_id`, `resume_id`, and optional `message` form fields.
- [ ] Keep the API client’s public read API read-only; use a private, narrowly scoped authenticated write helper in the writer.
- [ ] Classify deterministic provider responses without leaking tokens, cookies, headers, or response bodies into logs.
- [ ] Treat timeout, connection reset, EOF, and uncertain 5xx as `UNKNOWN_SEND_RESULT`; do not retry.
- [ ] Write tests for exact form encoding, optional message, provider classification, ambiguous transport, and no retry.

## Task 2: Make controlled applicability include availability

- [ ] Extend the controlled submission applicability path to represent `Available` and `AvailableKnown` without changing browser defaults or weakening existing gates.
- [ ] Ensure unknown availability fails closed as `REVIEW_REQUIRED`/ineligible and never reaches an executor.
- [ ] Add tests proving unavailable, unknown, duplicate, test-present, required-empty-letter, and unsafe-resume cases block execution.

## Task 3: Add approval freshness and the explicit `hh-api apply` orchestration

- [ ] Define a focused approval artifact model in `internal/runtime/hh_api_application.go` containing vacancy ID, provider resume ID, cover letter, content hash, nonce, status, final decision, and preview freshness timestamp.
- [ ] Require explicit artifact loading from `--approval-file`; never select a default artifact.
- [ ] Validate artifact schema, exact vacancy/resume identity, `READY_FOR_EXPLICIT_SEND`, final decision `MATCH`, content hash, nonce, and freshness (use a named bounded max age and test it at the boundary).
- [ ] Add `runHHAPIApply` with exactly one vacancy and one resume, using `apiVacancyPreflightWithSource` and trusted structured fields before any write decision.
- [ ] Build the controlled application request from the validated artifact and preflight; reject provider resume mismatch and invalid cover-letter content.
- [ ] In dry-run, perform all reads and validation, emit `WOULD_APPLY`, and stop before constructing/invoking the mutation adapter.
- [ ] In live mode, require `HH_DRY_RUN=false` and `HH_WRITE_ENABLED=true`, configure a per-run/per-day cap of one, and compose the existing write gateway with `applicationattempt` and `applicationsubmission`.
- [ ] Add tests for every approval/preflight gate, dry-run with `HH_WRITE_ENABLED=false`, real-mode configuration rejection, exact identity, and one-application caps.

## Task 4: Add mandatory targeted reconciliation and final outcomes

- [ ] Add final controlled outcomes `POST_SUCCESS_RECONCILED`, `POST_SUCCESS_UNCONFIRMED`, `ALREADY_APPLIED_RECONCILED`, `UNKNOWN_SEND_RECONCILED_SUCCESS`, and `UNKNOWN_SEND_UNRESOLVED`.
- [ ] Implement an API evidence reader using targeted vacancy/preflight and negotiation collection reads; do not depend on the currently unsupported generic `ReadApplications` method.
- [ ] Route `SUCCESS`, `ALREADY_APPLIED`, and `UNKNOWN_SEND_RESULT` through exactly one targeted reconciliation read before final classification.
- [ ] Persist attempt state/evidence so restart after an uncertain result cannot send another application or let another resume bypass the vacancy lock.
- [ ] Test POST-then-GET ordering, all three reconciliation-triggering classes, confirmed/unconfirmed branches, and ambiguous no-retry behavior with `httptest.Server`.

## Task 5: Expose and document the command

- [ ] Parse only the explicit grammar `hh-api apply <vacancy-id> --resume-id <provider-id> --approval-file <path>`; reject missing, duplicate, extra, or implicit/default approval arguments.
- [ ] Dispatch the command through `hh_api_command.go` and update help text.
- [ ] Update `README.md` with dry-run and live-mode requirements, explicit artifact usage, safety outcomes, and the no-bulk/no-real-POST validation rule.
- [ ] Add CLI parser and command-level tests for valid and invalid invocations.

## Task 6: Verify and produce the requested evidence

- [ ] Run an injected dry-run scenario with `HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_TRANSPORT=api`; record eligibility/preflight/artifact checks, `WOULD_APPLY`, and zero POST requests.
- [ ] Run `gofmt -w .`, `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`; resolve failures before claiming completion.
- [ ] Inspect the diff for secrets, cookies, personal raw chat data, accidental browser/bulk wiring, and any real HH endpoint invocation in tests.
- [ ] Select and report a proposed live vacancy for manual review only (the planned candidate is vacancy `137112468`); do not execute it and do not use unrelated vacancies `137244538` or `137493556`.
- [ ] Add a sanitized validation report under `docs/validation/` containing the dry-run evidence and the proposed-live-vacancy status.

## Plan self-review checklist

- [ ] Every approved amendment has an implementation step and a test.
- [ ] Every task names concrete files, interfaces, or observable behavior.
- [ ] No step relies on placeholders such as “implement later” or “appropriate error”.
- [ ] The plan preserves existing browser writes, bulk behavior, CLI compatibility outside the new command, and dry-run protection.
