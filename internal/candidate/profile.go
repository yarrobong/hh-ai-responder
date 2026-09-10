package candidate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CandidateSource is the legacy profile provenance vocabulary.
type CandidateSource string

const (
	CandidateSourceUserConfirmed  CandidateSource = "user_confirmed"
	CandidateSourceHHResume       CandidateSource = "hh_resume"
	CandidateSourceGithubVerified CandidateSource = "github_verified"
	CandidateSourceDerived        CandidateSource = "derived"
	CandidateSourceUnknown        CandidateSource = "unknown"
)

// SkillLevel is intentionally separate from truth. A level describes the
// claimed depth; ProfileFact describes whether the claim is safe to use.
type SkillLevel string

const (
	SkillLevelUnknown   SkillLevel = "unknown"
	SkillLevelHeardOf   SkillLevel = "heard_of"
	SkillLevelBasic     SkillLevel = "basic"
	SkillLevelWorking   SkillLevel = "working"
	SkillLevelConfident SkillLevel = "confident"
	SkillLevelAdvanced  SkillLevel = "advanced"
)

type ProfileFact struct {
	Source      CandidateSource `json:"source"`
	Confirmed   bool            `json:"confirmed"`
	ConfirmedAt time.Time       `json:"confirmed_at,omitempty"`
	Evidence    []string        `json:"evidence,omitempty"`
}

type CandidateIdentity struct {
	FullName ProfileStringFact `json:"full_name,omitempty"`
	Location ProfileStringFact `json:"location,omitempty"`
}

type ProfileStringFact struct {
	Value string `json:"value"`
	ProfileFact
}

type ProfileIntFact struct {
	Value int `json:"value"`
	ProfileFact
}

type EducationFact struct {
	Level       string `json:"level,omitempty"`
	Institution string `json:"institution,omitempty"`
	Specialty   string `json:"specialty,omitempty"`
	Details     string `json:"details,omitempty"`
	ProfileFact
}

type WorkExperienceFact struct {
	Company      string `json:"company,omitempty"`
	Role         string `json:"role,omitempty"`
	Description  string `json:"description,omitempty"`
	Achievements string `json:"achievements,omitempty"`
	StartDate    string `json:"start_date,omitempty"`
	EndDate      string `json:"end_date,omitempty"`
	ProfileFact
}

type ProjectFact struct {
	Name           string   `json:"name,omitempty"`
	Role           string   `json:"role,omitempty"`
	Description    string   `json:"description,omitempty"`
	Technologies   []string `json:"technologies,omitempty"`
	BusinessImpact string   `json:"business_impact,omitempty"`
	ProfileFact
}

type CandidateSkill struct {
	Name     string     `json:"name"`
	Level    SkillLevel `json:"level"`
	Negative bool       `json:"negative,omitempty"`
	ProfileFact
}

type LanguageFact struct {
	Name  string `json:"name"`
	Level string `json:"level,omitempty"`
	ProfileFact
}

type WorkPreferences struct {
	PreferredRoles ProfileStringFact `json:"preferred_roles,omitempty"`
	PrimaryRoles   ProfileStringFact `json:"primary_roles,omitempty"`
	SecondaryRoles ProfileStringFact `json:"secondary_roles,omitempty"`
	WorkMode       ProfileStringFact `json:"work_mode,omitempty"`
	Relocation     ProfileStringFact `json:"relocation,omitempty"`
	BusinessTrips  ProfileStringFact `json:"business_trips,omitempty"`
	SalaryMinimum  ProfileIntFact    `json:"salary_minimum,omitempty"`
}

type ProfileListFact struct {
	Values []string `json:"values,omitempty"`
	ProfileFact
}

type EmployerCommunicationPreferences struct {
	Salary           ProfileStringFact `json:"salary,omitempty"`
	Interview        ProfileStringFact `json:"interview,omitempty"`
	Documents        ProfileStringFact `json:"documents,omitempty"`
	OtherPreferences ProfileStringFact `json:"other_preferences,omitempty"`
	AlwaysEmphasize  ProfileListFact   `json:"always_emphasize,omitempty"`
	AvoidClaiming    ProfileListFact   `json:"avoid_claiming,omitempty"`
}

