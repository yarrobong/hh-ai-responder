# PERF-1.2 — Dashboard Repeated Read Optimization

## 1. Status

`PASS` for the dashboard read-model optimization and semantic parity.

The component-level dashboard cold path improved materially. End-to-end Overview
latency remains limited by a separate mutex-queued Inbox draft worker; that
concurrency behavior is measured below and is not changed in this stage.

## 2. Context

PERF-1 identified repeated dashboard reads and approximately 1,210
code-derived repository/query calls on the current local dataset. The previous
PERF-1.1 change removed notification quality-log write amplification, but did
not reduce the dashboard read graph.

This stage stayed within `/api/dashboard`, analytics, and a dashboard-specific
conversation read. It did not change global conversation repository semantics,
Inbox payloads, frontend sequencing, HH behavior, candidate data, or the global
mutex architecture.

## 3. Pre-Fix Baseline

The pre-fix dashboard path was replayed from the pre-change analytics source
against the current local PostgreSQL dataset in safe mode. Cold requests used a
unique query string to bypass the existing 15-second view cache.

| Metric | Before |
| --- | ---: |
| `/api/dashboard` cold | 2.675 / 2.601 / 2.596 s (min / median / max) |
| `/api/dashboard` warm/cache | 0.001 s |
| Dashboard payload | 4,431–4,432 B |
| Dashboard query/read calls | approximately 1,210, code-derived |
| `/api/inbox` isolated | 0.376 s (one current pre-fix spot) |
| `/api/notifications` isolated | 13.760 s (one current pre-fix spot; not used as a comparable cold median) |
| Full waterfall | not measured to completion by the pre-fix probe harness |
| Browser useful render | not measured on the current pre-fix source |
| Mutex wait/hold | not measured before instrumentation was added |

The pre-fix call count is structural rather than a historical PostgreSQL
aggregate: `pg_stat_statements` is unavailable in this environment.

## 4. Queue / Mutex Attribution

`DashboardServer.mu` is acquired in `ServeHTTP` around `refreshLocalFiles`, the
read/write dispatch, handler execution, and JSON serialization. The sync
service can also use the same lock for local commits. Minimal fixed-name
instrumentation records mutex wait and hold durations without logging request
content or identifiers.

The full sequence showed:

- isolated notifications after a fresh process: **0.158–0.168 s**, median
  **0.163 s**;
- full sequence notifications: **9.580 s**, **9.622 s**, and **13.768 s**;
- after the first full sequence, dashboard mutex hold records exceeded HTTP
  request records by one, identifying a background `generateInboxDraft` lock
  holder;
- the first full-sequence counter delta was approximately **9.693 s** of
  mutex wait and **10.445 s** of mutex-held work, with one extra hold beyond
  the HTTP request count.

The evidence attributes the discrepancy to a draft worker started by
`/api/inbox`: `scheduleInboxDrafts` launches `generateInboxDraft`, which takes
`DashboardServer.mu` and performs local/AI draft preparation while holding it.
The notification computation itself is fast when no queued worker is ahead of
it. `HH_BACKGROUND_INBOX_REFRESH=false` does not disable this Inbox draft
worker. No concurrency redesign was made in PERF-1.2.

`refreshLocalFiles` was also measured; in the dashboard-only run it accounted
for approximately **0.747 ms across 8 calls**, so it is not the component
bottleneck.

## 5. Existing Dashboard Read Graph

| Output field / group | Source | Before | Required information |
| --- | --- | ---: | --- |
| Vacancy totals | `VacancyStore.List` | repeated | vacancy creation time and match presence |
| Conversation workflow counts | `ConversationStore.ListConversations` plus `CareerSnapshot.ResolveConversation` | repeated parent + child reads | conversation metadata and delivered message history |
| Application cohort metrics | `ApplicationStore.ListApplications` | repeated | application status and timestamps |
| Application dates/status history | per-application timeline reads | O(applications) repeated | application events grouped by application ID |
| Vacancy fallback recommendation | per-application vacancy lookup | O(applications) | one vacancy map keyed by local ID |
| Employer replies/charts | conversation messages | repeated full list reads | message sender/source/timestamp/text semantics |
| Draft and clarification counts | local stores | repeated | current queue records |
| Follow-up analytics | career snapshot | repeated snapshot and timelines | one shared snapshot plus grouped events |

