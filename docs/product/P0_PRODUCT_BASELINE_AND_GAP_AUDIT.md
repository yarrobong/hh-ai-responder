# Executive summary

Current product maturity: **guarded operational prototype / partial Career Agent**.

The repository has a credible safety foundation and a surprisingly broad product
surface: vacancy ingestion, deterministic matching, typed AI assistance, an
Inbox, Today, follow-up eligibility, Candidate Knowledge, reliability views, and
a controlled HH write path. PostgreSQL is the accepted runtime source of truth
for the principal career entities. The product is not yet a dependable daily
job-search command center because the strongest user loop is fragmented:
vacancies are collected but not consistently analyzed or ranked, preparation is
mostly an internal capability rather than a first-class review flow, and the
application/conversation relationship is incomplete in the current PostgreSQL
snapshot.

Biggest strength: **truthful, fail-closed human control around employer and HH
actions**. Draft, approval, fresh preflight, send, and reconciliation are
separate states; unknown candidate facts remain unknown; dry-run and disabled
write capability block HH mutation. The Inbox and conversation assistant make
this safety model visible to an operator.

Biggest weakness: **the product does not yet produce a trustworthy, fresh,
ordered set of new vacancies and carry a selected vacancy cleanly through
reviewable application preparation**. The dashboard can list and explain stored
vacancies, but it does not give the candidate a clear “new jobs to consider
today” queue with an end-to-end preparation handoff.

Recommended P1: **Vacancy Discovery & Ranking**. Make the existing search and
matching capability produce a fresh, deduplicated, ranked candidate shortlist
with explicit fit bands, decision reasons, freshness, and already-applied
state. This has the highest leverage because it improves the first step of the
journey for every search run and creates the input for a later Application
Review & Preparation stage.

This audit is product-only. No production code, schema, prompts, semantic
scope, migrations, reindexing, or HH writes were changed or run. The existing
dirty worktree was preserved.


# Current product map

The following describes the current surfaces as they are implemented, not just
the underlying packages. “PostgreSQL” means the surface can use the configured
PostgreSQL backend; some auxiliary operator state (notifications, drafts,
quality logs, write-action history, and compatibility audit data) remains
file-backed or mixed. That distinction matters to the user because a page can
look current while a related CLI report reads a different snapshot.

| Workflow | What the user/operator can do today | Entry point | PostgreSQL data | AI | Semantic retrieval | Actionability | Mode |
|---|---|---|---|---|---|---|---|
| Dashboard / Overview | See counts, sync state, Inbox focus, notifications, workflow KPIs, and AI/knowledge queues | Dashboard | Yes for core entities; mixed auxiliary state | Optional | Indirect | Moderate | Assisted, read-oriented |
| Today | See needs-reply, needs-action, interviews/external tests, follow-ups, changes, and notifications | Dashboard | Yes, through dashboard projections | Optional drafts | No direct retrieval | Moderate | Assisted/read-only |
| Vacancy discovery | Sync vacancy search results, page through search profiles, deduplicate by vacancy ID, retain vacancy records | CLI, dashboard Sync, scheduler | Yes in configured runtime | Not required for ingestion | No | Low until analyzed | Read/sync |
| Vacancy filtering | Search by text; filter recommendation and score range in dashboard; configure include/exclude/search URLs through environment/config | Dashboard, CLI/config | Yes | No | No | Moderate but manual | Assisted |
| Vacancy scoring/matching | Inspect score, recommendation, matched skills, missing skills, unknown skills, risks, and reason | Dashboard vacancy detail, application pipeline, internal service | Yes for stored results | Typed AI is advisory in application analysis; deterministic Go policy is authoritative | Limited/optional | High for analyzed records | Assisted |
| Pilot shortlist | Rank up to five suitable conversations for a controlled first reply pilot; inspect the last employer message and reasons | CLI `hh pilot-*`, dashboard/API | Yes for conversations/applications | No generation in shortlist | No | Moderate for conversation pilot selection | Read-only |
| Vacancy analysis | Analyze candidate/vacancy fit and produce `MATCH`, `REVIEW_REQUIRED`, or `REJECT` inputs to the application pipeline | Scheduler, CLI/runtime, internal service | Yes | Yes when configured | Optional semantic hints in the application-preparation composition; not a required part of root matching | High in principle; low coverage today | Assisted/read-only |
| Application preparation | Build an in-memory prepared application, cover-letter draft, possible test-answer preparation, candidate-input outcome, or safe skip | Internal application-processing path; limited dashboard exposure after an application exists | Reads PostgreSQL; local drafts/clarifications may be persisted | Yes | Cover-letter path can retrieve safe candidate context | High in principle | Assisted/internal |
| Cover letters | Generate, edit, save, approve, reject, and inspect facts used when linked to an application/conversation draft | Dashboard assistant for linked records; internal AI orchestrator | Mixed | Yes | Useful where the cover-letter path invokes retriever; not used as truth | Moderate | Assisted, manual approval |
| Test-answer preparation | Read a current test snapshot and prepare/validate answers without submission when the path is reached | Internal application-preparation path; preview-oriented CLI/runtime | Reads source data; no HH write | Yes | No material evidence | Low in normal UX | Assisted/internal |
| Application history | Browse applications, statuses, timeline, match display, vacancy, conversation link if available, follow-up state | Dashboard, CLI sync/status flows | Yes | Optional linked assistant | Indirect | Moderate | Assisted/read-only |
| Inbox | Group conversations into reply, action, waiting, interview/external action, and no-action sections; open a conversation | Dashboard, CLI `hh inbox`, Career monitor | Yes for conversations; current linkage is incomplete | Optional drafts/classification | No material evidence | High for visible linked records | Assisted |
| Employer reply drafts | Select a conversation, generate a draft from safe context, edit/copy/reject/approve exact text, and inspect warnings and facts used | Dashboard, CLI `hh draft`, internal orchestrator | Yes/mixed | Yes | Limited; not a strong reply-ranking feature | High for safe manual replies | Assisted, manual approval |
| Auto-chat review | Run one auto-chat iteration, classify reply/leave/no-reply/manual-review outcomes, and preview what would happen | Scheduler, CLI/runtime, reliability dashboard | Attempts and reconciliation are PostgreSQL-backed where configured; preview/audit is mixed | Yes for reply proposal | No material evidence | Low for ordinary user; useful for operator | Review-only under current config |
| Follow-ups | Evaluate eligibility, view reason/cooldown/exclusions, generate a draft, or dismiss | Dashboard/API, Career snapshot, CLI diagnostics | Uses applications/conversations; drafts mixed | Yes for draft generation | No | Moderate; no end-to-end send | Assisted/read model |
| Career monitor | Refresh/read conversations, classify workflow, create notifications, and run bounded or recurring monitoring | CLI `monitor`, scheduler, dashboard sync | Yes for core reads | Optional | No material evidence | Moderate | Read/assisted |
| Candidate Knowledge | Inspect skills, projects, achievements, stories, unknowns, source/truth status, confidence, and proposals | Dashboard, CLI `profile` / `candidate` commands | Yes in PostgreSQL mode; legacy JSON path remains | AI can interpret answers/propose facts | Three project documents are indexed | Moderate | Assisted, user-confirmed mutation |
| Candidate clarification | Answer a question tied to an unknown; retain answer as evidence; review a generated proposal separately | Dashboard Knowledge questions, internal/API, CLI proposal commands | Yes in PostgreSQL mode | Yes for interpretation | Possible projection after confirmed mutation | High but low-volume | Assisted, explicit confirmation |
| Notifications | View, acknowledge, resolve, dismiss, snooze, open related item, and mark irrelevant | Dashboard/API, generated by Career/reliability flows | Mixed auxiliary store | No required AI | No | Moderate; historical volume reduces focus | Assisted |
| Reliability/operator tools | Inspect application/autochat attempts, lifecycle, blockers, write capability, and perform read-only reconciliation | Dashboard, CLI/API diagnostics | Yes for attempt authority; action/audit views mixed | No | No | High for safety/operator trust | Read-only |
| Scheduler controls | Configure cadence and enable/disable auto-apply, auto-chat, resume touch, job status, follow-up limits, and run-once behavior | Environment/config, CLI; no dashboard control plane | Reads/writes through configured runtime | Depends on job | No material evidence | Low for candidate; moderate for operator | Automated but gated |


