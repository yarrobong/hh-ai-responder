package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CandidateSemanticEmbeddingDimensions is the single schema/provider
// dimension for the first production embedding model. Test providers may use
// smaller vectors behind an in-memory repository.
const CandidateSemanticEmbeddingDimensions = 1536

const (
	CandidateSemanticEntityStory       CandidateSemanticEntityType = "story"
	CandidateSemanticEntityProject     CandidateSemanticEntityType = "project"
	CandidateSemanticEntityAchievement CandidateSemanticEntityType = "achievement"
)

type CandidateSemanticEntityType string

type CandidateSemanticEvidenceRef struct {
	EntityType string   `json:"entity_type"`
	EntityID   string   `json:"entity_id"`
	Relation   string   `json:"relation,omitempty"`
	ClaimIDs   []string `json:"claim_ids,omitempty"`
}

type CandidateSemanticTruthSnapshot struct {
	TruthStatus string                         `json:"truth_status,omitempty"`
	ClaimIDs    []string                       `json:"claim_ids,omitempty"`
	Evidence    []string                       `json:"evidence,omitempty"`
	References  []CandidateSemanticEvidenceRef `json:"references,omitempty"`
}

// CandidateSemanticDocumentDraft is produced without I/O, AI or mutation.
// Ineligible drafts are useful to reindex/status commands but are never
// written to the active semantic set.
type CandidateSemanticDocumentDraft struct {
	CandidateID   string
	EntityType    CandidateSemanticEntityType
	EntityID      string
	Content       string
	ContentHash   string
	Title         string
	Metadata      map[string]any
	TruthSnapshot CandidateSemanticTruthSnapshot
	EvidenceRefs  []CandidateSemanticEvidenceRef
	Eligible      bool
	Eligibility   string
	SourceUpdated time.Time
}

type CandidateSemanticDocument struct {
	ID                  int64
	CandidateID         string
	EntityType          CandidateSemanticEntityType
	EntityID            string
	Content             string
	ContentHash         string
	Embedding           []float32
	EmbeddingModel      string
	EmbeddingDimensions int
	TruthSnapshot       CandidateSemanticTruthSnapshot
	IndexedAt           time.Time
	SourceUpdatedAt     time.Time
	Metadata            map[string]any
}

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

type CandidateSemanticResult struct {
	EntityType   CandidateSemanticEntityType
	EntityID     string
	Score        float64
	Title        string
	Excerpt      string
	EvidenceRefs []CandidateSemanticEvidenceRef
	ContentHash  string
	Model        string
}

type CandidateSemanticQuery struct {
	CandidateID string
	Query       string
	EntityTypes []CandidateSemanticEntityType
	Limit       int
	MinScore    *float64
}

type CandidateSemanticSearchQuery struct {
	CandidateID string
	Embedding   []float32
	EntityTypes []CandidateSemanticEntityType
	Limit       int
	MinScore    *float64
}

type CandidateSemanticRepository interface {
	UpsertDocument(ctx context.Context, document CandidateSemanticDocument) error
	DeleteDocument(ctx context.Context, candidateID string, entityType CandidateSemanticEntityType, entityID string) error
	Search(ctx context.Context, query CandidateSemanticSearchQuery) ([]CandidateSemanticResult, error)
	ListDocuments(ctx context.Context, candidateID string) ([]CandidateSemanticDocument, error)
}

type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

// BuildSemanticDocument builds one deterministic searchable representation.
// It intentionally knows nothing about embeddings, PostgreSQL or LLMs.
func BuildSemanticDocument(entity any) (CandidateSemanticDocumentDraft, error) {
	switch value := entity.(type) {
	case CanonicalCandidateStory:
		return buildStorySemanticDocument(value)
	case *CanonicalCandidateStory:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic story is nil")
		}
		return buildStorySemanticDocument(*value)
	case CanonicalCandidateProject:
		return buildProjectSemanticDocument(value)
	case *CanonicalCandidateProject:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic project is nil")
		}
		return buildProjectSemanticDocument(*value)
	case CandidateAchievement:
		return buildAchievementSemanticDocument(value)
	case *CandidateAchievement:
		if value == nil {
			return CandidateSemanticDocumentDraft{}, errors.New("semantic achievement is nil")
		}
		return buildAchievementSemanticDocument(*value)
	default:
		return CandidateSemanticDocumentDraft{}, fmt.Errorf("unsupported semantic entity %T", entity)
	}
}

