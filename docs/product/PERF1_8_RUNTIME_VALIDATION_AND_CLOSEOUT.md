# PERF-1.8 — Runtime Validation and Performance Track Closeout

## 1. Status

`PASS`.

PERF-1.7 was validated in the real local PostgreSQL runtime profile. The
initial Overview readers overlap at runtime, no read-path regression was
observed, the browser useful-content target is met, and no measured remaining
finding has sufficient ROI for another performance stage.

## 2. Purpose

This stage measured the end-to-end effect of PERF-1.7. It did not add
production optimization code, change the frontend, change the PostgreSQL
schema, or change the shared-read classification.

## 3. Environment

Runtime command used for every server replay:

```bash
HH_DRY_RUN=true HH_WRITE_ENABLED=false HH_BACKGROUND_INBOX_REFRESH=false \
go run ./cmd/hh-ai-responder web --host 127.0.0.1 --port 18080
```

The process ran locally on macOS against the existing PostgreSQL-backed
dataset. The application used the existing `.env` AI configuration; no
loopback-AI substitution was used. Docker was not used. The process was
stopped after measurement.

The cache policy was unchanged: the existing 15-second view cache remained
enabled. Two profiles were measured explicitly:

- cold/read-model profile: unique query strings (`?perf18_run=N`) bypassed
  the view cache, as in the PERF-1.2 methodology;
- warm/frontend-equivalent profile: the actual URLs were replayed without a
  query-string bypass, preserving normal cache behavior.

## 4. Dataset

Cardinalities were read from PostgreSQL before the primary probe. No data was
modified to improve the result.

| Collection | Rows/items |
| --- | ---: |
| vacancies | 279 |
| applications | 98 |
| application_events | 308 |
| conversations | 259 |
| conversation_messages | 794 |
| semantic documents | 3 |
| active Notifications Overview items | 8 |
| Inbox Overview items | 5 |

## 5. Methodology

The primary concurrent probe ran 30 rounds. Each round launched these three
GET requests together:

```text
/api/dashboard?perf18_run=N
/api/inbox/overview?perf18_run=N
/api/notifications/overview?perf18_run=N
```

For each response the probe captured status, payload bytes, TTFB, and total
client time. The server `/api/performance` snapshot was captured before and
after the probe. Existing server counters were used for read-model and
handler timing.

The frontend-equivalent waterfall replay ran 30 rounds with this dependency
graph:

```text
dashboard + sync/status
        ↓
inbox/overview + notifications/overview
```

The browser replay used the in-app browser, 20 reloads of `/`, and waited for
the first visible `main .page-heading`. DOM size was measured with
`document.querySelectorAll("*").length`. The browser harness exposed no
Performance API (`performance` was `undefined`), so DCL/load values are not
claimed.

## 6. Shared-read Runtime Validation

The current Overview endpoints returned 200 for all 30 concurrent rounds.
The read handlers executed concurrently in the live process. A single
timeline sample showed:

| Reader | Start offset | End offset | Client total |
| --- | ---: | ---: | ---: |
| dashboard | +37 ms | +254 ms | 156.6 ms |
| inbox/overview | +37 ms | +166 ms | 70.8 ms |
| notifications/overview | +37 ms | +157 ms | 60.4 ms |

The server counter delta for the 30-round cold concurrent probe recorded 30
calls for each reader. The measured read/handler intervals totalled more than
the sum of the 30 per-round batch windows, while each batch completed near the
maximum reader duration rather than the sum of all three durations. This is
runtime evidence of partial overlap, not only three client processes being
started together.

No HTTP 500, malformed JSON, panic, or deadlock occurred. No writer or sync
operation was intentionally started during the probe. Background Inbox draft
work remained detached from the shared read critical path; its observed
exclusive commit/snapshot activity was bounded and was treated as writer
activity, not as reader contention.

## 7. Concurrent Probe

Cold/read-model profile, 30 rounds, unique query-string cache bypass:

| Endpoint | Runs | p50 | p90 | p95 | Max | Payload |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `/api/dashboard` | 30 | 134.129 ms | 148.028 ms | 156.560 ms | 156.954 ms | 4,431–4,432 B |
| `/api/inbox/overview` | 30 | 60.143 ms | 77.380 ms | 81.156 ms | 84.341 ms | 5,365 B |
| `/api/notifications/overview` | 30 | 51.992 ms | 68.747 ms | 72.999 ms | 78.443 ms | 3,038 B |

All 90 responses were HTTP 200.

## 8. Mutex Wait / Hold

The existing runtime counters record exclusive `DashboardServer.mu` wait and
hold, but PERF-1.7 does not expose a separate timed `RLock` wait counter.
Therefore a numeric shared-reader wait is not invented in this report.

