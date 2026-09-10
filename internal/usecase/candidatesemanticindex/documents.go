package candidatesemanticindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/semantic"
)

// CandidateSemanticDocumentDraft is a deterministic, pre-embedding semantic
// document. Ineligible drafts are retained for status and reindex planning,
// but are never written to the active semantic set.
type CandidateSemanticDocumentDraft struct {
	CandidateID   string
	EntityType    semantic.CandidateSemanticEntityType
	EntityID      string
	Content       string
	ContentHash   string
	Title         string
	Metadata      map[string]interface{}
	TruthSnapshot semantic.CandidateSemanticTruthSnapshot
	EvidenceRefs  []semantic.CandidateSemanticEvidenceRef
	Eligible      bool
	Eligibility   string
	SourceUpdated time.Time
}

// BuildStorySemanticDocument builds one deterministic searchable story
// representation. It does not perform I/O, embedding, or Candidate mutation.
func BuildStorySemanticDocument(value candidate.CanonicalCandidateStory) (CandidateSemanticDocumentDraft, error) {
	return buildStorySemanticDocument(value)
}

// BuildProjectSemanticDocument builds one deterministic searchable project
// representation. It does not perform I/O, embedding, or Candidate mutation.
func BuildProjectSemanticDocument(value candidate.CanonicalCandidateProject) (CandidateSemanticDocumentDraft, error) {
	return buildProjectSemanticDocument(value)
}

// BuildAchievementSemanticDocument builds one deterministic searchable
// achievement representation. It does not perform I/O, embedding, or
// Candidate mutation.
func BuildAchievementSemanticDocument(value candidate.CandidateAchievement) (CandidateSemanticDocumentDraft, error) {
	return buildAchievementSemanticDocument(value)
}

type semanticTextField struct{ Label, Value string }

type semanticHashPayload struct {
	Type   semantic.CandidateSemanticEntityType `json:"type"`
	Fields []semanticTextField                  `json:"fields"`
}

func buildStorySemanticDocument(value candidate.CanonicalCandidateStory) (CandidateSemanticDocumentDraft, error) {
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
	return semanticDraft(semantic.CandidateSemanticEntityStory, value.ID, value.Title, fields, semanticHashPayload{Type: semantic.CandidateSemanticEntityStory, Fields: normalizedFields(fields)})
}

func buildProjectSemanticDocument(value candidate.CanonicalCandidateProject) (CandidateSemanticDocumentDraft, error) {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Name) == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic project requires id and name")
	}
	fields := []semanticTextField{
		{"Name", value.Name}, {"Role", value.Role}, {"Description", value.Description},
		{"Technologies", strings.Join(sortedNormalizedList(value.Technologies), ", ")},
		{"Tasks", strings.Join(normalizedList(value.Tasks), "; ")}, {"Results", strings.Join(normalizedList(value.Results), "; ")},
		{"Related skills", strings.Join(sortedNormalizedList(value.RelatedSkills), ", ")},
	}
	return semanticDraft(semantic.CandidateSemanticEntityProject, value.ID, value.Name, fields, semanticHashPayload{Type: semantic.CandidateSemanticEntityProject, Fields: normalizedFields(fields)})
}

func buildAchievementSemanticDocument(value candidate.CandidateAchievement) (CandidateSemanticDocumentDraft, error) {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.Title) == "" {
		return CandidateSemanticDocumentDraft{}, errors.New("semantic achievement requires id and title")
	}
	fields := []semanticTextField{
		{"Title", value.Title}, {"Problem", value.Problem}, {"Actions", strings.Join(normalizedList(value.Actions), "; ")},
		{"Solution", strings.Join(normalizedList(value.Solution), "; ")}, {"Result", strings.Join(normalizedList(value.Result), "; ")},
		{"Technologies", strings.Join(sortedNormalizedList(value.Technologies), ", ")},
	}
	return semanticDraft(semantic.CandidateSemanticEntityAchievement, value.ID, value.Title, fields, semanticHashPayload{Type: semantic.CandidateSemanticEntityAchievement, Fields: normalizedFields(fields)})
}

