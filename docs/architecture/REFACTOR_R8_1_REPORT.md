# R8.1 — Candidate Semantic Contracts + PostgreSQL / pgvector Persistence Boundary

## Before

Semantic persistence was implemented in the root `main` package by
`PostgresCandidateSemanticRepository` in
`postgres_candidate_semantic_repository.go`. That file owned the semantic
document SQL, JSONB encoding/decoding, timestamp reconstruction, vector text
encoding, validation, and cosine search query.

Infrastructure-neutral semantic values and the persistence contract were also
declared in `candidate_semantic.go`, together with Candidate document-building
and truth-eligibility policy. The root `CandidateSemanticIndexService` and
`CandidateSemanticSearchService` owned indexing/retrieval orchestration. The
HTTP embedding provider remained in `candidate_semantic_provider.go`.

The post-commit flow was already:

```text
canonical Candidate mutation
        ↓ commit
semantic indexing / embedding
        ↓
success or stale-index warning
```

## Classification

| Symbol / area | Before owner | After owner | Decision |
| --- | --- | --- | --- |
| `CandidateSemanticDocument`, result, query, entity kind, evidence and truth snapshot | root semantic model | `internal/semantic` | moved as aliases-compatible pure values |
| `CandidateSemanticRepository` | root semantic model | `internal/ports` | moved as a consumer-facing persistence contract |
| `CandidateSemanticDocumentDraft`, index states and document builder | root | root | remains Candidate/indexing policy for R8.2 |
| `CandidateSemanticIndexService` | root | root | remains post-commit/indexing orchestration |
| `CandidateSemanticSearchService` | root | root | remains embedding/retrieval/truth-filtering orchestration |
| `OpenAICompatibleEmbeddingProvider` | root | root | remains provider boundary; deferred to R8.2 |
| semantic document INSERT/UPSERT/DELETE/SELECT | root PostgreSQL repository | `internal/adapters/storage/postgres/semantic.go` | moved |
| pgvector text encode/scan | root PostgreSQL repository | PostgreSQL adapter | moved; no pgvector type leaks |
| JSONB and exact timestamp mechanics | root PostgreSQL repository | PostgreSQL adapter | moved |
| root repository name and constructor | root | thin aliases/constructors | compatibility retained |
| `postgres_compat_helpers.go` | root temporary helpers | deleted | dead after semantic SQL extraction; adapter already owns equivalent mechanics |
| migrations and schema | migrations | migrations | unchanged |
| config fields | `internal/config` | `internal/config` | unchanged |
| Candidate canonical repository/store | PostgreSQL adapter | PostgreSQL adapter | unchanged and semantically independent |

## Semantic value model

Package: `hh-ai-responder/internal/semantic`

Values:

- `CandidateSemanticEntityType`
- `CandidateSemanticEvidenceRef`
- `CandidateSemanticTruthSnapshot`
- `CandidateSemanticDocument`
- `CandidateSemanticResult`
- `CandidateSemanticSearchQuery`
- `CandidateSemanticEmbeddingDimensions`

Dependencies: standard library only (`math`, `time`). PostgreSQL, pgx,
pgvector, embedding-provider DTOs, SQL, and Candidate policy are absent.

Provider DTOs remain root-owned (`embeddingRequest`, `embeddingResponse`).
PostgreSQL-private values are `semanticVector` and its text parser/scanner.

The root package keeps type aliases for the existing names, so current indexer,
search, JSON, and test call sites retain source compatibility without leaving
duplicate structural definitions.

## Semantic ports

`internal/ports.CandidateSemanticRepository` is the single cohesive contract
required by the current call graph:

- upsert one supplied document;
- delete one document by Candidate/entity scope;
- list documents for one Candidate;
- run a Candidate-scoped vector search.

It imports only `context` and `internal/semantic`. It does not expose pgx,
pgvector, SQL, provider DTOs, or Candidate canonical repository operations.
The existing `CandidateReader`, `CandidateWriter`, and
`CandidateMutationWriter` contracts are unchanged.

