# Before

Postgres Conversation:

The authoritative PostgreSQL Conversation repository, SQL, message persistence,
scanners, codecs, timestamp handling, immutable-history checks, claims, and
PostgreSQL error translation were in the root package. The implementation was
the first 728 lines of `postgres_application_conversation_repository.go`.

Remaining mixed file:

The same file also contained the PostgreSQL Career transaction composition
(`CareerTx`, `PostgresCareerStore`, and its transaction bundle). That
composition remains root-owned, now in a 64-line compatibility file.

SQL:

Conversation SQL covered `conversations` and `conversation_messages`. Claims
and experience claims are fields inside the `conversations.summary` JSONB
document; the current schema has no separate Conversation claim tables.

Messages:

Messages were normalized rows, loaded in `ORDER BY timestamp,sequence` order,
and appended after durable identity lookup. No raw HH payload parsing belonged
to the repository.

Identity:

Conversation and message IDs used the existing `kind-` plus 16 random-byte hex
format. Message duplicates matched local ID or non-empty `(source, external_id)`
and were compared with `conversation.SameMessage`.

Claims:

Candidate claims were conversation annotations. They were appended to summary,
deduplicated by exact value, and validated by the Conversation domain against a
recorded candidate message. They never mutated Candidate Knowledge.

Summary/state:

Summary, normalized status, next action, waiting timestamp, follow-up state,
raw status, activity projections, HH metadata, and relation IDs were persisted
as supplied or calculated by existing aggregate semantics. No policy or AI
logic was present.

Transactions:

Pool-backed mutations used an owned transaction; transaction-bound repositories
used the caller's `pgx.Tx`. PostgreSQL `Save` was a context/configuration check
because mutations are committed immediately.

Errors:

Conversation external-ID uniqueness remained distinct. Message identity
conflicts and unknown uniqueness violations remained repository conflicts.
Not-found and vacancy/message relation errors retained their established
sentinels where the Conversation adapter owns the relation.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `PostgresConversationRepository` | root mixed file | `postgresstorage.ConversationRepository` | Move implementation; keep root alias |
| `conversationColumns`, insert/update SQL | root mixed file | PostgreSQL Conversation adapter | Move all runtime Conversation SQL |
| Conversation scanner and JSONB codecs | root mixed file | PostgreSQL Conversation adapter + shared PG mechanics | Move and preserve NULL/timestamp behavior |
| `Get`, `List`, vacancy lookup, external lookup | root mixed file | PostgreSQL Conversation adapter | Move unchanged query semantics |
| `Upsert`, state, summary, claims | root mixed file | PostgreSQL Conversation adapter | Move persistence only |
| message append, dedup, immutable prefix | root mixed file | PostgreSQL Conversation adapter + domain `SameMessage` | Move without weakening history rules |
| `Timeline` | root mixed file | PostgreSQL Conversation adapter | Move; Conversation messages only |
| `Import`, `ImportMessage` | root mixed file | PostgreSQL Conversation adapter | Preserve migration compatibility |
| `CareerTx`, `PostgresCareerStore` | root mixed file | root compatibility/composition file | Keep root-owned |
| Candidate and semantic PostgreSQL code | root files | root files | Deliberately unchanged |
| Application `AttachConversation` cross-link | Application adapter | Application adapter | Preserve existing cross-aggregate SQL |
| migrations | migrations | migrations | No changes |

# New adapter

Implementation:

`postgresstorage.ConversationRepository` in
`internal/adapters/storage/postgres/conversation.go`.

Port:

`ports.ConversationStore`, with compile-time assertions for
`ConversationReader`, `ConversationWriter`, and `ConversationStore`.

Dependencies:

`internal/conversation`, `internal/ports`, and PostgreSQL/stdlib mechanics only.
There is no root-package, JSON-adapter, Candidate, Candidate-context,
Conversation-policy, HH, AI, Dashboard, or CLI dependency.

# SQL contract

Tables:

- `conversations`
- `conversation_messages`

Columns:

The adapter selects and writes every existing Conversation column: identity and
relations, company/vacancy snapshot, status, created/updated timestamps and
nanosecond companions, HH/activity timestamps and companions, summary,
next-action/waiting fields, follow-up state, raw status, and HH metadata.

