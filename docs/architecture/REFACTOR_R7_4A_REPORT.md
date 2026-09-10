# R7.4a — Candidate PostgreSQL Canonical Repository Boundary

## Before

`PostgresCandidateRepository` was root-owned in `postgres_candidate_repository.go`.
It implemented canonical Candidate reads, full aggregate replacement, import,
version-checked replacement, child-row SQL, JSONB codecs, timestamp handling,
error translation, and candidate fingerprinting.

`PostgresCandidateStore` was root-owned and remains root-owned. It starts and
commits/rolls back the Candidate transaction and exposes the compatibility
`CandidateTx` facade.

`CandidateTx` created a transaction-bound root repository and exposed
`CandidateWriter`, `CurrentCandidate`, and `PersistCandidateIfVersion` to the
mutation service. Its lifecycle responsibility is unchanged.

Canonical SQL covered the `candidates` aggregate root and all current-state
child tables listed below. No semantic SQL was part of that repository.

Versioning uses the persisted `candidates.version` integer. Imported canonical
aggregates retain their supplied positive version; versioned mutation requires
`candidate.Version == expectedVersion + 1`, locks the row with `FOR UPDATE`,
and returns the existing candidate version conflict error on mismatch.

Claims are Candidate canonical claims in `candidate_claims`; they are distinct
from conversation claims. Stories are Candidate canonical stories stored as a
relational row plus JSONB payload and story references.

Semantic coupling was already outside this repository. The mutation service
runs optional semantic indexing only after the Candidate transaction commits.

## Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `CurrentCandidate` | root `PostgresCandidateRepository` | `postgresstorage.CandidateRepository` | moved |
| `PersistCandidate` | root `PostgresCandidateRepository` | `postgresstorage.CandidateRepository` | moved |
| `PersistCandidateIfVersion` | root `PostgresCandidateRepository` | `postgresstorage.CandidateRepository` | moved |
| `ImportCandidate` | root repository | `postgresstorage.CandidateRepository` | moved as canonical import primitive |
| canonical child SELECT/INSERT/DELETE SQL | root repository | `postgresstorage.CandidateRepository` | moved |
| Candidate JSONB/timestamp codecs | root repository/helpers | PostgreSQL Candidate adapter | moved |
| Candidate error translation | root repository | PostgreSQL Candidate adapter | moved |
| `PostgresCandidateStore` | root | root | retained for R7.4b |
| `CandidateTx` | root | root | retained for R7.4b |
| mutation legality | `internal/usecase/candidatemutation` | unchanged | not moved |
| semantic indexing and pgvector | semantic root files | semantic root files | deferred to R8 |
| migration orchestration | root `candidate_migration.go` | root | retained |

## New adapter

Implementation: `postgresstorage.CandidateRepository` in
`internal/adapters/storage/postgres/candidate.go`.

It implements:

- `ports.CandidateReader`;
- `ports.CandidateWriter`;
- `ports.CandidateMutationWriter`.

The adapter persists and returns `internal/candidate.Candidate` values through
the port contracts. It has no dependency on the root package, JSON storage,
HH, AI, dashboard, CLI/config, or semantic clients.

Constructors are:

- `NewCandidateRepository(pool)`;
- `NewCandidateRepositoryForID(pool, candidateID)`;
- `NewCandidateRepositoryForTx(tx)`;
- `NewCandidateRepositoryForTxID(tx, candidateID)`.

## SQL contract

Tables touched by the canonical repository:

| Table | Classification |
|---|---|
| `candidates` | canonical aggregate root and persisted version |
| `candidate_contacts` | canonical aggregate |
| `candidate_external_references` | canonical aggregate |
| `candidate_education` | canonical aggregate |
| `candidate_languages` | canonical aggregate |
| `candidate_experiences` | canonical aggregate |
| `candidate_skills` | canonical aggregate |
| `candidate_skill_capabilities` | canonical aggregate child rows |
| `candidate_skill_uses` | canonical aggregate child rows |
| `candidate_projects` | canonical aggregate |
| `candidate_achievements` | canonical aggregate |
| `candidate_preferences` | canonical aggregate |
| `candidate_constraints` | canonical aggregate |
| `candidate_claims` | Candidate claim storage |
| `candidate_stories` | Candidate story storage |
| `candidate_story_refs` | Candidate story storage |
| `candidate_unknowns` | canonical Candidate knowledge state |
| `candidate_knowledge_proposals` | canonical Candidate knowledge state |
| `candidate_knowledge_events` | append-only canonical Candidate history |
| `candidate_knowledge_sources` | canonical provenance index |
| `candidate_evidence` | canonical evidence index |

