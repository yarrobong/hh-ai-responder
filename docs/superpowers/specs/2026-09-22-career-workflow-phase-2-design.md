# Career Workflow Phase 2 Design

**Status:** approved for implementation, 2026-09-22

## Goal

Make the Career Agent durable and usable after restart while preserving the
existing HH read/write boundaries, Candidate Knowledge truth model, approval
flow, application states, JSON compatibility and dashboard architecture.

The resulting default daily flow is:

```text
discover -> normalize -> deterministic route -> bounded AI advisory review
        -> prepare -> persist -> human review
```

No step in this flow authorizes an HH write. Existing explicit approval,
fresh preflight, reservation and `HHWriteGateway` contracts remain the only
write path.

## Constraints and invariants

- `internal/runtime/migrations` is the embedded PostgreSQL migration authority.
- Root `migrations/` remains a compatibility mirror and must contain the same
  ordered files and bytes.
- Existing databases continue to use `schema_migrations`; new migrations are
  additive and never rewrite canonical vacancy/application/candidate data.
- AI remains advisory. Deterministic Go code owns hard requirements, resume
  enablement/provider identity, review states and write safety.
- Unknown critical state is `REVIEW_REQUIRED`; it never becomes an automatic
  application.
- Run telemetry and preparation artifacts contain no cookies, tokens, raw
  prompts or unrelated private messages. Error summaries are redacted.
- JSON and PostgreSQL backends expose the same workflow semantics and are
  covered by parity tests where the existing backend contract requires it.
- Existing CLI flags, environment variables, JSON envelopes and HH transport
  behavior remain compatible.

## Architecture

### Migration authority

`internal/runtime/postgres.go` continues to embed and apply
`internal/runtime/migrations/*.up.sql` in deterministic filename order. The
root `migrations/` directory is synchronized as a checked-in mirror for
operators and existing tooling. A contract test compares both directories,
including migration names, up/down pairing and file bytes. The next migration
is added to the embedded authority and the root mirror in the same change.

### Durable workflow ledger

Add a narrow workflow port rather than a generic agent framework:

```go
type CareerWorkflowStore interface {
    StartRun(context.Context, careeragent.AgentRun) error
    FinishRun(context.Context, careeragent.AgentRun) error
    UpsertRunItem(context.Context, careeragent.AgentRunItem) error
    UpsertPreparation(context.Context, careeragent.ApplicationPreparation) error
    GetRun(context.Context, string) (careeragent.AgentRun, error)
    ListRuns(context.Context, careeragent.RunQuery) ([]careeragent.AgentRun, error)
    GetPreparation(context.Context, int) (careeragent.ApplicationPreparation, error)
    ListPreparations(context.Context, careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error)
    RecoverInterruptedRuns(context.Context, time.Time) error
}
```

The exact port may be split into smaller named read/write interfaces if that
matches existing repository conventions. PostgreSQL uses additive tables
`agent_runs`, `agent_run_items` and `application_preparations`. JSON uses one
versioned atomic workflow file. Both backends use stable natural keys and
upsert semantics so retries do not create duplicate items or preparations.

An interrupted `running` run is recovered to a terminal failed/partial state
with result code `INTERRUPTED`; it cannot be reported as successful.

### Preparation artifact

`ApplicationPreparation` is a review artifact, not a write authorization. It
contains the vacancy ID, selected resume identity, candidate version/hash,
deterministic route and advisory analysis evidence, selected story IDs, exact
cover-letter text and hash, test-answer drafts, unresolved Knowledge Requests,
freshness and preparation status. It records an input fingerprint so the same
preparation is idempotent and a candidate knowledge/version change marks the
artifact stale.

The existing application approval artifact is extended with optional
preparation identity/hash fields. Validation checks that the approval refers
to the exact reviewed artifact and exact cover-letter hash. Existing approval
files without these optional fields remain valid under their current
compatibility rules.

### Resume routing and bounded AI review

The canonical `RouteDecision` and existing route JSON shape remain unchanged.
For `ROUTE_AMBIGUOUS`, a new deterministic helper may expose an advisory
candidate only when it is an enabled, provider-valid resume without hard
blockers. The workflow may analyze that candidate, but it must retain
`REVIEW_REQUIRED` and must not submit, approve or choose a disabled/wrong
provider resume. `NO_SUITABLE_RESUME`, clear hard mismatch and unavailable
resume state remain terminal deterministic review/reject paths.

### Vacancy snapshot and queue

`VacancyReviewSnapshot` is computed as a read model. It references, rather
than replaces, canonical vacancy, freshness/review state, application,
conversation, Candidate Knowledge and preparation data. It exposes the
vacancy, pipeline state, deterministic match, advisory analysis, route,
unknowns, preparation, application/conversation state and next safe action.

The queue is computed in Go with an explicit priority function. Clarification
and safety blockers come first, followed by ready review, strong matches,
bounded-AI review, new items, dismissed/rejected items and closed items.
Freshness, application state and archive state are inputs; publication time
is only a deterministic tie-breaker.

### Dashboard

The existing local HTTP server and vanilla JavaScript application remain the
presentation layer. Add read-only career endpoints for the ordered queue,
vacancy workspace and run timeline, and extend existing overview metrics with
Career Agent counts. Add a small queue/workspace presentation using current
components and status badges; do not introduce a framework or new write
surface.

## Data flow and failure behavior

1. Existing HH read/search discovers and normalizes vacancies.
2. The workflow records a run and item outcome before/after each stage.
3. Deterministic filters and resume routing run first.
4. AI is called only for eligible deterministic candidates, including
   ambiguous-but-plausible routes through the bounded advisory candidate.
5. Unknown hard requirements create review/Knowledge Request state and block
   application preparation from becoming write-ready.
6. Safe preparation artifacts are persisted with exact content hashes.
7. Human review and existing approval/preflight remain required for any write.
8. Storage errors fail the run/item accounting closed and are redacted in
   telemetry.

Malformed stored JSON/evidence is a read failure, not an empty successful
   result. Duplicate processing is idempotent. Reconciliation continues to be
   based on positive evidence and existing application state contracts.

## Verification strategy

- Migration mirror contract: names, ordering, up/down pairing and bytes.
- Domain tests: run validation/recovery, preparation staleness, queue order and
  snapshot projection.
- Storage tests: JSON idempotency, malformed JSON/evidence, interrupted runs,
  PostgreSQL repository behavior and JSON/PostgreSQL parity where applicable.
- Workflow regressions: technical support, Python/Django backend,
  automation/integration, unrelated, ambiguous and unknown-hard-requirement
  vacancies.
- Approval regressions: exact preparation identity/hash, stale artifact,
  changed cover letter and compatibility approval without new fields.
- Dashboard API and JavaScript syntax tests.
- Final project checks: `go test ./...`, race, vet, build, `git diff --check`
  and existing JavaScript checks.

## Non-goals

- No generic `agents/` framework.
- No microservices or frontend rewrite.
- No new canonical vacancy/application/candidate truth tables.
- No automatic application, test submission, chat message, resume touch or
  job-search status update from AI output.
- No removal of existing CLI behavior or JSON compatibility contracts.