## PostgreSQL adapter

Implementation: `internal/adapters/storage/postgres/semantic.go`

Type: `postgresstorage.SemanticRepository`

Constructors:

- `NewSemanticRepository(*pgxpool.Pool)`
- `NewSemanticRepositoryForTx(pgx.Tx)`

Dependencies: `internal/semantic`, `internal/ports`, pgx/pgxpool, and the
standard library. The adapter does not import the root package, embedding
client, HH, dashboard, conversation policy, or mutation service.

Compile-time assertion:

```go
var _ ports.CandidateSemanticRepository = (*SemanticRepository)(nil)
```

## SQL contract

Semantic table: `candidate_semantic_documents`.

Classification:

- document source: the full semantic document row;
- embedding vector: `embedding`, with `embedding_model` and
  `embedding_dimensions`;
- index metadata: `content_hash`, `indexed_at`, `indexed_at_ns`,
  `source_updated_at_ns`, `truth_snapshot`, and `metadata`;
- search support: `candidate_semantic_documents_candidate_type_idx` plus the
  unique `(candidate_id, entity_type, entity_id)` constraint;
- separate semantic metadata table: none.

Columns round-tripped by the adapter include document ID, Candidate ID,
entity kind/ID, content, content hash, embedding, model, dimensions, truth
snapshot, indexed/source timestamps, and metadata. Timestamps retain the
existing nanosecond companion-column behavior. Schema and migrations are
unchanged.

## Documents

Identity remains the persisted `(candidate_id, entity_type, entity_id)` key.
The adapter does not derive document IDs, choose documents, or generate
chunks. The existing root builder/indexer supplies the authoritative desired
document values.

Upsert preserves the existing conflict behavior: rows are updated only when
content hash, embedding model, or embedding dimensions differ. Current
replacement behavior is mechanical and caller-driven: the root indexer
upserts changed documents and deletes stale/ineligible documents one at a
time. The adapter does not claim whole-Candidate replacement atomicity.

Delete remains Candidate-scoped by Candidate ID, entity type, and entity ID.
List remains Candidate-scoped and deterministically ordered by entity type and
entity ID. The adapter never writes canonical Candidate truth.

## Vector search

Metric: cosine distance via pgvector `<=>`.

Score conversion: `score = 1 - distance`; higher score is better. The public
contract remains score-based, with an optional inclusive minimum score in
`[0, 1]`.

Ordering: `score DESC, entity_type, entity_id`.

Limit: default `5`, maximum `50`, preserving the existing behavior.

Filters: mandatory Candidate ID and persisted embedding dimension; optional
entity-type filter and minimum score. No answerability, truth exposure,
vacancy relevance, conversation ranking, or source-priority policy is in SQL.

Search results are retrieval hints, not Candidate evidence. Existing root
search/context policy still validates content hashes/models and applies
employer-safe filtering before context use.

## Transaction semantics

The semantic adapter can operate on a pool or an explicitly supplied pgx
transaction. It does not open or close pools, run migrations, retry, spawn
goroutines, or join the canonical Candidate transaction.

Current per-document writes retain their prior transaction behavior. No new
whole-index atomicity is claimed. The canonical Candidate transaction and the
semantic indexing transaction remain separate.

## Canonical Candidate separation

Canonical Candidate source of truth: `postgresstorage.CandidateRepository`.

Semantic index: `postgresstorage.SemanticRepository`, a rebuildable
Candidate-scoped retrieval projection.

Canonical transaction dependency on semantic: **NONE**.

`CandidateStore` and `CandidateTx` have no semantic dependency. Semantic search
cannot mutate Candidate truth, Candidate version, claims, or Candidate
Knowledge.

## Post-commit safety

- Candidate commit before semantic indexing: **YES**
- Semantic failure rolls back Candidate: **NO**
- Candidate conflict invokes semantic indexing: **NO**

The existing mutation service and stale-index warning contract remain root
owned and unchanged.

## Embedding provider

Owner: **ROOT / DEFERRED R8.2**.

