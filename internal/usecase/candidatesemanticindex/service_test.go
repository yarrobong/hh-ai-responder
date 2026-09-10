package candidatesemanticindex

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/semantic"
)

func confirmedMetadata() candidate.KnowledgeMetadata {
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	return candidate.KnowledgeMetadata{TruthStatus: candidate.TruthStatusConfirmed, CreatedAt: at, UpdatedAt: at, ConfirmedAt: &at}
}

func TestBuildCandidateSemanticDocumentsIsDeterministicAndTruthSafe(t *testing.T) {
	project := candidate.CanonicalCandidateProject{ID: "project-1", Name: "API automation", Description: "11 months commercial experience", Technologies: []string{"PostgreSQL", "Go"}, Metadata: confirmedMetadata()}
	unsafeSkill := candidate.CanonicalCandidateSkill{ID: "kubernetes", Name: "Kubernetes", Negative: true, State: candidate.CanonicalClaimActive, Metadata: confirmedMetadata()}
	unsafeProject := candidate.CanonicalCandidateProject{ID: "project-2", Name: "Cluster work", RelatedSkills: []string{"kubernetes"}, Metadata: confirmedMetadata()}
	value := candidate.Candidate{ID: "candidate-1", UpdatedAt: time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC), Projects: []candidate.CanonicalCandidateProject{unsafeProject, project}, Skills: []candidate.CanonicalCandidateSkill{unsafeSkill}, Stories: []candidate.CanonicalCandidateStory{{ID: "story-1", Title: "API story", Action: "built an API integration", ProfileRefs: []string{"project-1"}}}, Achievements: []candidate.CandidateAchievement{{ID: "achievement-1", Title: "Reduced work", Solution: []string{"automated the API"}, KnowledgeMetadata: confirmedMetadata()}}}

	first, err := BuildCandidateSemanticDocuments(value)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildCandidateSemanticDocuments(value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same candidate produced different drafts: %#v %#v", first, second)
	}
	byID := map[string]CandidateSemanticDocumentDraft{}
	for _, draft := range first {
		byID[draft.EntityID] = draft
	}
	if !byID["project-1"].Eligible || byID["project-2"].Eligible {
		t.Fatalf("truth eligibility changed: project-1=%+v project-2=%+v", byID["project-1"], byID["project-2"])
	}
	if !strings.Contains(byID["project-1"].Content, "11 months commercial experience") {
		t.Fatalf("exact experience value was not preserved: %q", byID["project-1"].Content)
	}
	if byID["project-1"].ContentHash == "" || byID["story-1"].ContentHash == "" {
		t.Fatal("eligible drafts must have content hashes")
	}
}

type fakeEmbeddingProvider struct {
	model      string
	dimensions int
	calls      int
	countDelta int
	fail       error
}

func (p *fakeEmbeddingProvider) Model() string   { return p.model }
func (p *fakeEmbeddingProvider) Dimensions() int { return p.dimensions }
func (p *fakeEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.calls++
	if p.fail != nil {
		return nil, p.fail
	}
	count := len(texts) + p.countDelta
	if count < 0 {
		count = 0
	}
	result := make([][]float32, count)
	for i := range result {
		result[i] = make([]float32, p.dimensions)
	}
	return result, nil
}

type fakeRepository struct {
	documents map[string]semantic.CandidateSemanticDocument
	ops       []string
	listErr   error
	upsertErr error
	deleteErr error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{documents: map[string]semantic.CandidateSemanticDocument{}}
}

func (r *fakeRepository) UpsertDocument(_ context.Context, document semantic.CandidateSemanticDocument) error {
	r.ops = append(r.ops, "upsert:"+document.EntityID)
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.documents[key(document.CandidateID, document.EntityType, document.EntityID)] = document
	return nil
}

func (r *fakeRepository) DeleteDocument(_ context.Context, candidateID string, entityType semantic.CandidateSemanticEntityType, entityID string) error {
	r.ops = append(r.ops, "delete:"+entityID)
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.documents, key(candidateID, entityType, entityID))
	return nil
}

func (r *fakeRepository) Search(context.Context, semantic.CandidateSemanticSearchQuery) ([]semantic.CandidateSemanticResult, error) {
	return nil, nil
}

func (r *fakeRepository) ListDocuments(_ context.Context, candidateID string) ([]semantic.CandidateSemanticDocument, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	result := make([]semantic.CandidateSemanticDocument, 0, len(r.documents))
	for _, document := range r.documents {
		if document.CandidateID == candidateID {
			result = append(result, document)
		}
	}
	return result, nil
}

