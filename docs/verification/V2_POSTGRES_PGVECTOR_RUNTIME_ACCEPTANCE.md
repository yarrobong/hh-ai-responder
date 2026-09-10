# PostgreSQL + pgvector Runtime Acceptance (V2)

Date: 2026-09-10

## Executive summary

V2: **FAIL**

PostgreSQL runtime: **PASS** for startup, source-of-truth reads, dashboard,
restart, and data integrity.

Semantic: **PASS**. Real openai-compatible / mistral-embed retrieval returned
active PostgreSQL/pgvector project documents with 1024 dimensions.

HH: **FAIL for the affected read-sync workflow**. Authentication and the
bounded GET reached HH, but the typed conversation response decoder rejected the
current payload shape.

AI: **PARTIAL**. Real Mistral completed an employer-reply draft decision and
real embedding queries worked. Vacancy-analysis/application preparation could
not reach the AI boundary because the empty PostgreSQL application-attempt
store was incorrectly treated as unavailable.

LIVE HH WRITES: **0**

Two production defects were isolated and intentionally not fixed in V2:

- **V2.Fix-1** — bounded conversation GET parsing fails because the live
  resources.vacancies.archived value is an object while the typed model
  requires bool.
- **V2.Fix-2** — an empty automatic_application_attempts table returns
  no rows in result set through the attempt-authority path, so every bounded
  auto-apply candidate is rejected before analysis.

Per the V2 rules, the affected workflows were stopped after diagnosis. No
migration, reindex, prompt change, semantic-scope change, or production fix was
performed.

## Baseline

Effective configuration was verified without printing secret values:

| Setting | Effective value |
|---|---|
| STORAGE_BACKEND | postgres |
| DATABASE_URL | configured |
| EMBEDDING_PROVIDER | openai-compatible |
| EMBEDDING_MODEL | mistral-embed |
| EMBEDDING_DIMENSIONS | 1024 |
| HH_DRY_RUN | true |
| HH_WRITE_ENABLED | false |

Pre-acceptance PostgreSQL counts matched the required baseline:

| Entity | Count |
|---|---:|
| Candidate | 1 |
| Vacancies | 279 |
| Applications | 98 |
| Application events | 308 |
| Conversations | 259 |
| Messages | 791 |
| Application attempts | 0 |
| Auto-chat attempts | 0 |
| Semantic documents | 3 |

## Startup

PASS. The canonical executable and read-only dashboard started normally with
the real configuration. PostgreSQL opened successfully, Candidate status
reported candidate_storage=postgres, and semantic status reported READY. No
panic or silent JSON career fallback was observed.

The configured task surface was inspected and the bounded --run-once path was
exercised with application, auto-chat, resume-touch, and job-status writes
disabled. No external mutation occurred.

## PostgreSQL source-of-truth

PASS for normal reads.

- candidate status reported candidate_storage=postgres and
  legacy_json=compatibility_only;
- dashboard vacancies, applications, and conversations returned PostgreSQL-
  backed counts of 279, 98, and 259;
- reliability readers reported storage postgres and empty attempt collections;
- Candidate Knowledge returned non-empty PostgreSQL-backed data;
- no normal career read required regenerating legacy JSON.

## Dashboard / Today

PASS for the core read-only surface. All listed GET endpoints returned HTTP
200 with non-empty expected data:

| Endpoint | Result | Size / timing |
|---|---|---:|
| /api/today | PASS | 295,993 bytes / 0.334 s |
| /api/dashboard | PASS | 4,478 bytes / 2.747 s |
| /api/vacancies | PASS | 1,092,702 bytes / 0.106 s |
| /api/applications | PASS | 83,932 bytes / 2.315 s |
| /api/conversations | PASS | 629,136 bytes / 0.044 s |
| /api/inbox | PASS | 947,220 bytes / 0.302 s |
| /api/knowledge | PASS | 26,053 bytes / 0.081 s |
| /api/notifications | PASS | 100,369 bytes / 0.154 s |
| /api/reliability/application-attempts | PASS | empty collection |
| /api/reliability/autochat-attempts | PASS | empty collection |
| /api/health | PASS | 1,809 bytes / 0.118 s |

Today composed migrated data successfully: needs reply 1, needs action 4,
follow-ups 41, interviews 18, notifications 177.

Health reported capability BLOCKED_BY_DRY_RUN, dry_run=true, and
hh_write_enabled=false.

