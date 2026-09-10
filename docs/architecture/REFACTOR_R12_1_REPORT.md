# Before

`CareerMonitor.Run` owned the recurring timer, cancellation, iteration loop,
iteration error handling, and completion-based next-run scheduling.

`CareerMonitor.RunOnce` owned the full synchronous iteration:

1. validate the sync, reconciler, and notification dependencies;
2. set `LastRunAt` and call `RefreshInbox`;
3. record sync evidence and increment the consecutive-failure counter;
4. after three sync failures, add and persist the local `sync_problem`
   notification (save errors ignored);
5. persist failure state (save errors ignored) and return the first sync error;
6. call `CareerDataReconciler.Reconcile(false)`;
7. resolve conversation/application state and persist conversation state;
8. load vacancies, applications, conversations, timelines, and clarifications;
9. construct the existing `CareerSnapshot`;
10. calculate and persist local notifications unless quiet hours are active;
11. persist monitor state and reset the failure counter on success.

The monitor had no HH write capability. `CareerDataReconciler` remained local
relation/projection repair and was not delivery reconciliation.

# Characterization

| Phase/failure | Current behavior | Preserved |
| --- | --- | --- |
| Successful iteration | Refresh, reconcile, resolve, snapshot, audit, notifications, notification save, then monitor-state save; returns nil. | Yes |
| `RefreshInbox` error or sync result error | Increments failures, sets `NextRunAt`; on failure 3 adds `sync_problem`; notification/state save errors are ignored; returns a new error containing the first sync error. | Yes |
| Sync recovery | Successful completion resets `ConsecutiveFailures` to zero. | Yes |
| Reconciliation error | Increments failures, ignores monitor-state save failure, stops before resolution/snapshot/notifications, returns the reconciler error. | Yes |
| Resolution conversation/application read error | Stops and returns the read error; no final monitor-state save. | Yes |
| Vacancy/application/conversation snapshot read error | Error is ignored and the corresponding snapshot portion is empty/nil; later stages continue. | Yes |
| Timeline read error | Error is ignored for that application; other timelines and later stages continue. | Yes |
| Clarification read error | Error is ignored; snapshot keeps the empty clarification collection. | Yes |
| Notification calculation error | Stops and returns the error; final monitor-state save is not attempted. | Yes |
| Notification save error | Stops and returns the error; final monitor-state save is not attempted. | Yes |
| Monitor-state save error | State is updated in memory, then the save error is returned. | Yes |
| Quiet hours | Notification calculation and notification persistence are skipped; the iteration still completes and persists successful monitor state. | Yes |
| Context cancellation | Caller context is passed to sync/reconciliation and context-aware local capabilities. The scheduler still returns `ctx.Err()` when cancellation is observed. | Yes |

The ignored snapshot-read behavior is intentional compatibility behavior, not
a new observability policy. It is covered by
`internal/usecase/careeriteration/service_test.go`.

# Classification

| Symbol/responsibility | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| `CareerMonitor.Run` | `CareerMonitor` | `CareerMonitor` | Scheduler remains in root. |
| One iteration sequence | `CareerMonitor.RunOnce` | `careeriteration.Service.Run` | Extracted; one authoritative owner. |
| `RefreshInbox` | `HHReadSyncService` | `HHReadSyncService`, through `InboxRefresher` | Existing R9 boundary reused. |
| `CareerDataReconciler` | `CareerDataReconciler` | `CareerDataReconciler`, through a narrow adapter | Logic not moved. |
| Conversation state policy | `ConversationStateResolver` / `conversationpolicy` | Same policy, through `StateResolver` | No duplicate state rules. |
| `CareerSnapshot` | Root compatibility value | Root compatibility value, assembled from `careeriteration.Snapshot` | Type migration deferred to avoid compatibility churn. |
| Notification calculation | `CandidateNotificationEngine` | Same engine, through `NotificationProcessor` | Algorithm and persistence remain in root. |
| Audit report | Root audit builder | Same builder, through `Auditor` | No new dashboard read model. |
| Monitor state persistence | `CareerMonitor.saveState` | Same root persistence, through `StateStore` | JSON shape and locking preserved. |
| Dashboard and CLI workflows | Root callers | Root callers | Not migrated in R12.1. |

