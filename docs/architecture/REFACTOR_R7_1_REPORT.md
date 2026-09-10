# Before

Postgres Vacancy repository:

The authoritative `PostgresVacancyRepository`, Vacancy SQL, row scanner,
JSONB encoding/decoding, timestamp helpers, ID allocation, and PostgreSQL
error translation lived in the root package in `postgres_vacancy_repository.go`.
Root composition, career migration, and reconciliation referred to that type
directly.

Shared infrastructure:

The root PostgreSQL package still owns the shared `postgresDBTX`, context,
timestamp, and nullable-JSON helpers required by the deferred Application,
Conversation, Candidate, and semantic implementations. R7.1 did not move
those repositories or process-level infrastructure.

Connection ownership:

Root composition still reads configuration, opens `*pgxpool.Pool`, applies
migrations, selects JSON versus PostgreSQL, and owns pool shutdown.
`postgresstorage` receives a typed pool or transaction and does not read
configuration or environment variables.

Validation:

Intrinsic normalized Vacancy validation was previously reachable through the
JSON adapter (`root validateVacancy -> jsonstorage.ValidateVacancy`). The
rules now live in `internal/vacancy.Validate`. JSON keeps only JSON-file
collection validation and secret/file-format checks.

SQL:

All runtime PostgreSQL Vacancy SELECT/INSERT/UPDATE statements and the
Vacancy ID-allocation query moved to the adapter. Migrations and test cleanup
SQL remain where they were.

Scanner:

The Vacancy row scanner and JSONB/timestamp conversion now live in the
PostgreSQL adapter. The root package retains only generic mechanical helpers
needed by repositories intentionally deferred to R7.2-R7.4.

Errors:

Vacancy not-found and duplicate identity sentinels are now defined in
`internal/vacancy` and aliased by both JSON and PostgreSQL adapters. PostgreSQL
unique violations are translated to those domain sentinels.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| `PostgresVacancyRepository` | root | `postgresstorage.VacancyRepository` | Move implementation; retain root type alias |
| `NewPostgresVacancyRepository` | root | `postgresstorage.NewVacancyRepository` plus root wrapper | Compatibility wrapper |
| `newPostgresVacancyRepositoryTx` | root | adapter `NewVacancyRepositoryForTx` plus root wrapper | Preserve transaction composition |
| Vacancy SELECT/INSERT/UPDATE SQL | root | `internal/adapters/storage/postgres/vacancy.go` | Move |
| Vacancy row scanner | root | `internal/adapters/storage/postgres/vacancy.go` | Move |
| Vacancy JSONB/timestamp helpers | root Vacancy file | PostgreSQL adapter | Move Vacancy-specific mechanics |
| `postgresDBTX` | root shared repository file | root | Keep for Application/Conversation/Candidate |
| `postgresTimeExact`, `decodeNullableJSON`, nullable SQL helpers | root shared helpers | root compatibility helpers | Keep for deferred repositories |
| Vacancy validation | JSON adapter/root wrapper | `internal/vacancy.Validate` | Consolidate shared semantic rule |
| JSON file validation | JSON adapter | JSON adapter | Keep JSON-specific |
| Pool creation and backend selection | root | root | Keep composition ownership |
| Migration runner and `migrations/` | root | root | Unchanged |
| Application PostgreSQL | root | root | Deferred to R7.2 |
| Conversation PostgreSQL | root | root | Deferred to R7.3 |
| Candidate PostgreSQL/CandidateTx | root | root | Deferred to R7.4 |
| Semantic/pgvector | root | root | Deferred to R8 |

# New Postgres adapter

Package:

`internal/adapters/storage/postgres` with package name `postgresstorage`.

Files:

- `vacancy.go` — Vacancy repository, SQL, scanner, JSONB/timestamp mechanics,
  duplicate translation, and typed pool/transaction constructors.
- `vacancy_test.go` — scanner, JSONB, timestamp, response-count, and duplicate
  error characterization tests without requiring PostgreSQL.

Dependencies:

