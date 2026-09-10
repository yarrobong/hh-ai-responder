# Before

Recurring loops:

- `HHAIResponder.Run` started four independent completion-based loops. Each
  invoked its task immediately, then waited after completion: resume touch (4h),
  job-search status (24h), auto-apply (12h executable behavior), and auto-chat
  (15m).
- `CareerMonitor.Run` loaded state, ran `RunOnce` immediately, then waited its
  configured interval after completion.
- Dashboard background inbox refresh waited for `MonitorInterval`, then used a
  fixed ticker to trigger the existing asynchronous/coalescing queue.
- `runMonitorCommand` maintained a separate one-minute process-lock heartbeat.
- The HH read adapter maintained its own rate/concurrency scheduler and was
  explicitly excluded from this stage.

Timer locations before extraction included `main.go` for the four business
loops and HH read scheduling, `career_monitor.go` for CareerMonitor, and
`dashboard_sync.go` for the dashboard ticker. Other timer sites are LLM or
semantic/provider retry seams and are not recurring business schedulers.

Startup behavior was immediate for the four root recurring tasks and
CareerMonitor. Their registration order was touch, job status, auto-apply,
auto-chat; each task ran synchronously inside its own goroutine. Dashboard
background refresh had no immediate run and waited one `MonitorInterval`.

Shutdown was context-driven for business loops and dashboard work. The monitor
process heartbeat also stopped on context cancellation or its local completion
signal. Run-once commands bypassed all recurring waits.

# Scheduler classification

| Loop | Type | First run | Interval | Current owner |
|---|---|---|---|---|
| Resume touch | A. completion-based task loop | immediately | 4h | root `HHAIResponder.Run` |
| Job-search status | A. completion-based task loop | immediately | 24h | root `HHAIResponder.Run` |
| Auto-apply | A. completion-based task loop | immediately | 12h | root `HHAIResponder.Run` |
| Auto-chat | A. completion-based task loop | immediately | 15m | root `HHAIResponder.Run` |
| Career monitor | A. completion-based task loop | immediately after state load | configured, default 15m | `CareerMonitor.Run` |
| Dashboard inbox refresh ticker | B. fixed ticker | wait first | configured `MonitorInterval`, default 15m | scheduler ticker wired by dashboard |
| Dashboard inbox refresh queue | C. coalescing background queue | trigger-dependent | one queued/running target per key; maximum 8 targets | dashboard root |
| Monitor process-lock heartbeat | D. process-liveness heartbeat | wait first | 1m | `runMonitorCommand` |
| HH read scheduler | E. transport rate limiter | adapter-controlled | adapter-controlled | HH read adapter |
| LLM/semantic retries | F. other/provider or use-case retry | request-dependent | request-dependent | provider/use-case boundary |

# Characterization

| Task | Existing semantics | Preserved |
|---|---|---|
| Resume touch | Immediate; synchronous; errors logged; wait 4h after return; no overlap | Yes |
| Job-search status | Immediate; synchronous; unsuccessful result/error logs warning; wait 24h after return | Yes |
| Auto-apply | Immediate; synchronous batch; error logged; wait 12h after return | Yes |
| Auto-chat | Immediate; synchronous; error logged; wait 15m after return; next run is a new workflow | Yes |
| Career monitor | Load state, immediate `RunOnce`, wait configured interval after return | Yes |
| Dashboard background refresh | Wait-first ticker; queue target is coalesced and asynchronously executed | Yes |
| Process heartbeat | Wait-first one-minute ticker touches the lock; independent of business iterations | Unchanged |

Completion loops do not overlap: the next timer is created only after the
current task returns. Ordinary task errors do not abort the loop or shorten the
cadence. Cancellation stops a pending timer and is checked before a future task
invocation. No panic recovery was added or removed. Existing task behavior and
write gates remain in their compatibility/workflow boundaries.

# Scheduler boundary

Package: `internal/platform/scheduler`.

Types:

- `Task func(context.Context) error` for one synchronous iteration;
- `Loop` for immediate, completion-based recurring work;
- `Ticker` for wait-first fixed-timer triggers;
- narrow `Clock`/`Timer` seams and `RealClock` for production composition;
- small `Logger` interface for scheduler-level task-error logging.

Task contract: the scheduler invokes an opaque task, does not inspect its
business result, does not read configuration, and does not select retries.

Timer/clock seam: production uses `scheduler.RealClock`; tests use an in-memory
timer implementation, so no 4h/12h/24h sleep is required.

The package has no environment/config access and no imports of HH adapters,
HH read/write ports, AI, candidate stores, dashboard, or use cases.

