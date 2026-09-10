# Executive summary

S2: COMPLETE

Before, chat and embeddings implicitly shared `HH_AI_BASE_URL` and
`HH_AI_API_KEY`; semantic vectors were required to be `1536` dimensions by
the runtime and PostgreSQL schema.

After, chat remains on `HH_AI_*`, while embeddings use an explicit provider,
endpoint, key, model and dimensions contract. Every generated, stored and
queried vector is checked against that contract. A deterministic, non-secret
space ID prevents provider/model/endpoint/dimension mixing. Existing semantic
rows are preserved and marked legacy until an explicit reindex.

# Chat provider configuration

Chat continues to use:

- `HH_AI_BASE_URL`
- `HH_AI_API_KEY`
- `HH_AI_MODEL`

`CompletionProvider`, LLM workflows, retry policy, prompts and chat behavior
were not changed. Embedding settings cannot reroute chat calls.

# Embedding provider configuration

Embedding configuration is:

- `EMBEDDING_PROVIDER` — `openai` or `openai-compatible`; setting it explicitly
  enables semantic composition in PostgreSQL mode;
- `EMBEDDING_BASE_URL`;
- `EMBEDDING_API_KEY`;
- `EMBEDDING_MODEL`;
- `EMBEDDING_DIMENSIONS`.

The existing OpenAI-compatible adapter is reused for both supported embedding
provider names; no vendor-specific embedding logic was added.

# Compatibility fallback

When an embedding endpoint is empty, the effective endpoint is
`HH_AI_BASE_URL`. When an embedding key is empty, the effective key is
`HH_AI_API_KEY`. Explicit `EMBEDDING_*` values take precedence. This fallback
is embedding-only and does not make chat depend on embedding configuration.

The compatibility default model remains `text-embedding-3-small`, and the
compatibility default dimension remains `1536`. A configured dimension may be
any integer from `1` through `16000`.

# Embedding contract

Provider: `openai` or `openai-compatible`.

Model: configured `EMBEDDING_MODEL`, defaulting to
`text-embedding-3-small`.

Dimensions: configured `EMBEDDING_DIMENSIONS`, defaulting to `1536`.

Space identity: `sha256(provider, model, dimensions, sanitized endpoint)`.
The contract is represented by `semantic.EmbeddingContract`; production
providers expose it through the embedding port. Legacy in-process test/local
providers are isolated in a deterministic `legacy` space.

# Secret boundary

Space identity excludes API keys and authorization data. Endpoint sanitizing
removes URL userinfo, query parameters and fragments before fingerprinting.
Keys are retained only in runtime provider memory and are not printed by
status/help or persisted in semantic rows.

# Previous 1536 assumptions

The old domain constant is retained only as a source-compatibility/default
alias. Active semantic behavior now reads `EmbeddingContract.Dimensions`.
Adapter, indexer, retriever, memory repository and PostgreSQL repository
validate the configured dimension; there is no truncation, padding or runtime
dimension inference.

The historical `000004` migration remains unchanged as required. Its fixed
dimension is removed by the next migration rather than edited in place.

# Vector storage

Before: `embedding vector(1536)` plus
`embedding_dimensions = 1536`.

After: `embedding vector` (dimension-flexible), explicit provider/model/space
metadata, positive bounded dimensions, and a database check that
`embedding_dimensions = vector_dims(embedding)`. Application/repository code
also validates vector length and finite values before writes and before a
search query.

