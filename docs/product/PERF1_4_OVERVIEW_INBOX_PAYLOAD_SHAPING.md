# PERF-1.4 — Overview Inbox Payload Shaping

## 1. Status

PASS. Measurements and verification date: 2026-09-11.

## 2. Context

The initial Overview requested the full `/api/inbox` response although it rendered only the inbox count and approximately five cards. Full Inbox remains on `/api/inbox`.

Measurements used `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, and `HH_BACKGROUND_INBOX_REFRESH=false`. Docker was not used. PERF-1.3 measurements were not reused as a strict baseline because they used a different AI endpoint/cache/warmness profile.

## 3. Pre-Fix Baseline

Five isolated full-Inbox requests before production changes returned 954,029–954,036 bytes and 259 records, with total latency min 254 ms / median 284 ms / max 377 ms.

The pre-fix browser useful samples were 753, 272, and 265 ms (median 272 ms); DOM timing was 364 ms. A strict pre-fix full waterfall was not captured, so historical PERF-1.3 waterfall values are excluded. The prior populated-fixture code audit counted approximately 783 storage/read operations.

## 4. Overview vs Full Inbox Usage

The served runtime asset `internal/runtime/web/app.js` was audited field by field; the root `web/app.js` copy is synchronized.

| Inbox field | Overview uses | Full Inbox uses | Notes |
| --- | --- | --- | --- |
| `items` | `length` and first five cards | all items | Overview cap is after classification/sorting |
| `sections` | no | headings and grouped items | omitted from Overview |
| `important_count` / counts | no; count comes from dashboard metrics | full Inbox count/state display | not duplicated |
| `follow_ups` | no direct initial-card rendering | follow-up section/indicators | full contract preserved |
| `ai_drafts[]` | generated draft `status` and `text` | full draft/status rendering | card fields retained |
| `workflow` | `state`, `what_to_do` | workflow labels/actions | both retained |
| timestamps | latest-message timestamp and updated fallback | item/card timestamps | retained where read |
| conversation data | id, company, title, updated_at | full object and history | nested unused data omitted |
| message preview | latest text/timestamp | preview plus full history | one latest meaningful message |
| vacancy body/other metadata | no | details, warnings, clarifications, related metadata | not inferred for Overview |

Full Inbox still reads complete items, sections, important count, follow-ups, conversation data, warnings, drafts, and histories. Only the initial Overview fetch changed to `/api/inbox/overview`; dashboard, notifications, polling, and full Inbox rendering did not change.

## 5. Existing Read/Projection Path

Old Overview dependency:

```text
HTTP handler → s.inbox() → full conversation/application reads
             → CandidateInbox/workflow projection → full JSON serialization
```

New dependency:

```text
HTTP handler → s.inboxOverview() → narrow conversations/latest messages
             → application/event/draft/clarification enrichment
             → same workflow classification and global ordering
             → first five card DTOs