# Completion-based loops

Auto apply: root composition registers the existing `ApplyVacancies` batch as
an opaque task with `12 * time.Hour`. Its existing prepare → fresh submit → R11
gateway path is unchanged.

Auto chat: root composition registers the existing `AutoRespondChats` facade
with `15 * time.Minute`. It still delegates to `autochatorchestration`; chat
mode, send, leave, and ambiguity handling remain outside the scheduler.

Resume touch: root composition registers `TouchResume` with 4h. Existing
maintenance write gating is unchanged.

Job status: root composition registers `SetActiveJobSearchStatus` with 24h.
Existing maintenance write gating is unchanged.

Career monitor: `CareerMonitor.Run` loads state and delegates the recurring
mechanics to `scheduler.Loop`; its one-iteration business delegate remains
`RunOnce`/`careeriteration.Service.Run`.

# Dashboard background refresh

Ticker: the wait-first timer is now supplied by `scheduler.Ticker`.

Queue: dashboard code still owns `queueBackgroundInboxRefresh`, async
execution, the maximum eight-target limit, and status tracking.

Coalescing: the `inbox` target remains deduplicated while running. Manual and
targeted refresh routes retain their existing behavior and API responses.

Moved: PARTIAL.

Why: the dashboard trigger is materially different from a completion-based
business loop. Moving the timer trigger is safe; moving queue/coalescing policy
would risk changing duplicate targets, concurrent manual/background triggers,
and the maximum queued follow-up behavior. The intentional residual queue
mechanics are R12.6 inventory.

# Process heartbeat

Owner: `runMonitorCommand` and `internal/platform.ProcessLock`.

Changed: NO. The one-minute ticker, lock file shape, acquisition/release,
stale threshold, and `Touch` behavior are unchanged. It is liveness
infrastructure, not a business iteration.

# HH read rate limiter

Owner: HH read adapter (`HHRequester`).

Changed: NO. Its safe-read concurrency, queue priority, wake channel, and
transport timing remain adapter-owned.

# Task ownership

| Task | Scheduler owner | Iteration/business owner |
|---|---|---|
| Resume touch | `scheduler.Loop` wired by root | `HHAIResponder.TouchResume` / R11 gateway |
| Job-search status | `scheduler.Loop` wired by root | `HHAIResponder.SetActiveJobSearchStatus` / R11 gateway |
| Auto-apply | `scheduler.Loop` wired by root | `ApplyVacancies` → application processing/submission → R11 gateway |
| Auto-chat | `scheduler.Loop` wired by root | `AutoRespondChats` → auto-chat orchestration → R11 gateway |
| Career monitor | `scheduler.Loop` delegated by `CareerMonitor` | `RunOnce` → career iteration |
| Inbox background trigger | `scheduler.Ticker` wired by dashboard | dashboard queue → `inboxrefresh.Service.Run` |

# Write safety

Auto apply path: scheduler → `ApplyVacancies` compatibility/batch boundary →
`applicationprocessing` → `applicationsubmission` → R11 gateway.

Auto chat path: scheduler → `AutoRespondChats` compatibility boundary →
`autochatorchestration` → R11 gateway.

Touch path: scheduler → `TouchResume` → R11 gateway.

Job status path: scheduler → `SetActiveJobSearchStatus` → R11 gateway.

Scheduler HH capability: NONE.

HH write retry: NONE. A later scheduled iteration is a new workflow run, not
an immediate retry of the same write action. Cross-run application ambiguity
residual risk is unchanged.

# Timing

Auto apply: 12h actual code. The nearby old comment says 24h; this stage keeps
the executable 12h contract and does not change the comment.

Auto chat: 15m after completion.

Touch: 4h after completion.

Job status: 24h after completion.

Career: configured monitor interval, default 15m, after completion.

Dashboard: wait-first fixed `MonitorInterval` ticker, default 15m, with
existing coalescing queue semantics.

# First-run semantics

Immediate then wait: resume touch, job-search status, auto-apply, auto-chat,
and CareerMonitor after its required state load.

Wait then trigger: dashboard background inbox refresh and monitor process-lock
heartbeat.

Run-once commands call their existing one-iteration methods directly and do
not enter a scheduler wait.

# Error behavior

Completion task errors are logged and followed by the normal interval. There
is no immediate retry, exponential backoff, retry queue, or business-result
interpretation in the generic scheduler. Existing task-specific logging and
safe R11 behavior remain in the task adapters.

# Cancellation / shutdown

