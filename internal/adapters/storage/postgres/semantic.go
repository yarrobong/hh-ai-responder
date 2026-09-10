package postgresstorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
)

// SemanticRepository owns only the PostgreSQL projection of Candidate
// semantic documents. Candidate truth, document selection, embedding calls,
// and post-commit indexing policy remain outside this adapter.
type SemanticRepository struct {
	db postgresDBTX
}

var _ ports.CandidateSemanticRepository = (*SemanticRepository)(nil)

func NewSemanticRepository(pool *pgxpool.Pool) *SemanticRepository {
	if pool == nil {
		return &SemanticRepository{}
	}
	return &SemanticRepository{db: pool}
}

func NewSemanticRepositoryForTx(tx pgx.Tx) *SemanticRepository {
	return &SemanticRepository{db: tx}
}

func (r *SemanticRepository) requireDB() error {
	if r == nil || r.db == nil {
		return errors.New("postgres semantic repository is not configured")
	}
	return nil
}

func (r *SemanticRepository) UpsertDocument(ctx context.Context, document semantic.CandidateSemanticDocument) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	if err := validateSemanticDocument(document); err != nil {
		return err
	}
	if strings.TrimSpace(document.EmbeddingProvider) == "" || strings.TrimSpace(document.EmbeddingSpaceID) == "" {
		return errors.New("semantic document embedding provider and space are required")
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
		(candidate_id,entity_type,entity_id,content,content_hash,embedding,embedding_provider,embedding_model,embedding_dimensions,embedding_space_id,truth_snapshot,indexed_at,indexed_at_ns,source_updated_at_ns,metadata)
		VALUES($1,$2,$3,$4,$5,$6::vector,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT(candidate_id,entity_type,entity_id) DO UPDATE SET
		content=EXCLUDED.content, content_hash=EXCLUDED.content_hash, embedding=EXCLUDED.embedding,
		embedding_provider=EXCLUDED.embedding_provider, embedding_model=EXCLUDED.embedding_model, embedding_dimensions=EXCLUDED.embedding_dimensions,
		embedding_space_id=EXCLUDED.embedding_space_id,
		truth_snapshot=EXCLUDED.truth_snapshot, indexed_at=EXCLUDED.indexed_at, indexed_at_ns=EXCLUDED.indexed_at_ns,
		source_updated_at_ns=EXCLUDED.source_updated_at_ns, metadata=EXCLUDED.metadata
		WHERE candidate_semantic_documents.content_hash IS DISTINCT FROM EXCLUDED.content_hash
		   OR candidate_semantic_documents.embedding_provider IS DISTINCT FROM EXCLUDED.embedding_provider
		   OR candidate_semantic_documents.embedding_model IS DISTINCT FROM EXCLUDED.embedding_model
		   OR candidate_semantic_documents.embedding_dimensions IS DISTINCT FROM EXCLUDED.embedding_dimensions
		   OR candidate_semantic_documents.embedding_space_id IS DISTINCT FROM EXCLUDED.embedding_space_id`,
		document.CandidateID, document.EntityType, document.EntityID, document.Content, document.ContentHash, vector,
		document.EmbeddingProvider, document.EmbeddingModel, document.EmbeddingDimensions, document.EmbeddingSpaceID, truth, indexedAt, indexedAt.UnixNano(), document.SourceUpdatedAt.UnixNano(), metadata)
	if err != nil {
		return fmt.Errorf("upsert semantic document %s/%s: %w", document.EntityType, document.EntityID, err)
	}
	return nil
}

func (r *SemanticRepository) DeleteDocument(ctx context.Context, candidateID string, entityType semantic.CandidateSemanticEntityType, entityID string) error {
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

func (r *SemanticRepository) Search(ctx context.Context, query semantic.CandidateSemanticSearchQuery) ([]semantic.CandidateSemanticResult, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query.CandidateID) == "" {
		return nil, errors.New("semantic search candidate id is required")
	}
	if len(query.Embedding) == 0 {
		return nil, errors.New("semantic search embedding is required")
	}
	if strings.TrimSpace(query.EmbeddingProvider) == "" || strings.TrimSpace(query.EmbeddingModel) == "" || strings.TrimSpace(query.EmbeddingSpaceID) == "" || query.EmbeddingDimensions <= 0 {
		return nil, errors.New("semantic search requires an explicit embedding provider, model, space and dimensions")
	}
	if err := semantic.ValidateVector(query.Embedding, query.EmbeddingDimensions, "semantic search query"); err != nil {
		return nil, err
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
	args := []interface{}{query.CandidateID, formatSemanticVector(query.Embedding), query.EmbeddingSpaceID, query.EmbeddingDimensions, query.EmbeddingProvider, query.EmbeddingModel}
	where := []string{"candidate_id=$1", "embedding_space_id=$3", "embedding_dimensions=$4", "embedding_provider=$5", "embedding_model=$6"}
	if len(query.EntityTypes) > 0 {
		values := make([]string, 0, len(query.EntityTypes))
		for _, entityType := range query.EntityTypes {
			values = append(values, string(entityType))
		}
		where = append(where, "entity_type = ANY($7::text[])")
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
	rows, err := r.db.Query(ctx, `SELECT entity_type,entity_id,`+scoreExpr+` AS score,content,content_hash,embedding_provider,embedding_model,embedding_dimensions,embedding_space_id,truth_snapshot,metadata FROM candidate_semantic_documents WHERE `+strings.Join(where, " AND ")+` ORDER BY score DESC, entity_type, entity_id LIMIT $`+strconv.Itoa(limitArg), args...)
	if err != nil {
		return nil, fmt.Errorf("search semantic documents: %w", err)
	}
	defer rows.Close()
	result := []semantic.CandidateSemanticResult{}
	for rows.Next() {
		var entityType string
		var item semantic.CandidateSemanticResult
		var truth, metadata []byte
		if err := rows.Scan(&entityType, &item.EntityID, &item.Score, &item.Excerpt, &item.ContentHash, &item.Provider, &item.Model, &item.Dimensions, &item.SpaceID, &truth, &metadata); err != nil {
			return nil, err
		}
		item.EntityType = semantic.CandidateSemanticEntityType(entityType)
		item.Excerpt = shortSemanticExcerpt(item.Excerpt, 280)
		var snapshot semantic.CandidateSemanticTruthSnapshot
		if err := decodeNullableJSON(truth, &snapshot); err != nil {
			return nil, err
		}
		item.EvidenceRefs = append([]semantic.CandidateSemanticEvidenceRef{}, snapshot.References...)
		var meta map[string]interface{}
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

func (r *SemanticRepository) EmbeddingSpaceMatches(ctx context.Context, candidateID string, contract semantic.EmbeddingContract) error {
	if err := r.requireDB(); err != nil {
		return err
	}
	var incompatible bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM candidate_semantic_documents
		WHERE candidate_id=$1
		  AND (embedding_provider IS DISTINCT FROM $2 OR embedding_model IS DISTINCT FROM $3 OR embedding_dimensions IS DISTINCT FROM $4 OR embedding_space_id IS DISTINCT FROM $5)
	)`, candidateID, contract.Provider, contract.Model, contract.Dimensions, contract.SpaceID).Scan(&incompatible); err != nil {
		return fmt.Errorf("check semantic embedding space: %w", err)
	}
	if incompatible {
		return &semantic.SemanticIndexIncompatibilityError{CandidateID: candidateID, ActiveSpace: contract.SpaceID}
	}
	return nil
}

