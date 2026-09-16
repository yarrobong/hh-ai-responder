# PERF-1.1 — Notification Initial-Render Write Amplification Fix

## 1. Status

`PASS`

The acceptance property is satisfied: 100 new `shown` observations produce one durable quality-log save, while a duplicate batch produces zero saves. Notification calculation and the HTTP contract were not changed.

## 2. Problem

`GET /api/notifications` calculated the active projection and called `QualityLog.Record` once per active notification. `Record` delegates to `RecordMany` with a batch of one, so every new `shown` observation serialized and atomically rewrote the complete quality log.

PERF-1's clean baseline was 99 active notifications, 52,855 bytes, and 10.147 s endpoint latency. The quality log was approximately 329,811 bytes.

## 3. Pre-Fix Baseline

The source-level 100-observation fixture measured the old call shape directly: calling `Record` once per observation invoked 100 durable saves.

The current checkout did not reproduce the original cold runtime state before the fix. Three pre-fix runtime GETs were 0.178–0.223 s with 101 then 100 active notifications; the local quality log was already warm and measured 331,853 bytes after the probe. This is recorded as non-comparable rather than presented as a new cold baseline.

## 4. Root Cause

`shown` feedback was recorded inside the active-notification loop. Its identity was `notification/shown/<notification.ID>`. For a new observation, `QualityLog.RecordMany` appended it and immediately called `Save`, performing full JSON serialization and the existing atomic private-file replacement.

## 5. Chosen Design

The notification projection now collects `shown` events in deterministic input order and submits them through one `QualityLog.RecordMany` call after assembling the projection.

`RecordMany` constructs a complete next event slice, applies the existing observation-key deduplication and validation rules, performs one durable save if the slice changed, and publishes the new in-memory slice only after success. `Record` remains a batch-of-one compatibility API.

The existing Today notification display path now uses the same batching because it has the same `shown` semantics.

## 6. Alternatives Considered

- Uncontrolled fire-and-forget persistence was rejected because it could lose observations, change error semantics, race with other writes, and hide rather than remove amplification.
- A cache was rejected because it would hide cold cost without fixing persistence volume.
- Moving `shown` to a new POST lifecycle was rejected as out of scope and a product-semantics change.

## 7. Implementation

- `internal/runtime/quality_log.go`: transactional batch persistence, a minimal persistence seam for tests, and store locking.
- `internal/runtime/dashboard_server.go`: batched active-notification `shown` observations; projection sorting and fields are unchanged.
- `internal/runtime/daily_operations.go`: batched Today `shown` observations.
- `internal/runtime/quality_log_test.go`: batch, idempotency, endpoint, reload, ordering, and failure tests.

The persistence format remains quality-log version 1 with the existing JSON fields and file location. `WritePrivateFileAtomic` and the existing process file lock remain in use.

## 8. Semantic Compatibility

- `shown` still means that an active notification was included in the displayed notification projection.
- Identity remains `ObservationKey`, specifically `notification/shown/<notification.ID>`; duplicate detection still compares that key against existing and earlier batch events.
- A notification has at most one idempotent `shown` observation, as before. Other actions retain action-specific keys.
- Each event retains its own `time.Now().UTC()` timestamp; the batch does not collapse timestamps.
- New events append in input order; no map iteration is used for batch ordering.
- Notification calculation, active count, priority, unread count, IDs, grouping, filtering, candidate truth, and career snapshot semantics were not changed.
- Quality-log consumers and other event paths continue to use the same store and on-disk format.

## 9. Persistence / Atomicity

The prior `RecordMany` could leave appended events in process memory when `Save` failed. The new implementation keeps current state unchanged until the complete next snapshot passes validation and the existing atomic save succeeds. A failure returns an error and cannot publish a partial batch or corrupt the target file.

Empty and all-duplicate batches perform no save. A batch with new events performs exactly one save. Existing temp-file, sync, rename, private-file, and lock behavior is preserved.

## 10. Concurrency Safety

`QualityLogStore` now serializes Load/Record/Save operations with an internal RWMutex. This is compatible with the existing `DashboardServer.mu` serialization and introduces no nested dashboard-lock calls. The race suite passed.

## 11. Tests

Focused command:

```bash
go test -count=1 -v ./internal/runtime -run 'TestQualityLog|TestNotificationsEndpoint'
```

The tests cover 100 all-new events (one save and reload parity), 100 duplicates (zero saves and unchanged file), mixed batches (only new events, one save, deterministic order), batch-of-one, empty batches, injected persistence failure, and endpoint parity across repeated GETs.

## 12. Post-Fix Benchmark

