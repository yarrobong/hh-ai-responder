package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type canonicalSkillAssertion struct {
	name        string
	displayName string
	id          string
	detailed    bool
	level       SkillLevel
	negative    bool
	metadata    KnowledgeMetadata
	category    string
	canDo       []string
	cannotClaim []string
	lastUsed    string
	projects    []string
	uses        []CanonicalSkillUse
	claimID     string
}

const canonicalCandidateVersion = 1

// BuildCanonicalCandidate maps already-loaded legacy values only. It has no
// I/O and does not mutate any input. A non-fatal diagnostic is returned when
// source assertions cannot be safely collapsed into one effective value.
func BuildCanonicalCandidate(input CanonicalCandidateInput) (Candidate, CanonicalCandidateDiagnostics, error) {
	var diagnostics CanonicalCandidateDiagnostics
	if err := ValidateCandidateProfile(input.Profile); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical profile input: %w", err)
	}
	if input.ResumeFacts != nil && input.ResumeFacts.TotalExperienceMonthsKnown && input.ResumeFacts.TotalExperienceMonths < 0 {
		return Candidate{}, diagnostics, errors.New("canonical resume facts: negative total experience")
	}
	if ContainsForbiddenSecret([]byte(input.Contacts)) || ContainsForbiddenSecret([]byte(input.GitHubURL)) {
		return Candidate{}, diagnostics, errors.New("canonical config input contains a forbidden secret marker")
	}
	for i, story := range input.Stories {
		if err := story.Validate(); err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("canonical story %d: %w", i+1, err)
		}
	}
	if err := ValidateKnowledgeCollection(input.Knowledge.Skills); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical skills: %w", err)
	}
	if err := ValidateKnowledgeCollection(input.Knowledge.Projects); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical projects: %w", err)
	}
	if err := ValidateKnowledgeCollection(input.Knowledge.Achievements); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical achievements: %w", err)
	}
	if err := ValidateKnowledgeCollection(input.Knowledge.Unknowns); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical unknowns: %w", err)
	}
	for i, proposal := range input.Knowledge.Proposals {
		if err := ValidateKnowledgeProposal(proposal); err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("canonical proposal %d: %w", i+1, err)
		}
	}
	if err := ValidateKnowledgeCollection(input.Knowledge.Events); err != nil {
		return Candidate{}, diagnostics, fmt.Errorf("canonical events: %w", err)
	}

	candidate := Candidate{
		Version:   canonicalCandidateVersion,
		ID:        firstNonEmpty(strings.TrimSpace(input.CandidateID), "candidate-local"),
		UpdatedAt: input.Profile.UpdatedAt,
		Identity: CanonicalCandidateIdentity{
			FullName:         input.Profile.Identity.FullName.Value,
			FullNameMetadata: profileFactMetadata(input.Profile.Identity.FullName.ProfileFact),
			Location:         input.Profile.Identity.Location.Value,
			LocationMetadata: profileFactMetadata(input.Profile.Identity.Location.ProfileFact),
		},
	}
	candidate.Profile = CanonicalProfileSnapshot{TotalExperienceMonths: input.Profile.TotalExperienceMonths, WorkPreferences: input.Profile.WorkPreferences, Communication: input.Profile.EmployerCommunicationPreferences}

	if value := strings.TrimSpace(input.Contacts); value != "" {
		candidate.Contacts = []CanonicalCandidateContact{{ID: canonicalLegacyID("contact", "contacts|"+value), Kind: "contacts", Value: input.Contacts, Metadata: untrustedInputMetadata("config.contacts")}}
	}
	if value := strings.TrimSpace(input.GitHubURL); value != "" {
		candidate.ExternalReferences = []CanonicalExternalReference{{ID: canonicalLegacyID("external", "github|"+value), Kind: "github", URL: input.GitHubURL, Metadata: untrustedInputMetadata("config.github_url")}}
	}

	for _, fact := range input.Profile.Education {
		id := canonicalLegacyID("education", fact)
		claimID := addCanonicalClaim(&candidate, "education", id, "fact", fact, profileFactMetadata(fact.ProfileFact))
		candidate.Education = append(candidate.Education, CanonicalCandidateEducation{ID: id, Level: fact.Level, Institution: fact.Institution, Specialty: fact.Specialty, Details: fact.Details, ClaimID: claimID, Metadata: profileFactMetadata(fact.ProfileFact)})
	}
	for _, fact := range input.Profile.Languages {
		id := canonicalLegacyID("language", fact)
		claimID := addCanonicalClaim(&candidate, "language", id, "fact", fact, profileFactMetadata(fact.ProfileFact))
		candidate.Languages = append(candidate.Languages, CanonicalCandidateLanguage{ID: id, Name: fact.Name, Level: fact.Level, ClaimID: claimID, Metadata: profileFactMetadata(fact.ProfileFact)})
	}
	for _, fact := range input.Profile.WorkExperience {
		id := canonicalLegacyID("experience", fact)
		metadata := profileFactMetadata(fact.ProfileFact)
		claimID := addCanonicalClaim(&candidate, "experience", id, "fact", fact, metadata)
		experience := CanonicalCandidateExperience{ID: id, Company: fact.Company, Position: fact.Role, StartDate: fact.StartDate, EndDate: fact.EndDate, Description: fact.Description, ClaimID: claimID, Metadata: metadata}
		if fact.Achievements != "" {
			experience.Achievements = []string{fact.Achievements}
		}
		candidate.Experience = append(candidate.Experience, experience)
	}

	profileProjectIDs := map[string][]int{}
	for _, fact := range input.Profile.Projects {
		id := canonicalLegacyID("project", fact)
		metadata := profileFactMetadata(fact.ProfileFact)
		claimID := addCanonicalClaim(&candidate, "project", id, "fact", fact, metadata)
		project := CanonicalCandidateProject{ID: id, SourceIDs: []string{id}, Name: fact.Name, Role: fact.Role, Description: fact.Description, Technologies: append([]string{}, fact.Technologies...), Results: nonEmptyStringSlice(fact.BusinessImpact), ClaimIDs: []string{claimID}, Metadata: metadata}
		factCopy := fact
		factCopy.Evidence = append([]string{}, fact.Evidence...)
		project.ProfileSource = &factCopy
		candidate.Projects = append(candidate.Projects, project)
		key := canonicalNaturalKey(fact.Name)
		if key != "" {
			profileProjectIDs[key] = append(profileProjectIDs[key], len(candidate.Projects)-1)
		}
	}

	for _, fact := range input.Knowledge.Projects {
		copy, err := cloneAssemblyValue(fact)
		if err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("clone project %q: %w", fact.ID, err)
		}
		metadata := copy.KnowledgeMetadata
		claimID := addCanonicalClaim(&candidate, "project", copy.ID, "entity", copy, metadata)
		key := canonicalNaturalKey(copy.Name)
		matches := profileProjectIDs[key]
		if key != "" && len(matches) == 1 {
			project := &candidate.Projects[matches[0]]
			project.SourceIDs = appendUniqueString(project.SourceIDs, copy.ID)
			project.ClaimIDs = appendUniqueString(project.ClaimIDs, claimID)
			mergeCanonicalProject(project, copy)
			copyForSource := copy
			project.DetailedSource = &copyForSource
			continue
		}
		if key != "" && len(matches) > 1 {
			diagnostics.addUnique(&diagnostics.Conflicts, "ambiguous project match for "+copy.Name)
			diagnostics.addUnique(&diagnostics.UnresolvedReferences, "project:"+copy.ID)
		}
		candidate.Projects = append(candidate.Projects, canonicalProjectFromKnowledge(copy, claimID))
	}
	for _, achievement := range input.Knowledge.Achievements {
		copy, err := cloneAssemblyValue(achievement)
		if err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("clone achievement %q: %w", achievement.ID, err)
		}
		candidate.Achievements = append(candidate.Achievements, copy)
		addCanonicalClaim(&candidate, "achievement", copy.ID, "entity", copy, copy.KnowledgeMetadata)
	}
	for _, unknown := range input.Knowledge.Unknowns {
		copy, err := cloneAssemblyValue(unknown)
		if err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("clone unknown %q: %w", unknown.ID, err)
		}
		candidate.Unknowns = append(candidate.Unknowns, copy)
	}
	for _, question := range input.Profile.UnknownPendingFacts {
		metadata := profileFactMetadata(ProfileFact{Source: CandidateSourceUnknown, Evidence: []string{question.Reason}})
		candidate.Unknowns = append(candidate.Unknowns, CandidateUnknown{ID: canonicalLegacyID("unknown", question), Question: question.Question, RelatedEntity: question.Topic, Status: CandidateUnknownNeedsConfirmation, KnowledgeMetadata: metadata})
	}
	for _, proposal := range input.Knowledge.Proposals {
		copy, err := cloneAssemblyValue(proposal)
		if err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("clone proposal %q: %w", proposal.ID, err)
		}
		candidate.Proposals = append(candidate.Proposals, copy)
	}
	for _, event := range input.Knowledge.Events {
		copy, err := cloneAssemblyValue(event)
		if err != nil {
			return Candidate{}, diagnostics, fmt.Errorf("clone event %q: %w", event.ID, err)
		}
		candidate.Events = append(candidate.Events, copy)
	}

	mapCanonicalSkills(&candidate, &diagnostics, input.Profile.Skills, input.Knowledge.Skills)
	mapCanonicalPreferences(&candidate, &diagnostics, input.Profile)
	mapCanonicalStories(&candidate, &diagnostics, input.Stories)
	if input.ResumeFacts != nil {
		copy := *input.ResumeFacts
		candidate.ResumeFacts = &copy
	}
	sortCanonicalCandidate(&candidate)
	return candidate, diagnostics, nil
}

