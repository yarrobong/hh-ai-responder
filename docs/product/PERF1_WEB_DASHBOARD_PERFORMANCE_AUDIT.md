# PERF-1 — Web / Dashboard Performance Audit

## 1. Executive Summary

Status: `PASS` for the read-only audit. No production optimization was implemented.

The current initial Overview load is dominated by the backend/API path, not by HTML, CSS, JavaScript download, or browser DOM construction:

- Browser first useful Overview content: **14.17–14.99 s** across 3 measured reloads.
- `DOMContentLoaded`: **42–53 ms**; browser `load`: **46–58 ms**.
- The reproducible API waterfall completes at **13.90 s** on a clean-ish local process.
- The critical-path response is `/api/notifications`: **10.15 s** in the clean waterfall and **9.59–11.27 s** across observed samples.
- `/api/dashboard` is **3.10 s** on the clean bootstrap request; a second Overview request is a cache hit at **47 ms**.
- `/api/inbox` is **954,045 bytes** and takes **465 ms** in the clean waterfall.
- PostgreSQL itself is not showing a single expensive plan: selected `EXPLAIN (ANALYZE, BUFFERS)` executions are **0.2–0.9 ms** with current cardinality. The measured endpoint cost comes from a large number of repeated repository calls, full conversation/message materialization, application-row enrichment, local computation, and local JSON quality-log work.

The highest-value measured first fix is `PERF-1.1 — eliminate the notification initial-render feedback write amplification`. `/api/notifications` records `shown` feedback for every active notification during a GET; the first cold-ish call rewrites the quality log repeatedly and was measured at about 10 seconds. This recommendation is intentionally not implemented in PERF-1.

## 2. Scope and Safety Constraints

Scope was the initial dashboard Overview load and related read paths:

```text
Browser → HTML/static assets → frontend bootstrap → initial API calls
→ dashboard handlers → read models → repositories → PostgreSQL/files
→ JSON serialization → frontend render
```

The audit also inspected Applications, Vacancies, Inbox, Today, Conversations, fast Health, deep Health, and pilot endpoints for comparison and path separation.

Constraints applied:

- `HH_DRY_RUN=true`
- `HH_WRITE_ENABLED=false`
- no HH write endpoint was called;
- no application/test/chat/resume/status write was attempted;
- no SQL, index, cache, API contract, handler, frontend, or concurrency change was made;
- Candidate Knowledge was not modified;
- Docker was not used;
- PostgreSQL remained the canonical storage path;
- existing untracked `out` was preserved.

## 3. Environment

Measured runtime:

| Item | Value |
| --- | --- |
| OS/runtime | macOS local process, Go application via `go run ./cmd/hh-ai-responder web` |
| Dashboard bind | `127.0.0.1:18080` |
| Storage backend | PostgreSQL, selected by existing `.env` configuration |
| Database | `hh_ai_responder_s3` |
| PostgreSQL | `14.19 (Homebrew)` |
| pgvector | `0.8.6` |
| Semantic documents | 3 |
| HH write mode | dry-run/read-only; writer disabled |
| Background inbox refresh | explicitly disabled for the audit process |
| Docker | not used |

Observed database cardinality:

| Table | Rows |
| --- | ---: |
| `vacancies` | 279 |
| `applications` | 98 |
| `application_events` | 308 |
| `conversations` | 259 |
| `conversation_messages` | 794 |
| `candidate_semantic_documents` | 3 |

## 4. Methodology

Source inspection was performed before runtime measurement. The server was started using the existing command path with safety overrides:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_BACKGROUND_INBOX_REFRESH=false \
  go run ./cmd/hh-ai-responder web --port 18080
```

Browser measurements used the available in-app browser. Three reloads were timed around:

- navigation/reload completion;
- `domcontentloaded`;
- `load`;
- first visible `main .page-heading` (first useful dashboard content);
- final DOM node count.

The browser automation sandbox did not expose `window.performance`/`PerformanceResourceTiming` to the read-only page evaluator. Therefore browser DCL/load/useful-render timings are wall-clock measurements around browser events, while request TTFB/size timings come from `curl -w` and server behavior/source inspection. This is a measurement limitation, not an inference presented as a browser timing fact.

The clean initial API waterfall was reproduced with two concurrent bootstrap requests followed by the frontend's sequential Overview requests:

```bash
# equivalent request shape; all calls were GET/read-only
GET /api/dashboard       # parallel bootstrap indicator
GET /api/sync/status     # parallel bootstrap status
GET /api/dashboard       # Overview function, after bootstrap Promise.all
GET /api/inbox
GET /api/notifications
```

The command captured start offset, duration, status, raw body bytes, TTFB, and total transfer time. Static asset timings used the same `curl -w` method.

PostgreSQL audit commands used read-only queries and selected plans only:

```bash
psql "$DATABASE_URL" -X -P pager=off -c \
  "EXPLAIN (ANALYZE, BUFFERS) <selected read-only SELECT>"