Deep health and pilot-shortlist returned HTTP 200 but each took approximately
14 seconds over 259 conversations. This is recorded as a UX/performance
observation, not the V2 integration failure.

## HH authenticated read

FAIL: V2.Fix-1.

The bounded smoke used a targeted read of one current PostgreSQL conversation.
The request was GET-only and HH authentication/network access succeeded far
enough for the response body to reach the typed decoder. The read then failed
with:

    invalid HH chat detail response: json: cannot unmarshal object into Go struct field rawChatVacancy.resources.vacancies.archived of type bool

No local PostgreSQL import was committed by this failed read. A full sync was
not attempted after this bounded defect was identified.

## Bounded HH sync

FAIL: V2.Fix-1. The targeted read returned fetched=0, created=0, updated=0,
errors=1; PostgreSQL counts were unchanged. No HH mutation was available
through this GET-only path.

The existing runtime merge contract preserves historical partial vacancies,
does not delete migrated history merely because a record is currently
unavailable, excludes the two legacy vacancy_id=0 records, and does not rewrite
Candidate truth. This acceptance did not execute a successful merge, so no new
merge result is claimed.

## Vacancy analysis

Blocked: V2.Fix-2.

The selected real current PostgreSQL vacancy was 135380957,
“Senior python-разработчик / Team Lead по AI-автоматизации”; it was not
archived and had a 9,379-character description.

The bounded real auto-apply iteration fetched 47 HH vacancy cards, deduplicated
to 33, and safely processed 33. All 33 stopped at the attempt-authority gate
with no rows in result set. AI-evaluated, matched, prepared, and would-apply
counts were all zero. A real vacancy-analysis call through this workflow
therefore cannot be claimed as exercised.

## Semantic retrieval

PASS.

Three real semantic queries used the active PostgreSQL/pgvector retriever and
the configured embedding provider:

| Query | Results | Entity types | Dimensions |
|---|---:|---|---:|
| Python automation | 3 | project, project, project | 1024 |
| PostgreSQL integrations | 3 | project, project, project | 1024 |
| API integration | 3 | project, project, project | 1024 |

All results were scoped to the active space sha256:1ae6b8f2... (the full value
was verified locally), with provider openai-compatible and model mistral-embed.
No Candidate truth changed and no legacy vector participated.

## Semantic scope

Eligible semantic entity types: truth-safe project, story, and achievement
documents; an entity is active only when its confirmed/verified truth and
referenced claims/skills pass the deterministic eligibility policy.

Current active documents: **3**.

Current canonical inventory: 3 projects, 5 stories, 0 achievements. The 5
stories are currently ineligible and no active story/achievement document is
present.

Is current scope intentional: **YES**.

Impact: 3 project documents only is the current truth-safe product design, not
an indexing gap for V2. No semantic scope was expanded.

## Application preparation

BLOCKED by V2.Fix-2 before vacancy analysis, semantic context assembly, cover
letter generation, or application answer preparation. The safe application
writer was never reached.

The live one-shot iteration used HH_DRY_RUN=true, HH_WRITE_ENABLED=false,
HH_AUTO_APPLY=true, and one-vacancy/one-application bounds. It generated no
application preview because the attempt-authority gate failed closed for the
empty attempt table.

## Application dry-run safety

PASS for the configured boundary.

candidate semantic status, hh write-status, runtime health, and the bounded
iteration confirmed:

- HH_DRY_RUN=true;
- HH_WRITE_ENABLED=false;
- gateway capability BLOCKED_BY_DRY_RUN;
- no application/test provider mutation;
- no send/leave/resume/status mutation.

The empty attempt-store defect prevented reaching the application provider
boundary; it did not weaken the dry-run gate.

## Applications

PASS for readability. All 98 migrated applications remained readable through
the PostgreSQL dashboard/API path. Status, vacancy relation, event timeline,
and conversation relation did not expose a migration-only format.

## Conversations / inbox

PASS for local PostgreSQL read. All 259 conversations and 791 messages were
available through conversation and inbox views with provider IDs, sender,
direction, ordering, timestamps, and vacancy/application context.

The current HH detail refresh is blocked by V2.Fix-1; local PostgreSQL history
remained available without JSON career fallback.

## Employer reply draft

PASS for a real AI draft decision. A real PostgreSQL conversation was passed
through hh draft with PostgreSQL Candidate context and the configured Mistral
completion provider. The typed result was valid and returned the safe outcome
manual_review because the question could not be decomposed into supported
atomic facts. No unsupported Candidate fact was generated, and no send or
approval occurred.