The dashboard needs message text for latest-message policy classification and
the full delivered-message sequence for exact response-time samples. The new
dashboard-specific PostgreSQL read therefore preserves complete message values,
but loads all child messages in one bulk query rather than one query per
conversation. The global conversation API remains unchanged.

## 6. Root Cause

`analytics()` loaded vacancies and conversations, then called
`careerSnapshotLocal()`, which loaded vacancies, applications, application
events, conversations, drafts, and clarifications again. It then loaded
applications a second time, enriched every application with vacancy/timeline/
conversation lookups, and loaded every application timeline again.

The PostgreSQL conversation list additionally loaded each conversation's
messages in a separate child query. This was a dashboard-specific repeated
read multiplier, not one slow PostgreSQL plan.

## 7. Chosen Design

`dashboardSnapshot` loads the six required collections once:

1. vacancies;
2. applications;
3. application events;
4. conversations;
5. drafts;
6. clarifications.

It also captures the current sync state. In-memory indexes group vacancies,
events, and conversations by their stable IDs. Analytics then performs one
deterministic pass over those values.

The PostgreSQL adapter exposes only a narrow `ListForDashboard` method. It
uses one conversation-parent query and one bulk message query. `List()` and
the root `ListConversations()` compatibility behavior are unchanged for all
other callers.

## 8. Query / Repository Call Reduction

| Path | Before | After |
| --- | ---: | ---: |
| Dashboard read groups | approximately 1,210 code-derived calls | 6 request-scoped read groups |
| PostgreSQL conversation child loading | one query per conversation | one bulk query |
| Effective dashboard SQL/read groups | approximately 1,210 | 7 structural groups including the bulk conversation child query |
| Per-application vacancy/timeline/conversation reads | present | zero |

The focused regression test observes exactly one call to each of the six
dashboard read groups. It repeats the check after adding 25 applications; the
read-group count remains constant. This is an operation-count assertion, not a
wall-clock assertion.

## 9. Semantic Compatibility

The existing dashboard analytics date/window rules were retained, including
local-time midnight boundaries, inclusive 7-day and 30-day windows, undated
application handling, event-vs-status classifications, first employer reply
dates, and follow-up policy inputs.

The application fallback behavior is preserved through `vacanciesByID`:
missing application `MatchResult` still falls back to the related vacancy when
that vacancy exists, while missing relations remain missing/manual-review
inputs. No candidate facts or match results are written.

The response parity test compares repeated logical analytics responses at a
fixed `now`; the existing date/cohort fixture covers multiple statuses,
application events, missing dates, and calendar boundaries. It passes without
fuzzy metric comparison.

## 10. Implementation

- Added `dashboardSnapshot` and request-local relation indexes.
- Reused one application-event collection instead of per-application timeline
  reads.
- Reused one vacancy collection instead of per-application fallback lookups.
- Reused one conversation collection for resolved workflow and application
  enrichment.
- Added dashboard-only bulk conversation child loading in the PostgreSQL
  adapter.
- Extracted the deterministic application-row projection so the regular
  Applications endpoint retains its existing lookup behavior.
- Added minimal mutex wait/hold and local-file refresh timing counters.

No schema, migration, index, cache TTL, frontend, HH, AI, or write-path change
was made.

## 11. Focused Tests

```bash
go test -count=1 ./internal/runtime -run 'TestDashboardAnalytics|TestDashboardServerStartsAndEmptyStores|TestDashboardReadAPIsAndFilters'
```

The focused coverage includes analytics date/cohort semantics, empty stores,
filters, stable logical output, bounded read groups, and constant read-group
count as application cardinality grows.

