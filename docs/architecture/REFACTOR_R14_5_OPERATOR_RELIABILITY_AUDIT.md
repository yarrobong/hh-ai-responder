# Executive summary

Highest operator/reliability gap: **automatic application and legacy auto-chat
attempts are durable safety records without an operator read model**.

Why: the records survive restart and correctly block unsafe replay, but the
dashboard and CLI do not load or list them. The operator therefore cannot see
the AttemptID, target, provider identifiers, evidence, reconciliation time,
or the reason an automatic workflow remains blocked. Controlled chat is the
reference model: it has durable ActionID state, an audit record, exact
provider-message evidence, and a read-only reconciliation endpoint.

This audit makes no production change other than this document. No retry,
reset, TTL release, scheduler, migration, dashboard control, or new HH write
path was added. Existing worktree changes were preserved.

The governing conclusion is:

- unresolved post-dispatch state is not evidence of non-delivery;
- `DELIVERY_UNCERTAIN` and `SENDING` must remain blocking without stronger
  evidence;
- “resolve” must never mean “send again”;
- a future intentional repeat must be a new, explicit action with a fresh ID,
  fresh reads, fresh preflight, and normal write gates;
- `HH WRITE RETRY: NONE` remains unchanged.

# State inventory

The categories below describe semantics, not a new shared state machine.
`OPERATOR_ATTENTION` is a presentation classification for a state that needs
human awareness; it is not an added domain state.

## A. Controlled chat

Source: `internal/runtime/hh_write_gateway.go`,
`internal/usecase/hhwritereconcile`, and the durable HH write audit store.

| Actual state | Classification | Meaning |
|---|---|---|
| `pending` | OTHER | Draft/action exists but is not approved for sending. |
| `approved` | OPERATOR_ATTENTION | Explicit approval exists; send remains a separate gated action. |
| `sending` | UNRESOLVED_BLOCKING | Durable send reservation exists; transport outcome is not final. |
| `sent` | TERMINAL_ACCEPTED | Transport/provider request was accepted; this is distinct from readback confirmation. |
| `sent_unconfirmed` | UNRESOLVED_BLOCKING, OPERATOR_ATTENTION | A send result exists but delivery is not confirmed. |
| `delivery_uncertain` | UNRESOLVED_BLOCKING, OPERATOR_ATTENTION | The provider effect may have happened and must not be resent. |
| `manual_review` | UNRESOLVED_BLOCKING, OPERATOR_ATTENTION | Local persistence or provider evidence requires review. |
| `delivery_confirmed` | TERMINAL_CONFIRMED | Exact provider message ID was found in the targeted conversation. |
| `failed` | OTHER | Final local failure status; it is not an automatic retry instruction. |
| `cancelled` | OTHER | Local action was cancelled before execution; it is not a provider delivery claim. |
| `stale` | OPERATOR_ATTENTION, OTHER | Action is no longer valid for execution. |

Controlled chat also has result/audit values such as `persistence_uncertain`.
That outcome must not be confused with a durable action state: after a
post-transport local failure, the durable action/audit state is the safety
authority and reconciliation remains read-only.

## B. Automatic application

Source: `internal/applicationattempt/model.go`, JSON/Postgres adapters, and
`internal/usecase/applicationreconciliation`.

| Actual state | Classification | Meaning |
|---|---|---|
| `SENDING` | UNRESOLVED_BLOCKING | Attempt was persisted before possible dispatch. A crash does not prove non-delivery. |
| `ACCEPTED` | TERMINAL_ACCEPTED, blocking | Existing transport semantics accepted the response; delivery/causality is not necessarily confirmed. |
| `REJECTED` | REPLAYABLE | Existing execution classified the request as rejected; a later normal run may make a separately identified attempt. |
| `NOT_SENT` | REPLAYABLE | Execution proved that the writer was not called; this is not an operator intuition state. |
| `DELIVERY_UNCERTAIN` | UNRESOLVED_BLOCKING | Dispatch may have happened or the outcome/persistence is insufficient. |
| `TARGET_RESPONSE_CONFIRMED` | TERMINAL_CONFIRMED, blocking | Strong fresh HH evidence shows an applicant response for the vacancy. It confirms target response, not necessarily causal ownership by this AttemptID. |

