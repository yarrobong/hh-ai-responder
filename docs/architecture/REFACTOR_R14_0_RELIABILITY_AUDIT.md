# Executive summary

Highest reliability risk: **automatic vacancy application cross-run ambiguity**.

Why:

- The automatic path performs fresh vacancy/test preflight and then calls the HH vacancy-response POST, but it has no durable application-attempt record, attempt ID, nonce, reservation, or application record before dispatch.
- A network error, HTTP 409/5xx, malformed response, or equivalent incomplete evidence is returned as `DELIVERY_UNCERTAIN` / `DELIVERY_UNCERTAIN`-equivalent, but only an `application_error` event is emitted afterward. `writeEvent` ignores the output error and no durable application state is updated.
- On restart, automatic application gating reloads only the separate vacancy-ID `already_responded` JSON state. That state is populated from confirmed read preflight, not from an accepted or ambiguous application POST. The local application repository and later application-list sync are not consulted by `ApplyVacancies` before it decides to dispatch.
- Consequently, a later run can dispatch the same vacancy/resume application again if HH has not yet exposed the prior response, or if the response evidence is unavailable. There is no vacancy-response idempotency key in the current adapter.

The same-run invariant is stronger: search results are deduplicated by vacancy ID and one application item reaches `SubmitVacancyResponse` at most once in an `ApplyVacancies` invocation. There is no automatic same-run resend after 409, 5xx, network failure, malformed evidence, or local event-output failure.

Controlled chat is materially stronger. Approval creates a durable action ID and nonce; the gateway durably reserves `sending` and consumes the nonce before dispatch; terminal/uncertain action states block replay; and targeted history reconciliation matches the exact provider message ID. Its remaining gaps are mostly projection/audit persistence windows and JSON-only action durability, not an equivalent application replay gap.

Other maintenance writes (legacy auto-chat, leave, resume touch, and job-search status) use the direct compatibility writer without durable action identity. Auto-chat can repeat a logically identical reply on a later 15-minute run if provider history has not yet reflected the prior reply. Touch and status are semantically repeatable maintenance operations, but their local outcomes are not durable or reconciled.

No production behavior was changed by R14.0.

# Mutation inventory

The inventory is based on the current composition root and adapters. A vacancy test, when present, is part of the atomic vacancy-response request; it is not a separate HH mutation.

| Mutation | Workflow callers | Durable action identity | Preflight | Transport outcome | Reconciliation | Local projection |
|---|---|---|---|---|---|---|
| Chat send, controlled | Dashboard approved-action `Send` / `HHWriteGateway.Send` | Yes: `ApprovedHHAction.ID`; `SendNonce`; `sending` reservation before transport | Fresh conversation/application/candidate validation and fresh action preflight | `accepted`, `rejected`, `not_sent`, `delivery_uncertain`; post-transport persistence can become `persistence_uncertain` | Yes: targeted conversation history; exact provider message ID; no text fallback | Action store, conversation, application follow-up, draft, audit and pilot observations; several later saves are best effort |
| Chat send, legacy auto-chat | `AutoRespondChats` → `autochatorchestration` → `legacyAutoChatActions.SendChatMessage` | No durable identity; a new UUID idempotency key is generated per call | Fresh chat list/history and write-allowed/history-length checks | Same typed transport outcomes, but no action persistence | None | CLI/event-stream `chat_reply` or `chat_reply_error`; output errors ignored; in-memory `ignoredChats` only |
| Chat leave | Legacy auto-chat discarded-chat branch and direct legacy writer capability | None | Chat list/discard classification only; no leave-specific durable preflight | `accepted`, `rejected`, `not_sent`, `delivery_uncertain`; adapter sends no idempotency key | None targeted | Run result/log only; no durable leave record |
| Vacancy response / atomic test | `ApplyVacancies` → `applicationprocessing` → `applicationsubmission` → legacy writer | None; no attempt ID or nonce; vacancy ID + resume ID are request fields only | Fresh vacancy-response state and, if applicable, fresh test metadata | `accepted`, `rejected`, `not_sent`, `delivery_uncertain`; malformed/uncertain provider evidence is not accepted | None targeted; later general application sync is not wired as submission reconciliation | `application` / `application_error` event stream only; no application/action write at submission time |
| Resume touch | Scheduler every 4h → `TouchResume` → legacy writer | None | Configuration flag and resume hash; no write-specific preflight | 2xx accepted, non-2xx classified, network/response ambiguity | None | Logger/result only; no durable touch attempt |
| Job-search status | Scheduler every 24h → `SetActiveJobSearchStatus` → legacy writer | None | Configuration flag; no current-status read in the write path | 2xx accepted, non-2xx classified, network/response ambiguity | None | Logger/result only; no durable status-write attempt |