func key(candidateID string, entityType semantic.CandidateSemanticEntityType, entityID string) string {
	return candidateID + "\x00" + string(entityType) + "\x00" + entityID
}

func TestServicePreservesIdempotencyModelAndDimensionPlanning(t *testing.T) {
	value := candidate.Candidate{ID: "candidate-1", Projects: []candidate.CanonicalCandidateProject{{ID: "project-1", Name: "API", Metadata: confirmedMetadata()}}}
	repository := newFakeRepository()
	provider := &fakeEmbeddingProvider{model: "model-v1", dimensions: 3}
	service := NewService(repository, provider)

	if report, err := service.Reindex(context.Background(), value, true); err != nil || report.New != 1 || provider.calls != 1 {
		t.Fatalf("first reindex report=%+v calls=%d err=%v", report, provider.calls, err)
	}
	if report, err := service.Reindex(context.Background(), value, true); err != nil || report.Unchanged != 1 || provider.calls != 1 {
		t.Fatalf("unchanged reindex report=%+v calls=%d err=%v", report, provider.calls, err)
	}
	provider.model = "model-v2"
	if report, err := service.Reindex(context.Background(), value, true); err != nil || report.Changed != 1 || provider.calls != 2 {
		t.Fatalf("model change report=%+v calls=%d err=%v", report, provider.calls, err)
	}
	provider.dimensions = 4
	if report, err := service.Reindex(context.Background(), value, true); err != nil || report.Changed != 1 || provider.calls != 3 {
		t.Fatalf("dimension change report=%+v calls=%d err=%v", report, provider.calls, err)
	}
}

func TestServiceEmbedsBeforeDeletingStaleAndFailsClosed(t *testing.T) {
	value := candidate.Candidate{ID: "candidate-1", Projects: []candidate.CanonicalCandidateProject{{ID: "project-1", Name: "API", Metadata: confirmedMetadata()}}}
	repository := newFakeRepository()
	provider := &fakeEmbeddingProvider{model: "model", dimensions: 2}
	service := NewService(repository, provider)
	if _, err := service.Reindex(context.Background(), value, true); err != nil {
		t.Fatal(err)
	}
	repository.documents[key(value.ID, semantic.CandidateSemanticEntityProject, "stale")] = semantic.CandidateSemanticDocument{CandidateID: value.ID, EntityType: semantic.CandidateSemanticEntityProject, EntityID: "stale", ContentHash: "old", EmbeddingModel: provider.model, EmbeddingDimensions: provider.dimensions}
	repository.ops = nil
	changed := value
	changed.Projects[0].Description = "changed API"
	provider.fail = errors.New("offline")
	if _, err := service.Reindex(context.Background(), changed, true); err == nil || len(repository.ops) != 0 {
		t.Fatalf("embedding failure changed index before replacement: err=%v ops=%v", err, repository.ops)
	}
	provider.fail = nil
	if _, err := service.Reindex(context.Background(), changed, true); err != nil {
		t.Fatal(err)
	}
	if len(repository.ops) != 2 || repository.ops[0] != "upsert:project-1" || repository.ops[1] != "delete:stale" {
		t.Fatalf("stale deletion did not follow successful replacement: ops=%v", repository.ops)
	}
}

func TestServiceRejectsEmbeddingCountMismatch(t *testing.T) {
	value := candidate.Candidate{ID: "candidate-1", Projects: []candidate.CanonicalCandidateProject{{ID: "project-1", Name: "API", Metadata: confirmedMetadata()}}}
	repository := newFakeRepository()
	provider := &fakeEmbeddingProvider{model: "model", dimensions: 2, countDelta: -1}
	if _, err := NewService(repository, provider).Reindex(context.Background(), value, true); err == nil {
		t.Fatal("count mismatch must fail")
	}
	if len(repository.ops) != 0 {
		t.Fatalf("count mismatch must not upsert: ops=%v", repository.ops)
	}
}

func TestServiceReindexesConfigured1024Space(t *testing.T) {
	value := candidate.Candidate{ID: "candidate-1", Projects: []candidate.CanonicalCandidateProject{{ID: "project-1", Name: "API", Metadata: confirmedMetadata()}}}
	repository := newFakeRepository()
	provider := &fakeEmbeddingProvider{model: "model-1024", dimensions: 1024}
	if _, err := NewService(repository, provider).Reindex(context.Background(), value, true); err != nil {
		t.Fatal(err)
	}
	document := repository.documents[key(value.ID, semantic.CandidateSemanticEntityProject, "project-1")]
	if document.EmbeddingDimensions != 1024 || len(document.Embedding) != 1024 || document.EmbeddingSpaceID == "" {
		t.Fatalf("1024 contract was not persisted: %+v", document)
	}
}
