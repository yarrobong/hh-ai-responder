package candidate

import (
	"errors"
	"strings"
)

type EmployerSafeProfileKnowledge struct {
	Education             []EducationFact      `json:"education,omitempty"`
	WorkExperience        []WorkExperienceFact `json:"work_experience,omitempty"`
	TotalExperienceMonths *int                 `json:"total_experience_months,omitempty"`
	Projects              []ProjectFact        `json:"projects,omitempty"`
	Skills                []CandidateSkill     `json:"skills,omitempty"`
	Languages             []LanguageFact       `json:"languages,omitempty"`
	SalaryMinimum         *int                 `json:"salary_minimum,omitempty"`
	SalaryPreference      string               `json:"salary_preference,omitempty"`
	Relocation            string               `json:"relocation,omitempty"`
	WorkMode              string               `json:"work_mode,omitempty"`
	BusinessTrips         string               `json:"business_trips,omitempty"`
	PrimaryRoles          string               `json:"primary_roles,omitempty"`
	SecondaryRoles        string               `json:"secondary_roles,omitempty"`
	PreferredRoles        string               `json:"preferred_roles,omitempty"`
	AlwaysEmphasize       []string             `json:"always_emphasize,omitempty"`
	AvoidClaiming         []string             `json:"avoid_claiming,omitempty"`
}
type EmployerSafeCandidateKnowledge struct {
	Skills       []CandidateSkillDetailed     `json:"skills"`
	Projects     []CandidateProject           `json:"projects"`
	Achievements []CandidateAchievement       `json:"achievements"`
	Profile      EmployerSafeProfileKnowledge `json:"profile"`
}

func CanExposeToEmployer(metadata KnowledgeMetadata) bool {
	return metadata.TruthStatus == TruthStatusConfirmed || metadata.TruthStatus == TruthStatusVerified
}
func CanExposeProfileFact(fact ProfileFact) bool {
	return fact.Confirmed && SourceTrustedForEmployerCommunication(fact.Source)
}
func SafeKnowledgeMetadata(metadata KnowledgeMetadata) bool { return CanExposeToEmployer(metadata) }

