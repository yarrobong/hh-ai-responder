# Before

Search service: root `CandidateSemanticSearchService` in
`candidate_semantic_service.go`. It owned query validation, one-query
embedding, the scoped semantic repository call, the canonical Candidate read,
deterministic eligibility rebuild, content-hash/model filtering, and ordered
result return.

Query embedding: root `CandidateSemanticSearchService.Search` called the
root-compatible `EmbeddingProvider` alias directly with the caller's exact
query text.

Freshness: the root service rebuilt current semantic drafts and dropped hits
whose content hash differed from the current draft or whose model differed
from the provider model. Ineligible and missing canonical entities were
therefore dropped. Repository ordering was retained.

Canonical validation: root search used `CandidateRepository`, already aliased
to `ports.CandidateReader`, and `BuildCandidateSemanticDocuments`, already
extracted by R8.2a. Root context projection separately rebuilt employer-safe
prompt text in `BuildSafeSemanticContext`.

Context integration: `semantic_retrieval.go`, `conversation_context.go`,
cover-letter orchestration, and `RelevantKnowledge` consumed semantic results
as optional relevance hints. CandidateContext remained the deterministic owner
of answerability and safety.

Fallback: conversation and cover-letter callers already treated retrieval
errors as optional and retained canonical/structured context. This behavior is
unchanged.

# Classification

| Symbol | Before owner | After owner | Decision |
|---|---|---|---|
| `CandidateSemanticSearchService.Search` | root `candidate_semantic_service.go` | `internal/usecase/candidatesemanticsearch.Service` | moved; root alias retained |
| `CandidateSemanticQuery` | root `candidate_semantic.go` | `candidatesemanticsearch.Query` | moved; root alias retained |
| query embedding orchestration | root search service | search use case | moved unchanged; one provider call |
| scoped vector search | root search service / repository port | search use case + `ports.CandidateSemanticRepository` | moved; SQL remains in adapter |
| canonical Candidate read | root `CandidateRepository` alias | search use case via `ports.CandidateReader` | moved behind narrow port |
| deterministic document/hash policy | R8.2a index use case | `candidatesemanticindex.BuildCandidateSemanticDocuments` | reused; no duplicate builder |
| eligibility/freshness filtering | root search service | search use case | moved; current hash/model behavior preserved |
| `SemanticRetrievalRequest` / retriever capability | root retrieval file | search use case; root aliases retained | moved as API types; no context dependency added |
| query builders and retrieval routing | root context/vacancy orchestration | root | retained; caller supplies plain query |
| `BuildSafeSemanticContext` | root context integration | root | retained; canonical employer-safe projection unchanged |
| `RelevantKnowledge` integration | root | root | unchanged; semantic selections remain separate |
| Candidate mutation/index post-commit | root mutation flow | root + R8.2a index use case | unchanged |
| PostgreSQL semantic repository | R8.1 adapter | R8.1 adapter | unchanged; no SQL/schema changes |

# Search use case

Package: `internal/usecase/candidatesemanticsearch`.

Dependencies: `internal/ports`, `internal/semantic`, and
`internal/usecase/candidatesemanticindex` plus the standard library. The
service has no Candidate writer, PostgreSQL, pgx, concrete embedding adapter,
HH, dashboard, conversation store, or AI completion dependency.

Input: `Query` containing Candidate ID, plain query text, optional entity
types, limit, and minimum score. `RetrievalRequest` adds the descriptive
higher-level purpose used by existing callers.

Output: `[]semantic.CandidateSemanticResult` in repository order after safe
freshness filtering. Results identify relevant canonical entities; they do not
confirm Candidate facts.

# Retrieval flow

Query: the caller constructs the existing plain query. No LLM intent
classification, history ingestion, vacancy loading, or query prefix was added.

Embedding: exactly one `ports.EmbeddingProvider.Embed` call receives the exact
query string in a one-element batch. Provider count and vector-dimension
validation fail closed.

Repository search: one `ports.CandidateSemanticRepository.Search` call uses
the requested Candidate ID, query vector, entity-type filter, normalized
limit, and caller minimum score. The repository remains authoritative for
score calculation and ordering (`1 - cosine distance`, higher is better).

