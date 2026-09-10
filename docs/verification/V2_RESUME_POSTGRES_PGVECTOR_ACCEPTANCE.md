# Executive summary

Date: 2026-09-10

V2.Resume: **FAIL / PARTIAL ACCEPTANCE EVIDENCE**

Final V2: **FAIL**

PostgreSQL + pgvector cutover: **NOT ACCEPTED under the strict V2.Resume rule**

LIVE HH WRITES: **0**

V2.Fix-1 and V2.Fix-2 both passed their focused regressions and the targeted real PostgreSQL/HH checks. PostgreSQL remained the runtime source of truth and pgvector remained READY with three 1024-dimensional documents. The strict acceptance gate is not closed because the current auto-chat list request returned HTTP 400, the Career monitor one-run path did not complete within a bounded observation, and no vacancy reached application preparation under the unchanged production policy. No production code was changed during this acceptance run.

Observed follow-up blockers:

- **V2.Fix-3a** — current auto-chat loader returns `400 Bad Request` before detail evaluation.
- **V2.Fix-3b** — Career one-run monitor uses a broad detail sync and did not complete within approximately five minutes; it was stopped by context cancellation.
- **V2.Fix-3c** — no explicit real PostgreSQL application-preparation harness is exposed, and the unchanged bounded production policy produced zero MATCH results.

# Baseline

Effective configuration was verified without printing secret values:

| Setting | Value |
|---|---|
| STORAGE_BACKEND | postgres |
| DATABASE_URL | configured |
| EMBEDDING_PROVIDER | openai-compatible |
| EMBEDDING_MODEL | mistral-embed |
| EMBEDDING_DIMENSIONS | 1024 |
| HH_DRY_RUN | true |
| HH_WRITE_ENABLED | false |

Pre-acceptance PostgreSQL counts matched the requested baseline:

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

S3 migration was not rerun. JSON migration sources were not modified or used as runtime authority. Docker was not applicable.

# V2.Fix-1 regression

**PASS.** Focused tests passed for `internal/adapters/hh/read`, including boolean, null, missing, and current `{"@hidden": bool}` archived shapes; explicit errors for unsupported shapes; and preservation of conversation, vacancy, message identity, and message order.

The focused HH read, sync, auto-chat compatibility, and Career-related test packages passed. No provider write capability is present in the read adapter.

# Real HH conversation GET

**PASS.** The canonical executable performed one bounded authenticated GET-only conversation sync for HH conversation `5599447353`.

- authentication: PASS;
- HTTP method: GET;
- typed decode: PASS;
- ConversationID: preserved;
- VacancyID: preserved (`136874905`);
- messages: parsed, including the current provider archived-object shape;
- sync result: `fetched=1`, `created=0`, `updated=1`, `errors=0`;
- HH mutations: 0.

# Bounded conversation sync

**PASS.** The same targeted `hh sync conversation` path completed through the fixed decoder, updated the PostgreSQL projection, and was immediately visible through PostgreSQL-backed `hh inbox` and the dashboard. No JSON regeneration, runtime restart, or manual import was used. Counts stayed at 259 conversations and 791 messages, so the update was a legitimate no-op in row cardinality.

# V2.Fix-2 regression

**PASS.** Focused tests passed for PostgreSQL/JSON application-attempt repositories, attempt policy, reconciliation, auto-chat orchestration, and the full suite. Coverage confirmed:

- empty store and unrelated vacancy: `NO_ATTEMPT`, continue;
- `SENDING`, `ACCEPTED`, `DELIVERY_UNCERTAIN`, and `TARGET_RESPONSE_CONFIRMED`: block;
- `REJECTED` and `NOT_SENT`: replay semantics preserved;
- real repository errors: fail closed;
- unknown explicit AttemptID: canonical not-found;
- reservation concurrency and reconciliation: PASS.

# Empty application-attempt authority

**PASS.** The opt-in real PostgreSQL integration test ran against the configured database. Before and after `FindBlocking`/unknown-ID lookup:

```text
automatic_application_attempts = 0
FindBlocking(current vacancy) = applicationattempt.ErrAttemptNotFound
unknown explicit AttemptID = applicationattempt.ErrAttemptNotFound
after lookup = 0
```

No placeholder or application-attempt row was created.