# End-to-end user journey

| Transition | Classification | Current reality |
|---|---|---|
| Find vacancy | **PARTIAL** | HH search profiles, pagination, deduplication, and sync exist, but the normal user does not receive a reliable fresh ranked “new for me” queue. |
| Understand vacancy | **PARTIAL** | Vacancy detail contains description, structured metadata, requirements, score, matches, gaps, unknowns, risks, and reason, but only a minority of stored vacancies have analysis. |
| Decide suitability | **TECHNICALLY PRESENT BUT POOR UX** | Deterministic policy and `apply`/`maybe`/`skip` recommendations exist; the user lacks a strong ordered review layer and stage-specific explanation of why a decision was reached. |
| Prepare application | **PARTIAL** | The application-processing service can reach `PREPARED`, `NEEDS_CANDIDATE_INPUT`, or safe skip, but preparation is not a dedicated vacancy-first dashboard workflow. |
| Apply | **TECHNICALLY PRESENT BUT POOR UX** | Controlled application submission has a gateway, approval, preflight, nonce, send, failure, and reconciliation lifecycle. The user path starts awkwardly because selecting a vacancy and opening a complete preparation package is not a clear first-class flow; live writes are intentionally disabled in this P0. |
| Track response | **PARTIAL** | Applications, events, statuses, Inbox, Today, and analytics exist, but current PostgreSQL conversation records are not linked to application records, weakening continuity. |
| Answer employer | **PARTIAL** | Reply classification, safe drafting, candidate clarifications, exact-text approval, and preflight are strong. Coverage and visibility are reduced by manual-review volume and incomplete application/conversation joins. |
| Follow up | **PARTIAL** | Eligibility and draft generation are usable and conservative; there is no final end-to-end send workflow in the product’s normal daily loop. |
| Update Candidate Knowledge | **PARTIAL** | Unknown → question → answer → proposal → confirmation is implemented, but the queue is sparse, proposal visibility is not yet a regular daily habit, and current data has unresolved unknowns with no pending proposals. |
| Repeat | **PARTIAL** | Scheduler and Today support repetition, but the repeat loop is operational refresh plus manual inspection rather than a clear daily “new shortlist → decisions → preparation” cycle. |


# Vacancy discovery

Vacancies enter through configured HH search URLs. `HH_SEARCH_URLS` supports
multiple sources separated by `||`, with `HH_SEARCH_URL` as fallback. The
runtime preserves the configured query context, removes an incoming `page`
parameter, adds publication ordering and a seven-day search period, requests up
to 50 items per page, walks subsequent pages until an empty page, and
deduplicates by vacancy ID across all profiles.

