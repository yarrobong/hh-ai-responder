# R7.4b — Candidate PostgreSQL Store / Transaction Boundary

## Before

`PostgresCandidateStore` and the `CandidateTx` facade were implemented in the
root package. The store validated its pool/callback, began a PostgreSQL
transaction, constructed a transaction-bound Candidate repository, invoked a
typed callback, and committed after callback success. A deferred rollback
preserved rollback-on-error and panic behavior; rollback errors were ignored.

`CandidateTx` exposed the existing Candidate writer, repository, current
snapshot read, and expected-version persistence primitive. It contained no
Candidate policy or semantic behavior.

`CandidateMutationService` performed the application flow: read the current
Candidate inside the transaction, ran the pure mutation callback, advanced the
version, persisted with `PersistCandidateIfVersion`, and returned only after a
successful commit. Its optional semantic indexer ran after commit and read
back the committed Candidate.

The transaction lifecycle was:

```text
BEGIN
  -> CurrentCandidate
  -> pure mutation policy
  -> PersistCandidateIfVersion(expected N, version N+1)
  -> COMMIT
  -> optional semantic reindex
```

## Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `PostgresCandidateStore` | root | `postgresstorage.CandidateStore` | moved; root alias/constructors retained |
| `CandidateTx` | root | `postgresstorage.CandidateTx` | moved; root alias retained |
| Candidate transaction begin/commit/rollback | root | `postgresstorage.CandidateStore` | moved |
| transaction-scoped repository construction | root | `postgresstorage.CandidateStore` | moved |
| canonical Candidate SQL | `postgresstorage.CandidateRepository` | unchanged | already extracted in R7.4a |
| version checking | `postgresstorage.CandidateRepository` | unchanged | preserved |
| mutation legality/policy | root + `candidatemutation` | unchanged | not moved |
| semantic post-commit hook | root `CandidateMutationService` | unchanged | remains outside adapter |
| backend composition | root | unchanged | remains root |
| migration orchestration | root | unchanged | remains root |

## New Postgres transaction boundary

`postgresstorage.CandidateStore` owns the Candidate-specific transaction
lifecycle and retains the application-owned `*pgxpool.Pool` reference. It does
not open or close the pool, read configuration, run migrations, apply
Candidate policy, or call semantic/AI/HH code.

`postgresstorage.CandidateTx` is the typed persistence facade for one already
open transaction. Its implementation delegates to one
`postgresstorage.CandidateRepository` created with
`NewCandidateRepositoryForTxID`.

Dependencies are:

```text
CandidateStore / CandidateTx
    -> internal/candidate
    -> internal/ports
    -> pgx/pgxpool
```

There is no dependency on semantic indexing, embeddings, pgvector, AI, HH, or
the root package. No generic transaction port was added.

## Transaction lifecycle

Begin uses the caller context and the existing `pool.Begin(ctx)` behavior. No
isolation change, advisory lock, retry, context detachment, or hidden
goroutine was introduced. The configured Candidate ID is passed unchanged to
the transaction-bound repository.

The callback uses the established typed shape `func(CandidateTx) error`.
Candidate policy remains in `CandidateMutationService` and
`internal/usecase/candidatemutation`; the adapter exposes persistence only.

Commit is attempted only after callback success. A commit error is wrapped as
`commit postgres candidate transaction` and returned as failure. The deferred
rollback does not turn a failed commit into success.

Rollback remains active for callback errors, repository/current-candidate
errors, version conflicts, context cancellation, commit failure, and panics.
Rollback errors remain ignored, preserving the previous contract and primary
error precedence. Panic recovery was not introduced; a callback panic still
propagates after the deferred rollback attempt.

## Versioning

Store interpretation: NONE.

The repository remains the expected-version authority. The caller reads
version `N`, pure mutation produces version `N+1`, and
`PersistCandidateIfVersion` verifies expected `N` before replacing the full
aggregate. The store does not increment, rewrite, normalize, retry, or hide
version conflicts.

Mutation stale/base mismatch remains distinct from PostgreSQL
`postgresstorage.ErrConflict`. Automatic retry is NONE.

## CandidateMutationService

Owner: ROOT / APPLICATION ORCHESTRATION.

Pure mutation: `internal/usecase/candidatemutation` and the existing root
orchestration helpers.

Persistence: `postgresstorage.CandidateStore`, `CandidateTx`, and
`CandidateRepository`, reached through root compatibility aliases.

Semantic: POST-COMMIT OUTSIDE ADAPTER.

The service still loads the current Candidate in the transaction callback,
prepares and applies the mutation, persists the next version, and interprets
the semantic warning only after the transaction returns successfully.

## Semantic safety

Runs after commit: YES.

Runs on conflict: NO.

Runs on rollback: NO.

Runs on commit error: NO.

Semantic failure rolls back Candidate: NO. A committed Candidate remains
committed and the service reports the established stale-index warning.

The focused relevant-knowledge/preflight regression passes. The opt-in
PostgreSQL semantic post-commit contract is available but was skipped because
`POSTGRES_TEST_DATABASE_URL` is unset.

