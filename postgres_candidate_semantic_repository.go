package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresCandidateSemanticRepository struct {
	db postgresDBTX
}

func NewPostgresCandidateSemanticRepository(pool *pgxpool.Pool) *PostgresCandidateSemanticRepository {
	if pool == nil {
		return &PostgresCandidateSemanticRepository{}
	}
	return &PostgresCandidateSemanticRepository{db: pool}
}

func newPostgresCandidateSemanticRepositoryTx(db postgresDBTX) *PostgresCandidateSemanticRepository {
	return &PostgresCandidateSemanticRepository{db: db}
}

func (r *PostgresCandidateSemanticRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres semantic repository is not configured")
	}
	return nil
}

func (r *PostgresCandidateSemanticRepository) UpsertDocument(ctx context.Context, document CandidateSemanticDocument) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := validateSemanticDocument(document); err != nil {
		return err
	}
	truth, err := json.Marshal(document.TruthSnapshot)
	if err != nil {
		return fmt.Errorf("encode semantic truth snapshot: %w", err)
	}
	metadata, err := json.Marshal(document.Metadata)
	if err != nil {
		return fmt.Errorf("encode semantic metadata: %w", err)
	}
	indexedAt := document.IndexedAt.UTC()
	if indexedAt.IsZero() {
		indexedAt = time.Now().UTC()
	}
	vector := formatSemanticVector(document.Embedding)
	_, err = r.db.Exec(ctx, `
		INSERT INTO candidate_semantic_documents
		(candidate_id,entity_type,entity_id,content,content_hash,embedding,embedding_model,embedding_dimensions,truth_snapshot,indexed_at,indexed_at_ns,source_updated_at_ns,metadata)
		VALUES($1,$2,$3,$4,$5,$6::vector,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT(candidate_id,entity_type,entity_id) DO UPDATE SET
		content=EXCLUDED.content, content_hash=EXCLUDED.content_hash, embedding=EXCLUDED.embedding,
		embedding_model=EXCLUDED.embedding_model, embedding_dimensions=EXCLUDED.embedding_dimensions,
		truth_snapshot=EXCLUDED.truth_snapshot, indexed_at=EXCLUDED.indexed_at, indexed_at_ns=EXCLUDED.indexed_at_ns,
		source_updated_at_ns=EXCLUDED.source_updated_at_ns, metadata=EXCLUDED.metadata
		WHERE candidate_semantic_documents.content_hash IS DISTINCT FROM EXCLUDED.content_hash
		   OR candidate_semantic_documents.embedding_model IS DISTINCT FROM EXCLUDED.embedding_model
		   OR candidate_semantic_documents.embedding_dimensions IS DISTINCT FROM EXCLUDED.embedding_dimensions`,
		document.CandidateID, document.EntityType, document.EntityID, document.Content, document.ContentHash, vector,
		document.EmbeddingModel, document.EmbeddingDimensions, truth, indexedAt, indexedAt.UnixNano(), document.SourceUpdatedAt.UnixNano(), metadata)
	if err != nil {
		return fmt.Errorf("upsert semantic document %s/%s: %w", document.EntityType, document.EntityID, err)
	}
	return nil
}