The controlled gateway's authoritative action states are the existing values `pending`, `approved`, `sending`, `sent`, `failed`, `cancelled`, `stale`, `sent_unconfirmed`, `delivery_uncertain`, `manual_review`, and `delivery_confirmed`. The extracted gateway result additionally exposes `accepted`, `rejected`, `not_sent`, `delivery_uncertain`, and `persistence_uncertain`. Automatic application exposes `SUBMITTED`, `REJECTED`, `DELIVERY_UNCERTAIN`, and `NOT_SENT` at the use-case boundary, but does not persist those statuses.

## Current outcome classification

| Current outcome/name | Meaning in the current implementation | Durable for automatic application? |
|---|---|---|
| `OutcomeNotSent` / `NOT_SENT` | Validation, disabled-write, policy, or pre-dispatch failure; no writer dispatch | No |
| `OutcomeAccepted` / `SUBMITTED` | Provider returned a successful transport result; for chat, a provider message ID is required; this is not general delivery confirmation | No |
| `OutcomeRejected` / `REJECTED` | Provider/client classified the request as rejected, typically non-409 4xx or explicit HH error | No |
| `OutcomeAmbiguous` / `DELIVERY_UNCERTAIN` | Network error, 409, 5xx, malformed/insufficient response evidence, or other result that cannot prove the write outcome | No |
| `OutcomePersistenceUncertain` | HH write may have happened but gateway post-transport action/audit persistence failed | No application action path |
| `sent_unconfirmed` / `delivery_uncertain` / `manual_review` | Controlled-chat durable states which block automatic replay and require read-only reconciliation | Not used by automatic applications |
| `RECONCILED_SENT` / `delivery_confirmed` | Controlled-chat reconciliation found the exact provider message | Not used by applications |
| `RECONCILED_NOT_SENT` | Controlled reconciliation transition for a non-delivery conclusion where the use case permits it; absence alone is not treated as proof | Not used by applications |

# Failure windows

The following is the current sequence model. “Durable” means persisted in a store that is reloaded by a later process, not merely present in memory or an output stream.

| Window | Controlled chat | Automatic application | Legacy auto-chat / leave / touch / status |
|---|---|---|---|
| A. Before transport dispatch | Action is approved; fresh preflight and request validation run. `Reserve` persists `sending` and nonce use before the writer call. | Fresh vacancy proof and fresh test proof run, but no application attempt/reservation is persisted. | Fresh read/configuration gates run, but no mutation reservation is persisted. |
| B. During transport | One chat writer call. Client/network failure is ambiguous. | One vacancy-response POST containing optional atomic test answers. Client/network failure is ambiguous. | One direct writer call. Client/network failure is ambiguous. |
| C. Provider accepted, client evidence incomplete | Provider ID may be absent/unknown; result becomes uncertain or accepted-but-unconfirmed depending on adapter path. | A 2xx without reliable application evidence, malformed body, or lost response cannot be tied to a local attempt. | A 2xx is normally accepted for non-chat maintenance; auto-chat requires a provider message ID and otherwise becomes uncertain. |
| D. Transport result obtained | Extracted core records action outcome; failure to record turns the result into `persistence_uncertain`. | Result is converted to application status in memory only. | Result is converted to an in-memory orchestration result only. |
| E. Local action state persisted | Core action `Record` is post-transport and checked. Root later updates action/draft again, with additional best-effort saves. | No action state is written. `already_responded` is not updated by a successful/ambiguous POST. | No action state is written. |
| F. Local events/projections persisted | Conversation append and application follow-up projection occur after accepted transport; several saves/audits are ignored. | `application`, `application_error`, preflight, and summary events go to `eventWriter`; encoder errors are ignored. | Auto-chat events go to the same output stream; leave/touch/status have no durable projection. |
| G. Post-write read/reconciliation | Targeted chat read runs after accepted send and can be run manually later. | No targeted vacancy-response reconciliation. Later application-list sync is a separate read/import operation. | No targeted reconciliation. Fresh next-run history/state is only an indirect observation for auto-chat. |
| H. Crash between any two steps | Before reservation: no dispatch. After reservation: restart sees `sending` and does not automatically resend. After transport: action state may be `sent`, `sent_unconfirmed`, `delivery_uncertain`, or `manual_review`, subject to the checked core record and later saves. | No local attempt survives unless an output event happened to be written; restart can consider the same vacancy again. | No logical write attempt survives; next scheduled iteration can call the writer again. |