`postgresstorage` imports `internal/vacancy`, `internal/ports`, pgx/pgtype,
pgxpool, and the standard library. It does not import the JSON adapter or the
root package.

# Vacancy repository

Port:

`ports.VacancyStore`, through `ports.VacancyReader` and
`ports.VacancyWriter`.

Create:

Preserves caller-supplied IDs and timestamps. If the legacy PostgreSQL
compatibility path receives ID zero, it allocates the next ID under the same
advisory transaction lock and applies the existing timestamp defaults. Each
operation is committed transactionally when the repository owns the pool
transaction.

Update:

Locks and reads the existing creation timestamp, preserves it when omitted,
applies the existing update timestamp behavior, validates the normalized
Vacancy, and translates unique violations.

Get:

Reads by local ID with the existing normalized domain return type.

GetByExternalID:

Uses the opaque `external_id` value exactly as supplied, retaining the
existing `ORDER BY id LIMIT 1` behavior for a corrupt duplicate state.

List:

Implements `ports.VacancyQuery` for local ID and external ID filters and
preserves `ORDER BY id`.

Save:

Remains a compatibility no-op after checking context and repository
configuration. PostgreSQL Create/Update are already durable operations; no
in-memory batching was introduced.

# SQL contract

Tables:

`vacancies` only. No schema or migration files changed in R7.1.

Columns:

`id`, `external_id`, `name`, `title`, `description`, `requirements`, `skills`,
`salary`, `salary_currency`, `location`, `work_format`, `employment_type`,
`source`, `published_at`, `hh_updated_at`, `hh_metadata`, `created_at`,
`updated_at`, `published_at_ns`, `hh_updated_at_ns`, `created_at_ns`,
`updated_at_ns`, `work_schedule`, `work_experience`, `links`,
`total_responses_count`, `area_name`, `company_id`, `company_name`,
`company_site_url`, `compensation`, `creation_time`, `last_change_time`,
`user_labels`, `response_letter_required`, `user_test_present`, `archived`,
`response_url`, `total_responses_count_known`, `match_result`,
`application_recommendation`, `data_completeness`, and
`reconciliation_evidence`.

JSONB:

`requirements`, `skills`, `hh_metadata`, `links`, `compensation`,
`last_change_time`, `user_labels`, `match_result`,
`application_recommendation`, and `reconciliation_evidence` retain their
existing JSON representation. Matching and reconciliation values are stored
and loaded without running policy or AI logic.

Nullability:

Database NULL JSONB values decode to nil domain fields. Zero domain times are
written as NULL, and non-zero times use both the existing timestamptz column
and nanosecond companion column. The scanner prefers the nanosecond companion
when present and falls back to timestamptz for older rows.

No schema changes:

YES.

# Validation

Intrinsic:

`internal/vacancy.Validate` owns non-negative ID and paired/ordered timestamp
rules.

JSON-specific:

`jsonstorage.ValidateVacancies` retains JSON snapshot duplicate detection,
version/file validation, and secret-marker protection.

Postgres-specific:

Database unique constraints and PostgreSQL error-code/constraint translation
remain in `postgresstorage`.

Old JSON-owned shared validation:

REMOVED. `jsonstorage.ValidateVacancy` no longer exists. JSON persistence calls
`vacancy.Validate` directly, while `jsonstorage.ValidateVacancies` retains only
JSON snapshot collection checks; PostgreSQL calls the same domain rule directly
and has no JSON adapter dependency.

# IDs

Local IDs remain caller-supplied when present. The existing zero-ID PostgreSQL
compatibility allocation (`advisory lock` plus `MAX(id)+1`) is unchanged.
External IDs remain opaque strings, with the existing partial unique index
semantics for non-empty values. No primary-key type or allocation strategy
was changed.

# TotalResponsesCount

Unknown:

`total_responses_count_known = false` remains unknown even when the integer
column is zero.

Explicit zero:

`total_responses_count = 0` with `total_responses_count_known = true` remains
known zero.

Non-zero:

The integer count and independent known bit are preserved.

