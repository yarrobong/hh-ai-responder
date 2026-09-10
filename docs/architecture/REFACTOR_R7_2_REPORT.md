# Before

Postgres Application:

The PostgreSQL application repository, its SQL, scanners, event ledger, and
mutation helpers lived in the root package. The mixed source file was 1,456
lines and also contained the Conversation PostgreSQL repository.

Shared Application/Conversation source:

`postgres_application_conversation_repository.go` contained both repositories,
the root `postgresDBTX` mechanic, relation SQL, and root compatibility
composition.

SQL:

Application SQL covered `applications` and `application_events`, including
current-state reads/writes, external-ID lookup, relation updates, and event
history reads/inserts.

Scanner:

The application scanner reconstructed all persisted application fields,
including timestamps plus nanosecond columns and JSONB match, HH metadata, and
reconciliation values. The event scanner reconstructed immutable event IDs,
application IDs, type, timestamp, and description payload.

Events:

`Create` inserted a `created` event. A new `UpsertImported` row also inserted a
`created` event; an existing imported row was updated without fabricating
history. `Import` inserted only the supplied snapshot and `ImportEvent` appended
supplied history. Status and match mutations appended events in the same
application mutation transaction.

Relations:

Application rows stored scalar `vacancy_id` and `conversation_id` values. The
existing attach operation also updated the normalized SQL-side
`conversations.application_id` link; it did not call a Conversation repository.

Transactions:

Pool-backed Create, Update, UpsertImported, relation, status, match, and event
mutations used an owned transaction. Transaction-bound repositories used the
caller transaction and did not start another one. Import and ImportEvent
retained their existing direct-execution behavior.

Errors:

Application identity errors are now shared from `internal/application` rather
than owned by JSON storage. PostgreSQL-specific translation remains in the
Postgres adapter. External-ID uniqueness remains distinct; other unique
violations retain the prior repository-conflict behavior.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `PostgresApplicationRepository` | root mixed file | `postgresstorage.ApplicationRepository` | Move implementation; keep root alias |
| `applicationColumns`, insert/update SQL | root mixed file | Postgres application adapter | Move all runtime Application SQL |
| Application scanner and JSONB codec use | root mixed file | Postgres application adapter + shared PG mechanics | Move scanner; preserve NULL decoding |
| `Create`, `Update`, `UpsertImported`, `Import` | root mixed file | Postgres application adapter | Move unchanged semantics |
| `AttachConversation*` | root mixed file | Postgres application adapter | Move relation SQL; no repository dependency |
| Status/match/follow-up mutations | root mixed file | Postgres application adapter | Move persistence only |
| Application event insert/scanner/Timeline/ListEvents | root mixed file | Postgres application adapter | Move append-only ledger |
| `conversationColumns`, conversation SQL/scanners | root mixed file | root mixed file | Deliberately leave for R7.3 |
| Candidate SQL/versioned aggregate | root files | root files | Deliberately leave for R7.4 |
| `postgresDBTX`, timestamp/nullable JSON mechanics | root and vacancy adapter | adapter-local/shared package mechanics | Reuse only where already justified |
| status/source/event validation | root wrappers and domain | `internal/application` | Reuse domain rules |
| application identity sentinels | JSON adapter | `internal/application` | Narrow cross-backend consolidation |
| Career transaction composition | root | root | Constructor compatibility only |
| migrations | migrations | migrations | No changes |

# New adapter

Implementation:

`postgresstorage.ApplicationRepository` in
`internal/adapters/storage/postgres/application.go`.

Port:

`ports.ApplicationStore`, with compile-time assertions for
`ApplicationReader`, `ApplicationWriter`, and `ApplicationStore`.

Dependencies:

`internal/application`, `internal/conversation`, `internal/vacancy`,
`internal/ports`, and the required pgx packages. There is no root-package,
JSON-adapter, Candidate, HH, AI, Dashboard, or CLI dependency.