The highest-risk H window is the automatic-application interval after the vacancy POST may have reached HH and before any durable state that identifies that logical attempt exists. For controlled chat, the comparable post-transport projection windows can lose local read-model information, but the durable action reservation/terminal state is the replay gate.

# Automatic application

## Current sequence

`ApplyVacancies` loads the vacancy-ID `already_responded` JSON set, clears the in-memory preflight cache, fetches and deduplicates vacancies by ID across search profiles, and processes each unique item once. It skips IDs already in that local set, performs deterministic policy checks, and delegates preparation to `applicationprocessing`.

Preparation performs description/AI evaluation, read-only applicability, deterministic requirement reconciliation, and optional test preparation. A preparation preflight event is emitted; only a positive read preflight (`AlreadyRespondedKnown && AlreadyResponded`) is added to the local already-responded set. A prepared item is passed to `applicationsubmission`.

`applicationsubmission` binds the prepared item to the current resume, performs a fresh vacancy-response read immediately before the write, performs a fresh test-metadata comparison when needed, and calls the executor once. The executor constructs a fresh `newLegacyWriteService`, whose `VacancyResponseWriter` is the HH write adapter but whose `Actions` and `Audit` dependencies are nil. The adapter sends the vacancy-response POST with the selected resume, cover letter, and optional atomic test fields.

The result is mapped to `SUBMITTED`, `REJECTED`, `DELIVERY_UNCERTAIN`, or `NOT_SENT`. The root then increments the accepted application counter and writes an `application` event only for a submitted result; errors write `application_error`. Neither event is an application attempt record.

## Same-run ambiguity

**SAME-RUN AUTOMATIC RESEND = NONE.**

The search-profile fetch deduplicates by vacancy ID. The application loop has no retry branch after the single `applyPreparedApplication` call. 409, 5xx, network failure, malformed success evidence, and local event-output failure all exit that item without another dispatch. The `applicationsInRun` counter is incremented only after a submitted result; an ambiguous attempt therefore does not consume that counter, but it also does not trigger a same-run resend. `maxVacanciesPerRun` and the separate `maxApplicationsPerRun` policy remain the current loop controls.

## Cross-run ambiguity

For the requested scenario:

1. Run N passes fresh vacancy and test proof.
2. The vacancy-response POST may reach HH.
3. The client receives a network error, 409, 5xx, malformed evidence, or another incomplete result.
4. The typed result is ambiguous; the root writes an `application_error` event if the output stream accepts it, but no durable action/application attempt is recorded.
5. Run N may continue to other vacancies or exit; there is no recovery operation.
6. Run N+1 reloads only the local vacancy-ID already-responded set for this gate. A prior automatic POST did not populate it.
7. The local `ApplicationStore` and application-list sync are not queried by `ApplyVacancies` to gate this decision.
8. N+1 performs fresh provider applicability. If HH now exposes `already responded`, it safely skips. If the state is unknown, the preflight blocks rather than guessing. If HH still reports an applicable/unresponded vacancy, N+1 can send another POST.

Therefore the current cross-run automatic replay risk is **HIGH**. There is a timing window while HH's response/application projection has not caught up. There is no vacancy POST idempotency key in the adapter, and the repository does not establish the provider's duplicate-response behavior. A provider 409 is deliberately treated as ambiguous, so it does not resolve the local attempt identity.

## Persistence

Answers, cover letter, vacancy ID, and resume ID are carried in the request, but there is no durable logical application attempt before dispatch. Vacancy ID + resume ID identify the target pair, not the logical attempt: they cannot distinguish a first attempt, a retry, an accepted response, or two competing runs.

What survives restart:

