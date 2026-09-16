# PERF-1.5 — Notifications Read-Path and Payload Optimization

## 1. Status

`PASS`. Measurements and verification date: 2026-09-11.

The full `/api/notifications` contract remains available. Overview now uses a
separate bounded projection backed by a notification-specific read model.

## 2. Context

PERF-1.4 reduced the initial Overview Inbox dependency to five cards and about
5.4 KB. The remaining notification dependency still read the broad local
career snapshot and returned every active notification even though Overview
renders eight cards.

Runtime profile:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_BACKGROUND_INBOX_REFRESH=false
Docker: not used
```

No HH, AI, embedding, or other external write path was used.

## 3. Current Baseline

The current pre-change checkout was measured before this read-path change.
Five isolated full notification requests returned 104 active records and
54,797 bytes in 0.1608, 0.1645, 0.1652, 0.1667, and 0.1820 seconds:

```text
min / median / max = 0.161 / 0.165 / 0.182 s
```

The existing notification handler and mutex counters for those five requests
were 835.895 ms and 835.898 ms total; average hold/handler was approximately
167.2 ms. Mutex wait was 0.009 ms total, approximately 0.0018 ms per request.

The old path had six runtime read groups: vacancies, applications,
application events, conversations, drafts, and clarifications. For the current
PostgreSQL shape, conversations were one parent query plus 259 per-conversation
message queries. Including the other three SQL list queries, this was
approximately 263 SQL operations plus local-file reads. This is structural
source evidence, not a `pg_stat_statements` aggregate.

Warm baseline requests produced zero new durable quality-log saves. The
PERF-1.1 invariant remains covered separately: 100 new shown observations make
one durable save and a duplicate batch makes zero saves.

## 4. Notification Call Graph

```text
GET /api/notifications
  → DashboardServer.ServeHTTP and DashboardServer.mu
  → refreshLocalFiles
  → readAPI("notifications")
  → refreshNotifications
      → loadNotificationSnapshot
      → CandidateNotificationEngine.Calculate
      → delivery-uncertain local reconciliation
  → activeNotifications
      → active filtering, shown feedback, QualityLog.RecordMany
      → global priority/time sort
  → JSON serialization

GET /api/notifications/overview
  → the same complete calculation, sorting, and feedback path
  → first eight logical records
  → Overview DTO projection and JSON serialization