There is no transition from unresolved application state to `NOT_SENT`, and no
clear/reset transition. The conflict key is vacancy-wide and the stored
identity includes `AttemptID`, `VacancyID`, and `ResumeID`.

## C. Legacy auto-chat reply

Source: `internal/autochatattempt/model.go`, its JSON/Postgres adapters, and
`internal/usecase/autochatorchestration`.

| Actual state | Classification | Meaning |
|---|---|---|
| `SENDING` | UNRESOLVED_BLOCKING | Reply attempt was reserved before the one provider mutation. |
| `ACCEPTED` | TERMINAL_ACCEPTED, blocking | Existing provider/transport outcome accepted; no delivery reconciliation exists yet. |
| `REJECTED` | REPLAYABLE | A later normal run may consider a separately identified attempt. |
| `NOT_SENT` | REPLAYABLE | Only when execution proved no writer call occurred. |
| `DELIVERY_UNCERTAIN` | UNRESOLVED_BLOCKING | A reply may already exist. |
| `TARGET_REPLY_CONFIRMED` | TERMINAL_CONFIRMED, blocking | Schema-level target confirmation concept; no complete operator reconciliation flow currently exists. |

The durable identity is `AttemptID`, `ConversationID`, exact
`TriggerMessageID`, `ActionType=REPLY`, request key, and optional outgoing
provider message ID. A blocked M1 does not block a genuinely new employer
trigger M2 with a different provider message ID.

## D. Legacy auto-chat leave

Leave uses the same durable trigger reservation but has `ActionType=LEAVE`.
Its states are:

| Actual state | Classification | Meaning |
|---|---|---|
| `SENDING` | UNRESOLVED_BLOCKING | Leave may have been dispatched. |
| `ACCEPTED` | TERMINAL_ACCEPTED, blocking | Provider/transport accepted the leave request. |
| `REJECTED` | REPLAYABLE | Replayable only by later normal policy as a new attempt. |
| `NOT_SENT` | REPLAYABLE | Only when the writer was definitively not called. |
| `DELIVERY_UNCERTAIN` | UNRESOLVED_BLOCKING | Active/left state cannot be inferred from absence of a later read. |
| `TARGET_LEAVE_CONFIRMED` | TERMINAL_CONFIRMED, blocking | Future strong provider evidence could confirm the target outcome. |

There is no provider idempotency contract or exact read reconciliation for
leave in the current implementation. A reply and leave cannot both reserve
the same conversation/trigger because the conflict key intentionally excludes
action type.

## Maintenance direct writes

`TouchResume` and `SetActiveJobSearchStatus` remain intentional best-effort
operations. Their effects are semantically repeatable and duplicate impact is
low. They return errors/booleans and scheduler/runtime logs record failures.
No durable attempt state or operator-resolution workflow is required by the
current safety model.

# Visibility matrix

Legend: Yes means the relevant surface exposes the actual state and identity;
Partial means only an event, aggregate, or generic error is visible; No means
there is no current inspection surface for that workflow/state.

| Workflow | State | Dashboard | CLI | Logs | Durable | Operator clarity |
|---|---|---:|---:|---:|---:|---:|
| Controlled chat | `pending`, `approved` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `sending` | Yes | Partial | Yes | Yes | Partial |
| Controlled chat | `sent` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `sent_unconfirmed` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `delivery_uncertain` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `manual_review` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `delivery_confirmed` | Yes | Partial | Yes | Yes | Yes |
| Controlled chat | `failed`, `cancelled`, `stale` | Yes | Partial | Yes | Yes | Partial |
| Automatic application | `SENDING` | No | Partial | Partial | Yes | No |
| Automatic application | `ACCEPTED` | No | Partial | Partial | Yes | No |
| Automatic application | `REJECTED` | No | Partial | Partial | Yes | No |
| Automatic application | `NOT_SENT` | No | Partial | Partial | Yes | No |
| Automatic application | `DELIVERY_UNCERTAIN` | No | Partial | Partial | Yes | No |
| Automatic application | `TARGET_RESPONSE_CONFIRMED` | No | Partial | Partial | Yes | No |
| Legacy auto-chat reply | `SENDING` | No | No | Partial | Yes | No |
| Legacy auto-chat reply | `ACCEPTED` | No | No | Partial | Yes | No |
| Legacy auto-chat reply | `REJECTED` | No | Partial | Partial | Yes | No |
| Legacy auto-chat reply | `NOT_SENT` | No | Partial | Partial | Yes | No |
| Legacy auto-chat reply | `DELIVERY_UNCERTAIN` | No | No | Partial | Yes | No |
| Legacy auto-chat reply | `TARGET_REPLY_CONFIRMED` | No | No | Partial | Yes | No |
| Legacy auto-chat leave | `SENDING` | No | No | Partial | Yes | No |
| Legacy auto-chat leave | `ACCEPTED` | No | No | Partial | Yes | No |
| Legacy auto-chat leave | `REJECTED` | No | Partial | Partial | Yes | No |
| Legacy auto-chat leave | `NOT_SENT` | No | Partial | Partial | Yes | No |
| Legacy auto-chat leave | `DELIVERY_UNCERTAIN` | No | No | Partial | Yes | No |
| Legacy auto-chat leave | `TARGET_LEAVE_CONFIRMED` | No | No | Partial | Yes | No |