The isolated runtime endpoint was measured five times after the fix with:

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
HH_BACKGROUND_INBOX_REFRESH=false
```

The current warm state had 100 active notifications and 53,318-byte responses. Results were 0.167, 0.192, 0.187, 0.186, and 0.186 s: min/median/max **0.167 / 0.186 / 0.192 s**. The current state had no new `shown` observations.

The deterministic 100-new-observation fixture measured the architectural result directly: **100 individual saves before the call-shape change, 1 batch save after it**. A repeated duplicate GET had **0** saves.

## 13. Initial Overview Re-Measurement

The PERF-1 request shape was repeated three times in the same safe mode:

| Run | Dashboard bootstrap | Sync status | Dashboard overview | Inbox | Notifications | Waterfall total |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 4.063 s | 0.001 s | 0.001 s | 0.588 s | 9.930 s | 14.582 s |
| 2 | 5.661 s | 0.002 s | 0.001 s | 0.595 s | 9.664 s | 15.921 s |
| 3 | 4.958 s | 0.001 s | 0.001 s | 0.527 s | 9.610 s | 15.096 s |
| **min / median / max** | — | — | — | — | **9.610 / 9.664 / 9.930 s** | **14.582 / 15.096 / 15.921 s** |

The full waterfall did not improve in this re-measurement. Isolated notification requests were fast, while the notification request after cold dashboard/inbox work remained slow. This identifies a residual read/background interaction on that sequence; it is not evidence that batch saving repeated and is outside this fix.

Browser reload measurement after the fix:

| Metric | Run 1 | Run 2 | Run 3 | Min / median / max |
| --- | ---: | ---: | ---: | ---: |
| DOMContentLoaded | 197 ms | 87 ms | 102 ms | 87 / 102 / 197 ms |
| Load | 208 ms | 94 ms | 111 ms | 94 / 111 / 208 ms |
| First useful Overview content | 15.890 s | 17.808 s | 15.534 s | 15.534 / 15.890 / 17.808 s |
| DOM elements | 354 | 354 | 354 | 354 / 354 / 354 |

## 14. Before / After

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Active notifications | 99 audit baseline | 100 current runtime; 100 fixture | Dataset/state changed; fixture projection parity passes |
| New `shown` observations | 100 sequential `Record` calls | 100 in one `RecordMany` batch | Saves reduced from 100 to 1 |
| Durable saves | 100 for 100 individual calls | 1 for 100 new batch events; 0 for duplicates | -99 / -100% for duplicate batch |
| `/api/notifications` min | 9.59 s audit range | 0.167 s isolated post-fix run | Not directly comparable: pre-fix was cold/new, post-fix warm |
| `/api/notifications` median | 10.147 s clean audit run | 0.186 s isolated post-fix run | Not directly comparable; structural save metric is acceptance |
| `/api/notifications` max | 11.27 s audit range | 0.192 s isolated post-fix run | Not directly comparable |
| Full initial waterfall | 13.901 s baseline | 15.096 s median re-measurement | Residual cold dashboard/inbox/background interaction |
| Browser useful render median | 14.37 s baseline | 15.890 s | Dataset/runtime path changed; no browser improvement claimed |
| Notification payload | 52,855 B baseline | 53,318 B | Dataset changed |
| Quality-log size | ~329,811 B baseline | 331,853 B current state | Historical local state changed; schema unchanged |

The endpoint-focused test verifies semantic parity directly: the same 100 active notifications and unread count are returned on repeat, all 100 `shown` events are persisted, and no duplicate feedback is added.

## 15. Remaining Bottlenecks

The largest measured cost in the repeated full sequence is the residual `/api/notifications` request after cold dashboard/inbox work (9.610–9.930 s), despite isolated warm notification requests completing in 0.167–0.192 s. This should be profiled as a separate read/background contention issue before changing it.

The next deterministic cold-path item remains `/api/dashboard` at 4.063–5.661 s; the PERF-1 audit attributed approximately 1,210 repository/SQL calls to its current projection. Inbox remains approximately 954 KB and 0.527–0.595 s in this measurement.

## 16. Verification

All required checks passed:

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

## 17. Safety Confirmation

```text
HH_DRY_RUN=true
HH_WRITE_ENABLED=false
Live HH writes observed: 0
Candidate truth changed: no
Match policy changed: no
HH write safety changed: no
Semantic scope changed: no
PostgreSQL source-of-truth changed: no
Notification projection semantics changed: no
Quality feedback lost: no
Docker used: no
```

Runtime probes used the local dashboard only in dry-run/read-only mode. No HH write endpoint, AI call, database schema change, migration, index, Docker command, or frontend change was made. Existing untracked `out` and the PERF-1 audit artifact were preserved.

## 18. Recommended Next Stage

```text
PERF-1.2 — Collapse dashboard repeated reads into one measured read model
```

Do not implement that stage as part of PERF-1.1. First review the residual notification sequence behavior and the new dashboard waterfall.