Candidate read: after repository search, one fresh `ports.CandidateReader`
snapshot is loaded, matching the prior operation order. A Candidate ID
mismatch fails closed so a result cannot cross Candidate scope.

Canonical document validation: the current Candidate is passed to
`candidatesemanticindex.BuildCandidateSemanticDocuments`. Only currently
eligible entities with matching content hashes and the current provider model
are accepted. Evidence references are refreshed from the current draft.

Result: remaining results preserve repository relative ordering. Stored
semantic text is not promoted into Candidate context; higher-level canonical
projection rebuilds employer-safe text from the fresh Candidate.

# Freshness contract

Candidate scope: the repository query is always Candidate-scoped, and the
fresh canonical Candidate ID must equal the requested ID.

Entity identity: `(entity_type, entity_id)` is matched against the current
eligible deterministic document set. Missing canonical entities are dropped.

Content hash: stored hash must equal the current SHA-256 document hash. A
mismatch drops the hit; no stale text or synchronous reindex is used.

Embedding model: stored result model must equal `EmbeddingProvider.Model()`.
Model mismatch drops the hit.

Dimensions: the query vector must have exactly the provider-reported
dimensions. No padding, truncation, alternate embedding, or reindex fallback
is used. Persistence-side stored-vector dimension checks remain in the
existing repository adapter.

Truth snapshot: the result API carries no independent truth snapshot field;
the established policy is preserved by rebuilding the current canonical
document and its eligibility/truth references, then requiring hash equality.
Canonical Candidate truth remains authoritative on every discrepancy.

Missing entity: the result is dropped and never reconstructed from stored
semantic content.

# Truth safety

Semantic result authority:

NONE — retrieval is a relevance hint only.

Canonical Candidate authority:

YES.

Hypothesis promotion:

NO. Ineligible hypothesis/unknown/negative entities are excluded by the
shared indexing eligibility policy, regardless of score.

Unknown inference:

NO. A stale or high-scoring row cannot make unknown Kubernetes answerable.

Score -> truth:

NO. Similarity never changes truth status, confidence, Candidate version,
proposals, or answerability.

# Score/ranking

Metric: existing repository metric, `1 - cosine distance`.

Score: higher is better; no second conversion or Go reranking was added.

Minimum: caller-supplied inclusive repository threshold; context retrieval
retains the existing default `0.55` when omitted.

Ordering: repository ordering, with stale/invalid hits removed and remaining
relative order preserved.

Limit: existing default `5`, cap `50`, with no overfetch compensation.

# Failure / fallback

Candidate read: error is returned by the search use case; optional callers
retain their existing deterministic/canonical fallback.

Embedding: provider errors and malformed count/dimension results fail closed
with `ErrSemanticUnavailable` compatibility wrapping.

Repository: error is returned unchanged, preserving existing caller behavior.

Stale hit: dropped; no warning mutation, cleanup write, or automatic reindex.

Empty index: empty results, not an inference that the Candidate knows nothing.

Semantic disabled: composition still omits the optional retriever/provider;
canonical Candidate and context construction continue without semantic calls.

# CandidateContext

Core package dependency changes: `internal/usecase/candidatecontext` remains
free of semantic persistence/provider dependencies.

Answerability: UNCHANGED — `ANSWERABLE`, `PARTIALLY_ANSWERABLE`, `UNKNOWN`,
and `RESTRICTED` remain owned by CandidateContext.

Semantic role: RETRIEVAL HINT ONLY. Existing employer-safe projection and
restricted/negative/unknown safeguards remain in the root context integration.

# RelevantKnowledge

Semantic index included in authoritative hash: NO; semantic selections remain
separate from canonical facts according to the existing contract.

Behavior: UNCHANGED. Semantic selections are not Candidate evidence and do not
override stale approval or targeted preflight behavior.

# Root compatibility

Search service: `CandidateSemanticSearchService` is a root type alias to
`candidatesemanticsearch.Service`; the existing constructor delegates to
`candidatesemanticsearch.NewService`.