Details behind the matrix:

- Controlled actions expose ActionID, conversation/application/draft identity,
  status, nonce metadata, timestamps, error, audit event, and provider message
  ID when known. The dashboard exposes a read-only reconciliation action. The
  CLI exposes controlled `hh write-status` and `hh action preflight` /
  `request-preview`, but not a general reliability list.
- Application records contain AttemptID, VacancyID, ResumeID, state,
  ProviderStatus/ErrorClass, provider application/negotiation IDs, evidence
  source/kind/strength, and observed time when reconciliation has run. Those
  fields are not loaded by the dashboard or a dedicated CLI command. Runtime
  events may show a blocked AttemptID/state or a generic application error,
  but not a durable operator view.
- Auto-chat records contain AttemptID, ConversationID, TriggerMessageID,
  ActionType, request key, optional outgoing provider ID, state, and
  ErrorClass. They are durable, but there is no `Get`, `List`, or reconciliation
  read model in the auto-chat port or dashboard.
- None of the application/auto-chat operator surfaces currently show a
  `last persistence error`, `ObservedAt`, or `ReconciledAt`. ErrorClass is
  stored where available but is not projected.
- Durable means the JSON/Postgres attempt/action store, not the in-memory
  512-event lifecycle buffer. The event stream itself is not the authority.

# Controlled chat

Controlled chat is the strongest existing observability model. A durable
ActionID and nonce are created before dispatch. The audit store records action
identity, operation, provider ID when returned, status/result, HTTP metadata,
endpoint/method, timestamps, and sanitized error/correlation fields.

`HHWriteGateway.ReconcileDelivery` is explicitly read-only. It performs a
bounded targeted conversation read and confirms only an exact matching
provider outgoing message ID. Absence is not negative evidence and cannot
release the action. A reconcile call cannot create a new send. The dashboard
currently exposes “Run read-only reconciliation”; sending is a separate
explicit action requiring the stored nonce, fresh preflight, and write gates.

Current gaps are projection and failure reporting, not a missing replay guard:
CLI coverage is aggregate/point lookup only, reconciliation outcomes are not
turned into a complete operator notification workflow, and some local action
reload/audit errors are surfaced only partially. `manual_review` is a useful
local operator-attention state, but it does not authorize a resend.

# Automatic applications

R14.1–R14.3 provide a sound fail-closed safety state: reserve `SENDING` before
the one existing executor call; persist one outcome; keep the blocking residue
on outcome-persistence failure; reconcile only with fresh read-only provider
evidence. `SENDING`, `ACCEPTED`, `DELIVERY_UNCERTAIN`, and
`TARGET_RESPONSE_CONFIRMED` block future automatic dispatch.

Application reconciliation can positively confirm a response from fresh
`AlreadyResponded` or matching applicant-response/application/negotiation
evidence. It cannot prove that this particular AttemptID caused the response,
and it has no reliable post-dispatch negative evidence. Therefore it must not
convert uncertainty to `NOT_SENT`.

The dashboard application page is a `JobApplication` read model, not an
automatic-attempt read model. It does not display AttemptID, VacancyID plus
ResumeID attempt identity, state, ProviderNegotiationID, evidence source,
ObservedAt/ReconciledAt, or persistence failure. The CLI has run summaries and
generic events but no attempt inspection command. This is the highest gap.