- The separate `already_responded` JSON file survives only successfully written confirmed read observations. Its write errors are logged and ignored, and it stores only vacancy IDs.
- `job_applications.json` or PostgreSQL application records survive only if created/imported by another workflow such as HH application-list sync. Automatic submission itself does not create or update one.
- CLI/event output may contain `application` or `application_error`, but `writeEvent` ignores encoder errors and this stream is not the application/action source of truth.
- No attempt nonce, sending state, ambiguous state, or provider correlation is reloaded for automatic applications.

A local event-save failure after an automatic POST loses observability, not just a projection: there is no separate durable action state behind the event. The later run has the same replay decision inputs as if the event had never been emitted.

## Provider evidence

The vacancy preflight reader parses structured/embedded response state and known HTML phrases for already-responded, archived, test, letter, and applicability facts. It is authoritative for blocking when a critical field is known, but “not already responded” is only a current read observation. It may be unavailable, stale, or temporarily fail to reflect a just-accepted response.

The application-list reader can later import negotiations with vacancy ID, external negotiation/topic ID, status, created/updated timestamps, chat ID, and a `delivery_confirmed` marker when the topic is `RESPONSE_BY_APPLICANT`. This is useful moderate-to-strong evidence that a response exists, but it is not a targeted attempt reconciliation: it is not tied to a local attempt ID or resume ID, and `ApplyVacancies` does not use it as its pre-dispatch gate. Absence from the list is absence of evidence, not proof that the POST was not sent.

## Answers to the primary questions

| Question | Current answer |
|---|---|
| Durable logical application-attempt record before dispatch? | **No.** |
| Attempt ID/nonce persisted before write? | **No.** |
| Vacancy ID + resume ID sufficient to identify the logical action? | **No.** They identify a target, not an attempt. |
| What survives process restart? | Only independently persisted local read-model/state files and any output that was successfully written; no application attempt state. |
| What survives ambiguous transport? | In memory until the run ends, possibly an `application_error` output event; no durable application/action state. |
| What survives local event-save failure? | Nothing application-specific. The external effect may exist without a durable local conclusion. |
| What prevents a later scheduled run from sending again? | Only fresh HH applicability if it now exposes the prior response, or a previously persisted positive read preflight vacancy ID. Neither records the ambiguous attempt. |
| Does fresh HH state reliably expose a previous response? | **No.** It can provide useful positive evidence, but propagation, availability, and parser coverage are not guaranteed. |
| Targeted reconciliation method? | **No** vacancy-response reconciliation method exists. |
| Is absence of response evidence treated safely? | It is not treated as “not sent” by a reconciliation layer; however, if fresh applicability reports eligible, the current application loop is permitted to dispatch again. |

# Controlled chat comparison

Controlled flow:

`draft` → explicit approval → action saved as `approved` with action ID and nonce → local stale validation → fresh preflight → `hhwritegateway` → durable `sending` reservation and nonce consumption → one chat writer call → core post-transport action record → local conversation/application projection → targeted read reconciliation → `sent_unconfirmed` or `delivery_confirmed`.

The pre-transport reservation is the key safety property. A restart after reservation sees a non-`approved` action and cannot automatically send it again. An ambiguous or persistence-uncertain action is surfaced as `delivery_uncertain`/`manual_review` and remains blocked until read-only reconciliation or an explicit new action.

The gateway requires an exact provider message ID for a confirmed chat write. Reconciliation checks targeted history using that ID; it does not fall back to text matching. If history is unavailable or the exact ID is absent, delivery remains unconfirmed/uncertain. This is stronger than vacancy application submission because the local attempt identity exists before dispatch and the provider evidence is matched to that identity.

The remaining controlled-chat weakness is projection ordering: after the provider accepts, conversation append, conversation save, application follow-up changes, draft save, audit append, observation append, and later action saves are separate operations, with several errors ignored. The core action record is checked and the pre-transport reservation already prevents replay, but a crash or failed projection can leave a stale conversation/read model or incomplete operator evidence.

| Property | Controlled chat | Vacancy application |
|---|---|---|
| Durable logical action before send | Yes: approved action and pre-dispatch `sending` reservation | No |
| Nonce/idempotency identity | Durable `SendNonce`; provider request carries it | No vacancy idempotency key; no durable attempt nonce |
| Delivery evidence | Exact provider message ID from response and targeted history reconciliation | General preflight/application list only; no attempt correlation |
| Targeted reconciliation | Yes, conversation-specific and read-only | No |
| Replay prevention across restart | Terminal/uncertain/sending action states block the same action | None for an ambiguous POST; depends on later HH state or separate confirmed-ID file |
| Ambiguity resolution | `delivery_confirmed`, `sent_unconfirmed`, `delivery_uncertain`, or manual review | Remains an in-memory/result error; no durable resolution state |

