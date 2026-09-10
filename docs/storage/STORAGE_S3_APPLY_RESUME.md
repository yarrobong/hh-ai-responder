# Executive summary

S3.Apply.Resume: **COMPLETE**

S3: **COMPLETE**

Career data was imported into PostgreSQL from a fresh immutable Resume
snapshot. Candidate APPLY was not rerun. Candidate semantic parity remained
valid, the semantic index was bootstrapped with the accepted contract, and
PostgreSQL/runtime smoke checks passed. No live HH mutation was performed.

V2 is **READY**, but was not started.

## Checkpoint

Candidate: `candidate-local`, PostgreSQL count `1`.

Candidate semantic parity: **PASS**. Candidate migration dry-run before and
after Career APPLY reported `already_present`, `safe_to_apply=true`.

Candidate re-import: **NO**. No Candidate APPLY was run in S3.Apply.Resume.

## Migration source

The previous S3 temporary source was absent. It was reconstructed
deterministically from the preserved S3.Prep source and the accepted
S3.Resolve exclusion decision. No broad HH recovery was repeated.

Fresh immutable snapshot:

- Path: `/tmp/hh-ai-responder-s3resume.WEXrSP`
- Created: `2026-09-10T18:16:37+0500`
- Snapshot permissions: read-only; writable entries after freeze: `0`
- Logical counts: `279 / 98 / 308 / 259 / 791`

Files and SHA-256:

| File | SHA-256 |
|---|---|
| `candidate_achievements.json` | `59d538b355c5365962108f2cc8684c62c075d9058a2fb1f459a4ae575a2a2458` |
| `candidate_events.json` | `af6b5fafded42cd05a9d3622c8ca599cd2395bb38f960d17219f3389eadc0f86` |
| `candidate_profile.json` | `303e72196f213cb2dbccb0095006347875feb3f18936d10ade238081a08f9129` |
| `candidate_projects.json` | `bd176d84d8dae674418d91693cc3f7c8db8af338147cd02db1084c63fb7d9690` |
| `candidate_proposals.json` | `923f6894fafca17c4fad6ec5e2537ebe4f6cc309688f5c6f8f1d35c25d87eb61` |
| `candidate_skills.json` | `4aaeb91c5ebd05d9e725d8f35d2cbe3d780e801ff4d8c9f7d3a92d6de77f5eab` |
| `candidate_stories.json` | `90429f909c259aad5ce739fd06662e68697d7d46a38abf21b39d9a3a764f47eb` |
| `candidate_unknowns.json` | `bc8248236ed5ebc501f3b95347dc189c25e0ff668c7586d61b43a664e44fc82c` |
| `employer_conversations.json` | `0817ae7ce4b371312ec3b783396deed0f294f802d0813ca59207f4f62078df85` |
| `job_applications.json` | `ad56506bea638a3c184541c0bd67d7f77e3e8e260c8e0d43c72e39fe145573bd` |
| `vacancies.json` | `bc1f028caefc9875e6ed663580c55d4e6681748eea952bde9cc7ce137a807c1a` |

The frozen source was unchanged after migration and semantic work.

## PostgreSQL before Career apply

Database identity: `hh_ai_responder_s3`; schema `public`; role `Yaroslav`;
PostgreSQL `14.19`; pgvector `0.8.6`; schema migrations `000001–000010`.

Before Career APPLY: Candidate `1`; Vacancies `0`; Applications `0`; Events
`0`; Conversations `0`; Messages `0`; Semantic documents `0`; application
attempts `0`; legacy auto-chat attempts `0`.

## Career dry-run

Command: `storage migrate-postgres --dry-run --source-dir <S3_RESUME_SOURCE>`.

**PASS** — source counts `279 / 98 / 308 / 259 / 791`, relation errors `0`,
conflicts `0`, silent skips `0`, `safe_to_apply=true`, no rows committed.

## Career apply

Command: existing `storage migrate-postgres --apply` using the frozen source.

**PASS** — one transaction committed and built-in post-apply verification
reported `279 / 98 / 308 / 259 / 791` with relation errors `0` and conflicts
`0`. No failure recovery, truncation, drop, rerun, or JSON fallback was used.