psql "$DATABASE_URL" -X -P pager=off -c \
  "SELECT ... FROM pg_indexes ..."
psql "$DATABASE_URL" -X -P pager=off -c \
  "SELECT ... FROM pg_stat_user_tables ..."
```

`pg_stat_statements` is not installed, so aggregate per-query historical timing attribution was unavailable. No permanent instrumentation or pprof endpoint was added.

## 5. Web Architecture / Initial Load Path

### Frontend source and served copy

`web/index.html` and `web/styles.css` are byte-identical to `internal/runtime/web/index.html` and `internal/runtime/web/styles.css`.

The JavaScript files are **not** byte-identical:

| File | Size | SHA-1 | Runtime role |
| --- | ---: | --- | --- |
| `web/app.js` | 89,556 bytes | `3fd0b3...` | root workspace copy; not directly served by the dashboard handler |
| `internal/runtime/web/app.js` | 86,283 bytes | `0b1ec6...` | embedded and served runtime copy |

The server uses:

```go
//go:embed web/index.html web/app.js web/styles.css
```

and serves `/app.js` from the embedded `internal/runtime/web/app.js` relative to `dashboard_server.go`. The root `web/app.js` contains additional reliability UI code and is therefore an independent/stale-or-unsynchronised copy from the runtime point of view. This audit did not change either copy.

### Frontend bootstrap

`web/index.html` loads `/styles.css` and deferred `/app.js`. The script invokes `render()` immediately at the end of the file.

For every render:

1. `updateNotificationIndicator()` and `updateGlobalSyncStatus()` start in `Promise.all`.
2. After both resolve, route rendering starts.
3. Overview calls `/api/dashboard`, then `/api/inbox`, then `/api/notifications` sequentially.
4. Only after those calls complete does `main.innerHTML = html` build the first useful Overview DOM.

Relevant source locations:

- initial render and bootstrap Promise: `web/app.js`, `render()`;
- Overview calls: `web/app.js`, `overview()`;
- route dispatch/static serving: `internal/runtime/dashboard_server.go`;
- `/api/dashboard`: `internal/runtime/dashboard_server.go` → `analytics()`;
- `/api/inbox`: `internal/runtime/dashboard_server.go` → `inbox()`;
- `/api/notifications`: `internal/runtime/dashboard_server.go` → `refreshNotifications()` → `activeNotifications()`.

### Backend path

The runtime path is:

```text
GET /api/dashboard
  → DashboardServer.ServeHTTP
  → serveCachedAPI
  → readAPI("dashboard")
  → analytics("all")
  → VacancyStore.List / ConversationStore.ListConversations
    / ApplicationStore.ListApplications and per-row helpers
  → PostgreSQL repositories and local operational stores
  → json.Marshal

GET /api/inbox
  → readAPI("inbox")
  → DashboardServer.inbox
  → HHReadSyncService.GetCandidateInbox
  → ConversationStore.ListConversations
  → careerSnapshotLocal and workflow projection
  → json.Marshal

GET /api/notifications
  → readAPI("notifications")
  → refreshNotifications
  → careerSnapshotLocal
  → CandidateNotificationEngine.Calculate
  → activeNotifications
  → per-active-notification QualityLog.Record("shown")
  → json.Marshal