`scheduler.Loop` checks cancellation before every task, selects cancellation
while waiting, stops the timer on cancellation, and returns the context error.
`scheduler.Ticker` applies the same waiting shutdown behavior and checks again
before invoking its trigger. Tasks receive the same context; no next iteration
starts after cancellation. Root/dashboard/monitor lifecycle ownership remains
unchanged.

# Root compatibility

`HHAIResponder.Run`: retains startup logging, run-once bypass, feature gates,
task registration order, independent goroutine lifecycle, and context wait;
bespoke business timer loops are removed.

`CareerMonitor.Run`: remains the compatibility entry point and now delegates
to `scheduler.Loop` around `RunOnce`.

Dashboard: HTTP routes and API responses are unchanged. Manual refresh remains
direct; background refresh still goes through dashboard coalescing and then
`inboxrefresh`.

# Remaining root scheduler debt

The dashboard async queue/coalescing machinery remains intentionally in the
dashboard root because it is not a completion loop. The monitor process-lock
heartbeat remains intentionally in monitor command infrastructure. HH read
transport scheduling and provider/use-case retry timers remain outside this
business scheduler boundary.

# R12.6 inventory

`main.go`: remaining HH read scheduler timer and compatibility composition.

`runHHCommand`: command composition and direct one-shot operations remain
outside R12.5.

`DashboardServer.writeAPI`: HTTP write/API behavior was not refactored.

`executeSync` non-inbox modes: existing full and targeted sync behavior is
unchanged.

Compatibility facades: existing root-to-usecase adapters remain the business
workflow boundary.

JSON/Postgres composition: unchanged.

Root timer leftovers: HH read scheduler, process heartbeat, dashboard queue
trigger boundary, and provider/use-case retry seams; no duplicate business
completion scheduler remains in root.

# Tests

Added deterministic scheduler tests for:

- completion timing after task completion;
- no overlap while a task is blocked;
- error followed by normal cadence, with no immediate retry;
- cancellation while waiting;
- cancellation during a task;
- wait-first ticker behavior;
- exact 12h/15m/4h/24h root cadence constants.

Existing career iteration, inbox refresh, auto-chat orchestration,
application processing, application submission, HH write gateway, and full
repository tests remain applicable and unchanged.

# Timer audit

| Occurrence | Classification | R12.5 action |
|---|---|---|
| `internal/platform/scheduler` real timer | normal cadence | new infrastructure owner |
| root recurring task timers | normal cadence | extracted |
| CareerMonitor timer | normal cadence | extracted |
| dashboard ticker | dashboard coalescing trigger | moved to narrow ticker primitive; queue retained |
| monitor command ticker | process heartbeat | unchanged |
| HHRequester timers | safe HH read rate limiter | unchanged/excluded |
| LLM and semantic/use-case timers | provider/use-case retry | unchanged/excluded |

# Retry audit

HH write retry: NONE.

Remaining retry/timer occurrences are normal scheduler cadence, safe HH read
transport scheduling, provider/LLM retry, semantic/use-case retry, dashboard
coalescing, or process heartbeat. No scheduled write closure is re-invoked
immediately by the scheduler.

# Dependencies

`go list -deps ./internal/platform/scheduler` resolves to the scheduler package
and the Go standard library only. The scheduler has no dependency on HH write,
HH adapters, AI, candidate stores, dashboard, CLI, application processing, or
auto-chat orchestration.

`go list ./...`: PASS.

# Behavior

Auto-apply remains actual 12h behavior; the 24h comment mismatch is unchanged.
Auto-chat remains 15m completion-based. Resume touch remains 4h, job status
remains 24h, and CareerMonitor remains default 15m. First-run semantics,
completion timing, no-overlap behavior, error cadence, cancellation, run-once
bypass, feature gates, storage, API/CLI behavior, and R11 write safety are
unchanged.

# Verification

gofmt: PASS

go test -count=1 ./...: PASS

go test -race ./...: PASS

go vet ./...: PASS

go build ./...: PASS

git diff --check: PASS

node --check web/app.js: PASS

Focused scheduler tests: PASS

Focused R12 use-case suites: PASS as part of `go test -count=1 ./...`.

Explicit focused suites for career iteration, inbox refresh, auto-chat
orchestration, application processing, application submission, and HH write
gateway: PASS.

Docker: SKIPPED — Docker CLI is installed, but the daemon is unavailable
(`Cannot connect to the Docker daemon at unix:///var/run/docker.sock`).

LIVE HH WRITES: 0.

# R12 status

R12.5: EXTRACTED

# Ready

R12.6 — Residual Root / Composition Cleanup
READY
