package candidatesemanticsearch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/semantic"
	candidatesemanticindex "hh-ai-responder/internal/usecase/candidatesemanticindex"
)

type searchEmbeddingProvider struct {
	model      string
	dimensions int
	texts      []string
	result     [][]float32
	err        error
}

func (p *searchEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.texts = append([]string{}, texts...)
	if p.err != nil {
		return nil, p.err
	}
	if p.result != nil {
		return p.result, nil
	}
	return [][]float32{make([]float32, p.dimensions)}, nil
}

func (p *searchEmbeddingProvider) Model() string { return p.model }

func (p *searchEmbeddingProvider) Dimensions() int { return p.dimensions }

type searchRepository struct {
	results []semantic.CandidateSemanticResult
	query   semantic.CandidateSemanticSearchQuery
	err     error
}

func (r *searchRepository) UpsertDocument(context.Context, semantic.CandidateSemanticDocument) error {
	return errors.New("unexpected semantic write")
}

func (r *searchRepository) DeleteDocument(context.Context, string, semantic.CandidateSemanticEntityType, string) error {
	return errors.New("unexpected semantic write")
}

func (r *searchRepository) Search(_ context.Context, query semantic.CandidateSemanticSearchQuery) ([]semantic.CandidateSemanticResult, error) {
	r.query = query
	if r.err != nil {
		return nil, r.err
	}
	return append([]semantic.CandidateSemanticResult{}, r.results...), nil
}

func (r *searchRepository) ListDocuments(context.Context, string) ([]semantic.CandidateSemanticDocument, error) {
	return nil, errors.New("unexpected semantic list")
}

type searchCandidateReader struct {
	candidate candidate.Candidate
	err       error
}

func (r searchCandidateReader) CurrentCandidate(context.Context) (candidate.Candidate, error) {
	if r.err != nil {
		return candidate.Candidate{}, r.err
	}
	return r.candidate, nil
}

func searchMetadata(status candidate.TruthStatus) candidate.KnowledgeMetadata {
	at := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	metadata := candidate.KnowledgeMetadata{
		TruthStatus: status,
		CreatedAt:   at,
		UpdatedAt:   at,
		Sources:     []candidate.KnowledgeSourceRecord{{Type: candidate.KnowledgeSourceUserConfirmed, Evidence: []string{"candidate confirmed"}}},
		Evidence:    []string{"candidate confirmed"},
	}
	if status == candidate.TruthStatusConfirmed {
		metadata.ConfirmedAt = &at
	}
	return metadata
}

func searchCandidate(id string) candidate.Candidate {
	return candidate.Candidate{
		ID: id,
		Projects: []candidate.CanonicalCandidateProject{{
			ID: "project-api", Name: "API automation", Description: "11 months of API integration", Technologies: []string{"Python"}, Metadata: searchMetadata(candidate.TruthStatusConfirmed),
		}},
		Stories: []candidate.CanonicalCandidateStory{{
			ID: "story-api", Title: "API story", Action: "built an API integration", ProfileRefs: []string{"project-api"},
		}},
	}
}