The current built-in search profiles are oriented around Python/Django/backend,
automation/integrations/implementation, and support/product support. Include
and exclude keywords are configured for the candidate’s target roles. The
search path itself does not establish that a vacancy is suitable: structured
vacancy fields, deterministic checks, response/already-applied gates, and the
application policy remain separate steps.

The system handles already-responded and archived state conservatively in the
application pipeline. Unknown critical state is review, not permission. Search
freshness is bounded by the configured seven-day query and the time of the last
sync; there is no prominent dashboard concept of “new since last review” or a
candidate decision cursor.

**Can the user open the product and receive a useful ranked set of NEW
vacancies to consider? PARTIAL, currently closer to NO for the normal daily
experience.**

The product can fetch and store plausible results and can filter stored
records, but the dashboard vacancy list is publication-date ordered, not a
dedicated fit-ranked new queue. PostgreSQL currently contains 279 vacancies;
only 20 have match analysis and 259 do not. There is no clear first-class
shortlist action for a vacancy, no visible “unseen/new” state, and no single
action that takes a chosen vacancy into a complete preparation review.


# Matching

The effective pipeline is:

1. Apply the attempt/replay and already-responded gates.
2. Read the vacancy and trusted structured fields.
3. Resolve the canonical Candidate context.
4. Run deterministic checks for role/skills, location/work format, experience,
   salary and explicit exclusions.
5. Optionally run typed AI vacancy analysis and extract requirements as
   advisory evidence.
6. Derive `met`, `missing`, and `unknown` in Go; AI cannot promote an unknown
   hard requirement to met.
7. Apply the production policy and return `MATCH`, `REVIEW_REQUIRED`, or
   `REJECT`.
8. Only a safe match proceeds to read-only applicability/preflight and then
   preparation; no match alone authorizes HH mutation.

The documented V2 production result of `MATCH = 0` was primarily an upstream
policy/gating result: the unchanged production matching policy rejected every
vacancy before application preparation was reached. The acceptance harness
could bypass an initial rejection only in a test-only path to exercise
preparation; that did not change production policy or send anything. The
evidence does not justify blaming semantic retrieval alone or claiming that
Candidate Knowledge is simply insufficient. The more accurate diagnosis is a
combination of strict deterministic policy, query/result quality, unknown hard
requirements, and missing per-stage decision telemetry.

The current stored PostgreSQL analysis snapshot is more nuanced than the
historical zero-MATCH run:

| Bounded current sample | Count |
|---|---:|
| Vacancies with stored match result | 20 |
| `apply` | 1 |
| `maybe` | 14 |
| `skip` | 5 |
| Score 80–100 | 4 |
| Score 65–79 | 9 |
| Score 40–64 | 7 |
| Vacancies with an unknown-skill risk | 11 |
| Vacancies with a location/work-format risk | 6 |

The last two rows are overlapping vacancy counts, not a mutually exclusive
rejection histogram. The current model does not persist a complete,
stage-by-stage rejection reason for every vacancy, so it cannot reliably
separate wrong role, location, experience, salary, hard skill, candidate
unknown, AI reject, and other reasons over the full set. That is a product
quality gap: the operator sees the final explanation for analyzed items, but
cannot explain zero-MATCH runs or prioritize remediation with confidence.

The current applications snapshot contains 98 applications and 0 stored
application-level match results. This is distinct from the 20 vacancy-level
results and makes the application review surface less informative than the
matching surface.


# Ranking

There is a scoring field and three recommendation decisions: `apply`, `maybe`,
and `skip`. The dashboard can filter by recommendation and score range, and
the vacancy view can show the score and explanation. The current list is still
sorted by publication date, not by a product ranking that combines fit,
freshness, novelty, response state, and user review priority.

The distinction between perfect fit, good fit, worth reviewing, low fit, and
hard reject is therefore only partially expressed. In practice the user gets
a binary-ish action decision plus a middle `maybe` bucket, rather than a
ranked work queue with explicit fit bands and a clear reason to inspect item 1
before item 20.

Ranking is a missing product layer, not a reason to change the accepted
threshold or semantic scope.


# Vacancy explanation

For analyzed vacancies, the user-facing vacancy panel exposes:

- score and recommendation;
- explanation and reason;
- confirmed matches;
- missing skills;
- unknown skills;
- risks.

This is materially useful and follows the project’s truthfulness model. It
separates known matches from missing and unknown information instead of
treating absence from the resume as proof of absence. Structured HH facts are
preferred where available.

The remaining problem is presentation and coverage. Machine policy fields and
user explanation are closely adjacent; the user does not get a compact answer
to “why apply?”, “why review?”, or “why skip?” across a ranked queue. Salary,
location, and work-format implications are present as risks when detected, but
not elevated into a consistent decision summary. Unanalyzed vacancies simply
lack this explanation.


# Application preparation

The accepted preparation composition can produce an in-memory `PREPARED`
result, or a safe `NEEDS_CANDIDATE_INPUT` / skipped result. It can use the
canonical Candidate, structured applicability, typed vacancy analysis,
cover-letter generation and validation, and test-answer preparation when a
current test snapshot exists. Preparation ends before reservation, mutation, or
HH transport.

What a normal user can currently see is weaker than the backend capability:

- An application detail can show the vacancy, application timeline,
  conversation if linked, match result, candidate context, follow-up, and a
  linked AI assistant.
