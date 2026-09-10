package runtime

import (
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
	candidatesemanticindex "hh-ai-responder/internal/usecase/candidatesemanticindex"
	candidatesemanticsearch "hh-ai-responder/internal/usecase/candidatesemanticsearch"
)

// These aliases preserve the root package compatibility surface while the
// semantic values and indexing policy live in importable packages.
const CandidateSemanticEmbeddingDimensions = semantic.CandidateSemanticEmbeddingDimensions

const (
	CandidateSemanticEntityStory       = semantic.CandidateSemanticEntityStory
	CandidateSemanticEntityProject     = semantic.CandidateSemanticEntityProject
	CandidateSemanticEntityAchievement = semantic.CandidateSemanticEntityAchievement

	CandidateSemanticNew        = candidatesemanticindex.CandidateSemanticNew
	CandidateSemanticChanged    = candidatesemanticindex.CandidateSemanticChanged
	CandidateSemanticUnchanged  = candidatesemanticindex.CandidateSemanticUnchanged
	CandidateSemanticStale      = candidatesemanticindex.CandidateSemanticStale
	CandidateSemanticIneligible = candidatesemanticindex.CandidateSemanticIneligible
)

type CandidateSemanticEntityType = semantic.CandidateSemanticEntityType
type CandidateSemanticEvidenceRef = semantic.CandidateSemanticEvidenceRef
type CandidateSemanticTruthSnapshot = semantic.CandidateSemanticTruthSnapshot
type CandidateSemanticDocument = semantic.CandidateSemanticDocument
type CandidateSemanticResult = semantic.CandidateSemanticResult
type CandidateSemanticSearchQuery = semantic.CandidateSemanticSearchQuery
type EmbeddingContract = semantic.EmbeddingContract
type CandidateSemanticDocumentDraft = candidatesemanticindex.CandidateSemanticDocumentDraft
type CandidateSemanticIndexState = candidatesemanticindex.CandidateSemanticIndexState
type CandidateSemanticIndexItem = candidatesemanticindex.CandidateSemanticIndexItem
type CandidateSemanticIndexReport = candidatesemanticindex.CandidateSemanticIndexReport
type CandidateSemanticQuery = candidatesemanticsearch.Query
type CandidateSemanticRepository = ports.CandidateSemanticRepository
type EmbeddingProvider = ports.EmbeddingProvider

func BuildSemanticDocument(entity any) (CandidateSemanticDocumentDraft, error) {
	switch value := entity.(type) {
	case CanonicalCandidateStory:
		return candidatesemanticindex.BuildStorySemanticDocument(value)
	case *CanonicalCandidateStory:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic story is nil")
		}
		return candidatesemanticindex.BuildStorySemanticDocument(*value)
	case CanonicalCandidateProject:
		return candidatesemanticindex.BuildProjectSemanticDocument(value)
	case *CanonicalCandidateProject:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic project is nil")
		}
		return candidatesemanticindex.BuildProjectSemanticDocument(*value)
	case CandidateAchievement:
		return candidatesemanticindex.BuildAchievementSemanticDocument(value)
	case *CandidateAchievement:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic achievement is nil")
		}
		return candidatesemanticindex.BuildAchievementSemanticDocument(*value)
	default:
		return CandidateSemanticDocumentDraft{}, fmt.Errorf("unsupported semantic entity %T", entity)
	}
}

func BuildCandidateSemanticDocuments(candidate Candidate) ([]CandidateSemanticDocumentDraft, error) {
	return candidatesemanticindex.BuildCandidateSemanticDocuments(candidate)
}

func shortSemanticExcerpt(content string, max int) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if len([]rune(content)) <= max {
		return content
	}
	return string([]rune(content)[:max]) + "…"
}