# Career iteration usecase

Package: `internal/usecase/careeriteration`

Service: `NewService(deps Dependencies, options Options) *Service` and
`(*Service).Run(context.Context) (Result, error)`.

Dependencies are narrow consumer-owned capabilities for inbox refresh,
reconciliation, vacancy reads, application/timeline reads, conversation state
read/write, clarification reads, state resolution, notifications, audit, and
monitor-state persistence. The package does not import a concrete HH read
adapter, JSON/Postgres adapter, CLI, dashboard, AI, or HH write package.

Options contain only the existing interval used for persisted `NextRunAt`, the
quiet-hours value used by notification calculation, and a clock seam for
deterministic tests. The service has no timer, ticker, retry, goroutine, or
next-run policy.

Result contains sync evidence, reconciliation evidence, the assembled snapshot,
notification counters, and the current monitor state. It contains no scheduler
instruction.

# Iteration sequence

1. Call `InboxRefresher.RefreshInbox(ctx)` and retain the established
   `HHSyncAllResult`-shaped evidence.
2. Apply the existing sync error ordering, threshold, notification, and
   failure-state behavior.
3. Call `Reconciler.Reconcile(ctx, false)`.
4. Resolve local conversation state using the existing resolver and persist
   the same state fields in the same order.
5. Read vacancies, applications, conversations, timelines, and clarifications
   with the existing ignored-error distinctions.
6. Assemble the existing snapshot data set.
7. Build the existing audit report.
8. Calculate and save local notifications outside quiet hours.
9. Set success timestamps, reset failures, set `NextRunAt`, and save monitor
   state.

Behavior: **UNCHANGED**.

# HH boundary

HH reads: the extracted use case receives only the narrow `InboxRefresher`
interface; root composition adapts `HHReadSyncService.RefreshInbox`.

HH writes: **NONE**. The extracted package has no write-shaped dependency and
does not import `internal/ports/hhwrite`, `internal/adapters/hh/write`,
`internal/usecase/hhwritegateway`, `internal/usecase/writeapproval`,
`internal/usecase/hhwritepreflight`, or `internal/usecase/hhwritereconcile`.

# Career reconciliation

Owner: `CareerDataReconciler`.

Called by: `careeriteration` through `Reconciler`.

Logic moved: **NO**. Relation repair, partial application creation, vacancy
enrichment, match backfill, and event creation remain in the existing
reconciler.

# Snapshot

Type: `careeriteration.Snapshot` is a neutral orchestration input containing
the same vacancies, applications, conversations, events, clarifications, and
empty consistency map used by the monitor's existing `CareerSnapshot`.

Construction: after resolution persistence, using the same read ordering and
the same ignored read errors. Timelines are appended only when their reads
succeed.

Partial-read behavior: preserved. Snapshot read failures do not become fatal in
this stage; they produce the existing empty/partial snapshot behavior.

# Notifications

Engine: existing `CandidateNotificationEngine.Calculate`.

Persistence: existing `NotificationStore.Save`.

Fingerprints, cooldowns, lifecycle transitions, deduplication, and local
sync-problem notification behavior are unchanged.

Quiet hours: the existing `HH:MM-HH:MM` semantics remain notification input;
they do not control scheduler timing.

# Monitor state

Shape: the root `CareerMonitorState` JSON envelope and field names remain
unchanged.

Failure counter: incremented on a sync failure or reconciliation failure;
three consecutive sync failures produce the existing local notification.

Reset behavior: successful completion resets the counter to zero. Notification
or final monitor-state failures prevent the success/reset sequence, as before.