func sortCanonicalCandidate(candidate *Candidate) {
	sort.SliceStable(candidate.Education, func(i, j int) bool { return candidate.Education[i].ID < candidate.Education[j].ID })
	sort.SliceStable(candidate.Languages, func(i, j int) bool { return candidate.Languages[i].ID < candidate.Languages[j].ID })
	sort.SliceStable(candidate.Experience, func(i, j int) bool { return candidate.Experience[i].ID < candidate.Experience[j].ID })
	sort.SliceStable(candidate.Skills, func(i, j int) bool { return candidate.Skills[i].ID < candidate.Skills[j].ID })
	sort.SliceStable(candidate.Projects, func(i, j int) bool { return candidate.Projects[i].ID < candidate.Projects[j].ID })
	sort.SliceStable(candidate.Achievements, func(i, j int) bool { return candidate.Achievements[i].ID < candidate.Achievements[j].ID })
	sort.SliceStable(candidate.Preferences, func(i, j int) bool { return candidate.Preferences[i].ID < candidate.Preferences[j].ID })
	sort.SliceStable(candidate.Constraints, func(i, j int) bool { return candidate.Constraints[i].ID < candidate.Constraints[j].ID })
	sort.SliceStable(candidate.Unknowns, func(i, j int) bool { return candidate.Unknowns[i].ID < candidate.Unknowns[j].ID })
	sort.SliceStable(candidate.Proposals, func(i, j int) bool { return candidate.Proposals[i].ID < candidate.Proposals[j].ID })
	sort.SliceStable(candidate.Events, func(i, j int) bool { return candidate.Events[i].ID < candidate.Events[j].ID })
	sort.SliceStable(candidate.Claims, func(i, j int) bool { return candidate.Claims[i].ID < candidate.Claims[j].ID })
	for i := range candidate.Skills {
		sort.Strings(candidate.Skills[i].SourceIDs)
		sort.Strings(candidate.Skills[i].ClaimIDs)
		sort.SliceStable(candidate.Skills[i].Capabilities, func(a, b int) bool {
			return candidate.Skills[i].Capabilities[a].ID < candidate.Skills[i].Capabilities[b].ID
		})
		sort.SliceStable(candidate.Skills[i].Uses, func(a, b int) bool { return candidate.Skills[i].Uses[a].ID < candidate.Skills[i].Uses[b].ID })
	}
	for i := range candidate.Projects {
		sort.Strings(candidate.Projects[i].SourceIDs)
		sort.Strings(candidate.Projects[i].ClaimIDs)
		sort.Strings(candidate.Projects[i].StoryIDs)
	}
}