## 12. Dashboard Before / After

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Dashboard query/read calls | ~1,210 | 7 structural groups | approximately −99.4% |
| `/api/dashboard` cold min | 2.596 s | 0.126 s | −95.1% |
| `/api/dashboard` cold median | 2.601 s | 0.128 s | −95.1% |
| `/api/dashboard` cold max | 2.675 s | 0.150 s | −94.4% |
| Dashboard mutex wait | not measured | ~0.010 ms aggregate across 7 requests in dashboard-only probe | measured after |
| Dashboard mutex hold | not measured | ~0.126–0.150 s per cold dashboard request | measured after |
| Dashboard payload | 4,431–4,432 B | 4,431–4,432 B | contract preserved |
| Logical response parity | not measured before instrumentation | PASS | no metric drift observed |

The existing 15-second cache was preserved. The post-fix warm request was
approximately **0.001 s**.

## 13. Mutex Before / After

The before binary had no mutex timing counters, so a numeric before hold-time
value is not claimed. The after dashboard-only probe recorded near-zero queue
wait and dashboard hold time tracking the 0.126–0.150 s cold handler duration.

The full sequence is different: the dashboard handler is fast, but an Inbox
draft worker takes the shared lock afterward. That worker is the measured
source of the notification queue delay and remains outside this stage's scope.

## 14. Full Waterfall Before / After

Post-fix request timings for the required shape were:

| Run | Bootstrap | Sync status | Dashboard | Inbox | Notifications | Approx. total |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.169 s | 0.002 s | 0.001 s | 0.668 s | 9.580 s | 10.419 s |
| 2 | 0.144 s | 0.002 s | 0.001 s | 0.407 s | 9.622 s | 10.174 s |
| 3 | 0.207 s | 0.003 s | 0.001 s | 0.415 s | 13.768 s | 14.391 s |
| **min / median / max** | — | — | — | — | — | **10.174 / 10.419 / 14.391 s** |

The current pre-fix probe did not complete a full comparable waterfall, so no
invented before total is reported. Component-level dashboard speedup is
therefore the reliable acceptance result; the remaining E2E path is limited by
the Inbox draft worker described above.

## 15. Browser Before / After

Using the same local Overview route and a browser useful-content condition, the
post-fix runs were:

| Run | Useful Overview content | DOM nodes |
| ---: | ---: | ---: |
| 1 | 12.190 s | 367 |
| 2 | 12.168 s | 367 |
| 3 | 14.181 s | 367 |
| **min / median / max** | **12.168 / 12.190 / 14.181 s** | **367 / 367 / 367** |

Current pre-fix browser timings were not captured. Browser navigation timing
APIs were unavailable in the in-app evaluator, so DCL and `load` are not
claimed for this stage.

## 16. Remaining Bottlenecks

Post-fix ranking from the measured path:

1. Shared mutex queueing from `generateInboxDraft` after Inbox; it dominates
   full Overview latency and explains the notifications discrepancy.
2. Inbox itself remains approximately 954 KB and 0.407–0.668 s in the three
   full runs.
3. Isolated notifications remain fast at 0.158–0.168 s; their waterfall delay
   is queueing, not notification calculation.

The next stage should be selected after review of this attribution. General
conversation batching and Inbox payload changes were not implemented here.

## 17. Verification

The following checks passed after the implementation:

```text
gofmt -l .
git diff --check
go test -count=1 ./...
go test -race ./...
go vet ./...
go build ./...
go build ./cmd/hh-ai-responder
node --check web/app.js
node --check internal/runtime/web/app.js
```

## 18. Safety

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
Dashboard output semantics changed: no
Docker used: no
```

The existing user/untracked `out` path was preserved. No secrets or private
conversation content were added to logs or the report.

## 19. Recommended Next Stage

Review and isolate the shared-lock Inbox draft worker queueing before selecting
whether the next stage is concurrency-safe draft scheduling, Inbox payload
shaping, or general conversation child batching.