# Bounded auto-apply

**PASS through the former Fix-2 gate; no application was prepared or sent.** One bounded real run used small limits with `HH_DRY_RUN=true`, `HH_WRITE_ENABLED=false`, auto-chat/touch/job-status disabled, and the real HH search plus PostgreSQL attempt authority.

| Counter | Result |
|---|---:|
| HH vacancy cards fetched | 82 |
| deduplicated | 46 |
| considered/processed | 6 |
| attempt-gate errors | 0 |
| deterministic-policy rejected | 5 |
| AI evaluated | 3 |
| AI matched | 0 |
| review-required | 0 |
| prepared | 0 |
| would-apply | 0 |
| applied | 0 |
| other errors | 0 |

The pipeline advanced beyond the previous empty-store failure and reached real Mistral vacancy analysis. No production filter or threshold was weakened.

# Vacancy analysis

**PASS.** Three real current vacancies reached typed Mistral analysis through the production auto-apply path using the PostgreSQL Candidate and PostgreSQL vacancy runtime. The emitted typed results were valid and policy-safe:

- supported Candidate facts were used;
- unknown education, language, skill, and experience facts remained unknown;
- no unsupported Senior/highload/commercial-experience claim was introduced;
- AI rejection and hard-requirement rejection were recorded safely;
- HH mutations: 0.

No vacancy was a production MATCH, so application preparation was not entered naturally.

# Semantic context

**PASS for the configured semantic runtime and analysis boundary.** Live PostgreSQL/pgvector status:

```text
provider: openai-compatible
model: mistral-embed
dimensions: 1024
space: sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9
project: eligible=3 indexed=3 stale=0
status: READY
```

The real query `Python automation` returned three `project` results. The semantic scope remained exactly three truth-safe project documents. No reindex or scope expansion occurred.

# Application preparation

**NOT REACHED.** The real bounded policy produced zero MATCH results, and the repository does not expose an existing explicit real PostgreSQL/Mistral application-preparation harness. Production matching thresholds and filters were not weakened. Therefore cover-letter/test-answer preparation cannot be claimed as an integration acceptance result in this run.

# Application dry-run boundary

**PASS.** The workflow was exercised to the available safe boundary:

- `HH_DRY_RUN=true`;
- `HH_WRITE_ENABLED=false`;
- gateway: `BLOCKED_BY_DRY_RUN`;
- provider response submission: 0;
- test-answer submission: 0;
- application attempts after the run: 0.

No temporary write enablement was used and no reservation side effect was created.

# Career loop

**PARTIAL / BLOCKED.** The correct one-run invocation was started with the PostgreSQL Candidate, PostgreSQL career repositories, and read-only HH config. The monitor uses a broad detail sync and held an authenticated HH connection for approximately five minutes without producing its final result. It was stopped with context cancellation while requesting a chat detail. The targeted conversation read/sync path itself passed; a completed bounded Career iteration is not claimed.

# Auto-chat review

**FAIL for the current runtime path.** One run used review mode with all HH writes disabled. The loader failed before detail evaluation with:

```text
load chats error: unexpected HTTP status 400 Bad Request
```

Chat send: 0. Chat leave: 0. The fixed typed decoder independently passed in the targeted real conversation GET and focused tests.

# Employer reply regression

**PASS.** A real PostgreSQL conversation draft flow executed with the PostgreSQL Candidate and AI boundary. It returned the safe typed outcome `need_candidate_input` because the employer asked whether the candidate had read the vacancy conditions. No Candidate fact was invented and no send occurred.

# Follow-up

**PASS.** Read-only follow-up evaluation used PostgreSQL applications and conversations. It reported 41 follow-up-eligible conversations, with no send or provider mutation.

# Scheduler

Canonical composition initialized the expected job classes: auto apply, auto-chat, resume touch, job status, and Career. The scheduler interval tests passed. The bounded auto-apply scheduler/use-case run passed the attempt authority gate with zero errors. Auto-chat scheduler review remained affected by the current HTTP 400 loader result.

Resume-touch and job-status writes were disabled and not exercised against the provider. Resume touches: 0. Job-status writes: 0.

# PostgreSQL source-of-truth

