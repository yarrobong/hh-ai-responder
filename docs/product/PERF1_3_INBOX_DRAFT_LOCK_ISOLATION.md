# PERF-1.3 — Isolate Inbox Draft Worker From Shared Dashboard Lock

## 1. Status

`PASS` for the requested lock-isolation change.

The Inbox draft worker now snapshots its inputs under `DashboardServer.mu`,
performs context assembly, semantic retrieval, and AI preparation without that
mutex, then revalidates and commits under a short critical section. The change
does not alter frontend sequencing, response payloads, HH write behavior, or
the broader dashboard lock architecture.

## 2. Context

PERF-1.2 reduced the dashboard cold read path, exposing the remaining critical
path: `/api/inbox` launched `generateInboxDraft`, and the worker held the
shared dashboard mutex while preparing an employer reply. The preparation can
include context construction, semantic retrieval, an AI call, and local draft
or clarification persistence.

That made the following request wait behind unrelated background work:

```text
GET /api/inbox → draft worker → GET /api/notifications
```

## 3. Pre-Fix Baseline

The baseline below is taken from the PERF-1.2 audit and its safe local
measurements. It is retained as historical evidence; no new external-AI run
was made for this stage.

| Metric | Before PERF-1.3 |
| --- | ---: |
| Isolated `/api/notifications` | 0.158–0.168 s, median 0.163 s |
| `/api/notifications` after `/api/inbox` | 9.580 / 9.622 / 13.768 s |
| Full waterfall median | 10.419 s |
| Browser useful Overview median | 12.190 s |
| Inbox payload | approximately 954 KB |
| Draft-worker mutex hold | not isolated; first sequence included approximately 10.445 s aggregate held work with one extra background hold |

## 4. Existing Lock Model

`DashboardServer.mu` remains the caller-serialization boundary for local
stores. `ServeHTTP` holds it across local refresh, handler reads, and JSON
serialization. Sync commits continue to acquire the same lock through the
existing timed-lock adapter. The worker still uses that lock for its final
local commit because the local stores are not generally independent
concurrency boundaries.

The old Inbox path also acquired the lock around the complete draft lifecycle,
including the AI preparation phase. PERF-1.3 changes only that lifecycle.

## 5. Draft Worker Lifecycle

```text
Inbox request
  └─ short DashboardServer.mu snapshot
       ├─ canonical conversation + complete message history
       ├─ detached candidate/knowledge resolver copy
       └─ conversation/knowledge freshness tokens
  └─ draftMu bounded worker phase (no DashboardServer.mu)
       ├─ detached context construction
       ├─ optional semantic retrieval
       └─ AI preparation
  └─ short DashboardServer.mu commit
       ├─ refresh and freshness revalidation
       ├─ duplicate-draft check
       └─ persist decision and invalidate views
```

## 6. Root Cause

The previous worker called the persistence-owning employer workflow while the
dashboard mutex was held. That workflow rebuilt conversation context and
performed the AI request before saving a draft or clarification. The mutex
therefore became an accidental queue for all dashboard API requests.

## 7. Chosen Design

The implementation splits the existing orchestrator path into two explicit
operations:

- `PrepareEmployerReplyFromContext` performs only detached reply preparation.
- `PersistPreparedEmployerReply` performs only the existing typed local
  draft/clarification persistence during commit.

`BuildForReplySnapshot` provides the same context policy from caller-owned
copies. The synchronous compatibility method remains unchanged.

## 8. Snapshot Phase

`inboxDraftJob` stores a deep-copied `EmployerConversation`, its full message
timeline, a copied `CandidateContextResolver`, the configured semantic
retriever, and hashes of the original conversation and resolver state.

The snapshot is captured after Inbox has selected a `WorkflowNeedsReply`
conversation. No AI, HTTP, HH request, or semantic retrieval is performed
while taking it.

## 9. Preparation Phase

Preparation uses the same reply service, prompt construction, model, retry
configuration, validation, and candidate-context policy as the existing
orchestrator. Only the input source changes from a live store lookup to the
detached snapshot.

The optional semantic retriever is invoked after the dashboard mutex has been
released. This is the critical safety property of the stage.

## 10. Commit and Stale-Result Safety

Before AI and again during commit, the worker checks:

- the current conversation hash equals the snapshot hash;
- the current candidate/knowledge hash equals the snapshot hash;
- no generated draft already exists for the same conversation and input
  fingerprint.

The commit refreshes local files before revalidation. A changed employer
conversation or candidate knowledge causes the result to be discarded without
retry. A newly existing matching generated draft also causes a no-op. This
prevents a slow result from overwriting newer local state.

## 11. Duplicate Scheduling and Concurrency Bound

`statusMu` plus `draftTargets` continues to provide one in-flight worker per
conversation target. The existing `draftMu` now explicitly preserves the
former effective one-worker bound for expensive draft preparation across
different targets. It is independent of `DashboardServer.mu`, so dashboard
reads can proceed while a worker is waiting on or holding it.