The existing manual `ApplyVacancy`, `ApplyVacancyWithTest`, and `SendResponse`
paths remain separate from the automatic attempt wrapper and retain fresh
preflight/write gates. They are sufficient as a future explicit manual path;
a “force retry automatic attempt” control is unnecessary and unsafe.

# Legacy auto-chat

R14.4 correctly persists the trigger identity and blocks the exact old trigger
after restart. The records are not visible in the dashboard or a dedicated
CLI. A `chat_reply_error` event can include attempt/conversation/trigger/state
context, but a successful `chat_reply` projection does not carry the full
durable state/provider identity. The runtime also ignores the auto-chat result
object, so per-item store failures can be hidden from the top-level run result.

An old unresolved M1 and a new employer trigger M2 are distinguishable by
`TriggerMessageID`; M2 does not require a destructive “clear M1” operation.
The future UI should show both records, not reset M1 to process M2.

Manual candidate interference is not causally attributable today. If later
history proves that a candidate reply exists, a future reconciliation may mark
the target response confirmed, but must say “target reply confirmed” and must
not claim that the legacy automatic attempt itself succeeded. The original
attempt should remain causally unresolved unless stronger evidence links it.

LEAVE has even weaker evidence: no exact outgoing-message reconciliation and no
documented provider idempotency key. It should remain blocking when uncertain;
manual reset is not necessary under the current policy.

# Maintenance writes

Resume touch and job-search status are best-effort, intentionally repeatable
maintenance writes. Runtime and scheduler return/log errors; these errors are
visible in normal logs and do not create duplicate-sensitive business state.
No durable operator-resolution UI or attempt store is needed. A future health
view may aggregate scheduler errors, but should not introduce retry controls or
make maintenance state part of application/chat gating.

# Evidence model

Positive evidence:

- Controlled chat: exact provider outgoing message ID in the targeted
  conversation, using the shared deterministic matcher.
- Automatic application: fresh positive `AlreadyResponded`, or fresh
  matching vacancy evidence with `ResponseByApplicant` and a stable provider
  application/negotiation ID. This is strong target evidence, not necessarily
  exact AttemptID causality.
- Legacy auto-chat: no complete current positive reconciliation contract. A
  provider outgoing ID returned from the write is useful identity, but without
  a bounded history reader and matching rules it is not a reconciliation result.

Negative evidence:

- Controlled pre-dispatch validation and a proven zero writer call can prove
  “not sent” before provider dispatch.
- Application and auto-chat execution metadata can classify `NOT_SENT` only
  when the writer was definitively not called.
- There is currently no general strong post-dispatch negative provider
  evidence for application, reply, or leave. A missing history item, missing
  read response, timeout, or process restart is not negative evidence.

Absence:

Absence of a provider record is “evidence unavailable/insufficient,” never
“not sent.” Positive evidence may upgrade a local unresolved record; absence
must preserve the block.

# Safe operator actions

| Workflow/state | Action | Safe? | Required evidence |
|---|---|---|---|
| Controlled unresolved | Reconcile now | SAFE | Existing bounded read-only exact-ID reconciliation. |
| Application `SENDING`/`DELIVERY_UNCERTAIN` | Reconcile now | SAFE | Existing read-only application reconciliation; no writer capability. |
| Auto-chat `SENDING`/`DELIVERY_UNCERTAIN` | Reconcile now | SAFE ONLY AFTER READ CONTRACT | Bounded history read and explicit REPLY/LEAVE matching; not currently implemented. |
| Any unresolved | Acknowledge | SAFE | Local notification/action acknowledgment only; does not alter attempt safety state. |
| Any unresolved | Mark reviewed | SAFE | Local audit annotation; no state release and no external effect. |
| Controlled | Mark confirmed | EVIDENCE-ONLY | Exact provider message ID match. |
| Application | Mark confirmed | EVIDENCE-ONLY | Strong fresh target-response evidence; do not claim exact attempt causality. |
| Auto-chat reply | Mark confirmed | EVIDENCE-ONLY | Strong matching history evidence; distinguish target reply from automatic-attempt success. |
| Auto-chat leave | Mark confirmed | EVIDENCE-ONLY | Strong positive provider leave/inactive evidence; currently unavailable. |
| Rejected/`NOT_SENT` | Observe | SAFE | No intervention required; normal future policy handles replay. |
| Any state | Create new action | EVIDENCE-ONLY | Explicit intent, fresh ID, fresh reads/preflight, normal gates, separate warning. |