During the 30-round cold concurrent probe, the existing exclusive counters
changed by:

| Counter | Calls | Aggregate |
| --- | ---: | ---: |
| `dashboard.mutex_wait` | 7 | 443.843 ms |
| `dashboard.mutex_hold` | 7 | 234.314 ms |

These events came from existing exclusive writer/snapshot paths, including
detached Inbox draft lifecycle work. They did not serialize the three shared
read projections: the reader timeline and overlapping handler counters show
that the readers proceeded concurrently. The focused PERF-1.7 mutex test also
proved that a blocked notification projection does not block `/api/dashboard`.

No multi-second ordinary read lock wait or read-after-read serialization was
observed. Actual writer exclusion remains structural: writers and reloads use
the exclusive lock, while the three narrow projections use `RLock`.

## 9. Overview Waterfall

### Cold/read-model replay

Thirty frontend-equivalent replays with unique query-string cache bypass:

| Metric | Value |
| --- | ---: |
| min | 304 ms |
| p50 | 310 ms |
| p90 | 329 ms |
| p95 | 379 ms |
| max | 410 ms |

This profile intentionally measures uncached read-model work and is the new
cold canonical baseline. The shell replay launches local curl processes and
uses process waits, so browser useful-content timing is evaluated separately.

### Warm/frontend-equivalent replay

Thirty replays using the actual frontend URLs and normal cache policy:

| Metric | Value |
| --- | ---: |
| min | 107 ms |
| p50 | 109 ms |
| p90 | 164 ms |
| p95 | 271 ms |
| max | 273 ms |

The warm median is below the PERF-1.6 reference median of 116.5 ms under the
same local PostgreSQL/read-only profile. The comparison is marked comparable
for the warm profile; the cold profile is not directly comparable to the
PERF-1.6 warm/cache replay.

## 10. Browser Measurements

Twenty Overview reloads were measured by wall-clock time from reload start to
the first visible useful heading. Results:

| Metric | Value |
| --- | ---: |
| min | 152 ms |
| p50 | 164 ms |
| p90 | 286 ms |
| p95 | 357 ms |
| max | 376 ms |
| DOM nodes | 364 in every run |

The browser Performance API was unavailable in the harness, so DCL and load
are intentionally omitted. The accessibility snapshot confirmed dashboard
metrics, Inbox cards, notification cards, notification indicator,
synchronization indicator, navigation links, and ordering. No visual or
product-semantic change was observed.

## 11. Endpoint Ranking

Cold endpoint ranking by p50 and p95:

1. `/api/dashboard`: 134.129 / 156.560 ms, 4,431–4,432 B.
2. `/api/inbox/overview`: 60.143 / 81.156 ms, 5,365 B.
3. `/api/notifications/overview`: 51.992 / 72.999 ms, 3,038 B.
4. `/api/sync/status`: 0.628 / 0.957 ms, 5,357 B in the waterfall replay.

The real slowest component is the uncached dashboard read model. It remains
well below the 250 ms repeatable-backend-blocker threshold and does not create
a multi-second user-visible path in the current architecture.

## 12. Query / Read Invariants

The prior structural invariants remained intact:

- Dashboard uses the bounded snapshot/read-model path, approximately seven
  structural read groups rather than the historical approximately 1,210
  repository calls.
- The runtime cold probe recorded 30 bulk dashboard vacancy reads for 279
  items each, 30 application reads for 98 items each, 30 event reads for 308
  items each, and 30 conversation reads for 259 items each.
- Inbox Overview stayed bounded at five items and retained its bounded
  conversations/applications/events/clarifications/drafts projection.
- Notifications Overview stayed bounded at eight items, retained the bulk
  conversation-message snapshot, and remained approximately 3 KB rather
  than the full approximately 55 KB Notifications response.
- Full `/api/inbox` and `/api/notifications` remained conservative full
  routes; their smoke responses were valid JSON and unchanged in scope.

No unexpected O(N) read-group or payload regression was observed.

## 13. Correctness Regression Checks

The focused regression suite passed for:

- 100 new QualityLog `shown` events producing one durable save;
- duplicate 100-event batch producing zero saves;
- notification projection parity and bounded ordering;
- Inbox Overview bounded payload and full-route top-card parity;
- draft preparation outside `DashboardServer.mu`;
- stale draft result discard;
- duplicate draft-worker deduplication;
- shared reader overlap while notification projection is blocked.

Full route sanity also passed with HTTP 200 and valid JSON for:

```text
/api/inbox
/api/notifications
/api/applications
/api/vacancies
/api/conversations
```

