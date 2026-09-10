package runtime

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	domaincandidate "hh-ai-responder/internal/candidate"
)

type CandidateSource = domaincandidate.CandidateSource
type SkillLevel = domaincandidate.SkillLevel
type ProfileFact = domaincandidate.ProfileFact
type CandidateIdentity = domaincandidate.CandidateIdentity
type ProfileStringFact = domaincandidate.ProfileStringFact
type ProfileIntFact = domaincandidate.ProfileIntFact
type EducationFact = domaincandidate.EducationFact
type WorkExperienceFact = domaincandidate.WorkExperienceFact
type ProjectFact = domaincandidate.ProjectFact
type CandidateSkill = domaincandidate.CandidateSkill
type LanguageFact = domaincandidate.LanguageFact
type WorkPreferences = domaincandidate.WorkPreferences
type ProfileListFact = domaincandidate.ProfileListFact
type EmployerCommunicationPreferences = domaincandidate.EmployerCommunicationPreferences
type PendingProfileQuestion = domaincandidate.PendingProfileQuestion

const (
	CandidateSourceUserConfirmed  = domaincandidate.CandidateSourceUserConfirmed
	CandidateSourceHHResume       = domaincandidate.CandidateSourceHHResume
	CandidateSourceGithubVerified = domaincandidate.CandidateSourceGithubVerified
	CandidateSourceDerived        = domaincandidate.CandidateSourceDerived
	CandidateSourceUnknown        = domaincandidate.CandidateSourceUnknown
	SkillLevelUnknown             = domaincandidate.SkillLevelUnknown
	SkillLevelHeardOf             = domaincandidate.SkillLevelHeardOf
	SkillLevelBasic               = domaincandidate.SkillLevelBasic
	SkillLevelWorking             = domaincandidate.SkillLevelWorking
	SkillLevelConfident           = domaincandidate.SkillLevelConfident
	SkillLevelAdvanced            = domaincandidate.SkillLevelAdvanced
)

type CandidateProfile = domaincandidate.CandidateProfile

func NewCandidateProfile(now time.Time) CandidateProfile {
	return domaincandidate.NewProfile(now)
}

func validateCandidateSource(source CandidateSource) error {
	return domaincandidate.ValidateCandidateSource(source)
}

func sourcePriority(source CandidateSource) int {
	return domaincandidate.SourcePriority(source)
}

func sourceTrustedForEmployerCommunication(source CandidateSource) bool {
	return domaincandidate.SourceTrustedForEmployerCommunication(source)
}

func validateProfileFact(fact ProfileFact) error {
	return domaincandidate.ValidateProfileFact(fact)
}

func validateCandidateProfile(profile CandidateProfile) error {
	return domaincandidate.ValidateCandidateProfile(profile)
}

func validSkillLevel(level SkillLevel) bool {
	return domaincandidate.ValidSkillLevel(level)
}

func profileContainsSecret(raw []byte) bool {
	return domaincandidate.ContainsForbiddenSecret(raw)
}

func LoadCandidateProfile(path string) (CandidateProfile, error) {
	return jsonstorage.NewCandidateProfileStore(path).Load()
}

func SaveCandidateProfile(path string, profile CandidateProfile) error {
	return jsonstorage.NewCandidateProfileStore(path).Save(profile)
}

func ImportCandidateProfile(sourcePath, targetPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(targetPath) == "" {
		return errors.New("candidate profile import paths must not be empty")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("import source profile: %w", err)
	}

	// Load the source before touching the destination. LoadCandidateProfile is
	// strict about JSON shape, allowed sources and secret-bearing fields.
	incoming, err := LoadCandidateProfile(sourcePath)
	if err != nil {
		return fmt.Errorf("import candidate profile: %w", err)
	}

	oldRaw, oldErr := os.ReadFile(targetPath)
	hasOld := oldErr == nil
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return fmt.Errorf("read existing candidate profile: %w", oldErr)
	}
	merged := incoming
	if hasOld {
		current, err := LoadCandidateProfile(targetPath)
		if err != nil {
			return fmt.Errorf("validate existing candidate profile: %w", err)
		}
		merged = mergeCandidateProfiles(current, incoming)
	}
	if err := validateCandidateProfile(merged); err != nil {
		return fmt.Errorf("validate imported candidate profile: %w", err)
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("encode imported candidate profile: %w", err)
	}
	if profileContainsSecret(encoded) {
		return errors.New("imported candidate profile contains a forbidden secret field")
	}

	if hasOld {
		backupPath := targetPath + ".bak"
		if err := os.WriteFile(backupPath, oldRaw, 0o600); err != nil {
			return fmt.Errorf("backup existing candidate profile: %w", err)
		}
		if err := os.Chmod(backupPath, 0o600); err != nil {
			return fmt.Errorf("secure candidate profile backup: %w", err)
		}
	}
	if err := SaveCandidateProfile(targetPath, merged); err != nil {
		return fmt.Errorf("save imported candidate profile: %w", err)
	}
	return nil
}

func mergeCandidateProfiles(current, incoming CandidateProfile) CandidateProfile {
	return domaincandidate.MergeCandidateProfiles(current, incoming)
}

func normalizeProfileName(value string) string {
	return domaincandidate.NormalizeProfileName(value)
}

func canonicalSkillName(value string) string {
	return domaincandidate.CanonicalSkillName(value)
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
	storiesPath := defaultCandidateStoriesPath()
	command := ""
	importSource := ""
	var knowledgeArgs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "show", "questions", "bootstrap", "import", "stories", "communication", "knowledge":
			if command != "" {
				return profileUsageError()
			}
			command = args[i]
			if command == "knowledge" {
				for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					knowledgeArgs = append(knowledgeArgs, args[i])
				}
			}
			if command == "import" {
				if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" || strings.HasPrefix(args[i+1], "-") {
					return errors.New("usage: profile import file [-candidate-profile path]")
				}
				importSource = args[i+1]
				i++
			}
		case "-candidate-profile":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return profileUsageError()
			}
			path = args[i+1]
			i++
		case "-candidate-stories":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return profileUsageError()
			}
			storiesPath = args[i+1]
			i++
		default:
			if command == "knowledge" && !strings.HasPrefix(args[i], "-") {
				knowledgeArgs = append(knowledgeArgs, args[i])
				continue
			}
			return profileUsageError()
		}
	}
	if command == "knowledge" {
		return runKnowledgeCommand(knowledgeArgs, path, out)
	}
	if command == "import" {
		return ImportCandidateProfile(importSource, path)
	}
	if command == "communication" {
		_, err := io.WriteString(out, candidateCommunicationProfile)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(candidateCommunicationProfile, "\n") {
			_, err = io.WriteString(out, "\n")
		}
		return err
	}
	if command == "stories" {
		stories, err := LoadCandidateStories(storiesPath)
		if err != nil {
			return err
		}
		encoded, err := formatCandidateStories(stories)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, encoded)
		return err
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
		return profileUsageError()
	}
}

func profileUsageError() error {
	return errors.New("usage: profile [show|questions|bootstrap|import file|stories|communication|knowledge proposals|knowledge confirm <id>|knowledge reject <id>] [-candidate-profile path] [-candidate-stories path]")
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
			profile.RemovePendingQuestion(0)
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
	profile.MergeSkill(CandidateSkill{Name: name, Level: SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUnknown, Confirmed: false}})
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
		profile.MergeSkill(CandidateSkill{Name: name, Level: level, Negative: level == SkillLevelUnknown, ProfileFact: ProfileFact{Source: CandidateSourceUserConfirmed, Confirmed: true, ConfirmedAt: now, Evidence: []string{evidence}}})
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
