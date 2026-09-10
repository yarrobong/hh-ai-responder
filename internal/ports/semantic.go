package ports

import (
	"context"
	"errors"

	"hh-ai-responder/internal/semantic"
)

// EmbeddingProvider generates vectors for an ordered batch of texts. Model
// and dimensions are part of the provider contract because they participate
// in semantic index freshness decisions.
type EmbeddingProvider interface {
	Embed(context.Context, []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

// EmbeddingContractProvider is optional for compatibility with small test and
// local providers. Production providers should expose the full non-secret
// contract so semantic writes and reads use the same space identity.
type EmbeddingContractProvider interface {
	EmbeddingContract() semantic.EmbeddingContract
}

func EmbeddingContractFor(provider EmbeddingProvider) (semantic.EmbeddingContract, error) {
	if provider == nil {
		return semantic.EmbeddingContract{}, errors.New("embedding provider is required")
	}
	if contracted, ok := provider.(EmbeddingContractProvider); ok {
		contract := contracted.EmbeddingContract()
		if contract.SpaceID == "" || contract.Provider == "" || contract.Model == "" || contract.Dimensions <= 0 || contract.Model != provider.Model() || contract.Dimensions != provider.Dimensions() {
			return semantic.EmbeddingContract{}, errors.New("embedding provider returned an invalid contract")
		}
		return contract, nil
	}
	// Legacy in-process providers have no endpoint/provider metadata. They are
	// isolated in a deterministic legacy space instead of being confused with
	// an explicitly configured network provider.
	return semantic.NewEmbeddingContract("legacy", provider.Model(), provider.Dimensions(), "")
}

// CandidateSemanticSpaceInspector lets a read use case reject a partially
// reindexed candidate while repositories still filter every SQL/vector query
// to the requested active space.
type CandidateSemanticSpaceInspector interface {
	EmbeddingSpaceMatches(context.Context, string, semantic.EmbeddingContract) error
}

// CandidateSemanticRepository is the persistence capability used by the
// existing semantic indexing and retrieval orchestration. It stores supplied
// documents and performs scoped vector retrieval; document selection,
// truth-safety filtering, embedding generation, and stale-index policy remain
// outside the port.
type CandidateSemanticRepository interface {
	UpsertDocument(context.Context, semantic.CandidateSemanticDocument) error
	DeleteDocument(context.Context, string, semantic.CandidateSemanticEntityType, string) error
	Search(context.Context, semantic.CandidateSemanticSearchQuery) ([]semantic.CandidateSemanticResult, error)
	ListDocuments(context.Context, string) ([]semantic.CandidateSemanticDocument, error)
}
