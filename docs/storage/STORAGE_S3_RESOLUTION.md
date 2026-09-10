# Executive summary

S3.Resolve: **COMPLETE**

S3 Apply: **READY**

The migration scope is resolved without changing the original JSON sources and
without any HH write. The PostgreSQL career and candidate dry-runs both passed
against the resolved temporary source. No PostgreSQL user data was written.

The temporary source and the exclusion manifest are intentionally untracked
artifacts outside the repository:

- source: `/tmp/hh-ai-responder-s3resolve.5f58md/source`
- exclusion manifest: `/tmp/hh-ai-responder-s3resolve.5f58md/S3_RESOLVE_EXCLUSIONS.json`

The prior S3.Prep recovery directory was not available when this stage ran, so
the temporary source was rebuilt deterministically from the immutable source
JSON and the S3.Prep recovery evidence. No HH recovery read was repeated.

# Explicit migration decision

Known non-zero unavailable vacancies: **INCLUDE AS PARTIAL HISTORICAL**.

Unknown zero-ID conversations: **EXCLUDE FROM POSTGRESQL MIGRATION; PRESERVE
IN ORIGINAL LEGACY JSON**.

The two exclusions are explicit scope exceptions, not silent skips. No vacancy
with ID `0` was created, no fuzzy match was used, and the original JSON was not
edited.

# Historical partial vacancies

There are 19 explicitly known non-zero provider vacancy IDs whose current HH
detail reads returned 403 or 404. Their exact IDs remain in the migration
source as `data_completeness=partial` records. They carry only exact provider
identity and source conversation metadata; unavailable fields remain empty.

403 IDs: `134105512`, `134265607`, `134309947`, `134363946`, `134441846`,
`134582431`, `134587035`, `134621257`, `134668017`, `134674229`, `134704057`,
`134717087`, `134757158`, `134841763`, `134855429`, `134888306`, `135025044`,
`135401801`.

404 ID: `134671307`.

No current active/archived state, salary, description, location, experience,
employment, or work format was inferred for these records. The 404/403 result
is not interpreted beyond current detail unavailability.

# Explicit legacy exclusions

Conversations: **2**

Messages: **2**

Reason: no deterministic provider VacancyID.

Original source retained: **YES**.

The untracked manifest records each local conversation ID, HH chat ID, original
`vacancy_id=0`, message count, reason, decision, and the source file SHA-256.
It contains no message bodies or secrets.

# Migration accounting

| Entity | Original | PostgreSQL scope | Explicit legacy exclusion |
|---|---:|---:|---:|
| Vacancies | 118 | 279 | 0 |
| Applications | 98 | 98 | 0 |
| Application events | 308 | 308 | 0 |
| Conversations | 261 | 259 | 2 |
| Messages | 793 | 791 | 2 |

The differences are fully explained:

`261 = 259 + 2` conversations and `793 = 791 + 2` messages.

# Relation validation

Every included conversation resolves to an exact non-zero VacancyID present in
the temporary vacancy source.

- Missing conversation-to-vacancy relations: **0**
- Missing application-to-vacancy relations: **0**
- Missing application-to-conversation relations: **0**
- Included zero-ID conversations: **0**
- Migration conflicts: **0**
- Silent skips: **0**

# Candidate dry-run

Command: `candidate migrate-postgres --dry-run --source-dir <resolved-source>`

- Status: `new`
- Safe to apply: **true**
- Source files unmodified: **true**
- PostgreSQL candidate rows created: **0**

# Career dry-run

Command: `storage migrate-postgres --dry-run --source-dir <resolved-source>`

- Vacancies: **279**
- Applications: **98**
- Application events: **308**
- Conversations: **259**
- Messages: **791**
- Critical relation errors: **0**
- Conflicts: **0**
- Safe to apply: **true**
- Applied: **false**
- Source JSON files unmodified: **true**

# R14 authority

`application_attempts.json`: absent; treated as empty.

`autochat_attempts.json`: absent; treated as empty.

PostgreSQL attempt tables remain empty. No attempt records were generated from
historical career data.

# Controlled actions

`hh_write_actions.json` remains available locally and unchanged. Current action
count: **13**. It was not migrated or modified.

# Original source integrity

PASS. The original source JSON files remain byte-for-byte unchanged. Their
post-resolution SHA-256 values match the existing S3 source manifest, including:

- `vacancies.json`: `825258d2305e16233684073b56af271118ff887de1552c95f46519b635316b4a`
- `job_applications.json`: `ad56506bea638a3c184541c0bd67d7f77e3e8e260c8e0d43c72e39fe145573bd`
- `employer_conversations.json`: `5e8bc5193e4eaefd2287de4efd7376b4e84eecfd7369f27f25e19e17248d4bdf`

The candidate-source hash set also remains identical to the S3 source manifest.

# PostgreSQL destination state

Destination database: `hh_ai_responder_s3`.

After both dry-runs, all PostgreSQL user tables remain empty:

- vacancies: `0`
- applications: `0`
- application events: `0`
- conversations: `0`
- conversation messages: `0`
- candidates: `0`
- automatic application attempts: `0`
- legacy auto-chat attempts: `0`

Schema bookkeeping contains 10 applied schema rows. No user-data transaction
was committed.

# Embedding contract

The existing S3.Prep smoke remains applicable; no new live embedding request
and no reindex were performed.

- Provider: `openai-compatible`
- Model: `mistral-embed`
- Dimensions: `1024`
- SpaceID: `sha256:1ae6b8f2cca884672780f5bca9d51c03b70f8af1731072353ab272a5d0e531b9`

# HH safety

Effective safety configuration remains `HH_DRY_RUN=true` and
`HH_WRITE_ENABLED=false`.

- HH requests in S3.Resolve: **0**
- LIVE HH WRITES: **0**

No application, test submission, chat message, chat leave, resume touch,
job-search status update, or other HH write was performed.

# Verification

The following checks were run after this report was written and all passed:

- **PASS** `go test -count=1 ./...`
- **PASS** `go test -race ./...`
- **PASS** `go vet ./...`
- **PASS** `go build ./...`
- **PASS** `go build ./cmd/hh-ai-responder`
- **PASS** `git diff --check`
- **PASS** `gofmt -l .`
- **PASS** `node --check web/app.js`

# Decision

All S3.Resolve acceptance conditions pass.

S3 APPLY: **READY**
