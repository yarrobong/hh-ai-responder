-- Refuse rollback if any vector cannot be represented by the historical
-- vector(1536) contract. The migration transaction then rolls back safely.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM candidate_semantic_documents
        WHERE embedding_dimensions <> 1536 OR vector_dims(embedding) <> 1536
    ) THEN
        RAISE EXCEPTION 'cannot rollback candidate semantic contract while non-1536 vectors exist';
    END IF;
END $$;

ALTER TABLE candidate_semantic_documents
    DROP CONSTRAINT IF EXISTS candidate_semantic_vector_dimensions_check,
    DROP CONSTRAINT IF EXISTS candidate_semantic_dimensions_check,
    DROP CONSTRAINT IF EXISTS candidate_semantic_provider_check,
    DROP CONSTRAINT IF EXISTS candidate_semantic_space_check,
    DROP COLUMN IF EXISTS embedding_provider,
    DROP COLUMN IF EXISTS embedding_space_id;

ALTER TABLE candidate_semantic_documents
    ALTER COLUMN embedding TYPE vector(1536) USING embedding::vector(1536),
    ADD CONSTRAINT candidate_semantic_dimensions_check CHECK (embedding_dimensions = 1536);
