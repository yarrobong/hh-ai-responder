package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// RelevantKnowledgeSnapshot is the semantic, query-scoped part of Candidate
// Knowledge used by one draft. Timestamps, event ids and collection metadata
// are intentionally excluded: they are audit information, not knowledge.
type RelevantKnowledgeSnapshot struct {
	Facts              []RelevantKnowledgeFact     `json:"facts"`
	ForbiddenClaims    []string                    `json:"forbidden_claims,omitempty"`
	SemanticSelections []RelevantSemanticKnowledge `json:"semantic_selections,omitempty"`
}

// RelevantSemanticKnowledge is a query-scoped narrative selection. It is
// deliberately separate from Facts: similarity is relevance, never evidence.
type RelevantSemanticKnowledge struct {
	EntityType   CandidateSemanticEntityType    `json:"entity_type"`
	EntityID     string                         `json:"entity_id"`
	Score        float64                        `json:"score"`
	Title        string                         `json:"title"`
	Excerpt      string                         `json:"excerpt,omitempty"`
	EvidenceRefs []CandidateSemanticEvidenceRef `json:"evidence_refs,omitempty"`
	ContentHash  string                         `json:"content_hash"`
	Model        string                         `json:"embedding_model"`
}

type RelevantKnowledgeFact struct {
	Key             string                        `json:"key"`
	NormalizedValue string                        `json:"normalized_value"`
	TruthStatus     TruthStatus                   `json:"truth_status"`
	Provenance      []RelevantKnowledgeProvenance `json:"provenance,omitempty"`
}

