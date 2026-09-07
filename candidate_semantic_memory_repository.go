package main

import (
	"context"
	"errors"
	"math"
	"sort"
)

type MemoryCandidateSemanticRepository struct {
	documents map[string]CandidateSemanticDocument
}

func NewMemoryCandidateSemanticRepository() *MemoryCandidateSemanticRepository {
	return &MemoryCandidateSemanticRepository{documents: map[string]CandidateSemanticDocument{}}
}

func (r *MemoryCandidateSemanticRepository) UpsertDocument(_ context.Context, document CandidateSemanticDocument) error {
	if r == nil {
		return errors.New("memory semantic repository is not configured")
	}
	if err := validateMemorySemanticDocument(document); err != nil {
		return err
	}
	if r.documents == nil {
		r.documents = map[string]CandidateSemanticDocument{}
	}
	r.documents[semanticDocumentKey(document.CandidateID, document.EntityType, document.EntityID)] = document
	return nil
}
func (r *MemoryCandidateSemanticRepository) DeleteDocument(_ context.Context, candidateID string, entityType CandidateSemanticEntityType, entityID string) error {
	if r != nil {
		delete(r.documents, semanticDocumentKey(candidateID, entityType, entityID))
	}
	return nil
}
func (r *MemoryCandidateSemanticRepository) ListDocuments(_ context.Context, candidateID string) ([]CandidateSemanticDocument, error) {
	result := []CandidateSemanticDocument{}
	if r == nil {
		return result, nil
	}
	for _, doc := range r.documents {
		if doc.CandidateID == candidateID {
			result = append(result, doc)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return semanticDocumentKey(result[i].CandidateID, result[i].EntityType, result[i].EntityID) < semanticDocumentKey(result[j].CandidateID, result[j].EntityType, result[j].EntityID)
	})
	return result, nil
}
func (r *MemoryCandidateSemanticRepository) Search(_ context.Context, query CandidateSemanticSearchQuery) ([]CandidateSemanticResult, error) {
	if r == nil {
		return nil, errors.New("memory semantic repository is not configured")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	type scored struct{ result CandidateSemanticResult }
	items := []scored{}
	for _, doc := range r.documents {
		if doc.CandidateID != query.CandidateID || len(doc.Embedding) != len(query.Embedding) {
			continue
		}
		if len(query.EntityTypes) > 0 && !semanticTypeAllowed(doc.EntityType, query.EntityTypes) {
			continue
		}
		score := cosineSimilarity(query.Embedding, doc.Embedding)
		if query.MinScore != nil && score < *query.MinScore {
			continue
		}
		items = append(items, scored{CandidateSemanticResult{EntityType: doc.EntityType, EntityID: doc.EntityID, Score: score, Title: semanticTitle(doc), Excerpt: shortSemanticExcerpt(doc.Content, 280), EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, doc.TruthSnapshot.References...), ContentHash: doc.ContentHash, Model: doc.EmbeddingModel}})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].result.Score != items[j].result.Score {
			return items[i].result.Score > items[j].result.Score
		}
		return semanticDocumentKey(query.CandidateID, items[i].result.EntityType, items[i].result.EntityID) < semanticDocumentKey(query.CandidateID, items[j].result.EntityType, items[j].result.EntityID)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]CandidateSemanticResult, len(items))
	for i := range items {
		result[i] = items[i].result
	}
	return result, nil
}
func semanticDocumentKey(candidateID string, entityType CandidateSemanticEntityType, entityID string) string {
	return candidateID + "\x00" + string(entityType) + "\x00" + entityID
}
func semanticTypeAllowed(value CandidateSemanticEntityType, allowed []CandidateSemanticEntityType) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
func semanticTitle(doc CandidateSemanticDocument) string {
	if value, ok := doc.Metadata["title"].(string); ok {
		return value
	}
	return ""
}
func cosineSimilarity(left, right []float32) float64 {
	var dot, leftNorm, rightNorm float64
	for i := range left {
		dot += float64(left[i]) * float64(right[i])
		leftNorm += float64(left[i]) * float64(left[i])
		rightNorm += float64(right[i]) * float64(right[i])
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	score := dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
func validateMemorySemanticDocument(document CandidateSemanticDocument) error {
	if document.EmbeddingDimensions != len(document.Embedding) || document.EmbeddingDimensions <= 0 {
		return errors.New("invalid memory semantic dimensions")
	}
	if document.CandidateID == "" || document.EntityID == "" || document.ContentHash == "" {
		return errors.New("invalid memory semantic identity")
	}
	return nil
}
