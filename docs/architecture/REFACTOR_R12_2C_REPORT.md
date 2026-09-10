# R12.2c — Legacy Auto-Chat Orchestration Boundary

## Before

`HHAIResponder.AutoRespondChats` owned one complete legacy iteration:

```text
HH chat discovery/read
  -> current-resume and candidate-context resolution
  -> awaiting-reply filtering
  -> chat history read and safety gate
  -> autochatreply.Service.Prepare
  -> review/no-reply mapping
  -> immediate SendChatMessage or LeaveChat
  -> local event projection
```

The root method was called by the run-once task path and by the recurring
completion-based auto-chat loop. No manual CLI or dashboard entry point calls
`AutoRespondChats` directly. Tests call it directly.

### Triggers

| Trigger | Mode | Can send | Can leave | Cadence | Limits |
|---|---|---:|---:|---|---|
| run-once task path | configured `off`, `review`, or `auto` | only `auto` and enabled/non-dry-run | only `auto` and enabled/non-dry-run | once | one read scan, max 10 pages |
| runtime auto-chat loop | configured `off`, `review`, or `auto` | only `auto` and enabled/non-dry-run | only `auto` and enabled/non-dry-run | 15 minutes after completion | one read scan, max 10 pages |
| direct tests | test-selected mode | test-selected gates | test-selected gates | caller controlled | fixture controlled |

The timer remains in the root and was not moved.

### Chat modes

The actual normalized values are `off`, `review`, and `auto`.

| Mode | Reads | AI | Send | Leave | Dry-run | Write disabled |
|---|---:|---:|---:|---:|---|---|
| `off` | no iteration | no | no | no | no iteration | no iteration |
| `review` | yes | yes | no; preview event | no; review log | preview only | no mutation |
| `auto` | yes | yes | only after leaf reply and legacy gates | only for discarded chats after legacy gates | preview only; zero action calls | gateway blocks before transport |

`HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` remain composition/runtime
policy. They are not read from the environment by the new package.

### Reads and history

The existing normalized HH read façade still performs the exact discovery and
selection rules: current resume resource, ignored chat list, last-message
presence/age, candidate-last-message exclusion, bot answer exclusion, vacancy
and resume resource checks, employer-safe candidate context, vacancy mapping,
buttons, and `DISCARD` detection. The service then reads each selected chat
history in order. History remains in provider order, with timestamp, display
author, and text only; the write-state gate rejects `WriteAllowed=false` and
the boundary of 20 or more messages. Such chats are added to the existing
in-memory ignore list.

## Characterization

| Case | Existing behavior | Preserved |
|---|---|---|
| no chats | one read scan, return nil | Yes |
| not awaiting reply / terminal-style filtering | filtered before history/AI | Yes |
| candidate already replied | skipped by list selection | Yes |
| incomplete or unsafe history | history read, then ignored; no AI/write | Yes |
| high-risk/manual review | one leaf proposal, preview only | Yes |
| leaf `no_reply` | no mutation and no preview event | Yes |
| leaf `reply` | review-gated or one send | Yes |
| leaf `leave_recommended` | no mutation; legacy root did not authorize leave from this outcome | Yes |
| leaf/provider error | continue to next selected chat | Yes |
| chat/history read error | list error aborts iteration; item history error continues | Yes |
| send success | one `chat_reply` event | Yes |
| send rejected | one attempt, error event, item ignored, continue | Yes |
| send ambiguous | one attempt, uncertainty preserved, no retry and no leave fallback | Yes |
| discard/leave success | one leave, no send | Yes |
| leave error/ambiguous | no retry, log and continue | Yes |
| dry-run | proposals/previews allowed; zero action calls | Yes |
| `HH_WRITE_ENABLED=false` | gateway blocks before transport; no HH mutation | Yes |
| per-run limit | max 10 pages, not a new per-chat cap | Yes |
| multiple chats with one failure | continue in original order | Yes |
| cancellation | context is passed through reads, leaf, and executor; cancelled reads/actions cannot mutate later chats | Yes |

## Auto-chat orchestration

Package: `internal/usecase/autochatorchestration`

Service:

```go
func NewService(deps Dependencies, options Options) *Service
func (s *Service) Run(ctx context.Context, input Input) (Result, error)
```

`Run` is exactly one iteration. It has no ticker, sleep, scheduler goroutine,
or next-run state.

Dependencies are narrow capabilities:

- `ChatSource` — normalized awaiting-chat/history reads and the existing local
  ignore-list projection;
- `ReplyPreparer` — the typed `autochatreply` proposal boundary;
- `ChatActionExecutor` — the consumer-owned send/leave semantic capability;
- `AuditSink` — local event projection;
- `Logger` — existing root logger adapter.

The result reports seen, eligible, generated, replied, left, manual-review,
skipped, and failed/uncertain item counts without exposing scheduler state.

## Iteration sequence

1. Read the selected awaiting chats through the normalized read capability.
2. Preserve selected-chat order.
3. For discard chats, authorize only the existing auto/non-dry-run leave path.
4. Read history and apply the existing write-state and 20-message safety gate.
5. Build the detached `autochatreply.Input` with the same candidate, buttons,
   history, communication profile, contacts, GitHub URL, and extra prompt.
6. Call `autochatreply.Service.Prepare` once.
7. Preserve `no_reply` and `leave_recommended` as no-action outcomes.
8. Map review/dry-run to the existing preview event.
9. Execute at most one send for a reply, then map success/error locally.
10. Continue to the next distinct chat after item-level read, AI, or write
    failure.

