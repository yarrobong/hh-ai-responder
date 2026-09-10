# Executive summary

V2: **PASS**

PostgreSQL + pgvector cutover: **ACCEPTED**

Storage/refactor acceptance track: **CLOSED**

LIVE HH WRITES during V2.Close: **0**

All previously blocking V2 workflows were exercised or carried forward from
accepted evidence on the current unchanged production code. PostgreSQL is the
runtime source of truth for Candidate, Vacancies, Applications, Conversations,
and attempt authority. Semantic retrieval is PostgreSQL/pgvector-backed and
READY. HH writes remained disabled throughout.

No production code, migration, semantic index, or HH write configuration was
changed by V2.Close.

# Baseline

The pre-check found a pre-existing large refactor worktree. It was preserved;
no reset, clean, restore, checkout, or migration was run.

Effective non-secret configuration was verified through the application dotenv
loader:

| Setting | Effective value |
|---|---|
| STORAGE_BACKEND | postgres |
| DATABASE_URL | configured |
| HH_CANDIDATE_ID | candidate-local |
| HH_DRY_RUN | true |
| HH_WRITE_ENABLED | false |
| HH_AI_BASE_URL | configured |
| HH_AI_MODEL | ministral-8b-latest |
| HH_AI_API_KEY | configured |
| EMBEDDING_PROVIDER | openai-compatible |
| EMBEDDING_MODEL | mistral-embed |
| EMBEDDING_DIMENSIONS | 1024 |

Required canonical baseline before acceptance was Candidate 1, Vacancies 279,
Applications 98, Application events 308, Conversations 259, canonical HH
messages 791, Application attempts 0, Auto-chat attempts 0, and Semantic
documents 3.

# Fix closure matrix

| Fix | Status | Final regression |
|---|---|---|
| V2.Fix-1 | CLOSED | PASS |
| V2.Fix-2 | CLOSED | PASS |
| V2.Fix-3a | CLOSED | PASS |
| V2.Fix-3b | CLOSED | PASS |
| V2.Fix-3c | CLOSED | PASS |

Focused decoder, empty-attempt-store, opaque-cursor, bounded-read, and
write-boundary tests passed. Existing accepted Fix-3a and Fix-3b live evidence
remains valid; no relevant production code changed after that evidence.

# PostgreSQL

PASS. The canonical database is `hh_ai_responder_s3`.

- PostgreSQL server: 14.19 (Homebrew)
- pgvector: 0.8.6
- Schema migrations: 1 through 10 applied; `schema_migrations` contains 10 rows
- No migration ran during V2.Close
- Candidate, Vacancy, Application, Conversation, and attempt reads used the
  PostgreSQL repositories
- The dashboard restarted with PostgreSQL counts 279/98/259

# Semantic

PASS. Semantic status is READY:

| Field | Value |
|---|---|
| Provider | openai-compatible |
| Model | mistral-embed |
| Dimensions | 1024 |
| Active SpaceID | accepted unchanged space `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9` |
| Documents | 3 |
| Scope | 3 eligible/indexed projects, 0 stale |

A real `Python automation` query through the normal PostgreSQL/pgvector
retriever returned 3 non-empty project results, all in the active space and
with 1024-dimensional vectors. No Candidate mutation, reindex, new vector, or
legacy-space retrieval occurred.

# HH reads

PASS. The targeted authenticated conversation read/sync completed through the
typed decoder with `fetched=1`, `updated=1`, and `errors=0`. It was GET-only;
no HH mutation was attempted. The archived compatibility cases remain green:
bool, null, missing, object-shaped archived values, and unsupported-shape
failure. Conversation, vacancy, message identity, and ordering were preserved.

# Vacancy analysis

PASS. Accepted V2 evidence remains valid for real Mistral vacancy analysis
through the production PostgreSQL Candidate/Vacancy path. Typed results were
validated; supported candidate facts were safe, unknown requirements remained
unknown, and no match threshold or production policy was weakened.