The representation follows pgvector's documented unconstrained `vector` type
for mixed-dimension storage and uses exact cosine search without HNSW or
IVFFlat. This was checked against the [official pgvector storage and dimension
documentation](https://github.com/pgvector/pgvector#storing).

# Migration

Number: `000010_candidate_semantic_contract` in
`internal/runtime/migrations/` (the next actual embedded migration after
`000009`). Historical migrations were not edited.

Existing 1536 rows: preserved in place. The vector typmod is widened, existing
rows receive `embedding_provider=legacy` and `embedding_space_id=legacy`, and
their vector values are not regenerated.

Down behavior: explicitly checks for any non-1536 vector/metadata and raises a
PostgreSQL exception before changing the schema. It never deletes, truncates
or coerces non-1536 vectors. With only 1536 rows it restores `vector(1536)` and
the historical dimension check.

# Dimension validation

Adapter: OpenAI-compatible response data must contain exactly the configured
number of elements. Empty vectors, wrong dimensions, duplicate/missing
indexes, malformed JSON, HTTP errors and non-finite values fail safely;
dimension mismatches use `semantic.DimensionMismatchError`.

Domain/usecase: `semantic.ValidateVector` is used by indexing and retrieval.
The indexer validates the full batch before any upsert and stores the exact
active contract on each document.

Repository: PostgreSQL and memory upsert paths reject empty/mismatched vectors,
missing contract identity and invalid values. Search validates the query
locally before issuing SQL, so a wrong-dimension query produces no database
query. No vector is padded or truncated.

# Embedding-space compatibility

Search carries provider, model, dimensions, space ID and vector together.
PostgreSQL SQL filters by candidate, provider, model, dimensions and space ID;
the vector is parameterized. The optional repository space inspector detects
legacy, stale or mixed rows. Semantic usecase retrieval returns the typed
`ErrSemanticIndexIncompatible` / reindex-required condition instead of
presenting a partial mixed index as complete.

# Mixed-dimension behavior

The repository regression stores a 1536-dimensional document and a
1024-dimensional document for one candidate. A 1024 active-space query
participates only in the 1024 rows and never compares against the 1536 vector.
At the usecase boundary, the presence of any incompatible row is reported as
`REINDEX_REQUIRED` so an in-progress mixed index is not treated as complete.

# Reindex policy

`candidate semantic reindex --apply` is still the explicit operator action.
It supports both 1536 and 1024 configured dimensions with fake providers in
tests. A provider/model/endpoint/dimension change marks same-key rows changed;
legacy or old-space rows require reindex. During a non-atomic reindex, the
space inspector reports incompatibility until stale old-space rows are gone.

Startup never performs a full automatic reindex and does not regenerate old
vectors merely because the schema migration is applied.

# Candidate mutation / semantic projection

The PostgreSQL canonical Candidate transaction commits first. The existing
post-commit semantic projection then uses the active embedding contract. A
provider error or wrong dimension records a semantic-index warning while the
canonical Candidate mutation remains committed and safe. Semantic projection
cannot create or confirm Candidate facts.

# Semantic retrieval

Ranking, cosine semantics, top-k defaults, truth weighting and prompt assembly
are unchanged. Retrieval validates current canonical Candidate documents after
the space-scoped query. Existing optional semantic fallback behavior remains:
when semantic retrieval is unavailable, callers retain their structured
Candidate Knowledge context; failure is not converted into an empty candidate
profile and cannot become Candidate truth.

# JSON mode

`STORAGE_BACKEND=json` remains the JSON canonical path. It does not open
PostgreSQL and does not require embedding credentials when semantic search is
disabled. No JSON Candidate truth or application/chat behavior was changed.

# PostgreSQL mode

Semantic disabled:

`STORAGE_BACKEND=postgres` with `EMBEDDING_PROVIDER` unset remains valid for
PostgreSQL Candidate/career persistence. Semantic retriever/indexer composition
is absent and no embedding credential is required.

Semantic enabled:

An explicit provider creates the embedding adapter from the effective endpoint,
key, model and dimensions. Invalid provider/dimension/endpoint configuration
fails clearly; it is not silently disabled. Persisted rows from another space
produce an explicit reindex-required condition.

# Mistral compatibility

Mistral chat + separate embedding provider: SUPPORTED.

1024-dimensional embedding contract: SUPPORTED by configuration, adapter,
indexing, retrieval validation, memory tests and provider fixtures. A live
Mistral embedding call was not required or performed.

# CLI / health

The existing semantic CLI was not redesigned. `candidate semantic status`
shows non-secret provider, model, dimensions, space ID and index state. It
reports `DISABLED`, `READY` or `REINDEX_REQUIRED`; provider/store failures are
returned as explicit command errors. Keys are never displayed.

# S1 regression

S1 backend-selection tests PASS. PostgreSQL career repositories remain the
source of truth in PostgreSQL mode, JSON remains the source of truth in JSON
mode, and there is no PostgreSQL-to-JSON silent fallback.

# R14 regression

R14 write gateway, attempt state, auto-chat state, reconciliation,
notifications and write retry behavior were not changed. All existing R14
tests PASS. No HH write path was invoked.

# Tests

Added/updated coverage includes configuration precedence/fallback, contract
space identity and endpoint sanitization, 1024/1536 provider responses,
typed dimension mismatch, malformed/empty/error response handling, independent
chat/embedding endpoint-key routing, mixed memory spaces, semantic index
reindexing, migration structure and existing PostgreSQL integration fixtures.

# Verification

tests: PASS (`go test -count=1 ./...`)

race: PASS (`go test -race ./...`)

vet: PASS (`go vet ./...`)

build: PASS (`go build ./...`)

canonical: PASS (`go build ./cmd/hh-ai-responder`)

diff: PASS (`git diff --check`)

node: PASS (`node --check web/app.js`)

Docker: NOT APPLICABLE

PostgreSQL live integration: NOT RUN (`POSTGRES_TEST_DATABASE_URL` is not
configured); static migration tests and skip-safe integration fixtures PASS.

LIVE HH WRITES: 0

# Ready

S3 — JSON → PostgreSQL Production Data Migration / Semantic Reindex READY

Do not start S3 as part of this change.