Behavior: **UNCHANGED**.

## Selection and history

The current filtering and candidate-context resolution remain in the root
composition adapter behind `ChatSource`; the importable service sees only
detached normalized chat values. This preserves the exact three-day cutoff,
candidate-last-message check, ignored-chat behavior, current-resume resource
check, vacancy/resource lookup, discard state, button text, history order,
sender display names, and message bound.

## AI boundary

Leaf: `internal/usecase/autochatreply`

Direct `CompletionProvider`: **NO**. The orchestration package has no generic
AI client, prompt builder, parser, or business retry. It calls the leaf once
per eligible chat.

## External action policy

Reply: one `SendChatMessage` semantic execution after `auto` mode, dry-run,
review, and leaf gates. A rejected or uncertain result is recorded locally;
there is no send-to-leave fallback.

Leave: one `LeaveChat` semantic execution for an existing discarded-chat
decision in `auto` mode when not dry-run. A leaf `leave_recommended` result is
not itself authorization and remains no-action, matching the legacy path.

Manual review: zero HH mutations; the existing preview event is emitted for a
proposal with a review reason.

No reply: zero HH mutations and no invented leave.

## R11 integration

Send path:

```text
autochatorchestration
  -> legacyAutoChatActions
  -> hhwritegateway.Service
  -> legacyChatGatewayWriter
  -> HH write adapter
  -> one transport attempt
```

Leave follows the same executor/gateway/adapter boundary through the leave
writer. The orchestration package does not import the concrete writer or
`internal/ports/hhwrite`.

`HH_WRITE_ENABLED=false` blocks in the gateway before transport. Dry-run is
handled before any action executor call for proposal and discard preview
paths. Ambiguous delivery remains uncertain, with no retry, new nonce, or
leave fallback.

## Idempotency

The existing root UUIDv4 generation remains the owner for a logical legacy
send attempt. One send call creates one key. It is never regenerated by this
service after rejection or ambiguity, because this service never retries.

## Failure behavior

- list/discovery failure: return the existing `load chats error` failure;
- history failure: log, record an item failure, and continue;
- AI failure: record an item failure and continue;
- send rejection: one attempt, error event, ignore that chat, continue;
- send ambiguity: one attempt, `delivery_uncertain`, error event, no retry;
- leave failure/ambiguity: log, no retry, continue;
- audit failure: ignored by the local audit adapter and never triggers a
  second HH mutation.

## Scheduler

Owner: **ROOT**
Cadence: **15 minutes after completion**
Changed: **NO**

## Capability containment

| Capability | Boundary |
|---|---|
| HH reads | normalized read façade only |
| AI | typed `autochatreply` leaf only |
| HH writes | `ChatActionExecutor` -> existing R11 gateway only |
| Candidate mutation | none |
| Approval/draft workflow | not used |
| Controlled employer reply | not used |
| Inbox refresh | not merged |

## Root compatibility

`AutoRespondChats` now checks the existing `off` mode and delegates one call
to `autochatorchestration.Service.Run`. Root composition constructs the read,
leaf, action, logging, and audit adapters. The recurring loop and run-once
caller remain unchanged.

## R12.3 inventory

`CandidateKnowledgeAcquisitionService` remains root/composition-owned. Its
current entry points are controlled employer-reply clarification creation and
candidate answer/proposal confirmation flows. Gap detection, interpretation,
proposal persistence, mutation, and semantic reindexing remain outside this
stage and were not changed.

## Tests

Pure `autochatorchestration` tests use fakes for chat reads, history, the
typed leaf, action executor, audit, and logging. They cover reply, leave,
dry-run, review, manual review, no reply, ambiguity, item continuation,
unsafe-history limits, and cancellation. Existing root tests continue to
exercise HH-shaped read fixtures and verify no write endpoint is called in
dry-run.

## Dependencies

`go list -deps ./internal/usecase/autochatorchestration` contains the typed
`autochatreply` leaf and candidate value packages. The orchestration package
has no direct root, dashboard, CLI, storage, concrete HH read adapter,
concrete HH write adapter, approval, preflight, reconciliation, or generic
completion-provider import. The leaf's own transitive completion boundary is
present only because `autochatreply` owns the R10 AI call.

## Retry audit

The orchestration package contains no retry, resend, backoff, sleep, or
second-attempt path. The existing AI provider retry remains below the typed
leaf; HH write retry is **NONE**.

## Behavior

Auto-chat trigger, cadence, modes, selection, history window/order, candidate
safety, leaf semantics, generation count, send/leave policy, dry-run,
`HH_WRITE_ENABLED`, controlled workflow separation, inbox separation, and
application pipeline separation remain unchanged.

## Verification

```text
gofmt: PASS
go test -count=1 ./...: PASS
race: PASS
vet: PASS
build: PASS
diff: PASS
node: PASS
Docker: SKIPPED (daemon unavailable)
LIVE HH WRITES: 0
```

Focused regressions include `autochatorchestration`, `autochatreply`,
`hhwritegateway`, HH write adapter, employer reply/follow-up, inbox refresh,
and the root `AutoRespondChats` tests; all passed. `go list ./...` and the
three requested dependency listings also passed. The static orchestration
audit found no forbidden concrete adapter or write/approval dependency, and
the auto-chat path has no HH-write retry/resend/backoff/second-attempt edge.

## R12 status

R12.2c: **EXTRACTED**

## Ready

R12.3 — Candidate Learning Orchestration: **READY**

No R12.3, R12.4, R12.5, or R12.6 work was started.