# Unsafe actions

Mark not sent:

Do not allow an operator to mark application or auto-chat `SENDING` or
`DELIVERY_UNCERTAIN` as `NOT_SENT` by intuition, timeout, or absent history.
The recommendation is **NO**, unless strong negative evidence proves no
provider dispatch. The local record must remain blocking otherwise.

Clear:

Do not allow “clear blocking attempt” as a standalone operation. It can erase
the safety history and make an old external effect indistinguishable from a
new attempt. Acknowledge/review may be local annotations; they must not clear
the gate.

Retry:

Do not expose “Retry” or “Resend” for unresolved attempts. Reconciliation must
not call a writer. If a repeat is ever permitted, it is a separately named
“Create new action,” not a state reset to `SENDING` and not reuse of the old
AttemptID/ActionID/request key.

# New action semantics

A future explicit new application or chat action must:

1. require visible operator intent and a high-friction confirmation;
2. create a new AttemptID/ActionID exactly once;
3. perform fresh provider reads and fresh preflight;
4. use normal dry-run, write-enabled, eligibility, and per-run gates;
5. preserve the old unresolved record and link the new action to it in the
   audit trail;
6. never be triggered by a read page, notification resolution, or reconcile
   endpoint.

The current manual application methods are already a separate explicit path.
Controlled chat approval/send is already a separate explicit path. Legacy
auto-chat needs no “retry legacy auto-chat” control; manual controlled reply is
the distinct workflow.

# Operator action safety matrix

`EVIDENCE-ONLY` means the action is safe only after the specified positive or
negative provider/execution evidence exists. `UNSAFE` means it must not be a
future control for that state.

| Workflow/state | Reconcile now | Mark confirmed | Mark not sent | Clear block | Create new action |
|---|---|---|---|---|---|
| Controlled `sending` / uncertain | SAFE | EVIDENCE-ONLY | EVIDENCE-ONLY before dispatch only | UNSAFE | EVIDENCE-ONLY |
| Application `SENDING` | SAFE | EVIDENCE-ONLY | EVIDENCE-ONLY if writer never called | UNSAFE | EVIDENCE-ONLY |
| Application `ACCEPTED` | SAFE | EVIDENCE-ONLY | UNSAFE | UNSAFE | EVIDENCE-ONLY |
| Application `DELIVERY_UNCERTAIN` | SAFE | EVIDENCE-ONLY | UNSAFE | UNSAFE | EVIDENCE-ONLY |
| Application `TARGET_RESPONSE_CONFIRMED` | SAFE | N/A | UNSAFE | UNSAFE | UNSAFE as automatic release |
| Auto-chat REPLY `SENDING` / uncertain | SAFE ONLY AFTER READ CONTRACT | EVIDENCE-ONLY | UNSAFE | UNSAFE | EVIDENCE-ONLY |
| Auto-chat LEAVE `SENDING` / uncertain | SAFE ONLY AFTER READ CONTRACT | EVIDENCE-ONLY | UNSAFE | UNSAFE | EVIDENCE-ONLY |
| Any `REJECTED` / `NOT_SENT` | N/A | N/A | N/A | N/A | N/A; normal policy already handles it |

No row authorizes a direct automatic resend. “Mark not sent” is only a safe
local conclusion where execution evidence proves the writer was never called;
there is no safe operator override for a post-dispatch unresolved state.

# Method, CSRF, and intent conventions

The current dashboard is loopback-only and checks same-host Origin when
present, rejects cross-site `Sec-Fetch-Site`, requires `POST` plus
`X-Career-Agent: local` and JSON for mutating routes, and requires a
server-side send nonce plus fresh preflight for controlled sending. The
existing reconcile endpoint is technically POST but calls only a read-only
provider path; this is safe when its contract remains explicit.