## 14. Race / Stress Verification

The 30-round concurrent HTTP probe had no HTTP 500, malformed JSON, panic, or
deadlock. The required race suite passed:

```text
go test -race ./...  PASS
```

The targeted PERF-1.7 reader test and the Inbox draft concurrency tests also
passed independently.

## 15. Performance Journey

| Stage | Main bottleneck | Before | After |
| --- | --- | ---: | ---: |
| PERF-1 | baseline | ~14 s useful Overview | audit |
| PERF-1.1 | notification writes | ~10 s notification path | ~0.18 s isolated |
| PERF-1.2 | dashboard repeated reads | ~2.6 s | ~0.128 s |
| PERF-1.3 | draft worker lock | ~10 s queue | removed |
| PERF-1.4 | Inbox Overview | ~954 KB / ~284 ms | ~5 KB / ~64 ms |
| PERF-1.5 | Notifications Overview | ~55 KB / ~165 ms | ~3 KB / ~47 ms |
| PERF-1.6 | frontend sequencing | ~151 ms replay | ~116.5 ms |
| PERF-1.7 | shared read serialization | structural bottleneck | shared reads enabled |
| PERF-1.8 | final runtime validation | no comparable post-lock runtime baseline | 109 ms warm waterfall p50; 164 ms browser useful p50 |

PERF-1.8 values are measured in this report. Historical values are retained
from the prior reports; where cache/warmness differs, the comparison is
explicitly marked as incomparable rather than presented as a single trend.

## 16. Remaining Findings

Only measured findings remain:

1. The uncached dashboard read model is the slowest endpoint at 134 ms p50
   and 157 ms p95, with a 157 ms maximum in the 30-run concurrent probe.
2. The browser had a small number of local scheduling outliers, with 357 ms
   p95 and 376 ms maximum, but no multi-second ordinary read outlier.
3. A direct numeric shared-`RLock` wait is not available from current runtime
   counters. This is an observability limitation, not evidence of contention;
   overlap was demonstrated by runtime timeline/handler behavior and the
   race/focused concurrency tests passed.

## 17. ROI Analysis

| Finding | Cost | User impact | Fix complexity | Regression risk | Worth fixing now? |
| --- | ---: | --- | --- | --- | --- |
| Uncached dashboard p50 134 ms | medium | low; browser p50 remains 164 ms | high | medium/high | No |
| Browser p95 357 ms local outliers | low | low; below 500 ms target and not repeatable as a backend bottleneck | unclear | medium | No |
| Missing direct RLock wait counter | low | none demonstrated | low/medium instrumentation-only | low | No; add only with a future measurement need |
| Existing detached draft writer activity | existing | no shared-read regression; exclusive work is bounded | high to redesign | high | No |

No finding meets the bar for a narrow PERF-1.9 fix: repeatable user-visible
latency above 500 ms, repeatable backend work above 250 ms, a multi-second
ordinary-read outlier, large lock wait, race/deadlock, unexpected O(N)
regression, or an unnecessary external call.

## 18. PERF Track Decision

The result is GREEN:

- browser useful p50 is 164 ms, below 200 ms;
- browser useful p95 is 357 ms, below 500 ms;
- warm Overview waterfall p50 is 109 ms, below 150 ms;
- cold reader p95 values remain tens to low hundreds of milliseconds;
- readers overlap at runtime;
- no read-path race, deadlock, 500, payload regression, or semantic drift was
  observed.

## 19. Verification

All required checks passed on the existing worktree:

```text
gofmt -l .                              PASS
git diff --check                        PASS
go test -count=1 ./...                  PASS
go test -race ./...                     PASS
go vet ./...                            PASS
go build ./...                          PASS
go build ./cmd/hh-ai-responder          PASS
node --check web/app.js                 PASS
node --check internal/runtime/web/app.js PASS
```

The focused PERF regression tests and 30-round concurrent HTTP stress probe
also passed.

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

Dashboard semantics changed: no
Inbox semantics changed: no
Notification semantics changed: no
Draft semantics changed: no

Production optimization code added in PERF-1.8: no

Docker used: no
```

The existing dirty worktree was preserved. PERF-1.8 added this report only;
no existing source or user change was reset, cleaned, stashed, reverted, or
formatted as part of the stage.

## 21. Recommended Next Stage

Do not start another performance stage automatically. Return to the product
roadmap and address the missing ranked daily vacancy queue.

Recommended next stage: `P1 — Vacancy Discovery & Ranking`.

PERF TRACK: CLOSED

Reason:
Current user-visible latency is sufficiently low and remaining work has poor ROI.

Recommended next stage:
P1 — Vacancy Discovery & Ranking