# Application preparation

PASS for the accepted preparation boundary.

The real acceptance harness used the PostgreSQL Candidate, PostgreSQL Vacancy,
production preparation adapters, configured completion provider, and no HH
writer or reservation port:

- configured model: `ministral-8b-latest`
- selected VacancyID: `135732605`
- production policy evaluation: `REJECT`
- harness result: `PREPARED`
- semantic integration: wired; this selected preparation required no semantic
  query (`semantic_results=0`)
- writer: none supplied
- durable mutations: 0
- application attempts: 0 -> 0
- applications: unchanged
- Candidate truth and semantic document count: unchanged

The acceptance harness’s documented upstream-selection bypass was used only to
exercise preparation. It does not override production policy approval and did
not submit or persist an application.

# Employer conversation

PASS. Existing accepted real Mistral employer-reply draft evidence remains
valid and focused employer-reply tests pass. Safe outcomes remain draft,
manual review, candidate input, or no response. Unsupported candidate facts and
high-risk salary, scheduling, relocation, document, link, and software topics
remain conservative. No reply was sent.

# Auto-chat

PASS. Focused request tests verify that the first page omits `from`, later pages
use the provider’s opaque cursor, and unsupported responses fail explicitly.
The one-run review-mode regression completed with no eligible action, no
application/test write, no chat send, and no chat leave. HTTP 400 count was 0.
Auto-chat attempt authority remained PostgreSQL and empty.

# Career

PASS. Command:

`monitor --run-once --max-conversations-per-run 3`

Result: `completed_bounded_scope`; requested 3, processed 3, detail reads 3,
provider reads 4, skipped 0, errors 0. PostgreSQL was the source of truth and
HH mutations were 0. The run did not wait for normal scheduler cadence.

# Scheduler

PASS. Composition and dry-run tests verify:

- auto-apply uses the fixed attempt gate;
- auto-chat uses the fixed loader and review boundary;
- Career uses the bounded operator path;
- resume-touch and job-status paths remain write-disabled;
- no provider mutation was exercised.

# Restart

PASS. The canonical executable was built from `./cmd/hh-ai-responder`, started
with the real configuration, stopped, and restarted once. After restart it
served the same PostgreSQL-backed counts (279 vacancies, 98 applications, 259
conversations) and `BLOCKED_BY_DRY_RUN`. No startup reindex or JSON Career
fallback was observed.

# Dashboard

PASS. Required loopback endpoints all returned HTTP 200:

| Endpoint | Result |
|---|---|
| `/api/health` | PASS |
| `/api/today` | PASS |
| `/api/dashboard` | PASS |
| `/api/applications` | PASS |
| `/api/conversations` | PASS |
| `/api/inbox` | PASS |

The health response reported PostgreSQL-backed counts and
`write_gateway.capability=BLOCKED_BY_DRY_RUN`.

# Follow-up

PASS. The read-only `hh workflow` evaluation completed using the PostgreSQL
applications and conversations. It reported 259 conversations and 38
follow-up-eligible records. No send or other HH mutation occurred.

# Data integrity

PASS. Read-only SQL checks returned zero for:

- orphan applications, application events, conversations, and messages;
- canonical `vacancy_id=0` relations;
- duplicate stable Vacancy, Application, and Conversation provider IDs;
- bad semantic dimensions.

Candidate count was 1.

# Final database counts

| Entity | Before | After | Result |
|---|---:|---:|---|
| Candidate | 1 | 1 | PASS |
| Vacancies | 279 | 279 | PASS |
| Applications | 98 | 98 | PASS |
| Application events | 308 | 308 | PASS |
| Conversations | 259 | 259 | PASS |
| Canonical HH messages | 791 | 791 | PASS |
| Application attempts | 0 | 0 | PASS |
| Auto-chat attempts | 0 | 0 | PASS |
| Semantic documents | 3 | 3 | PASS |