## Root compatibility

`PostgresCandidateStore`: ALIAS / WRAPPER.

`CandidateTx`: ALIAS.

Root constructors `NewPostgresCandidateStore` and
`NewPostgresCandidateStoreForID` delegate to `postgresstorage` constructors.
Root callers, migration orchestration, backend composition, and tests retain
their established API. There is no duplicate Candidate transaction
implementation.

## Candidate persistence status

Repository: EXTRACTED.

Transaction/store: EXTRACTED.

Canonical SQL: POSTGRES ADAPTER.

Semantic: DEFERRED TO R8.

## Backend asymmetry

JSON remains reader-oriented with its acquisition/store compatibility path.
PostgreSQL remains the Candidate `Reader` + `Writer` + `MutationWriter`
backend with transaction-scoped full aggregate persistence. No fallback,
dual-write, or JSON/PostgreSQL mirroring was introduced.

## Integration

Version conflict: SKIPPED — `POSTGRES_TEST_DATABASE_URL` is not configured.

Rollback: SKIPPED — `POSTGRES_TEST_DATABASE_URL` is not configured.

Commit durability: SKIPPED — `POSTGRES_TEST_DATABASE_URL` is not configured.

Concurrent conflict: SKIPPED — `POSTGRES_TEST_DATABASE_URL` is not configured.

The existing opt-in root PostgreSQL contract remains the integration coverage
for import, repository round-trip, transaction update, rollback, version
conflict, and durability when a test database is supplied. No shared or
development database was touched.

## Cross-stage debt

Application/Conversation cross-link: DEFERRED. The existing
`ApplicationRepository.AttachConversation` relation remains future Career /
application transaction-boundary work.

Root PostgreSQL semantic mechanics remain in `postgres_compat_helpers.go` and
`postgres_candidate_semantic_repository.go`; they are semantic-only and are
deferred to R8. Root Candidate repository constructors/fingerprint/validation
helpers remain compatibility shims only.

## Tests

Passed:

- focused Candidate, ports, PostgreSQL storage, JSON storage, candidate
  context, candidate mutation, and candidate acquisition tests;
- `go test -count=1 ./...`;
- `go test -race ./...`;
- `go vet ./...`;
- `go build ./...`;
- semantic post-commit and RelevantKnowledge/write-preflight regression tests
  that do not require PostgreSQL.

Skipped by environment:

- PostgreSQL integration tests because `POSTGRES_TEST_DATABASE_URL` is unset;
- Docker build because the Docker daemon is unavailable.

No live HH writes occurred.

## Size

Root Store/Tx LOC before: 73.

Root compatibility after: 20.

PostgreSQL adapter Store/Tx LOC: 86.

The ownership result is the objective: one Candidate transaction lifecycle
implementation now lives in `internal/adapters/storage/postgres`.

## Dependencies

The Candidate transaction/store implementation depends on:

- `internal/candidate`;
- `internal/ports`;
- standard library `context`, `errors`, and `fmt`;
- `github.com/jackc/pgx/v5/pgxpool`.

The package dependency audit shows no root-package, semantic, AI, or HH
dependency. `internal/candidate` remains stdlib-only; `internal/ports` depends
only on Candidate values.

## Behavior

Candidate: UNCHANGED.

Mutation: UNCHANGED.

Truth: UNCHANGED.

Provenance: UNCHANGED.

CandidateContext: UNCHANGED.

CandidateAcquisition: UNCHANGED.

Canonical Postgres: UNCHANGED.

Optimistic versioning: UNCHANGED.

Transaction atomicity: UNCHANGED.

Semantic: UNCHANGED and post-commit.

JSON Candidate: UNCHANGED and intentionally asymmetric.

Vacancy/Application/Conversation: UNCHANGED.

HH/AI/Dashboard: UNCHANGED; LIVE HH WRITES = 0.

Schema/migrations: UNCHANGED.

## Verification

`gofmt`: PASS.

`go test -count=1 ./...`: PASS.

`go test -race ./...`: PASS.

`go vet ./...`: PASS.

`go build ./...`: PASS.

`git diff --check`: PASS.

`node --check web/app.js`: PASS.

Docker: SKIPPED — daemon unavailable.

PostgreSQL integration: SKIPPED — `POSTGRES_TEST_DATABASE_URL` unset.

LIVE HH WRITES: 0.

## R7 status

Vacancy PostgreSQL: EXTRACTED.

Application PostgreSQL: EXTRACTED.

Conversation PostgreSQL: EXTRACTED.

Candidate PostgreSQL Repository: EXTRACTED.

Candidate PostgreSQL Transaction Store: EXTRACTED.

R7: COMPLETE.

## Ready for R8

READY.

- Candidate transaction lifecycle has one owner;
- implementation lives in `postgresstorage`;
- root Store/Tx are aliases/thin compatibility only;
- semantic hooks remain post-commit and outside Candidate PostgreSQL
  transaction code;
- Candidate conflicts and rollbacks cannot trigger semantic indexing;
- no canonical Candidate SQL remains root-owned.