func mapCanonicalSkills(candidate *Candidate, diagnostics *CanonicalCandidateDiagnostics, profile []CandidateSkill, detailed []CandidateSkillDetailed) {
	byName := map[string][]canonicalSkillAssertion{}
	add := func(a canonicalSkillAssertion) {
		key := canonicalNaturalKey(a.name)
		if key != "" {
			byName[key] = append(byName[key], a)
		}
	}
	for _, skill := range profile {
		metadata := profileFactMetadata(skill.ProfileFact)
		id := canonicalLegacyID("skill", skill)
		claimID := addCanonicalClaim(candidate, "skill", id, "fact", skill, metadata)
		add(canonicalSkillAssertion{name: CanonicalSkillName(skill.Name), displayName: skill.Name, id: id, level: skill.Level, negative: skill.Negative, metadata: metadata, claimID: claimID})
	}
	for _, skill := range detailed {
		metadata := skill.KnowledgeMetadata
		claimID := addCanonicalClaim(candidate, "skill", skill.ID, "entity", skill, metadata)
		add(canonicalSkillAssertion{name: CanonicalSkillName(skill.Name), displayName: skill.Name, id: skill.ID, detailed: true, level: skill.Level, negative: skill.Negative, metadata: metadata, category: skill.Category, canDo: append([]string{}, skill.CanDo...), cannotClaim: append([]string{}, skill.CannotClaim...), lastUsed: skill.LastUsed, projects: append([]string{}, skill.Projects...), uses: cloneSkillUses(skill.Uses), claimID: claimID})
	}
	keys := make([]string, 0, len(byName))
	for key := range byName {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		assertions := byName[key]
		sort.SliceStable(assertions, func(i, j int) bool { return assertions[i].id < assertions[j].id })
		name := assertions[0].name
		ids, claimIDs := []string{}, []string{}
		for _, a := range assertions {
			ids = appendUniqueString(ids, a.id)
			claimIDs = appendUniqueString(claimIDs, a.claimID)
		}
		canonicalID := firstDetailedSkillID(assertions)
		if canonicalID == "" || len(detailedSkillIDs(assertions)) > 1 {
			canonicalID = canonicalLegacyID("canonical-skill", key)
		}
		positive, negative := false, false
		for _, a := range assertions {
			if a.negative {
				negative = true
			} else {
				positive = true
			}
		}
		if positive && negative {
			diagnostics.addUnique(&diagnostics.Conflicts, "positive and negative skill assertions for "+name)
			markCanonicalClaimsDisputed(candidate, claimIDs)
		}
		chosen := chooseSkillAssertion(assertions)
		state := CanonicalClaimActive
		if positive && negative {
			state = CanonicalClaimDisputed
		}
		if conflictingTrustedLevels(assertions) {
			diagnostics.addUnique(&diagnostics.Conflicts, "conflicting trusted skill levels for "+name)
			markCanonicalClaimsDisputed(candidate, claimIDs)
			state = CanonicalClaimDisputed
		}
		if hasConfirmedNegative(assertions) {
			chosen.negative = true
			chosen.level = SkillLevelUnknown
		}
		canonical := CanonicalCandidateSkill{ID: canonicalID, SourceIDs: ids, Name: name, DisplayName: name, Level: chosen.level, Category: firstNonEmptyAssertionCategory(assertions), CannotClaim: uniqueStringsFromAssertions(assertions, func(a canonicalSkillAssertion) []string { return a.cannotClaim }), LastUsed: firstNonEmptyAssertionLastUsed(assertions), ClaimIDs: claimIDs, Negative: chosen.negative, State: state, Metadata: chosen.metadata}
		canonical.DisplayName = firstNonEmptyAssertionDisplayName(assertions)
		for _, assertion := range assertions {
			canonical.SourceAssertions = append(canonical.SourceAssertions, CanonicalCandidateSkillAssertion{ID: assertion.id, Name: assertion.displayName, Detailed: assertion.detailed, Category: assertion.category, Level: assertion.level, Projects: append([]string{}, assertion.projects...), Uses: cloneSkillUses(assertion.uses), CanDo: append([]string{}, assertion.canDo...), CannotClaim: append([]string{}, assertion.cannotClaim...), LastUsed: assertion.lastUsed, Negative: assertion.negative, Metadata: assertion.metadata})
		}
		for _, a := range assertions {
			canonical.Uses = append(canonical.Uses, a.uses...)
			for _, capability := range a.canDo {
				canonical.Capabilities = append(canonical.Capabilities, CanonicalSkillCapability{ID: canonicalLegacyID("capability", a.id+"|"+capability), Text: capability, ClaimID: a.claimID})
			}
			for _, projectID := range a.projects {
				canonical.Uses = append(canonical.Uses, CanonicalSkillUse{ID: canonicalLegacyID("skill-use", a.id+"|"+projectID), SkillID: canonicalID, ProjectID: projectID, Context: contextForAssertion(a), Evidence: append([]string{}, a.metadata.Evidence...), ClaimID: a.claimID})
			}
		}
		canonical.Capabilities = uniqueCapabilities(canonical.Capabilities)
		canonical.Uses = uniqueSkillUses(canonical.Uses)
		candidate.Skills = append(candidate.Skills, canonical)
	}
}

