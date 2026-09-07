package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrSemanticUnavailable = errors.New("semantic retrieval is unavailable")

type CandidateSemanticIndexReport struct {
	Items          []CandidateSemanticIndexItem
	New            int
	Changed        int
	Unchanged      int
	Stale          int
	Ineligible     int
	Applied        bool
	EmbeddingModel string
}

type CandidateSemanticIndexService struct {
	Repository CandidateSemanticRepository
	Embeddings EmbeddingProvider
	Now        func() time.Time
}

func NewCandidateSemanticIndexService(repository CandidateSemanticRepository, embeddings EmbeddingProvider) *CandidateSemanticIndexService {
	return &CandidateSemanticIndexService{Repository: repository, Embeddings: embeddings, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *CandidateSemanticIndexService) Plan(ctx context.Context, candidate Candidate) (CandidateSemanticIndexReport, error) {
	if s == nil || s.Repository == nil {
		return CandidateSemanticIndexReport{}, errors.New("semantic repository is required")
	}
	drafts, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		return CandidateSemanticIndexReport{}, err
	}
	existing, err := s.Repository.ListDocuments(ctx, candidate.ID)
	if err != nil {
		return CandidateSemanticIndexReport{}, err
	}
	byKey := map[string]CandidateSemanticDocument{}
	for _, document := range existing {
		byKey[semanticDocumentKey(document.CandidateID, document.EntityType, document.EntityID)] = document
	}
	seen := map[string]bool{}
	report := CandidateSemanticIndexReport{Items: []CandidateSemanticIndexItem{}, EmbeddingModel: semanticProviderModel(s.Embeddings)}
	for _, draft := range drafts {
		key := semanticDocumentKey(candidate.ID, draft.EntityType, draft.EntityID)
		seen[key] = true
		if !draft.Eligible {
			report.Ineligible++
			report.Items = append(report.Items, CandidateSemanticIndexItem{Draft: draft, State: CandidateSemanticIneligible})
			continue
		}
		old, ok := byKey[key]
		state := CandidateSemanticNew
		if ok && old.ContentHash == draft.ContentHash && old.EmbeddingModel == report.EmbeddingModel && old.EmbeddingDimensions == semanticProviderDimensions(s.Embeddings) {
			state = CandidateSemanticUnchanged
			report.Unchanged++
		} else if ok {
			state = CandidateSemanticChanged
			report.Changed++
		} else {
			report.New++
		}
		report.Items = append(report.Items, CandidateSemanticIndexItem{Draft: draft, State: state})
	}
	for _, old := range existing {
		key := semanticDocumentKey(candidate.ID, old.EntityType, old.EntityID)
		if !seen[key] {
			report.Stale++
			draft := CandidateSemanticDocumentDraft{CandidateID: candidate.ID, EntityType: old.EntityType, EntityID: old.EntityID, ContentHash: old.ContentHash, Title: semanticTitle(old), Eligible: false, Eligibility: "canonical entity no longer exists"}
			report.Items = append(report.Items, CandidateSemanticIndexItem{Draft: draft, State: CandidateSemanticStale})
		}
	}
	sort.Slice(report.Items, func(i, j int) bool {
		return semanticDocumentKey(candidate.ID, report.Items[i].Draft.EntityType, report.Items[i].Draft.EntityID) < semanticDocumentKey(candidate.ID, report.Items[j].Draft.EntityType, report.Items[j].Draft.EntityID)
	})
	return report, nil
}