func searchResult(t *testing.T, value candidate.Candidate, entityType semantic.CandidateSemanticEntityType, entityID, model string, score float64) semantic.CandidateSemanticResult {
	t.Helper()
	drafts, err := candidatesemanticindex.BuildCandidateSemanticDocuments(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, draft := range drafts {
		if draft.EntityType == entityType && draft.EntityID == entityID {
			return semantic.CandidateSemanticResult{EntityType: entityType, EntityID: entityID, Score: score, ContentHash: draft.ContentHash, Model: model}
		}
	}
	t.Fatalf("semantic document %s/%s not found", entityType, entityID)
	return semantic.CandidateSemanticResult{}
}

func TestServiceSearchPreservesQueryAndRepositoryContract(t *testing.T) {
	value := searchCandidate("candidate-1")
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	repository := &searchRepository{results: []semantic.CandidateSemanticResult{searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-api", provider.model, .92)}}
	minScore := .8
	service := NewService(repository, provider, searchCandidateReader{candidate: value})

	results, err := service.Search(context.Background(), Query{
		CandidateID: value.ID,
		Query:       "  exact query  ",
		EntityTypes: []semantic.CandidateSemanticEntityType{semantic.CandidateSemanticEntityProject},
		Limit:       2,
		MinScore:    &minScore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provider.texts, []string{"  exact query  "}) {
		t.Fatalf("embedding texts=%q", provider.texts)
	}
	if repository.query.CandidateID != value.ID || repository.query.Limit != 2 || repository.query.MinScore == nil || *repository.query.MinScore != minScore || !reflect.DeepEqual(repository.query.EntityTypes, []semantic.CandidateSemanticEntityType{semantic.CandidateSemanticEntityProject}) {
		t.Fatalf("repository query=%+v", repository.query)
	}
	if len(repository.query.Embedding) != provider.dimensions || len(results) != 1 || len(results[0].EvidenceRefs) == 0 {
		t.Fatalf("results=%+v query=%+v", results, repository.query)
	}
}

func TestServiceSearchFiltersStaleIneligibleMissingAndWrongModelResults(t *testing.T) {
	value := searchCandidate("candidate-1")
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	valid := searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-api", provider.model, .95)
	stale := valid
	stale.ContentHash = "old-hash"
	wrongModel := valid
	wrongModel.Model = "model-old"
	missing := semantic.CandidateSemanticResult{EntityType: semantic.CandidateSemanticEntityProject, EntityID: "gone", ContentHash: valid.ContentHash, Model: provider.model, Score: 1}
	ineligibleCandidate := searchCandidate(value.ID)
	ineligibleCandidate.Projects[0].Metadata = searchMetadata(candidate.TruthStatusHypothesis)
	repository := &searchRepository{results: []semantic.CandidateSemanticResult{stale, wrongModel, missing, valid}}
	results, err := NewService(repository, provider, searchCandidateReader{candidate: value}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query"})
	if err != nil || len(results) != 1 || results[0].EntityID != valid.EntityID {
		t.Fatalf("filtered results=%+v err=%v", results, err)
	}
	results, err = NewService(&searchRepository{results: []semantic.CandidateSemanticResult{valid}}, provider, searchCandidateReader{candidate: ineligibleCandidate}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query"})
	if err != nil || len(results) != 0 {
		t.Fatalf("ineligible result=%+v err=%v", results, err)
	}
}

func TestServiceSearchRejectsCandidateScopeMismatch(t *testing.T) {
	value := searchCandidate("candidate-1")
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	repository := &searchRepository{results: []semantic.CandidateSemanticResult{searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-api", provider.model, 1)}}
	_, err := NewService(repository, provider, searchCandidateReader{candidate: searchCandidate("candidate-2")}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query"})
	if err == nil || !strings.Contains(err.Error(), "scope mismatch") {
		t.Fatalf("scope mismatch error=%v", err)
	}
}

func TestServiceSearchPreservesOrderingAndEmptyIndex(t *testing.T) {
	value := searchCandidate("candidate-1")
	value.Projects = append(value.Projects, candidate.CanonicalCandidateProject{ID: "project-second", Name: "Second project", Description: "second", Metadata: searchMetadata(candidate.TruthStatusConfirmed)})
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	first := searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-api", provider.model, .91)
	second := searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-second", provider.model, .89)
	repository := &searchRepository{results: []semantic.CandidateSemanticResult{first, second}}
	results, err := NewService(repository, provider, searchCandidateReader{candidate: value}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query", Limit: 1})
	if err != nil || len(results) != 2 || results[0].EntityID != first.EntityID || results[1].EntityID != second.EntityID {
		t.Fatalf("ordering=%+v err=%v", results, err)
	}
	results, err = NewService(&searchRepository{}, provider, searchCandidateReader{candidate: value}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query"})
	if err != nil || len(results) != 0 {
		t.Fatalf("empty index=%+v err=%v", results, err)
	}
}

func TestServiceSearchPreservesEmptyQueryAndFailureBoundaries(t *testing.T) {
	value := searchCandidate("candidate-1")
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	repository := &searchRepository{}
	service := NewService(repository, provider, searchCandidateReader{candidate: value})
	for _, query := range []string{"", "   "} {
		if _, err := service.Search(context.Background(), Query{CandidateID: value.ID, Query: query}); err == nil || len(provider.texts) != 0 {
			t.Fatalf("query %q err=%v texts=%q", query, err, provider.texts)
		}
	}
	if _, err := service.Search(context.Background(), Query{CandidateID: value.ID, Query: "x"}); err != nil || len(provider.texts) != 1 {
		t.Fatalf("short query err=%v texts=%q", err, provider.texts)
	}

	embedErr := errors.New("provider offline")
	provider.err = embedErr
	if _, err := service.Search(context.Background(), Query{CandidateID: value.ID, Query: "query"}); !errors.Is(err, ErrSemanticUnavailable) || !strings.Contains(err.Error(), embedErr.Error()) {
		t.Fatalf("embedding error=%v", err)
	}
	provider.err = nil
	repository.err = errors.New("repository offline")
	if _, err := service.Search(context.Background(), Query{CandidateID: value.ID, Query: "query"}); !strings.Contains(err.Error(), "repository offline") {
		t.Fatalf("repository error=%v", err)
	}
	repository.err = nil
	readerErr := errors.New("candidate unavailable")
	if _, err := NewService(repository, provider, searchCandidateReader{err: readerErr}).Search(context.Background(), Query{CandidateID: value.ID, Query: "query"}); !errors.Is(err, readerErr) {
		t.Fatalf("candidate reader error=%v", err)
	}
	provider.result = [][]float32{{1}}
	if _, err := service.Search(context.Background(), Query{CandidateID: value.ID, Query: "query"}); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("dimension error=%v", err)
	}
}

func TestServiceRetrieveUsesDefaultMinimumWithoutRewritingQuery(t *testing.T) {
	value := searchCandidate("candidate-1")
	provider := &searchEmbeddingProvider{model: "model-v1", dimensions: 3}
	result := searchResult(t, value, semantic.CandidateSemanticEntityProject, "project-api", provider.model, .9)
	repository := &searchRepository{results: []semantic.CandidateSemanticResult{result}}
	_, err := NewService(repository, provider, searchCandidateReader{candidate: value}).Retrieve(context.Background(), RetrievalRequest{CandidateID: value.ID, Query: "plain query"})
	if err != nil || !reflect.DeepEqual(provider.texts, []string{"plain query"}) || repository.query.MinScore == nil || *repository.query.MinScore != DefaultMinScore {
		t.Fatalf("retrieve err=%v texts=%q query=%+v", err, provider.texts, repository.query)
	}
}