func semanticDraft(entityType semantic.CandidateSemanticEntityType, entityID, title string, fields []semanticTextField, payload semanticHashPayload) (CandidateSemanticDocumentDraft, error) {
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
	title = normalizeSemanticText(title)
	return CandidateSemanticDocumentDraft{EntityType: entityType, EntityID: entityID, Content: content, ContentHash: hex.EncodeToString(sum[:]), Title: title, Metadata: map[string]interface{}{"title": title}}, nil
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

func safeSemanticMetadata(metadata candidate.KnowledgeMetadata) bool {
	return metadata.TruthStatus == candidate.TruthStatusConfirmed || metadata.TruthStatus == candidate.TruthStatusVerified
}

func candidateClaimSafe(value candidate.Candidate, claimID string) bool {
	if strings.TrimSpace(claimID) == "" {
		return true
	}
	for _, claim := range value.Claims {
		if claim.ID == claimID {
			return claim.State == candidate.CanonicalClaimActive && safeSemanticMetadata(claim.Metadata)
		}
	}
	return false
}

func semanticProjectEligibility(value candidate.Candidate, project candidate.CanonicalCandidateProject) (bool, string) {
	if !safeSemanticMetadata(project.Metadata) {
		return false, "project truth status is not confirmed or verified"
	}
	for _, claimID := range project.ClaimIDs {
		if !candidateClaimSafe(value, claimID) {
			return false, "project references an unsafe claim"
		}
	}
	for _, skill := range project.RelatedSkills {
		found := false
		for _, candidateSkill := range value.Skills {
			if strings.EqualFold(candidateSkill.ID, skill) || strings.EqualFold(candidateSkill.Name, skill) || strings.EqualFold(candidateSkill.DisplayName, skill) {
				found = true
				if candidateSkill.Negative || candidateSkill.State != candidate.CanonicalClaimActive || !safeSemanticMetadata(candidateSkill.Metadata) {
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

func semanticExperienceSafe(value candidate.Candidate, id string) bool {
	for _, experience := range value.Experience {
		if experience.ID == id {
			return safeSemanticMetadata(experience.Metadata) && candidateClaimSafe(value, experience.ClaimID)
		}
	}
	return false
}

// BuildCandidateSemanticDocuments applies the canonical truth-eligibility
// policy after deterministic document construction. Candidate is passed by
// value and is never modified.
func BuildCandidateSemanticDocuments(value candidate.Candidate) ([]CandidateSemanticDocumentDraft, error) {
	projects := map[string]bool{}
	projectReasons := map[string]string{}
	for _, project := range value.Projects {
		eligible, reason := semanticProjectEligibility(value, project)
		projects[project.ID] = eligible
		projectReasons[project.ID] = reason
	}
	result := make([]CandidateSemanticDocumentDraft, 0, len(value.Stories)+len(value.Projects)+len(value.Achievements))
	for _, story := range value.Stories {
		draft, err := BuildStorySemanticDocument(story)
		if err != nil {
			return nil, fmt.Errorf("build story %s: %w", story.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = value.ID, value.UpdatedAt
		draft.EvidenceRefs = make([]semantic.CandidateSemanticEvidenceRef, 0, len(story.ProfileRefs))
		eligible := len(story.ProfileRefs) > 0
		reason := ""
		for _, ref := range story.ProfileRefs {
			refEligible := false
			for _, project := range value.Projects {
				if project.ID == ref {
					refEligible = projects[ref]
					if !refEligible {
						reason = firstNonEmpty(projectReasons[ref], "story references an unsafe project")
					}
					draft.EvidenceRefs = append(draft.EvidenceRefs, semantic.CandidateSemanticEvidenceRef{EntityType: string(semantic.CandidateSemanticEntityProject), EntityID: ref, Relation: "story_profile_ref", ClaimIDs: append([]string{}, project.ClaimIDs...)})
				}
			}
			for _, experience := range value.Experience {
				if experience.ID == ref {
					refEligible = semanticExperienceSafe(value, ref)
					if !refEligible {
						reason = "story references an unsafe experience"
					}
					draft.EvidenceRefs = append(draft.EvidenceRefs, semantic.CandidateSemanticEvidenceRef{EntityType: "experience", EntityID: ref, Relation: "story_profile_ref", ClaimIDs: nonEmptyIDs(experience.ClaimID)})
				}
			}
			if !refEligible && reason == "" {
				reason = "story references an unknown canonical entity"
			}
			eligible = eligible && refEligible
		}
		draft.Eligible, draft.Eligibility = eligible, reason
		draft.TruthSnapshot = semantic.CandidateSemanticTruthSnapshot{References: append([]semantic.CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
		result = append(result, draft)
	}
	for _, project := range value.Projects {
		draft, err := BuildProjectSemanticDocument(project)
		if err != nil {
			return nil, fmt.Errorf("build project %s: %w", project.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = value.ID, value.UpdatedAt
		draft.Eligible, draft.Eligibility = projects[project.ID], projectReasons[project.ID]
		draft.EvidenceRefs = []semantic.CandidateSemanticEvidenceRef{{EntityType: string(semantic.CandidateSemanticEntityProject), EntityID: project.ID, Relation: "canonical_project", ClaimIDs: append([]string{}, project.ClaimIDs...)}}
		draft.TruthSnapshot = semantic.CandidateSemanticTruthSnapshot{TruthStatus: string(project.Metadata.TruthStatus), ClaimIDs: append([]string{}, project.ClaimIDs...), Evidence: append([]string{}, project.Metadata.Evidence...), References: append([]semantic.CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
		result = append(result, draft)
	}
	for _, achievement := range value.Achievements {
		draft, err := BuildAchievementSemanticDocument(achievement)
		if err != nil {
			return nil, fmt.Errorf("build achievement %s: %w", achievement.ID, err)
		}
		draft.CandidateID, draft.SourceUpdated = value.ID, value.UpdatedAt
		draft.Eligible = safeSemanticMetadata(achievement.KnowledgeMetadata)
		if !draft.Eligible {
			draft.Eligibility = "achievement truth status is not confirmed or verified"
		}
		draft.EvidenceRefs = []semantic.CandidateSemanticEvidenceRef{{EntityType: string(semantic.CandidateSemanticEntityAchievement), EntityID: achievement.ID, Relation: "canonical_achievement"}}
		if achievement.ProjectID != "" {
			draft.EvidenceRefs[0].ClaimIDs = nil
			if !projects[achievement.ProjectID] {
				draft.Eligible, draft.Eligibility = false, "achievement references an unsafe project"
			}
			draft.EvidenceRefs = append(draft.EvidenceRefs, semantic.CandidateSemanticEvidenceRef{EntityType: string(semantic.CandidateSemanticEntityProject), EntityID: achievement.ProjectID, Relation: "achievement_project_ref"})
		}
		draft.TruthSnapshot = semantic.CandidateSemanticTruthSnapshot{TruthStatus: string(achievement.TruthStatus), Evidence: append([]string{}, achievement.Evidence...), References: append([]semantic.CandidateSemanticEvidenceRef{}, draft.EvidenceRefs...)}
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