Constructors: existing `NewCandidateSemanticSearchService` and
`NewCandidateSemanticIndexService` remain available. Root semantic value and
retrieval request aliases remain source-compatible.

Duplicate search implementation: NONE. Query embedding, repository search,
canonical read, and freshness filtering have one importable owner.

# Indexing

R8.2a: UNCHANGED. The extracted deterministic builder and index service remain
the only document interpretation and indexing implementation.

Post-commit: UNCHANGED. Candidate commit/read-back/index sequencing, conflict,
rollback, and index-failure behavior were not modified.

# Persistence

R8.1: UNCHANGED.

SQL changes: NONE. No migration, pgvector, semantic table, or vector-index
change was made.

# Embedding

R8.2a adapter: UNCHANGED. Retrieval depends only on
`ports.EmbeddingProvider`; no HTTP or provider DTO was copied into the use
case.

# Tests

Search: new typed-fake tests cover exact query text, one-call embedding,
Candidate scope, entity filters, limit/minimum score, empty/short query,
empty index, provider/repository/reader failures, and dimension failure.

Freshness: tests cover matching hash, stale hash, missing entity, model
mismatch, ineligible current entity, ordering, and Candidate scope mismatch.

Truth safety: shared index eligibility is reused; hypotheses and stale rows
cannot be surfaced. Existing CandidateContext regressions cover unknown
Kubernetes, negative facts, restricted facts, exact experience values, and
answerability.

CandidateContext: existing context, semantic routing, employer-safe projection,
and fallback tests pass unchanged.

Conversation: employer-message routing and semantic-failure fallback tests
pass unchanged.

Preflight: existing RelevantKnowledge and write-preflight tests pass; semantic
selections remain outside authoritative approval hashing.

Post-commit: existing commit/index success, failure, conflict, and rollback
coverage passes unchanged.

Postgres: existing R8.1 adapter tests pass; integration remains opt-in and was
not run without configured PostgreSQL test infrastructure.

# Size

Root search implementation before: approximately 44 lines in
`candidate_semantic_service.go`, plus the 18-line root `Retrieve` adapter.

Root compatibility after: 26 lines in `candidate_semantic_service.go`, with
request/retriever aliases retained in `semantic_retrieval.go`.

Search use case: 178 lines including package documentation and service code;
focused tests are 240 lines.

# Dependencies

```text
internal/semantic
    -> stdlib
internal/ports
    -> internal/candidate, internal/semantic
internal/usecase/candidatesemanticindex
    -> internal/candidate, internal/semantic, internal/ports
internal/usecase/candidatesemanticsearch
    -> internal/semantic, internal/ports, candidatesemanticindex
internal/usecase/candidatecontext
    -> internal/candidate
internal/adapters/storage/postgres
    -> internal/ports, internal/semantic, pgx
internal/adapters/embedding/openai
    -> internal/ports, internal/semantic, net/http
```

The search use case imports no PostgreSQL, pgx, concrete embedding adapter,
HH, or AI chat implementation.

# Behavior

Retrieval: UNCHANGED.

Candidate: UNCHANGED.

CandidateContext: UNCHANGED.

Indexing: UNCHANGED.

Postgres: UNCHANGED.

Embedding: UNCHANGED.

RelevantKnowledge: UNCHANGED.

Conversation: UNCHANGED.

HH: UNCHANGED; live HH writes = 0.

# Verification

gofmt: PASS

go test -count=1 ./...: PASS

race: PASS

vet: PASS

build: PASS

diff: PASS

node: PASS

Docker: SKIPPED (Docker daemon unavailable).

LIVE HH WRITES: 0.

# R8 status

Semantic values: EXTRACTED.

Semantic ports: EXTRACTED.

PostgreSQL/pgvector: EXTRACTED.

Indexing policy: EXTRACTED.

Embedding provider: EXTRACTED.

Retrieval: EXTRACTED.

R8: COMPLETE.

# Ready for next stage

R9.1 — HH Read Client / Transport Boundary

READY.

Semantic retrieval has one importable owner; Candidate remains canonical truth;
freshness and eligibility checks precede accepted hits; CandidateContext does
not depend on semantic storage; and indexing/post-commit behavior is unchanged.