func (r *PostgresCandidateSemanticRepository) DeleteDocument(ctx context.Context, candidateID string, entityType CandidateSemanticEntityType, entityID string) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if strings.TrimSpace(candidateID) == "" || strings.TrimSpace(entityID) == "" {
		return errors.New("semantic document identity is required")
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM candidate_semantic_documents WHERE candidate_id=$1 AND entity_type=$2 AND entity_id=$3`, candidateID, entityType, entityID); err != nil {
		return fmt.Errorf("delete semantic document %s/%s: %w", entityType, entityID, err)
	}
	return nil
}

func (r *PostgresCandidateSemanticRepository) Search(ctx context.Context, query CandidateSemanticSearchQuery) ([]CandidateSemanticResult, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query.CandidateID) == "" {
		return nil, errors.New("semantic search candidate id is required")
	}
	if len(query.Embedding) == 0 {
		return nil, errors.New("semantic search embedding is required")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	if query.MinScore != nil && (*query.MinScore < 0 || *query.MinScore > 1 || math.IsNaN(*query.MinScore)) {
		return nil, errors.New("semantic search minimum score must be between 0 and 1")
	}
	args := []any{query.CandidateID, formatSemanticVector(query.Embedding)}
	where := []string{"candidate_id=$1", "embedding_dimensions=$3"}
	args = append(args, CandidateSemanticEmbeddingDimensions)
	if len(query.EntityTypes) > 0 {
		values := make([]string, 0, len(query.EntityTypes))
		for _, entityType := range query.EntityTypes {
			values = append(values, string(entityType))
		}
		where = append(where, "entity_type = ANY($4::text[])")
		args = append(args, values)
	}
	scoreExpr := "1 - (embedding <=> $2::vector)"
	if query.MinScore != nil {
		minScoreArg := len(args) + 1
		where = append(where, scoreExpr+" >= $"+strconv.Itoa(minScoreArg))
		args = append(args, *query.MinScore)
	}
	limitArg := len(args) + 1
	args = append(args, limit)
	rows, err := r.db.Query(ctx, `SELECT entity_type,entity_id,`+scoreExpr+` AS score,content,content_hash,embedding_model,truth_snapshot,metadata FROM candidate_semantic_documents WHERE `+strings.Join(where, " AND ")+` ORDER BY score DESC, entity_type, entity_id LIMIT $`+strconv.Itoa(limitArg), args...)
	if err != nil {
		return nil, fmt.Errorf("search semantic documents: %w", err)
	}
	defer rows.Close()
	result := []CandidateSemanticResult{}
	for rows.Next() {
		var entityType string
		var item CandidateSemanticResult
		var truth, metadata []byte
		if err := rows.Scan(&entityType, &item.EntityID, &item.Score, &item.Excerpt, &item.ContentHash, &item.Model, &truth, &metadata); err != nil {
			return nil, err
		}
		item.EntityType = CandidateSemanticEntityType(entityType)
		item.Excerpt = shortSemanticExcerpt(item.Excerpt, 280)
		var snapshot CandidateSemanticTruthSnapshot
		if err := decodeNullableJSON(truth, &snapshot); err != nil {
			return nil, err
		}
		item.EvidenceRefs = append([]CandidateSemanticEvidenceRef{}, snapshot.References...)
		var meta map[string]any
		if err := decodeNullableJSON(metadata, &meta); err != nil {
			return nil, err
		}
		if title, ok := meta["title"].(string); ok {
			item.Title = title
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *PostgresCandidateSemanticRepository) ListDocuments(ctx context.Context, candidateID string) ([]CandidateSemanticDocument, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id,entity_type,entity_id,content,content_hash,embedding_model,embedding_dimensions,truth_snapshot,indexed_at,indexed_at_ns,source_updated_at_ns,metadata FROM candidate_semantic_documents WHERE candidate_id=$1 ORDER BY entity_type,entity_id`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("list semantic documents: %w", err)
	}
	defer rows.Close()
	result := []CandidateSemanticDocument{}
	for rows.Next() {
		var item CandidateSemanticDocument
		var entityType string
		var indexedAt pgtype.Timestamptz
		var indexedAtNS, sourceUpdatedNS int64
		var truth, metadata []byte
		if err := rows.Scan(&item.ID, &entityType, &item.EntityID, &item.Content, &item.ContentHash, &item.EmbeddingModel, &item.EmbeddingDimensions, &truth, &indexedAt, &indexedAtNS, &sourceUpdatedNS, &metadata); err != nil {
			return nil, err
		}
		item.CandidateID, item.EntityType = candidateID, CandidateSemanticEntityType(entityType)
		item.IndexedAt, item.SourceUpdatedAt = postgresTimeExact(indexedAt, pgtype.Int8{Int64: indexedAtNS, Valid: true}), time.Unix(0, sourceUpdatedNS).UTC()
		if err := decodeNullableJSON(truth, &item.TruthSnapshot); err != nil {
			return nil, err
		}
		if err := decodeNullableJSON(metadata, &item.Metadata); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func validateSemanticDocument(document CandidateSemanticDocument) error {
	if strings.TrimSpace(document.CandidateID) == "" || strings.TrimSpace(document.EntityID) == "" {
		return errors.New("semantic document requires candidate and entity ids")
	}
	if document.EntityType != CandidateSemanticEntityStory && document.EntityType != CandidateSemanticEntityProject && document.EntityType != CandidateSemanticEntityAchievement {
		return fmt.Errorf("unsupported semantic entity type %q", document.EntityType)
	}
	if strings.TrimSpace(document.Content) == "" || strings.TrimSpace(document.ContentHash) == "" || strings.TrimSpace(document.EmbeddingModel) == "" {
		return errors.New("semantic document content, hash and model are required")
	}
	if document.EmbeddingDimensions != CandidateSemanticEmbeddingDimensions || len(document.Embedding) != document.EmbeddingDimensions {
		return fmt.Errorf("semantic embedding dimensions must be %d", CandidateSemanticEmbeddingDimensions)
	}
	return nil
}

func formatSemanticVector(values []float32) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