# Legacy auto-chat

Legacy auto-chat deliberately bypasses AIDraft approval. Each eligible reply run reads the current chat list and history, checks write permission and history length, generates a reply, and calls the direct legacy writer. The writer generates a fresh UUID idempotency key for that call, but does not persist it or a logical action before dispatch.

On ambiguity, the orchestration result can be `delivery_uncertain` for the current run, an error event is sent to the output stream, and the chat is added to an in-memory ignore list. The ignore list is not durable. The next scheduler iteration is every 15 minutes and reconstructs eligibility from fresh provider data.

Fresh history can prevent a duplicate if HH now shows the candidate's outgoing message as the last relevant message or otherwise makes the chat fail the awaiting-reply filter. That is an indirect, timing-sensitive replay gate. During eventual-consistency or read-failure windows, the employer message can remain the apparent last message and the next run can generate and send the same logical reply again with a different UUID. This is **HIGH** duplicate risk for a substantive employer reply.

Leave is separate: discarded-chat processing calls the direct leave writer, with no durable logical identity, nonce, or targeted post-leave read. An ambiguous leave can be called again if the provider continues to list the conversation; the external consequence is usually less harmful than a duplicate message, but provider idempotence is not established by this repository.

# Resume touch

The scheduler invokes `TouchResume` every four hours. The method checks configuration and dry-run, then calls the direct `ResumeWriter` with the resume hash. There is no durable action ID, pre-dispatch reservation, provider idempotency key, or post-write read reconciliation. The next 4-hour iteration is independent of the prior result.

The adapter treats a 2xx as accepted and network/response uncertainty as ambiguous. No current read path proves a touch by an exact provider operation ID or a guaranteed `updated_at` transition. Repeating the same touch is normally a low-consequence maintenance operation, so duplicate side-effect risk is **LOW**, but local failure and ambiguity are not retained beyond logs/return values.

# Job-search status

The scheduler invokes `SetActiveJobSearchStatus` every 24 hours. The method sends the fixed `looking_for_offers` state through the direct writer. There is no durable action identity, reservation, idempotency key, post-write local state, or current-status reconciliation in this path.

Setting the same status repeatedly is semantically idempotent in the local intent, so the duplicate external consequence is **LOW**. A network/5xx ambiguity does not affect the next 24-hour dispatch decision, and a local log failure can hide the result. The repository does not prove that a provider read can distinguish the current status or the particular write.

# Chat leave

Leave uses `/chatik/api/leave` with only the conversation ID. The adapter has no idempotency key. A 2xx is accepted without a response body; network errors, 409, and 5xx are ambiguous; other non-2xx results are rejected. No action record is reserved before dispatch and no leave-specific reconciliation is implemented.

The auto-chat discard workflow does not persist an ignored/left state. A fresh chat listing may stop returning a left conversation, but that is indirect provider observability and can be delayed or unavailable. A repeated leave is likely less harmful than a repeated reply, but this codebase does not establish provider idempotence; classify as **MEDIUM** risk until provider behavior is verified.

# Post-write persistence

| Mutation | Persistence after possible provider dispatch | Failure consequence | Severity |
|---|---|---|---|
| Controlled chat send | Core `ActionStore.Record` is attempted after transport and its failure returns `persistence_uncertain`; root then separately appends conversation, updates application follow-up/state, saves conversation/application/action/draft, audits, and records observations, with several ignored errors | Durable action may remain `sending` or otherwise incomplete; projection/audit can be stale. The pre-dispatch reservation normally still blocks replay, but operator evidence and read models can be incomplete | HIGH for state/evidence integrity; lower duplicate risk than application |
| Automatic vacancy response | No action/application persistence. Only `application` success or `application_error` is written to `eventWriter`, and `writeEvent` ignores encoder errors | Provider may have accepted while all local attempt evidence is absent; future run has no replay gate | CRITICAL safety/replay gap |
| Legacy auto-chat reply | `chat_reply`/`chat_reply_error` is emitted through the output stream; audit errors are ignored; in-memory ignore is not durable | Ambiguous reply can be repeated next run; local event may be missing | HIGH |
| Chat leave | No durable post-write state; orchestration result/log only | Repeat behavior depends on fresh provider listing; no local proof of leave | MEDIUM |
| Resume touch | No durable post-write state; scheduler/log only | Ambiguous or accepted touch is forgotten until the next interval | LOW |
| Job-search status | No durable post-write state; scheduler/log only | Operator cannot distinguish accepted/ambiguous after the run; same-state next write remains possible | LOW |