func mapCanonicalPreferences(candidate *Candidate, diagnostics *CanonicalCandidateDiagnostics, profile CandidateProfile) {
	addPreference := func(kind, value string, fact ProfileFact) {
		if value == "" {
			return
		}
		metadata := profileFactMetadata(fact)
		id := canonicalLegacyID("preference", kind+"|"+value)
		claimID := addCanonicalClaim(candidate, "preference", id, kind, value, metadata)
		candidate.Preferences = append(candidate.Preferences, CanonicalCandidatePreference{ID: id, Kind: kind, Value: value, ClaimID: claimID, Metadata: metadata})
	}
	addPreference("preferred_roles", profile.WorkPreferences.PreferredRoles.Value, profile.WorkPreferences.PreferredRoles.ProfileFact)
	addPreference("primary_roles", profile.WorkPreferences.PrimaryRoles.Value, profile.WorkPreferences.PrimaryRoles.ProfileFact)
	addPreference("secondary_roles", profile.WorkPreferences.SecondaryRoles.Value, profile.WorkPreferences.SecondaryRoles.ProfileFact)
	addPreference("work_mode", profile.WorkPreferences.WorkMode.Value, profile.WorkPreferences.WorkMode.ProfileFact)
	addPreference("relocation", profile.WorkPreferences.Relocation.Value, profile.WorkPreferences.Relocation.ProfileFact)
	addPreference("business_trips", profile.WorkPreferences.BusinessTrips.Value, profile.WorkPreferences.BusinessTrips.ProfileFact)
	if profile.WorkPreferences.SalaryMinimum.Value != 0 {
		addPreference("salary_minimum", fmt.Sprint(profile.WorkPreferences.SalaryMinimum.Value), profile.WorkPreferences.SalaryMinimum.ProfileFact)
		diagnostics.addUnique(&diagnostics.LossyMappings, "salary_minimum preserved as preference; mandatory constraint is not proven")
	}
	addPreference("salary_communication", profile.EmployerCommunicationPreferences.Salary.Value, profile.EmployerCommunicationPreferences.Salary.ProfileFact)
	addPreference("interview_communication", profile.EmployerCommunicationPreferences.Interview.Value, profile.EmployerCommunicationPreferences.Interview.ProfileFact)
	addPreference("documents_communication", profile.EmployerCommunicationPreferences.Documents.Value, profile.EmployerCommunicationPreferences.Documents.ProfileFact)
	addPreference("other_communication", profile.EmployerCommunicationPreferences.OtherPreferences.Value, profile.EmployerCommunicationPreferences.OtherPreferences.ProfileFact)
	for _, value := range profile.EmployerCommunicationPreferences.AlwaysEmphasize.Values {
		addPreference("always_emphasize", value, profile.EmployerCommunicationPreferences.AlwaysEmphasize.ProfileFact)
	}
	for _, value := range profile.EmployerCommunicationPreferences.AvoidClaiming.Values {
		if value == "" {
			continue
		}
		metadata := profileFactMetadata(profile.EmployerCommunicationPreferences.AvoidClaiming.ProfileFact)
		id := canonicalLegacyID("constraint", "communication_avoid_claiming|"+value)
		claimID := addCanonicalClaim(candidate, "constraint", id, "communication_avoid_claiming", value, metadata)
		candidate.Constraints = append(candidate.Constraints, CanonicalCandidateConstraint{ID: id, Kind: "communication_avoid_claiming", Value: value, ClaimID: claimID, Metadata: metadata})
	}
}

