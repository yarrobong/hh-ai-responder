package candidatesemanticindex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/semantic"
)

var ErrSemanticUnavailable = errors.New("semantic retrieval is unavailable")

type CandidateSemanticIndexState string

const (
	CandidateSemanticNew        CandidateSemanticIndexState = "new"
	CandidateSemanticChanged    CandidateSemanticIndexState = "changed"
	CandidateSemanticUnchanged  CandidateSemanticIndexState = "unchanged"
	CandidateSemanticStale      CandidateSemanticIndexState = "stale"
	CandidateSemanticIneligible CandidateSemanticIndexState = "ineligible"
)

type CandidateSemanticIndexItem struct {
	Draft CandidateSemanticDocumentDraft
	State CandidateSemanticIndexState
}

type CandidateSemanticIndexReport struct {
	Items               []CandidateSemanticIndexItem
	New                 int
	Changed             int
	Unchanged           int
	Stale               int
	Ineligible          int
	Applied             bool
	EmbeddingProvider   string
	EmbeddingModel      string
	EmbeddingDimensions int
	EmbeddingSpaceID    string
}

// Service compares canonical semantic drafts with the supplied derived index,
// embeds only changed/new documents, upserts replacements, then removes stale
// or newly ineligible documents. The order is intentional: working index rows
// are not deleted before replacement embedding and upsert succeed.
type Service struct {
	Repository ports.CandidateSemanticRepository
	Embeddings ports.EmbeddingProvider
	Now        func() time.Time
}

func NewService(repository ports.CandidateSemanticRepository, embeddings ports.EmbeddingProvider) *Service {
	return &Service{Repository: repository, Embeddings: embeddings, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Plan(ctx context.Context, value candidate.Candidate) (CandidateSemanticIndexReport, error) {
	if s == nil || s.Repository == nil {
		return CandidateSemanticIndexReport{}, errors.New("semantic repository is required")
	}
	drafts, err := BuildCandidateSemanticDocuments(value)
	if err != nil {
		return CandidateSemanticIndexReport{}, err
	}
	existing, err := s.Repository.ListDocuments(ctx, value.ID)
	if err != nil {
		return CandidateSemanticIndexReport{}, err
	}
	byKey := map[string]semantic.CandidateSemanticDocument{}
	for _, document := range existing {
		byKey[semanticDocumentKey(document.CandidateID, document.EntityType, document.EntityID)] = document
	}
	seen := map[string]bool{}
	contract := semantic.EmbeddingContract{}
	if s.Embeddings != nil {
		contract, err = ports.EmbeddingContractFor(s.Embeddings)
		if err != nil {
			return CandidateSemanticIndexReport{}, err
		}
	}
	report := CandidateSemanticIndexReport{Items: []CandidateSemanticIndexItem{}, EmbeddingProvider: contract.Provider, EmbeddingModel: contract.Model, EmbeddingDimensions: contract.Dimensions, EmbeddingSpaceID: contract.SpaceID}
	for _, draft := range drafts {
		key := semanticDocumentKey(value.ID, draft.EntityType, draft.EntityID)
		seen[key] = true
		if !draft.Eligible {
			report.Ineligible++
			report.Items = append(report.Items, CandidateSemanticIndexItem{Draft: draft, State: CandidateSemanticIneligible})
			continue
		}
		old, ok := byKey[key]
		state := CandidateSemanticNew
		if ok && old.ContentHash == draft.ContentHash && old.EmbeddingProvider == contract.Provider && old.EmbeddingModel == report.EmbeddingModel && old.EmbeddingDimensions == contract.Dimensions && old.EmbeddingSpaceID == contract.SpaceID {
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
		key := semanticDocumentKey(value.ID, old.EntityType, old.EntityID)
		if !seen[key] {
			report.Stale++
			draft := CandidateSemanticDocumentDraft{CandidateID: value.ID, EntityType: old.EntityType, EntityID: old.EntityID, ContentHash: old.ContentHash, Title: semanticTitle(old), Eligible: false, Eligibility: "canonical entity no longer exists"}
			report.Items = append(report.Items, CandidateSemanticIndexItem{Draft: draft, State: CandidateSemanticStale})
		}
	}
	sort.Slice(report.Items, func(i, j int) bool {
		return semanticDocumentKey(value.ID, report.Items[i].Draft.EntityType, report.Items[i].Draft.EntityID) < semanticDocumentKey(value.ID, report.Items[j].Draft.EntityType, report.Items[j].Draft.EntityID)
	})
	return report, nil
}

func (s *Service) Reindex(ctx context.Context, value candidate.Candidate, apply bool) (CandidateSemanticIndexReport, error) {
	report, err := s.Plan(ctx, value)
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
		contract, err := ports.EmbeddingContractFor(s.Embeddings)
		if err != nil {
			return report, err
		}
		if len(vectors) != len(changed) {
			return report, errors.New("embedding provider returned an invalid batch")
		}
		now := s.now()
		for i, item := range changed {
			if err := semantic.ValidateVector(vectors[i], contract.Dimensions, "embedding provider"); err != nil {
				return report, err
			}
			document := semantic.CandidateSemanticDocument{CandidateID: value.ID, EntityType: item.Draft.EntityType, EntityID: item.Draft.EntityID, Content: item.Draft.Content, ContentHash: item.Draft.ContentHash, Embedding: vectors[i], EmbeddingProvider: contract.Provider, EmbeddingModel: contract.Model, EmbeddingDimensions: contract.Dimensions, EmbeddingSpaceID: contract.SpaceID, TruthSnapshot: item.Draft.TruthSnapshot, IndexedAt: now, SourceUpdatedAt: item.Draft.SourceUpdated, Metadata: item.Draft.Metadata}
			if err := s.Repository.UpsertDocument(ctx, document); err != nil {
				return report, err
			}
		}
	}
	for _, item := range report.Items {
		if item.State == CandidateSemanticStale || item.State == CandidateSemanticIneligible {
			if err := s.Repository.DeleteDocument(ctx, value.ID, item.Draft.EntityType, item.Draft.EntityID); err != nil {
				return report, err
			}
		}
	}
	report.Applied = true
	return report, nil
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func semanticDocumentKey(candidateID string, entityType semantic.CandidateSemanticEntityType, entityID string) string {
	return candidateID + "\x00" + string(entityType) + "\x00" + entityID
}

func semanticTitle(document semantic.CandidateSemanticDocument) string {
	if value, ok := document.Metadata["title"].(string); ok {
		return value
	}
	return ""
}
