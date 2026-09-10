# Before

`HHReadSyncService` in the root package owned HH page progression, import
mapping, identity matching, local merge rules, message import, cursor/state
updates, and the JSON/PostgreSQL commit paths. Vacancy, application, and
conversation import logic was duplicated between the ordinary JSON batch and
the PostgreSQL transaction path.

The R9.1 read adapter already owned normalized HH transport values and raw
provider decoding. Profile bootstrap, resume facts, write-preflight readback,
and vacancy-test metadata remain outside this stage.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| HH page reads and provider cursors | root/R9.1 adapter | `hhreadsync.ReadBatch` + R9.1 adapter | progression moved; transport mechanics retained |
| Vacancy mapping/import | root, duplicated in PG path | `internal/usecase/hhreadsync` | moved |
| Application mapping/import | root, duplicated in PG path | `internal/usecase/hhreadsync` | moved |
| Conversation/message mapping/import | root, duplicated in PG path | `internal/usecase/hhreadsync` | moved |
| Identity and local merge policy | root | `internal/usecase/hhreadsync` | moved |
| JSON clone/lock/SaveUnlocked | root + JSON adapter | root + JSON adapter | backend choreography retained |
| PostgreSQL CareerTx | root + PostgreSQL adapter | root + PostgreSQL adapter | transaction lifecycle retained |
| Sync state files and recurrence | root | root | composition/scheduling retained |
| Matching, AI, dashboard, write/preflight | root | root | outside scope |

# Sync use case

Package: `internal/usecase/hhreadsync`

Service: `hhreadsync.Service`

Dependencies: typed `Dependencies` containing `ports/hhread.HHReadSource`,
domain-specific vacancy/application/conversation persistence ports, and small
pure compatibility callbacks for established conversation-state normalization.
Candidate matching and AI enrichment are not dependencies of the use case.

HH source: read-only `ports/hhread.HHReadSource`; targeted conversation reads
use a separate optional narrow capability.

Storage ports: `ports.VacancyStore`, `ports.ApplicationStore`, and
`ports.ConversationStore`. No JSON, PostgreSQL, pgx, HTTP, Candidate, AI, or
HH write implementation is imported by the use case.

# Constructor

Typed: YES — `hhreadsync.NewService(hhreadsync.Dependencies{...})`.

Root compatibility: `NewHHReadSyncService` and
`NewHHReadSyncServiceWithRepositories` remain source-compatible wrappers for
legacy root callers. They translate legacy dependencies and invoke the typed
service; import policy is not implemented there.

Remaining `...any`: root compatibility constructors retain the historical
untyped dependency list because existing tests and CLI/dashboard callers pass
mixed legacy dependencies. There is no `any` or `...any` in the new use-case
package. This is compatibility-only debt.

# Vacancy sync

Mapping: `hhreadsync.MapVacancy` consumes the normalized R9.1 record.

Identity: external HH vacancy ID; existing local numeric ID and creation time
are retained on update.

Local merge: match result, recommendation, and reconciliation evidence survive
provider refresh. Root compatibility applies the existing matching result after
the typed provider import; the use case has no matching or AI hook.

Response count: value and `TotalResponsesCountKnown` are passed unchanged.
JSON persistence now omits the numeric field when unknown so unknown, explicit
zero, and positive values survive save/restart.

Detail reuse: unchanged; metadata comparison and detail-fetch decisions remain
in the R9.1 read path.

Pagination: `hhreadsync.ReadBatch` owns sequential page/cursor progression;
root owns only scheduling, progress presentation, and persistence lifecycle.
The service also provides sequential one-run `Sync` for typed callers.

# Application sync

Identity: external HH negotiation/application ID.

Status: raw provider status is retained; deterministic mapping uses the
existing root status mapper callback, with unknown values becoming
`application.StatusUnknown`.

Import/upsert: existing `UpsertImported` semantics are reused.

Events: refreshes do not create events; new imports retain the repository’s
single created-event behavior.

Relations: vacancy and observed conversation relations are linked through
existing persistence operations.

Local merge: notes, next action, follow-up state, match result, reconciliation
evidence, and an existing conversation relation are preserved.

# Conversation sync

Identity: HH conversation external ID.

Messages: normalized R9.1 messages are sorted deterministically and imported
through the conversation aggregate.

Dedup: existing provider identity and `conversation.SameMessage` repository
semantics remain authoritative; historical messages and conflicts are not
rewritten.

Service messages: retained as HH system messages.

Activity: recalculated through `conversation.RefreshActivity`.

Summary/claims preservation: existing summary, candidate claims, follow-up
state, and next action survive refresh; no claims or summary are generated.

# Incremental sync

Vacancy cursor: existing root state and numeric/provider cursor behavior.

Applications cursor: existing root state and page cursor behavior.

