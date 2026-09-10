# R12.2b — Inbox Refresh / Post-Sync Projection Orchestration Boundary

## Before

### `DashboardServer.executeSync`

Before this change, `executeSync` performed the following sequence for every
non-`all` read target:

```text
HHReadSyncService.SyncAll or syncRead
  -> DashboardServer.mu
  -> reset decisions and invalidate views
  -> refreshLocalFiles
  -> inbox projection/classification
  -> asynchronous employer-draft scheduling from the projection
  -> notification calculation and persistence
  -> daily refresh-state calculation and persistence
  -> sync status/result update
```

The `all` path remains a full vacancies/applications/conversations read. The
new service owns only the `inbox` target, which is the path used by the
background inbox ticker and the dashboard inbox refresh. Other modes retain
their root behavior.

### Modes

| Mode | HH read operation | Local post-processing | Draft work | Notifications | Moved |
|---|---|---|---|---|---|
| `all` | `SyncAll` | root reload and full-sync compatibility path | none in this boundary | root | No |
| `inbox` | `RefreshInbox` / `syncRead("inbox")` | reload, inbox projection, daily state | existing root async scheduler | local refresh and save | Yes |
| `vacancies` | `syncRead("vacancies")` | root compatibility post-processing | root | root | No |
| `applications` | `syncRead("applications")` | root compatibility post-processing | root | root | No |
| `conversations` | `syncRead("conversations")` | root compatibility post-processing | root | root | No |
| `conversation:<id>` | targeted `syncRead` | root targeted compatibility path | root | root | No |

The non-inbox modes are intentionally not forced through the new service:
their read operation and response behavior are materially different.

### Callers and scheduling

- The dashboard synchronous inbox endpoint calls `executeSync("inbox")`.
- `queueBackgroundInboxRefresh` calls the same target and remains unchanged.
- `startBackgroundInboxRefresh` retains its ticker and cadence.
- `handleSync` retains async response, duplicate-target, and eight-target
  coalescing behavior.
- `hh sync` CLI paths remain separate CLI/report workflows. `hh inbox` is a
  local projection read and is not changed into a refresh command.

The old `inbox()` projection still owns the existing asynchronous draft
scheduler. It is invoked by the root projection adapter used by the new
service, so scheduler mechanics, `goroutine` ownership, draft coalescing, and
the dashboard display behavior remain root-owned. The scheduler calls the
R12.2a `AIReplyOrchestrator.PrepareEmployerReply` compatibility façade, which
delegates to `employerreplyworkflow`; it is not a direct AI call from
`inboxrefresh`.

Application-answer preparation remains root-owned and is only exposed by its
explicit dashboard/CLI paths. It is not scheduled by the inbox refresh.

## Characterization

| Case/phase | Existing behavior | Preserved |
|---|---|---|
| Successful inbox refresh | Read HH inbox, reload local files, build projection, refresh notifications, save daily state | Yes |
| HH read error | Still attempt local reload; return the original HH error; skip projection/notification/daily phases | Yes |
| Partial sync result with nil Go error | Continue into local post-processing; sync evidence retains embedded errors | Yes |
| Local reload error | Return reload error and stop after reload | Yes |
| Inbox projection error | Return error; do not refresh notifications or daily state | Yes |
| Employer reply preparation | Existing async root scheduler/facade remains in projection path | Yes |
| Employer manual/no-action result | Existing scheduler swallows per-item result/error as before | Yes |
| Follow-up preparation | Not scheduled by this refresh path; explicit follow-up route remains root/R12.2a | Yes |
| Notification calculation/persistence error | Return error; daily state is not persisted | Yes |
| Daily-state persistence error | Return error after notification persistence | Yes |
| Context cancellation | Context is passed to HH read and each local port; a cancelled context stops the next phase | Yes |
| Zero eligible conversations | Projection completes; no new draft scheduling work is created | Yes |
| Repeated unchanged refresh | Existing draft and notification deduplication owners remain unchanged | Yes |
| Candidate truth | No Candidate mutation, answer processing, or semantic reindex | Yes |

The extracted service has pure fake-port tests for call order, sync/reload
failure isolation, post-sync failure isolation, and cancellation. Existing
dashboard, notification, daily-state, draft-reuse, and R12.2a tests remain in
the repository and were run as regression coverage.

## Inbox refresh usecase

Package: `internal/usecase/inboxrefresh`

Service:

```go
func NewService(deps Dependencies, options Options) *Service
func (s *Service) Run(ctx context.Context, input Input) (Result, error)
```

Dependencies are narrow ports:

- `InboxSyncer.RefreshInbox(context.Context)` — R9 read-only sync boundary;
- `LocalProjection.Reload` and `LocalProjection.Build` — root local reload and
  inbox projection adapter;
- `NotificationRefresher.Refresh` — existing notification engine adapter;
- `DailyStateStore.Save` — existing daily-state projection/persistence adapter.

`Input` contains only the optional iteration time. Cadence, queue state,
coalescing, and background context ownership remain callers' concerns.

