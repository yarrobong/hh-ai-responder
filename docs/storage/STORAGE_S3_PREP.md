# Executive summary

S3.Prep: **BLOCKED**

S3 user-data apply: **NOT STARTED**

The dedicated PostgreSQL destination and semantic embedding configuration are ready. Recovery produced usable temporary vacancy records for 142 of 161 missing non-zero vacancy IDs. Nineteen provider IDs were unavailable to the read adapter, and two conversations with `vacancy_id=0` had no deterministic provider vacancy mapping. No source JSON was changed and no HH mutation was performed.

# PostgreSQL

Local server: PASS — local PostgreSQL 14.19, reachable through the Unix socket as role `Yaroslav`.

Database: `hh_ai_responder_s3`

Identity: PASS — `current_database() = hh_ai_responder_s3`, schema `public`, user `Yaroslav`.

Destination classification before schema setup: EMPTY.

Schema through: `000010_candidate_semantic_contract` — migrations 000001 through 000010 applied.

pgvector: PASS — extension `vector` version `0.8.6`.

Semantic schema: PASS — `candidate_semantic_documents.embedding` is `vector`; dimensions are stored separately, constrained to `1..16000`, and checked with `vector_dims(embedding)`.

All PostgreSQL user tables were verified empty after schema setup and dry-runs. Migration bookkeeping contains the schema versions only.

Docker: NOT APPLICABLE.

# Configuration

`STORAGE_BACKEND`: `postgres`

`DATABASE_URL`: CONFIGURED — verified through the project PostgreSQL adapter against `hh_ai_responder_s3`; the DSN uses local socket authentication and contains no invented password.

`HH_CANDIDATE_ID`: `candidate-local`

`HH_DRY_RUN`: `true`

`HH_WRITE_ENABLED`: `false`

The existing `HH_AI_BASE_URL`, `HH_AI_MODEL`, and `HH_AI_API_KEY` values were preserved. `EMBEDDING_BASE_URL` and `EMBEDDING_API_KEY` remain unset; the project fallback resolves them from the existing `HH_AI_*` configuration.

The pre-change `.env` backup is at `/tmp/hh-ai-responder-s3prep-20260910T000000Z/.env.20260910T000000Z.bak`. It is outside the repository and its contents are not included here.

# Embedding

Provider: `openai-compatible`

Model: `mistral-embed`

Configured dimensions: `1024`

Live vector dimensions: `1024`

Smoke: PASS — exactly one harmless request through the project embedding adapter; all returned values were finite.

SpaceID: `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9`

No Candidate rows were created and no semantic reindex was run.

# Original source integrity

PASS. The following original JSON sources were hashed before and after recovery; the before/after sets are identical:

`vacancies.json`, `job_applications.json`, `employer_conversations.json`, `candidate_profile.json`, `candidate_stories.json`, `candidate_skills.json`, `candidate_projects.json`, `candidate_achievements.json`, `candidate_unknowns.json`, `candidate_proposals.json`, `candidate_events.json`.

The temporary migration source is `/tmp/hh-ai-responder-s3prep-20260910T000000Z/source`. The recovery machine report is `/tmp/hh-ai-responder-s3prep-20260910T000000Z/hh-recovery.json`. Neither path is tracked by Git.

# Missing vacancy recovery

Initial source counts recomputed from the actual files:

- Conversations total: `261`
- Conversations with vacancy present: `98`
- Conversations with missing vacancy: `163`
- Unique missing non-zero VacancyIDs: `161`
- Conversations with `vacancy_id=0`: `2`

Recovery results:

- Complete local exact vacancy records: `0`
- Local exact partial conversation metadata used: `161` IDs
- Recovered from HH exact vacancy reads: `142` IDs with HTTP 200
- HH exact reads returning HTTP 403: `18` IDs
- HH exact reads returning HTTP 404: `1` ID
- Zero-ID exact recoveries: `0`
- Temporary vacancies added: `161`
- Remaining unresolved recovery records: `21` conversations

The temporary source contains `279` unique vacancies: the original `118` plus the `161` exact-ID recovery records. Existing original vacancy records always win on duplicate IDs. Recovered records contain only exact ID, provider-linked title/company metadata, canonical provider link, and the description returned by the read adapter when available. Missing fields remain empty and records are marked partial.

One returned description contained the storage adapter's secret-marker words `authorization` and `password`. To keep the temporary source within the repository's secret rejection contract, that optional description was omitted from the temporary JSON and retained only in the untracked recovery artifact with a `storage_secret_marker_guard` note. No replacement text was invented.