- A linked draft can be edited, copied, rejected, and approved as exact text.
- Candidate facts used and warnings can be shown for a conversation reply or
  linked draft.
- There is no dedicated vacancy-first “Prepare application” workspace that
  presents the whole package together before an application exists.
- Test answers are not a clearly exposed, normal dashboard review artifact;
  the typed preparation capability is primarily internal/preview-oriented.
- Application-specific questions, unknown facts, and the exact effect of each
  unresolved item are not assembled into one review checklist.

Therefore the user cannot yet comfortably review an application as a single
package. The missing preview information is the complete requirement coverage
map, explicit unknowns and their blockers, exact cover letter, validated test
answers where applicable, candidate facts used, source/provenance, and the
next safe action in one place.


# Application execution UX

The controlled write workflow is conceptually sound:

1. A draft or prepared action is generated locally.
2. The user edits if needed and approves exact text.
3. A fresh read-only preflight checks vacancy availability, existing response,
   test/letter requirements, applicability, and write eligibility.
4. A nonce-bound action becomes `READY_TO_SEND` only when the gateway allows it.
5. The user explicitly triggers Send.
6. The lifecycle records sent, delivery-confirmed, failed, stale, or uncertain
   states.
7. Ambiguous delivery is not retried automatically; the user can run
   read-only reconciliation.

The dashboard makes this lifecycle clearest for employer replies. It labels
write capability, shows the exact approved text, last preflight, facts used,
warnings, action history, and safe blocked state. In the current P0, the Send
button is intentionally blocked by `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`.

UX quality is **good at the safety boundary, poor-to-moderate at the product
entry point**. The candidate has no obvious “select this vacancy → prepare →
review → preflight” path from the vacancy list. The execution surface also
inherits the incomplete application/conversation linkage, so the user may
need to navigate separate records to reconstruct context.


# Inbox

The Inbox is one of the most complete current surfaces. It groups items into
needs reply, needs candidate action/clarification, waiting for employer,
interview or external action, and no-action/terminal areas. Conversation detail
adds workflow state, what is happening, what to do, waiting and external-action
cards, diagnostics, message history, memory, AI assistant, and a controlled
reply pilot indicator.

It also exposes classification feedback and an irrelevant/no-attention action,
which creates a useful path for correcting workflow classification.

The main weakness is signal quality and continuity. In the current PostgreSQL
snapshot, 259 conversations exist and 167 are in `manual_review`; all 259 have
no application link. The Inbox can still operate as a conversation queue, but
the candidate cannot reliably move from an application record to its complete
conversation history and vice versa. The user-facing list also mixes actions,
technical state, feedback controls, and raw latest-message content in one card.
This is useful for an operator who understands the system, but not yet a calm
candidate command center.


# Employer replies

The user opens a conversation from Inbox or a linked application, sees the
latest employer message and conversation memory, and can generate an AI draft.
The reply workflow loads the safe Candidate context, checks unresolved and
inconsistent facts, and can create a candidate clarification instead of
guessing. High-risk topics such as salary, relocation, scheduling, documents,
contracts, banking, credentials, and suspicious links/software remain manual
review.

The user can edit the draft, copy it, give quality feedback, reject it, approve
exact text, run preflight, and—when writes are enabled—send explicitly. The
assistant explains refusal or review through warnings, missing information,
context warnings, used facts, and a decision reason. This is a useful daily
operation for a small number of conversations.

The remaining UX issue is that the workflow is conversation-first rather than
candidate-goal-first. The user sees how to answer a message, but not always how
that answer relates to the application, vacancy fit, pending candidate
decision, or next follow-up. Current PostgreSQL orphaning amplifies this issue.


# Auto-chat

Auto-chat eligibility is deliberately conservative. It requires a valid chat,
readable history, an actionable trigger, an active state, a safe destination,
and no ambiguity or stale/terminal/manual-review blocker. It blocks when the
history is incomplete, the sender/direction is unknown, the conversation is
waiting on the employer or candidate, an external action is required, the
vacancy/application state is unknown or terminal, or a prior attempt is
ambiguous. Durable attempt gates prevent replay and duplicate mutation.

Review mode produces understandable categories in principle: reply prepared,
leave recommended, no reply, candidate input required, skipped, or manual
review. It records why the action would not run, and dry-run/write-disabled
state prevents transport. Current configuration has `HH_AUTO_CHAT=false`,
`HH_CHAT_MODE=off`, and writes disabled, so nothing can be left unattended as a
live chat sender in P0.

Trust is high at the safety boundary but moderate in the product view. The
dashboard primarily exposes reliability and conversation review; the full
auto-chat decision stream is more operator/CLI-oriented than a normal user
workflow. “Would reply”, “would leave”, “no action”, and “needs my answer” need
to be presented as a compact queue with clear next actions before this can be
left running confidently.


# Follow-ups

An application becomes eligible only when the application/vacancy relation is
usable, the application is confirmed as sent/active, the employer is the
waiting party, the application date is known, the waiting interval has elapsed,
and no exclusion applies. Current defaults are approximately five days after
application, three days after a candidate message, a maximum of two follow-ups,
and a three-day minimum interval. Dismissed, manual-review, terminal, rejected,
offer, interview, candidate-action, commitment, stale, and otherwise uncertain
states are excluded.

The user sees eligibility, waiting age, reason, previous count, warnings, and
next eligible date. The user can generate a local AI draft or dismiss the
suggestion. A recent accepted bounded run demonstrated a non-empty eligible
pool; the local validation snapshot recorded six eligible items.