No critical method/CSRF vulnerability was found. Future resolution endpoints
must use the same local-dashboard protections and must be clearly separated:
reconciliation and acknowledgment may only mutate local evidence/annotation;
any endpoint authorizing a new HH write must require explicit intent, a fresh
ID, fresh preflight, and the normal write gates. A GET, page load, notification
resolution, or reconciliation request must never mutate HH.

# Audit trail and provider identifiers

If a later stage adds operator actions, the minimum durable audit entry is:
operator action type, target AttemptID/ActionID, target identity, timestamp,
previous state, resulting local annotation/state, evidence source and
observed/reconciled time, and a new action ID when a new action was explicitly
created. The old attempt must remain immutable history.

Provider identifiers safe and useful to display are vacancy ID, conversation
ID, provider negotiation/application ID, trigger message ID, and provider
outgoing message ID. Local AttemptID/ActionID should be prominent. Request
keys may be shown only as a masked/correlation value if needed for support;
cookies, authorization headers, session secrets, and environment variables
must never be displayed.

# State/status conflation risks

The following must remain separate in every future read model:

- HH application status (provider business state);
- automatic application attempt state (local replay gate);
- transport outcome (`ACCEPTED`, `REJECTED`, or uncertain);
- reconciliation evidence and its observation time;
- conversation state;
- controlled chat action state; and
- legacy auto-chat attempt state.

The current dashboard mostly avoids direct conflation by omitting automatic
attempts, but omission is itself the observability gap. A generic
“application failed” or “chat failed” projection must not replace the durable
workflow-specific state.

# Listing, bounds, and retention-readiness

The stores currently support targeted point reads (`Get`/blocking lookups),
not operator listing. The smallest future capabilities are bounded
workflow-specific queries: application `ListUnresolved(limit)` and, if
needed, `ListByVacancy(vacancyID)`; auto-chat `ListUnresolved(limit)` and
`FindByConversation(conversationID)`. Results need stable ordering, a limit or
cursor, and an explicit “more available” signal. No unbounded attempt history
load or generic `ListAll` is justified.

The dashboard has some bounded lifecycle/health views but several ordinary
career lists are broad store reads; a new reliability table must not copy the
unbounded pattern. Listing is read-only and should not alter gates.

# Test review

Existing characterization coverage includes:

- controlled action lifecycle, sent-unconfirmed/manual-review behavior, exact
  read-only reconciliation, dashboard action routes, and audit-store failure;
- application state transitions, JSON store corruption/atomicity, executor
  reservation/outcome persistence uncertainty, gating, and application
  reconciliation evidence;
- auto-chat state transitions, JSON durability/corruption, blocking before AI,
  new-trigger behavior, ambiguous send, and migration contracts;
- runtime/dashboard/performance regressions and notification-store behavior.

Missing tests for a later stage are operator-facing characterization tests:

- bounded application and auto-chat unresolved listing/detail and restart
  visibility;
- dashboard/CLI projection of all IDs, evidence, persistence errors, and
  store availability;
- dedicated application/auto-chat uncertainty notifications and confirmed vs
  conflicting reconciliation notifications;
- propagation of auto-chat per-item store failures to a visible run result;
- manual-message interference, target confirmation without claiming automatic
  causality, and eventual leave evidence rules;
- pagination, retention safety, idempotent acknowledgment/review, and proof
  that no read/reconcile action invokes an HH writer.

No tests were added in this audit because no production behavior changed.

# Reconcile-now feasibility

| Workflow | Already exists? | Read-only? | Bounded? | Requires provider write? | Can change only local evidence? |
|---|---:|---:|---:|---:|---:|
| Controlled chat | Yes | Yes | Yes, targeted conversation read | No | Yes |
| Automatic application | Internal service exists | Yes | Yes, vacancy-scoped fresh reads | No | Yes |
| Legacy auto-chat reply | No | N/A | N/A | N/A | N/A |
| Legacy auto-chat leave | No | N/A | N/A | N/A | N/A |

Application reconciliation can be exposed later without adding a write path.
Auto-chat requires a separately specified read-only evidence contract first;
the existing durable fields alone do not make history matching safe.

# Persistence/store failure visibility

