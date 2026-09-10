package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/semantic"
)

func semanticTestMetadata(status TruthStatus) KnowledgeMetadata {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	meta := KnowledgeMetadata{TruthStatus: status, CreatedAt: at, UpdatedAt: at, Sources: []KnowledgeSourceRecord{{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"candidate confirmed"}}}, Evidence: []string{"candidate confirmed"}}
	if status == TruthStatusConfirmed {
		meta.ConfirmedAt = &at
	}
	if status == TruthStatusHypothesis {
		meta.Sources = []KnowledgeSourceRecord{{Type: KnowledgeSourceDerived, Evidence: []string{"derived hypothesis"}}}
	}
	return meta
}

func TestBuildSemanticDocumentDeterministicAndTimestampIndependent(t *testing.T) {
	project := CanonicalCandidateProject{ID: "project-1", Name: "API automation", Role: "Integrator", Description: "Reduced repetitive requests", Technologies: []string{"Go", "PostgreSQL"}, Tasks: []string{"connect API", "remove manual step"}, Results: []string{"faster processing"}}
	first, err := BuildSemanticDocument(project)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildSemanticDocument(project)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash || first.Content != second.Content {
		t.Fatalf("same project produced different document: %+v %+v", first, second)
	}
	project.Metadata = semanticTestMetadata(TruthStatusConfirmed)
	third, err := BuildSemanticDocument(project)
	if err != nil || third.ContentHash != first.ContentHash {
		t.Fatalf("timestamps/truth metadata changed searchable hash: %v %+v", err, third)
	}
	project.Technologies = []string{"PostgreSQL", "Go"}
	ordered, err := BuildSemanticDocument(project)
	if err != nil || ordered.ContentHash != first.ContentHash {
		t.Fatalf("unordered technologies changed hash: %v %+v", err, ordered)
	}
	project.Description = "Different automation result"
	changed, err := BuildSemanticDocument(project)
	if err != nil || changed.ContentHash == first.ContentHash {
		t.Fatalf("semantic field did not change hash: %v", err)
	}
}

func TestBuildSemanticDocumentCoversStoryAndAchievement(t *testing.T) {
	story, err := BuildSemanticDocument(CanonicalCandidateStory{ID: "story-1", Title: "Automated routine work", Task: "remove manual requests", Action: "built an API integration", Result: "saved time", Technologies: []string{"Python"}})
	if err != nil || !strings.Contains(story.Content, "Title: Automated routine work") || !strings.Contains(story.Content, "Action: built an API integration") {
		t.Fatalf("story document=%+v err=%v", story, err)
	}
	achievement, err := BuildSemanticDocument(CandidateAchievement{ID: "achievement-1", Title: "Reduced manual work", Problem: "manual processing", Actions: []string{"mapped the process"}, Solution: []string{"automated the API"}, Result: []string{"fewer manual steps"}, Technologies: []string{"Python"}})
	if err != nil || !strings.Contains(achievement.Content, "Solution: automated the API") {
		t.Fatalf("achievement document=%+v err=%v", achievement, err)
	}
}

func TestSemanticEligibilityFailsClosed(t *testing.T) {
	confirmed := CanonicalCandidateProject{ID: "project-safe", Name: "Safe project", Description: "real work", Metadata: semanticTestMetadata(TruthStatusConfirmed)}
	hypothesis := CanonicalCandidateProject{ID: "project-unsafe", Name: "Hypothesis project", Description: "unverified work", Metadata: semanticTestMetadata(TruthStatusHypothesis)}
	story := CanonicalCandidateStory{ID: "story-safe", Title: "Safe story", Action: "did the work", ProfileRefs: []string{"project-safe"}}
	unsupported := CanonicalCandidateStory{ID: "story-unsupported", Title: "Unsupported story", Action: "unknown reference", ProfileRefs: []string{"missing"}}
	candidate := Candidate{ID: "candidate-1", Version: 1, Projects: []CanonicalCandidateProject{confirmed, hypothesis}, Stories: []CanonicalCandidateStory{story, unsupported}, Achievements: []CandidateAchievement{{ID: "achievement-unsafe", Title: "Unsafe", KnowledgeMetadata: semanticTestMetadata(TruthStatusHypothesis)}}}
	drafts, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		t.Fatal(err)
	}
	eligible := map[string]bool{}
	for _, draft := range drafts {
		eligible[string(draft.EntityType)+":"+draft.EntityID] = draft.Eligible
	}
	for key, want := range map[string]bool{"project:project-safe": true, "project:project-unsafe": false, "story:story-safe": true, "story:story-unsupported": false, "achievement:achievement-unsafe": false} {
		if eligible[key] != want {
			t.Fatalf("eligibility %s=%t want %t", key, eligible[key], want)
		}
	}
}