// CanonicalEmployerSafeProjection is pure: it validates no external state and
// returns only confirmed/verified canonical knowledge.
func CanonicalEmployerSafeProjection(value Candidate) (EmployerSafeCandidateKnowledge, error) {
	view := EmployerSafeCandidateKnowledge{Skills: []CandidateSkillDetailed{}, Projects: []CandidateProject{}, Achievements: []CandidateAchievement{}}
	for _, skill := range value.Skills {
		if skill.State == CanonicalClaimDisputed {
			return EmployerSafeCandidateKnowledge{}, errors.New("canonical projection: disputed skill")
		}
		if !CanExposeToEmployer(skill.Metadata) {
			continue
		}
		for _, assertion := range skill.SourceAssertions {
			if assertion.Detailed && CanExposeToEmployer(assertion.Metadata) {
				view.Skills = append(view.Skills, CandidateSkillDetailed{ID: assertion.ID, Name: assertion.Name, Category: assertion.Category, Level: assertion.Level, Projects: append([]string{}, assertion.Projects...), Uses: assertion.Uses, CanDo: append([]string{}, assertion.CanDo...), CannotClaim: append([]string{}, assertion.CannotClaim...), LastUsed: assertion.LastUsed, Negative: assertion.Negative, KnowledgeMetadata: assertion.Metadata})
			}
		}
		if len(skill.SourceAssertions) == 0 && CanExposeToEmployer(skill.Metadata) {
			projects := make([]string, 0, len(skill.Uses))
			capabilities := make([]string, 0, len(skill.Capabilities))
			for _, use := range skill.Uses {
				if use.ProjectID != "" && !containsString(projects, use.ProjectID) {
					projects = append(projects, use.ProjectID)
				}
			}
			for _, capability := range skill.Capabilities {
				if capability.Text != "" && !containsString(capabilities, capability.Text) {
					capabilities = append(capabilities, capability.Text)
				}
			}
			view.Skills = append(view.Skills, CandidateSkillDetailed{ID: skill.ID, Name: skill.DisplayName, Category: skill.Category, Level: skill.Level, Projects: projects, Uses: append([]CanonicalSkillUse{}, skill.Uses...), CanDo: capabilities, CannotClaim: append([]string{}, skill.CannotClaim...), LastUsed: skill.LastUsed, Negative: skill.Negative, KnowledgeMetadata: skill.Metadata})
		}
	}
	for _, project := range value.Projects {
		if project.DetailedSource != nil && CanExposeToEmployer(project.DetailedSource.KnowledgeMetadata) {
			view.Projects = append(view.Projects, *project.DetailedSource)
		}
	}
	for _, achievement := range value.Achievements {
		if CanExposeToEmployer(achievement.KnowledgeMetadata) {
			view.Achievements = append(view.Achievements, achievement)
		}
	}
	view.Profile = safeProfile(value)
	return view, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (p CanonicalCandidateProject) DetailedSourceOrValue() CandidateProject {
	if p.DetailedSource != nil {
		return *p.DetailedSource
	}
	return CandidateProject{ID: p.ID, Name: p.Name, Type: p.Type, Role: p.Role, Period: p.Period, Description: p.Description, Technologies: append([]string{}, p.Technologies...), Tasks: append([]string{}, p.Tasks...), Results: append([]string{}, p.Results...), RelatedSkills: append([]string{}, p.RelatedSkills...), KnowledgeMetadata: p.Metadata}
}

func safeProfile(value Candidate) EmployerSafeProfileKnowledge {
	profile := EmployerSafeProfileKnowledge{}
	for _, item := range value.Education {
		if CanExposeToEmployer(item.Metadata) {
			profile.Education = append(profile.Education, EducationFact{Level: item.Level, Institution: item.Institution, Specialty: item.Specialty, Details: item.Details, ProfileFact: profileFact(item.Metadata)})
		}
	}
	for _, item := range value.Experience {
		if CanExposeToEmployer(item.Metadata) {
			achievement := ""
			if len(item.Achievements) > 0 {
				achievement = item.Achievements[0]
			}
			profile.WorkExperience = append(profile.WorkExperience, WorkExperienceFact{Company: item.Company, Role: item.Position, Description: item.Description, Achievements: achievement, StartDate: item.StartDate, EndDate: item.EndDate, ProfileFact: profileFact(item.Metadata)})
		}
	}
	for _, item := range value.Languages {
		if CanExposeToEmployer(item.Metadata) {
			profile.Languages = append(profile.Languages, LanguageFact{Name: item.Name, Level: item.Level, ProfileFact: profileFact(item.Metadata)})
		}
	}
	for _, item := range value.Projects {
		if item.ProfileSource != nil && CanExposeProfileFact(item.ProfileSource.ProfileFact) {
			profile.Projects = append(profile.Projects, *item.ProfileSource)
		}
	}
	for _, item := range value.Skills {
		if len(item.SourceAssertions) == 0 && CanExposeToEmployer(item.Metadata) {
			profile.Skills = append(profile.Skills, CandidateSkill{Name: item.DisplayName, Level: item.Level, Negative: item.Negative, ProfileFact: profileFact(item.Metadata)})
		}
		for _, assertion := range item.SourceAssertions {
			if !assertion.Detailed && CanExposeToEmployer(assertion.Metadata) {
				profile.Skills = append(profile.Skills, CandidateSkill{Name: assertion.Name, Level: assertion.Level, Negative: assertion.Negative, ProfileFact: profileFact(assertion.Metadata)})
			}
		}
	}
	if CanExposeProfileFact(value.Profile.TotalExperienceMonths.ProfileFact) {
		n := value.Profile.TotalExperienceMonths.Value
		profile.TotalExperienceMonths = &n
	}
	if CanExposeProfileFact(value.Profile.WorkPreferences.SalaryMinimum.ProfileFact) {
		n := value.Profile.WorkPreferences.SalaryMinimum.Value
		profile.SalaryMinimum = &n
	}
	copyString := func(f ProfileStringFact) string {
		if CanExposeProfileFact(f.ProfileFact) {
			return strings.TrimSpace(f.Value)
		}
		return ""
	}
	profile.SalaryPreference = copyString(value.Profile.Communication.Salary)
	profile.Relocation = copyString(value.Profile.WorkPreferences.Relocation)
	profile.WorkMode = copyString(value.Profile.WorkPreferences.WorkMode)
	profile.BusinessTrips = copyString(value.Profile.WorkPreferences.BusinessTrips)
	profile.PrimaryRoles = copyString(value.Profile.WorkPreferences.PrimaryRoles)
	profile.SecondaryRoles = copyString(value.Profile.WorkPreferences.SecondaryRoles)
	profile.PreferredRoles = copyString(value.Profile.WorkPreferences.PreferredRoles)
	if f := value.Profile.Communication.AlwaysEmphasize; CanExposeProfileFact(f.ProfileFact) {
		profile.AlwaysEmphasize = append([]string{}, f.Values...)
	}
	if f := value.Profile.Communication.AvoidClaiming; CanExposeProfileFact(f.ProfileFact) {
		profile.AvoidClaiming = append([]string{}, f.Values...)
	}
	return profile
}

// SafeProfile exposes the profile portion of the canonical pure projection.
func SafeProfile(value Candidate) EmployerSafeProfileKnowledge { return safeProfile(value) }

func ProfileFactFromMetadata(metadata KnowledgeMetadata) ProfileFact { return profileFact(metadata) }
func profileFact(metadata KnowledgeMetadata) ProfileFact {
	source := CandidateSourceUnknown
	if len(metadata.Sources) > 0 {
		source = CandidateSource(metadata.Sources[0].Type)
	}
	fact := ProfileFact{Source: source, Confirmed: CanExposeToEmployer(metadata), Evidence: append([]string{}, metadata.Evidence...)}
	if metadata.ConfirmedAt != nil {
		fact.ConfirmedAt = *metadata.ConfirmedAt
	} else if len(metadata.Sources) > 0 && metadata.Sources[0].ObservedAt != nil {
		fact.ConfirmedAt = *metadata.Sources[0].ObservedAt
	}
	return fact
}