type PendingProfileQuestion struct {
	Topic     string    `json:"topic"`
	Question  string    `json:"question"`
	Category  string    `json:"category,omitempty"`
	VacancyID int       `json:"vacancy_id,omitempty"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func NewProfile(now time.Time) CandidateProfile {
	return CandidateProfile{Version: 1, UpdatedAt: now}
}

// CandidateProfile is the legacy persisted profile value. It is deliberately
// distinct from Candidate, which is the canonical normalized aggregate.
type CandidateProfile struct {
	Version                          int                              `json:"version"`
	UpdatedAt                        time.Time                        `json:"updated_at"`
	Identity                         CandidateIdentity                `json:"identity"`
	Education                        []EducationFact                  `json:"education,omitempty"`
	WorkExperience                   []WorkExperienceFact             `json:"work_experience,omitempty"`
	TotalExperienceMonths            ProfileIntFact                   `json:"total_experience_months,omitempty"`
	Projects                         []ProjectFact                    `json:"projects,omitempty"`
	Skills                           []CandidateSkill                 `json:"skills,omitempty"`
	Languages                        []LanguageFact                   `json:"languages,omitempty"`
	WorkPreferences                  WorkPreferences                  `json:"work_preferences"`
	EmployerCommunicationPreferences EmployerCommunicationPreferences `json:"employer_communication_preferences"`
	UnknownPendingFacts              []PendingProfileQuestion         `json:"unknown_pending_facts,omitempty"`
}

// ProfileSnapshot is retained as a domain compatibility name during the
// staged extraction of the legacy profile.
type ProfileSnapshot = CandidateProfile

func ValidateCandidateSource(source CandidateSource) error {
	switch source {
	case CandidateSourceUserConfirmed, CandidateSourceHHResume, CandidateSourceGithubVerified, CandidateSourceDerived, CandidateSourceUnknown:
		return nil
	default:
		return fmt.Errorf("invalid candidate source %q", source)
	}
}

// CompareSourcePriority returns a positive value when a is preferred over b.
func CompareSourcePriority(a, b CandidateSource) int {
	return sourcePriority(a) - sourcePriority(b)
}

// SourcePriority exposes the deterministic rank for compatibility adapters.
func SourcePriority(source CandidateSource) int { return sourcePriority(source) }

func sourcePriority(source CandidateSource) int {
	switch source {
	case CandidateSourceUserConfirmed:
		return 4
	case CandidateSourceHHResume:
		return 3
	case CandidateSourceGithubVerified:
		return 2
	case CandidateSourceDerived:
		return 1
	default:
		return 0
	}
}

func SourceTrustedForEmployerCommunication(source CandidateSource) bool {
	return source == CandidateSourceUserConfirmed || source == CandidateSourceHHResume || source == CandidateSourceGithubVerified
}

func ValidateProfileFact(fact ProfileFact) error {
	if fact.Source == "" && !fact.Confirmed && fact.ConfirmedAt.IsZero() && len(fact.Evidence) == 0 {
		return nil
	}
	if err := ValidateCandidateSource(fact.Source); err != nil {
		return err
	}
	if fact.Confirmed && !SourceTrustedForEmployerCommunication(fact.Source) {
		return fmt.Errorf("source %q cannot have confirmed=true", fact.Source)
	}
	if fact.Confirmed && fact.ConfirmedAt.IsZero() {
		return errors.New("confirmed fact is missing confirmed_at")
	}
	if fact.Confirmed && len(fact.Evidence) == 0 {
		return errors.New("confirmed fact is missing evidence")
	}
	return nil
}

func ValidSkillLevel(level SkillLevel) bool {
	switch level {
	case SkillLevelUnknown, SkillLevelHeardOf, SkillLevelBasic, SkillLevelWorking, SkillLevelConfident, SkillLevelAdvanced:
		return true
	default:
		return false
	}
}

func ValidateCandidateProfile(profile CandidateProfile) error {
	for _, fact := range []ProfileFact{
		profile.Identity.FullName.ProfileFact, profile.Identity.Location.ProfileFact,
		profile.TotalExperienceMonths.ProfileFact, profile.WorkPreferences.SalaryMinimum.ProfileFact,
		profile.WorkPreferences.PreferredRoles.ProfileFact, profile.WorkPreferences.PrimaryRoles.ProfileFact,
		profile.WorkPreferences.SecondaryRoles.ProfileFact, profile.WorkPreferences.WorkMode.ProfileFact,
		profile.WorkPreferences.Relocation.ProfileFact, profile.WorkPreferences.BusinessTrips.ProfileFact,
		profile.EmployerCommunicationPreferences.Salary.ProfileFact, profile.EmployerCommunicationPreferences.Interview.ProfileFact,
		profile.EmployerCommunicationPreferences.Documents.ProfileFact, profile.EmployerCommunicationPreferences.OtherPreferences.ProfileFact,
		profile.EmployerCommunicationPreferences.AlwaysEmphasize.ProfileFact, profile.EmployerCommunicationPreferences.AvoidClaiming.ProfileFact,
	} {
		if err := ValidateProfileFact(fact); err != nil {
			return err
		}
	}
	for _, fact := range profile.Education {
		if err := ValidateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.WorkExperience {
		if err := ValidateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.Projects {
		if err := ValidateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.Skills {
		if err := ValidateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
		if !ValidSkillLevel(fact.Level) {
			return fmt.Errorf("invalid skill level %q", fact.Level)
		}
	}
	for _, fact := range profile.Languages {
		if err := ValidateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, question := range profile.UnknownPendingFacts {
		if strings.TrimSpace(question.Topic) == "" || strings.TrimSpace(question.Question) == "" || strings.TrimSpace(question.Reason) == "" {
			return errors.New("pending profile question is incomplete")
		}
	}
	return nil
}

func ValidateProfileSnapshot(profile ProfileSnapshot) error {
	return ValidateCandidateProfile(profile)
}

func (p CandidateProfile) ResolveSkill(name string) (CandidateSkill, bool) {
	canonical := CanonicalSkillName(name)
	if canonical == "" {
		return CandidateSkill{}, false
	}
	var best CandidateSkill
	found := false
	for _, skill := range p.Skills {
		if CanonicalSkillName(skill.Name) != canonical || !skill.Confirmed || !SourceTrustedForEmployerCommunication(skill.Source) {
			continue
		}
		if !found || SourcePriority(skill.Source) > SourcePriority(best.Source) {
			best, found = skill, true
		}
	}
	return best, found
}

func (p CandidateProfile) TrustedSkillsText() string {
	var names []string
	seen := make(map[string]struct{})
	for _, skill := range p.Skills {
		if !skill.Confirmed || !SourceTrustedForEmployerCommunication(skill.Source) || skill.Negative {
			continue
		}
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		key := CanonicalSkillName(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (p CandidateProfile) TrustedExperienceText() string {
	var entries []string
	for _, fact := range p.WorkExperience {
		if !fact.Confirmed || !SourceTrustedForEmployerCommunication(fact.Source) {
			continue
		}
		parts := []string{fact.Role, fact.Company, fact.Description, fact.Achievements}
		if value := strings.TrimSpace(strings.Join(parts, "\n")); value != "" {
			entries = append(entries, value)
		}
	}
	return strings.Join(entries, "\n\n")
}

func (p CandidateProfile) TrustedProjectsText() string {
	var projects []string
	for _, fact := range p.Projects {
		if !fact.Confirmed || !SourceTrustedForEmployerCommunication(fact.Source) {
			continue
		}
		parts := []string{fact.Name, fact.Role, fact.Description, fact.BusinessImpact}
		if len(fact.Technologies) > 0 {
			parts = append(parts, "Technologies: "+strings.Join(fact.Technologies, ", "))
		}
		if value := strings.TrimSpace(strings.Join(parts, "\n")); value != "" {
			projects = append(projects, value)
		}
	}
	return strings.Join(projects, "\n\n")
}

func (p CandidateProfile) TrustedEducation() (string, string, bool) {
	var selected EducationFact
	found := false
	for _, fact := range p.Education {
		if fact.Confirmed && SourceTrustedForEmployerCommunication(fact.Source) && (!found || SourcePriority(fact.Source) > SourcePriority(selected.Source)) {
			selected, found = fact, true
		}
	}
	if found {
		return selected.Level, strings.TrimSpace(strings.Join([]string{selected.Institution, selected.Specialty, selected.Details}, ", ")), true
	}
	return "", "", false
}

func (p CandidateProfile) TrustedCommunicationRules() (string, string) {
	trusted := func(fact ProfileListFact) string {
		if !fact.Confirmed || !SourceTrustedForEmployerCommunication(fact.Source) {
			return ""
		}
		return strings.Join(fact.Values, ", ")
	}
	return trusted(p.EmployerCommunicationPreferences.AlwaysEmphasize), trusted(p.EmployerCommunicationPreferences.AvoidClaiming)
}

func (p *CandidateProfile) AddPendingQuestion(question PendingProfileQuestion) bool {
	question.Topic = strings.TrimSpace(question.Topic)
	question.Question = strings.TrimSpace(question.Question)
	question.Reason = strings.TrimSpace(question.Reason)
	if question.CreatedAt.IsZero() {
		question.CreatedAt = time.Now()
	}
	for _, existing := range p.UnknownPendingFacts {
		if NormalizeProfileName(existing.Topic) == NormalizeProfileName(question.Topic) || NormalizeProfileName(existing.Question) == NormalizeProfileName(question.Question) {
			return false
		}
	}
	p.UnknownPendingFacts = append(p.UnknownPendingFacts, question)
	return true
}

func (p *CandidateProfile) AnswerPendingQuestion(index int, level SkillLevel, now time.Time) error {
	if index < 0 || index >= len(p.UnknownPendingFacts) {
		return errors.New("pending profile question index is out of range")
	}
	if !ValidSkillLevel(level) {
		return fmt.Errorf("invalid answer skill level %q", level)
	}
	question := p.UnknownPendingFacts[index]
	p.Skills = append(p.Skills, CandidateSkill{Name: question.Topic, Level: level, Negative: level == SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"answered profile question: " + question.Question}}})
	p.UnknownPendingFacts = append(p.UnknownPendingFacts[:index], p.UnknownPendingFacts[index+1:]...)
	return nil
}

func (p *CandidateProfile) RemovePendingQuestion(index int) {
	p.UnknownPendingFacts = append(p.UnknownPendingFacts[:index], p.UnknownPendingFacts[index+1:]...)
}

func (p *CandidateProfile) MergeHHResumeFacts(resume ResumeItem, facts ResumeFacts, now time.Time) {
	if p.Version == 0 {
		p.Version = 1
	}
	if strings.TrimSpace(resume.Area) != "" && SourcePriority(p.Identity.Location.Source) <= SourcePriority(CandidateSourceHHResume) {
		p.Identity.Location = ProfileStringFact{Value: resume.Area, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"HH resume area"}}}
	}
	if facts.EducationKnown && SourcePriority(CandidateSourceHHResume) >= SourcePriority(p.EducationSource()) {
		p.Education = []EducationFact{{Level: facts.EducationLevel, Details: facts.EducationDetails, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"structured HH resume education"}}}}
	}
	if strings.TrimSpace(facts.ExperienceText) != "" {
		preserveUserExperience := false
		for _, fact := range p.WorkExperience {
			if fact.Source == CandidateSourceUserConfirmed && fact.Confirmed {
				preserveUserExperience = true
				break
			}
		}
		if !preserveUserExperience {
			p.WorkExperience = []WorkExperienceFact{{Description: facts.ExperienceText, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"structured HH resume experience"}}}}
		}
	}
	if facts.TotalExperienceMonthsKnown && SourcePriority(p.TotalExperienceMonths.Source) <= SourcePriority(CandidateSourceHHResume) {
		if !(p.TotalExperienceMonths.Source == CandidateSourceUserConfirmed && p.TotalExperienceMonths.Confirmed) {
			p.TotalExperienceMonths = ProfileIntFact{Value: facts.TotalExperienceMonths, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"structured HH resume total experience"}}}
		}
	}
	for _, name := range parseResumeSkillNames(resume.Skills) {
		p.MergeSkill(CandidateSkill{Name: name, Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"HH resume skills"}}})
	}
	p.UpdatedAt = now
}

func parseResumeSkillNames(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '•' || r == '|'
	})
	var result []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || len([]rune(name)) < 2 {
			continue
		}
		key := CanonicalSkillName(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	return result
}

func ParseResumeSkillNames(value string) []string { return parseResumeSkillNames(value) }

func (p CandidateProfile) EducationSource() CandidateSource {
	for _, fact := range p.Education {
		if fact.Confirmed {
			return fact.Source
		}
	}
	return CandidateSourceUnknown
}

func (p *CandidateProfile) MergeSkill(incoming CandidateSkill) {
	for i, existing := range p.Skills {
		if CanonicalSkillName(existing.Name) != CanonicalSkillName(incoming.Name) {
			continue
		}
		if SourcePriority(existing.Source) > SourcePriority(incoming.Source) || (existing.Source == CandidateSourceUserConfirmed && existing.Confirmed) {
			return
		}
		p.Skills[i] = incoming
		return
	}
	p.Skills = append(p.Skills, incoming)
}

func MergeCandidateProfiles(current, incoming CandidateProfile) CandidateProfile {
	result := current
	if result.Version == 0 {
		result.Version = 1
	}
	result.Identity.FullName = preferStringFact(result.Identity.FullName, incoming.Identity.FullName)
	result.Identity.Location = preferStringFact(result.Identity.Location, incoming.Identity.Location)
	result.Education = mergeEducationFacts(result.Education, incoming.Education)
	result.WorkExperience = mergeWorkExperienceFacts(result.WorkExperience, incoming.WorkExperience)
	result.Projects = mergeProjectFacts(result.Projects, incoming.Projects)
	result.Skills = mergeSkillFacts(result.Skills, incoming.Skills)
	result.Languages = mergeLanguageFacts(result.Languages, incoming.Languages)
	result.TotalExperienceMonths = preferIntFact(result.TotalExperienceMonths, incoming.TotalExperienceMonths)
	result.WorkPreferences.PreferredRoles = preferStringFact(result.WorkPreferences.PreferredRoles, incoming.WorkPreferences.PreferredRoles)
	result.WorkPreferences.PrimaryRoles = preferStringFact(result.WorkPreferences.PrimaryRoles, incoming.WorkPreferences.PrimaryRoles)
	result.WorkPreferences.SecondaryRoles = preferStringFact(result.WorkPreferences.SecondaryRoles, incoming.WorkPreferences.SecondaryRoles)
	result.WorkPreferences.WorkMode = preferStringFact(result.WorkPreferences.WorkMode, incoming.WorkPreferences.WorkMode)
	result.WorkPreferences.Relocation = preferStringFact(result.WorkPreferences.Relocation, incoming.WorkPreferences.Relocation)
	result.WorkPreferences.BusinessTrips = preferStringFact(result.WorkPreferences.BusinessTrips, incoming.WorkPreferences.BusinessTrips)
	result.WorkPreferences.SalaryMinimum = preferIntFact(result.WorkPreferences.SalaryMinimum, incoming.WorkPreferences.SalaryMinimum)
	result.EmployerCommunicationPreferences.Salary = preferStringFact(result.EmployerCommunicationPreferences.Salary, incoming.EmployerCommunicationPreferences.Salary)
	result.EmployerCommunicationPreferences.Interview = preferStringFact(result.EmployerCommunicationPreferences.Interview, incoming.EmployerCommunicationPreferences.Interview)
	result.EmployerCommunicationPreferences.Documents = preferStringFact(result.EmployerCommunicationPreferences.Documents, incoming.EmployerCommunicationPreferences.Documents)
	result.EmployerCommunicationPreferences.OtherPreferences = preferStringFact(result.EmployerCommunicationPreferences.OtherPreferences, incoming.EmployerCommunicationPreferences.OtherPreferences)
	result.EmployerCommunicationPreferences.AlwaysEmphasize = preferListFact(result.EmployerCommunicationPreferences.AlwaysEmphasize, incoming.EmployerCommunicationPreferences.AlwaysEmphasize)
	result.EmployerCommunicationPreferences.AvoidClaiming = preferListFact(result.EmployerCommunicationPreferences.AvoidClaiming, incoming.EmployerCommunicationPreferences.AvoidClaiming)
	for _, question := range incoming.UnknownPendingFacts {
		result.AddPendingQuestion(question)
	}
	if incoming.UpdatedAt.After(result.UpdatedAt) {
		result.UpdatedAt = incoming.UpdatedAt
	}
	return result
}

func preferProfileFact(current, incoming ProfileFact) ProfileFact {
	if current.Source == "" && incoming.Source != "" {
		return incoming
	}
	if incoming.Source != "" && SourcePriority(incoming.Source) >= SourcePriority(current.Source) {
		return incoming
	}
	return current
}

func preferStringFact(current, incoming ProfileStringFact) ProfileStringFact {
	if strings.TrimSpace(current.Value) == "" {
		return incoming
	}
	if strings.TrimSpace(incoming.Value) == "" {
		return current
	}
	if selected := preferProfileFact(current.ProfileFact, incoming.ProfileFact); selected.Source == incoming.Source && selected.ConfirmedAt.Equal(incoming.ConfirmedAt) {
		return incoming
	}
	return current
}

func preferIntFact(current, incoming ProfileIntFact) ProfileIntFact {
	if current.Source == "" {
		return incoming
	}
	if incoming.Source != "" && SourcePriority(incoming.Source) >= SourcePriority(current.Source) {
		return incoming
	}
	return current
}

func preferListFact(current, incoming ProfileListFact) ProfileListFact {
	if len(current.Values) == 0 {
		return incoming
	}
	if len(incoming.Values) == 0 {
		return current
	}
	if selected := preferProfileFact(current.ProfileFact, incoming.ProfileFact); selected.Source == incoming.Source && selected.ConfirmedAt.Equal(incoming.ConfirmedAt) {
		return incoming
	}
	return current
}

func mergeEducationFacts(current, incoming []EducationFact) []EducationFact {
	result := append([]EducationFact(nil), current...)
	for _, fact := range incoming {
		key := NormalizeProfileName(strings.Join([]string{fact.Level, fact.Institution, fact.Specialty}, "|"))
		found := false
		for i, existing := range result {
			if key != "" && NormalizeProfileName(strings.Join([]string{existing.Level, existing.Institution, existing.Specialty}, "|")) == key {
				result[i].ProfileFact = preferProfileFact(existing.ProfileFact, fact.ProfileFact)
				if SourcePriority(fact.Source) >= SourcePriority(existing.Source) {
					result[i] = fact
				}
				found = true
				break
			}
		}
		if !found {
			result = append(result, fact)
		}
	}
	return result
}

func mergeWorkExperienceFacts(current, incoming []WorkExperienceFact) []WorkExperienceFact {
	result := append([]WorkExperienceFact(nil), current...)
	for _, fact := range incoming {
		key := NormalizeProfileName(strings.Join([]string{fact.Company, fact.Role, fact.Description}, "|"))
		found := false
		for i, existing := range result {
			if key != "" && NormalizeProfileName(strings.Join([]string{existing.Company, existing.Role, existing.Description}, "|")) == key {
				if SourcePriority(fact.Source) >= SourcePriority(existing.Source) {
					result[i] = fact
				}
				found = true
				break
			}
		}
		if !found {
			result = append(result, fact)
		}
	}
	return result
}

func mergeProjectFacts(current, incoming []ProjectFact) []ProjectFact {
	result := append([]ProjectFact(nil), current...)
	for _, fact := range incoming {
		found := false
		for i, existing := range result {
			if NormalizeProfileName(existing.Name) == NormalizeProfileName(fact.Name) && NormalizeProfileName(fact.Name) != "" {
				if SourcePriority(fact.Source) >= SourcePriority(existing.Source) {
					result[i] = fact
				}
				found = true
				break
			}
		}
		if !found {
			result = append(result, fact)
		}
	}
	return result
}

func mergeSkillFacts(current, incoming []CandidateSkill) []CandidateSkill {
	result := CandidateProfile{Skills: append([]CandidateSkill(nil), current...)}
	for _, fact := range incoming {
		result.MergeSkill(fact)
	}
	return result.Skills
}

func mergeLanguageFacts(current, incoming []LanguageFact) []LanguageFact {
	result := append([]LanguageFact(nil), current...)
	for _, fact := range incoming {
		found := false
		for i, existing := range result {
			if NormalizeProfileName(existing.Name) == NormalizeProfileName(fact.Name) && NormalizeProfileName(fact.Name) != "" {
				if SourcePriority(fact.Source) >= SourcePriority(existing.Source) {
					result[i] = fact
				}
				found = true
				break
			}
		}
		if !found {
			result = append(result, fact)
		}
	}
	return result
}

func NormalizeProfileName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func CanonicalSkillName(value string) string {
	name := strings.ToLower(NormalizeProfileName(value))
	if name == "golang" {
		return "go"
	}
	return name
}