```

The new path classifies all narrow candidate records needed for global ordering, but does not materialize full histories, vacancy bodies, sections, or the full Inbox DTO.

## 6. Root Cause

One large response served consumers with different data requirements. The full response serialized 259 items with nested histories/metadata while Overview rendered five cards, causing unnecessary repository materialization, JSON construction, serialization, and transfer.

## 7. Chosen Read Model / API Design

Added explicit read-only `GET /api/inbox/overview`. Its DTO contains only the bounded items, conversation id/company/title/updated timestamp, one latest meaningful candidate/employer preview, generated draft status/text, and workflow state/action.

The existing `GET /api/inbox` full contract is preserved. Postgres omits `vacancy_description` and full message history for the Overview read. Legacy in-memory stores are narrowed before classifier input/serialization.

## 8. Ordering and Semantic Compatibility

No raw database `LIMIT 5` is applied before business logic. Overview follows the same visible/seen filtering, application/event enrichment, workflow resolution, follow-up calculation, classification, and `sortWorkflowInbox` ordering as full Inbox, then takes five.

The result-limit test verifies full Inbox returns the complete set and compares displayed Overview cards with full Inbox top five. Runtime replay matched id, company/title, latest text/timestamps, workflow state, and `what_to_do`.

## 9. Draft Scheduling Compatibility

Eligible Inbox draft scheduling remains intentional. Eligibility and deduplication are unchanged. Overview moves complete-history snapshot work to a background goroutine; the existing dashboard mutex is held only for detached snapshotting and is released before preparation/AI work. PERF-1.3 lock isolation and the existing worker path remain in use.

Full Inbox scheduling remains on its existing route. No HH write is added; dry-run protection remains authoritative.

## 10. Implementation

- `internal/runtime/dashboard_inbox_overview.go` — narrow DTO, parity workflow, ordering, bounded serialization.
- `internal/runtime/dashboard_inbox_overview_test.go` — parity, limit, JSON, partial-data, repository, draft tests.
- `internal/runtime/conversation_store.go` — Overview repository boundary/fallback.
- `internal/adapters/storage/postgres/conversation.go` — narrow parent/latest-message queries.
- `internal/runtime/dashboard_server.go` — route dispatch.
- `internal/runtime/dashboard_sync.go` — deferred Overview draft snapshot.
- `internal/runtime/web/app.js`, `web/app.js` — Overview endpoint selection.

## 11. Query / Read Reduction

The old Overview request traversed the full path (approximately 783 storage/read operations in the prior populated audit). The new path has five top-level read groups: conversations, applications, application events, drafts, and clarifications. Conversations use two SQL queries: narrow parents plus one latest-message query. There is no per-item history enrichment.

The latest query uses `DISTINCT ON (conversation_id)` with timestamp/sequence ordering. Counting-repository coverage verifies one bounded Overview conversation read and one latest message per conversation; fixture coverage tests 0, 4, 5, and 6 records without wall-clock assertions.

## 12. Payload Reduction

Five quiet direct Overview samples returned 5,365 bytes and five records. The former full response was approximately 954 KB and 259 records: approximately 99.44% payload reduction.

After replay, full `/api/inbox` remained approximately 954 KB with 259 records. No full-route fields were removed.

## 13. Focused Tests

Coverage proves Overview JSON has the fields used by the initial UI and omits histories/vacancy bodies; full Inbox remains complete; top-five ordering/data match; empty/partial/exact/over-limit sets are bounded; conversation access is a bounded group; and eligible draft scheduling remains asynchronous without blocking another Overview request.

## 14. Before / After

| Metric | Before | After | Change |
| --- | ---: | ---: | --- |
| Overview Inbox payload | 954,029–954,036 bytes via full route | 5,365 bytes | −99.44% |
| Overview Inbox records returned | 259 | 5 | bounded |
| Inbox read/query groups | ≈783 read operations, prior audit | 5 top-level groups / 6 SQL reads | bounded; no per-item history reads |
| Overview Inbox min | 254 ms, full route | 58 ms | −196 ms |
| Overview Inbox median | 284 ms, full route | 64 ms | −220 ms |
| Overview Inbox max | 377 ms, full route | 68 ms | −309 ms |
| Full `/api/inbox` payload | 954,029–954,036 bytes | 954 KB, 259 items | unchanged |
| Full Inbox contract parity | baseline contract | PASS | preserved |
| Full waterfall median | no strict comparable capture | 769 ms same-profile reference | N/A |
| Browser useful median | 272 ms | 467 ms | noisy; no reliable E2E gain established |

## 15. Full Waterfall

Same-profile after replay measured the unchanged full-inbox waterfall at 736–818 ms (median 769 ms). The new Overview waterfall was 502–583 ms (median 575 ms), a 19–29% local replay reduction.

The waterfall included dashboard boot, sync/status, dashboard Overview, the Inbox dependency, and notifications. Notifications remain separate and were not optimized here. No strict before waterfall is claimed.

## 16. Browser Measurement

Three local reloads on the same safe profile:

```text
before useful: 753, 272, 265 ms → median 272 ms
after useful:  614, 467, 264 ms → median 467 ms
DOM timing:    364 ms in both
```

Browser timing is explicitly noisy and does not establish end-to-end improvement. Direct API and server waterfall measurements establish payload/component-latency reductions.

## 17. Remaining Bottlenecks

Notifications are the largest remaining request in the Overview waterfall. Dashboard boot and the sequential request chain are also visible; no notification redesign or frontend parallelization was included in this stage.

## 18. Verification

All required checks passed:

```text
gofmt -w .
git diff --check
go test ./...
go test -race ./...
go vet ./...
go build ./...
go build -o ./out ./cmd/hh-ai-responder
node --check web/app.js
node --check internal/runtime/web/app.js
```

## 19. Safety

```text
Live HH writes observed: 0
Candidate truth changed: no
Candidate Knowledge changed: no
Match policy changed: no
HH write safety changed: no
Semantic scope changed: no
PostgreSQL source-of-truth changed: no
Full Inbox semantics changed: no
Draft scheduling semantics changed: no
Docker used: no
```

The endpoint is read-only. No HH application, test, chat, resume, or job-search-status write was performed. No secrets, cookies, authorization headers, or private raw AI response bodies were added.

## 20. Recommended Next Stage

PERF-1.5 — Notifications read-path and payload optimization.