The worker counters record started, active, and maximum-active workers for
diagnostics. No unbounded duplicate queue was introduced.

## 12. Lock Ordering and Race Safety

The worker order is bookkeeping, then `draftMu`, then short acquisitions of
`DashboardServer.mu`. No path added by this stage holds `DashboardServer.mu`
while waiting for `draftMu`, and no reverse lock order was introduced.

All detached slices and resolver data are copied before the worker starts.
Failure cleanup removes the target marker and releases both worker and
dashboard locks.

## 13. Focused Tests

Added `internal/runtime/dashboard_draft_lock_test.go` with a blocking fake AI:

- `TestInboxDraftWorkerDoesNotHoldDashboardLockDuringPreparation` verifies a
  blocked AI call does not block Inbox or Notifications and that a successful
  draft commits.
- `TestInboxDraftWorkerDiscardsStaleResult` changes the conversation while AI
  is blocked and verifies no stale draft is committed.
- `TestInboxDraftSchedulingDeduplicatesConcurrentTarget` verifies concurrent
  scheduling invokes AI once and commits one draft.
- `TestInboxDraftWorkerFailureReleasesBookkeeping` verifies malformed AI output
  releases the in-flight target and dashboard mutex.

## 14. Mutex Before / After

Before instrumentation could not isolate a worker hold numerically, but the
PERF-1.2 full sequence recorded approximately 9.693 s aggregate mutex wait and
10.445 s aggregate mutex-held work, with one extra hold beyond HTTP requests.

After PERF-1.3, a safe local process with the AI endpoint intentionally set to
an unavailable loopback address recorded these aggregate fixed-name counters
after the unique Inbox/Notifications probe:

| Counter | Calls | Total | Average |
| --- | ---: | ---: | ---: |
| `dashboard.inbox_draft.mutex_hold` | 8 | 0.327 s | 40.8 ms |
| `dashboard.inbox_draft.mutex_wait` | 8 | 0.757 s | 94.6 ms |
| `dashboard.notifications.mutex_wait` | 16 | 0.116 s | 7.3 ms |
| `dashboard.notifications.mutex_hold` | 16 | 2.797 s | 174.8 ms |

These are process aggregates, not a claim that every request has the average
duration. They demonstrate that the draft worker no longer contributes a
multi-second mutex hold. The notification counters include warm local reads
and the first queued worker, so they are not compared as a cold benchmark.

## 15. Request Waterfall Before / After

The PERF-1.2 full waterfall had a median of 10.419 s and was dominated by the
notification request waiting behind draft work. A new safe, warm local probe
used `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, disabled background Inbox
refresh, and an unavailable loopback AI endpoint. Query-string cache bypass was
used for the Inbox and Notifications samples.

| Run | Inbox | Notifications |
| ---: | ---: | ---: |
| 1 | 0.439 s | 0.383 s |
| 2 | 0.327 s | 0.167 s |
| 3 | 0.283 s | 0.166 s |
| 4 | 0.306 s | 0.159 s |
| 5 | 0.271 s | 0.173 s |
| **min / median / max** | **0.271 / 0.306 / 0.439 s** | **0.159 / 0.167 / 0.383 s** |

The after values are a safe loopback-AI profile, not a directly comparable
external-AI baseline. The useful comparison is the removal of the 9–14 second
shared-lock queue, confirmed by the focused blocking test and mutex counters.

## 16. Browser Before / After

The PERF-1.2 browser baseline was 12.168 / 12.190 / 14.181 s useful Overview
content. Three after reloads in the safe loopback-AI profile measured 0.985,
0.778, and 0.794 s; median **0.794 s**, with 354 DOM nodes.

Because the before and after runs differ in AI endpoint behavior, cache state,
and process warmness, these browser values are directional evidence only. No
frontend or payload optimization is claimed by this stage.

## 17. Remaining Bottlenecks

The current measured ranking is:

1. `/api/inbox` local read/projection and its approximately 954 KB response,
   around 0.306 s median in the safe unique probe.
2. `/api/notifications` local read/projection, around 0.167 s median after
   the first queued sample.
3. `/api/dashboard` cold read model, approximately 0.13 s in the same safe
   local process; warm reads are cache-dependent.

The next bottleneck is therefore payload/read shaping for Inbox, not another
shared-lock change.

## 18. Verification

All required checks passed after the implementation:

```text
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go build ./cmd/hh-ai-responder
node --check web/app.js
node --check internal/runtime/web/app.js
git diff --check
```

Focused tests and the focused race run also passed.

## 19. Safety Confirmation

- No HH write endpoint was called.
- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` were used for runtime probes.
- No application, test, chat, resume, job-search-status, or other HH write was
  attempted.
- No external AI endpoint was used for the blocking tests or runtime probe.
- No secrets, cookies, prompts, raw AI bodies, or private candidate data were
  added to logs or committed.
- Existing local changes and the untracked `out` path were preserved.

## 20. Recommended Next Stage

`PERF-1.4 — Shape Overview Inbox Payload`