This is an actionable **read model plus draft action**, not a complete
follow-up pipeline. There is no normal user-facing final send path, no
automatic follow-up sender, and no single queue that combines due follow-ups
with all other candidate actions.


# Today

Today is a credible candidate homepage in structure. It contains:

- a summary and last-refresh time;
- needs-reply queue;
- needs-action queue;
- interviews and external tests/actions;
- follow-ups for today;
- new important changes;
- notification cards;
- links to Inbox and refresh.

It can function as a daily command center for employer conversations. It is
not yet the command center for the whole job search because new vacancies and
application-preparation work are absent from its primary action queue. The
candidate must leave Today for Vacancies, manually filter a publication-date
list, and reconstruct fit/preparation state.

Informational noise comes from technical diagnostics, classification feedback,
historical notifications, and raw conversation cards being displayed near
actionable items. Some sections duplicate Inbox and notification content.
Missing are a ranked new-vacancy block, an explicit “blocked by candidate
input” rollup, application-preparation checklist, stale application queue, and
a single due-date ordering across replies, tests, follow-ups, and new jobs.


# Notifications

Notification categories include new employer messages, candidate action,
clarification required, external action, interview detected, manual review,
follow-up available, status changed, and reliability/application/autochat
events. The system records lifecycle and supports acknowledge/seen,
dismiss/resolve, snooze, open, and irrelevant feedback. Career and reliability
projection paths deduplicate related signals.

The local historical notification snapshot contains 262 records: 129 high,
128 medium, and 5 low priority; 96 are new and 166 resolved. The categories
are valuable, but historical volume and mixed notification types make it hard
to identify the few actions that matter now. The dashboard typically shows only
a small slice and does not provide a unified urgency/deadline ordering. This
is a product signal-to-noise problem, not evidence that the notification
mechanism is unsafe.


# Candidate Knowledge

The Knowledge surface lets the user inspect skills with level/truth status,
projects, achievements/stories, unknowns, proposals, sources, and confidence.
The questions/proposals view makes an important safety distinction explicit:
the user’s answer remains an unconfirmed clarification until a generated
proposal is separately confirmed or rejected.

The current PostgreSQL snapshot contains 22 candidate skills, 3 projects, 7
unknowns needing confirmation, and no pending proposals. The product therefore
has the right concepts and provenance-aware statuses, but the user’s view of
“what the AI knows about me” is still fragmented across cards, questions,
conversation context, and draft facts. Preferences and constraints are not
presented as a first-class review area comparable to skills and projects.

Answering a missing fact is possible through the Knowledge questions page. It
is not yet easy to understand why the question matters to a specific vacancy,
application, or employer message without navigating back to the source.


# Knowledge acquisition

The implemented lifecycle is:

`unknown detected → clarification → candidate answer → AI proposal → explicit
confirmation/rejection → canonical Candidate mutation → optional semantic
projection`

Unknowns can originate from employer questions and candidate-context gaps. The
orchestration preserves raw answer evidence, validates proposal references
against known experience/projects, requires user confirmation for new facts,
and can update canonical skills or stories. Confirmed changes can invalidate a
previous approval through relevant-knowledge hashes, which is a strong safety
property.

The flow is **technically present but productically incomplete** in three ways:

- vacancy-analysis unknowns are not consistently surfaced as a candidate-facing
  question tied to the vacancy review screen;
- application-preparation unknowns and test questions are not assembled in a
  single preparation checklist;
- employer-message unknowns reach Knowledge questions, but the user must move
  between conversation and Knowledge pages and proposals are currently absent
  in the canonical snapshot.

The canonical distinction between confirmed, observed, inferred, and unknown
must remain intact in any future product work.


# Semantic product value

The accepted semantic scope is intentionally small: three project documents in
the active 1024-dimensional PostgreSQL/pgvector space. This P0 does not propose
expanding it.

| Workflow | Assessment | Evidence |
|---|---|---|
| Vacancy analysis | **LIMITED** | The root vacancy decision is deterministic and semantic context is optional/advisory. Accepted runs include `semantic_results=0`; semantic similarity does not decide hard requirements. |
| Application preparation | **USEFUL but narrow** | The cover-letter composition can retrieve safe project context and use it to ground a truthful letter. It is not automatically present for every preparation, and it is not a complete application-ranking signal. |
| Employer replies | **LIMITED / not currently material as a primary feature** | Safe candidate context and conversation facts dominate reply safety. There is no strong evidence that semantic retrieval materially changes everyday reply decisions. |

Semantic retrieval is therefore a supporting personalization capability, not the
highest-impact P1. More semantic documents would not solve the missing new-job
queue, application continuity, or preparation UX.


# Automation