Regression:

PASS. Adapter scanner coverage verifies explicit zero versus unknown, and the
existing opt-in PostgreSQL contract remains available.

# Error semantics

Not found:

`pgx.ErrNoRows` is translated to the shared `vacancy.ErrVacancyNotFound`
sentinel; root and JSON compatibility aliases continue to match it.

Duplicate:

PostgreSQL `23505` violations for the primary key and Vacancy external-ID
constraint map to shared duplicate sentinels. Constraint names do not become
business-facing API.

Context:

Canceled/deadline contexts are returned before work where already canceled;
the same context is passed to pgx operations with no background replacement or
hidden goroutines.

DB:

Other database errors remain wrapped with operation context and preserve their
cause for `errors.Is`/`errors.As`.

# Root compatibility

Aliases:

`type PostgresVacancyRepository = postgresstorage.VacancyRepository` remains
in the root package.

Wrappers:

`NewPostgresVacancyRepository` and the transaction-local constructor delegate
to the adapter. Reconciliation uses the adapter's composition-only `Pool()`
inspection method rather than reaching into an unexported adapter field.

Runtime Vacancy SQL in root:

NONE.

# JSON parity

JSON behavior remains unchanged semantically. Its repository still batches
mutations in memory, uses explicit Save, preserves normalized Vacancy values,
and delegates intrinsic validation to the domain package. Existing JSON
adapter and root contract tests pass.

# Port compliance

VacancyReader:

PASS.

VacancyWriter:

PASS.

VacancyStore:

PASS.

Compile-time assertions are located in `postgresstorage`; JSON assertions
remain in `jsonstorage`.

# Postgres integration

Environment:

SKIPPED — `POSTGRES_TEST_DATABASE_URL` was not configured.

Results:

The root opt-in PostgreSQL Vacancy contract was selected and passed its
non-database configuration checks; the live database contract was skipped by
its existing convention. No development database was touched.

# Deliberately deferred

Application PostgreSQL:

ROOT.

Conversation PostgreSQL:

ROOT.

Candidate PostgreSQL:

ROOT.

Semantic:

ROOT / DEFERRED.

Migrations:

UNCHANGED.

Composition:

ROOT.

# Dependencies

The verified dependency direction is:

```text
internal/vacancy  -> standard library
internal/ports    -> internal/vacancy
jsonstorage       -> internal/vacancy + internal/ports + platform
postgresstorage   -> internal/vacancy + internal/ports + pgx/stdlib
```

`postgresstorage` has no dependency on `jsonstorage` or the root package.
`internal/vacancy` has no adapter, pgx, or root dependency.

# Tests

Added focused domain validation characterization and PostgreSQL scanner/error
tests. Existing root Vacancy, JSON adapter, repository, migration, and
PostgreSQL opt-in tests were preserved.

# Size

Root Postgres Vacancy LOC before:

497.

Root compatibility LOC after:

21.

Postgres adapter LOC:

565 implementation lines in `vacancy.go` (plus 91 focused test lines).

# Behavior

Vacancy:

UNCHANGED.

JSON:

UNCHANGED.

Postgres:

UNCHANGED semantics; implementation owner moved.

Other domains:

UNCHANGED and deliberately not migrated.

HH:

UNCHANGED.

Dashboard:

UNCHANGED.

# Verification

gofmt:

PASS.

go test:

PASS — `go test -count=1 ./...`.

race:

PASS — `go test -race ./...`.

vet:

PASS — `go vet ./...`.

build:

PASS — `go build ./...`.

diff:

PASS — `git diff --check`.

node:

PASS — `node --check web/app.js`.

Docker:

SKIPPED — Docker CLI is present, but the daemon is unavailable.

LIVE HH WRITES:

0.

# Ready for R7.2

R7.2 — Application PostgreSQL Adapter Boundary

READY.

R7.1 stopped after the PostgreSQL Vacancy adapter boundary. No Application,
Conversation, Candidate, semantic, HH, dashboard, or composition extraction
was started.