| Store/workflow | Current behavior | Visibility classification |
|---|---|---|
| Application JSON/Postgres | Corrupt/unavailable store fails closed; automatic application does not dispatch. Runtime logs the application error, but dashboard/CLI do not load the attempt store. | PARTIAL |
| Auto-chat JSON/Postgres | Corrupt/unavailable store fails closed for live AUTO. Per-item failures are placed in a result that the runtime currently discards at the top level. | HIDDEN / PARTIAL |
| Controlled action/audit | Load errors can stop dashboard/CLI or appear as health/audit errors; some reload errors are only partially projected. | CLEAR / PARTIAL |
| Notification store | Corruption/load failure prevents notification view/startup paths; notifications are not authoritative. | PARTIAL |
| Maintenance | Scheduler/runtime errors are returned/logged. | CLEAR enough |

The safety behavior is conservative, but an operator may not know why writes
stopped. Future read models should surface store availability and last error
class without exposing secrets or treating a health row as permission to
write. Store-failure incidents need not authorize any action.

# Notification gaps

Controlled `sent_unconfirmed`/`delivery_uncertain` audit events are converted
to a durable local `delivery_uncertain` notification with wording that repeat
sending is prohibited until reconciliation. Application and legacy auto-chat
uncertainty do not currently receive dedicated notification projections.

Reconciliation confirmed/conflicting results are not consistently projected as
notifications. Store-unavailable conditions also do not reliably create a
notification. Notification save/delivery failure can hide an important signal,
so notifications must remain advisory; action/attempt stores remain the
authority. Existing notification dismiss/resolve is local notification
lifecycle only and must not be named or implemented as attempt resolution.

# Dashboard gaps

The dashboard dependencies and views load controlled action/audit state,
career/application read models, conversations, notifications, and in-memory
lifecycle events. They do not load `application_attempts.json`/
`automatic_application_attempts` or `autochat_attempts.json`/
`legacy_auto_chat_attempts`.

Missing future read capability:

- bounded unresolved application list/detail with AttemptID, vacancy/resume,
  state, provider IDs, evidence, timestamps, and persistence error class;
- bounded unresolved auto-chat list/detail with AttemptID, conversation,
  trigger, REPLY/LEAVE, request key status, provider outgoing ID, and outcome;
- explicit distinction between HH application status, automatic attempt state,
  transport outcome, and reconciliation evidence;
- store availability/error projection;
- notification projection for app/auto-chat uncertainty and reconciliation
  outcomes.

Do not load unbounded history. Follow the existing bounded/paginated dashboard
conventions; no generic “admin” page is required.

# CLI gaps

Current `hh write-status` and `hh action preflight|request-preview` inspect
controlled HH actions. Top-level `reconcile` is career-data reconciliation,
not application/auto-chat attempt reconciliation. `monitor`, `audit`, and
`hh sync` likewise do not provide durable attempt inspection.

A future narrow command could be added under `hh`, such as bounded reliability
inspection and workflow-specific reconcile commands, without creating a
generic admin framework. The first useful read capability is a bounded
`ListUnresolved(limit)`-style query; application may additionally need
`ListByVacancy(vacancyID)`, and auto-chat may need
`FindByConversation(conversationID)`. Do not add these in R14.5.

# Retention

JSON/Postgres attempt stores currently accumulate durable records; terminal
history is not pruned. No current count/retention policy is encoded. This is
acceptable for the audit but is a future operational concern.

Never age-delete `SENDING`, `ACCEPTED`, `DELIVERY_UNCERTAIN`, or target-
confirmed records merely to reduce storage. They are blocking safety history.
`REJECTED`/`NOT_SENT` could be archived only under a future retention contract
that preserves auditability and replay semantics. No TTL deletion or cleanup
is recommended now.

# Vocabulary

Recommended plain labels:

| Technical state | Recommended label |
|---|---|
| `SENDING` | “Ожидает проверки: исход отправки неизвестен” |
| `ACCEPTED` | “HH принял запрос; доставка не подтверждена” |
| `DELIVERY_UNCERTAIN` | “Возможно отправлено; повторная отправка запрещена” |
| `TARGET_RESPONSE_CONFIRMED` | “Отклик подтверждён на HH” |
| `TARGET_REPLY_CONFIRMED` | “Ответ в диалоге подтверждён на HH” |
| `TARGET_LEAVE_CONFIRMED` | “Выход из диалога подтверждён на HH” |
| `NOT_SENT` | “Не отправлено: writer не вызывался” |
| `REJECTED` | “Запрос отклонён HH; новая попытка — отдельное действие” |
| `manual_review` | “Требуется ручная проверка” |
| persistence uncertainty | “Эффект мог произойти; локальная фиксация неполна” |