func markCanonicalClaimsDisputed(candidate *Candidate, claimIDs []string) {
	for i := range candidate.Claims {
		for _, id := range claimIDs {
			if candidate.Claims[i].ID == id {
				candidate.Claims[i].State = CanonicalClaimDisputed
			}
		}
	}
}

func mapCanonicalStories(candidate *Candidate, diagnostics *CanonicalCandidateDiagnostics, stories []CandidateStory) {
	for _, story := range stories {
		copy := CanonicalCandidateStory{ID: story.ID, Title: story.Title, Situation: story.Situation, Context: story.Context, Summary: story.Summary, Description: story.Description, Story: story.Story, Task: story.Task, Problem: story.Problem, Action: story.Action, Actions: story.Actions, Contribution: story.Contribution, Result: story.Result, Outcome: story.Outcome, Achievement: story.Achievement, Achievements: story.Achievements, Technologies: append([]string{}, story.Technologies...), Skills: append([]string{}, story.Skills...), Keywords: append([]string{}, story.Keywords...), Tags: append([]string{}, story.Tags...), Roles: append([]string{}, story.Roles...), Relevance: append([]string{}, story.Relevance...), RelevantFor: append([]string{}, story.RelevantFor...), RelevantRoles: append([]string{}, story.RelevantRoles...), ProfileRefs: append([]string{}, story.ProfileRefs...)}
		if copy.ID == "" {
			copy.ID = canonicalLegacyID("story", story)
		}
		candidate.Stories = append(candidate.Stories, copy)
		linked := false
		for _, ref := range story.ProfileRefs {
			for i := range candidate.Experience {
				if candidate.Experience[i].ID == ref {
					candidate.Experience[i].StoryIDs = appendUniqueString(candidate.Experience[i].StoryIDs, copy.ID)
					linked = true
				}
			}
			for i := range candidate.Projects {
				if candidate.Projects[i].ID == ref {
					candidate.Projects[i].StoryIDs = appendUniqueString(candidate.Projects[i].StoryIDs, copy.ID)
					linked = true
				}
			}
			if !linked {
				diagnostics.addUnique(&diagnostics.UnsupportedStoryRefs, ref)
			}
		}
	}
}

