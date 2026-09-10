# Before

`ApplicationStore` owned the authoritative application slice and event slice,
JSON envelope, load/save implementation, validation, local ID generation,
external-ID lookup, relation mutation, status/event mutation, match-result
mutation, timeline sorting and file locking. It also retained the existing
`ConversationStore`, `CandidateContextResolver`, statistics and
`ApplicationContext` read-model coupling.

`JSONApplicationRepository` in `repositories.go` was only a root-package
delegating wrapper around `ApplicationStore`; it did not own JSON I/O or state.
The file was `job_applications.json`, with version 1 and `applications` plus
`events` arrays. Mutations were in-memory and `Save` was explicit. Local IDs
used the existing `application-<32 hex>` and `application-event-<32 hex>`
format. Events were append-only in storage and returned in timestamp order by
Timeline. Conversation relations were stored as IDs; the old root façade also
performed the optional same-vacancy check when a `ConversationStore` was
configured.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| Application JSON envelope | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved |
| Application state | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved; root fields are mirrors only |
| Load / Save / locking | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved |
| Local and event IDs | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved; format preserved |
| External-ID lookup and uniqueness | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved |
| Relations | `ApplicationStore` | `jsonstorage.ApplicationRepository` | ID values persisted; root keeps context validation |
| Status → event mapping | root compatibility/domain helper | `internal/application.EventTypeForStatus` called by adapter | Reused, not duplicated |
| Event append and Timeline | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Moved |
| Match / follow-up / reconciliation values | `ApplicationStore` | `jsonstorage.ApplicationRepository` | Persisted unchanged, not interpreted |
| Statistics | `ApplicationStore` | root `ApplicationStore` | Read model remains root |
| Candidate context | `ApplicationStore` | root `ApplicationStore` | Remains root |
| Conversation orchestration | root `ApplicationStore` | root `ApplicationStore` | Remains root |

# New adapter

Implementation: `internal/adapters/storage/json/application.go`

Port: `ports.ApplicationStore`, with compile-time assertions for
`ApplicationReader`, `ApplicationWriter` and `ApplicationStore`.

Dependencies are limited to `internal/application`, `internal/vacancy`,
`internal/conversation`, `internal/ports`, `internal/platform` and the
standard library. There is no root-package, candidate, HH, AI, dashboard or
PostgreSQL dependency in the application adapter source.

# JSON contract

Path: `job_applications.json`.

Schema: version 1; `applications` is required and non-null; `events` may be
omitted and is treated as an empty timeline, matching the previous contract.
Unknown fields and trailing JSON are rejected. Existing fields, tags,
`omitempty` behavior, timestamps, relations, match result, follow-up and
reconciliation metadata are unchanged.

Missing files load as empty stores. Corrupt, unknown-version, secret-marker,
null-application and trailing-data files fail without replacing in-memory
state. Saves remain atomic, private and explicit, with 0700 directories and
0600 files.

# IDs

Local IDs preserve the existing random `application-<32 hex>` format. Event IDs
preserve `application-event-<32 hex>`. Explicit IDs remain supported and local
or external duplicates are rejected. Empty external IDs remain allowed for
normal Create and required for imported upserts.

# Persistence operations

Create applies the existing default source/status/timestamps, stores a detached
value and appends the existing `created` event. Update/import replaces values
without fabricating update events; imported upserts preserve existing IDs and
creation timestamps. Conversation relations store only IDs in the adapter;
same-vacancy validation remains in the root compatibility façade. Status,
follow-up and match updates preserve timestamps and existing event behavior.
AppendEvent validates normalized domain values, generates a unique event ID and
appends in memory. Save is the only durability operation; batch sync uses
`SaveUnlocked` under its existing outer lock choreography.

# Event contract

Append-only: PASS

Ordering: storage order is preserved; `Timeline` uses the established stable
timestamp ordering.

Duplicate: generated event IDs are unique; same-type events remain distinct and
are not replaced.

Timeline: returns only persisted application events, with the existing missing
application behavior.

Detachment: application values, nested match/reconciliation data, event slices
and Timeline results are cloned before being returned or stored.

# ApplicationStore compatibility

Persistence: DELEGATED.

Context/read-model: ROOT.

Candidate dependency: ROOT ONLY.

Conversation dependency: ROOT ONLY.

The root façade retains deprecated synchronized mirrors solely for source
compatibility with existing root-package workflows that inspect legacy fields.
The adapter repository is authoritative; batch sync clones and swaps the
adapter rather than copying root persistence state.

# Root compatibility

`JSONApplicationRepository` is now a thin root wrapper embedding the adapter;
its `store` pointer exists only for legacy constructor/test compatibility.
`NewJSONApplicationRepository` still works. Root `ApplicationStore` methods
delegate to the adapter while context assembly and statistics remain local.

Duplicate JSON implementation: NONE. Root audit/hash decoding uses diagnostic
anonymous structs and does not load or save application persistence.

# Port compliance

ApplicationReader: PASS

ApplicationWriter: PASS

ApplicationStore: PASS

# Postgres

UNCHANGED / ROOT. PostgreSQL repository code, SQL and transactions were not
modified by R6.3b.

# Vacancy shared-validation debt

UNCHANGED and recorded for R7. The existing root `validateVacancy` compatibility
helper still delegates to `jsonstorage.ValidateVacancy`, and PostgreSQL callers
still reach that root helper. R6.3b does not expand the Vacancy refactor or make
PostgreSQL import the application adapter.

# Tests

Adapter: focused JSON adapter tests cover missing/corrupt/version data,
round-trip/restart, explicit Save, IDs, duplicates, relations, status, match,
follow-up, append-only events, ordering, detached nested results, cancellation,
permissions and parallel reads.

Root: existing application storage, context, reconciliation, dashboard and
workflow tests pass.

Storage contracts: existing `application_storage_contract_test.go` and
`repository_contract_test.go` pass unchanged.

Race: PASS in final verification.

# Size

Root Application JSON LOC before: 739.

Root compatibility/read-model LOC after: 464 (`job_application_store.go`).

Adapter LOC: 856 implementation plus 213 focused tests.

# Dependencies

`go list -deps ./internal/adapters/storage/json` confirms the adapter has no
root-package dependency. `internal/application` depends only on its intended
conversation and vacancy domain values. `internal/ports` remains contract-only.

# Behavior

Application: UNCHANGED

Vacancy: UNCHANGED

Conversation: UNCHANGED

Candidate: UNCHANGED

HH: UNCHANGED; no live HH writes occurred.

Dashboard: UNCHANGED

# Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED; Docker CLI was present but the daemon was unavailable.

LIVE HH WRITES: 0

# Ready for R6.3c

READY

R6.3c — Conversation JSON Adapter Boundary may begin in a separate stage.