Semantic tables: deferred. The repository does not reference
`candidate_semantic_documents`, pgvector, embeddings, semantic chunks, or
semantic search.

JSONB encoding uses the existing `encoding/json` representation. Existing
JSONB payloads remain readable. Empty optional JSON values map to SQL `NULL`
where the previous implementation did so; required collection/object values
retain their JSON representation. Nullable strings, timestamps, timestamp
nanoseconds, optional proposal payloads, project sources, resume facts,
metadata, and evidence preserve their prior NULL/empty distinctions.

Timestamp columns continue to use the existing timestamptz plus nanosecond
companion columns. Reads prefer the exact nanosecond value when present and
otherwise use PostgreSQL timestamptz.

No schema changes: YES.

## Canonical read

`CurrentCandidate` reads the selected aggregate root, reconstructs all
canonical child collections, loads Candidate claims/stories/unknowns/proposals
and append-only events, restores metadata and JSONB fields, and applies the
existing deterministic ordering. SQL child reads retain explicit `ORDER BY`
clauses. It returns the domain Candidate and validates the reconstructed
canonical state.

Semantic infrastructure is not consulted. A semantic outage or disabled
semantic provider cannot prevent a canonical read.

## Full persist

`PersistCandidate` validates the supplied complete canonical aggregate, opens a
transaction when called on a pool-bound repository, replaces current-state
child rows, updates or inserts the aggregate root, and inserts all canonical
children in that same transaction. Append-only event rows retain their
existing conflict-safe insert behavior. Delete-and-reinsert replacement
semantics were preserved; no UPSERT redesign was introduced.

`ImportCandidate` remains the additive migration primitive: an identical
existing aggregate is a no-op, a different existing aggregate is a repository
conflict, and a missing aggregate is inserted transactionally.

## Versioned persist

Expected version: the caller supplies `expectedVersion`; the replacement
Candidate must have `Version == expectedVersion + 1`.

Conflict: `postgresstorage.ErrConflict`, preserved through the root
`ErrConflict` compatibility alias, with `errors.Is` behavior and the existing
expected/current version message fields.

Version increment: the repository persists the caller-supplied next version;
it does not generate or normalize a version.

Atomicity: the transaction-bound repository locks the aggregate row with
`SELECT version ... FOR UPDATE`, checks the expected version, replaces the
root and all canonical child rows, and leaves commit ownership to
`PostgresCandidateStore`. Any child failure returns through the transaction and
the store rolls the complete aggregate back.

The use-case stale/base check remains in `candidatemutation`. The database
version conflict remains a persistence-level conflict. These layers were not
collapsed.

## Claims

Candidate claims are persisted and reloaded with identity, subject, field,
value, polarity, state, conflict-set ID, supersession ID, and knowledge
metadata. The repository stores already-decided values. It does not decide
confirmed/verified/hypothesis/unknown state, resolve conflicts, apply source
priority, or promote confidence.

## Stories

Canonical PostgreSQL stories retain their relational IDs and titles, complete
JSONB payload fields, profile references, and relational story references.
Story persistence is not downgraded to JSON parity and has no semantic-index
dependency.

## Truth / provenance

Repository interpretation: NONE. The adapter validates persistence shape and
allowed serialized values but does not perform truth promotion, source
priority, proposal transitions, employer-safe projection, or mutation policy.

Round trip: PASS for the existing opt-in PostgreSQL contract; the non-DB test
suite remains independent of PostgreSQL.

## Backend asymmetry

| Capability | JSON | PostgreSQL |
|---|---:|---:|
| CandidateReader | YES | YES |
| CandidateWriter | NO | YES |
| CandidateMutationWriter | NO | YES |