func addCanonicalClaim(candidate *Candidate, subjectType, subjectID, field string, value interface{}, metadata KnowledgeMetadata) string {
	raw, _ := json.Marshal(value)
	claimID := canonicalLegacyID("claim", subjectType+"|"+subjectID+"|"+field+"|"+string(raw))
	polarity := CanonicalClaimPositive
	if negativeValue(value) {
		polarity = CanonicalClaimNegative
	}
	candidate.Claims = append(candidate.Claims, CanonicalCandidateClaim{ID: claimID, SubjectType: subjectType, SubjectID: subjectID, Field: field, Value: string(raw), Polarity: polarity, State: CanonicalClaimActive, Metadata: metadata})
	return claimID
}

func profileFactMetadata(fact ProfileFact) KnowledgeMetadata {
	source := mapCandidateSource(fact.Source)
	status := TruthStatusUnknown
	confirmedAt := (*time.Time)(nil)
	if fact.Confirmed {
		switch fact.Source {
		case CandidateSourceUserConfirmed:
			status = TruthStatusConfirmed
			value := fact.ConfirmedAt
			confirmedAt = &value
		case CandidateSourceHHResume, CandidateSourceGithubVerified:
			status = TruthStatusVerified
		default:
			status = TruthStatusHypothesis
		}
	} else if fact.Source == CandidateSourceDerived {
		status = TruthStatusHypothesis
	}
	observedAt := (*time.Time)(nil)
	if !fact.ConfirmedAt.IsZero() {
		value := fact.ConfirmedAt
		observedAt = &value
	}
	return KnowledgeMetadata{TruthStatus: status, Sources: []KnowledgeSourceRecord{{Type: source, Evidence: append([]string{}, fact.Evidence...), ObservedAt: observedAt}}, Evidence: append([]string{}, fact.Evidence...), ConfirmedAt: confirmedAt}
}

