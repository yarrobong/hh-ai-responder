# SDD ledger — plan: docs/superpowers/plans/2026-09-20-reset-api-3a.md

## Pre-flight

- Task 1 → Task 2: Task 1 produces structured application error classification and sanitized evidence; Task 2 consumes the same writer response path for redirect/identity handling. No conflicting types found.
- Task 1 → Task 5: Task 1 produces provider-neutral result classes used by dry-run/reconciliation assertions. Existing classes are sufficient; captcha manual-review behavior will remain fail-closed without adding automatic retry.
- Task 3 → Task 5: Task 3 produces controlled gateway composition with `cfg.HHMaxWritesPerDay` and shared durable `AttemptCounter`; Task 5 consumes it through the dry-run/live-path tests. Existing `HHWriteAuditStore` is the intended capability.
- Task 4 → Task 5: Task 4 produces the focused approval artifact consumed by `hh-api apply`; Task 5 validates the round trip through the command. Empty letters remain permitted only when fresh preflight says `response_letter_required=false`, per the approved RESET-API-3 spec.
- Ruling: The user explicitly approved implementation on `main`; use the current checkout without creating a worktree — cost if wrong: commits would be made on the shared branch, but this is directly authorized.
- Ruling: Keep the existing focused approval JSON schema and add export as a sibling subcommand rather than introducing a second artifact type — cost if wrong: future consumers would need a schema migration.

## Tasks

- Task 1: complete — RED: `go test ./internal/adapters/hh/api -run 'TestAPIApplicationWriter(ClassifiesDeterministicResponses|BoundsStructuredErrorEvidence)' -count=1` failed on undocumented 403/OAuth/captcha cases; GREEN: same focused tests and `go test ./internal/adapters/hh/api -count=1` passed; structured evidence is bounded/sanitized and duplicate substring text is not classified as already-applied.
- Task 2: pending
- Task 3: pending
- Task 4: pending
- Task 5: pending
- Ruling: The installed executing-plans package references `task-start`/`task-done` scripts that are absent; use the available `task-brief` plus manual test-output/ledger evidence instead — cost if wrong: no automated ledger helper, but the required RED/GREEN and final verification evidence remains recorded.
