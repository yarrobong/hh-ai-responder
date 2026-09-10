# Before

Document builder: root `candidate_semantic.go`; it built stories, projects and
achievements, applied truth eligibility, and generated deterministic content
hashes.

Truth eligibility: root `candidate_semantic.go`, allowing only confirmed or
verified metadata and safe canonical references; hypotheses, unknowns and
negative facts were not promoted.

Index service: root `CandidateSemanticIndexService` in
`candidate_semantic_service.go`.

Embedding provider: root `OpenAICompatibleEmbeddingProvider` in
`candidate_semantic_provider.go`, including its HTTP DTOs and request mapping.

Post-commit integration: `CandidateMutationService` committed PostgreSQL first,
read the canonical Candidate back, and then called `Reindex`; index failure
returned a stale-index warning without compensation.

Retrieval service: root `CandidateSemanticSearchService`; intentionally kept
root-owned and unchanged in this stage.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `CandidateSemanticDocumentDraft` | root | `internal/usecase/candidatesemanticindex` | moved; root alias retained |
| `BuildSemanticDocument` | root | `internal/usecase/candidatesemanticindex` | moved; root delegating wrapper |
| `BuildCandidateSemanticDocuments` | root | `internal/usecase/candidatesemanticindex` | moved; root delegating wrapper |
| truth eligibility helpers | root | `internal/usecase/candidatesemanticindex` | moved unchanged |
| content/hash generation | root | `internal/usecase/candidatesemanticindex` | one implementation |
| `CandidateSemanticIndexService` | root | `candidatesemanticindex.Service` | moved; root type alias/constructor |
| index planning | root | `candidatesemanticindex.Service` | moved unchanged |
| `EmbeddingProvider` | root | `internal/ports` | new narrow port |
| OpenAI-compatible HTTP | root | `internal/adapters/embedding/openai` | moved; DTOs private to adapter |
| `CandidateSemanticSearchService` | root | root | deferred to R8.2b |
| `CandidateMutationService` | root | root | post-commit orchestration retained |
| semantic PostgreSQL repository | R8.1 adapter | R8.1 adapter | unchanged |

# Indexing use case

Package: `internal/usecase/candidatesemanticindex`.

Dependencies: `internal/candidate`, `internal/semantic`, `internal/ports`, and
stdlib only. It imports no root package, PostgreSQL, pgx, HTTP, HH, dashboard,
or conversation policy.

Input: canonical `candidate.Candidate` snapshot, existing semantic documents,
and an optional embedding provider supplied by composition.

Output/effect: deterministic drafts and an index report; when applied, ordered
embedding, supplied-document upserts, then stale/ineligible deletes through
`ports.CandidateSemanticRepository`. It never mutates Candidate.

# Document policy

Entity kinds: story, project, achievement.

Identity: `(candidate_id, entity_type, entity_id)`; entity IDs are derived by
the indexing use case and are not derived by PostgreSQL.

Content: existing labels, field order, whitespace normalization, list ordering,
story rendering, granularity, and exact values are preserved.

Hash: SHA-256 of the existing normalized JSON hash payload; content hash is
kept separate from Candidate fingerprints and `RelevantKnowledgeHash`.

Truth eligibility: confirmed and verified metadata only, with active/safe
claims, projects, skills, stories, experiences, and achievement references as
previously required. Hypothesis, unknown, and negative facts retain their prior
behavior and are never promoted by confidence.

Ordering: documents are sorted by entity type and entity ID; provider inputs
follow changed-document order, and response indices are preserved by the
adapter.

# Index plan

Existing: loaded once from `CandidateSemanticRepository.ListDocuments`.

Desired: built once from the canonical Candidate snapshot.

Unchanged: same content hash, model, and dimensions.

Changed: existing identity whose content hash, model, or dimensions differ.

New: desired eligible identity absent from the existing list.

Stale: existing identity absent from desired canonical entities.

Ineligible: desired canonical entity that fails the existing truth/reference
policy; it is not embedded and is deleted if an old row exists.

# Operation ordering

`Plan` loads existing rows, builds desired drafts, compares identity/hash/model/
dimensions, and sorts the report. `Reindex(..., true)` embeds all new/changed
texts in one ordered batch, upserts each returned vector in order, and only
then deletes stale or newly ineligible rows. Dry-run planning performs no
embedding or repository writes.

# Partial failure semantics

Embedding failure: no replacement upsert or stale delete occurs; the canonical
Candidate remains committed and the caller reports a stale-index warning.

Upsert failure: prior upserts remain, later upserts and all deletes stop; the
old index can therefore be partly stale, matching the existing sequential
behavior.

Delete failure: prior replacements and deletes remain, later deletes stop.