```

Every ordinary API GET passes through `DashboardServer.mu` and `refreshLocalFiles()`. The lock serializes local store work even when the frontend starts independent requests in parallel.

## 6. Browser Baseline

Three sequential in-app browser reloads produced:

| Run | DCL | Load | First useful Overview content | DOM elements |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 53 ms | 58 ms | 14,370 ms | 364 |
| 2 | 42 ms | 46 ms | 14,167 ms | 364 |
| 3 | 50 ms | 53 ms | 14,990 ms | 364 |
| **min / median / max** | **42 / 50 / 53 ms** | **46 / 53 / 58 ms** | **14,167 / 14,370 / 14,990 ms** | **364 / 364 / 364** |

Interpretation:

- HTML parsing and static document load are fast.
- The first useful content is delayed until the API chain finishes.
- The initial Overview does not render incrementally; it waits for its data and then performs one full `main.innerHTML` replacement.
- The measured 364-node DOM is not evidence of a browser rendering bottleneck. The browser remains idle-looking while the API chain is pending; source and timing evidence point to backend/API completion.

## 7. Static Assets

Raw response measurements from the runtime server:

| Asset | Bytes | TTFB | Total |
| --- | ---: | ---: | ---: |
| `/` HTML | 2,710 | 0.6 ms | 0.7 ms |
| `/app.js` served runtime copy | 86,283 | 0.5 ms | 0.6 ms |
| `/styles.css` | 18,968 | 0.6 ms | 0.7 ms |
| **static total** | **107,961** | — | — |

Findings:

- JS/CSS are not a substantial share of the measured 13.90 s critical path by latency.
- The runtime JS is 86 KB raw, but there is no evidence from the 42–58 ms DCL/load timings that parsing/evaluation is the first-order problem.
- CSS and JS are normal blocking dependencies in the HTML in the usual sense, but they complete in under 1 ms locally and do not block the measured useful render in a material way.
- No duplicate static asset request was observed in the initial source path. The two JS files in the repository are source/runtime duplication, but only `internal/runtime/web/app.js` affects runtime delivery.
- No compression was observed/required on localhost; the reported transfer sizes are raw body sizes.

## 8. Initial API Waterfall

Clean waterfall run, with phases named to avoid overwriting the first bootstrap request:

| # | Endpoint | Start offset | Duration | Status | Raw bytes | Sequence | Caller / role |
| ---: | --- | ---: | ---: | ---: | ---: | --- | --- |
| 1 | `/api/dashboard` | +37 ms | 3,095 ms | 200 | 4,431 | parallel bootstrap | `updateNotificationIndicator()` |
| 2 | `/api/sync/status` | +38 ms | 54 ms | 200 | 688 | parallel bootstrap | `updateGlobalSyncStatus()` |
| 3 | `/api/dashboard` | +3,169 ms | 47 ms | 200 | 4,431 | after Promise.all | `overview()`; 15 s view-cache hit |
| 4 | `/api/inbox` | +3,253 ms | 465 ms | 200 | 954,045 | sequential | `overview()` |
| 5 | `/api/notifications` | +3,754 ms | 10,147 ms | 200 | 52,855 | sequential | `overview()` |

The last response completes at approximately **13,901 ms** after the waterfall start. This aligns with the browser useful-render range of 14.17–14.99 s after browser/navigation overhead.

Initial request payload total for this clean waterfall, including HTML and static assets:

```text
HTML 2,710
+ app.js 86,283
+ styles.css 18,968
+ dashboard x2 8,862
+ sync/status 688
+ inbox 954,045
+ notifications 52,855
= 1,124,411 bytes raw
```

Parallelism and dependencies:

- The first dashboard and sync-status requests are launched in parallel by JavaScript.
- Backend `DashboardServer.mu` serializes local API work, so frontend parallel launch does not imply parallel repository execution.
- The second dashboard request is independent at the frontend level but is served from the 15-second view cache in this run.
- Inbox and notifications are explicitly sequential in `overview()`.
- There is no duplicate `/api/inbox` or `/api/notifications` in the initial `overview()` function.
- `/api/dashboard` is intentionally called twice during initial render: once for the indicator and once for the Overview body.

## 9. Endpoint Timing Breakdown

Observed external endpoint samples are mixed cold/cache states; the clean waterfall above is the primary baseline. Additional observations included dashboard samples of roughly 2.85–4.34 s, inbox samples of 0.44–0.57 s, and notifications samples of 9.59–11.27 s. Cached dashboard hits were about 45–50 ms.

The existing `/api/performance` snapshot after the runs showed:

| Existing runtime metric | Observation |
| --- | ---: |
| `app.startup` | 62.8 ms in one clean process |
| `dashboard.http` | cumulative wall time; reflects all requests, not a component split |
| `display.page_hit` | microsecond-scale cache-hit handler work |
| `lock.critical_section` | only local file-lock sections, not DashboardServer mutex hold time |
| `lock.process_wait` | low-millisecond cumulative file-lock wait in the captured process |

Exact DB-vs-handler-vs-serialization timing is **not directly measurable without instrumentation**. The repository has no per-query timing hook in this path, and `pg_stat_statements` is unavailable. Best evidence is therefore the endpoint wall clock, code-derived query count, selected PostgreSQL plans, payload size, and existing runtime metrics.

## 10. PostgreSQL Query Audit

### `/api/dashboard`

`analytics()` performs broad projections and then enriches each application row. With the measured current data shape:

| Operation | Query count evidence |
| --- | ---: |
| `VacancyStore.List` at analytics start | 1 |
| `ConversationStore.ListConversations` at analytics start | 1 parent + 259 message queries = 260 |
| `careerSnapshotLocal`: vacancies | 1 |
| `careerSnapshotLocal`: applications | 1 |
| `careerSnapshotLocal`: application events | 1 |
| `careerSnapshotLocal`: conversations | 1 parent + 259 message queries = 260 |
| 98 application rows: vacancy lookup because all 98 `match_result` values are NULL | 98 |
| 98 application rows: timeline (`ensureApplication` + event list) | 196 |
| 98 application rows: linked conversation (`conversation` + messages) | 196 |
| 98 application rows: second timeline read in `analytics()` | 196 |
| **Code-derived total** | **1,210** |

This count is a source/data-shape attribution, not a guessed database statistic. It follows the repository calls in `analytics()` and the measured counts: 98 applications, all 98 without application `match_result`, all 98 linked to conversations, and 259 conversations with messages.

### `/api/applications`

`applicationList()` loads all 98 applications, then for every row calls:

- one vacancy lookup because `match_result` is absent;
- one application timeline, which itself performs `ensureApplication` plus the event query;
- one linked conversation, which performs one conversation query plus one message query.

Code-derived count: **1 + 98 × (1 + 2 + 2) = 491 SQL calls**.

### `/api/inbox`

`GetCandidateInbox()` loads conversations once, then `DashboardServer.inbox()` loads conversations again and builds `careerSnapshotLocal()`, which loads vacancies, applications, events, and conversations again.

Code-derived count: **260 + 260 + 263 = 783 SQL calls**, before any future changes to the data shape.

### Conversation message loading

`ConversationRepository.listConversations()` explicitly loops over every parent conversation and calls:

```text
SELECT ... FROM conversation_messages
WHERE conversation_id=$1
ORDER BY timestamp,sequence
```

This is a confirmed N+1 pattern: one parent query plus 259 child queries for the current 259 conversations. The current database has only 794 messages, so the measured per-query plans are cheap; the pattern has a clear linear scaling risk.

## 11. EXPLAIN ANALYZE Findings

All analyzed statements were read-only SELECTs. No index was created or modified.

| Query shape | Actual rows | Plan | Buffers | Planning | Execution |
| --- | ---: | --- | ---: | ---: | ---: |
| Applications list ordered by `created_at,id` | 98 | Seq Scan + in-memory quicksort | 13 hit | 1.596 ms | 0.218 ms |
| Application events list ordered by `created_at,sequence` | 308 | Seq Scan + quicksort | 16 hit | 0.707 ms | 0.404 ms |
| Conversations list ordered by `created_at,id` | 259 | Seq Scan + quicksort | 25 hit | 0.854 ms | 0.283 ms |
| One conversation message lookup | 2 actual rows | Bitmap index scan on `conversation_messages_conversation_timestamp_idx` + sort | 31 hit in a query including test subplan | 7.931 ms | 0.920 ms |
| One application timeline lookup | 3 actual rows | Bitmap index scan on `application_events_application_created_idx` + sort | 21 hit in a query including test subplan | 1.195 ms | 0.193 ms |
| Vacancies list ordered by `id` | 279 | Seq Scan + quicksort | 36 hit | 5.794 ms | 0.601 ms |

The message/timeline examples used a read-only subquery to select one existing parent, so their planning figures include that selection. The indexed child access is the relevant observation.

Current seq scans are not automatically a problem: the largest listed table is 279 vacancies and the largest query execution above is sub-millisecond. The evidence supports reducing repeated calls/materialization before proposing a new index. Existing relevant indexes include primary keys, application event `(application_id, created_at, id)`, conversation message `(conversation_id, timestamp, id)`, and foreign-key lookup indexes.

`pg_stat_user_tables` showed high cumulative scan counts from the application's repeated access pattern, especially applications and conversations, but no aggregate timing attribution is available because `pg_stat_statements` is not installed.

## 12. N+1 Audit

Confirmed patterns:

| Path | Parent rows | Generated DB operations | Critical path | Scaling characteristic |
| --- | ---: | ---: | --- | --- |
| Conversation list → messages | 259 | 1 parent + 259 child queries | yes: dashboard, inbox, notifications | O(conversations + messages-query calls) |
| Applications list → timeline | 98 | 1 list + 98 × (ensure + events) | yes: applications and dashboard | O(applications) |
| Applications list → linked conversation | 98 | 98 conversation + 98 message loads | yes: applications and dashboard | O(applications × conversation child load) |
| Applications list → vacancy fallback | 98 | 98 vacancy lookups | yes: all 98 current applications lack stored match result | O(applications) |
| Inbox composition | 259 | repeats full conversation list and snapshot loads | yes: overview inbox | O(repeated full projections) |

The cycle in `CandidateNotificationEngine.Calculate()` alone is not labelled N+1; the finding is based on confirmed repository DB calls underneath the lists.

## 13. Filesystem / JSON Reads

PostgreSQL mode deliberately avoids reading career JSON files for vacancies, applications, and conversations. This was verified in `loadHHLocalStoresForBackend()` and the selected repository composition.

Initial startup still reads operational local JSON stores, including:

- candidate clarifications: 8,759 bytes;
- AI drafts: 8,165 bytes;
- notification events: 196,296 bytes at measurement time;
- quality log: 329,811 bytes at measurement time;
- approved actions: 27,378 bytes;
- HH write audit: 30,690 bytes;
- pilot observations: 1,111 bytes;
- sync/daily state files and related small operational state.

`DashboardServer.refreshLocalFiles()` performs `os.Stat` checks on tracked local paths for each API request and reloads only when file metadata changes. It is therefore a small per-request filesystem check, not a full career JSON fallback.

Important observed behavior on the notifications path:

- `activeNotifications()` calls `recordNotificationFeedback(notification, "shown")` for every active notification.
- `QualityLog.Record()` appends and saves the whole quality-log JSON when the observation is new.
- In the cold-ish initial waterfall there were 99 active notifications and `/api/notifications` took 10.147 s.
- This is local file-backed work on a GET, not an HH write, but it is on the initial dashboard critical path.

## 14. External HH / AI Calls

No external HH/AI/embedding call was observed on the initial dashboard path.

```text
No external HH/AI/embedding call observed on initial dashboard load.
```

Evidence:

- dashboard construction creates a lazy HH reader and an AI client, but the initial read handlers do not invoke them;
- `/api/dashboard`, `/api/inbox`, `/api/notifications`, and `/api/sync/status` use local stores/repositories;
- the slow initial calls continued even with no sync operation and no AI request path;
- source inspection found HH reads only under explicit sync/preflight/detail workflows, not Overview bootstrap.

### Deep Health / Pilot separation

`/api/health/deep` and `/api/pilot-shortlist` were probed as separate read-only operations with a 10-second curl deadline. Neither produced a response within that deadline. They are **not part of the initial Overview path**: `render()`/`overview()` never requests them. Therefore they are slow operations requiring a separate reliability/performance stage, but they are not the root cause of the measured initial dashboard critical path.

## 15. Payload Analysis

Measured response sizes and record counts:

| Endpoint | Raw payload | Records/shape | Initial? |
| --- | ---: | --- | --- |
| `/api/dashboard` | 4,431 bytes | metrics + daily/follow-up summary | yes, twice |
| `/api/sync/status` | 688 bytes | state/progress summary | yes |
| `/api/inbox` | **954,045 bytes** | 259 items, 5 sections, 38 follow-ups | yes |
| `/api/notifications` | 52,855 bytes | 99 notifications, 99 unread in captured response | yes |
| `/api/applications` | 83,961 bytes | 98 application rows | no |
| `/api/vacancies` | **1,092,702 bytes** | 279 vacancy rows | no |
| `/api/conversations` | 630,698 bytes | 259 conversations including 794 messages | no |
| `/api/today` | 241,844 bytes | Today projection including inbox-style items | no |
| `/api/knowledge` | 26,943 bytes | candidate knowledge/clarification projection | no |

For the initial Overview, `/api/inbox` is the largest response and is almost 1 MB even though the Overview UI renders only `items.slice(0, 5)`. The full Inbox page maps all returned items. This is a measured payload/usage mismatch, but its first-order latency contribution in the clean waterfall is about 465 ms, much smaller than notifications.

## 16. Frontend Rendering

Measured initial Overview result:

- 364 DOM elements after useful render;
- one final `main.innerHTML` assignment after all initial data calls;
- Overview displays at most five inbox cards (`inbox.items.slice(0, 5)`) and at most eight notification cards (`rows.slice(0, 8)`), despite receiving all 259 inbox items and 99 notifications;
- no evidence of a large client-side sort/filter or message-by-message render on Overview;
- DCL/load remain below 60 ms, while useful render is about 14–15 seconds.

Conclusion: frontend rendering is not the measured first-order bottleneck for the initial Overview. The current render model does amplify payload/DOM work on other routes, especially full Inbox, but that is a separate follow-up finding.

## 17. Polling / Refetch

The runtime frontend contains two periodic loops:

1. Every **45 seconds**, call `/api/generation`; if the generation changed, call `render(true)`.
2. Every **2 seconds**, call `updateGlobalSyncStatus()`, which calls `/api/sync/status` even when no sync is running. If sync watching is enabled and finishes, it may trigger a render.

There is also a route-navigation render and action-triggered render. `loading` and `busy` gates reduce overlapping full renders, but the 2-second status requests can overlap the long initial render at the network level. Because the server serializes local store work with `DashboardServer.mu`, these requests join the same queue.

Source evidence shows no polling of HH, AI, or embedding endpoints. During a 60-second open page, the status loop can issue about 30 lightweight `/api/sync/status` requests and the generation loop can issue one `/api/generation` request. Exact 10/30/60-second browser request counts were not directly available because `PerformanceResourceTiming` is unavailable in the browser evaluator; the interval behavior is directly verified in source.

## 18. CPU / Memory / Serialization

No permanent pprof endpoint or instrumentation was added. Existing runtime metrics expose startup, HTTP, view-cache hits, and local file-lock timings, but not CPU samples, allocations, JSON marshal duration, or PostgreSQL wait separately.

Best available evidence:

- process startup metric: 62.8 ms in one clean run;
- selected SQL execution: 0.193–0.920 ms;
- endpoint wall clock: 3.095 s for dashboard bootstrap and 10.147 s for notifications;
- response serialization is likely non-zero, especially for the 954 KB inbox payload, but is not directly measurable in the current code without instrumentation;
- repeated repository calls and local JSON feedback writes are directly visible in source and are stronger evidence than a speculative CPU hotspot.

The audit therefore classifies the dominant cost as **repeated local read/materialization plus local feedback persistence**, with exact CPU/I/O split unmeasured.

## 19. Startup vs Warm Requests

Measured distinction:

| Phase | Evidence |
| --- | --- |
| Process startup | `app.startup` 62.8 ms in existing `/api/performance` snapshot |
| First dashboard bootstrap request | 3,095 ms in clean waterfall |
| Second dashboard request | 47 ms, served from 15-second view cache |
| First useful browser render | 14.17–14.99 s across reload runs |

The first dashboard request is materially slower than the warm cache hit. The initial user-visible page remains slow because the Overview still waits for Inbox and Notifications after the second dashboard call.

Notifications also show cold/warm local-state effects: captured calls ranged from about 9.59 to 11.27 seconds in mixed runs, while later isolated calls after feedback state was established were sub-second. The clean initial waterfall is the relevant user-facing baseline because it includes the first feedback observations.

## 20. Baseline Summary

| Metric | Current value | Evidence / method |
| --- | ---: | --- |
| HTML TTFB | 0.6 ms | `curl -w` against `/` |
| DOMContentLoaded | 42–53 ms; median 50 ms | 3 browser reload runs |
| Load | 46–58 ms; median 53 ms | 3 browser reload runs |
| First useful dashboard rendering | 14.17–14.99 s; median 14.37 s | first visible `main .page-heading` |
| Initial API count | 5 API calls in the clean critical chain; 4 distinct endpoints | source + clean waterfall |
| Initial transferred bytes | 1,124,411 raw bytes including HTML/assets | clean waterfall + static sizes |
| Slowest initial endpoint | `/api/notifications` | clean waterfall |
| Slowest initial endpoint latency | 10,147 ms | clean waterfall |
| Largest initial payload | `/api/inbox` | 954,045 bytes |
| Largest route payload measured | `/api/vacancies` | 1,092,702 bytes |
| SQL queries during `/api/dashboard` | approximately 1,210 code-derived calls | repository call graph + current row counts |
| Slowest selected SQL | one message plan: 0.920 ms execution | `EXPLAIN (ANALYZE, BUFFERS)` |
| PostgreSQL time | not directly measurable as endpoint total; selected plans are sub-ms | no query instrumentation; no `pg_stat_statements` |
| External calls | 0 observed | source/runtime audit; no HH/AI/embedding in initial path |
| Frontend render time | useful-render wall time 14.17–14.99 s; JS-only time not measured | browser event timing; Performance API limitation |

## 21. Bottleneck Ranking

| Priority | Bottleneck | Evidence | Current cost | Critical-path contribution | Confidence | Candidate future fix | Risk |
| --- | --- | --- | ---: | ---: | --- | --- | --- |
| P0 | Notification GET performs per-active `shown` feedback persistence | 99 active notifications; `activeNotifications()` calls `QualityLog.Record()` for each; clean endpoint 10.147 s | 10.147 s | ~73% of 13.901 s waterfall | High | Separate display observation from initial render, batch one write, or defer non-critical feedback | Must preserve quality semantics and idempotency |
| P1 | Dashboard analytics repeats repository reads and per-row enrichments | approximately 1,210 SQL calls; dashboard bootstrap 3.095 s | 3.095 s cold bootstrap | ~22% before notifications | High | Build one request-scoped snapshot/read model and avoid repeated per-row timeline/conversation loads | Metric correctness and safety/read-model regression |
| P1 | Conversation repository loads messages with confirmed N+1 | 259 parent conversations → 259 message queries per list; repeated in dashboard/inbox | included in dashboard/inbox cost | scaling risk; repeated in critical path | High | Batch child loading or a dedicated summary projection | Message ordering/visibility correctness |
| P1 | Inbox returns full 259-item projection while Overview renders five | 954,045-byte payload; source uses `slice(0, 5)` only after receipt | 465 ms clean path + transfer/parse work | ~3% in clean path; larger scaling risk | High | Overview-specific summary projection or server-side bounded response | Inbox completeness/API compatibility |
| P2 | Initial dashboard is requested twice | same 4,431-byte response; second call is a cache hit at 47 ms | 47 ms warm in clean run | small current cost | High | Reuse bootstrap dashboard result in frontend | Indicator freshness and route lifecycle |
| P2 | 2-second sync-status polling starts regardless of sync state | source `setInterval`; lightweight but serialized by server mutex | about 30 requests/minute | adds queue pressure during long initial loads | High | Gate polling by sync state/visibility or use a less frequent status path | Sync progress freshness |
| P3 | Runtime/root JS copies are unsynchronised | 86,283-byte served copy vs 89,556-byte root copy | no measured initial latency impact | none observed | High | Establish a single source/generation flow in a separate maintenance stage | Risk of serving stale UI behavior |
| P3 | Seq scans on current small tables | plans scan 98–279 rows in 0.2–0.6 ms | negligible current cost | none first-order | High | Re-evaluate only after cardinality/plan changes | Unnecessary index/write overhead |

## 22. Root Causes

### Root cause A — notification display has a write-like local side effect

The slow `/api/notifications` endpoint is not slow because of external HH/AI work. It refreshes the notification projection, then marks every active notification as shown by recording a quality-log event. New records cause full quality-log serialization and atomic file replacement. This is on a GET that is required before Overview render.

### Root cause B — broad snapshot work is repeated instead of request-scoped

`analytics()` loads broad data, calls `careerSnapshotLocal()`, and then enriches 98 applications individually. Each conversation load materializes all messages. The database plans are cheap individually, but the call count and repeated decoding/round trips add up.

### Root cause C — child collections are loaded per parent

The PostgreSQL conversation repository first loads all parent rows and then queries messages once per parent. This is directly confirmed by code and current counts; it is not inferred from the existence of a loop alone.

## 23. Scaling Risks

- Notification feedback work grows with active notifications and quality-log history. Repeated whole-file saves can approach `active_notifications × quality_log_size` write volume on a cold path.
- Dashboard analytics grows with applications, linked conversations, application events, and child message loads. At 5–10× current data, the approximately 1,210 calls can grow substantially unless the request shape changes.
- Conversation list cost grows with conversation count even when each conversation has few messages because the child query count is one per parent.
- Inbox payload and client processing grow with all returned items, while the Overview only uses five. A larger inbox directly increases response bytes and future DOM/client work on the Inbox route.
- Current seq scans are not a scaling finding by themselves; current cardinalities are too small and measured execution is sub-millisecond.

## 24. Candidate Optimizations

Only evidence-supported candidates are listed; none was implemented:

1. Batch/defer notification `shown` quality observations so the first Overview render does not perform one full quality-log save per active notification.
2. Introduce a request-scoped dashboard read snapshot or dedicated read model so vacancies, applications, events, conversations, and messages are loaded once per endpoint.
3. Remove repeated application timeline/conversation/vacancy enrichment from the critical Overview projection, or supply the required fields from one read query/projection.
4. Batch conversation messages for list projections, while retaining full timelines for detail routes.
5. Shape Overview Inbox data to the five cards it actually displays, leaving full Inbox behavior unchanged until its contract is explicitly reviewed.
6. Reuse the first dashboard response for the notification indicator during initial bootstrap.
7. Revisit 2-second sync-status polling behavior after the critical path is measured again.

No index recommendation is ranked above these because the relevant current plans use existing indexes where selective child lookup is needed and execute in sub-millisecond time at current cardinality.

## 25. Value / Risk Matrix

| Fix | Expected impact | Implementation effort | Regression risk | Safety risk | Recommended order |
| --- | --- | --- | --- | --- | ---: |
| Batch/defer notification display feedback | Remove most of the measured 9.6–11.3 s cold notification cost if confirmed with before/after benchmark | Low–medium | Medium: quality-log semantics/idempotency | Low if no HH path is touched | 1 |
| Request-scoped dashboard read model | Reduce repeated dashboard calls and likely several seconds as data grows | Medium–high | Medium–high: metrics/read-model parity | Low | 2 |
| Batch conversation messages | Eliminate 259 child queries per full list and improve scaling | Medium | Medium: ordering and message completeness | Low | 3 |
| Overview-specific bounded inbox projection | Reduce ~954 KB response and client parse/transfer cost | Medium | Medium: endpoint/UI contract split | Low | 4 |
| Reuse first dashboard response | Save one small request; current second call is already a 47 ms cache hit | Low | Low | Low | 5 |
| Add indexes for current list scans | No measured first-order upside | Low–medium | Write/storage overhead | Low | Not recommended now |

## 26. Proposed PERF-1.1 / PERF-1.2 / PERF-1.3

### PERF-1.1 — Remove notification initial-render write amplification

Scope:

- change only the notification display-feedback lifecycle;
- preserve idempotent `shown` observations and quality-log semantics;
- keep HH writes, Candidate Knowledge, and safety gates untouched;
- compare deferred/batched behavior against the clean waterfall.

Expected impact: largest measured opportunity; target should be based on a fresh before/after run, with `/api/notifications` and first useful render as primary metrics.

Likely files: `internal/runtime/dashboard_server.go`, `internal/runtime/quality_log.go`, related focused tests.

Required tests: notification observation idempotency, persistence failure behavior, no duplicate feedback, read-only/HH safety regression tests.

Acceptance criteria to define before implementation: `/api/notifications` median should be below 1 second on the same fixture if the write amplification is removed; first useful Overview render should fall from the current 14.17–14.99 s to a measured target near the remaining dashboard/inbox path. This is a provisional acceptance direction, not a promise.

### PERF-1.2 — Collapse dashboard repeated reads into one measured read model

Scope: dashboard analytics only; preserve metric semantics and structured PostgreSQL truth.

Expected impact: remove the approximately 1,210-call path and reduce cold `/api/dashboard` latency.

Likely files: `internal/runtime/dashboard_views.go`, runtime store/repository boundaries, focused dashboard tests.

Required tests: all-time/today/7d/30d metric parity, application state mapping, event/date semantics, PostgreSQL integration fixture.

Acceptance criteria: measure query count and endpoint median; do not accept a speedup that changes counts/statuses or bypasses canonical PostgreSQL data.

### PERF-1.3 — Batch conversation child loading and shape Overview Inbox payload

Scope: first batch conversation read model, then explicit Overview payload review; do not silently change the full Inbox contract.

Expected impact: eliminate 259 message queries per full conversation list and reduce the 954 KB Overview dependency if the product contract permits a bounded summary.

Likely files: PostgreSQL conversation repository and dashboard/inbox projection code.

Required tests: message ordering, source/direction preservation, incomplete-content flags, Inbox/detail parity, payload-size benchmark.

Acceptance criteria: query count must be measured directly; payload target must retain all fields used by the Overview and must not discard data needed by full Inbox/detail routes.

## 27. Recommended Next Stage

```text
PERF-1.1 — Remove notification initial-render write amplification
```

Why first:

- it is the largest directly measured critical-path cost: 10.147 s of a 13.901 s clean waterfall;
- the causal code path is narrow and local to notifications/quality logging;
- it does not require SQL or API-contract changes;
- it has a clearer rollback and focused test surface than a dashboard read-model rewrite;
- it can plausibly remove most of the current cold notification delay while preserving read-only HH behavior.

The first implementation stage must re-run the same clean waterfall and require:

- `/api/notifications` median materially below the current 9.59–11.27 s observed range;
- first useful Overview render materially below the current 14.17–14.99 s range;
- no new HH request or HH write;
- unchanged notification semantics and quality-log idempotency;
- `go test`, race, vet, build, and focused dashboard tests passing.

## 28. Verification

The following checks are required after the audit artifact is created. Results are recorded here after execution.

| Check | Result |
| --- | --- |
| `gofmt -l .` | PASS; no output |
| `git diff --check` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |

No source formatter was run with `-w`.

## 29. Safety Confirmation

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
Live HH writes observed: 0
Candidate truth changed: no
Match policy changed: no
HH write safety changed: no
Semantic scope changed: no
PostgreSQL source-of-truth changed: no
Production optimization code added: no
Docker used: no
```

The only intended tracked artifact from PERF-1 is this audit document. Existing untracked `out` was pre-existing and was not modified.
