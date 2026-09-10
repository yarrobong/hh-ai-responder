package candidatesemanticsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
	candidatesemanticindex "hh-ai-responder/internal/usecase/candidatesemanticindex"
)

// ErrSemanticUnavailable is the compatibility error for failures in the
// optional semantic retrieval path. It is shared with semantic indexing so
// existing callers can continue to use errors.Is.
var ErrSemanticUnavailable = candidatesemanticindex.ErrSemanticUnavailable

// Query is the plain-text semantic search input. Query text is passed to the
// embedding provider unchanged; callers own any domain-specific query
// construction.
type Query struct {
	CandidateID string
	Query       string
	EntityTypes []semantic.CandidateSemanticEntityType
	Limit       int
	MinScore    *float64
}

// Purpose identifies the higher-level read flow that requested retrieval. It
// does not change truth or answerability policy.
type Purpose string

const (
	PurposeEmployerReply Purpose = "employer_reply"
	PurposeCoverLetter   Purpose = "cover_letter"
	PurposeVacancy       Purpose = "vacancy"
)

// RetrievalRequest is the context-facing retrieval input. Purpose is
// descriptive only and is not used to alter search ranking or truth policy.
type RetrievalRequest struct {
	CandidateID string
	Query       string
	EntityTypes []semantic.CandidateSemanticEntityType
	Limit       int
	MinScore    *float64
	Purpose     Purpose
}

// Retriever is the narrow read-only capability consumed by higher-level
// context builders.
type Retriever interface {
	Retrieve(context.Context, RetrievalRequest) ([]semantic.CandidateSemanticResult, error)
}

// DefaultMinScore is the existing caller-facing default for context retrieval.
const DefaultMinScore = 0.55

// Service owns semantic query embedding, scoped vector search, and freshness
// validation against the current canonical Candidate.
type Service struct {
	Repository ports.CandidateSemanticRepository
	Embeddings ports.EmbeddingProvider
	Candidates ports.CandidateReader
}

func NewService(repository ports.CandidateSemanticRepository, embeddings ports.EmbeddingProvider, candidates ports.CandidateReader) *Service {
	return &Service{Repository: repository, Embeddings: embeddings, Candidates: candidates}
}

// Search embeds one exact query, searches only the requested Candidate scope,
// then validates each result against the current canonical Candidate and the
// shared deterministic semantic document builder. Repository ordering is
// preserved for remaining valid results.
func (s *Service) Search(ctx context.Context, query Query) ([]semantic.CandidateSemanticResult, error) {
	if s == nil || s.Repository == nil || s.Embeddings == nil || s.Candidates == nil {
		return nil, fmt.Errorf("%w: semantic search is not fully configured", ErrSemanticUnavailable)
	}
	if strings.TrimSpace(query.CandidateID) == "" || strings.TrimSpace(query.Query) == "" {
		return nil, errors.New("semantic search candidate and query are required")
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}
	vectors, err := s.Embeddings.Embed(ctx, []string{query.Query})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSemanticUnavailable, err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("%w: query embedding missing", ErrSemanticUnavailable)
	}
	contract, err := ports.EmbeddingContractFor(s.Embeddings)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSemanticUnavailable, err)
	}
	if err := semantic.ValidateVector(vectors[0], contract.Dimensions, "query embedding"); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSemanticUnavailable, err)
	}
	if inspector, ok := s.Repository.(ports.CandidateSemanticSpaceInspector); ok {
		if err := inspector.EmbeddingSpaceMatches(ctx, query.CandidateID, contract); err != nil {
			return nil, err
		}
	}
	results, err := s.Repository.Search(ctx, semantic.CandidateSemanticSearchQuery{
		CandidateID:         query.CandidateID,
		Embedding:           vectors[0],
		EmbeddingProvider:   contract.Provider,
		EmbeddingModel:      contract.Model,
		EmbeddingDimensions: contract.Dimensions,
		EmbeddingSpaceID:    contract.SpaceID,
		EntityTypes:         append([]semantic.CandidateSemanticEntityType{}, query.EntityTypes...),
		Limit:               limit,
		MinScore:            query.MinScore,
	})
	if err != nil {
		return nil, err
	}
	candidateSnapshot, err := s.Candidates.CurrentCandidate(ctx)
	if err != nil {
		return nil, err
	}
	if candidateSnapshot.ID != query.CandidateID {
		return nil, fmt.Errorf("semantic search candidate scope mismatch: requested %q, got %q", query.CandidateID, candidateSnapshot.ID)
	}
	eligible, err := candidatesemanticindex.BuildCandidateSemanticDocuments(candidateSnapshot)
	if err != nil {
		return nil, err
	}
	allowed := map[semantic.CandidateSemanticEntityType]map[string]candidatesemanticindex.CandidateSemanticDocumentDraft{}
	for _, draft := range eligible {
		if !draft.Eligible {
			continue
		}
		byID := allowed[draft.EntityType]
		if byID == nil {
			byID = map[string]candidatesemanticindex.CandidateSemanticDocumentDraft{}
			allowed[draft.EntityType] = byID
		}
		byID[draft.EntityID] = draft
	}
	filtered := make([]semantic.CandidateSemanticResult, 0, len(results))
	for _, result := range results {
		byID := allowed[result.EntityType]
		draft, ok := byID[result.EntityID]
		if !ok || result.ContentHash != draft.ContentHash || result.Model != contract.Model || (result.SpaceID != "" && result.SpaceID != contract.SpaceID) || (result.Dimensions != 0 && result.Dimensions != contract.Dimensions) {
			continue
		}
		result.EvidenceRefs = append([]semantic.CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)
		filtered = append(filtered, result)
	}
	return filtered, nil
}

// Retrieve adapts the context-facing request to the core search input. It
// supplies the existing minimum score only when the caller did not specify
// one; it does not perform query rewriting or intent classification.
func (s *Service) Retrieve(ctx context.Context, request RetrievalRequest) ([]semantic.CandidateSemanticResult, error) {
	if s == nil {
		return nil, ErrSemanticUnavailable
	}
	if strings.TrimSpace(request.Query) == "" {
		return nil, errors.New("semantic retrieval query is required")
	}
	minScore := request.MinScore
	if minScore == nil {
		defaultScore := DefaultMinScore
		minScore = &defaultScore
	}
	return s.Search(ctx, Query{
		CandidateID: request.CandidateID,
		Query:       request.Query,
		EntityTypes: append([]semantic.CandidateSemanticEntityType{}, request.EntityTypes...),
		Limit:       request.Limit,
		MinScore:    minScore,
	})
}

var _ Retriever = (*Service)(nil)