# Crash/restart behavior

| Scenario | Automatic application | Controlled chat | Other direct writes |
|---|---|---|---|
| 1. Crash before dispatch | No attempt exists; item may be retried on restart after normal reads | Approved action remains; no `sending` reservation, so it remains eligible for explicit send | No attempt exists; next scheduler iteration may run |
| 2. Crash after request body sent, before response | No local evidence; next run can POST again if fresh state still says applicable | Durable `sending` reservation remains if it was persisted; same action is not auto-sent again | No local evidence; next interval may repeat |
| 3. Provider accepted, crash before gateway outcome persistence | No gateway outcome persistence exists | Action reservation remains `sending`; reconciliation can inspect the target chat, subject to action store durability | No local evidence |
| 4. Outcome persisted, crash before event/projection | No outcome persistence exists | Core action state can survive while conversation/application/audit projection is stale | No outcome persistence exists |
| 5. Accepted response before read sync | Application is only visible through future vacancy/app-list reads; no local attempt correlation | Targeted chat reconciliation may confirm exact message; general sync is not required for action identity | No operation-specific read confirmation |
| 6. Restart before provider read model reflects mutation | Fresh vacancy preflight may still report eligible and allow a repeat application | Action state blocks resend even if history has not caught up; later reconciliation remains read-only | Next interval can repeat direct mutation |

For controlled chat, uncertain actions remain visible in `hh_write_actions.json` when action persistence succeeded, and the dashboard exposes reconciliation/manual-review states. For automatic applications, uncertain actions are not reloaded because none are stored. Auto-chat, touch, status, and leave likewise have no durable uncertain action state.

# Provider evidence matrix

| Mutation | Evidence | Strength | Can prove sent | Can prove not sent |
|---|---|---|---|---|
| Chat send | Successful response with exact provider message ID; targeted history contains that exact ID with matching conversation/direction | Strong positive; `delivery_confirmed` when targeted match succeeds | Yes, for that message ID | No from missing history alone; explicit pre-dispatch validation/rejection is only transport-negative evidence |
| Chat send | History read unavailable, empty, or lacks exact ID | Absence of evidence / weak negative | No | No |
| Vacancy response | Fresh vacancy-response page says already responded or response action unavailable; known HH phrases/flags | Moderate-to-strong positive for a prior response, but not a local-attempt match | Usually proves a response exists, not which attempt/resume caused it | No from “not already responded” at one read; it can be stale |
| Vacancy response | Application/negotiation list has vacancy ID, negotiation/topic ID, response-by-applicant marker, status and timestamps | Moderate-to-strong positive after sync; exact resume/attempt identity unavailable | Often proves a provider application/negotiation exists | No from omission or incomplete pagination |
| Vacancy response | No application list entry or fresh page still says applicable | Absence of evidence / stale-read risk | No | No |
| Resume touch | HTTP 2xx response only | Weak positive transport evidence | Confirms server accepted the request, not durable touch state | No reliable provider read proof |
| Job-search status | HTTP 2xx response only | Weak positive transport evidence | Confirms request response, not the particular persisted transition | No reliable provider read proof |
| Chat leave | HTTP 2xx response; later chat disappearance if observed | Weak positive; disappearance is indirect | No exact leave receipt is exposed | No reliable negative proof |

The adapter's explicit rejected classifications (`OutcomeRejected`) are negative transport outcomes for the request, but the repository does not add an independent provider-side proof that no side effect occurred. In every read path, absence must remain `ABSENCE_OF_EVIDENCE`, not `DEFINITELY_NOT_SENT`, unless a future provider contract establishes stronger semantics.

# JSON/Postgres durability

Current durability is not symmetric across the two storage modes for write reliability:

- Controlled HH action and HH write audit stores are runtime JSON files (`hh_write_actions.json` and `hh_write_events.json`) with private atomic file writes and process/file locking. No PostgreSQL action/nonce table is present.
- The JSON application repository (`job_applications.json`) stores application values and application events with atomic writes, but automatic application submission does not write it.
- The PostgreSQL application repository stores applications, application events, HH metadata, and reconciliation evidence in `applications`/`application_events`; migrations contain no application-attempt/action/nonce relation. Automatic submission does not write these tables either.
- `already_responded` is a separate JSON file in either backend mode and stores only vacancy IDs. Its save errors are logged and ignored.
- HH read sync can import provider negotiations into JSON or PostgreSQL application storage, but import is a read-model operation and is not an atomic companion to the preceding application POST.
- Output events written by the root runtime are not equivalent to either application repository or action store. They are stream output and may be stdout; encoder failures are ignored.

Therefore a future reliability change cannot assume that adding state to the JSON action store alone provides PostgreSQL parity. The current application gap exists in both modes, while controlled-chat action durability is currently JSON-only.

# Duplicate-risk matrix

| Mutation | Duplicate consequence | Current cross-run protection | Risk |
|---|---|---|---|
| Automatic vacancy response | Duplicate application, possible duplicate test submission/cover letter processing, incorrect application counts, operator uncertainty | Fresh provider preflight or previously confirmed vacancy-ID file only; no attempt identity, no application-store gate, no provider idempotency key | **CRITICAL** |
| Controlled chat reply | Duplicate employer message | Durable action ID/nonce, pre-dispatch reservation, terminal/uncertain states, exact-message reconciliation | **LOW** |
| Legacy auto-chat reply | Duplicate employer reply on later 15m run | Fresh history and last-sender/text filters only; new ephemeral UUID each run | **HIGH** |
| Chat leave | Repeated leave request or inconsistent local interpretation of conversation state | Provider listing may eventually stop showing chat; no durable leave state | **MEDIUM** |
| Resume touch | Repeated maintenance touch; primarily timestamp/ranking churn | None; next 4h scheduler iteration is independent | **LOW** |
| Job-search status | Repeated setting of the same status | None; next 24h iteration repeats | **LOW** |

# Observability

Controlled chat is **CLEAR/PARTIAL**: action statuses `sent_unconfirmed`, `delivery_uncertain`, `manual_review`, and `delivery_confirmed` are represented in the dashboard, with action history, nonce-use information, external message ID when available, and a read-only reconciliation control. Audit or notification persistence can still fail. The UI's delivery-uncertain notification is derived from HH write audit events; it is not an independent source of action truth.

Automatic application ambiguity is **PARTIAL to MISLEADING**: the CLI/event stream can show `application_error`, but there is no dedicated durable application-attempt state, no provider ID, no ambiguity status in the application repository, and no automatic notification tied to the uncertain write. A later application-list sync can show a negotiation, but it is not visibly linked to the ambiguous attempt and does not itself update the automatic-application gate.

Legacy auto-chat ambiguity is **PARTIAL** for the current run (`delivery_uncertain` item and error event) but not durable across restart. Leave, touch, and job-status ambiguity is **NOT EXPOSED** as durable workflow state; it is limited to return values and logs.

Notification failure must be separated from action-state failure. If a notification save or quality-log write fails, the operator may not see an alert, but a successfully persisted controlled-chat action state still governs replay prevention. For automatic applications there is no durable action state underneath the notification/event, so losing the event removes both the main local evidence and the operator signal. No notification failure itself authorizes or triggers a retry.

# Retry audit

HH WRITE RETRY:
**NONE**

The HH adapter marks ambiguous transport as non-retryable. There is no repository-wide automatic HH write retry or retry queue. Safe read operations and AI attempts are separate concerns. The scheduler's next iteration is not a same-action retry mechanism, but it can become an unintended replay for operations without durable identity (especially automatic applications and legacy auto-chat). Controlled-chat manual reconciliation is read-only; a later explicit new action is a new user-controlled action, not an automatic write retry.

# Application counters and limits

Automatic application counters are process-local: `applicationsInRun` increases after a submitted application and is used by the application preparation limit; an ambiguous/rejected/not-sent result does not increase it. `maxVacanciesPerRun` counts eligible vacancy-loop entries. These counters are not persisted and do not gate a later process.