The physical `conversation_messages` table contains 794 rows both before and
after acceptance: 791 canonical `source=hh` rows plus 3 pre-existing
`source=hh_write` local projection rows. The three local rows match the
pre-existing controlled-write history and were not changed by V2.Close. The
two source-only HH messages belong to the two documented zero-VacancyID legacy
exclusions. This is an explained scope/projection distinction, not an
unexplained mutation.

# Existing non-blocking debt

- Deep health / pilot-shortlist: approximately 14 seconds in the prior
  observation. Classification: UX/performance debt; not a runtime correctness
  blocker.
- Semantic scope: 3 truth-safe project documents. Classification: intentional
  current product design; not a cutover defect.

No new blocking storage defect was found.

# Acceptance matrix

| Workflow | Final result | Evidence |
|---|---|---|
| PostgreSQL startup | PASS | Canonical executable; database identity and repositories verified |
| PostgreSQL source-of-truth | PASS | Live counts and repository-composition checks |
| pgvector retrieval | PASS | READY status; real `Python automation` query returned 3 results |
| HH conversation GET | PASS | Authenticated typed targeted read/sync, fetched 1, errors 0 |
| HH bounded sync | PASS | Career bounded run: processed 3, errors 0 |
| Vacancy analysis | PASS | Accepted real Mistral typed-analysis evidence |
| Application attempt gate | PASS | Empty PostgreSQL store; canonical not-found and blocking tests |
| Application preparation | PASS | Real PostgreSQL/Mistral harness result `PREPARED` |
| Application dry-run/write boundary | PASS | `BLOCKED_BY_DRY_RUN`; no writer or reservation |
| Employer reply draft | PASS | Accepted real draft evidence and safety regressions |
| Unknown-fact safety | PASS | Unknown remains review/candidate-input; no unsupported claim |
| Follow-up | PASS | PostgreSQL read-only workflow evaluation; no send |
| Auto-chat review | PASS | Fixed cursor; no HTTP 400, send, leave, or eligible action |
| Career bounded one-run | PASS | `completed_bounded_scope`, bound 3, errors 0 |
| Scheduler composition | PASS | Fixed gate/loader/operator path; maintenance writes disabled |
| Restart | PASS | Same canonical state after one restart |
| Dashboard | PASS | Six required endpoints returned HTTP 200 |
| Data integrity | PASS | All required orphan/duplicate/dimension checks zero |

# Regression

| Check | Result |
|---|---|
| `gofmt -l .` | PASS; no output |
| `git diff --check` | PASS |
| `go test -count=1 ./...` | PASS |
| `go test -race ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go build ./cmd/hh-ai-responder` | PASS |
| `node --check web/app.js` | PASS |
| `node --check internal/runtime/web/app.js` | PASS |
| Docker | NOT APPLICABLE |

# HH safety

| Control | V2.Close result |
|---|---|
| HH_DRY_RUN | true |
| HH_WRITE_ENABLED | false |
| Vacancy response writes | 0 |
| Application/test writes | 0 |
| Chat sends | 0 |
| Chat leaves | 0 |
| Resume touches | 0 |
| Job-status writes | 0 |
| Other HH mutations | 0 |
| LIVE HH WRITES | 0 |
| HH WRITE RETRY | NONE / unchanged |

The existing local audit store contains historical entries, including 3 prior
`sent` records and 3 corresponding `hh_write` message projections. Its 57
events and 13 approved actions were unchanged during V2.Close; they are not
V2.Close deltas.

# Final decision

V2: **PASS**

POSTGRESQL + PGVECTOR CUTOVER: **ACCEPTED**

STORAGE / REFACTOR ACCEPTANCE TRACK: **CLOSED**

PROJECT: **READY FOR PRODUCT DEVELOPMENT**

Product development is not started by this acceptance stage. HH writes remain
disabled, and no migration, reindex, application, chat send, chat leave, resume
touch, or job-search status update was performed.