func buildStorySemanticDocument(value CanonicalCandidateStory) (CandidateSemanticDocumentDraft, error) {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Title) == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic story requires id and title")
	}
	fields := []semanticTextField{
		{"Title", value.Title}, {"Situation", value.Situation}, {"Context", value.Context}, {"Summary", value.Summary},
		{"Description", value.Description}, {"Story", value.Story}, {"Task", value.Task}, {"Problem", value.Problem},
		{"Action", value.Action}, {"Actions", value.Actions}, {"Contribution", value.Contribution}, {"Result", value.Result},
		{"Outcome", value.Outcome}, {"Achievement", value.Achievement}, {"Achievements", value.Achievements},
		{"Technologies", strings.Join(sortedNormalizedList(value.Technologies), ", ")}, {"Skills", strings.Join(sortedNormalizedList(value.Skills), ", ")},
		{"Keywords", strings.Join(sortedNormalizedList(value.Keywords), ", ")}, {"Tags", strings.Join(sortedNormalizedList(value.Tags), ", ")},
		{"Roles", strings.Join(sortedNormalizedList(value.Roles), ", ")}, {"Relevance", strings.Join(sortedNormalizedList(value.Relevance), ", ")},
		{"Relevant for", strings.Join(sortedNormalizedList(value.RelevantFor), ", ")}, {"Relevant roles", strings.Join(sortedNormalizedList(value.RelevantRoles), ", ")},
	}
	return semanticDraft(CandidateSemanticEntityStory, value.ID, value.Title, fields, semanticHashPayload{
		Type: CandidateSemanticEntityStory, Fields: normalizedFields(fields),
	})
}

func buildProjectSemanticDocument(value CanonicalCandidateProject) (CandidateSemanticDocumentDraft, error) {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Name) == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic project requires id and name")
	}
	fields := []semanticTextField{
		{"Name", value.Name}, {"Role", value.Role}, {"Description", value.Description},
		{"Technologies", strings.Join(sortedNormalizedList(value.Technologies), ", ")},
		{"Tasks", strings.Join(normalizedList(value.Tasks), "; ")}, {"Results", strings.Join(normalizedList(value.Results), "; ")},
		{"Related skills", strings.Join(sortedNormalizedList(value.RelatedSkills), ", ")},
	}
	return semanticDraft(CandidateSemanticEntityProject, value.ID, value.Name, fields, semanticHashPayload{
		Type: CandidateSemanticEntityProject, Fields: normalizedFields(fields),
	})
}

func buildAchievementSemanticDocument(value CandidateAchievement) (CandidateSemanticDocumentDraft, error) {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Title) == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic achievement requires id and title")
	}
	fields := []semanticTextField{
		{"Title", value.Title}, {"Problem", value.Problem}, {"Actions", strings.Join(normalizedList(value.Actions), "; ")},
		{"Solution", strings.Join(normalizedList(value.Solution), "; ")}, {"Result", strings.Join(normalizedList(value.Result), "; ")},
		{"Technologies", strings.Join(sortedNormalizedList(value.Technologies), ", ")},
	}
	return semanticDraft(CandidateSemanticEntityAchievement, value.ID, value.Title, fields, semanticHashPayload{
		Type: CandidateSemanticEntityAchievement, Fields: normalizedFields(fields),
	})
}

type semanticTextField struct{ Label, Value string }

type semanticHashPayload struct {
	Type   CandidateSemanticEntityType `json:"type"`
	Fields []semanticTextField         `json:"fields"`
}

