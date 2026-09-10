package runtime

import (
	candidatesemanticindex "hh-ai-responder/internal/usecase/candidatesemanticindex"
	candidatesemanticsearch "hh-ai-responder/internal/usecase/candidatesemanticsearch"
)

// ErrSemanticUnavailable remains a root compatibility error for both semantic
// retrieval and indexing.
var ErrSemanticUnavailable = candidatesemanticsearch.ErrSemanticUnavailable

// CandidateSemanticIndexService is a compatibility alias. The implementation
// and all indexing policy are owned by candidatesemanticindex.Service.
type CandidateSemanticIndexService = candidatesemanticindex.Service

func NewCandidateSemanticIndexService(repository CandidateSemanticRepository, embeddings EmbeddingProvider) *CandidateSemanticIndexService {
	return candidatesemanticindex.NewService(repository, embeddings)
}

// CandidateSemanticSearchService is a compatibility alias. The retrieval
// implementation is owned by candidatesemanticsearch.Service.
type CandidateSemanticSearchService = candidatesemanticsearch.Service

func NewCandidateSemanticSearchService(repository CandidateSemanticRepository, embeddings EmbeddingProvider, candidates CandidateRepository) *CandidateSemanticSearchService {
	return candidatesemanticsearch.NewService(repository, embeddings, candidates)
}