**PASS.** Candidate status reported `candidate_storage=postgres` and `legacy_json=compatibility_only`. Dashboard, inbox, applications, conversations, attempt authority, and semantic status used PostgreSQL. No normal workflow silently regenerated or fell back to Career JSON.

# Semantic

**PASS.** Candidate semantic status remained READY, with three documents, 1024 dimensions, and the unchanged active space. No 1536-dimensional active vectors, startup reindex, or retest reindex occurred.

# Restart

**PASS.** A fresh canonical runtime started after the affected workflows with PostgreSQL configured and all write tasks disabled. Candidate and semantic status were available; the write boundary remained `BLOCKED_BY_DRY_RUN`.

# Dashboard quick regression

**PASS.** A fresh read-only dashboard returned HTTP 200 for every required endpoint:

| Endpoint | HTTP | Bytes |
|---|---:|---:|
| `/api/health` | 200 | 1,290 |
| `/api/today` | 200 | 295,576 |
| `/api/dashboard` | 200 | 4,322 |
| `/api/applications` | 200 | 83,932 |
| `/api/conversations` | 200 | 629,224 |
| `/api/inbox` | 200 | 947,306 |

# Data integrity

All read-only integrity checks passed:

| Check | Count |
|---|---:|
| orphan applications | 0 |
| orphan application events | 0 |
| orphan conversations | 0 |
| orphan messages | 0 |
| zero VacancyID canonical relations | 0 |
| duplicate stable external IDs | 0 |
| candidates | 1 |
| semantic bad-dimension rows | 0 |

# Final database counts

Final counts matched the pre-resume baseline exactly:

| Entity | Before | After | Result |
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

# Existing non-blocking observations

Deep health/pilot-shortlist performance was not remeasured; the prior approximately 14-second observation remains UX debt and was not optimized.

Semantic scope remains intentionally limited to three truth-safe project documents. No migration source was modified, no migration was rerun, and no semantic scope was expanded.

# Targeted acceptance matrix

| Workflow | Previous V2 | V2.Resume | Evidence |
|---|---|---|---|
| HH targeted conversation read | FAIL / Fix-1 | PASS | Real GET-only sync decoded archived object; 1 fetched, 0 errors |
| Bounded HH conversation sync | FAIL / Fix-1 | PASS | PostgreSQL projection updated/read back; counts unchanged |
| Vacancy analysis | BLOCKED / Fix-2 | PASS | Three real typed Mistral analyses; policy-safe results |
| Application preparation | BLOCKED / Fix-2 | NOT REACHED | Zero MATCH under unchanged policy; no explicit real harness |
| Application dry-run boundary | BLOCKED upstream | PASS | Gateway blocked by dry-run; zero provider submissions |
| Career loop | BLOCKED / Fix-1 | PARTIAL / BLOCKED | One-run monitor did not complete within bounded observation |
| Auto-chat review | PARTIAL / Fix-1 | FAIL | Current chat loader returned HTTP 400 before detail evaluation |
| Auto-apply scheduler iteration | BLOCKED / Fix-2 | PASS through gate | Attempt-gate errors 0; AI evaluation reached |
| Scheduler composition | PARTIAL | PASS | Canonical job classes and interval tests passed |

Previously passing foundational workflows remain regression PASS: PostgreSQL runtime/source-of-truth, dashboard smoke, semantic retrieval, employer-reply draft safety, restart, integrity, Fix-1 focused adapter tests, and Fix-2 attempt authority tests.

# Regression

| Check | Result |
|---|---|
| focused Fix-1/Fix-2 tests | PASS |
| real empty-attempt PostgreSQL integration | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `gofmt -l .` | PASS; no output |
| `git diff --check` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |
| Docker | NOT APPLICABLE |

# HH safety

HH_DRY_RUN: **true**

HH_WRITE_ENABLED: **false**

| Mutation class | V2.Resume delta |
|---|---:|
| Vacancy response writes | 0 |
| Application/test writes | 0 |
| Chat sends | 0 |
| Chat leaves | 0 |
| Resume touches | 0 |
| Job-status writes | 0 |
| Other HH mutations | 0 |
| LIVE HH WRITES | **0** |

HH write retry: **none / unchanged**. Historical write-audit totals were not treated as V2.Resume activity; the acceptance delta above is zero.