func (s *CandidateSemanticIndexService) Reindex(ctx context.Context, candidate Candidate, apply bool) (CandidateSemanticIndexReport, error) {
	report, err := s.Plan(ctx, candidate)
	if err != nil || !apply {
		return report, err
	}
	changed := []CandidateSemanticIndexItem{}
	for _, item := range report.Items {
		if item.State == CandidateSemanticNew || item.State == CandidateSemanticChanged {
			changed = append(changed, item)
		}
	}
	if len(changed) > 0 {
		if s.Embeddings == nil {
			return report, fmt.Errorf("%w: embedding provider is required for %d changed documents", ErrSemanticUnavailable, len(changed))
		}
		texts := make([]string, len(changed))
		for i := range changed {
			texts[i] = changed[i].Draft.Content
		}
		vectors, err := s.Embeddings.Embed(ctx, texts)
		if err != nil {
			return report, fmt.Errorf("%w: %v", ErrSemanticUnavailable, err)
		}
		if len(vectors) != len(changed) || s.Embeddings.Dimensions() <= 0 {
			return report, errors.New("embedding provider returned an invalid batch")
		}
		now := s.now()
		for i, item := range changed {
			if len(vectors[i]) != s.Embeddings.Dimensions() {
				return report, errors.New("embedding provider returned an invalid vector dimension")
			}
			document := CandidateSemanticDocument{CandidateID: candidate.ID, EntityType: item.Draft.EntityType, EntityID: item.Draft.EntityID, Content: item.Draft.Content, ContentHash: item.Draft.ContentHash, Embedding: vectors[i], EmbeddingModel: s.Embeddings.Model(), EmbeddingDimensions: s.Embeddings.Dimensions(), TruthSnapshot: item.Draft.TruthSnapshot, IndexedAt: now, SourceUpdatedAt: item.Draft.SourceUpdated, Metadata: item.Draft.Metadata}
			if err := s.Repository.UpsertDocument(ctx, document); err != nil {
				return report, err
			}
		}
	}
	for _, item := range report.Items {
		if item.State == CandidateSemanticStale || item.State == CandidateSemanticIneligible {
			if err := s.Repository.DeleteDocument(ctx, candidate.ID, item.Draft.EntityType, item.Draft.EntityID); err != nil {
				return report, err
			}
		}
	}
	report.Applied = true
	return report, nil
}

func (s *CandidateSemanticIndexService) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

type CandidateSemanticSearchService struct {
	Repository CandidateSemanticRepository
	Embeddings EmbeddingProvider
	Candidates CandidateRepository
}

func NewCandidateSemanticSearchService(repository CandidateSemanticRepository, embeddings EmbeddingProvider, candidates CandidateRepository) *CandidateSemanticSearchService {
	return &CandidateSemanticSearchService{Repository: repository, Embeddings: embeddings, Candidates: candidates}
}

func (s *CandidateSemanticSearchService) Search(ctx context.Context, query CandidateSemanticQuery) ([]CandidateSemanticResult, error) {
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
	results, err := s.Repository.Search(ctx, CandidateSemanticSearchQuery{CandidateID: query.CandidateID, Embedding: vectors[0], EntityTypes: query.EntityTypes, Limit: limit, MinScore: query.MinScore})
	if err != nil {
		return nil, err
	}
	candidate, err := s.Candidates.CurrentCandidate(ctx)
	if err != nil {
		return nil, err
	}
	eligible, err := BuildCandidateSemanticDocuments(candidate)
	if err != nil {
		return nil, err
	}
	allowed := map[string]CandidateSemanticDocumentDraft{}
	for _, draft := range eligible {
		if draft.Eligible {
			allowed[semanticDocumentKey(candidate.ID, draft.EntityType, draft.EntityID)] = draft
		}
	}
	filtered := make([]CandidateSemanticResult, 0, len(results))
	for _, result := range results {
		draft, ok := allowed[semanticDocumentKey(candidate.ID, result.EntityType, result.EntityID)]
		if !ok || result.ContentHash != draft.ContentHash || result.Model != s.Embeddings.Model() {
			continue
		}
		result.EvidenceRefs = append([]CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)
		filtered = append(filtered, result)
	}
	return filtered, nil
}

func semanticProviderModel(provider EmbeddingProvider) string {
	if provider == nil {
		return ""
	}
	return provider.Model()
}
func semanticProviderDimensions(provider EmbeddingProvider) int {
	if provider == nil {
		return 0
	}
	return provider.Dimensions()
}