## Unknown fact handling

PASS as an unknown-safe outcome. The real employer-reply workflow returned
manual_review with the reason that the employer question was not supported by
known atomic facts; it did not invent an answer. Semantic-failure fallback tests
also passed: structured Candidate context remains available when the semantic
provider fails.

## Follow-up

PASS for read-only evaluation. The migrated PostgreSQL applications and
conversations produced a valid follow-up view with 41 current follow-up items.
Eligibility/cooldown evaluation was read-only; no follow-up was sent.

## Career loop

BLOCKED by V2.Fix-1. A full Career iteration was not continued after the
targeted authenticated HH read exposed the typed-response incompatibility.
Local Career/Today composition remained readable, but a successful live
sync-backed iteration cannot be claimed.

## Auto-chat review

PARTIAL / BLOCKED by V2.Fix-1. Local PostgreSQL conversation eligibility and
reply-draft safety were exercised in review-safe form. A full live auto-chat
iteration was not run after the HH detail decoder defect was isolated. No
external chat send or leave occurred.

## Reliability

PASS for inspection.

- PostgreSQL automatic application attempts: 0;
- PostgreSQL legacy auto-chat attempts: 0;
- controlled local HH actions: 13, still accessible;
- the historical HH write audit was unchanged; newest event remained
  2026-09-07T09:16:15.960936Z, before this acceptance run;
- ordinary reliability inspection did not call HH mutation endpoints.

## Scheduler

PARTIAL. The canonical runtime initialized with auto-apply, auto-chat, resume
touch, job status, and Career monitor task composition. The bounded one-shot
auto-apply iteration ran with dry-run enabled and made no external mutation.
Application processing stopped at V2.Fix-2; Career/auto-chat sync-backed
iterations were not continued after V2.Fix-1.

## Restart

PASS.

After acceptance activity, the dashboard restarted normally and returned:

- /api/health: HTTP 200, PostgreSQL counts 279/98/259;
- /api/today: HTTP 200, 295,488 bytes;
- semantic status: READY;
- semantic documents: still 3 at 1024 dimensions;
- write capability: BLOCKED_BY_DRY_RUN.

No startup reindex or embedding rebuild was observed. Space ID and document
count remained unchanged.

## Performance smoke

Representative local timings were healthy for the main views:

| View | Time |
|---|---:|
| Today | 0.334 s |
| Dashboard analytics | 2.747 s |
| Vacancies | 0.106 s |
| Applications | 2.315 s |
| Conversations | 0.044 s |
| Inbox | 0.302 s |
| Knowledge | 0.081 s |
| Notifications | 0.154 s |
| Semantic CLI query | successful for all three queries |

Deep health and pilot-shortlist each took approximately 14 seconds. No
optimization was attempted.

## Final database counts

| Entity | Final | Baseline | Result |
|---|---:|---:|---|
| Candidate | 1 | 1 | PASS |
| Vacancies | 279 | 279 | PASS |
| Applications | 98 | 98 | PASS |
| Application events | 308 | 308 | PASS |
| Conversations | 259 | 259 | PASS |
| Messages | 791 | 791 | PASS |
| Application attempts | 0 | 0 | PASS |
| Auto-chat attempts | 0 | 0 | PASS |
| Semantic documents | 3 | 3 | PASS |

The failed bounded HH sync created or updated no PostgreSQL career rows. No
migration or semantic reindex was rerun. Candidate truth was not rewritten.

## Data integrity

PASS.

- orphan applications: 0;
- orphan application events: 0;
- orphan conversations: 0;
- orphan messages: 0;
- application/conversation relations with vacancy_id=0: 0;
- duplicate non-empty stable vacancy external IDs: 0;
- duplicate non-empty stable application external IDs: 0;
- duplicate non-empty stable conversation external IDs: 0;
- semantic dimensions and pgvector dimensions: 1024 / 1024;
- active semantic space: unchanged.

Migration-source Candidate/career JSON was not used as the runtime source and
was not migrated, deleted, or reindexed during V2. Local draft, notification,
and run-event stores remain operational JSON by design.

## Product-quality observations

