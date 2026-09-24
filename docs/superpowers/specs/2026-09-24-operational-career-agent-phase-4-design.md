# Operational Career Agent Phase 4 Design

## Goal

Phase 4 turns the existing vacancy Career Agent and communication workflow into
one durable, read-only daily application service used by the CLI, dashboard,
and scheduler. It produces reviewable local artifacts and a deterministic
attention queue; it never authorizes or performs HH writes.

## Constraints

- Existing Phase 1–3 canonical state machines remain authoritative.
- `internal/careeragent` owns run/item/preparation domain contracts; existing
  vacancy, application, conversation, candidate, approval, preflight, gateway,
  and reconciliation domains are not duplicated.
- Daily orchestration receives read/sync services only. `HHWriteGateway`, an
  HH writer, approval mutation, and send flow are not dependencies of the
  daily service.
- `HH_DRY_RUN=true` remains an unconditional write block. Daily is read-only
  even when global write configuration is enabled.
- Unknown or critical incomplete state is `REVIEW_REQUIRED`, never an
  automatic action.
- Employer messages remain untrusted input. AI may draft or explain, but never
  approves, sends, classifies safety authority, or determines authoritative
  priority.
- Existing dirty and untracked user files are preserved and excluded from
  Phase 4 commits unless the requested behavior requires that exact file.
- JSON compatibility and macOS/Linux/Windows startup behavior are preserved.

## Architecture

```text
CLI / Dashboard POST / Scheduler
             |
             v
     DailyCareerAgentService
       /                \
read-only vacancy       read-only communication
  application flow       application flow
       \                /
        durable run + derived attention projection
```

`DailyCareerAgentService` is the only new orchestration boundary. It delegates
vacancy processing to the existing read-only Career Agent pipeline and
communication processing to the existing read-only sync/classification/draft
pipeline. It records one `AgentRun`, redacted `AgentRunItem` telemetry, and
existing preparations. It does not introduce another vacancy, application,
conversation, or candidate lifecycle.

The service is constructed from a typed dependency struct with explicit
interfaces for provider refresh, vacancy processing, communication processing,
workflow persistence, and clock. A process mutex plus a durable running-run
check prevents overlapping executions. A deterministic input fingerprint
makes repeated runs replay-safe; persistence uses existing upsert keys.

## Daily result contract

```go
type DailyCareerAgentRun struct {
    Run       careeragent.AgentRun
    Summary   DailyCareerAgentSummary
    Attention []AttentionItem
}

type DailyCareerAgentSummary struct {
    Vacancy       VacancyDailySummary
    Communication CommunicationDailySummary
    AI            AIBudgetSummary
    Attention     int
    HHWrites      int
    Result        string // SUCCESS, PARTIAL_SUCCESS, FAILED
}
```

The CLI renders a stable human envelope from this result. `--json` emits the
same typed fields with stable snake_case names and no raw prompts, cookies,
tokens, provider response bodies, or unrelated private candidate data.

## Workflow phases

1. Recover interrupted local runs.
2. Refresh provider state using the existing read-only HH sync service.
3. Discover and deduplicate vacancies through the existing search pipeline.
4. Apply deterministic eligibility and bounded AI evaluation limits.
5. Route resumes and resolve Candidate Knowledge through existing services.
6. Upsert application preparations and knowledge requests.
7. Refresh applications and conversations.
8. Classify employer messages deterministically.
9. Reuse existing clarification and draft generation paths.
10. Project interview, test, offer, rejection, and follow-up work items.
11. Derive and sort the unified attention queue.
12. Persist run/items and finish with `SUCCESS`, `PARTIAL_SUCCESS`, or
    `FAILED`.

One malformed vacancy, unavailable conversation, or bounded AI error produces
a failed/review-required item and `PARTIAL_SUCCESS` while other items survive.
Safety-critical persistence or provider infrastructure failure may fail the
whole run and is retained in run telemetry.

## PostgreSQL parity

The existing PostgreSQL career workflow tables are extended only where their
current ownership fits. Drafts and clarification requests are separate
canonical records today, so PostgreSQL parity requires additive tables when
consumer-owned storage interfaces cannot be extended safely in-place:

- `ai_drafts` stores the full validated draft lifecycle, exact text hash,
  message/knowledge fingerprints, application/conversation relation, and
  used-fact metadata;
- `candidate_clarifications` stores the request lifecycle, gap identity,
  relation/message references, answer/proposal state, and timestamps.

Both tables use deterministic unique keys for idempotent create/replace. JSON
stores remain versioned compatibility implementations of the same narrow
interfaces. The migration under `migrations/` and the embedded/runtime
migration copy are byte-equivalent in schema intent, have safe down migrations,
and are covered by migration contract and JSON/PostgreSQL parity tests. No
second truth table or separate queue table is introduced.

## Attention queue

```go
type AttentionItem struct {
    ID             string    `json:"id"`
    Type           string    `json:"type"`
    Priority       int       `json:"priority"`
    VacancyID      int       `json:"vacancy_id,omitempty"`
    ApplicationID  string    `json:"application_id,omitempty"`
    ConversationID string    `json:"conversation_id,omitempty"`
    Title          string    `json:"title"`
    Summary        string    `json:"summary"`
    Reason         string    `json:"reason"`
    Risk           string    `json:"risk"`
    NextAction     string    `json:"next_action"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

The model is derived from authoritative vacancy review, preparations,
communication work items, drafts, clarifications, and follow-up state. It is
recomputed after restart. Priority is deterministic:

```text
delivery/safety > offer > interview > test deadline > needs reply >
clarification > prepared application > follow-up > strong vacancy > review
```

Critical items cannot be hidden by AI scoring or UI filters.

## CLI, dashboard, and scheduler

- CLI: `career-agent daily`, with existing global configuration conventions,
  `--json`, and `--dry-run` compatibility. Daily forces read-only capability.
- Dashboard: existing `/api/career/*` routes are extended with the derived
  attention read model, durable daily run status, and one local POST action.
  The POST is a local orchestration mutation, not an HH mutation.
- Scheduler: existing `internal/platform/scheduler` is reused. A bounded
  daily task is default-off, uses the same service instance, and records the
  same run result. The scheduler never calls an HH writer and never overlaps a
  running daily task.
- UI updates are applied to both maintained `web/` assets and embedded
  `internal/runtime/web/` copies.

## Testing and verification

Tests cover fresh/ambiguous/unknown vacancies, employer replies, interview,
test, offer, rejection, follow-up, duplicate run, malformed item, provider/AI
partial failure, restart persistence, PostgreSQL parity, migration contracts,
zero HH writes, approval gating, suspicious communication, dry-run behavior,
manual dashboard execution, scheduler reuse, and no-overlap behavior.

Final verification is:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
node --check web/app.js
node --check internal/runtime/web/app.js
```
