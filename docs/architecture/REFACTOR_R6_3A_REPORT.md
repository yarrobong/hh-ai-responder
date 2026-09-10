# Before

`VacancyStore` was the real JSON persistence implementation in the root
package. It owned the `vacancies.json` envelope, strict decoding, missing-file
behavior, secret-marker checks, validation, local ID allocation, duplicate
external-ID checks, in-memory values, detached JSON cloning, and atomic save.
Mutations were memory-only and callers explicitly invoked `Save`.

`JSONVacancyRepository` in `repositories.go` was only a context/query wrapper
around `VacancyStore`; it did not own JSON I/O or independent state.

The file was `vacancies.json`, or the path supplied to `NewVacancyStore`. Saves
used a same-directory temporary file, `Sync`, rename, cleanup, and the existing
private process lock. The platform atomic writer created private directories
and files.

The root store did not maintain explicit maps. Lookup and local ID allocation
scanned the in-memory slice in insertion order.

# Classification

| Symbol | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| `VacancyStore` | Root package | Root compatibility façade over `jsonstorage.VacancyRepository` | Keep name for source compatibility; remove implementation |
| `JSONVacancyRepository` | Root wrapper | `internal/adapters/storage/json.VacancyRepository` | Root alias only |
| `vacancyStoreFile` | Root package | JSON adapter | Move envelope unchanged |
| JSON file I/O | `VacancyStore` | `jsonstorage.VacancyRepository` | Move |
| Serialization | `VacancyStore` plus `internal/vacancy.Vacancy` JSON methods | Adapter uses domain JSON contract | Move storage call site; preserve domain contract |
| Locking | Root `withStoreLock` plus platform lock | Adapter `platform.WithPrivateFileLock`; `SaveUnlocked` for existing batch choreography | Move without weakening lock/write sequence |
| Atomic write | Root `platform.WritePrivateFileAtomic` call | Adapter `platform.WritePrivateFileAtomic` call | Move; no duplicate writer |
| Local ID allocation | `VacancyStore.nextID` | Adapter `nextIDLocked` | Move unchanged |
| External-ID uniqueness | `VacancyStore` scan | Adapter scan | Move unchanged |
| Query | Root repository wrapper | Adapter `List` with `ports.VacancyQuery` | Move locally persisted filtering only |
| Domain validation | Root helper used by JSON and PostgreSQL paths | Adapter validation with thin root wrappers | Centralize persisted-vacancy validation without changing PostgreSQL |
| Domain policy | `internal/vacancy` | `internal/vacancy` | Unchanged |
| HH | HH sync/read packages | HH sync/read packages | Out of scope; unchanged |
| AI | Matching/analyzer packages | Matching/analyzer packages | Out of scope; unchanged |

# New adapter

Implementation: `internal/adapters/storage/json/vacancy.go`

Port: `ports.VacancyStore`, with compile-time assertions for
`ports.VacancyReader`, `ports.VacancyWriter`, and `ports.VacancyStore` in the
adapter package.

Focused tests: `internal/adapters/storage/json/vacancy_test.go`.

The Vacancy implementation imports only `internal/vacancy`, `internal/ports`,
`internal/platform`, and the standard library. It does not import the root
package, application, conversation, PostgreSQL, HH, AI, or dashboard code.
The JSON package also contains the pre-existing R6.2 candidate adapter; its
package-level dependency graph therefore still includes those earlier
candidate-related dependencies, while the new Vacancy file does not.

# JSON contract

Path: unchanged; default `vacancies.json`, with caller-provided paths
preserved.

Schema: unchanged version-1 envelope with `version` and `vacancies` fields.
Domain JSON behavior remains responsible for both `id` and legacy
`vacancyId`, HH-shaped fields, `MatchResult`, `ApplicationRecommendation`,
`DataCompleteness`, and `ReconciliationEvidence`.

Missing file: loads as an empty in-memory repository and does not create a file
until explicit `Save`.

Corrupt, unknown-version, missing-envelope-field, trailing-data, secret-marker,
or invalid-vacancy files fail closed and do not replace the existing in-memory
state.

Permissions: the adapter preserves private directory/file behavior: directory
`0700`, file `0600`. The atomic write continues to use the shared platform
helper.