type RelevantKnowledgeProvenance struct {
	Source    string   `json:"source"`
	Reference string   `json:"reference,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

func (s RelevantKnowledgeSnapshot) normalized() RelevantKnowledgeSnapshot {
	result := RelevantKnowledgeSnapshot{Facts: append([]RelevantKnowledgeFact{}, s.Facts...), ForbiddenClaims: normalizedStringSet(s.ForbiddenClaims), SemanticSelections: append([]RelevantSemanticKnowledge{}, s.SemanticSelections...)}
	sort.Slice(result.SemanticSelections, func(i, j int) bool {
		if result.SemanticSelections[i].Score != result.SemanticSelections[j].Score {
			return result.SemanticSelections[i].Score > result.SemanticSelections[j].Score
		}
		return semanticDocumentKey("", result.SemanticSelections[i].EntityType, result.SemanticSelections[i].EntityID) < semanticDocumentKey("", result.SemanticSelections[j].EntityType, result.SemanticSelections[j].EntityID)
	})
	for i := range result.Facts {
		result.Facts[i].Key = normalizeKnowledgeText(result.Facts[i].Key)
		result.Facts[i].NormalizedValue = normalizeKnowledgeText(result.Facts[i].NormalizedValue)
		for j := range result.Facts[i].Provenance {
			result.Facts[i].Provenance[j].Source = normalizeKnowledgeText(result.Facts[i].Provenance[j].Source)
			result.Facts[i].Provenance[j].Reference = normalizeKnowledgeText(result.Facts[i].Provenance[j].Reference)
			result.Facts[i].Provenance[j].Evidence = normalizedStringSet(result.Facts[i].Provenance[j].Evidence)
		}
		sort.Slice(result.Facts[i].Provenance, func(a, b int) bool {
			left, right := result.Facts[i].Provenance[a], result.Facts[i].Provenance[b]
			return provenanceSortKey(left) < provenanceSortKey(right)
		})
	}
	sort.Slice(result.Facts, func(i, j int) bool {
		if result.Facts[i].Key != result.Facts[j].Key {
			return result.Facts[i].Key < result.Facts[j].Key
		}
		return result.Facts[i].NormalizedValue < result.Facts[j].NormalizedValue
	})
	return result
}

// AddSemanticSelections attaches only already truth-validated retrieval
// results to an existing structured snapshot. It does not load canonical data
// or decide eligibility; callers must use CandidateSemanticSearchService.
func (s RelevantKnowledgeSnapshot) AddSemanticSelections(results []CandidateSemanticResult, limit int) RelevantKnowledgeSnapshot {
	if limit <= 0 {
		limit = 3
	}
	if len(results) > limit {
		results = results[:limit]
	}
	s.SemanticSelections = make([]RelevantSemanticKnowledge, 0, len(results))
	for _, result := range results {
		s.SemanticSelections = append(s.SemanticSelections, RelevantSemanticKnowledge{EntityType: result.EntityType, EntityID: result.EntityID, Score: result.Score, Title: result.Title, Excerpt: result.Excerpt, EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, result.EvidenceRefs...), ContentHash: result.ContentHash, Model: result.Model})
	}
	return s.normalized()
}

func (s RelevantKnowledgeSnapshot) AddSafeSemanticSelections(results []SafeSemanticSelection) RelevantKnowledgeSnapshot {
	s.SemanticSelections = make([]RelevantSemanticKnowledge, 0, len(results))
	for _, result := range results {
		s.SemanticSelections = append(s.SemanticSelections, RelevantSemanticKnowledge{
			EntityType: result.EntityType, EntityID: result.EntityID, Score: result.Score,
			Title: result.Title, Excerpt: truncateRunes(result.Text, 280), EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, result.EvidenceRefs...),
			ContentHash: result.ContentHash, Model: result.EmbeddingModel,
		})
	}
	return s.normalized()
}

func RelevantKnowledgeHash(snapshot RelevantKnowledgeSnapshot) string {
	raw, _ := json.Marshal(snapshot.normalized())
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeKnowledgeText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func normalizedStringSet(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = normalizeKnowledgeText(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func provenanceSortKey(value RelevantKnowledgeProvenance) string {
	return value.Source + "\x00" + value.Reference + "\x00" + strings.Join(value.Evidence, "\x00")
}

func profileProvenance(fact ProfileFact) []RelevantKnowledgeProvenance {
	if !fact.Confirmed || !sourceTrustedForEmployerCommunication(fact.Source) {
		return nil
	}
	return []RelevantKnowledgeProvenance{{Source: string(fact.Source), Evidence: fact.Evidence}}
}

func detailedProvenance(metadata KnowledgeMetadata) []RelevantKnowledgeProvenance {
	if metadata.TruthStatus == TruthStatusUnknown || metadata.TruthStatus == TruthStatusHypothesis {
		return nil
	}
	result := make([]RelevantKnowledgeProvenance, 0, len(metadata.Sources))
	for _, source := range metadata.Sources {
		result = append(result, RelevantKnowledgeProvenance{Source: string(source.Type), Reference: source.Reference, Evidence: source.Evidence})
	}
	return result
}

func snapshotFact(key, value string, status TruthStatus, provenance ...[]RelevantKnowledgeProvenance) RelevantKnowledgeFact {
	var sources []RelevantKnowledgeProvenance
	for _, group := range provenance {
		sources = append(sources, group...)
	}
	return RelevantKnowledgeFact{Key: key, NormalizedValue: value, TruthStatus: status, Provenance: sources}
}

func statusForProfile(fact ProfileFact) TruthStatus {
	if fact.Confirmed && sourceTrustedForEmployerCommunication(fact.Source) {
		return TruthStatusConfirmed
	}
	return TruthStatusUnknown
}

func profileAtomicSnapshotFacts(profile CandidateProfile, topic string) []RelevantKnowledgeFact {
	key := contextCanonical(topic)
	addString := func(name string, fact ProfileStringFact) []RelevantKnowledgeFact {
		if statusForProfile(fact.ProfileFact) == TruthStatusUnknown {
			return []RelevantKnowledgeFact{snapshotFact(name, "", TruthStatusUnknown)}
		}
		return []RelevantKnowledgeFact{snapshotFact(name, fact.Value, TruthStatusConfirmed, profileProvenance(fact.ProfileFact))}
	}
	switch key {
	case "salary":
		fact := profile.EmployerCommunicationPreferences.Salary
		if statusForProfile(fact.ProfileFact) == TruthStatusConfirmed && strings.TrimSpace(fact.Value) != "" {
			return addString("profile.salary", fact)
		}
		minimum := profile.WorkPreferences.SalaryMinimum
		if statusForProfile(minimum.ProfileFact) == TruthStatusConfirmed {
			return []RelevantKnowledgeFact{snapshotFact("profile.salary", fmt.Sprintf("minimum %d", minimum.Value), TruthStatusConfirmed, profileProvenance(minimum.ProfileFact))}
		}
		return []RelevantKnowledgeFact{snapshotFact("profile.salary", "", TruthStatusUnknown)}
	case "relocation":
		return addString("profile.relocation", profile.WorkPreferences.Relocation)
	case "work_mode":
		return addString("profile.work_mode", profile.WorkPreferences.WorkMode)
	case "business_trips":
		return addString("profile.business_trips", profile.WorkPreferences.BusinessTrips)
	case "roles":
		value := strings.Join([]string{profile.WorkPreferences.PrimaryRoles.Value, profile.WorkPreferences.SecondaryRoles.Value, profile.WorkPreferences.PreferredRoles.Value}, "; ")
		sources := append(profileProvenance(profile.WorkPreferences.PrimaryRoles.ProfileFact), profileProvenance(profile.WorkPreferences.SecondaryRoles.ProfileFact)...)
		sources = append(sources, profileProvenance(profile.WorkPreferences.PreferredRoles.ProfileFact)...)
		status := TruthStatusUnknown
		if strings.TrimSpace(value) != "" {
			status = TruthStatusConfirmed
		}
		return []RelevantKnowledgeFact{snapshotFact("profile.roles", value, status, sources)}
	case "total_experience":
		fact := profile.TotalExperienceMonths
		if statusForProfile(fact.ProfileFact) == TruthStatusConfirmed {
			return []RelevantKnowledgeFact{snapshotFact("profile.total_experience", fmt.Sprintf("%d months", fact.Value), TruthStatusConfirmed, profileProvenance(fact.ProfileFact))}
		}
		return []RelevantKnowledgeFact{snapshotFact("profile.total_experience", "", TruthStatusUnknown)}
	case "education", "higher_education":
		result := []RelevantKnowledgeFact{}
		for i, fact := range profile.Education {
			status := statusForProfile(fact.ProfileFact)
			value := strings.Join([]string{fact.Level, fact.Specialty, fact.Institution, fact.Details}, ", ")
			if status == TruthStatusUnknown {
				value = ""
			}
			result = append(result, snapshotFact(fmt.Sprintf("profile.%s.%d", key, i), value, status, profileProvenance(fact.ProfileFact)))
		}
		if len(result) == 0 {
			result = append(result, snapshotFact("profile."+key, "", TruthStatusUnknown))
		}
		return result
	case "english":
		result := []RelevantKnowledgeFact{}
		for i, fact := range profile.Languages {
			if !candidateContainsAny(strings.ToLower(fact.Name), "англ", "english") {
				continue
			}
			status := statusForProfile(fact.ProfileFact)
			value := fact.Name + ": " + fact.Level
			if status == TruthStatusUnknown {
				value = ""
			}
			result = append(result, snapshotFact(fmt.Sprintf("profile.english.%d", i), value, status, profileProvenance(fact.ProfileFact)))
		}
		if len(result) == 0 {
			result = append(result, snapshotFact("profile.english", "", TruthStatusUnknown))
		}
		return result
	}
	return nil
}

func relevantKnowledgeSnapshot(kb *CandidateKnowledgeBase, context CandidateContext, draftText string, usedFacts []string) (RelevantKnowledgeSnapshot, error) {
	if kb == nil {
		return RelevantKnowledgeSnapshot{}, errors.New("candidate knowledge is unavailable")
	}
	candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: kb.Profile, Knowledge: *kb})
	if err != nil || len(diagnostics.Conflicts) > 0 {
		return RelevantKnowledgeSnapshot{}, errors.New("candidate knowledge is unavailable")
	}
	return relevantKnowledgeSnapshotFromCandidate(candidate, context, draftText, usedFacts)
}

func relevantKnowledgeSnapshotFromCandidate(candidate Candidate, context CandidateContext, draftText string, usedFacts []string) (RelevantKnowledgeSnapshot, error) {
	safe, err := CanonicalEmployerSafeProjection(candidate)
	if err != nil {
		return RelevantKnowledgeSnapshot{}, errors.New("candidate knowledge is unavailable")
	}
	return buildRelevantKnowledgeSnapshot(candidate, safe, context, draftText, usedFacts)
}

func relevantKnowledgeSnapshotForResolver(resolver *CandidateContextResolver, context CandidateContext, draftText string, usedFacts []string) (RelevantKnowledgeSnapshot, error) {
	return relevantKnowledgeSnapshotForResolverWithSemantic(resolver, context, draftText, usedFacts, nil)
}

func relevantKnowledgeSnapshotForResolverWithSemantic(resolver *CandidateContextResolver, context CandidateContext, draftText string, usedFacts []string, semantic []SafeSemanticSelection) (RelevantKnowledgeSnapshot, error) {
	if resolver == nil {
		return RelevantKnowledgeSnapshot{}, errors.New("candidate knowledge is unavailable")
	}
	candidate, diagnostics, err := resolver.canonicalCandidate()
	if err != nil || len(diagnostics.Conflicts) > 0 {
		return RelevantKnowledgeSnapshot{}, errors.New("candidate knowledge is unavailable")
	}
	snapshot, err := relevantKnowledgeSnapshotFromCandidate(candidate, context, draftText, usedFacts)
	if err != nil {
		return RelevantKnowledgeSnapshot{}, err
	}
	return snapshot.AddSafeSemanticSelections(semantic), nil
}

func buildRelevantKnowledgeSnapshot(candidate Candidate, safe EmployerSafeCandidateKnowledge, context CandidateContext, draftText string, usedFacts []string) (RelevantKnowledgeSnapshot, error) {
	snapshot := RelevantKnowledgeSnapshot{Facts: []RelevantKnowledgeFact{}, ForbiddenClaims: []string{}}
	add := func(fact RelevantKnowledgeFact) { snapshot.Facts = append(snapshot.Facts, fact) }
	atomic := append(append(append([]ResolvedFact{}, context.ResolvedFacts...), context.PartiallyResolvedFacts...), context.UnknownAtomicFacts...)
	atomic = append(atomic, context.RestrictedFacts...)
	for i, fact := range atomic {
		claims := normalizedStringSet(fact.AllowedClaims)
		value, _ := json.Marshal(struct {
			Requested string             `json:"requested"`
			Status    ResolvedFactStatus `json:"status"`
			Value     string             `json:"value,omitempty"`
			Claims    []string           `json:"allowed_claims,omitempty"`
			Missing   string             `json:"missing_part,omitempty"`
		}{normalizeKnowledgeText(fact.RequestedFact), fact.Status, normalizeKnowledgeText(fact.Value), claims, normalizeKnowledgeText(fact.MissingPart)})
		provenance := profileAtomicSnapshotFacts(canonicalCandidateProfile(candidate), fact.Topic)
		var sources []RelevantKnowledgeProvenance
		for _, related := range provenance {
			sources = append(sources, related.Provenance...)
		}
		status := TruthStatusUnknown
		if fact.Status == ResolvedFactAnswerable || fact.Status == ResolvedFactPartiallyAnswerable {
			status = TruthStatusConfirmed
		}
		add(snapshotFact(fmt.Sprintf("atomic.%s.%d", contextCanonical(fact.Topic), i), string(value), status, sources))
	}
	for _, skill := range context.RelevantSkills {
		if detailed, ok := detailedSkillForTopic(EmployerSafeCandidateKnowledge{Skills: safe.Skills}, skill); ok {
			value, _ := json.Marshal(struct {
				Name        string     `json:"name"`
				Level       SkillLevel `json:"level"`
				CanDo       []string   `json:"can_do,omitempty"`
				CannotClaim []string   `json:"cannot_claim,omitempty"`
			}{normalizeKnowledgeText(detailed.Name), detailed.Level, normalizedStringSet(detailed.CanDo), normalizedStringSet(detailed.CannotClaim)})
			add(snapshotFact("skill."+contextCanonical(detailed.ID), string(value), detailed.TruthStatus, detailedProvenance(detailed.KnowledgeMetadata)))
		}
	}
	for _, project := range context.RelevantProjects {
		for _, safeProject := range safe.Projects {
			if contextCanonical(safeProject.Name) != contextCanonical(project.Name) {
				continue
			}
			value, _ := json.Marshal(struct {
				Name         string   `json:"name"`
				Role         string   `json:"role,omitempty"`
				Description  string   `json:"description,omitempty"`
				Technologies []string `json:"technologies,omitempty"`
				Tasks        []string `json:"tasks,omitempty"`
				Results      []string `json:"results,omitempty"`
			}{normalizeKnowledgeText(safeProject.Name), normalizeKnowledgeText(safeProject.Role), normalizeKnowledgeText(safeProject.Description), normalizedStringSet(safeProject.Technologies), normalizedStringSet(safeProject.Tasks), normalizedStringSet(safeProject.Results)})
			add(snapshotFact("project."+contextCanonical(safeProject.ID), string(value), safeProject.TruthStatus, detailedProvenance(safeProject.KnowledgeMetadata)))
		}
	}
	lowerDraft := strings.ToLower(draftText + "\n" + strings.Join(usedFacts, "\n"))
	for _, claim := range context.ForbiddenClaims {
		if strings.TrimSpace(claim) != "" && contextMentions(lowerDraft, claim) {
			snapshot.ForbiddenClaims = append(snapshot.ForbiddenClaims, claim)
		}
	}
	return snapshot.normalized(), nil
}

func canonicalCandidateProfile(candidate Candidate) CandidateProfile {
	profile := CandidateProfile{
		TotalExperienceMonths:            candidate.Profile.TotalExperienceMonths,
		WorkPreferences:                  candidate.Profile.WorkPreferences,
		EmployerCommunicationPreferences: candidate.Profile.Communication,
	}
	if safeMetadata(candidate.Identity.FullNameMetadata) {
		profile.Identity.FullName = ProfileStringFact{Value: candidate.Identity.FullName, ProfileFact: canonicalProfileFact(candidate.Identity.FullNameMetadata)}
	}
	if safeMetadata(candidate.Identity.LocationMetadata) {
		profile.Identity.Location = ProfileStringFact{Value: candidate.Identity.Location, ProfileFact: canonicalProfileFact(candidate.Identity.LocationMetadata)}
	}
	for _, education := range candidate.Education {
		profile.Education = append(profile.Education, EducationFact{Level: education.Level, Institution: education.Institution, Specialty: education.Specialty, Details: education.Details, ProfileFact: canonicalProfileFact(education.Metadata)})
	}
	for _, language := range candidate.Languages {
		profile.Languages = append(profile.Languages, LanguageFact{Name: language.Name, Level: language.Level, ProfileFact: canonicalProfileFact(language.Metadata)})
	}
	return profile
}

func relevantKnowledgeDiff(before, after RelevantKnowledgeSnapshot) []string {
	if RelevantKnowledgeHash(before) == RelevantKnowledgeHash(after) {
		return []string{}
	}
	left, right := before.normalized(), after.normalized()
	result := []string{}
	byKey := map[string]RelevantKnowledgeFact{}
	for _, fact := range left.Facts {
		byKey[fact.Key] = fact
	}
	for _, fact := range right.Facts {
		old, ok := byKey[fact.Key]
		if !ok {
			result = append(result, "added "+fact.Key)
			continue
		}
		if old.NormalizedValue != fact.NormalizedValue || old.TruthStatus != fact.TruthStatus || provenanceJSON(old.Provenance) != provenanceJSON(fact.Provenance) {
			result = append(result, fmt.Sprintf("changed %s: %s/%s → %s/%s", fact.Key, old.NormalizedValue, old.TruthStatus, fact.NormalizedValue, fact.TruthStatus))
		}
		delete(byKey, fact.Key)
	}
	for key := range byKey {
		result = append(result, "removed "+key)
	}
	if strings.Join(left.ForbiddenClaims, "\n") != strings.Join(right.ForbiddenClaims, "\n") {
		result = append(result, "forbidden claims changed")
	}
	sort.Strings(result)
	return result
}

func provenanceJSON(values []RelevantKnowledgeProvenance) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}