func (r *SemanticRepository) ListDocuments(ctx context.Context, candidateID string) ([]semantic.CandidateSemanticDocument, error) {
	if err := r.requireDB(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT id,entity_type,entity_id,content,content_hash,embedding,embedding_provider,embedding_model,embedding_dimensions,embedding_space_id,truth_snapshot,indexed_at,indexed_at_ns,source_updated_at_ns,metadata FROM candidate_semantic_documents WHERE candidate_id=$1 ORDER BY entity_type,entity_id`, candidateID)
	if err != nil {
		return nil, fmt.Errorf("list semantic documents: %w", err)
	}
	defer rows.Close()
	result := []semantic.CandidateSemanticDocument{}
	for rows.Next() {
		var item semantic.CandidateSemanticDocument
		var entityType string
		var vector semanticVector
		var indexedAt pgtype.Timestamptz
		var indexedAtNS, sourceUpdatedNS int64
		var truth, metadata []byte
		if err := rows.Scan(&item.ID, &entityType, &item.EntityID, &item.Content, &item.ContentHash, &vector, &item.EmbeddingProvider, &item.EmbeddingModel, &item.EmbeddingDimensions, &item.EmbeddingSpaceID, &truth, &indexedAt, &indexedAtNS, &sourceUpdatedNS, &metadata); err != nil {
			return nil, err
		}
		item.CandidateID, item.EntityType = candidateID, semantic.CandidateSemanticEntityType(entityType)
		item.Embedding = append([]float32{}, vector.Values...)
		if err := semantic.ValidateVector(item.Embedding, item.EmbeddingDimensions, "stored semantic vector"); err != nil {
			return nil, err
		}
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

func validateSemanticDocument(document semantic.CandidateSemanticDocument) error {
	if strings.TrimSpace(document.CandidateID) == "" || strings.TrimSpace(document.EntityID) == "" {
		return errors.New("semantic document requires candidate and entity ids")
	}
	if document.EntityType != semantic.CandidateSemanticEntityStory && document.EntityType != semantic.CandidateSemanticEntityProject && document.EntityType != semantic.CandidateSemanticEntityAchievement {
		return fmt.Errorf("unsupported semantic entity type %q", document.EntityType)
	}
	if strings.TrimSpace(document.Content) == "" || strings.TrimSpace(document.ContentHash) == "" || strings.TrimSpace(document.EmbeddingModel) == "" {
		return errors.New("semantic document content, hash and model are required")
	}
	if err := semantic.ValidateVector(document.Embedding, document.EmbeddingDimensions, "semantic document"); err != nil {
		return err
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

type semanticVector struct {
	Values []float32
}

func (v *semanticVector) Scan(value interface{}) error {
	if value == nil {
		v.Values = nil
		return nil
	}
	var raw string
	switch value := value.(type) {
	case string:
		raw = value
	case []byte:
		raw = string(value)
	default:
		return fmt.Errorf("scan semantic vector from %T", value)
	}
	parsed, err := parseSemanticVector(raw)
	if err != nil {
		return err
	}
	v.Values = parsed
	return nil
}

func parseSemanticVector(raw string) ([]float32, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, errors.New("invalid pgvector text")
	}
	raw = strings.TrimSpace(raw[1 : len(raw)-1])
	if raw == "" {
		return []float32{}, nil
	}
	parts := strings.Split(raw, ",")
	values := make([]float32, len(parts))
	for i, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid pgvector value at index %d", i)
		}
		values[i] = float32(value)
	}
	return values, nil
}

func shortSemanticExcerpt(content string, max int) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if len([]rune(content)) <= max {
		return content
	}
	return string([]rune(content)[:max]) + "…"
}
