# PERF-1.7 — Read-only Dashboard Mutex Concurrency

## 1. Status

`PASS` for the read-path lock refinement and race verification.

The dashboard server now distinguishes proven shared-read projections from
refresh, sync, draft-commit, notification lifecycle, and other mutable paths.
The initial Overview projections can hold a shared dashboard read lock at the
same time. Writers and local reloads remain exclusive.

This stage does not change frontend sequencing, polling, database schema, HH
write gates, notification rules, Inbox contracts, or draft-worker policy.

## 2. Ownership Audit

The old `DashboardServer.mu` was a caller-serialization boundary for local
stores, refresh/reload, view-cache mutation, HTTP handlers, and sync commits.
PostgreSQL repositories already use the pool/transaction boundary and do not
need a dashboard-global lock. The local JSON-backed stores are not generally
independent transaction boundaries, so their mutations continue to run under
the exclusive dashboard lock.

| Path | Reads state | Mutates state | Filesystem reload | Schedules work | Persists data | Lock requirement |
| --- | --- | --- | --- | --- | --- | --- |
| `ServeHTTP` static page | embedded assets | no | no | no | no | no dashboard lock |
| `refreshLocalFiles` | file metadata | may replace local stores, invalidate cache, advance generation | yes | no | no | exclusive dashboard lock when reloading; internal refresh mutex serializes checks |
| `/api/dashboard` | PostgreSQL vacancy/application/event/conversation projections, drafts, clarifications, sync state, knowledge and audit snapshot | no after removing `Audit.Reload` from analytics | no in handler; refresh phase handles it | no | no | shared dashboard read lock |
| `/api/sync/status` | sync state/progress/performance counters | no | no | no | no | existing status/store locks; no dashboard lock |
| `/api/inbox/overview` | bounded conversations, applications, events, drafts, clarifications, resolver state | QualityLog observations; starts deferred draft snapshot worker | refresh phase only | yes, deferred | QualityLog may persist observations | shared dashboard read lock plus QualityLog's own lock |
| `/api/notifications/overview` | applications, events, conversations, clarifications and notification lifecycle | notification reconciliation, `shown` feedback, creation metadata | refresh phase only | no | notification store and QualityLog may persist | shared dashboard read lock plus notification projection lock and QualityLog's own lock |
| `/api/inbox` | full conversations, drafts, clarifications and career projection | QualityLog observations; starts draft worker | refresh phase only | yes | QualityLog may persist | exclusive dashboard lock; full route is not classified as a pure read |
| `/api/notifications` | full notification projection and its dependencies | reconciliation, lifecycle-derived notification state, `shown` feedback | refresh phase only | no | notification store and QualityLog may persist | exclusive dashboard lock; full route remains conservative |
| `generateInboxDraft` snapshot | conversation and resolver state | no during preparation | no | already scheduled | no | short exclusive snapshot/commit phases; no dashboard lock during context/AI preparation |
| `generateInboxDraft` commit | current conversation, resolver and drafts | draft/clarification/decision state and view invalidation | yes | no | drafts/clarifications | exclusive dashboard lock |
| sync read/fetch | HH read responses | no while waiting for HH | no | operation bookkeeping | no | no dashboard lock while waiting; exclusive commit through `externalCommitMu` |
| sync commit paths | imported vacancies, applications, conversations, state | local store replacement and sync state | no | no | local stores/state | exclusive dashboard lock at commit |
| notification lifecycle POST | notification record | seen/dismiss/resolve/snooze/irrelevant state | no | no | notification store and QualityLog | exclusive dashboard lock |
| QualityLog `RecordMany` | current event list | in-memory event list | no | no | one atomic batch save when changed | QualityLog internal RWMutex; not dashboard-global by itself |
| draft store | current drafts | create/status/text changes | Load can replace state | no | explicit Save | caller-exclusive dashboard lock |
| clarification store | current clarifications | create/status/answer/proposal changes | Load can replace state | no | explicit Save | caller-exclusive dashboard lock |
| generation/version state | current server/cache generation | refresh/invalidation increments generation | no | no | no | cache mutex plus exclusive mutation paths |

## 3. Chosen Lock Model

`DashboardServer.mu` is now an explicit `sync.RWMutex`:

```text
read request
  → refreshForRead()
      → stat-only check
      → exclusive reload only when a file changed
  → RLock
      → read model / projection / JSON response

write or sync commit
  → Lock
      → refresh, mutate, persist, invalidate
```

The stat-only check is separate from the read lock. Consequently, a second
read request does not acquire an exclusive dashboard lock merely to repeat
`os.Stat` while another read handler is running. If a tracked file changed,
the reload still takes the exclusive lock before the request proceeds.

`viewCache`, `generation`, and refresh metadata have their own narrow locks.
This is required because multiple shared readers may populate or inspect the
cache concurrently; an `RWMutex` on the dashboard state alone would not make
the cache map safe for concurrent writes.