| Job | Cadence/trigger | Inputs and gates | Output | Safe failure | User visibility |
|---|---|---|---|---|---|
| Auto-apply | In-process recurring loop, nominally 12 hours; run-once path also exists | Search results, attempt gate, already-responded state, deterministic/AI policy, preflight, dry-run/write flags, application limit | Preview/prepared result in dry-run; live submission only after all gates and writes enabled | Reject/review/skip; no HH write in dry-run | CLI/runtime logs and dashboard reliability/application views; no clear daily candidate queue |
| Auto-chat | Nominally 15 minutes when enabled | Awaiting chats, readable history, trigger identity, durable attempt gate, mode, dry-run/write flags | Draft/review/leave/no-reply/skip result; live reply/leave only in explicit live mode | Manual review, skip, or durable blocking residue; no replay on ambiguity | Mostly CLI/scheduler and reliability views |
| Career monitor | Run-once bounded CLI or recurring monitor cadence | Conversation reads, local workflow resolver, clarification state, freshness | Conversation state, notifications, follow-up/read-model updates | Read-only errors and bounded reporting | Dashboard Today/Inbox and CLI `monitor` |
| Resume touch | Nominally 4 hours when enabled | Candidate/resume state, touch flags, dry-run/write flags | Resume touch preview or write in live mode | Disabled by current configuration; no live mutation | Configuration/CLI, weak dashboard visibility |
| Job-search status | Nominally 24 hours when enabled | Candidate status, configured status, dry-run/write flags | Status preview or write in live mode | Disabled by current configuration; no live mutation | Configuration/CLI, weak dashboard visibility |

With the current `.env`, `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`,
`HH_RUN_ONCE=true`, auto-chat is disabled, resume touch and job-status writes
are disabled, and auto-apply can only produce a read-only preview. What can be
left running unattended today is therefore **read-only collection, analysis,
monitoring, and preview generation**. Application sends, tests, replies,
leaves, resume touches, and job-status updates still require explicit operator
review and appropriate live configuration.


# Daily operating workflow

The best safe workflow using only current functionality is:

1. Run or open the dashboard and refresh Inbox/known HH data.
2. Open Today and clear needs-reply, needs-action, interview/test, and urgent
   notification items.
3. Open Vacancies, search/filter manually, inspect analyzed items, and compare
   score, recommendation, matches, unknowns, risks, and location/work format.
4. For a serious candidate, move into the available application/preparation
   path or use the CLI/runtime preview; resolve candidate questions before
   treating the result as ready.
5. Review application history and timeline; use the linked conversation where
   available.
6. Open Inbox conversations, generate/edit drafts, and approve only exact text
   grounded in confirmed facts.
7. Inspect follow-up eligibility, generate a draft for due items, and dismiss
   suggestions that should not be pursued.
8. Answer Knowledge questions and separately confirm or reject proposals.
9. Review Health/Reliability when a state is stale, ambiguous, failed, or
   delivery is uncertain.

The workflow becomes awkward at step 3: there is no ranked new-vacancy queue.
It becomes fragmented at step 4: application preparation is not a unified user
workspace. It becomes discontinuous at step 5: the current PostgreSQL
conversation records are not application-linked. It becomes repetitive at
step 9 because Today, Inbox, notifications, and reliability are related but
not one prioritized action list.


# Dashboard / CLI / Scheduler coverage

| Workflow | Dashboard | CLI | Scheduler | Internal only | UX quality |
|---|---|---|---|---|---|
| Dashboard / Today | Yes | No equivalent homepage | Refreshes/projections can feed it | No | Moderate; conversation-heavy |
| Vacancy discovery/sync | Sync controls | `hh sync vacancies` and main runtime | Yes through recurring application/Career flows | No | Moderate for operator, weak for candidate |
| Vacancy filtering | Text/recommendation/score filters | Search/config flags | Uses configured profiles | No | Moderate; no new/unseen filter |
| Vacancy matching | Detail explanation for analyzed records | Analysis through runtime/audit flows | Yes in application processing | No | Moderate per item, poor at queue level |
| Vacancy ranking | Recommendation/score filter only | Pilot ranking is for conversations | No candidate shortlist job | Missing product layer | Low |
| Pilot shortlist | Yes/API | `hh pilot-candidates`, `pilot-shortlist`, `pilot-show` | No | No | Good for controlled reply pilot, not vacancy selection |
| Application preparation | Partial after application/linked data | Runtime/internal preparation path | Auto-apply path can prepare preview | Yes for complete package | Low-to-moderate |
| Cover letter | Partial linked assistant | Internal/preview flows | Auto-apply preparation | Mostly | Moderate for linked draft |
| Test-answer preparation | Not a normal dedicated view | Internal/preview-oriented | Application preparation | Yes | Low |
| Application history | Yes | Sync/status/eligibility flows | Updated by application processing | No | Moderate; linkage gap |
| Inbox | Yes | `hh inbox` | Career monitor refreshes | No | Good structure, noisy/currently disconnected |
| Employer reply draft | Yes | `hh draft` and runtime | Auto-chat may prepare review output | No | Good safety UX, moderate context UX |
| Auto-chat review | Reliability/read views | Runtime/auto-chat CLI | Yes when enabled | Decision internals | Moderate for operator, low for candidate |
| Follow-ups | Yes, draft/dismiss | Diagnostics/read model | Career projection | No | Moderate; read model, not send pipeline |
| Notifications | Yes | Generated/read through monitor tools | Career/reliability create them | No | Moderate; priority/noise issue |
| Candidate Knowledge | Yes | `profile`, `candidate`, semantic commands | Can create gaps/proposals indirectly | No | Moderate; provenance good, source linkage weak |
| Knowledge acquisition | Questions/proposals/answer/confirm | Proposal commands; legacy sync path differs by backend | Triggered by analysis/reply | Orchestration internals | Moderate but fragmented |
| Career monitor | Dashboard output | `monitor --run-once` / recurring command | Yes | No | Moderate |
| Reliability/reconciliation | Yes | Audit/eligibility/reliability commands | Attempt lifecycle feeds it | No | Strong for operator safety |
| Scheduler controls | Status only; no full control plane | Config/flags | In-process loops | No | Low; environment-driven |
| Product audit | Health/deep and lifecycle views | `audit`, `health`, `eligibility-report` | No | No | **BUG/UX:** `audit` reads legacy JSON snapshots even when PostgreSQL is configured |

