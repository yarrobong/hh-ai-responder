package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"hh-ai-responder/internal/ports"
)

func TestPostgresCandidateSemanticRepositoryContract(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenPostgres(ctx, PostgresConfig{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := ApplyPostgresMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var extension string
	if err := pool.QueryRow(ctx, `SELECT extname FROM pg_extension WHERE extname='vector'`).Scan(&extension); err != nil || extension != "vector" {
		t.Skip("pgvector extension is unavailable")
	}
	at := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	candidateID := "semantic-contract-" + time.Now().UTC().Format("20060102150405.000000000")
	project := CanonicalCandidateProject{ID: "semantic-project-" + candidateID, Name: "API project", Description: "API automation", Metadata: KnowledgeMetadata{TruthStatus: TruthStatusConfirmed, CreatedAt: at, UpdatedAt: at, ConfirmedAt: &at, Sources: []KnowledgeSourceRecord{{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"fixture"}}}}}
	candidate := Candidate{ID: candidateID, Version: 1, UpdatedAt: at, Projects: []CanonicalCandidateProject{project}}
	repo := NewPostgresCandidateRepositoryForID(pool, candidateID)
	if err := repo.ImportCandidate(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	otherCandidateID := candidateID + "-other"
	otherProject := project
	otherProject.ID = project.ID + "-other"
	otherCandidate := Candidate{ID: otherCandidateID, Version: 1, UpdatedAt: at, Projects: []CanonicalCandidateProject{otherProject}}
	otherRepo := NewPostgresCandidateRepositoryForID(pool, otherCandidateID)
	if err := otherRepo.ImportCandidate(ctx, otherCandidate); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_semantic_documents WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_knowledge_sources WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_evidence WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_knowledge_events WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_projects WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_claims WHERE candidate_id=$1`, otherCandidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidates WHERE id=$1`, otherCandidateID)
	}()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_semantic_documents WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_knowledge_sources WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_evidence WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_knowledge_events WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_projects WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidate_claims WHERE candidate_id=$1`, candidateID)
		_, _ = pool.Exec(ctx, `DELETE FROM candidates WHERE id=$1`, candidateID)
	}()
	provider := &fakeEmbeddingProvider{model: "fake-1536", dimensions: CandidateSemanticEmbeddingDimensions}
	contract, err := ports.EmbeddingContractFor(provider)
	if err != nil {
		t.Fatal(err)
	}
	service := NewCandidateSemanticIndexService(NewPostgresCandidateSemanticRepository(pool), provider)
	report, err := service.Reindex(ctx, candidate, true)
	if err != nil || report.New != 1 {
		t.Fatalf("reindex=%+v err=%v", report, err)
	}
	otherReport, err := service.Reindex(ctx, otherCandidate, true)
	if err != nil || otherReport.New != 1 {
		t.Fatalf("other candidate reindex=%+v err=%v", otherReport, err)
	}
	results, err := NewPostgresCandidateSemanticRepository(pool).Search(ctx, CandidateSemanticSearchQuery{CandidateID: candidateID, Embedding: func() []float32 { v := make([]float32, CandidateSemanticEmbeddingDimensions); v[1] = 1; return v }(), EmbeddingProvider: contract.Provider, EmbeddingModel: contract.Model, EmbeddingDimensions: contract.Dimensions, EmbeddingSpaceID: contract.SpaceID, Limit: 5})
	if err != nil || len(results) != 1 || results[0].EntityID != project.ID {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	documents, err := NewPostgresCandidateSemanticRepository(pool).ListDocuments(ctx, candidateID)
	if err != nil || len(documents) != 1 {
		t.Fatalf("documents=%+v err=%v", documents, err)
	}
	if len(documents[0].Embedding) != CandidateSemanticEmbeddingDimensions {
		t.Fatalf("embedding dimensions were not round-tripped: dimensions=%d", len(documents[0].Embedding))
	}
	if documents[0].Embedding[1] != 1 {
		t.Fatalf("embedding value was not round-tripped: second=%v", documents[0].Embedding[1])
	}
	otherDocuments, err := NewPostgresCandidateSemanticRepository(pool).ListDocuments(ctx, otherCandidateID)
	if err != nil || len(otherDocuments) != 1 || otherDocuments[0].EntityID != otherProject.ID {
		t.Fatalf("other candidate documents=%+v err=%v", otherDocuments, err)
	}
	oldIndexed := documents[0].IndexedAt
	document := CandidateSemanticDocument{CandidateID: candidateID, EntityType: CandidateSemanticEntityProject, EntityID: project.ID, Content: report.Items[0].Draft.Content, ContentHash: report.Items[0].Draft.ContentHash, Embedding: make([]float32, CandidateSemanticEmbeddingDimensions), EmbeddingProvider: contract.Provider, EmbeddingModel: provider.Model(), EmbeddingDimensions: CandidateSemanticEmbeddingDimensions, EmbeddingSpaceID: contract.SpaceID, IndexedAt: oldIndexed.Add(time.Hour), SourceUpdatedAt: at, Metadata: report.Items[0].Draft.Metadata, TruthSnapshot: report.Items[0].Draft.TruthSnapshot}
	if err := NewPostgresCandidateSemanticRepository(pool).UpsertDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	after, err := NewPostgresCandidateSemanticRepository(pool).ListDocuments(ctx, candidateID)
	if err != nil || !after[0].IndexedAt.Equal(oldIndexed) {
		t.Fatalf("same hash/model was not a no-op: before=%v after=%v err=%v", oldIndexed, after[0].IndexedAt, err)
	}
	if err := NewPostgresCandidateSemanticRepository(pool).DeleteDocument(ctx, candidateID, CandidateSemanticEntityProject, project.ID); err != nil {
		t.Fatal(err)
	}
	after, err = NewPostgresCandidateSemanticRepository(pool).ListDocuments(ctx, candidateID)
	if err != nil || len(after) != 0 {
		t.Fatalf("delete stale document failed: %+v err=%v", after, err)
	}
	// A provider outage happens after the canonical transaction. The mutation
	// must remain committed and expose only a stale-index warning.
	failedProvider := &fakeEmbeddingProvider{model: "fake-1536", dimensions: CandidateSemanticEmbeddingDimensions, fail: true}
	mutations := NewCandidateMutationService(storageBackendPostgres, repo, NewPostgresCandidateStoreForID(pool, candidateID), "")
	mutations.SetSemanticIndexer(NewCandidateSemanticIndexService(NewPostgresCandidateSemanticRepository(pool), failedProvider))
	mutationResult, err := mutations.UpdateProject(ctx, UpdateProjectCommand{Actor: KnowledgeActorUser, Value: CandidateProject{ID: project.ID, Name: project.Name, Description: "committed while embedding provider is unavailable"}, Update: KnowledgeUpdate{Source: KnowledgeSourceRecord{Type: KnowledgeSourceUserConfirmed, Evidence: []string{"fixture update"}}, Reason: "fixture update"}})
	if err != nil || mutationResult.SemanticIndexWarning == "" {
		t.Fatalf("provider failure was not isolated after commit: result=%+v err=%v", mutationResult, err)
	}
	readBack, err := repo.CurrentCandidate(ctx)
	if err != nil || readBack.Projects[0].Description != "committed while embedding provider is unavailable" {
		t.Fatalf("canonical mutation was rolled back by embedding failure: candidate=%+v err=%v", readBack, err)
	}
}