`Result` contains sync evidence, projection counts, notification counts, and
daily-state persistence evidence. It contains no send authorization, nonce,
HH mutation result, or next-run time.

## Sequence

1. `InboxSyncer.RefreshInbox(ctx)`.
2. `LocalProjection.Reload(ctx)`.
3. `LocalProjection.Build(ctx, now)`.
4. `NotificationRefresher.Refresh(ctx, now)`.
5. `DailyStateStore.Save(ctx, input)`.

The order is intentionally unchanged. The root adapter resets dashboard
decisions and invalidates views during reload, uses the existing `inbox()`
projection/classification policy, and splits the existing notification/daily
helper only at its persistence boundary.

## Controlled draft work

Employer reply preparation remains the existing asynchronous root scheduling
boundary. Its substantive decision, reuse, clarification, and local-draft
rules are owned by `internal/usecase/employerreplyworkflow` through the R12.2a
compatibility façade.

Follow-up preparation remains `internal/usecase/followuporchestration` through
the explicit follow-up route. It is not silently added to the inbox refresh.

Application answers remain root-owned. Auto-chat, chat send/leave, and vacancy
auto-apply are outside this stage.

## Projection, notifications, and daily state

The existing projection is reused rather than replaced. It preserves the
authoritative HH inbox data, local conversation/application reads, workflow
classification, sorting, pending clarification display, existing draft
display, follow-up display, and quality observations.

Notification rules remain in `CandidateNotificationEngine`, including
fingerprints, cooldowns, lifecycle resolution, critical delivery-uncertainty
notifications, and deduplication. Notification persistence still occurs before
daily-state persistence.

The existing `DailyRefreshState` shape is unchanged:

- `last_refresh_at`;
- workflow and employer-message fingerprints;
- follow-up eligibility;
- notification lifecycle state;
- irrelevant-item markers;
- changes, summary, and `last_refresh` run evidence.

The existing atomic file save and store-lock behavior is unchanged. No
migration or new day-boundary policy was introduced.

## Error behavior

The service is fail-fast between phases, matching the old caller-visible
behavior. A failed HH read still attempts local reload but prevents later
post-processing. Reload, projection, notification, and daily-state failures
stop subsequent phases. The root asynchronous draft scheduler continues to
handle per-item preparation failures as it did before; they do not become a
new synchronous dashboard error.

## Scheduling boundary

Ticker: ROOT
Queue/coalescing: ROOT
Async draft scheduling: ROOT
Cadence: unchanged
New background work: none

## Side effects

| Effect | Service behavior |
|---|---|
| HH read | Yes, via `InboxSyncer` |
| AI | Not directly; only the existing root R12.2a draft scheduler may invoke the controlled façade |
| Local writes | Yes: existing local projection, notification, draft/clarification, and daily-state paths |
| HH write | **None** |
| Candidate truth mutation | **None** |
| Candidate semantic reindex | **None** |
| Scheduler | **None inside `inboxrefresh`** |

The `inboxrefresh` package imports no HH-write, approval, preflight,
reconciliation, auto-chat, vacancy-apply, dashboard, CLI, JSON-adapter, or
Postgres package. Its only sync dependency is the typed R9 read capability.

## Root compatibility

- `DashboardServer.executeSync("inbox")` delegates to one
  `inboxrefresh.Service.Run` call and projects the legacy `SyncResult` response.
- `executeSync("all")` and targeted/non-inbox modes remain root compatibility
  paths.
- Manual and background inbox refresh share the same `inboxrefresh` owner.
- API JSON shapes, async responses, ticker cadence, and coalescing semantics
  are unchanged.

## Tests and verification

Focused pure usecase tests:

```text
go test ./internal/usecase/inboxrefresh/...        PASS
```

R12.2a and R12.1 package tests remain separate and unchanged. Full repository
verification completed as follows:

```text
gofmt -w .                                      PASS
go test -count=1 ./...                          PASS
go test -race ./...                             PASS
go vet ./...                                    PASS
go build ./...                                  PASS
git diff --check                                PASS
node --check web/app.js                         PASS
go test ./internal/usecase/inboxrefresh/...     PASS
go test ./internal/usecase/employerreplyworkflow/... PASS
go test ./internal/usecase/followuporchestration/... PASS
go test ./internal/usecase/careeriteration/...  PASS
docker build -t hh-ai-responder:r12-2b .       SKIPPED (Docker daemon unavailable)
```

`go list -deps ./internal/usecase/inboxrefresh` contains no forbidden
HH-write, approval, preflight, auto-chat, application-answer,
candidate-mutation, concrete-adapter, dashboard, or root-package edge.

LIVE HH WRITES: 0

## R12.2c inventory

AutoRespondChats, `autochatreply.Service`, chat send/leave, and their cadence
remain outside this stage. No R12.2c implementation was started.

## R12 status

R12.2b: EXTRACTED

Ready for R12.2c — Legacy Auto-Chat Orchestration Boundary.