The PostgreSQL adapter receives already-produced `[]float32` vectors and an
opaque model identifier. It performs no HTTP, tokenization, retry, rate-limit,
batch embedding, or default-model selection. The 1536-dimension schema
contract is preserved; vectors are not padded or truncated.

## Root compatibility

`PostgresCandidateSemanticRepository` is a type alias of
`postgresstorage.SemanticRepository`.

`NewPostgresCandidateSemanticRepository` and the existing unexported
transaction constructor remain thin compatibility constructors. The root
contains no runtime semantic SQL, pgvector operator, semantic row scanner, or
vector codec.

## `postgres_compat_helpers`

Moved: semantic JSON/timestamp/vector mechanics now live in the PostgreSQL
adapter; the adapter reuses its existing package-local JSON/timestamp helpers.

Remaining: none.

Reason: after the semantic repository extraction, every symbol in the root
temporary helper file was unused. Removing it avoids retaining a dead root
database abstraction and leaves no semantic persistence implementation there.

## Fingerprints and hashes

`CandidateSemanticDocument.ContentHash` is the semantic document fingerprint
used for index freshness and retrieval validation. Canonical Candidate
fingerprints (`candidateFingerprint`) and `RelevantKnowledgeHash` are separate
root-owned contracts. R8.1 does not consolidate or reinterpret them.

## Integration

Postgres: **SKIPPED** — `POSTGRES_TEST_DATABASE_URL` was not configured.

pgvector: **SKIPPED** — integration was not run because no Postgres test
database was configured. No extension or user database was modified.

Document round-trip: **SKIPPED**

Search: **SKIPPED**

Candidate isolation: **SKIPPED**

Pure adapter coverage includes vector encode/scan, invalid vector handling,
schema-dimension validation, and nullable JSON codec behavior. The existing
opt-in root PostgreSQL test covers semantic upsert/search/list/delete,
embedding round-trip, Candidate isolation, and post-commit failure isolation
when configured.

## Tests

Focused adapter tests: `go test ./internal/adapters/storage/postgres/...`

The ordinary suite and the existing root semantic tests pass without requiring
PostgreSQL. The final verification results are recorded below.

## Size

- Root semantic persistence before: 220 LOC
- Root compatibility after: 21 LOC
- PostgreSQL semantic adapter: 286 LOC
- Root temporary compatibility helpers after: 0 LOC

The adapter is larger than the former root file because it now includes the
explicit pgvector scanner and pure adapter tests while preserving the same SQL
contract.

## Dependencies

```text
internal/semantic -> standard library
internal/ports -> internal/semantic
postgres semantic adapter -> internal/semantic + internal/ports + pgx
root compatibility -> postgres adapter
root index/search -> root compatibility contract + embedding provider
```

`internal/candidate` remains standard-library-only and does not import semantic
values, ports, pgx, or provider packages.

## Behavior

| Area | Result |
| --- | --- |
| Semantic persistence | UNCHANGED |
| Vector metric and ordering | UNCHANGED |
| Canonical Candidate | UNCHANGED |
| Candidate versioning and mutation | UNCHANGED |
| Post-commit semantic failure isolation | UNCHANGED |
| Embedding provider | UNCHANGED / ROOT |
| JSON storage | UNCHANGED |
| Other PostgreSQL repositories | UNCHANGED |
| HH and AI writes | UNCHANGED / 0 |
| Schema and migrations | UNCHANGED |

## Verification

gofmt: **PASS**

go test `-count=1`: **PASS**

race: **PASS**

vet: **PASS**

build: **PASS**

diff: **PASS**

node: **PASS**

Docker: **SKIPPED** — Docker CLI is available, but the Docker daemon is not
running in the environment.

LIVE HH WRITES: **0**

## Ready for R8.2

**READY**. Required code verification completed successfully; only the
optional Docker image build was skipped because its daemon was unavailable.

R8.2 remains responsible for Candidate semantic indexing/retrieval use-case
extraction and the embedding boundary. This stage did not begin that work.
