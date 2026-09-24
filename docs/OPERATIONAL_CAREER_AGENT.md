# Operational Career Agent — Phase 4

Phase 4 adds one read-only daily application service shared by the CLI, dashboard and scheduler.

## Entry points

```sh
# Human-readable output
./hh-ai-responder career-agent daily

# Stable machine-readable output
./hh-ai-responder career-agent daily --json

# Dashboard
./hh-ai-responder dashboard
```

The dashboard exposes `GET /api/career/attention`, `GET /api/career/runs`, and a manual `POST /api/career/run` button. The button starts the same `DailyCareerAgentService` used by the CLI. Scheduler wiring uses `internal/platform/scheduler` and the same service; the scheduler does not implement a second workflow.

The dashboard scheduler is default-off. Enable it with `HH_CAREER_AGENT_DAILY_ENABLED=true` and configure its completion-based interval with `HH_CAREER_AGENT_DAILY_INTERVAL=24h`.

## Safety boundary

`DailyCareerAgentService` receives a workflow telemetry store and read-only vacancy/communication stages. It does not receive `HHWriteGateway`, approval stores, HH write clients, or send callbacks. Daily composition also forces shadow mode, `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and disables legacy auto-apply, auto-chat, resume-touch, and job-search-status actions.

Approval and sending remain an explicit separate flow through `HHWriteGateway`. A daily result with `MATCH` or `REVIEW_REQUIRED` never authorizes an HH write.

## Durable behavior

The daily run id is deterministic: `daily-career-agent-YYYY-MM-DD` in UTC. The workflow store persists the run and every stage item before the terminal result. Repeated calls return a durable replay, and JSON/PostgreSQL coordinators claim the run atomically so separate processes cannot execute the same daily run concurrently. Stale interrupted runs are recovered only after the bounded stale window.

Completed daily runs also persist an immutable typed operator result on the existing `AgentRun`. A replay after process restart restores the original summary and deduplicated Attention Queue instead of re-running stages or returning zero-value counters. Legacy runs created before this payload existed are reconstructed only from durable run items and existing projections: unavailable raw-hit or route/AI diagnostics are reported as `unknown`, never guessed.

Terminal result codes are stable:

- `SUCCESS` — all configured stages completed;
- `PARTIAL_SUCCESS` — at least one stage or item failed, while successful results remain durable;
- `FAILED` — the run could not complete its durable lifecycle.

Errors are redacted before telemetry. No raw secrets, cookies, authorization headers, or full AI prompts are stored in normal run output.

## Attention Queue and Control Center

`AttentionItem` is a deterministic derived read model. It combines pending candidate clarifications, active notifications, and review/failed daily run items. It has no independent lifecycle or authority. Notification dismissal, candidate answers, approval, and HH sending continue to use their existing stores and flows.

The daily CLI prints the same operator-facing categories as JSON: AI reviewed, prepared, rejected, route ambiguous, low evidence, hard unknown, no suitable resume, and an Attention breakdown for application-ready, vacancy review, clarifications, replies, interviews, tests, offers, follow-ups, and other items. Existing backlog items may therefore keep Attention non-zero even when the current run has no new messages; active identities are deduplicated and resolved/dismissed projections are excluded.

## Storage parity

JSON remains the default backend. PostgreSQL mode uses additive migration `000017_drafts_clarifications` for `ai_drafts` and `candidate_clarifications`; payload JSON preserves the existing file schema while indexed input/identity keys provide replay idempotency. The migration is mirrored in both migration authorities and is applied by the existing migration runner.

No migration deletes user data. Existing JSON files are not read in PostgreSQL mode for these operational records.