## Count parity

| Entity | Expected | PostgreSQL | Result |
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

## Identity parity

Exact source-minus-PostgreSQL and PostgreSQL-minus-source set differences were
`0` for VacancyID, application ID, application-event ID, ConversationID, and
MessageID. Post-apply migration planning classified every entity as
`already_present` with `safe_to_apply=true`, confirming current identity and
content equivalence, including timezone-equivalent typed instants.

Application events retained their application association, type, timestamp and
payload. Messages retained provider IDs, sender, direction, timestamp and
content; database row order was not used for comparison.

## Relation integrity

Conversation → vacancy: `0` missing.

Application → vacancy: `0` missing.

Message → conversation: `0` missing.

Invalid zero VacancyID: `0`. Unexpected orphan rows: `0`.

## Partial historical vacancies

All 19 approved unavailable non-zero provider VacancyIDs are present with
their exact IDs and `data_completeness=partial`:

`134105512`, `134265607`, `134309947`, `134363946`, `134441846`, `134582431`,
`134587035`, `134621257`, `134668017`, `134674229`, `134704057`, `134717087`,
`134757158`, `134841763`, `134855429`, `134888306`, `135025044`, `135401801`,
`134671307`.

No unavailable field was enriched or replaced with a factual default.

## Legacy exclusions

Original accounting: `261 conversations = 259 migration scope + 2 explicit
legacy exclusions`; `793 messages = 791 migration scope + 2 exclusions`.

The two zero-ID conversations and their two messages remain in the original
`employer_conversations.json`, are absent from the frozen source, and are
absent from PostgreSQL. No additional exclusion was made.

## R14 authority

PostgreSQL automatic application attempts: `0`.

PostgreSQL legacy auto-chat attempts: `0`.

`hh_write_actions.json`: `13` controlled actions, accessible and unchanged.
The existing local HH audit history was not rewritten; no new S3.Resume write
attempt was added.

## Semantic contract

- Provider: `openai-compatible`
- Model: `mistral-embed`
- Dimensions: `1024`
- SpaceID: `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9`
- Effective endpoint/key: configured; secret values not printed

## Semantic status before

**REINDEX_REQUIRED**. Source was canonical PostgreSQL Candidate. The exact
semantic scope was 3 eligible projects, 5 ineligible entities, 0 indexed
documents, and 0 stale legacy documents. Stories and achievements had no
eligible documents; all 3 eligible documents were projects.

## Embedding smoke

One harmless real embedding request through the project adapter completed via
semantic search before reindex. The adapter accepted the configured
`mistral-embed` contract and returned a valid finite vector for the 1024-
dimension search path. No Candidate row was changed.

## Semantic dry-run

Command: `candidate semantic reindex --dry-run`.

**PASS** — source PostgreSQL Candidate; `new=3`, `changed=0`, `unchanged=0`,
`stale=0`, `ineligible=5`, `applied=false`.

## Semantic apply

Command: existing `candidate semantic reindex --apply`.

**PASS** — 3 project documents embedded and written; `new=3`, `changed=0`,
`stale=0`, `ineligible=5`, `applied=true`.

## Semantic status after

**READY**. Active provider is `openai-compatible`, model `mistral-embed`,
dimensions `1024`, and the expected SpaceID.

## Semantic integrity

All 3 active documents have `candidate_id=candidate-local`, the accepted
provider/model/SpaceID, `embedding_dimensions=1024`, `vector_dims=1024`, a
content hash, a truth snapshot, and finite vector values. No active legacy or
1536-dimensional vector participates in retrieval.

## Semantic retrieval smoke

Three real queries over confirmed Candidate concepts returned non-empty,
`candidate-local` project results in the active SpaceID:

- `Python automation`
- `PostgreSQL integrations`
- `API integration`

Candidate facts/claims/unknowns/provenance were not changed by semantic
indexing. Candidate post-migration dry-run remained `already_present` and
`safe_to_apply=true`.

## Runtime PostgreSQL smoke