## 4. Read-only Classification

The shared-read classification is deliberately narrow:

- `/api/dashboard` uses the request-scoped `dashboardSnapshot`, whose
  PostgreSQL collections are detached read results and whose local collections
  are protected from writers by the dashboard lock.
- `/api/inbox/overview` computes from bounded/request-local projection data.
  Its existing QualityLog observation and deferred draft scheduling remain
  explicit side effects; they are not treated as proof of pure read behavior.
- `/api/notifications/overview` retains the existing notification semantics,
  including reconciliation and `shown` observations. Those side effects are
  serialized by `notificationMu`, while the broader dashboard read lock is
  shared with independent read projections.
- `/api/sync/status` was already independent through `statusMu` and sync-store
  locks; it does not need the dashboard mutex.

The full Inbox and full Notifications endpoints remain under the conservative
exclusive path because they expose broad compatibility projections and have
side effects. This stage optimizes the initial Overview endpoints without
silently relabelling every GET as read-only.

## 5. Refresh Semantics

`refreshLocalFiles()` still tracks every existing local path and reloads a
store only after its file metadata changes. It does not become a no-op and it
does not move persistence into a read helper.

The refresh mutex serializes metadata checks and reload bookkeeping. The
exclusive dashboard lock is acquired by `refreshForRead()` only for an actual
reload, preventing partial replacement from being visible to a shared
projection. Normal writers already take the same exclusive lock. Existing
atomic file writes and process file locks remain unchanged.

## 6. Notification Side Effects

Notification calculation is not pure: the engine can reconcile lifecycle
state, the delivery-uncertain path can add records, and the active projection
records idempotent `shown` observations. PERF-1.7 therefore does not claim a
pure notification read.

The new `notificationMu` serializes the notification projection/reconciliation
sections that may run concurrently because the outer dashboard lock is shared.
QualityLog remains independently synchronized by its existing internal
RWMutex, and `RecordMany` keeps the PERF-1.1 one-batch durable-save behavior.
Notification IDs, ordering, lifecycle semantics, unread count, and feedback
identity are unchanged.

## 7. Store and Database Findings

- PostgreSQL vacancy, application, event, and conversation reads are safe
  through the existing pool/transaction boundary. The dashboard-specific
  conversation projection remains unchanged from PERF-1.2/1.5.
- JSON repositories already return detached values and own their repository
  mutexes. Dashboard-specific application/event getters and root conversation
  read models avoid `syncCompatibilityState`, whose legacy purpose may persist
  direct mirror edits. No shared backing slice is exposed by the optimized
  PostgreSQL dashboard reads, and a legacy mirror discrepancy is used only as
  a detached request-local value.
- Draft and clarification mutations remain caller-serialized. Their lists are
  copied before projection and are not exposed as response-owned mutable
  backing slices.
- QualityLog owns its own synchronization and atomic persistence. The
  dashboard lock is not held merely to make independent QualityLog calls safe.
- Sync state/progress own their existing locks. HH network waiting remains
  outside the dashboard lock; only local commit sections are exclusive.

## 8. Tests

Added `dashboard_mutex_test.go`:

- blocks the narrow notification projection lock;
- verifies `/api/notifications/overview` has already acquired the shared read
  side of the dashboard lock;
- verifies `/api/dashboard` completes while that notification projection is
  blocked;
- verifies the blocked notification request does not complete until its own
  side-effect lock is released.

Existing dashboard, notification, Inbox draft, stale-result, QualityLog and
read-model parity tests remain in place.

## 9. Verification

```text
gofmt -w internal/runtime/dashboard_server.go internal/runtime/dashboard_performance.go internal/runtime/dashboard_views.go internal/runtime/dashboard_mutex_test.go  PASS
go test -count=1 ./internal/runtime                                                        PASS
go test -race ./internal/runtime                                                           PASS
go test -race ./...                                                                       PASS
```

No HH write endpoint was called. Tests use local fixtures and fake HH/AI
clients. No external AI request, application, test, chat, resume, or
job-search-status write was performed.

## 10. Safety Confirmation

```text
HH_DRY_RUN=true                         preserved
HH_WRITE_ENABLED=false                  preserved
Live HH writes                         0
HH retry behavior                       unchanged (NONE)
Candidate truth / Knowledge             unchanged
Notification semantics                 unchanged
Inbox and full notification contracts  unchanged
Sync/write exclusivity                 preserved
Docker                                  not used
Secrets/private raw responses           not added
```

## 11. Remaining Work

This stage proves lock classification and concurrency safety through focused
blocking tests and the full race suite. It does not claim a new production
latency median because no comparable live PostgreSQL replay was run as part of
this code-only lock stage. A follow-up runtime replay may measure the benefit
under the same PERF-1.6 safe profile.