func untrustedInputMetadata(evidence string) KnowledgeMetadata {
	return KnowledgeMetadata{TruthStatus: TruthStatusUnknown, Sources: []KnowledgeSourceRecord{{Type: KnowledgeSourceUnknown, Evidence: []string{evidence}}}, Evidence: []string{evidence}}
}

func mapCandidateSource(source CandidateSource) KnowledgeSource {
	switch source {
	case CandidateSourceUserConfirmed:
		return KnowledgeSourceUserConfirmed
	case CandidateSourceHHResume:
		return KnowledgeSourceHHResume
	case CandidateSourceGithubVerified:
		return KnowledgeSourceGithubVerified
	case CandidateSourceDerived:
		return KnowledgeSourceDerived
	default:
		return KnowledgeSourceUnknown
	}
}

func canonicalLegacyID(kind string, value interface{}) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(append([]byte(kind+":"), raw...))
	return kind + "-" + hex.EncodeToString(digest[:8])
}

func canonicalNaturalKey(value string) string {
	return strings.ToLower(strings.TrimSpace(CanonicalSkillName(value)))
}

func nonEmptyStringSlice(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func AppendUniqueString(values []string, value string) []string {
	return appendUniqueString(values, value)
}

func canonicalProjectFromKnowledge(project CandidateProject, claimID string) CanonicalCandidateProject {
	copy := project
	return CanonicalCandidateProject{ID: project.ID, SourceIDs: []string{project.ID}, Name: project.Name, Type: project.Type, Role: project.Role, Period: project.Period, Description: project.Description, Technologies: append([]string{}, project.Technologies...), Tasks: append([]string{}, project.Tasks...), Results: append([]string{}, project.Results...), RelatedSkills: append([]string{}, project.RelatedSkills...), ClaimIDs: []string{claimID}, Metadata: project.KnowledgeMetadata, DetailedSource: &copy}
}

func mergeCanonicalProject(target *CanonicalCandidateProject, source CandidateProject) {
	target.Name = firstNonEmpty(target.Name, source.Name)
	target.Role = firstNonEmpty(target.Role, source.Role)
	target.Description = firstNonEmpty(target.Description, source.Description)
	target.Type = firstProjectType(target.Type, source.Type)
	if target.Period.Start == "" {
		target.Period.Start = source.Period.Start
	}
	if target.Period.End == "" {
		target.Period.End = source.Period.End
	}
	target.Technologies = appendUniqueStrings(target.Technologies, source.Technologies...)
	target.Tasks = appendUniqueStrings(target.Tasks, source.Tasks...)
	target.Results = appendUniqueStrings(target.Results, source.Results...)
	target.RelatedSkills = appendUniqueStrings(target.RelatedSkills, source.RelatedSkills...)
	if source.KnowledgeMetadata.TruthStatus == TruthStatusConfirmed || source.KnowledgeMetadata.TruthStatus == TruthStatusVerified {
		target.Metadata = source.KnowledgeMetadata
	}
	target.SourceIDs = appendUniqueString(target.SourceIDs, source.ID)
}

func firstProjectType(a, b CandidateProjectType) CandidateProjectType {
	if a != "" {
		return a
	}
	return b
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, value := range additions {
		if value != "" {
			values = appendUniqueString(values, value)
		}
	}
	return values
}

func firstDetailedSkillID(assertions []canonicalSkillAssertion) string {
	ids := detailedSkillIDs(assertions)
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

func detailedSkillIDs(assertions []canonicalSkillAssertion) []string {
	result := []string{}
	for _, value := range assertions {
		if value.detailed {
			result = appendUniqueString(result, value.id)
		}
	}
	return result
}

func chooseSkillAssertion(assertions []canonicalSkillAssertion) canonicalSkillAssertion {
	chosen := assertions[0]
	for _, value := range assertions[1:] {
		if skillAssertionRank(value) > skillAssertionRank(chosen) || skillAssertionRank(value) == skillAssertionRank(chosen) && value.id < chosen.id {
			chosen = value
		}
	}
	return chosen
}

func skillAssertionRank(assertion canonicalSkillAssertion) int {
	switch assertion.metadata.TruthStatus {
	case TruthStatusConfirmed:
		return 3
	case TruthStatusVerified:
		return 2
	case TruthStatusHypothesis:
		return 1
	default:
		return 0
	}
}

func firstNonEmptyAssertionCategory(assertions []canonicalSkillAssertion) string {
	for _, value := range assertions {
		if value.category != "" {
			return value.category
		}
	}
	return ""
}

func firstNonEmptyAssertionDisplayName(assertions []canonicalSkillAssertion) string {
	for _, value := range assertions {
		if value.displayName != "" {
			return value.displayName
		}
	}
	return ""
}

func firstNonEmptyAssertionLastUsed(assertions []canonicalSkillAssertion) string {
	for _, value := range assertions {
		if value.lastUsed != "" {
			return value.lastUsed
		}
	}
	return ""
}

func uniqueStringsFromAssertions(assertions []canonicalSkillAssertion, get func(canonicalSkillAssertion) []string) []string {
	result := []string{}
	for _, assertion := range assertions {
		for _, value := range get(assertion) {
			result = appendUniqueString(result, value)
		}
	}
	return result
}

func uniqueCapabilities(values []CanonicalSkillCapability) []CanonicalSkillCapability {
	result := []CanonicalSkillCapability{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value.ID] {
			seen[value.ID] = true
			result = append(result, value)
		}
	}
	return result
}

func uniqueSkillUses(values []CanonicalSkillUse) []CanonicalSkillUse {
	result := []CanonicalSkillUse{}
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value.ID] {
			seen[value.ID] = true
			result = append(result, value)
		}
	}
	return result
}