Canonical application startup succeeded with `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`; no panic occurred. The dashboard read-only server
started and served local PostgreSQL data for today/home, vacancies,
applications, conversations, inbox, knowledge, reliability, notifications,
and health. Health reported PostgreSQL counts `279` vacancies, `98`
applications and `259` conversations, with write capability
`BLOCKED_BY_DRY_RUN`.

## Dashboard

GET-only smoke: **PASS** for `/api/today`, `/api/dashboard`, `/api/vacancies`,
`/api/applications`, `/api/conversations`, `/api/inbox`, `/api/knowledge`,
both reliability collections, `/api/notifications`, and `/api/health`.

## CLI

Read-only **PASS** for candidate status, semantic status, `hh inbox`, `hh
workflow`, `hh eligible`, `hh write-status`, both reliability readers,
`hh pilot-shortlist`, and `audit`. Reliability readers reported backend
`postgres` and empty attempt collections.

## Application dry-run

The bounded local `hh pilot-shortlist` preparation path ran against PostgreSQL
Candidate, vacancies, applications and conversations; it returned an empty
bounded shortlist without AI or HH mutation. AI remained allowed by the
runtime configuration, while this read-only preparation command did not need
to invoke it. No application was submitted.

## Conversation draft

One PostgreSQL conversation was read and passed through the AI employer-reply
draft flow. The safe result was `need_candidate_input` because the available
trusted facts were insufficient; no unsupported answer was generated. Send:
**NO**. Approve: **NO**. Leave: **NO**. HH was not contacted.

## Restart

The runtime was stopped and restarted. Post-restart health was **PASS**:
Candidate available; Vacancies `279`; Applications `98`; Conversations `259`;
write capability still dry-run blocked; semantic state remained READY.

## Source integrity

All original JSON SHA-256 values still match
`STORAGE_S3_SOURCE_MANIFEST.md`. The original files were not edited. The
frozen Resume snapshot hashes remained unchanged after Career APPLY, semantic
reindex and runtime smoke.

## Source-of-truth matrix

| Data | Canonical source |
|---|---|
| Candidate | PostgreSQL |
| Vacancies | PostgreSQL |
| Applications | PostgreSQL |
| Conversations | PostgreSQL |
| Semantic index | PostgreSQL / pgvector |
| Clarifications | intentional local JSON |
| AI drafts | intentional local JSON |
| Notifications | intentional local JSON |
| Controlled actions/audit | intentional local JSON |
| Sync operational state | intentional local JSON |

No normal Career consumer required the legacy Career JSON stores.

## Regression

S1: **PASS**

S2: **PASS**

S3.Fix-1: **PASS** — candidate semantic parity tests.

R14: **PASS** — reliability/write-safety coverage.

## Verification

Tests: **PASS** — `go test -count=1 ./...`

Race: **PASS** — `go test -race ./...`

Vet: **PASS** — `go vet ./...`

Build: **PASS** — `go build ./...` and `go build ./cmd/hh-ai-responder`

Canonical: **PASS** — runtime and migration commands used PostgreSQL.

Gofmt: **PASS** — `gofmt -l .` returned no files.

Diff: **PASS** — `git diff --check`.

Node: **PASS** — `node --check internal/runtime/web/app.js`.

Docker: **NOT APPLICABLE**.

## HH safety

HH_DRY_RUN: `true`

HH_WRITE_ENABLED: `false`

Vacancy response writes: `0`

Application/test writes: `0`

Chat sends: `0`

Chat leaves: `0`

Resume touches: `0`

Job-status writes: `0`

Other HH mutations: `0`

LIVE HH WRITES: `0`

HH WRITE RETRY: **NONE / unchanged**

## Final decision

S3: **COMPLETE**

Reason: Career count, identity, relation, exclusion and historical-partial
parity passed; Candidate was not reimported; semantic bootstrap reached READY
with exact 1024-dimensional vectors in the accepted SpaceID; PostgreSQL
runtime/dashboard/CLI/restart checks passed; original sources and controlled
actions remained unchanged; and LIVE HH WRITES remained `0`.

## Ready

V2 — PostgreSQL + pgvector Runtime Acceptance: **READY**.

V2 was not started automatically. No broad HH sync, live application, chat
send, conversation leave, resume touch, job-status update, feature
development, or R15 work was started.