func semanticDraft(entityType CandidateSemanticEntityType, entityID, title string, fields []semanticTextField, payload semanticHashPayload) (CandidateSemanticDocumentDraft, error) {
	contentParts := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := normalizeSemanticText(field.Value); value != "" {
			contentParts = append(contentParts, field.Label+": "+value)
		}
	}
	content := strings.Join(contentParts, "\n")
	if content == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic entity has no searchable content")
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return CandidateSemanticDocumentDraft{}, fmt.Errorf("hash semantic entity: %w", err)
	}
	sum := sha256.Sum256(normalized)
	return CandidateSemanticDocumentDraft{EntityType: entityType, EntityID: entityID, Content: content, ContentHash: hex.EncodeToString(sum[:]), Title: normalizeSemanticText(title), Metadata: map[string]any{"title": normalizeSemanticText(title)}}, nil
}

func normalizedFields(fields []semanticTextField) []semanticTextField {
	result := make([]semanticTextField, 0, len(fields))
	for _, field := range fields {
		result = append(result, semanticTextField{Label: field.Label, Value: normalizeSemanticText(field.Value)})
	}
	return result
}

func normalizeSemanticText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func normalizedList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalizeSemanticText(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func sortedNormalizedList(values []string) []string {
	result := normalizedList(values)
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func safeSemanticMetadata(metadata KnowledgeMetadata) bool {
	return metadata.TruthStatus == TruthStatusConfirmed || metadata.TruthStatus == TruthStatusVerified
}

func candidateClaimSafe(candidate Candidate, claimID string) bool {
	if strings.TrimSpace(claimID) == "" {
		return true
	}
	for _, claim := range candidate.Claims {
		if claim.ID == claimID {
			return claim.State == CanonicalClaimActive && safeSemanticMetadata(claim.Metadata)
		}
	}
	return false
}

func semanticProjectEligibility(candidate Candidate, project CanonicalCandidateProject) (bool, string) {
	if !safeSemanticMetadata(project.Metadata) {
		return false, "project truth status is not confirmed or verified"
	}
	for _, claimID := range project.ClaimIDs {
		if !candidateClaimSafe(candidate, claimID) {
			return false, "project references an unsafe claim"
		}
	}
	for _, skill := range project.RelatedSkills {
		found := false
		for _, candidateSkill := range candidate.Skills {
			if strings.EqualFold(candidateSkill.ID, skill) || strings.EqualFold(candidateSkill.Name, skill) || strings.EqualFold(candidateSkill.DisplayName, skill) {
				found = true
				if candidateSkill.Negative || candidateSkill.State != CanonicalClaimActive || !safeSemanticMetadata(candidateSkill.Metadata) {
					return false, "project references an unsafe skill"
				}
			}
		}
		if !found {
			return false, "project references an unknown skill"
		}
	}
	return true, ""
}

func semanticExperienceSafe(candidate Candidate, id string) bool {
	for _, experience := range candidate.Experience {
		if experience.ID == id {
			return safeSemanticMetadata(experience.Metadata) && candidateClaimSafe(candidate, experience.ClaimID)
		}
	}
	return false
}

// BuildCandidateSemanticDocuments applies canonical eligibility rules after
// the pure builder. Only eligible documents are active employer-searchable
// candidates; unsafe entities stay in Candidate and are reported as such.
func BuildCandidateSemanticDocuments(candidate Candidate) ([]CandidateSemanticDocumentDraft, error) {
	projects := map[string]bool{}
	projectReasons := map[string]string{}
	for _, project := range candidate.Projects {
		eligible, reason := semanticProjectEligibility(candidate, project)
		projects[project.ID] = eligible
		projectReasons[project.ID] = reason
	}
	result := make([]CandidateSemanticDocumentDraft, 0, len(candidate.Stories)+len(candidate.Projects)+len(candidate.Achievements))
	for _, story := range candidate.Stories {
		draft, err := BuildSemanticDocument(story)
		if err != nil {
			return nil, fmt.Errorf("build story %s: %w", story.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = candidate.ID, candidate.UpdatedAt
		draft.EvidenceRefs = make([]CandidateSemanticEvidenceRef, 0, len(story.ProfileRefs))
		eligible := len(story.ProfileRefs) > 0
		reason := ""
		for _, ref := range story.ProfileRefs {
			refEligible := false
			for _, project := range candidate.Projects {
				if project.ID == ref {
					refEligible = projects[ref]
					if !refEligible {
						reason = firstNonEmpty(projectReasons[ref], "story references an unsafe project")
					}
					draft.EvidenceRefs = append(draft.EvidenceRefs, CandidateSemanticEvidenceRef{EntityType: string(CandidateSemanticEntityProject), EntityID: ref, Relation: "story_profile_ref", ClaimIDs: append([]string{}, project.ClaimIDs...)})
				}
			}
			for _, experience := range candidate.Experience {
				if experience.ID == ref {
					refEligible = semanticExperienceSafe(candidate, ref)
					if !refEligible {
						reason = "story references an unsafe experience"
					}
					draft.EvidenceRefs = append(draft.EvidenceRefs, CandidateSemanticEvidenceRef{EntityType: "experience", EntityID: ref, Relation: "story_profile_ref", ClaimIDs: nonEmptyIDs(experience.ClaimID)})
				}
			}
			if !refEligible && reason == "" {
				reason = "story references an unknown canonical entity"
			}
			eligible = eligible && refEligible
		}
		draft.Eligible, draft.Eligibility = eligible, reason
		draft.TruthSnapshot = CandidateSemanticTruthSnapshot{References: append([]CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
		result = append(result, draft)
	}
	for _, project := range candidate.Projects {
		draft, err := BuildSemanticDocument(project)
		if err != nil {
			return nil, fmt.Errorf("build project %s: %w", project.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = candidate.ID, candidate.UpdatedAt
		draft.Eligible, draft.Eligibility = projects[project.ID], projectReasons[project.ID]
		draft.EvidenceRefs = []CandidateSemanticEvidenceRef{{EntityType: string(CandidateSemanticEntityProject), EntityID: project.ID, Relation: "canonical_project", ClaimIDs: append([]string{}, project.ClaimIDs...)}}
		draft.TruthSnapshot = CandidateSemanticTruthSnapshot{TruthStatus: string(project.Metadata.TruthStatus), ClaimIDs: append([]string{}, project.ClaimIDs...), Evidence: append([]string{}, project.Metadata.Evidence...), References: append([]CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
		result = append(result, draft)
	}
	for _, achievement := range candidate.Achievements {
		draft, err := BuildSemanticDocument(achievement)
		if err != nil {
			return nil, fmt.Errorf("build achievement %s: %w", achievement.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = candidate.ID, candidate.UpdatedAt
		draft.Eligible = safeSemanticMetadata(achievement.KnowledgeMetadata)
		if !draft.Eligible {
			draft.Eligibility = "achievement truth status is not confirmed or verified"
		}
		draft.EvidenceRefs = []CandidateSemanticEvidenceRef{{EntityType: string(CandidateSemanticEntityAchievement), EntityID: achievement.ID, Relation: "canonical_achievement"}}
		if achievement.ProjectID != "" {
			draft.EvidenceRefs[0].ClaimIDs = nil
			if !projects[achievement.ProjectID] {
				draft.Eligible, draft.Eligibility = false, "achievement references an unsafe project"
			}
			draft.EvidenceRefs = append(draft.EvidenceRefs, CandidateSemanticEvidenceRef{EntityType: string(CandidateSemanticEntityProject), EntityID: achievement.ProjectID, Relation: "achievement_project_ref"})
		}
		draft.TruthSnapshot = CandidateSemanticTruthSnapshot{TruthStatus: string(achievement.TruthStatus), Evidence: append([]string{}, achievement.Evidence...), References: append([]CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
		result = append(result, draft)
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].EntityType)+"\x00"+result[i].EntityID < string(result[j].EntityType)+"\x00"+result[j].EntityID
	})
	return result, nil
}

func nonEmptyIDs(id string) []string {
	if id == "" {
		return nil
	}
	return []string{id}
}

func shortSemanticExcerpt(content string, max int) string {
	content = normalizeSemanticText(content)
	if len([]rune(content)) <= max {
		return content
	}
	return string([]rune(content)[:max]) + "…"
}