PostgreSQL retains canonical stories, claims, transaction-bound replacement,
and optimistic concurrency. JSON was not expanded to emulate those features.

## Transaction boundary

Repository transaction support: the adapter accepts a pool for standalone
full persistence and a typed `pgx.Tx` for an already-owned Candidate
transaction. No pgx type appears in ports.

`CandidateTx` remaining root responsibilities:

- begin one Candidate transaction indirectly through the store;
- expose commit/rollback callback lifecycle;
- expose the compatibility Candidate writer/repository view;
- delegate all canonical SQL to `postgresstorage.CandidateRepository`.

`PostgresCandidateStore` remaining root responsibilities:

- validate store/callback configuration;
- call `pool.Begin`;
- construct `NewCandidateRepositoryForTxID`;
- invoke the callback;
- commit on success and rollback on failure.

Semantic post-commit hooks remain on `CandidateMutationService`, outside the
repository and outside the transaction callback.

## Semantic

Repository dependency: NONE.

Post-commit behavior: UNCHANGED. Candidate DB mutation commits first; optional
semantic reindexing runs afterward; semantic failure does not roll back the
canonical Candidate mutation.

R8 ownership: semantic documents, embeddings, pgvector, and semantic search.

## Root compatibility

`PostgresCandidateRepository` is now a type alias to
`postgresstorage.CandidateRepository`. Existing root constructors delegate to
the adapter constructors. Root migration helpers delegate fingerprint and
canonical validation compatibility calls to the adapter.

Canonical Candidate SQL in root: NONE. SQL strings remaining in root Candidate
tests are test cleanup only; semantic SQL remains in the semantic repository
and its tests.

## Tests

Pure adapter tests cover port assertions, version-conflict preconditions,
deterministic ordering, NULL mapping, fingerprints, and PostgreSQL conflict
translation.

PostgreSQL integration: SKIPPED unless `POSTGRES_TEST_DATABASE_URL` is
configured. The existing root integration contract remains available and
covers import/read round trip, transactional update, rollback, stories,
claims, and exact timestamps.

Mutation regressions: PASS in the normal test suite.

Semantic post-commit: PASS in the existing regression suite.

Write safety: PASS; no HH write path was invoked.

## Size

Root canonical Candidate PostgreSQL LOC before: 1231.

Root compatibility LOC after: 54.

PostgreSQL adapter Candidate LOC: 1296, including the extracted canonical
repository, adapter aliases/constructors, and persistence tests.

## Dependencies

The Candidate adapter depends only on:

- `internal/candidate`;
- `internal/ports`;
- pgx/pgconn/pgtype/pgxpool;
- standard library packages.

Dependency direction is preserved:

```text
internal/candidate -> stdlib
internal/ports -> internal/candidate
postgresstorage Candidate -> internal/candidate + internal/ports + pgx
```

There is no adapter-to-adapter dependency and no root-package dependency.

## Behavior

Candidate: UNCHANGED.

Mutation: UNCHANGED; policy remains in `candidatemutation`.

Versioning: UNCHANGED.

Semantic: UNCHANGED / ROOT.

Other domains: UNCHANGED.

HH: UNCHANGED; LIVE HH WRITES = 0.

## Verification

`gofmt`: PASS.

`go test -count=1 ./...`: PASS.

`go test -race ./...`: PASS.

`go vet ./...`: PASS.

`go build ./...`: PASS.

`git diff --check`: PASS.

`node --check web/app.js`: PASS.

Focused Candidate, ports, JSON storage, PostgreSQL storage, candidate context,
candidate mutation, and candidate acquisition tests: PASS.

PostgreSQL integration: SKIPPED; `POSTGRES_TEST_DATABASE_URL` is not
configured.

Docker: SKIPPED; the Docker client is installed but no daemon is available.

LIVE HH WRITES: 0.

## Ready for R7.4b

READY.

- canonical Candidate SQL has one runtime owner;
- optimistic versioning semantics remain unchanged;
- Candidate aggregate persistence remains atomic;
- CandidateRepository has no semantic dependency;
- root CandidateTx/PostgresCandidateStore can be reduced in R7.4b without
  moving mutation policy.

R7.4b is not started by this change.
