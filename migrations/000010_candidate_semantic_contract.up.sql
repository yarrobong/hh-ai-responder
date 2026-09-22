-- S2: make semantic storage dimension-flexible while preserving every
-- existing vector. Legacy rows are intentionally marked outside any newly
-- configured embedding space and therefore require an explicit reindex.
ALTER TABLE candidate_semantic_documents
    ALTER COLUMN embedding TYPE vector USING embedding::vector;

ALTER TABLE candidate_semantic_documents
    DROP CONSTRAINT IF EXISTS candidate_semantic_dimensions_check;

ALTER TABLE candidate_semantic_documents
    ADD COLUMN IF NOT EXISTS embedding_provider TEXT,
    ADD COLUMN IF NOT EXISTS embedding_space_id TEXT;

UPDATE candidate_semantic_documents
SET embedding_provider = COALESCE(NULLIF(embedding_provider, ''), 'legacy'),
    embedding_space_id = COALESCE(NULLIF(embedding_space_id, ''), 'legacy')
WHERE embedding_provider IS NULL OR embedding_provider = '' OR embedding_space_id IS NULL OR embedding_space_id = '';

ALTER TABLE candidate_semantic_documents
    ALTER COLUMN embedding_provider SET NOT NULL,
    ALTER COLUMN embedding_space_id SET NOT NULL,
    ALTER COLUMN embedding_provider DROP DEFAULT,
    ALTER COLUMN embedding_space_id DROP DEFAULT;

ALTER TABLE candidate_semantic_documents
    ADD CONSTRAINT candidate_semantic_dimensions_check
        CHECK (embedding_dimensions > 0 AND embedding_dimensions <= 16000),
    ADD CONSTRAINT candidate_semantic_vector_dimensions_check
        CHECK (embedding_dimensions = vector_dims(embedding)),
    ADD CONSTRAINT candidate_semantic_provider_check
        CHECK (embedding_provider <> ''),
    ADD CONSTRAINT candidate_semantic_space_check
        CHECK (embedding_space_id <> '');
