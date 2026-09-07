# Performance Stage 22.1 — Instant UX / Smart Sync

Status: implemented locally, read-only HH path preserved.

Safety configuration for this validation:

```text
HH_WRITE_ENABLED=false
HH_DRY_RUN=true
LIVE HH WRITES=0
```

## User workflow before

Opening Inbox or a conversation suggested that the user should run Sync first.
The visible actions were `Sync All`, `Sync Vacancies`, `Sync Applications` and
`Sync Conversations`. A complete conversation verification fetched the history
of all 261 conversations and took about 5–6 minutes at the enforced HH read
interval.

Fresh preflight was already read-only, but the previous UI model made a manual
conversation sync look like a prerequisite. Delivery confirmation required a
targeted read, but the user-facing lifecycle did not clearly expose the local
state, HH check, and delivery confirmation as separate stages.

## User workflow after

- Overview and Inbox render from local projections immediately.
- Inbox uses `Refresh Inbox`, an incremental metadata flow. Unchanged chats
  reuse local history; changed chats receive targeted detail reads.
- Sync progress exposes metadata checked, changed/unchanged counts, history
  reuse, and detailed chats fetched. Full verification remains under
  Maintenance / Advanced and is labelled `Full verification — may take ~5–6
  minutes`.
- Conversation pages render local data first. If `HH_CONVERSATION_DISPLAY_TTL`
  (default `60s`) has elapsed, a background targeted read refreshes only that
  chat. The page shows local freshness and HH last-checked time and does not
  block first paint.
- `Run fresh preflight` remains one click. It uses targeted HH reads and the
  gateway's own fresh safety validation; display refresh is never treated as
  send safety.
- Post-send delivery confirmation remains targeted and read-only. No automatic
  retry is introduced: absent confirmation stays `SENT_UNCONFIRMED` /
  `DELIVERY_UNCERTAIN`.
- Optional `HH_BACKGROUND_INBOX_REFRESH=true` schedules a lightweight Inbox
  metadata refresh at `HH_SYNC_INTERVAL`. It never becomes a 261-detail full
  sync and remains below safety/foreground reads in the global limiter.
- A compact global HH status bar reports up-to-date, targeted/inbox refresh,
  full verification progress, or unavailable state.
- Fast Health is the default `/api/health` projection. Expensive eligibility and
  reconciliation diagnostics moved to `/api/health/deep` and the `Run deep
  audit` action.

## Scheduling and regression guards

The HH requester now owns one global read scheduler. It preserves the existing
minimum request-start interval while selecting waiting work by priority:

```text
safety preflight / delivery reconciliation
        > targeted conversation refresh
        > foreground Inbox refresh
        > background Inbox / Full verification
```

Identical sync targets still coalesce through the existing singleflight calls.
Local dashboard operations queue behind the store lock instead of exposing a
normal `Local operation in progress` retry state. The synchronous compatibility
contract remains fail-safe for an explicitly duplicated long-running operation.

Targeted conversation sync, preflight, and reconciliation contain no call to
full conversation sync. The new Stage 22.1 tests cover priority ordering,
targeted-only reads, and incremental reuse counters.

## Performance baseline and targets

The Stage 22 measured baselines remain the reference dataset measurements:

| Flow | Stage 22 baseline | Stage 22.1 behavior / target |
| --- | ---: | --- |
| Conversation API local render | ~11.88 ms | preserve; regression guard <50 ms |
| Inbox local API | ~138.97 ms | preserve; cold <250 ms, warm <50 ms |
| Health | ~418 ms | fast snapshot; repeated unchanged response target <100 ms |
| Targeted conversation read | 1 HH request | remains targeted and priority scheduled |
| Full 261 conversation verification | ~5m31s | maintenance-only; rate limit unchanged |
| Incremental Inbox refresh | ~1m53s | async/progressive; local Inbox is usable immediately |

The local private dataset smoke was run with the web server in
`HH_WRITE_ENABLED=false`, `HH_DRY_RUN=true` mode. HTTP calls were local-only
(the environment did not provide `curl`; a Node HTTP client was used), and no
HH request was made:

| Local endpoint | First paint | Warm repeat |
| --- | ---: | ---: |
| `/api/dashboard` | 93 ms | <1 ms |
| `/api/inbox` | 171 ms | 3 ms |
| `/api/health` fast | 1 ms | <1 ms |
| `/api/conversations/:id` | 13 ms | 12 ms |
| `/api/health/deep` | 425 ms | 423 ms |

The Inbox cold smoke remains below the 250 ms target and the warm result is
below 50 ms. The conversation page was interactive before any targeted
refresh completed.

The private-dataset acceptance flow remains:

```text
A. start web
B. open Overview
C. open Inbox
D. open conversation
E. observe automatic targeted refresh
F. Run fresh preflight
G. return to Inbox
H. open another conversation
```

The acceptance criterion is time-to-interactive before any HH refresh
completes, followed by targeted HH state visibility.

## Health and Overview notes

Health now separates fast process/capability/count/sync state from deep audit.
Overview analytics uses the local aggregate projection and does not eagerly
run all conversation consistency analysis merely to render cards. Existing
generation-keyed view caching and cross-process file invalidation remain in
place. No storage migration was made.

## Third pilot action

Read-only inspection of
`hh-action-a31ba56bedf1664d3cf2983d9e84448f` after the changes:

```text
status: approved
nonce_used_at: null
external_message_id: absent
transport attempt: absent
HH write metrics increment: absent
```

The action remains logically current under the saved pilot state. No send,
nonce consumption, audit write, or HH transport was performed.

## Verification

The following checks are required for completion and are run against this
workspace:

```text
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
go build ./...
git diff --check
node --check web/app.js
```