# Error behavior

Sync: first error text wins in the established vacancies → applications →
conversations → returned-error order; state and threshold side effects match
the previous implementation.

Reconcile: returned unchanged and iteration stops.

Snapshot reads: vacancy, application, conversation, timeline, and clarification
errors retain their existing ignored behavior after the fatal resolution reads.

Notifications: calculation/save errors return unchanged and prevent final
monitor-state persistence.

State persistence: sync/reconciliation failure-save errors remain ignored;
final successful-save errors are returned.

Cancellation: context is passed through the extracted read/reconciliation
boundary; `CareerMonitor.Run` retains its existing cancellation return behavior.

# Scheduler

Owner: `CareerMonitor.Run` / root.

Cadence: existing default 15 minutes, timer type, completion-based wait, and
error-to-next-iteration behavior.

Changed: **NO**.

# Root compatibility

`CareerMonitor.RunOnce` now delegates to `careeriteration.Service.Run` and
projects the returned state back into the root compatibility state, including
partial state visible when a later phase fails. It contains no second
iteration algorithm.

`runMonitorCommand` continues to construct and run the root monitor with the
same CLI behavior. Dashboard sync and other workflows are unchanged.

# Dependencies

```text
CareerMonitor.Run
        ↓
careeriteration.Service.Run
        ↓
  InboxRefresher (RefreshInbox only)
  Reconciler (CareerDataReconciler adapter)
  local read/state capabilities
  existing resolver
  existing notification engine
  local state saver
```

The package has no timer/cadence, HH write, AI, dashboard, CLI, or concrete
storage-adapter dependency.

# Duplicate iteration audit

- `CareerMonitor.RunOnce`: compatibility delegate only.
- `careeriteration.Service.Run`: sole monitor iteration owner.
- `HHReadSyncService.RefreshInbox`: R9 read/sync owner, not a duplicate Career
  iteration.
- `CandidateNotificationEngine.Calculate`: notification algorithm owner, not a
  duplicate orchestration sequence.
- Dashboard snapshot/sync paths: unchanged R12.2 scope and not equivalent to
  the monitor iteration.
- `CareerDataReconciler`: local reconciliation owner, invoked once by the
  iteration.

# Tests

Added `internal/usecase/careeriteration/service_test.go` with fake
dependencies covering:

- successful sequence and call ordering;
- sync failure threshold, sync recovery, and reset;
- reconciliation failure;
- vacancy/application/conversation/timeline/clarification read failures;
- notification calculation/save failures;
- monitor-state save failure;
- context cancellation;
- notification result propagation and partial snapshot behavior.

Existing root monitor and notification/reconciliation tests remain in place,
including the read-only/idempotent `CareerMonitor.RunOnce` integration test.

# Behavior

UNCHANGED for scheduler cadence, read count/type, reconciliation call count,
snapshot construction, notification behavior, monitor state shape, quiet hours,
dry-run/write boundaries, and current partial-read semantics.

# Verification

`gofmt -w .`: PASS
`go test -count=1 ./...`: PASS
`go test -race ./...`: PASS
`go vet ./...`: PASS
`go build ./...`: PASS
`git diff --check`: PASS
`node --check web/app.js`: PASS
`go test ./internal/usecase/careeriteration/...`: PASS
`go test ./internal/usecase/hhreadsync/...`: PASS
`go test ./internal/adapters/hh/read/...`: PASS
`go list ./...`: PASS
dependency audit for extracted package and `hhreadsync`: PASS; no forbidden
write dependencies found.
Docker build: SKIPPED — Docker CLI is installed, but the daemon is unavailable
at `/var/run/docker.sock`.

LIVE HH WRITES: **0**.

# R12 status

R12.1 Career iteration: **EXTRACTED**

# Ready for next stage

R12.2 — Conversation / Inbox / Follow-up Orchestration: **READY**

R12.1 stops here. No R12.2–R12.6 work was started.