Misleading or insufficient current labels:

- `internal/runtime/runtime.go` emits generic `application_error` for
  automatic outcomes, including uncertainty, without AttemptID, state,
  provider ID, evidence, or persistence uncertainty. It is not a sufficient
  operator label.
- The automatic/legacy event projections use generic application/chat error
  names. Auto-chat error context carries more state than the success event,
  but the dashboard does not project it.
- `web/app.js` has “Failure status”/“HH write failed” and “Send failed before
  HH request”/“Send failed after HH request” for controlled-chat branches.
  The before/after distinction is useful there, but those labels must not be
  reused for unresolved application or auto-chat attempts.
- Existing “Доставка не определена” is directionally correct, but future
  screens must add target identity, attempt identity, and the no-retry rule.

Avoid “Failed — retry” and “Not sent” for `DELIVERY_UNCERTAIN`.

# Priority ranking

1. **Automatic application unresolved-attempt read model.** Highest unsafe
   write risk and business impact: the block is durable, but the operator
   cannot inspect AttemptID, target, or evidence.
2. **Legacy auto-chat unresolved-attempt read model.** Duplicate reply/leave
   risk and poor post-restart explanation; M2 safety already works, so the
   immediate need is visibility and eventual read-only evidence.
3. **Store-failure visibility.** Application failures are only partly visible;
   auto-chat per-item store failures can be hidden by discarded results.
4. **Controlled-chat projection and notification completeness.** The safety
   model is strong, but persistence/audit/notification errors are not uniformly
   visible in one operator view.
5. **Bounded listing, retention, and maintenance summaries.** Important for
   operability, but lower duplicate-write risk; maintenance remains safely
   best-effort.

# Recommended R14 implementation stages

R14.5a — Reliability Read Models / Unresolved Attempt Inspection

Add bounded, workflow-specific read adapters and dashboard/CLI inspection for
application and auto-chat attempts. Surface existing fields and store health;
do not add a mutation or migration unless a later implementation proves a
missing field is necessary.

R14.5b — Read-only Operator Reconciliation

Expose the existing application reconciliation and controlled-chat
reconciliation as explicit read-only operations. Specify and implement a
separate bounded auto-chat history evidence contract before exposing auto-chat
reconciliation. No writer capability, reset, or release.

R14.5c — Local Review/Audit Annotations

Only if operators need them after 5a/5b, add idempotent local
`ACKNOWLEDGE`/`MARK_REVIEWED` annotations. Record operator action, target ID,
timestamp, previous state, and evidence; never change the external-effect
claim or blocking gate.

R14.5d — Notifications and Operator UX

Project application/auto-chat uncertainty, store-unavailable, confirmed, and
conflict outcomes into advisory notifications and clear labels. Notification
resolution must remain separate from attempt resolution.

R14.5e — Explicit New Action Safety, only if business need remains

If a repeat is required, provide a high-friction “Create new action” flow
linked to existing manual application or controlled-chat workflows. It must
create a fresh ID and pass fresh reads/preflight and all normal gates. Do not
add a retry button.

The smallest justified next stage is **R14.5a**. It should not be started as
part of this audit.

# Retry safety

HH WRITE RETRY:
**NONE**

No unresolved application, auto-chat, controlled-chat, or leave state may
directly transition into an automatic resend. A read-only reconciliation is
not a retry authorization. `REJECTED` and `NOT_SENT` are already handled by
normal future policy and need no operator retry control.

# Verification

The following commands are the required R14.5 verification set. They are
read-only checks against the current worktree; Docker is not applicable.

| Check | Result |
|---|---|
| `gofmt -l .` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| Docker | NOT APPLICABLE |
| LIVE HH WRITES | 0 |

# R14 status

R14.5: **AUDIT COMPLETE**

No production code, schema, CLI mutation, dashboard control, retry, reset,
or scheduler change was made.

# Ready

Exact next stage: **R14.5a — Reliability Read Models / Unresolved Attempt
Inspection**.

Do not start it as part of R14.5.