Candidate read failure: indexing is not attempted and the caller returns the
established stale-index warning.

Candidate transaction failure, rollback, conflict, or commit error: the
post-commit hook is not reached.

# Embedding contract

Port: `ports.EmbeddingProvider` with ordered batch `Embed`, `Model`, and
`Dimensions` methods; no HTTP DTOs or endpoint details leak through it.

Values: vectors remain neutral `[][]float32`; semantic persistence values stay
in `internal/semantic`.

Model: provider-reported model remains authoritative for freshness and stored
document metadata.

Dimensions: provider-reported dimensions remain authoritative to the indexer;
the established 1536 default/persistence contract is unchanged.

Batch: one call for all changed/new documents, preserving input and output
index mapping. Count and vector-dimension mismatches fail closed.

# Embedding adapter

Package: `internal/adapters/embedding/openai`.

HTTP behavior: same POST endpoint `/v1/embeddings`, JSON model/input payload,
optional Bearer authorization, timeout behavior, non-2xx handling, malformed
response handling, indexed response ordering, count validation, and dimension
validation. No retries were introduced.

Config ownership: ROOT / COMPOSITION. The adapter receives typed `Options` and
does not read environment variables.

Secrets: API keys remain in memory only; authorization and private document
content are not logged or persisted by the adapter.

# Post-commit

Candidate COMMIT first: YES

Index on conflict: NO

Index on rollback: NO

Index on commit error: NO

Index failure rolls back Candidate: NO

The indexing source remains the fresh committed CandidateReader read-back,
preserving commit visibility and Candidate version behavior.

# Semantic repository

R8.1 Postgres implementation: UNCHANGED.

Upsert contract characterization: the existing PostgreSQL upsert update guard
continues to use content hash, embedding model, and embedding dimensions. No
SQL, schema, migration, pgvector, or search changes were made.

# Retrieval

CandidateSemanticSearchService: ROOT / DEFERRED R8.2b.

CandidateContext: UNCHANGED.

# Root compatibility

Index service: root constructor delegates to `candidatesemanticindex.NewService`
and the root type is an alias.

Document types: root aliases and builder wrappers delegate to the use case.

Embedding provider: root constructor wrapper translates legacy arguments into
the adapter options; it contains no HTTP implementation.

Duplicate implementation: NONE. There is one indexing builder/policy and one
embedding HTTP adapter.

# Tests

Indexer: focused deterministic-build, truth-safety, idempotency, model change,
dimension change, stale ordering, embedding failure, and count-mismatch tests
in `internal/usecase/candidatesemanticindex`.

Provider: `httptest.Server` coverage for request/auth/model/batch ordering,
non-2xx, malformed response, wrong count, wrong dimensions, and cancellation
in `internal/adapters/embedding/openai`.

Post-commit: existing root mutation tests and PostgreSQL opt-in contract tests
remain in place.

Postgres: existing R8.1 adapter tests continue to pass; integration remains
opt-in through `POSTGRES_TEST_DATABASE_URL`.

Root regressions: existing semantic search, CandidateContext, RelevantKnowledge,
mutation, and write-preflight test suites remain unchanged in behavior.

# Size

Root indexing LOC before: approximately 502 across the root builder and index
service portions of the audited files.

Root provider LOC before: 139.

Root compatibility after: 62 for document aliases/wrappers plus the retained
root retrieval service and a 76-line provider composition wrapper.

Index usecase LOC: 508 including package documentation, document policy, and
index service.

Embedding adapter LOC: 151.

# Dependencies

```text
internal/semantic
    -> stdlib
internal/candidate
    -> stdlib
internal/ports
    -> candidate, semantic values
internal/usecase/candidatesemanticindex
    -> candidate, semantic, ports
internal/adapters/embedding/openai
    -> ports, semantic, net/http, stdlib
internal/adapters/storage/postgres/semantic
    -> ports, semantic, pgx
```

The index use case does not import PostgreSQL or the concrete embedding
adapter; the embedding adapter does not import PostgreSQL.

# Behavior

Indexing: UNCHANGED

Embedding: UNCHANGED

Retrieval: UNCHANGED / ROOT

Candidate: UNCHANGED

Postgres: UNCHANGED

HH: UNCHANGED; live HH writes = 0.

# Verification

gofmt: PASS

go test: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED (Docker daemon unavailable)

LIVE HH WRITES: 0

# Ready for R8.2b

R8.2b — Candidate Semantic Retrieval / Context Boundary

READY, subject to the final verification commands below.

The indexing policy has one importable owner, the embedding HTTP behavior has
one adapter owner, post-commit behavior is unchanged, retrieval remains root
owned, and semantic indexing cannot modify canonical Candidate state.