# SQL contract

Tables:

- `applications`
- `application_events`
- `conversations` only for the existing normalized relation attach update

Application columns:

`id`, `external_id`, `vacancy_id`, `conversation_id`, `company_name`,
`vacancy_title`, `vacancy_url`, `source`, `status`, `raw_status`, `created_at`,
`updated_at`, `created_at_ns`, `updated_at_ns`, `follow_up_state`, `notes`,
`next_action`, `match_result`, `hh_metadata`, `partial`, `data_completeness`,
`reconciliation_evidence`.

Event columns:

`sequence`, `id`, `application_id`, `event_type`, `created_at`, `created_at_ns`,
`payload`.

JSONB:

`match_result`, `hh_metadata`, and `reconciliation_evidence` retain the existing
nullable JSONB codec. SQL NULL decodes to nil/zero; non-NULL JSON is decoded
without changing its domain representation. Event payload remains an object
containing `description`.

Nullability:

The schema stores `conversation_id` as non-null empty-string-compatible text;
the scanner also accepts SQL NULL and maps it to the domain empty string.
JSONB match, HH metadata, and reconciliation evidence preserve nil. Timestamp
columns are non-null at the schema level, with the `*_ns` columns preserving
nanosecond precision. The Conversation-side `application_id` relation remains
nullable and is not loaded by this aggregate scanner.

No schema changes:

YES

# Application operations

Create:

Preserves explicit IDs, otherwise generates the existing
`application-<random-hex>` form; applies manual/discovered defaults and
timestamps; inserts the row and a created event atomically.

Import/upsert:

`Import` is insert-only and does not synthesize history. `UpsertImported`
preserves existing local ID and CreatedAt, updates the existing row without a
new event, and creates both row and created event for a new external ID.

Update:

Preserves identity and CreatedAt fallback behavior, validates through
`application.JobApplication.Validate`, and updates current persisted values.

Relations:

Stores normalized Vacancy and Conversation IDs and preserves the existing
vacancy consistency and SQL-side Conversation link checks. No repository-to-
repository call is made.

Status:

Uses `application.EventTypeForStatus`; status update and event append remain in
one pool-backed transaction.

Match:

Validates/normalizes and persists the current `vacancy.MatchResult`; changed
results append the existing matched event.

FollowUp:

Persists the normalized `conversation.FollowUpState` value only. Policy and
scheduling remain outside the adapter.

Save:

Context/configuration check and no-op, because PostgreSQL mutations persist
immediately.

# Event contract

Created event:

Generated by Create and newly inserted UpsertImported rows; not generated by
Import.

Status events:

Mapped by `internal/application.EventTypeForStatus`.

Append:

Inserts a new immutable event row after verifying the application exists.

Duplicate:

Existing PostgreSQL unique/error behavior is retained; event rows are never
updated or replaced.

Timeline:

Reads Application events only with `ORDER BY created_at, sequence`, preserves
empty results, and returns Application-not-found for a missing application.

Append-only:

PASS

# Transaction semantics

Pool-backed application mutations preserve the prior owned transaction shape.
Status and event insertion are atomic in that transaction. Transaction-local
construction uses `NewApplicationRepositoryForTx`; the pgx transaction does
not leak through `ports`. Import and ImportEvent remain direct operations as
before, so no stronger atomicity was invented for those calls.

# IDs

Application:

Explicit IDs are retained. Generated IDs retain the `application-` prefix and
16-byte random hex payload.

Event:

Generated IDs retain the `application-event-` prefix and 16-byte random hex
payload. Supplied import event IDs are retained unchanged.

External:

HH negotiation/application IDs remain opaque strings. Lookup keeps the prior
empty-ID filtering and not-found behavior.

# Errors

Not found:

Shared application/conversation sentinels preserve `errors.Is`; PostgreSQL
`pgx.ErrNoRows` is translated at the adapter boundary.

Duplicates:

The external-ID unique index maps to the shared duplicate-external sentinel.
Other unique violations remain repository conflicts, matching the prior
PostgreSQL behavior.

Relations:

Foreign-key and relation mismatch behavior is preserved, including vacancy and
application not-found translation.

DB:

Non-semantic PostgreSQL errors remain wrapped with their cause; constraint
names are not exposed as business errors.

# JSON/Postgres parity

Shared:

Domain round trips, external lookup, scalar relations, status, match,
follow-up, event append, created/status event semantics, and Application-only
Timeline behavior use the same R6.1 contracts.

Intentional differences:

JSON requires explicit Save and keeps an in-memory snapshot; PostgreSQL writes
immediately. PostgreSQL Import/ImportEvent are migration-oriented direct
operations, while JSON snapshot replacement is an adapter-specific durability
operation.

Unresolved differences:

NONE

# Root compatibility

Aliases:

`PostgresApplicationRepository` aliases
`postgresstorage.ApplicationRepository`.

Wrappers:

`NewPostgresApplicationRepository` and the transaction-local constructor remain
in the root package and delegate to the adapter.

Runtime Application SQL in root:

NONE. Remaining root references are aliases, composition, tests, migration
cleanup, or Conversation code that happens to enforce the application FK.

# Shared helper outcome

Moved:

Application SQL constants, scanner, event scanner/insert, application-specific
JSONB argument construction, ID/event generation, error translation, and
transaction helper.

Still root:

Conversation SQL/scanners/helpers, Candidate persistence, root composition, and
the generic root mechanics needed by those not-yet-migrated adapters.

Reason:

R7.2 moves Application only. A broad generic repository abstraction or
Conversation extraction would violate the stage boundary.

# Deliberately deferred

Conversation PostgreSQL:

ROOT

Candidate PostgreSQL:

ROOT

Semantic:

DEFERRED

Migrations:

UNCHANGED

Composition:

ROOT

# Tests

Pure/scanner:

`internal/adapters/storage/postgres/application_test.go` covers scanner
round-trip, NULL relation/JSONB behavior, nanosecond timestamps, event scan,
and PostgreSQL error translation.

JSON:

Existing JSON application adapter tests pass.

Postgres integration:

SKIPPED — `POSTGRES_TEST_DATABASE_URL` is not configured. Existing opt-in root
integration tests remain unchanged and continue to skip without a database.

Root regressions:

`go test ./...` PASS.

# Size

Root Application Postgres LOC before:

The mixed source was 1,456 LOC; the Application-owned ranges were approximately
665 LOC including SQL, repository methods, scanners, and error helpers.

Root compatibility LOC after:

23 LOC in `postgres_application_repository.go`, plus existing composition
references.

Postgres adapter LOC:

809 implementation LOC and 105 focused test LOC.

# Dependencies

Direct adapter dependencies are limited to Application, Conversation, Vacancy,
Ports, pgx, pgconn, pgtype, and pgxpool. No pgx type appears in domain or port
signatures.

# Behavior

Application:

UNCHANGED

JSON:

UNCHANGED

Postgres:

UNCHANGED

Other domains:

UNCHANGED

HH:

UNCHANGED; no HH writes were executed.

Dashboard:

UNCHANGED

# Verification

gofmt:

PASS

go test:

PASS (`go test ./...`)

race:

PASS (`go test -race ./...`)

vet:

PASS (`go vet ./...`)

build:

PASS (`go build ./...`)

diff:

PASS (`git diff --check`)

node:

PASS (`node --check web/app.js`)

Docker:

SKIPPED — Docker CLI is present but the daemon is unavailable.

LIVE HH WRITES:

0

# Ready for R7.3

R7.3 — Conversation PostgreSQL Adapter Boundary

READY

The runtime Application SQL has one owner, Application events remain
append-only, the Application adapter does not import a Conversation repository
implementation, and status/event mapping is delegated to
`internal/application`.