Backend capabilities effectively hidden from a normal user include the complete
application-preparation package, validated test-answer preparation, detailed
vacancy-search/profile controls, per-stage matching evidence, and the full
auto-chat review decision stream. The CLI is also not a uniform substitute for
the dashboard: some commands are PostgreSQL-aware while the career audit path
still reads JSON compatibility files.


# Product debt

| ID | Type | Area | Problem | Severity |
|---|---|---|---|---|
| BUG-1 | BUG | Audit/source of truth | `audit`/career-audit reads JSON snapshot files directly, while the configured runtime is PostgreSQL; operator reports can disagree with the dashboard and canonical DB. | HIGH |
| BUG-2 | BUG | Inbox interaction | Inbox cards render navigation links around buttons/select controls, creating invalid nested interactive elements and likely awkward click/keyboard behavior. | MEDIUM |
| UX-1 | UX | Discovery | Vacancy list is publication-date ordered with filters, but lacks new/unseen state, shortlist action, fit bands, and a daily review queue. | HIGH |
| UX-2 | UX | Preparation | No vacancy-first application preparation workspace; cover letter, tests, questions, facts, unknowns, preview, and approval are not one review package. | HIGH |
| UX-3 | UX | Daily operation | Today duplicates parts of Inbox/notifications and omits new vacancies and preparation blockers. | HIGH |
| UX-4 | UX | Reply/auto-chat | Safe decisions are available, but review/leave/no-action/need-input outcomes are more understandable to an operator than to an ordinary candidate. | MEDIUM |
| QUALITY-1 | QUALITY | Matching | Final decisions do not retain a complete stage-by-stage rejection taxonomy, so zero-MATCH runs cannot be diagnosed confidently. | HIGH |
| QUALITY-2 | QUALITY | Semantic value | Semantic retrieval is correctly bounded but has limited demonstrated material effect outside selected cover-letter paths. | MEDIUM |
| DATA-1 | DATA | Application continuity | PostgreSQL has 98 applications but 0 application-level match results; 259 conversations have no application link, weakening journey continuity. | BLOCKING for end-to-end product continuity |
| DATA-2 | DATA | Candidate Knowledge | Seven unknowns need confirmation and no proposals are pending; source linkage is not surfaced in the vacancy/preparation review context. | MEDIUM |
| DATA-3 | DATA | Notifications | Historical notification volume (262 local records) and mixed lifecycle views make “important now” less distinct. | MEDIUM |
| FEATURE-1 | FEATURE GAP | Discovery | No product layer turns fetched vacancies into a ranked new shortlist with explicit review priority. | HIGH |
| FEATURE-2 | FEATURE GAP | Applications | No complete candidate-controlled preparation/review workflow for a selected vacancy. | HIGH |
| FEATURE-3 | FEATURE GAP | Follow-up | Eligibility and drafts exist, but no integrated due queue through final approved action. | MEDIUM |
| FEATURE-4 | FEATURE GAP | Automation | Scheduler cadences and enablement are configuration-only; the dashboard does not explain what is currently running or what remains operator-owned. | MEDIUM |
| PERF-1 | PERFORMANCE | Health/sync | Deep health/pilot-shortlist evidence was about 14 seconds; accepted performance evidence also records roughly 5–6 minutes for full conversation sync and roughly 0.4 seconds for health/eligibility views. | MEDIUM |


# Top product gaps

The ranking below is by candidate value and frequency, not by architectural
elegance.

| Rank | Gap | Impact | Frequency | Effort | Risk |
|---:|---|---|---|---|---|
| 1 | No ranked, fresh, new-vacancy review queue | High: blocks the first product decision and wastes search output | Every search/day | L | MEDIUM |
| 2 | Only 20/279 vacancies analyzed; no visible analysis coverage/freshness workflow | High: most stored jobs cannot be explained or compared | Every sync/run | M | MEDIUM |
| 3 | No vacancy-first application preparation workspace | High: the user cannot confidently move from fit decision to a complete review package | Every serious application | L | HIGH |
| 4 | Applications have 0/98 match results and conversations are not linked to applications | High: breaks continuity and hides why an application was made | Every historical/current application | M/L | HIGH |
| 5 | Matching lacks per-stage rejection reason telemetry | High: zero-MATCH outcomes cannot be diagnosed or improved safely | Every analysis batch | M | LOW |
| 6 | Today omits new vacancies/preparation and duplicates Inbox/notifications | High: daily command-center promise is incomplete | Every daily session | M | LOW |
| 7 | Follow-up is eligibility plus draft, not an integrated due-to-approved workflow | Medium/high: stale opportunities can be missed | Several times per week | M | MEDIUM |
| 8 | Candidate questions are separated from the vacancy/application that caused them | Medium/high: user effort and uncertainty resolution time increase | Whenever a hard unknown occurs | M | LOW |
| 9 | Notification history competes with urgent items; no unified urgency/deadline queue | Medium: important action can be buried | Every monitor refresh | M | LOW |
| 10 | CLI/dashboard source-of-truth divergence and operator-only controls | Medium/high: trust and discoverability suffer | Whenever CLI audit/automation is used | M | MEDIUM |


