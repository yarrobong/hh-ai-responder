# PERF-1.6 — Frontend Request Sequencing and Dashboard Bootstrap Reuse

## 1. Status

`PASS`. The initial Overview no longer fetches dashboard twice during one
render cycle, and the independent Inbox/Notifications Overview reads are
started together. Backend contracts and local workflow semantics are
unchanged.

Measurements used `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and
`HH_BACKGROUND_INBOX_REFRESH=false`. Docker was not used.

## 2. Context

PERF-1.5 left the initial Overview with a fast dashboard bootstrap followed
by two narrow read endpoints. The frontend still waited for the bootstrap
indicator/status Promise and then fetched dashboard again in `overview()`;
Inbox and Notifications were then fetched sequentially.

The served asset is `internal/runtime/web/app.js`. `web/app.js` is the root
copy used for source maintenance. The two files already contain unrelated
reliability-only differences in this checkout; the PERF-1.6 sequencing changes
were applied to both copies identically.

## 3. Pre-Fix Baseline

The current pre-fix source had this initial request shape:

```text
render()
  ├─ GET /api/dashboard              updateNotificationIndicator
  ├─ GET /api/sync/status            updateGlobalSyncStatus
  └─ after both resolve:
       ├─ GET /api/dashboard          overview
       ├─ GET /api/inbox/overview
       └─ GET /api/notifications/overview
```

Structural counts per initial Overview render were therefore dashboard `2`,
sync `1`, Inbox `1`, and Notifications `1`.

A fresh safe local replay of the pre-fix source ran the equivalent ten-cycle
sequence. The cycle totals were 397, 396, 142, 144, 149, 143, 145, 153, 163,
and 218 ms: min / median / max **142 / 151 / 397 ms**. This is a local replay
with the existing 15-second dashboard view cache; it is not a claim about a
cold process on every run.

Browser reloads on the same pre-fix checkout measured useful Overview content
at 520, 361, 172, 173, and 154 ms: min / median / max **154 / 172 / 520 ms**.
The document contained **364 DOM nodes** on every run.

## 4. Initial Render Dependency Graph

| Request | Caller | Requires previous result? | Mutates local state? | Can run concurrently? |
| --- | --- | --- | --- | --- |
| `/api/dashboard` | global notification indicator and Overview metrics | no previous frontend result; one result is shared | no frontend-visible mutation beyond indicator text | one request per render; consumers await the same Promise |
| `/api/sync/status` | global sync indicator | no | header text/class only | yes, independent of dashboard |
| `/api/inbox/overview` | Overview body | dashboard result is required before final body assembly; no field from response is used to form the request | backend may schedule an eligible local draft worker in the background | yes, with Notifications after dashboard bootstrap |
| `/api/notifications/overview` | Overview body | dashboard result is required before final body assembly; no field from response is used to form the request | preserves existing notification projection and `shown` feedback lifecycle | yes, with Inbox after dashboard bootstrap |

The final DOM is still produced by one `main.innerHTML` assignment. No
progressive rendering or new global cache was introduced.

## 5. Duplicate Dashboard Fetch

The two old calls had the same URL (`GET /api/dashboard`), no query
parameters, the same JSON contract, and the same cache key. In the baseline,
the second call was commonly a 15-second view-cache hit, but it still created a
network round trip and handler dispatch.

The dashboard request is now created once in `render()`. The same Promise is
passed to `updateNotificationIndicator()` and `overview()`. A new navigation,
generation-triggered render, sync-completion render, or user-action render
creates a new Promise; no dashboard value is retained between render cycles.

## 6. Inbox / Notifications Dependency Analysis

`/api/inbox/overview` uses the PERF-1.4 bounded projection. Its request-local
read groups are conversations, applications, application events, drafts, and
clarifications. It performs the same classification/order logic and can
schedule eligible draft preparation asynchronously. The draft worker's
expensive preparation and AI path are outside `DashboardServer.mu` after
PERF-1.3; the remaining snapshot/commit behavior is unchanged.

`/api/notifications/overview` uses the PERF-1.5 notification-specific
projection. Its read groups are applications, application events,
conversations, and clarifications. It does not read drafts, schedule drafts,
or consume the Inbox projection. Its calculation, global ordering, bounded
top-eight response, global unread count, and batched `shown` feedback are
unchanged.

There is therefore no same-render causal dependency from Inbox scheduling to
the logical Notifications result. The endpoints may still queue behind the
backend's shared `DashboardServer.mu`; that is server serialization, not a
frontend dependency.

## 7. Chosen Frontend Design

The implementation uses a render-scoped Promise:

```text
render()
  ├─ dashboardPromise = GET /api/dashboard
  ├─ updateNotificationIndicator(dashboardPromise)
  ├─ GET /api/sync/status
  └─ Overview:
       ├─ await dashboardPromise
       └─ Promise.all(
            GET /api/inbox/overview,
            GET /api/notifications/overview
          )