| ID | Type | Workflow | Observation | Severity |
|---|---|---|---|---|
| Q-1 | QUALITY | Employer reply | Real Mistral produced a safe manual-review outcome for an unsupported question; no unsafe claim was emitted. | Informational |
| Q-2 | UX | Deep health / pilot shortlist | Both views return 200 but take about 14 seconds on 259 conversations. | Medium |
| D-1 | DATA / integration | HH conversation sync | Live HH response shape is incompatible with the typed decoder. | Blocking |
| D-2 | DATA / integration | Auto-apply | Empty attempt authority is treated as unavailable instead of an empty safe state. | Blocking |
| F-1 | FEATURE GAP | Vacancy analysis | No explicit safe PostgreSQL VacancyID-to-Mistral preparation command exists to bypass the attempt-gate defect. | Medium |

## Acceptance matrix

| Workflow | PostgreSQL | pgvector | Real HH read | Real AI | HH write | Result |
|---|---:|---:|---:|---:|---:|---|
| Startup | Yes | Yes | No | Configured | Blocked | PASS |
| Dashboard | Yes | No | No | No | Blocked | PASS |
| Today | Yes | No | No | No | Blocked | PASS |
| Vacancy read | Yes | No | No | No | Blocked | PASS |
| HH sync | Yes | No | Yes, decoder failed | No | 0 | FAIL — V2.Fix-1 |
| Vacancy analysis | Yes | Yes by design, not reached | HH cards fetched | No | 0 | FAIL — V2.Fix-2 |
| Application preparation | Yes | Not reached | HH cards fetched | Not reached | 0 | FAIL — V2.Fix-2 |
| Application dry-run gate | Yes | No | No | No | Blocked | PASS |
| Applications | Yes | No | No | No | Blocked | PASS |
| Conversation inbox | Yes | No | No | No | Blocked | PASS |
| Employer reply draft | Yes | Available | No | Yes, Mistral | 0 | PASS |
| Unknown fact | Yes | Fallback-safe | No | Yes, Mistral | 0 | PASS |
| Follow-up | Yes | No | No | No | 0 | PASS |
| Career loop | Yes | No | Blocked by Fix-1 | Not reached | 0 | BLOCKED |
| Auto-chat review | Yes | Available locally | Blocked by Fix-1 | Not reached | 0 | PARTIAL |
| Reliability | Yes | No | No | No | 0 | PASS |
| Semantic retrieval | Yes | Yes | No | Yes, real embedding provider | 0 | PASS |
| Restart | Yes | Yes | No | No | Blocked | PASS |
| Scheduler composition | Yes | No | Partial | Configured | Blocked | PARTIAL |

## Regression

All required checks passed:

    gofmt -l .                  PASS (no output)
    git diff --check            PASS
    go test -count=1 ./...      PASS
    go test -race ./...         PASS
    go vet ./...                PASS
    go build ./...              PASS
    go build ./cmd/hh-ai-responder PASS
    node --check web/app.js     PASS

Targeted semantic/fallback tests passed. The live PostgreSQL repository test
was skipped because POSTGRES_TEST_DATABASE_URL is not configured; the real
production database checks above were run separately against DATABASE_URL.

The PostgreSQL failure simulation produced an explicit connection error from
candidate status with a port-1 database URL and did not silently fall back to
JSON.

## HH safety

These are V2-acceptance deltas, not historical audit totals:

| Mutation class | V2 count |
|---|---:|
| Vacancy response writes | 0 |
| Application/test writes | 0 |
| Chat sends | 0 |
| Chat leaves | 0 |
| Resume touches | 0 |
| Job-status writes | 0 |
| Other HH mutations | 0 |
| LIVE HH WRITES | 0 |
| HH write retry | NONE / unchanged |

Historical local controlled actions remain 13 and historical write-audit events
remain available; they were not created by this acceptance run.

## Final decision

V2: **FAIL**

Reason: PostgreSQL, pgvector, dashboard, semantic retrieval, real Mistral
reply-draft composition, restart, dry-run safety, regression checks, and data
integrity passed. However, the current authenticated HH conversation read is
not typed-response compatible, and the empty PostgreSQL application-attempt
state blocks real vacancy analysis/application preparation. Both are
reproducible runtime defects, not merely empty-data or product-quality
observations.

Resolve V2.Fix-1 and V2.Fix-2 in a subsequent fix stage, then rerun the
affected acceptance workflows. Do not enable live HH writes or start feature
work from this result.

## Ready

**Not ready for acceptance.**

POSTGRESQL + PGVECTOR CUTOVER: **NOT ACCEPTED** until the two blocking runtime
defects are resolved and the affected workflows are rerun.