# Unresolved conversations

Category A — provider vacancy ID known, exact vacancy read unavailable:

- HTTP 403: `134105512`, `134265607`, `134309947`, `134363946`, `134441846`, `134582431`, `134587035`, `134621257`, `134668017`, `134674229`, `134704057`, `134717087`, `134757158`, `134841763`, `134855429`, `134888306`, `135025044`, `135401801`
- HTTP 404: `134671307`

Category B — vacancy ID is `0`, exact provider mapping unavailable:

- `conversation-fc067f67193b032d17f629b901d43daa`, HH chat `5492907458`
- `conversation-3d5d39723c93e2a32bcbb6e315dffc0b`, HH chat `5433708320`

No message bodies are included in this report. The two zero-ID conversations and all of their source messages remain in the temporary source; they were not dropped or rewritten.

# Temporary migration source

The source copy includes the repository-authoritative career files and the candidate files required by `LoadCandidateMigrationSource`:

`vacancies.json`, `job_applications.json`, `employer_conversations.json`, `candidate_profile.json`, `candidate_stories.json`, `candidate_skills.json`, `candidate_projects.json`, `candidate_achievements.json`, `candidate_unknowns.json`, `candidate_proposals.json`, and `candidate_events.json`.

Original files remain untouched.

# Candidate dry-run

Command equivalent: `candidate migrate-postgres --dry-run --source-dir <temporary-source>`

Result: PASS — status `new`, safe to apply `true`, source files unmodified `true`.

Candidate report: `/tmp/hh-ai-responder-s3prep-20260910T000000Z/candidate-dry-run.json`.

No Candidate rows were committed.

# Career dry-run

Command equivalent: `storage migrate-postgres --dry-run --source-dir <temporary-source>`

Result: BLOCKED by the two explicit zero-ID relation errors.

- Vacancies: source `279`, planned new `279`, conflicts `0`
- Applications: source `98`, planned new `98`, conflicts `0`
- Application events: source `308`, planned new `308`, conflicts `0`
- Conversations: source `261`, planned new `261`, conflicts `2` critical relations
- Messages: source `793`, planned new `793`, conflicts `0`
- Relation errors: `2`, both `conversation -> missing vacancy 0`

Consistency accounting is explicit: conversations `261 = 259 recoverable + 2 unresolved`; messages `793 = 791 attached to recoverable conversations + 2 attached to unresolved zero-ID conversations`. The migration command reports all source records and does not silently skip them.

Career report: `/tmp/hh-ai-responder-s3prep-20260910T000000Z/career-dry-run.json`.

No career rows were committed.

# R14 attempt authority

`application_attempts.json`: absent, record count `0`.

`autochat_attempts.json`: absent, record count `0`.

Destination `automatic_application_attempts`: `0` rows, no states.

Destination `legacy_auto_chat_attempts`: `0` rows, no states.

The S3 apply must keep both attempt tables empty unless a later explicit R14 migration defines the source authority and preserves idempotency, reconciliation state, provider identifiers, and replay safety. This prep stage did not migrate attempts.

# Controlled action access

`hh_write_actions.json` remains reachable and valid at version `1` with `13` actions. It was read-only inspected and not migrated or changed.

# Regression

PASS:

- `go test -count=1 ./...`
- `go test -race ./...`
- `go vet ./...`
- `go build ./...`
- `go build ./cmd/hh-ai-responder`
- `git diff --check`
- `gofmt -l .` returned no files
- `node --check web/app.js`

# HH safety

Logical HH GET operations: `165` — 161 exact vacancy reads, 2 exact conversation reads for zero-ID recovery, and 2 read-only responder setup reads. The adapter used GET-only capabilities; no mutation-capable HH client was used for recovery.

Mutation requests: `0`

LIVE HH WRITES: `0`

`HH_DRY_RUN=true` and `HH_WRITE_ENABLED=false` remained enforced throughout. No application, chat send, leave, resume touch, job-search status update, test submission, or other HH write was performed.

# Decision

S3 APPLY: **BLOCKED ONLY ON EXPLICIT UNRESOLVED-CONVERSATION DECISION**

Do not run candidate `migrate-postgres --apply`, career/storage `migrate-postgres --apply`, or semantic `reindex --apply` until the 21 unresolved recovery records have an explicit decision. Do not start V2 or feature development from this stage.