The extracted HH gateway has `MaxWritesPerRun` and `MaxWritesPerDay`, with the daily count based on durable `send_started` audit events. That configured action gateway is used by controlled dashboard chat. `newLegacyWriteService`, used by automatic application, legacy auto-chat, leave, touch, and job status, is constructed without `Actions`/`Audit` and without the configured HH write limit values. Thus the HH gateway limits do not provide a cross-run budget for the automatic application path. An ambiguous automatic application does not consume a persisted future application budget; its only current effects are process-local counters, logs/events if they succeed, and whatever HH itself records.

# Event store authority

Current local application events and root output events are audit/read-model material, not attempt identity and not authorization for a future HH write. The controlled action store is the replay gate for approved chat actions; its audit store supports evidence and daily action counting but is not by itself the action source of truth. The application event store is used by application repositories/read models and imported sync history, but no automatic application submission event is atomically coupled to the HH POST. Persisting an event therefore must not be treated as proof that the external side effect did or did not occur.

# Recommended R14 stages

These are recommendations only; R14.1 was not started.

## R14.1 — Durable automatic application attempt reservation

Add the smallest durable application-attempt identity and pre-dispatch reservation around the atomic vacancy-response operation. It must bind at least vacancy ID, resume ID, logical attempt ID/nonce, and lifecycle/outcome, and must be available in both JSON and PostgreSQL modes (or explicitly prevent unsupported mixed-mode operation). The reservation must fail closed before transport and survive restart.

## R14.2 — Vacancy-response reconciliation and evidence model

Use the existing vacancy-response and application-list reads to reconcile an uncertain attempt without sending again. Store the evidence strength and preserve `absence of evidence` as unresolved. Prefer exact provider response/application identity where HH exposes it; do not infer resume or attempt identity that HH cannot prove.

## R14.3 — Cross-run application gating and counters

Make unresolved application attempts visible to the next scheduled run, block automatic resend until reconciliation/manual decision, and define whether an ambiguous attempt consumes the application budget. Keep same-run single-dispatch behavior and daily/per-run semantics explicit.

## R14.4 — Legacy direct-write convergence and projection durability

After the application path is safe, decide whether legacy auto-chat, leave, touch, and status should receive narrow durable identities/projections or remain explicitly best-effort. At minimum, make auto-chat replay prevention and leave ambiguity policy explicit, and align JSON/PostgreSQL durability where state is intended to be authoritative.

## R14.5 — Operator and notification completeness

Expose automatic application ambiguity, persistence uncertainty, and reconciliation outcomes in the same clear vocabulary as controlled chat. Ensure notification loss is observable without making notifications the replay authority.

# Verification

The R14.0 audit was performed with live HH writes disabled; no HH write call was intentionally executed.

Required verification commands:

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
| Docker | NOT APPLICABLE / NOT SUPPORTED |
| LIVE HH WRITES | 0 |

Pre-audit `go list ./...` completed successfully. The canonical executable is `./cmd/hh-ai-responder`.

Relevant inspected areas:

- `internal/ports/hhwrite` — mutation capabilities and transport outcomes;
- `internal/usecase/hhwritegateway` — typed gateway, action reservation, limits, and persistence-uncertain outcome;
- `internal/usecase/applicationsubmission` and `internal/usecase/applicationprocessing` — automatic application choreography;
- `internal/runtime/hh_write_gateway.go`, `hh_write_gateway_core.go`, and `hh_write_reconcile_compat.go` — controlled action store, JSON audit, reservation, projection, and chat reconciliation;
- `internal/adapters/hh/write` — actual HH mutation endpoints and response classification;
- `internal/adapters/hh/read`, `internal/usecase/hhreadsync`, and `internal/runtime/hh_read_sync.go` — vacancy/application/chat read evidence;
- `internal/runtime/already_responded_state.go` and `application_processing.go` — automatic application gating and event output;
- JSON/PostgreSQL application repositories and migrations — current storage parity;
- `internal/runtime/recurring_scheduler.go` and `autochat_orchestration_compat.go` — scheduled direct-write callers.

# R14 status

R14.0: **AUDIT COMPLETE**

No production Go behavior, schema, action state, retry, reconciliation, or dashboard behavior was changed.

# Ready

Exact next stage: **R14.1 — Durable automatic application attempt reservation**.

Do not start it as part of R14.0.