# Proposed roadmap

P1: **Vacancy Discovery & Ranking**

Turn the existing search and analysis outputs into a candidate-facing queue of
fresh, deduplicated, new/unseen vacancies. Show fit bands, score, recommendation,
freshness, already-applied state, location/work-format implications, and a
short explanation. Make analysis coverage and unknowns visible without changing
the accepted deterministic safety policy.

P2: **Application Review & Preparation UX**

For one selected vacancy, assemble a review package: suitability decision,
requirements map, confirmed/missing/unknown facts, cover letter, application
questions, validated test answers, candidate facts used, warnings, editable
previews, approval, fresh preflight, and the controlled application boundary.

P3: **Employer Conversation Agent**

Unify Inbox, application, and conversation context; make needs reply, candidate
input, external action, interview, waiting, stale, and terminal states clear;
improve reply draft review and auto-chat preview without weakening safety.

P4: **Follow-up Pipeline**

Promote follow-up from a read model to a visible due queue with draft history,
candidate approval, fresh eligibility, and the same safe action lifecycle as
other HH writes.

P5: **Candidate Knowledge Acquisition & Quality Loop**

Tie unknowns to the exact vacancy/application/message, guide the user through
answer → proposal → confirmation, surface provenance in preparation/replies,
and measure classification, match, draft, and notification quality. Keep the
semantic scope unchanged unless later evidence specifically justifies a
product change.


# Recommended P1

Problem:

The product can collect vacancies and store a match result, but the normal user
experience does not answer the daily question “Which new vacancies should I
consider first, and why?” PostgreSQL currently contains 279 vacancies and only
20 analyzed results. The dashboard list is publication-date ordered and has
filters, not a ranked new-work queue. The historical zero-MATCH production run
also showed that the operator lacks enough stage-level explanation to tell
whether poor output came from search quality, deterministic gates, unknowns, or
AI.

User outcome:

After a refresh, the candidate sees a small, trustworthy ordered set of new
vacancies. Each item has a clear fit band, score, recommendation, freshness,
already-applied/terminal state, location/work-format implication, and concise
“why review / why skip” explanation. The candidate can open one item, understand
the decision, mark it for application review, or dismiss it without guessing.

Scope:

- Define a product ranking/read model on top of existing vacancy, match,
  recommendation, publication, response, archive, and analysis state.
- Add explicit new/unseen/reviewed/shortlisted semantics at the product layer.
- Make the queue expose analyzed versus unanalyzed coverage and safe
  `REVIEW_REQUIRED` items.
- Preserve deterministic hard-requirement authority and the existing
  `MATCH`/`REVIEW_REQUIRED`/`REJECT` safety model.
- Provide a vacancy explanation that separates confirmed matches, missing
  facts, unknowns, risks, and policy reason.
- Add bounded, non-sensitive decision-reason telemetry sufficient to explain
  rejected/unknown outcomes.
- Make the dashboard and CLI expose the same canonical ranked view where both
  are supported.

Explicit non-goals:

- No threshold weakening or automatic promotion of unknown facts.
- No automatic application, test submission, employer message, resume touch,
  status update, or other HH write.
- No PostgreSQL schema/migration work as part of P1 definition.
- No semantic-index expansion, semantic reindex, prompt redesign, or change to
  the accepted three-document scope.
- No dashboard-wide redesign.
- No reopening R15, S4, or the closed storage/refactor tracks.

Acceptance criteria:

- A read-only refresh produces a deterministic, deduplicated queue of new
  vacancies ordered by documented ranking fields.
- The queue distinguishes at least new/unseen, reviewed, shortlisted,
  already-responded, archived/terminal, `apply`, `maybe`, `skip`, and
  `REVIEW_REQUIRED` states without guessing unknowns.
- Every analyzed item exposes confirmed matches, missing skills, unknown
  skills, risks, score, recommendation, freshness, and a concise reason.
- Unanalyzed items are visibly labeled and do not appear as silently low-fit
  items.
- A bounded report can attribute outcomes to deterministic gate/policy,
  unknown requirement, location/work format, role/skill evidence, AI advisory
  result, or other safe category without logging private message content or
  candidate secrets.
- The user can move a selected vacancy into the future Application Review &
  Preparation stage without creating an HH write or implying that a match is an
  authorization.
- `HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` still yield zero HH writes.
- Dashboard and CLI results use the configured canonical PostgreSQL source, or
  explicitly label any compatibility/legacy source.


# Infrastructure status

PostgreSQL:

**ACCEPTED**

pgvector:

**ACCEPTED**

Storage/refactor:

**CLOSED**

R10–R14:

**CLOSED**

V1:

**PASS**

S1/S2/S3:

**COMPLETE**

V2:

**PASS**

Docker:

**NOT SUPPORTED / NOT APPLICABLE**

HH writes during P0:

**0**

P0 verification:

- `go test -count=1 ./...` — PASS
- `go vet ./...` — PASS
- `go build ./...` — PASS
- `git diff --check` — PASS
- `node --check web/app.js` — PASS

Evidence reviewed included the accepted V2/storage/performance/quality reports,
the configured non-secret runtime settings, current PostgreSQL aggregate
counts, CLI help and behavior, dashboard routes and frontend views, and the
application, employer-chat, test-answering, and Candidate Knowledge safety
contracts. Historical validation artifacts and write-audit records were used
only as historical evidence; no new HH mutation was performed during P0.

STOP: P1 was selected but not implemented.