```

The limit is applied only after calculation and global sorting. There is no
unsafe early database limit.

## 5. Notification Dependency Inventory

| Rule/type | Required inputs | Current source | Full collection required? |
| --- | --- | --- | --- |
| New employer message / candidate action | application relation, conversation workflow, latest meaningful message | applications + conversations | Complete conversation value; current rules derive latest state from full history |
| Clarification required | pending clarification and application/conversation relation | clarifications + applications + conversations | All pending clarifications |
| Interview / external action / offer | lifecycle, latest employer message, workflow state | applications + conversations | All conversations; full message values retained |
| Manual review / status changed | application relation, conversation summary/state, prior workflow state | applications + conversations + notification store | All conversations and persisted workflow states |
| Follow-up available | application date/status, follow-up events, conversation state, clarifications | applications + application events + conversations + clarifications | All applications/events for global eligibility |
| Shown/unread | notification lifecycle and quality-log observation identity | notification store + quality log | All active notifications for existing semantics |

Vacancies, AI drafts, and sync state are not consumed by
`CandidateNotificationEngine.Calculate`; they were removed from this
request-specific snapshot. Full conversation histories remain in the input
because current workflow and resolution functions inspect message-derived
state. PostgreSQL now loads them through one bulk child query.

## 6. Overview Frontend Usage

The served runtime asset is `internal/runtime/web/app.js`; `web/app.js` was
updated identically. Overview renders at most eight cards and uses:

```text
id, type, priority, lifecycle, message, created_at,
related_conversation_id, related_attempt_id, related_vacancy_id,
related_trigger_message_id, related_action_type, detail_path
```

The served copy directly uses `id`, `priority`, `lifecycle`, `message`,
`created_at`, and `related_conversation_id`. The root copy also uses the
reliability identifiers and `detail_path`; the DTO retains those fields. The
navigation indicator uses `/api/dashboard` metrics, not this response.
`unread` remains the global active unread count, not the number of returned
cards.

## 7. Root Cause

`careerSnapshotLocal()` loaded vacancies, applications, application events,
all conversations and message histories, drafts, and clarifications. The
general PostgreSQL conversation list performed one message query per
conversation. Vacancies and drafts were not notification dependencies.

The full active list was serialized even though Overview sliced it to eight
cards in JavaScript. Existing behavior marked every active notification shown
before that slice; the new route deliberately preserves this semantics.

## 8. Chosen Read Model/API Design

`loadNotificationSnapshot` reads only applications, application events,
conversations, and clarifications. `ListConversationsForDashboard` provides
one parent query plus one bulk message query. If the specialized projection
fails, the established general conversation read is retained as a fallback.

Added read-only `GET /api/notifications/overview`, which calculates the same
complete logical set, preserves global ordering before limiting, returns eight
DTOs with fields used by both frontend copies, returns the global active unread
count, and preserves existing shown feedback behavior.

`GET /api/notifications` still returns the complete `CandidateNotification`
objects with the same path and sorting.

## 9. Semantic and Feedback Compatibility

IDs, types, priorities, messages, timestamps, metadata, lifecycle, unread
count, ordering, fingerprints, deduplication, workflow-state history,
delivery-uncertain reconciliation, notification creation/resolution, and
quality-log observation identity are unchanged for the full route and are
identical before limiting for Overview.

The Overview path collects shown events for every active notification before
applying the limit. This preserves the existing behavior; redefining shown as
“one of eight visible cards” would be a separate product change.

PERF-1.1 batching remains intact:

```text
100 new shown observations → 1 durable QualityLog save
duplicate batch            → 0 durable saves
```

The after runtime probe recorded zero durable saves across five warm Overview
requests.

## 10. Implementation

- `internal/runtime/notification_read_model.go` — notification input read model.
- `internal/runtime/dashboard_server.go` — route dispatch, timing seams, DTO, and shared projection.
- `internal/runtime/dashboard_performance.go` — filesystem stat/reload counters.
- `internal/runtime/quality_log.go` — durable-save timing counter only.
- `internal/runtime/web/app.js`, `web/app.js` — bounded Overview fetch.
- `internal/runtime/dashboard_notifications_test.go` — parity, top-N, empty/partial/limit, read-group, and feedback tests.
- `internal/runtime/dashboard_test.go` — empty-server route coverage.

No draft-worker lifecycle, global mutex design, Inbox contract, dashboard
contract, HH safety gate, or PostgreSQL source-of-truth behavior was changed.

## 11. Read / Query Reduction

| Path | Before | After |
| --- | ---: | ---: |
| Notification runtime read groups | 6 | 4 |
| Conversation child reads | 259 per-conversation queries | 1 bulk query |
| PostgreSQL list/message operations | approximately 263 | 4 SQL operations plus one clarification file read |
| Unused vacancy/draft reads | 1 + 1 | 0 |
| Per-row repository growth | present for conversation messages | none in specialized path |

After counter delta for five Overview requests:

```text
applications       5 calls / 490 items
application_events 5 calls / 1,540 items
conversations      5 calls / 1,295 items
clarifications     5 calls / 55 items
```

Filesystem instrumentation showed 90 `os.Stat` checks across five requests,
0.395 ms total, with no reloads. Filesystem metadata checks are not a
measured bottleneck.

## 12. Payload Reduction

The full route remained approximately 54,797 B and returned the complete
active list in the final runtime state. Overview returned 3,038 B and eight
records, approximately 94.5% smaller.

The baseline and after active counts differed by one during live safe probes
because refresh state changed while the process was restarted. That count
difference is not used as a semantic parity claim; deterministic parity tests
compare exact logical records.

## 13. Focused Tests

Coverage includes exact legacy-vs-optimized input parity, exact engine parity
including IDs, full endpoint completeness, global priority/time ordering,
empty/below-limit/exact-limit/over-limit sets, global unread count, shown
identity and duplicate behavior, one bounded read group per required
collection, and absence of vacancy/draft notification reads. No timing
assertion is used as correctness.

## 14. Before / After

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Full notification records | 104 | 103 current state | state changed during refresh; contract preserved |
| Overview records | 104 received, 8 rendered client-side | 8 | bounded |
| Full payload | 54,797 B | 54,797 B | unchanged |
| Overview payload | 54,797 B dependency | 3,038 B | −94.5% |
| Read/query groups | 6; ≈263 SQL operations + local reads | 4; 4 SQL + 1 clarification read | bounded; no child N+1 |
| Isolated full endpoint min / median / max | 161 / 165 / 182 ms | 46 / 48 / 57 ms | lower read path; current state differs |
| Isolated Overview min / median / max | full route: 161 / 165 / 182 ms | 46 / 47 / 47 ms | materially lower |
| Mutex wait average | 0.0018 ms | 0.0016 ms | negligible |
| Mutex hold average | 167.2 ms | 50.3 ms Overview handler | −69.9% |
| Durable feedback saves | 0 warm; fixture 100→1 | 0 warm; fixture unchanged | invariant preserved |
| Full waterfall median | 370 ms pre-change replay | 237 ms | −35.9% local replay |
| Browser useful median | not captured on same pre-change runtime | 367 ms | after-only measurement |

After five Overview requests, the handler breakdown was: snapshot 47.7 ms
total, calculation 186.5 ms, shown feedback 2.7 ms, serialization 0.07 ms,
and mutex wait 0.008 ms total. Handler/mutex hold was 251.7 ms total.

## 15. Full Waterfall

After five replays:

```text
dashboard bootstrap: 129–153 ms
dashboard overview/cache read: 0.6–170 ms
inbox/overview: 54–80 ms
notifications/overview: 48–112 ms
critical-path totals: 303, 480, 237, 236, 234 ms
min / median / max = 234 / 237 / 480 ms
```

The 480 ms sample contains a dashboard cache/read outlier. Requests were not
parallelized in this stage.

## 16. Browser Measurement

Five local reloads measured first useful Overview content at 439, 242, 494,
367, and 156 ms:

```text
min / median / max = 156 / 367 / 494 ms
DOM nodes = 364 on every run
```

The browser evaluator did not expose a usable Navigation Timing object for
DCL/load extraction, so those timings are not claimed.

## 17. Remaining Bottlenecks

The new ranking is:

1. `/api/dashboard` bootstrap/read: approximately 129–153 ms cold, about
   133 ms median in the after replay;
2. `/api/inbox/overview`: approximately 56 ms median;
3. `/api/notifications/overview`: approximately 53 ms median in the
   waterfall and 46–47 ms isolated;
4. sync status and serialization: sub-millisecond to low-millisecond work.

The next bottleneck is dashboard bootstrap plus the sequential frontend chain,
not notification calculation or notification payload.

## 18. Verification

Focused tests passed:

```text
go test ./internal/runtime -run 'TestNotification|TestDashboardServerStartsAndEmptyStores' -count=1
```

The complete repository verification passed after this report was created:

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

## 19. Safety

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
Live HH writes observed: 0
Candidate truth changed: no
Candidate Knowledge changed: no
Match policy changed: no
HH write safety changed: no
PostgreSQL source-of-truth changed: no
Notification rule semantics changed: no
Notification IDs/order/full contract changed: no
Shown feedback semantics changed: no
Docker used: no
```

No secrets, cookies, authorization headers, raw AI bodies, or private
candidate data were added to the report.

## 20. Recommended Next Stage

`PERF-1.6 — Frontend Request Sequencing and Dashboard Bootstrap Reuse`