# IDs

Local: explicit positive IDs are preserved; zero IDs receive the next integer
after the largest current ID, starting at `1`. Restart reconstruction preserves
the sequence and collision behavior.

External: exact `ExternalID` values remain unique for non-empty values.

Duplicate semantics: duplicate local IDs return `ErrDuplicateVacancyID`;
duplicate external IDs return `ErrDuplicateVacancyExternal`. `Create` does not
silently become `Update`.

# Persistence

Create: validates and mutates memory only; it preserves generated IDs and
timestamps and retains the legacy behavior that the stored created value has a
known serialized response-count field.

Update: requires a non-zero ID, preserves the existing creation timestamp when
omitted, supplies an update timestamp when omitted, retains duplicate checks,
and mutates memory only.

Save: explicit `Save(context.Context)` acquires the existing private process
lock and calls `SaveUnlocked`. The latter is available for the existing HH
read-sync batch lock choreography and performs the same validation, encoding,
same-directory staging, sync, rename, and cleanup.

Restart: create/update/save, new repository construction, `Load`, and lookup
round-trip local IDs, external IDs, timestamps, match values, count-known
semantics, and reconciliation values.

Atomicity: unchanged platform atomic writer and lock scope; no second atomic
writer was introduced.

# Query behavior

`ports.VacancyQuery` is applied only to locally persisted values. `ID` and
exact non-empty `ExternalID` filters are supported. Results preserve insertion
order and are detached from repository state. No HH search or dashboard filter
was moved into the adapter.

# Root compatibility

Aliases: `JSONVacancyRepository` is a root type alias for
`jsonstorage.VacancyRepository`; `NewJSONVacancyRepository` returns the
adapter held by the compatibility `VacancyStore`.

Wrappers: `VacancyStore`, its legacy no-context CRUD methods, `Load`,
`Save`, `Delete`, the batch `saveUnlocked` path, and legacy error variables
remain as thin delegating compatibility surfaces. The root façade has no
authoritative vacancy slice, indexes, JSON envelope, or persistence algorithm;
the adapter owns all in-memory state, validation, cloning, and file persistence.

Duplicate implementation: NONE.

# Port compliance

VacancyReader: PASS

VacancyWriter: PASS

VacancyStore: PASS

# Postgres

UNCHANGED / ROOT. `PostgresVacancyRepository` and its SQL, transaction, and
migration code were not moved or edited for this stage.

# Tests

Adapter: PASS — missing file, corrupt JSON, restart round trip, explicit Save,
duplicate external ID, Create/Update distinction, ordering, detached results,
response-count absent/zero semantics, private permissions, and context
cancellation.

Root: PASS — existing Vacancy store and repository compatibility tests.

Contract: PASS — `vacancy_storage_contract_test.go` and
`repository_contract_test.go`.

Race: PASS — `go test -race ./...`.

# Size

Root Vacancy JSON LOC before: 297

Root compatibility LOC after: 150

Adapter implementation LOC: 395

Adapter test LOC: 185

# Dependencies

`go list ./...`: PASS.

`go list -deps ./internal/adapters/storage/json`: PASS. The new Vacancy
adapter file has only the allowed direct dependencies; the package graph also
contains the already extracted R6.2 candidate storage files.

`go list -deps ./internal/vacancy`: PASS; the Vacancy domain remains
stdlib-only.

# Behavior

Vacancy: UNCHANGED

Vacancy JSON: UNCHANGED

Vacancy IDs: UNCHANGED

External ID: UNCHANGED

Matching: UNCHANGED

Reconciliation metadata: UNCHANGED

HH: UNCHANGED

Application: UNCHANGED

Candidate: UNCHANGED

Dashboard: UNCHANGED

# Verification

gofmt: PASS

go test -count=1 ./...: PASS

go test -race ./...: PASS

go vet ./...: PASS

go build ./...: PASS

git diff --check: PASS

node --check web/app.js: PASS

Docker: SKIPPED — Docker CLI is present, but the Docker daemon is unavailable
at the configured socket.

LIVE HH WRITES: 0

# Ready for R6.3b

R6.3b — Application JSON Adapter Boundary

READY.