type fakeEmbeddingProvider struct {
	model      string
	dimensions int
	calls      int
	fail       bool
}

func (p *fakeEmbeddingProvider) Model() string   { return p.model }
func (p *fakeEmbeddingProvider) Dimensions() int { return p.dimensions }
func (p *fakeEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("provider offline")
	}
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vector := make([]float32, p.dimensions)
		if strings.Contains(strings.ToLower(text), "python") {
			vector[0] = 1
		} else if strings.Contains(strings.ToLower(text), "api") {
			vector[1] = 1
		} else {
			vector[2%p.dimensions] = 1
		}
		result[i] = vector
	}
	return result, nil
}

type staticCandidateRepository struct{ candidate Candidate }

func (r staticCandidateRepository) CurrentCandidate(context.Context) (Candidate, error) {
	return r.candidate, nil
}

func TestSemanticIndexingBatchesAndSkipsUnchangedDocuments(t *testing.T) {
	candidate := Candidate{ID: "candidate-1", Version: 1, Projects: []CanonicalCandidateProject{{ID: "project-1", Name: "API project", Description: "API automation", Metadata: semanticTestMetadata(TruthStatusConfirmed)}}}
	provider := &fakeEmbeddingProvider{model: "fake-v1", dimensions: 3}
	repository := NewMemoryCandidateSemanticRepository()
	service := NewCandidateSemanticIndexService(repository, provider)
	dryRunReport, err := service.Reindex(context.Background(), candidate, false)
	if err != nil || dryRunReport.New != 1 || provider.calls != 0 {
		t.Fatalf("dry-run called embedding provider: report=%+v calls=%d err=%v", dryRunReport, provider.calls, err)
	}
	report, err := service.Reindex(context.Background(), candidate, true)
	if err != nil || !report.Applied || provider.calls != 1 || report.New != 1 {
		t.Fatalf("first reindex report=%+v calls=%d err=%v", report, provider.calls, err)
	}
	report, err = service.Reindex(context.Background(), candidate, true)
	if err != nil || provider.calls != 1 || report.Unchanged != 1 {
		t.Fatalf("unchanged reindex report=%+v calls=%d err=%v", report, provider.calls, err)
	}
	provider.model = "fake-v2"
	report, err = service.Reindex(context.Background(), candidate, true)
	if err != nil || provider.calls != 2 || report.Changed != 1 {
		t.Fatalf("model change did not reindex: %+v calls=%d err=%v", report, provider.calls, err)
	}
}

func TestSemanticSearchRanksAndFiltersStaleCanonicalEntities(t *testing.T) {
	project := CanonicalCandidateProject{ID: "project-api", Name: "API project", Description: "API automation", Metadata: semanticTestMetadata(TruthStatusConfirmed)}
	candidate := Candidate{ID: "candidate-1", Version: 1, Projects: []CanonicalCandidateProject{project}}
	provider := &fakeEmbeddingProvider{model: "fake", dimensions: 3}
	repository := NewMemoryCandidateSemanticRepository()
	index := NewCandidateSemanticIndexService(repository, provider)
	if _, err := index.Reindex(context.Background(), candidate, true); err != nil {
		t.Fatal(err)
	}
	search := NewCandidateSemanticSearchService(repository, provider, staticCandidateRepository{candidate: candidate})
	results, err := search.Search(context.Background(), CandidateSemanticQuery{CandidateID: candidate.ID, Query: "API automation", Limit: 5})
	if err != nil || len(results) != 1 || results[0].EntityID != "project-api" || results[0].Score < 0.99 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	unsafe := candidate
	unsafe.Projects[0].Metadata = semanticTestMetadata(TruthStatusHypothesis)
	search = NewCandidateSemanticSearchService(repository, provider, staticCandidateRepository{candidate: unsafe})
	results, err = search.Search(context.Background(), CandidateSemanticQuery{CandidateID: candidate.ID, Query: "API automation", Limit: 5})
	if err != nil || len(results) != 0 {
		t.Fatalf("stale unsafe result was returned: %+v err=%v", results, err)
	}
}