Message columns include local/external IDs, timestamp and nanosecond companion,
sender, direction, source, text, system-event flag, content-unavailable flag,
and metadata.

JSONB:

`summary` stores the complete `conversation.Summary`, including candidate and
experience claims. `hh_metadata` and message `metadata` retain their existing
JSON object encoding. Nil/empty JSON remains distinct according to the existing
codec behavior.

Nullability:

`external_id` is `NOT NULL DEFAULT ''`, so an empty HH identity is written as
an empty string. `application_id`, optional timestamps, HH metadata, and message
metadata use SQL NULL for domain empty/nil values. Nanosecond columns are used
when valid, otherwise the PostgreSQL timestamp value is used.

No schema changes:

YES

# Conversation operations

Upsert:

Preserves local-ID and opaque external-ID lookup, existing relation checks,
default status/follow-up values, activity refresh, timestamp merging, message
append behavior, and summary/claim preservation. An incoming snapshot cannot
remove or rewrite the established message prefix or recorded claims.

Get:

Loads the complete Conversation row and its normalized message history. Missing
rows return `conversation.ErrConversationNotFound`.

External lookup:

Uses exact opaque `external_id` matching and ignores empty identities.

List:

Orders conversations by `created_at,id`, then loads each message history with
the existing `timestamp,sequence` ordering.

State:

Stores the caller-provided normalized status, next action, waiting timestamp,
and follow-up state. The adapter does not classify reply requirements or
terminal state.

Summary:

Stores the caller-provided summary and does not call AI or decide summarization.

FollowUp:

`FollowUpState` round-trips as a persisted domain value; no scheduler or
follow-up policy was moved.

Save:

Immediate PostgreSQL mutations remain immediate; `Save` retains its context and
configuration check/no-op behavior.

# Message contract

IDs:

Caller-provided IDs are accepted unchanged. Missing IDs use the existing
`message-` plus 16 random-byte hex format.

External identity:

Durable identity is local message ID or `(conversation_id, source, external_id)`
when external ID is non-empty.

Append:

Existing identical messages are idempotent. New normalized messages are
inserted; no raw HH parsing or arbitrary message update API exists.

Dedup:

Duplicate imports remain durable across repository instances and process
restarts through the PostgreSQL identity lookup/unique index.

Conflict:

Same identity with changed normalized content returns the established conflict
error and never replaces history. Unique violations are translated without
exposing constraint names.

Ordering:

Timeline uses `ORDER BY timestamp,sequence`; equal timestamps retain append
sequence order.

Immutability:

PASS. Existing message rows must remain an unchanged prefix during upsert, and
message append has no update path.

# Claims

Claims remain inside the conversation summary JSONB and use the domain's
message-linkage, sender, verbatim-excerpt, experience-value, and duplicate
validation. Exact duplicate claims are idempotent; claims are never promoted
to Candidate truth and the adapter has no Candidate dependency.

# Timeline

`ports.ConversationReader.Timeline` returns only normalized Conversation
messages, with established ordering, not-found behavior, empty results, and
detached values. Application events, drafts, notifications, and write actions
are not merged.

# Transaction semantics

`NewConversationRepository(pool)` uses an owned transaction for mutations.
`NewConversationRepositoryForTx(tx)` binds directly to the caller-owned
transaction. The root `newPostgresConversationRepositoryTx` wrapper preserves
the existing Career transaction composition API. No context backgrounding,
goroutines, cache, or per-message repository loading was introduced.

# Application relation cross-link

Application adapter writes `conversations.application_id`:

YES. The existing `ApplicationRepository.AttachConversation` operation still
updates both `applications.conversation_id` and `conversations.application_id`
within its transaction.

Conversation adapter writes applications/conversation relation:

The Conversation adapter writes only its own `conversations.application_id`
column during Conversation upsert. It does not call or update the Application
repository and does not write `applications.conversation_id`.

Current ownership:

The relation is historically duplicated in both aggregates. It is
transactionally synchronized by Application-side attach, but a general
Conversation upsert is not a two-table synchronization operation.

Deferred architecture decision:

Keep the compatibility behavior for now. A future Career transaction/use-case
layer can decide canonical ownership and synchronization; R7.3 does not redesign
that boundary.

# Errors

Conversation not found uses `conversation.ErrConversationNotFound`. Vacancy
foreign-key failures use the existing vacancy sentinel. Conversation external
identity duplicates remain `ErrDuplicateConversationExternal`. Message external
identity and other unique violations remain `ErrRepositoryConflict`; message
foreign-key failures map to Conversation not found. PostgreSQL error translation
is adapter-owned and does not expose constraint names.

# JSON/Postgres parity

Shared:

Both adapters implement the unchanged Conversation ports and preserve aggregate
round-trip, opaque external identity, append/dedup, immutable history, state,
summary, claim, follow-up, and Timeline semantics. Both use domain
`SameMessage`, `RefreshActivity`, and intrinsic validation.

Intentional differences:

JSON mutations are memory-first with explicit file Save and process locking;
PostgreSQL mutations are immediately durable and use SQL identity constraints.
PostgreSQL stores normalized messages in `conversation_messages` and summary
claims in JSONB rather than a JSON envelope.

Unresolved:

NONE

# Root compatibility

Aliases:

`PostgresConversationRepository` is a type alias to
`postgresstorage.ConversationRepository`.

Wrappers:

`NewPostgresConversationRepository` and the transaction constructor delegate to
the adapter constructors. Career transaction composition remains root-owned.

Runtime Conversation SQL in root:

NONE

# Shared PostgreSQL mechanics

Moved:

Conversation SQL, scanners, JSONB codecs specific to Conversation values,
timestamp/nullability mapping, message identity handling, and PostgreSQL error
translation moved to the adapter.

Remaining root:

The mechanical `postgresDBTX` interface and generic helper set remain available
to root Candidate/semantic code. This is intentionally narrow root compatibility
debt and was not broadened for hypothetical R7.4 work.

# Deliberately deferred

Candidate PostgreSQL:

ROOT

Semantic:

DEFERRED

Migrations:

UNCHANGED

Composition:

ROOT

# Tests

Pure:

Added adapter tests for complete Conversation scanning, nullable values,
nanosecond timestamps, and PostgreSQL identity/relation error translation.

JSON:

Existing JSON Conversation adapter tests pass.

Postgres integration:

SKIPPED (`POSTGRES_TEST_DATABASE_URL` is not configured).

Root regressions:

Full uncached suite and focused Conversation, ports, JSON, PostgreSQL,
conversation-policy, and application tests pass.

Write safety:

PASS. No HH write path was invoked; ordinary tests use local fixtures/mocks and
the opt-in PostgreSQL test was skipped.

# Size

Root Conversation Postgres LOC before:

728 lines of the mixed file, excluding the Career composition that followed.

Root compatibility LOC after:

64 lines for Career composition plus 23 lines for Conversation alias wrappers.

Postgres adapter LOC:

718 lines of Conversation implementation, plus focused adapter tests.

# Dependencies

The dependency direction is:

`internal/conversation` → standard library

`internal/ports` → `internal/conversation`

`internal/adapters/storage/postgres` Conversation → `internal/conversation`,
`internal/ports`, and pgx packages

There is no adapter → root, adapter → JSON, adapter → Candidate, or domain/port
→ adapter dependency.

# Behavior

Conversation:

UNCHANGED

Application:

UNCHANGED

Candidate:

UNCHANGED

HH:

UNCHANGED

AI:

UNCHANGED

Dashboard:

UNCHANGED

# Verification

gofmt:

PASS

go test:

PASS (`go test -count=1 ./...`)

race:

PASS (`go test -race ./...`)

vet:

PASS

build:

PASS

diff:

PASS (`git diff --check`)

node:

PASS (`node --check web/app.js`)

Docker:

SKIPPED. Docker CLI is installed, but the daemon is not running.

LIVE HH WRITES:

0

# Ready for R7.4

R7.4 — Candidate PostgreSQL + Transaction Boundary

READY

The Conversation SQL has one runtime owner, message history remains immutable,
durable duplicate external-message behavior is preserved, the adapter has no
Candidate dependency, and claim persistence cannot mutate Candidate truth.
