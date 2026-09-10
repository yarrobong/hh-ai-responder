package runtime

import (
	"errors"
	"fmt"
	"strings"

	domaincandidate "hh-ai-responder/internal/candidate"
)

// EmployerSafeProfileKnowledge is the trusted, affirmative part of the
// legacy profile. UnknownPendingFacts and derived values are intentionally not
// projected here. Detailed KB entities remain in their original files; this
// is a read-only view over both sources, not a duplicate store.
type EmployerSafeProfileKnowledge = domaincandidate.EmployerSafeProfileKnowledge

// EmployerSafeCandidateKnowledge is the unified read-only employer-safe
// projection used by CandidateContextResolver.
type EmployerSafeCandidateKnowledge = domaincandidate.EmployerSafeCandidateKnowledge

// GetEmployerSafeCandidateKnowledge validates and detaches every trusted
// source before returning it. It does not read stories, proposals, unknowns,
// or unconfirmed profile facts.
func (kb *CandidateKnowledgeBase) GetEmployerSafeCandidateKnowledge() (EmployerSafeCandidateKnowledge, error) {
	if kb == nil {
		return EmployerSafeCandidateKnowledge{}, errors.New("candidate knowledge is nil")
	}
	if err := validateCandidateProfile(kb.Profile); err != nil {
		return EmployerSafeCandidateKnowledge{}, fmt.Errorf("invalid candidate profile: %w", err)
	}
	detailed, err := kb.GetEmployerSafeKnowledge()
	if err != nil {
		return EmployerSafeCandidateKnowledge{}, err
	}
	view := EmployerSafeCandidateKnowledge{Skills: detailed.Skills, Projects: detailed.Projects, Achievements: detailed.Achievements}
	profile := EmployerSafeProfileKnowledge{}
	for _, fact := range kb.Profile.Education {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			profile.Education = append(profile.Education, fact)
		}
	}
	for _, fact := range kb.Profile.WorkExperience {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			profile.WorkExperience = append(profile.WorkExperience, fact)
		}
	}
	for _, fact := range kb.Profile.Projects {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			profile.Projects = append(profile.Projects, fact)
		}
	}
	for _, fact := range kb.Profile.Skills {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			profile.Skills = append(profile.Skills, fact)
		}
	}
	for _, fact := range kb.Profile.Languages {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			profile.Languages = append(profile.Languages, fact)
		}
	}
	if domaincandidate.CanExposeProfileFact(kb.Profile.TotalExperienceMonths.ProfileFact) {
		value := kb.Profile.TotalExperienceMonths.Value
		profile.TotalExperienceMonths = &value
	}
	if domaincandidate.CanExposeProfileFact(kb.Profile.WorkPreferences.SalaryMinimum.ProfileFact) {
		value := kb.Profile.WorkPreferences.SalaryMinimum.Value
		profile.SalaryMinimum = &value
	}
	copyString := func(fact ProfileStringFact) string {
		if domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
			return strings.TrimSpace(fact.Value)
		}
		return ""
	}
	profile.SalaryPreference = copyString(kb.Profile.EmployerCommunicationPreferences.Salary)
	profile.Relocation = copyString(kb.Profile.WorkPreferences.Relocation)
	profile.WorkMode = copyString(kb.Profile.WorkPreferences.WorkMode)
	profile.BusinessTrips = copyString(kb.Profile.WorkPreferences.BusinessTrips)
	profile.PrimaryRoles = copyString(kb.Profile.WorkPreferences.PrimaryRoles)
	profile.SecondaryRoles = copyString(kb.Profile.WorkPreferences.SecondaryRoles)
	profile.PreferredRoles = copyString(kb.Profile.WorkPreferences.PreferredRoles)
	if fact := kb.Profile.EmployerCommunicationPreferences.AlwaysEmphasize; domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
		profile.AlwaysEmphasize = append([]string{}, fact.Values...)
	}
	if fact := kb.Profile.EmployerCommunicationPreferences.AvoidClaiming; domaincandidate.CanExposeProfileFact(fact.ProfileFact) {
		profile.AvoidClaiming = append([]string{}, fact.Values...)
	}
	view.Profile = profile
	return cloneKnowledge(view)
}

// EmployerSafeCandidateView is a shorter compatibility name for callers that
// refer to the projection as a view rather than a knowledge object.
func (kb *CandidateKnowledgeBase) EmployerSafeCandidateView() (EmployerSafeCandidateKnowledge, error) {
	return kb.GetEmployerSafeCandidateKnowledge()
}

type CandidateKnowledgeSyncResult struct {
	MigratedSkills   int
	MigratedProjects int
	Skipped          int
}

// SyncProfileKnowledge performs the safe legacy-to-detail synchronization in
// memory. MigrateLegacyProfile is idempotent, preserves enriched records, and
// writes an audit event for each newly copied entity. It never infers
// capabilities, project periods, achievements, or facts from stories.
func (kb *CandidateKnowledgeBase) SyncProfileKnowledge() (CandidateKnowledgeSyncResult, error) {
	if kb == nil {
		return CandidateKnowledgeSyncResult{}, errors.New("candidate knowledge is nil")
	}
	if err := validateCandidateProfile(kb.Profile); err != nil {
		return CandidateKnowledgeSyncResult{}, fmt.Errorf("validate profile sync: %w", err)
	}
	beforeSkills, beforeProjects := len(kb.Skills), len(kb.Projects)
	if err := kb.MigrateLegacyProfile(); err != nil {
		return CandidateKnowledgeSyncResult{}, err
	}
	result := CandidateKnowledgeSyncResult{
		MigratedSkills:   len(kb.Skills) - beforeSkills,
		MigratedProjects: len(kb.Projects) - beforeProjects,
	}
	result.Skipped = len(kb.Profile.Skills) + len(kb.Profile.Projects) - result.MigratedSkills - result.MigratedProjects
	if result.Skipped < 0 {
		result.Skipped = 0
	}
	return result, nil
}
