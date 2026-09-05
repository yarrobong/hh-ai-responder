package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CandidateSource string

const (
	CandidateSourceUserConfirmed  CandidateSource = "user_confirmed"
	CandidateSourceHHResume       CandidateSource = "hh_resume"
	CandidateSourceGithubVerified CandidateSource = "github_verified"
	CandidateSourceDerived        CandidateSource = "derived"
	CandidateSourceUnknown        CandidateSource = "unknown"
)

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

func NewCandidateProfile(now time.Time) CandidateProfile {
	return CandidateProfile{Version: 1, UpdatedAt: now}
}

func validateCandidateSource(source CandidateSource) error {
	switch source {
	case CandidateSourceUserConfirmed, CandidateSourceHHResume, CandidateSourceGithubVerified,
		CandidateSourceDerived, CandidateSourceUnknown:
		return nil
	default:
		return fmt.Errorf("invalid candidate source %q", source)
	}
}

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

func sourceTrustedForEmployerCommunication(source CandidateSource) bool {
	return source == CandidateSourceUserConfirmed || source == CandidateSourceHHResume || source == CandidateSourceGithubVerified
}

func validateProfileFact(fact ProfileFact) error {
	if fact.Source == "" && !fact.Confirmed && fact.ConfirmedAt.IsZero() && len(fact.Evidence) == 0 {
		return nil
	}
	if err := validateCandidateSource(fact.Source); err != nil {
		return err
	}
	if fact.Confirmed && !sourceTrustedForEmployerCommunication(fact.Source) {
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

func validateCandidateProfile(profile CandidateProfile) error {
	if profile.Version == 0 {
		profile.Version = 1
	}
	validateStringFact := func(fact ProfileStringFact) error {
		return validateProfileFact(fact.ProfileFact)
	}
	if err := validateStringFact(profile.Identity.FullName); err != nil {
		return err
	}
	if err := validateStringFact(profile.Identity.Location); err != nil {
		return err
	}
	if err := validateProfileFact(profile.TotalExperienceMonths.ProfileFact); err != nil {
		return err
	}
	if err := validateProfileFact(profile.WorkPreferences.SalaryMinimum.ProfileFact); err != nil {
		return err
	}
	for _, fact := range profile.Education {
		if err := validateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.WorkExperience {
		if err := validateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.Projects {
		if err := validateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range profile.Skills {
		if err := validateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
		if !validSkillLevel(fact.Level) {
			return fmt.Errorf("invalid skill level %q", fact.Level)
		}
	}
	for _, fact := range profile.Languages {
		if err := validateProfileFact(fact.ProfileFact); err != nil {
			return err
		}
	}
	for _, fact := range []ProfileFact{
		profile.WorkPreferences.PreferredRoles.ProfileFact,
		profile.WorkPreferences.PrimaryRoles.ProfileFact,
		profile.WorkPreferences.SecondaryRoles.ProfileFact,
		profile.WorkPreferences.WorkMode.ProfileFact,
		profile.WorkPreferences.Relocation.ProfileFact,
		profile.WorkPreferences.BusinessTrips.ProfileFact,
		profile.EmployerCommunicationPreferences.Salary.ProfileFact,
		profile.EmployerCommunicationPreferences.Interview.ProfileFact,
		profile.EmployerCommunicationPreferences.Documents.ProfileFact,
		profile.EmployerCommunicationPreferences.OtherPreferences.ProfileFact,
		profile.EmployerCommunicationPreferences.AlwaysEmphasize.ProfileFact,
		profile.EmployerCommunicationPreferences.AvoidClaiming.ProfileFact,
	} {
		if err := validateProfileFact(fact); err != nil {
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

func validSkillLevel(level SkillLevel) bool {
	switch level {
	case SkillLevelUnknown, SkillLevelHeardOf, SkillLevelBasic, SkillLevelWorking, SkillLevelConfident, SkillLevelAdvanced:
		return true
	default:
		return false
	}
}

func profileContainsSecret(raw []byte) bool {
	text := strings.ToLower(string(raw))
	for _, marker := range []string{"api_key", "apikey", "access_token", "refresh_token", "authorization", "cookie", "xsrf", "password", "client_secret"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func LoadCandidateProfile(path string) (CandidateProfile, error) {
	if strings.TrimSpace(path) == "" {
		return NewCandidateProfile(time.Now()), nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewCandidateProfile(time.Now()), nil
	}
	if err != nil {
		return CandidateProfile{}, fmt.Errorf("read candidate profile: %w", err)
	}
	if profileContainsSecret(raw) {
		return CandidateProfile{}, errors.New("candidate profile contains a forbidden secret field")
	}
	var profile CandidateProfile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return CandidateProfile{}, fmt.Errorf("decode candidate profile: %w", err)
	}
	if err := validateCandidateProfile(profile); err != nil {
		return CandidateProfile{}, fmt.Errorf("validate candidate profile: %w", err)
	}
	if profile.Version == 0 {
		profile.Version = 1
	}
	return profile, nil
}

func SaveCandidateProfile(path string, profile CandidateProfile) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("candidate profile path is empty")
	}
	if err := validateCandidateProfile(profile); err != nil {
		return err
	}
	profile.Version = 1
	profile.UpdatedAt = time.Now()
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode candidate profile: %w", err)
	}
	if profileContainsSecret(raw) {
		return errors.New("candidate profile would contain a forbidden secret field")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create candidate profile directory: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write candidate profile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace candidate profile: %w", err)
	}
	return nil
}

func normalizeProfileName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

var explicitSkillAliases = map[string]string{
	"golang": "go",
}

func canonicalSkillName(value string) string {
	name := normalizeProfileName(value)
	if alias, ok := explicitSkillAliases[name]; ok {
		return alias
	}
	return name
}

func (p CandidateProfile) ResolveSkill(name string) (CandidateSkill, bool) {
	canonical := canonicalSkillName(name)
	if canonical == "" {
		return CandidateSkill{}, false
	}
	var best CandidateSkill
	found := false
	for _, skill := range p.Skills {
		if canonicalSkillName(skill.Name) != canonical || !skill.Confirmed || !sourceTrustedForEmployerCommunication(skill.Source) {
			continue
		}
		if !found || sourcePriority(skill.Source) > sourcePriority(best.Source) {
			best, found = skill, true
		}
	}
	return best, found
}

func (p CandidateProfile) TrustedSkillsText() string {
	var names []string
	seen := make(map[string]struct{})
	for _, skill := range p.Skills {
		if !skill.Confirmed || !sourceTrustedForEmployerCommunication(skill.Source) || skill.Negative {
			continue
		}
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		key := canonicalSkillName(name)
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
		if !fact.Confirmed || !sourceTrustedForEmployerCommunication(fact.Source) {
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
		if !fact.Confirmed || !sourceTrustedForEmployerCommunication(fact.Source) {
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
	for _, fact := range p.Education {
		if fact.Confirmed && sourceTrustedForEmployerCommunication(fact.Source) {
			return fact.Level, strings.TrimSpace(strings.Join([]string{fact.Institution, fact.Specialty, fact.Details}, ", ")), true
		}
	}
	return "", "", false
}

func (p CandidateProfile) TrustedCommunicationRules() (string, string) {
	trusted := func(fact ProfileListFact) string {
		if !fact.Confirmed || !sourceTrustedForEmployerCommunication(fact.Source) {
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
		if normalizeProfileName(existing.Topic) == normalizeProfileName(question.Topic) || normalizeProfileName(existing.Question) == normalizeProfileName(question.Question) {
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
	if !validSkillLevel(level) {
		return fmt.Errorf("invalid answer skill level %q", level)
	}
	question := p.UnknownPendingFacts[index]
	p.Skills = append(p.Skills, CandidateSkill{
		Name:     question.Topic,
		Level:    level,
		Negative: level == SkillLevelUnknown,
		ProfileFact: ProfileFact{
			Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now,
			Evidence: []string{"answered profile question: " + question.Question},
		},
	})
	p.UnknownPendingFacts = append(p.UnknownPendingFacts[:index], p.UnknownPendingFacts[index+1:]...)
	return nil
}

func (p *CandidateProfile) removePendingQuestion(index int) {
	p.UnknownPendingFacts = append(p.UnknownPendingFacts[:index], p.UnknownPendingFacts[index+1:]...)
}

func (p *CandidateProfile) MergeHHResumeFacts(resume ResumeItem, facts ResumeFacts, now time.Time) {
	if p.Version == 0 {
		p.Version = 1
	}
	if strings.TrimSpace(resume.Area) != "" && sourcePriority(p.Identity.Location.Source) <= sourcePriority(CandidateSourceHHResume) {
		p.Identity.Location = ProfileStringFact{Value: resume.Area, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"HH resume area"}}}
	}
	if facts.EducationKnown && sourcePriority(CandidateSourceHHResume) >= sourcePriority(p.educationSource()) {
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
	if facts.TotalExperienceMonthsKnown && sourcePriority(p.TotalExperienceMonths.Source) <= sourcePriority(CandidateSourceHHResume) {
		if !(p.TotalExperienceMonths.Source == CandidateSourceUserConfirmed && p.TotalExperienceMonths.Confirmed) {
			p.TotalExperienceMonths = ProfileIntFact{Value: facts.TotalExperienceMonths, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"structured HH resume total experience"}}}
		}
	}
	for _, name := range parseResumeSkillNames(resume.Skills) {
		p.mergeSkill(CandidateSkill{Name: name, Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceHHResume, Confirmed: true, ConfirmedAt: now, Evidence: []string{"HH resume skills"}}})
	}
	p.UpdatedAt = now
}

func (p CandidateProfile) educationSource() CandidateSource {
	for _, fact := range p.Education {
		if fact.Confirmed {
			return fact.Source
		}
	}
	return CandidateSourceUnknown
}

func (p *CandidateProfile) mergeSkill(incoming CandidateSkill) {
	for i, existing := range p.Skills {
		if canonicalSkillName(existing.Name) != canonicalSkillName(incoming.Name) {
			continue
		}
		if sourcePriority(existing.Source) > sourcePriority(incoming.Source) || (existing.Source == CandidateSourceUserConfirmed && existing.Confirmed) {
			return
		}
		p.Skills[i] = incoming
		return
	}
	p.Skills = append(p.Skills, incoming)
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
		key := canonicalSkillName(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	return result
}

func profileStringFact(value string, now time.Time) ProfileStringFact {
	return ProfileStringFact{Value: strings.TrimSpace(value), ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}}
}

func skillLevelQuestion(level SkillLevel) string {
	switch level {
	case SkillLevelUnknown:
		return "нет подтверждённого опыта"
	case SkillLevelHeardOf:
		return "только слышал или изучал"
	case SkillLevelBasic:
		return "делал базовые задачи"
	case SkillLevelWorking:
		return "использовал в проекте или рабочей задаче"
	case SkillLevelConfident:
		return "уверенно работаю"
	case SkillLevelAdvanced:
		return "продвинутый уровень"
	default:
		return string(level)
	}
}

func parseSkillLevelAnswer(value string) (SkillLevel, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	mapping := map[string]SkillLevel{"0": SkillLevelUnknown, "1": SkillLevelHeardOf, "2": SkillLevelBasic, "3": SkillLevelWorking, "4": SkillLevelConfident, "5": SkillLevelAdvanced}
	if level, ok := mapping[value]; ok {
		return level, nil
	}
	return SkillLevelUnknown, errors.New("enter a number from 0 to 5")
}

func runProfileCommand(args []string, in io.Reader, out io.Writer) error {
	path := defaultCandidateProfilePath()
	command := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "show", "questions", "bootstrap":
			if command != "" {
				return errors.New("usage: profile [show|questions|bootstrap] [-candidate-profile path]")
			}
			command = args[i]
		case "-candidate-profile":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return errors.New("usage: profile [show|questions|bootstrap] [-candidate-profile path]")
			}
			path = args[i+1]
			i++
		default:
			return errors.New("usage: profile [show|questions|bootstrap] [-candidate-profile path]")
		}
	}
	profile, err := LoadCandidateProfile(path)
	if err != nil {
		return err
	}
	switch command {
	case "show":
		encoded, err := json.MarshalIndent(profile, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(encoded))
		return err
	case "questions":
		if len(profile.UnknownPendingFacts) == 0 {
			_, err = fmt.Fprintln(out, "Нет ожидающих вопросов профиля.")
			return err
		}
		for i, question := range profile.UnknownPendingFacts {
			fmt.Fprintf(out, "%d. %s\n   причина: %s\n", i+1, question.Question, question.Reason)
		}
		return nil
	case "bootstrap":
		return runProfileBootstrap(&profile, path, in, out)
	case "":
		return runInteractiveProfile(&profile, path, in, out)
	default:
		return errors.New("usage: profile [show|questions|bootstrap] [-candidate-profile path]")
	}
}

func defaultCandidateProfilePath() string {
	if value := strings.TrimSpace(os.Getenv("HH_CANDIDATE_PROFILE")); value != "" {
		return value
	}
	wd, err := os.Getwd()
	if err != nil {
		return "candidate_profile.json"
	}
	return filepath.Join(wd, "candidate_profile.json")
}

func askProfileLine(reader *bufio.Reader, out io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(out, prompt+" "); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runInteractiveProfile(profile *CandidateProfile, path string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	now := time.Now()
	for len(profile.UnknownPendingFacts) > 0 {
		question := profile.UnknownPendingFacts[0]
		if question.Category != "" && question.Category != hardRequirementCategorySkill {
			fmt.Fprintf(out, "Ожидающий вопрос: %s\n", question.Question)
			answer, err := askProfileLine(reader, out, "Подтверждённый ответ (Enter — пропустить):")
			if err != nil {
				return err
			}
			if answer == "" {
				break
			}
			fact := ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"answered profile question: " + question.Question}}
			switch question.Category {
			case hardRequirementCategoryLanguage:
				profile.Languages = append(profile.Languages, LanguageFact{Name: question.Topic, Level: answer, ProfileFact: fact})
			case hardRequirementCategoryEducation:
				profile.Education = append(profile.Education, EducationFact{Details: answer, ProfileFact: fact})
			case hardRequirementCategoryExperienceYears:
				profile.WorkExperience = append(profile.WorkExperience, WorkExperienceFact{Description: answer, ProfileFact: fact})
			default:
				profile.Projects = append(profile.Projects, ProjectFact{Description: answer, ProfileFact: fact})
			}
			profile.removePendingQuestion(0)
			continue
		}
		fmt.Fprintf(out, "Ожидающий вопрос: %s\n0 — нет подтверждённого опыта; 1 — только изучал; 2 — базовые задачи; 3 — использовал в проекте; 4 — уверенно работаю; 5 — продвинутый уровень.\n", question.Question)
		answer, err := askProfileLine(reader, out, "Ответ (0-5, Enter — пропустить):")
		if err != nil {
			return err
		}
		if answer == "" {
			break
		}
		level, err := parseSkillLevelAnswer(answer)
		if err != nil {
			fmt.Fprintln(out, "Нужна цифра от 0 до 5.")
			continue
		}
		if err := profile.AnswerPendingQuestion(0, level, now); err != nil {
			return err
		}
	}

	if profile.Identity.FullName.Value == "" {
		value, err := askProfileLine(reader, out, "Имя (Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.Identity.FullName = profileStringFact(value, now)
		}
	}
	if profile.Identity.Location.Value == "" {
		value, err := askProfileLine(reader, out, "Город/локация (Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.Identity.Location = profileStringFact(value, now)
		}
	}
	if len(profile.Education) == 0 {
		value, err := askProfileLine(reader, out, "Образование: уровень, учебное заведение и специальность (Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.Education = append(profile.Education, EducationFact{Details: value, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}})
		}
	}
	if len(profile.Skills) == 0 {
		value, err := askProfileLine(reader, out, "Технологии и инструменты через запятую (только те, о которых готов рассказать):")
		if err != nil {
			return err
		}
		for _, name := range parseResumeSkillNames(value) {
			fmt.Fprintf(out, "Уровень %s: 0 — нет; 1 — только изучал; 2 — базовые задачи; 3 — использовал в проекте; 4 — уверенно; 5 — продвинутый.\n", name)
			levelText, err := askProfileLine(reader, out, "Уровень (0-5):")
			if err != nil {
				return err
			}
			level, levelErr := parseSkillLevelAnswer(levelText)
			if levelErr != nil {
				return levelErr
			}
			profile.Skills = append(profile.Skills, CandidateSkill{Name: name, Level: level, Negative: level == SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}})
		}
	}
	if len(profile.Projects) == 0 {
		value, err := askProfileLine(reader, out, "Проекты и рабочие задачи (кратко, Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.Projects = append(profile.Projects, ProjectFact{Description: value, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}})
		}
	}
	if len(profile.WorkExperience) == 0 {
		value, err := askProfileLine(reader, out, "Рабочие задачи/опыт (без выдумывания, Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.WorkExperience = append(profile.WorkExperience, WorkExperienceFact{Description: value, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}})
		}
	}
	if len(profile.Languages) == 0 {
		value, err := askProfileLine(reader, out, "Языки и уровень через запятую (например, English — B1; Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.Languages = append(profile.Languages, LanguageFact{Name: value, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{"explicitly entered in profile questionnaire"}}})
		}
	}
	if profile.WorkPreferences.PreferredRoles.Value == "" {
		value, err := askProfileLine(reader, out, "Предпочтительные роли через запятую (Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.WorkPreferences.PreferredRoles = profileStringFact(value, now)
		}
	}
	if profile.WorkPreferences.WorkMode.Value == "" {
		value, err := askProfileLine(reader, out, "Remote/office/hybrid и предпочтение:")
		if err != nil {
			return err
		}
		if value != "" {
			profile.WorkPreferences.WorkMode = profileStringFact(value, now)
		}
	}
	if profile.WorkPreferences.Relocation.Value == "" {
		value, err := askProfileLine(reader, out, "Релокация (да/нет/не знаю):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.WorkPreferences.Relocation = profileStringFact(value, now)
		}
	}
	if profile.WorkPreferences.BusinessTrips.Value == "" {
		value, err := askProfileLine(reader, out, "Командировки (да/нет/не знаю):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.WorkPreferences.BusinessTrips = profileStringFact(value, now)
		}
	}
	if profile.EmployerCommunicationPreferences.OtherPreferences.Value == "" {
		value, err := askProfileLine(reader, out, "Предпочтения общения с работодателем (без контактов и секретов; Enter — пропустить):")
		if err != nil {
			return err
		}
		if value != "" {
			profile.EmployerCommunicationPreferences.OtherPreferences = profileStringFact(value, now)
		}
	}
	return SaveCandidateProfile(path, *profile)
}

var bootstrapSkillNames = []string{
	"Python", "Django", "DRF", "PostgreSQL", "SQL", "Redis", "Celery", "React", "TypeScript", "Node.js",
	"Linux", "Docker", "CI/CD", "Nginx", "Cloudflare", "Playwright", "LLM API", "Telegram Bots",
	"CRM/API Integration", "Bitrix",
}

var bootstrapProjectNames = []string{"BizonVR", "BizonVR Operator", "ekb-metro"}

var bootstrapPrimaryRoles = []string{"Backend Developer", "Integration Engineer", "Automation Engineer"}

var bootstrapSecondaryRoles = []string{"Implementation Engineer", "Technical Product Specialist", "Product Support Engineer"}

var bootstrapAlwaysEmphasize = []string{"довожу задачи до результата", "автоматизация", "интеграции", "самостоятельное решение задач"}

var bootstrapAvoidClaiming = []string{"Kubernetes production", "Kafka", "Celery production", "ML engineering", "Senior level"}

func bootstrapFact(now time.Time, label string) ProfileFact {
	return ProfileFact{
		Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now,
		Evidence: []string{"explicitly confirmed during profile bootstrap: " + label},
	}
}

func askConfirmedText(reader *bufio.Reader, out io.Writer, prompt, label string, now time.Time) (ProfileStringFact, bool, error) {
	value, err := askProfileLine(reader, out, prompt+" (Enter — unknown)")
	if err != nil || value == "" {
		return ProfileStringFact{}, false, err
	}
	confirmed, err := askYesNo(reader, out, "Подтвердить этот факт: "+value+"? (да/нет)")
	if err != nil || !confirmed {
		return ProfileStringFact{}, false, err
	}
	return ProfileStringFact{Value: value, ProfileFact: bootstrapFact(now, label)}, true, nil
}

func askYesNo(reader *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	answer, err := askProfileLine(reader, out, prompt)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "да", "д", "y", "yes":
		return true, nil
	case "", "нет", "н", "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("ожидалось да/нет, получено %q", answer)
	}
}

func askConfirmedList(reader *bufio.Reader, out io.Writer, title string, values []string, now time.Time) (ProfileListFact, bool, error) {
	confirmed, err := askYesNo(reader, out, title+"\n  "+strings.Join(values, ", ")+"\nПодтвердить? (да/нет)")
	if err != nil || !confirmed {
		return ProfileListFact{}, false, err
	}
	return ProfileListFact{Values: append([]string(nil), values...), ProfileFact: bootstrapFact(now, title)}, true, nil
}

func hasUserConfirmedSkill(profile CandidateProfile, name string) bool {
	skill, ok := profile.ResolveSkill(name)
	return ok && skill.Source == CandidateSourceUserConfirmed
}

func appendUnknownSkill(profile *CandidateProfile, name string) {
	profile.mergeSkill(CandidateSkill{Name: name, Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUnknown, Confirmed: false}})
}

func runProfileBootstrap(profile *CandidateProfile, path string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	now := time.Now()
	fmt.Fprintln(out, "Bootstrap Candidate Knowledge Base. Каждый сохранённый факт нужно явно подтвердить.")

	if profile.Identity.Location.Value == "" || !profile.Identity.Location.Confirmed {
		if fact, ok, err := askConfirmedText(reader, out, "Город", "bootstrap personal city", now); err != nil {
			return err
		} else if ok {
			profile.Identity.Location = fact
		}
	}
	educationLevel, educationLevelOK, err := askConfirmedText(reader, out, "Уровень образования", "bootstrap education level", now)
	if err != nil {
		return err
	}
	institution, institutionOK, err := askConfirmedText(reader, out, "Учебное заведение", "bootstrap education institution", now)
	if err != nil {
		return err
	}
	specialty, specialtyOK, err := askConfirmedText(reader, out, "Специальность", "bootstrap specialty", now)
	if err != nil {
		return err
	}
	if educationLevelOK || institutionOK || specialtyOK {
		fact := EducationFact{ProfileFact: bootstrapFact(now, "bootstrap education and specialty")}
		if educationLevelOK {
			fact.Level = educationLevel.Value
		}
		if institutionOK {
			fact.Institution = institution.Value
		}
		if specialtyOK {
			fact.Specialty = specialty.Value
		}
		profile.Education = append(profile.Education, fact)
	}
	if roles, ok, err := askConfirmedText(reader, out, "Желаемые роли", "bootstrap preferred roles", now); err != nil {
		return err
	} else if ok {
		profile.WorkPreferences.PreferredRoles = roles
	}

	for {
		add, err := askYesNo(reader, out, "Добавить опыт работы? (да/нет)")
		if err != nil {
			return err
		}
		if !add {
			break
		}
		company, _, err := askConfirmedText(reader, out, "Компания", "bootstrap company", now)
		if err != nil {
			return err
		}
		role, _, err := askConfirmedText(reader, out, "Роль", "bootstrap work role", now)
		if err != nil {
			return err
		}
		tasks, _, err := askConfirmedText(reader, out, "Реальные задачи", "bootstrap work tasks", now)
		if err != nil {
			return err
		}
		achievements, _, err := askConfirmedText(reader, out, "Достижения", "bootstrap work achievements", now)
		if err != nil {
			return err
		}
		fact := WorkExperienceFact{Company: company.Value, Role: role.Value, Description: tasks.Value, Achievements: achievements.Value, ProfileFact: bootstrapFact(now, "bootstrap work experience")}
		if company.Value != "" || role.Value != "" || tasks.Value != "" || achievements.Value != "" {
			profile.WorkExperience = append(profile.WorkExperience, fact)
		}
	}

	for _, name := range bootstrapSkillNames {
		if hasUserConfirmedSkill(*profile, name) {
			continue
		}
		levelText, err := askProfileLine(reader, out, fmt.Sprintf("%s: уровень 0-5 (Enter — unknown)", name))
		if err != nil {
			return err
		}
		if levelText == "" {
			appendUnknownSkill(profile, name)
			continue
		}
		level, err := parseSkillLevelAnswer(levelText)
		if err != nil {
			return fmt.Errorf("skill %s: %w", name, err)
		}
		evidence, err := askProfileLine(reader, out, "Evidence (конкретный проект/задача; Enter — unknown)")
		if err != nil {
			return err
		}
		if evidence == "" {
			appendUnknownSkill(profile, name)
			continue
		}
		confirmed, err := askYesNo(reader, out, "Подтвердить навык "+name+" с уровнем "+string(level)+" и evidence? (да/нет)")
		if err != nil {
			return err
		}
		if !confirmed {
			appendUnknownSkill(profile, name)
			continue
		}
		profile.mergeSkill(CandidateSkill{Name: name, Level: level, Negative: level == SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{evidence}}})
	}

	for _, name := range bootstrapProjectNames {
		confirmed, err := askYesNo(reader, out, "Заполнить подтверждённые данные проекта "+name+"? (да/нет)")
		if err != nil {
			return err
		}
		if !confirmed {
			continue
		}
		role, _, err := askConfirmedText(reader, out, "Роль в проекте "+name, "bootstrap project role: "+name, now)
		if err != nil {
			return err
		}
		stack, _, err := askConfirmedText(reader, out, "Стек проекта "+name, "bootstrap project stack: "+name, now)
		if err != nil {
			return err
		}
		done, _, err := askConfirmedText(reader, out, "Что сделано в проекте "+name, "bootstrap project work: "+name, now)
		if err != nil {
			return err
		}
		impact, _, err := askConfirmedText(reader, out, "Business impact проекта "+name, "bootstrap project impact: "+name, now)
		if err != nil {
			return err
		}
		fact := ProjectFact{Name: name, Role: role.Value, Description: done.Value, BusinessImpact: impact.Value, Technologies: parseResumeSkillNames(stack.Value), ProfileFact: bootstrapFact(now, "bootstrap project: "+name)}
		profile.Projects = append(profile.Projects, fact)
	}

	if confirmed, err := askYesNo(reader, out, "Подтвердить work mode: Екатеринбург office only; remote Russia/world? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.WorkMode = ProfileStringFact{Value: "Екатеринбург office only; remote Russia/world", ProfileFact: bootstrapFact(now, "bootstrap work mode")}
	}
	if confirmed, err := askYesNo(reader, out, "Подтвердить preference: no relocation? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.Relocation = ProfileStringFact{Value: "no relocation", ProfileFact: bootstrapFact(now, "bootstrap no relocation")}
	}
	if confirmed, err := askYesNo(reader, out, "Подтвердить preference: no business trips? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.BusinessTrips = ProfileStringFact{Value: "no business trips", ProfileFact: bootstrapFact(now, "bootstrap no business trips")}
	}
	if confirmed, err := askYesNo(reader, out, "Подтвердить salary minimum 50000 RUB? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.SalaryMinimum = ProfileIntFact{Value: 50000, ProfileFact: bootstrapFact(now, "bootstrap salary minimum 50000 RUB")}
	}
	if confirmed, err := askYesNo(reader, out, "Подтвердить primary roles: "+strings.Join(bootstrapPrimaryRoles, ", ")+"? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.PrimaryRoles = ProfileStringFact{Value: strings.Join(bootstrapPrimaryRoles, ", "), ProfileFact: bootstrapFact(now, "bootstrap primary roles")}
	}
	if confirmed, err := askYesNo(reader, out, "Подтвердить secondary roles: "+strings.Join(bootstrapSecondaryRoles, ", ")+"? (да/нет)"); err != nil {
		return err
	} else if confirmed {
		profile.WorkPreferences.SecondaryRoles = ProfileStringFact{Value: strings.Join(bootstrapSecondaryRoles, ", "), ProfileFact: bootstrapFact(now, "bootstrap secondary roles")}
	}
	if fact, ok, err := askConfirmedList(reader, out, "Always emphasize", bootstrapAlwaysEmphasize, now); err != nil {
		return err
	} else if ok {
		profile.EmployerCommunicationPreferences.AlwaysEmphasize = fact
	}
	if fact, ok, err := askConfirmedList(reader, out, "Avoid claiming", bootstrapAvoidClaiming, now); err != nil {
		return err
	} else if ok {
		profile.EmployerCommunicationPreferences.AvoidClaiming = fact
	}

	return SaveCandidateProfile(path, *profile)
}