func cloneSkillUses(values []CanonicalSkillUse) []CanonicalSkillUse {
	if values == nil {
		return nil
	}
	return append([]CanonicalSkillUse{}, values...)
}

func contextForAssertion(assertion canonicalSkillAssertion) CanonicalSkillUsageContext {
	if assertion.negative {
		return CanonicalSkillUsageExplicitlyNotUsed
	}
	return CanonicalSkillUsageUnknown
}

func hasConfirmedNegative(assertions []canonicalSkillAssertion) bool {
	for _, assertion := range assertions {
		if assertion.negative && assertion.metadata.TruthStatus == TruthStatusConfirmed {
			return true
		}
	}
	return false
}

func conflictingTrustedLevels(assertions []canonicalSkillAssertion) bool {
	hasDetailed := false
	for _, assertion := range assertions {
		if assertion.detailed {
			hasDetailed = true
			break
		}
	}
	var level SkillLevel
	rank := -1
	for _, assertion := range assertions {
		if hasDetailed && !assertion.detailed {
			continue
		}
		currentRank := skillAssertionRank(assertion)
		if currentRank < 2 || assertion.negative || assertion.level == SkillLevelUnknown {
			continue
		}
		if rank == -1 {
			level, rank = assertion.level, currentRank
			continue
		}
		if currentRank == rank && assertion.level != level {
			return true
		}
	}
	return false
}

func negativeValue(value interface{}) bool {
	switch typed := value.(type) {
	case CandidateSkill:
		return typed.Negative
	case CandidateSkillDetailed:
		return typed.Negative
	}
	return false
}

func cloneAssemblyValue[T any](value T) (T, error) {
	var copy T
	raw, err := json.Marshal(value)
	if err != nil {
		return copy, err
	}
	if err := json.Unmarshal(raw, &copy); err != nil {
		return copy, err
	}
	return copy, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func SortCanonicalCandidate(value *Candidate) { sortCanonicalCandidate(value) }

func CanonicalProjectFromKnowledge(value CandidateProject, claimID string) CanonicalCandidateProject {
	return canonicalProjectFromKnowledge(value, claimID)
}