func TestSemanticSearchEmptyAndEmbeddingFailureAreExplicit(t *testing.T) {
	provider := &fakeEmbeddingProvider{model: "fake", dimensions: 3, fail: true}
	service := NewCandidateSemanticSearchService(NewMemoryCandidateSemanticRepository(), provider, staticCandidateRepository{candidate: Candidate{ID: "candidate-1", Version: 1}})
	if _, err := service.Search(context.Background(), CandidateSemanticQuery{CandidateID: "candidate-1", Query: "experience"}); !errors.Is(err, ErrSemanticUnavailable) {
		t.Fatalf("provider failure=%v", err)
	}
	provider.fail = false
	results, err := service.Search(context.Background(), CandidateSemanticQuery{CandidateID: "candidate-1", Query: "experience"})
	if err != nil || len(results) != 0 {
		t.Fatalf("no indexed result=%+v err=%v", results, err)
	}
}

func TestMemorySemanticRepositoryFiltersMixedDimensionsByActiveSpace(t *testing.T) {
	repository := NewMemoryCandidateSemanticRepository()
	space1536, err := semantic.NewEmbeddingContract("openai-compatible", "model-a", 1536, "https://embed-a.example")
	if err != nil {
		t.Fatal(err)
	}
	space1024, err := semantic.NewEmbeddingContract("openai-compatible", "model-b", 1024, "https://embed-b.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id       string
		contract semantic.EmbeddingContract
	}{
		{"old", space1536},
		{"active", space1024},
	} {
		if err := repository.UpsertDocument(context.Background(), CandidateSemanticDocument{
			CandidateID: "candidate", EntityType: CandidateSemanticEntityProject, EntityID: item.id,
			Content: "content", ContentHash: item.id, Embedding: make([]float32, item.contract.Dimensions),
			EmbeddingProvider: item.contract.Provider, EmbeddingModel: item.contract.Model, EmbeddingDimensions: item.contract.Dimensions, EmbeddingSpaceID: item.contract.SpaceID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := repository.Search(context.Background(), CandidateSemanticSearchQuery{CandidateID: "candidate", Embedding: make([]float32, 1024), EmbeddingProvider: space1024.Provider, EmbeddingModel: space1024.Model, EmbeddingDimensions: 1024, EmbeddingSpaceID: space1024.SpaceID, Limit: 5})
	if err != nil || len(results) != 1 || results[0].EntityID != "active" {
		t.Fatalf("mixed-space results=%+v err=%v", results, err)
	}
	if err := repository.EmbeddingSpaceMatches(context.Background(), "candidate", space1024); !errors.Is(err, semantic.ErrSemanticIndexIncompatible) {
		t.Fatalf("mixed-space compatibility=%v", err)
	}
}

func TestRelevantKnowledgeSnapshotKeepsSemanticSelectionsSeparate(t *testing.T) {
	snapshot := RelevantKnowledgeSnapshot{Facts: []RelevantKnowledgeFact{{Key: "skill", NormalizedValue: "python", TruthStatus: TruthStatusConfirmed}}}
	result := snapshot.AddSemanticSelections([]CandidateSemanticResult{{EntityType: CandidateSemanticEntityStory, EntityID: "story-1", Score: .91, Title: "Automation", Excerpt: "A real narrative", ContentHash: "hash", Model: "fake"}}, 2)
	if len(result.Facts) != 1 || len(result.SemanticSelections) != 1 || result.SemanticSelections[0].Score != .91 {
		t.Fatalf("snapshot=%+v", result)
	}
	if reflect.DeepEqual(snapshot.Facts, result.SemanticSelections) {
		t.Fatal("semantic selections replaced structured facts")
	}
}
