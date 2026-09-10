-- Semantic documents are a private, rebuildable projection of canonical
-- candidate knowledge. They are never the source of truth.
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS candidate_semantic_documents (
    id BIGSERIAL PRIMARY KEY,
    candidate_id TEXT NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    embedding vector(1536) NOT NULL,
    embedding_model TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL,
    truth_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    indexed_at TIMESTAMPTZ NOT NULL,
    indexed_at_ns BIGINT NOT NULL,
    source_updated_at_ns BIGINT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT candidate_semantic_entity_type_check CHECK (entity_type IN ('story', 'project', 'achievement')),
    CONSTRAINT candidate_semantic_hash_check CHECK (content_hash <> ''),
    CONSTRAINT candidate_semantic_model_check CHECK (embedding_model <> ''),
    CONSTRAINT candidate_semantic_dimensions_check CHECK (embedding_dimensions = 1536),
    CONSTRAINT candidate_semantic_truth_snapshot_object_check CHECK (jsonb_typeof(truth_snapshot) = 'object'),
    CONSTRAINT candidate_semantic_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT candidate_semantic_identity_unique UNIQUE (candidate_id, entity_type, entity_id)
);

CREATE INDEX IF NOT EXISTS candidate_semantic_documents_candidate_type_idx
    ON candidate_semantic_documents(candidate_id, entity_type, entity_id);

-- The knowledge base is currently small, so exact cosine search is preferred
-- over an approximate index. Add HNSW/IVFFlat only after measuring workload.