Conversation cursor: existing root state and `from` cursor behavior.

Advance timing: state is advanced only after the existing persistence path has
completed without import errors; failed reads/imports do not mark a completed
stream successful.

# Idempotency

Second identical sync: no duplicate aggregates or messages.

Vacancy duplicates: NONE

Application duplicates: NONE

Conversation duplicates: NONE

Message duplicates: NONE

Event duplicates: NONE

# Batch / transaction semantics

JSON: existing staged clone, ordered file locks, explicit `SaveUnlocked`, and
adoption/swap behavior remain in the root compatibility executor and adapters.
The use case does not know about files or locks.

Postgres: `CareerTx` remains the root-owned transaction lifecycle. The typed
use case receives transaction-bound domain ports and imports in Vacancy →
Application → Conversation order.

Usecase infrastructure awareness: NONE

Root compatibility transaction wrapper: `commitPostgresReadBatch`; it also
keeps the existing post-import vacancy matching outside the use case.

# Partial failure

Transport: accumulated records retain the existing incomplete result/error
behavior.

Repository: JSON import keeps historical accumulating errors; PostgreSQL uses
fail-fast import inside the existing transaction.

Save: explicit backend saves remain outside the use case.

Commit: PostgreSQL commit remains owned by `PostgresCareerStore`; JSON adoption
remains staged.

Cursor outcome: failed/partial work does not advance the corresponding
successful-sync state.

# Reconciliation

Moved: normalization/import consistency, identity matching, and relation
attachment required by the established sync contract.

Remaining root: CareerDataReconciler, candidate matching, notifications,
follow-up, dashboard refresh, and targeted post-write delivery reconciliation.

Reason: these are business workflows or backend lifecycle concerns, not HH
read synchronization policy.

# HH transport

R9.1 adapter: UNCHANGED

Raw DTO parsing in usecase: NONE

Rate limiting in usecase: NONE

# Legacy HH reads

Profile bootstrap: ROOT / DEFERRED

Resume facts: ROOT / DEFERRED

Write preflight read: ROOT / DEFERRED

Vacancy test metadata: ROOT / DEFERRED

# HH Write

Material changes: NONE

LIVE HH WRITES: 0

# Root compatibility

HHReadSyncService: WRAPPER with the historical public surface.

Runtime sync algorithm in root: import algorithm NONE; transport scheduling,
state, locks, and backend commit choreography remain for compatibility.

HHAIResponder sync responsibility: supplies the R9.1 read source and remains
owner of non-sync HH reads, matching, AI, preflight, writes, and composition.

# Tests

Pure: `internal/usecase/hhreadsync` tests cover normalized mapping, response
count knowledge, local merge, sequential sync, service messages, and
cancellation.

JSON: existing root and JSON adapter tests pass.

Postgres: existing opt-in tests remain unchanged; no database was configured in
this run.

Idempotency: existing HH sync, repository, conversation, and application tests
pass.

Cursor: existing sync and R9.1 cursor tests pass.

Write safety: existing suite passes; no HH write capability is present in the
new package.

# Size

Root `hh_read_sync.go` before: 1093 LOC.

Root `hh_read_sync.go` after: 864 LOC (legacy read compatibility remains).

`hhreadsync` package: 1,088 LOC including tests.

# Dependencies

`hhreadsync` directly depends on `internal/hhread`, `internal/ports/hhread`,
the domain/value packages, persistence ports, and the standard library. It has
no direct concrete transport/storage, Candidate, semantic, AI, dashboard, or
write gateway dependency. The repository's umbrella `internal/ports` package
currently also contains unrelated Candidate and semantic contracts, so
`go list -deps ./internal/usecase/hhreadsync` reports those existing
transitive packages; no hhreadsync source imports or uses them. The root
compatibility layer owns the post-import matching callback and it is not passed
into `hhreadsync`.

# Behavior

HH Read: UNCHANGED

HH Sync: UNCHANGED

Vacancy: UNCHANGED

Application: UNCHANGED

Conversation: UNCHANGED

HH Write: UNCHANGED

Other domains: UNCHANGED

# Verification

gofmt: PASS

go test -count=1: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED — Docker CLI is installed but the daemon is unavailable.

LIVE HH WRITES: 0

# R9 status

R9.1 sync-facing transport: EXTRACTED

R9.2 synchronization: EXTRACTED

R9: COMPLETE FOR SYNC PATH

Legacy non-sync HH GET capabilities: DEFERRED TO THEIR OWNERS

# Ready for next stage

R10.1 — AI Completion Transport / Provider Boundary

READY. The sync algorithm has one import owner, the use case has no
infrastructure implementation
dependency, JSON/PostgreSQL lifecycle boundaries remain outside it,
application/conversation history tests pass, and HH write capability is absent.