```

The indicator helper retains its existing best-effort error handling. If the
dashboard Promise fails, `overview()` still fails before launching its two
dependent reads, preserving the old dashboard-failure ordering and the outer
render error behavior.

## 8. Render-Cycle Data Reuse

The dashboard object is not copied into a module-level cache. The indicator
and Overview receive the resolved value produced by the one Promise. The
Overview still updates the indicator from the same metrics object before
returning its final HTML, so the header and body cannot use different
dashboard responses within the cycle.

## 9. Request Parallelization

The two Overview dependencies are launched in the same `Promise.all` after the
dashboard Promise and sync-status bootstrap complete. This preserves the
existing requirement that Overview body rendering waits for dashboard data and
keeps the sync indicator behavior unchanged, while removing only the
artificial Inbox → Notifications await chain.

## 10. Race / Navigation Safety

`render()` retains the existing `loading` gate and monotonically increasing
`currentRequest` token. The final `main.innerHTML` assignment is guarded by
`request === currentRequest`. The generation poll and sync-completion poll
continue to avoid starting a render while `loading` is true; browser link
navigation unloads the old document. No new shared async state or cancellation
framework was needed.

If an old Promise resolves after a render token is superseded, its result is
only local to the old render and cannot pass the existing final-DOM guard.

## 11. Error Semantics

The old indicator dashboard failure was swallowed locally, while the
Overview dashboard failure rejected before Inbox and Notifications were
requested. The new shared Promise keeps both properties: the indicator catches
its own failure, and `overview()` awaits the same rejected Promise before
starting the parallel dependency group.

Inbox or Notifications failure still rejects the `Promise.all`, so the outer
render shows the existing error UI and does not silently render partial data.
No `allSettled` behavior was introduced.

## 12. Implementation

Changed only:

- `internal/runtime/web/app.js` — render-scoped dashboard reuse and parallel
  Overview dependencies.
- `web/app.js` — the same sequencing change in the root copy.
- this report.

No backend endpoint, cache TTL/key, mutex, read model, draft worker, polling
cadence, HH path, Candidate Knowledge path, or write safety gate changed.

## 13. Request Count Before / After

| Request | Before / render | After / render | Change |
| --- | ---: | ---: | ---: |
| Dashboard | 2 | 1 | −1; duplicate removed |
| Inbox Overview | 1 | 1 | unchanged |
| Notifications Overview | 1 | 1 | unchanged |
| Sync status | 1 | 1 | unchanged |

The after source has one dashboard fetch expression in `render()` and no
dashboard fetch in either Overview consumer. The after ten-cycle client
harness launched Inbox and Notifications at the same millisecond offset for
each cycle.

## 14. Network Waterfall Before / After

The safe ten-cycle replay used the same local server profile and a warm/cache
state after the first cold-ish samples.

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Replay cycles | 10 | 10 | same |
| Full replay min | 142 ms | 114 ms | −28 ms |
| Full replay median | 151 ms | 116.5 ms | −34.5 ms |
| Full replay max | 397 ms | 311 ms | −86 ms |
| Client dependency start | Inbox then Notifications | same start offset | artificial chain removed |

Before, Inbox started only after the Overview dashboard request completed and
Notifications started only after Inbox completed. After, both dependency
requests were launched together. The first after cycle still took 135 ms in
the dependency window because the backend serialized local work; this is why
the measured gain is smaller than the theoretical `max(Inbox, Notifications)`
path on all runs.

## 15. Server Mutex Queueing

The backend still acquires `DashboardServer.mu` around local GET handling.
The dedicated ten-cycle before snapshot recorded notification mutex wait of
75.7 ms total across 10 notification requests and notification mutex hold of
485.3 ms. The after parallel snapshot recorded **38.4 ms wait** and
**460.5 ms hold** across 10 notification requests.

Client overlap therefore does not imply concurrent backend execution. The
after request start offsets prove frontend overlap; the counters prove that
the shared read mutex remains a bottleneck/queue boundary. PERF-1.6 does not
redesign that mutex.

The Inbox draft worker remained bounded and isolated according to PERF-1.3.
Its after snapshot had six short snapshot mutex holds totalling about 223 ms;
no multi-second preparation hold was reintroduced.

## 16. Browser Before / After

Five reloads were measured against the same local Overview route using the
first visible `main .page-heading` as useful content:

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Useful min | 154 ms | 159 ms | +5 ms |
| Useful median | 172 ms | 168 ms | −4 ms |
| Useful max | 520 ms | 411 ms | −109 ms |
| DOM nodes | 364 every run | 364 every run | unchanged |

These are wall-clock browser automation measurements and include local
scheduling/cache variance. They show UI parity and no DOM regression; the
structural request-count and waterfall changes are the primary acceptance
signals.

## 17. Semantic / UI Parity

- Dashboard metrics, sync text, Inbox cards, notification cards, ordering,
  counts, empty states, and navigation targets remain unchanged.
- Inbox remains bounded to the same five Overview cards and Notifications to
  the same eight cards.
- Notification `shown` semantics remain “active notification included in the
  calculated projection”; no duplicate notification request or feedback pass
  was added.
- Inbox draft scheduling remains background, deduplicated, and governed by the
  PERF-1.3 stale-result checks. The frontend does not await draft AI work.
- Browser DOM count remained 364 before and after.

Backend contracts were not edited. The read-only endpoint payload sizes in the
after replay remained approximately 4.4 KB dashboard, 5.4 KB Inbox Overview,
and 3.0 KB Notifications Overview.

## 18. Remaining Bottlenecks

The new ranking from this stage is:

1. Shared `DashboardServer.mu` serialization still limits the benefit of
   client-side concurrency.
2. Cold dashboard read/serialization remains the largest single Overview
   dependency in the measured profile.
3. Inbox and Notifications Overview handler work remain independent but are
   queued when they contend for the shared mutex.

The browser useful median did not materially beat the already-warm PERF-1.5
range, so no browser rendering bottleneck is claimed.

## 19. Verification

Focused/source checks:

```text
node --check web/app.js                 PASS
node --check internal/runtime/web/app.js PASS
source request-count inspection         PASS
10-cycle safe request replay            PASS
5 browser reloads before                PASS
5 browser reloads after                 PASS
```

Full required verification after the final patch:

```text
gofmt -w .                              PASS
git diff --check                        PASS
go test -count=1 ./...                  PASS
go test -race ./...                     PASS
go vet ./...                            PASS
go build ./...                          PASS
go build ./cmd/hh-ai-responder          PASS
node --check web/app.js                 PASS
node --check internal/runtime/web/app.js PASS
```

The Go commands are run as the final handoff verification because the change
is served runtime JavaScript but shares the application asset/build path.

## 20. Safety

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
Live HH writes observed: 0
Candidate truth changed: no
Candidate Knowledge changed: no
Match policy changed: no
HH write safety changed: no
Semantic scope changed: no
PostgreSQL source-of-truth changed: no
Dashboard contract changed: no
Inbox Overview contract changed: no
Notifications Overview contract changed: no
Notification shown semantics changed: no
Draft scheduling semantics changed: no
Docker used: no
```

No HH, AI, embedding, application, test, chat, resume, or job-search-status
write was performed. Existing user changes and the untracked `out` path were
preserved.

## 21. Recommended Next Stage

`PERF-1.7 — Read-only Dashboard Mutex Concurrency`.

The evidence is now a backend queueing bottleneck, not an unresolved frontend
dependency. Do not change the mutex in PERF-1.6; evaluate any read-only
concurrency change as a separate stage with explicit store/thread-safety and
semantic-parity tests.
