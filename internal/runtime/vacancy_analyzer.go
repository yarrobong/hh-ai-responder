package runtime

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// VacancyAnalyzer performs only local deterministic matching. It never calls
// HH, an LLM, or any application write path.
type VacancyAnalyzer struct{}

func NewVacancyAnalyzer() *VacancyAnalyzer {
	return &VacancyAnalyzer{}
}

func (a VacancyAnalyzer) Analyze(vacancy Vacancy, candidateKnowledge any) MatchResult {
	if candidate, ok := canonicalCandidateForAnalysis(candidateKnowledge); ok {
		return a.analyzeCanonical(vacancy, candidate)
	}
	// Compatibility fallback is only for legacy in-memory fixtures that cannot
	// satisfy canonical validation. It does not affect valid production data.
	return a.analyzeLegacy(vacancy, candidateKnowledge)
}

func canonicalCandidateForAnalysis(value any) (Candidate, bool) {
	switch typed := value.(type) {
	case Candidate:
		return typed, true
	case *Candidate:
		if typed == nil {
			return Candidate{}, false
		}
		return *typed, true
	case *CandidateKnowledgeBase:
		if typed == nil {
			return Candidate{}, false
		}
		candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: typed.Profile, Knowledge: *typed})
		return candidate, err == nil && len(diagnostics.Conflicts) == 0
	case CandidateKnowledgeBase:
		candidate, diagnostics, err := BuildCanonicalCandidate(CanonicalCandidateInput{Profile: typed.Profile, Knowledge: typed})
		return candidate, err == nil && len(diagnostics.Conflicts) == 0
	default:
		return Candidate{}, false
	}
}

func (a VacancyAnalyzer) analyzeCanonical(vacancy Vacancy, candidate Candidate) MatchResult {
	safe, err := CanonicalEmployerSafeProjection(candidate)
	if err != nil {
		return a.analyzeLegacy(vacancy, (*CandidateKnowledgeBase)(nil))
	}
	profile := canonicalCandidateProfile(candidate)
	profile.Version = 1
	profile.UpdatedAt = candidate.UpdatedAt
	for _, experience := range candidate.Experience {
		if safeMetadata(experience.Metadata) {
			profile.WorkExperience = append(profile.WorkExperience, WorkExperienceFact{Company: experience.Company, Role: experience.Position, Description: experience.Description, StartDate: experience.StartDate, EndDate: experience.EndDate, Achievements: firstString(experience.Achievements), ProfileFact: canonicalProfileFact(experience.Metadata)})
		}
	}
	for _, project := range candidate.Projects {
		if project.ProfileSource != nil && safeProfileFact(project.ProfileSource.ProfileFact) {
			profile.Projects = append(profile.Projects, *project.ProfileSource)
		}
	}
	for _, skill := range candidate.Skills {
		for _, assertion := range skill.SourceAssertions {
			if !assertion.Detailed && safeMetadata(assertion.Metadata) {
				profile.Skills = append(profile.Skills, CandidateSkill{Name: assertion.Name, Level: assertion.Level, Negative: assertion.Negative, ProfileFact: canonicalProfileFact(assertion.Metadata)})
			}
		}
	}
	return a.analyzeWithKnowledge(vacancy, &CandidateKnowledgeBase{Profile: profile, Skills: safe.Skills, Projects: safe.Projects, Achievements: safe.Achievements})
}

// Analyze returns a recommendation, not an instruction to apply. Candidate
// hypotheses and unknowns are deliberately excluded from positive evidence.
func (a VacancyAnalyzer) analyzeLegacy(vacancy Vacancy, candidateKnowledge any) MatchResult {
	return a.analyzeWithKnowledge(vacancy, candidateKnowledgeBaseValue(candidateKnowledge))
}

func (a VacancyAnalyzer) analyzeWithKnowledge(vacancy Vacancy, kb *CandidateKnowledgeBase) MatchResult {
	result := MatchResult{
		MatchedSkills:   []string{},
		UnknownSkills:   []string{},
		MatchedRoles:    []string{},
		MatchedProjects: []string{},
		MissingSkills:   []string{},
		Risks:           []string{},
		RiskDetails:     []MatchRisk{},
		Recommendations: []string{},
	}

	safe := employerSafeKnowledgeForAnalysis(kb)
	requiredSkills := vacancyRequiredSkills(vacancy)
	for _, required := range requiredSkills {
		status, matched := candidateSkillStatus(required, safe.Skills)
		switch status {
		case "matched":
			result.MatchedSkills = appendUniqueCanonical(result.MatchedSkills, matched)
		case "missing":
			result.MissingSkills = appendUniqueCanonical(result.MissingSkills, required)
		default:
			result.UnknownSkills = appendUniqueCanonical(result.UnknownSkills, required)
		}
	}

	roles := trustedCandidateRoles(kb, safe)
	vacancyTitle := firstNonEmpty(vacancy.Title, vacancy.Name)
	for _, role := range roles {
		if rolesMatch(vacancyTitle, role) {
			result.MatchedRoles = appendUniqueCanonical(result.MatchedRoles, role)
		}
	}

	experience := analyzeExperience(vacancy, kb)
	result.ExperienceNote = experience.note

	if len(result.MissingSkills) > 0 {
		for _, skill := range result.MissingSkills {
			addRisk(&result, MatchRisk{Type: "missing_skill", Description: "обязательный навык отсутствует: " + skill})
		}
	}
	for _, skill := range result.UnknownSkills {
		addRisk(&result, MatchRisk{Type: "unknown_skill", Description: "неизвестна подтверждённость навыка: " + skill})
	}
	if experience.risk != "" {
		addRisk(&result, MatchRisk{Type: "experience", Description: experience.risk})
	}
	if locationRisk := analyzeLocationRisk(vacancy, kb); locationRisk != "" {
		addRisk(&result, MatchRisk{Type: "location", Description: locationRisk})
	}
	if workFormatRisk := analyzeWorkFormatRisk(vacancy, kb); workFormatRisk != "" {
		addRisk(&result, MatchRisk{Type: "work_format", Description: workFormatRisk})
	}

	result.Score = calculateVacancyScore(result, requiredSkills, roles, vacancyTitle, experience, safe.Skills)
	result.Confidence = calculateConfidence(requiredSkills, safe.Skills)
	result.Explanation = buildMatchExplanation(result)
	recommendation := recommendationFor(result, roles)
	result.Recommendation = &recommendation
	result.Recommendations = []string{recommendation.Reason}
	return result
}

func candidateKnowledgeBaseValue(value any) *CandidateKnowledgeBase {
	switch kb := value.(type) {
	case *CandidateKnowledgeBase:
		return kb
	case CandidateKnowledgeBase:
		copy := kb
		return &copy
	default:
		return nil
	}
}

func employerSafeKnowledgeForAnalysis(kb *CandidateKnowledgeBase) EmployerSafeKnowledge {
	if kb == nil {
		return EmployerSafeKnowledge{Skills: []CandidateSkillDetailed{}, Projects: []CandidateProject{}, Achievements: []CandidateAchievement{}}
	}
	safe, err := kb.GetEmployerSafeKnowledge()
	if err != nil {
		return EmployerSafeKnowledge{Skills: []CandidateSkillDetailed{}, Projects: []CandidateProject{}, Achievements: []CandidateAchievement{}}
	}
	return safe
}

func vacancyRequiredSkills(v Vacancy) []string {
	result := []string{}
	for _, skill := range v.Skills {
		result = appendUniqueCanonical(result, skill)
	}
	if len(result) > 0 {
		return result
	}
	text := strings.Join(append(append([]string{}, v.Requirements...), v.Description), "\n")
	for _, name := range contextTechnologyNames {
		if contextMentions(text, name) {
			result = appendUniqueCanonical(result, name)
		}
	}
	// Short, explicit requirements are often imported without a separate
	// skills array (for example []string{"Python", "Django"}).
	for _, requirement := range v.Requirements {
		if len(strings.Fields(requirement)) <= 3 && !strings.ContainsAny(requirement, ".,;:!?()") {
			result = appendUniqueCanonical(result, requirement)
		}
	}
	return result
}

func candidateSkillStatus(required string, skills []CandidateSkillDetailed) (string, string) {
	for _, skill := range skills {
		if contextCanonical(skill.Name) != contextCanonical(required) {
			continue
		}
		if skill.Negative {
			return "missing", required
		}
		switch skill.TruthStatus {
		case TruthStatusConfirmed, TruthStatusVerified:
			if skill.Level == SkillLevelUnknown || skill.Level == SkillLevelHeardOf {
				return "unknown", required
			}
			return "matched", skill.Name
		default:
			return "unknown", required
		}
	}
	return "unknown", required
}

func trustedCandidateRoles(kb *CandidateKnowledgeBase, safe EmployerSafeKnowledge) []string {
	result := []string{}
	if kb != nil {
		for _, fact := range []ProfileStringFact{
			kb.Profile.WorkPreferences.PrimaryRoles,
			kb.Profile.WorkPreferences.SecondaryRoles,
			kb.Profile.WorkPreferences.PreferredRoles,
		} {
			if !fact.Confirmed {
				continue
			}
			for _, role := range splitFactValues(fact.Value) {
				result = appendUniqueCanonical(result, role)
			}
		}
		for _, experience := range kb.Profile.WorkExperience {
			if experience.Confirmed {
				result = appendUniqueCanonical(result, experience.Role)
			}
		}
	}
	for _, project := range safe.Projects {
		result = appendUniqueCanonical(result, project.Role)
	}
	return result
}

func splitFactValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '|' })
	result := []string{}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, strings.TrimSpace(part))
		}
	}
	return result
}

func rolesMatch(vacancyTitle, candidateRole string) bool {
	if contextCanonical(vacancyTitle) == contextCanonical(candidateRole) || contextMentions(vacancyTitle, candidateRole) {
		return true
	}
	vacancyCategories := roleCategories(vacancyTitle)
	candidateCategories := roleCategories(candidateRole)
	for category := range vacancyCategories {
		if candidateCategories[category] {
			return true
		}
	}
	return false
}

func roleCategories(value string) map[string]bool {
	text := strings.ToLower(value)
	categories := map[string]bool{}
	if strings.Contains(text, "backend") || strings.Contains(text, "back-end") || strings.Contains(text, "бекенд") || strings.Contains(text, "бэкенд") || strings.Contains(text, "сервер") {
		categories["backend"] = true
	}
	if strings.Contains(text, "full-stack") || strings.Contains(text, "fullstack") || strings.Contains(text, "full stack") || strings.Contains(text, "фуллстек") {
		categories["fullstack"] = true
	}
	if strings.Contains(text, "developer") || strings.Contains(text, "программист") || strings.Contains(text, "разработчик") {
		categories["developer"] = true
	}
	if strings.Contains(text, "support") || strings.Contains(text, "поддерж") {
		categories["support"] = true
	}
	if strings.Contains(text, "integration") || strings.Contains(text, "интеграц") {
		categories["integration"] = true
	}
	if strings.Contains(text, "automation") || strings.Contains(text, "автоматизац") {
		categories["automation"] = true
	}
	return categories
}

type experienceAnalysis struct {
	minimumMonths   int
	maximumMonths   int
	candidateMonths int
	known           bool
	close           bool
	note            string
	risk            string
}

var experienceRangeRE = regexp.MustCompile("(?i)([0-9]+)\\s*-\\s*([0-9]+)\\s*(лет|года|год|месяц(?:а|ев|ы)?|years?|months?)")
var experienceMinimumRE = regexp.MustCompile("(?i)(?:от|more than|at least|minimum)\\s*([0-9]+)\\s*(лет|года|год|месяц(?:а|ев|ы)?|years?|months?)")

func analyzeExperience(v Vacancy, kb *CandidateKnowledgeBase) experienceAnalysis {
	text := strings.Join([]string{v.WorkExperience, strings.Join(v.Requirements, " "), v.Description}, " ")
	minimum, maximum := 0, 0
	if match := experienceRangeRE.FindStringSubmatch(text); len(match) == 4 {
		minimum, _ = strconv.Atoi(match[1])
		maximum, _ = strconv.Atoi(match[2])
		if !strings.Contains(strings.ToLower(match[3]), "месяц") && !strings.Contains(strings.ToLower(match[3]), "month") {
			minimum, maximum = minimum*12, maximum*12
		}
	} else if match := experienceMinimumRE.FindStringSubmatch(text); len(match) == 3 {
		minimum, _ = strconv.Atoi(match[1])
		if !strings.Contains(strings.ToLower(match[2]), "месяц") && !strings.Contains(strings.ToLower(match[2]), "month") {
			minimum *= 12
		}
	}
	if minimum == 0 && maximum == 0 {
		return experienceAnalysis{note: "Требуемый опыт не указан"}
	}
	analysis := experienceAnalysis{minimumMonths: minimum, maximumMonths: maximum}
	if kb != nil && kb.Profile.TotalExperienceMonths.Confirmed && kb.Profile.TotalExperienceMonths.Value >= 0 {
		analysis.candidateMonths = kb.Profile.TotalExperienceMonths.Value
		analysis.known = true
	}
	if !analysis.known {
		analysis.note = "Опыт кандидата неизвестен, требуется уточнение"
		analysis.risk = "требуемый опыт не сопоставлен с подтверждённым опытом"
		return analysis
	}
	if analysis.candidateMonths >= minimum {
		analysis.note = "Подтверждённый опыт соответствует требованию"
		return analysis
	}
	gap := minimum - analysis.candidateMonths
	if gap <= 2 {
		analysis.close = true
		analysis.note = "Близкий уровень опыта, рассматривать"
	} else {
		analysis.note = fmt.Sprintf("Подтверждённый опыт ниже требования на %d мес.", gap)
	}
	analysis.risk = fmt.Sprintf("недостаточно подтверждённого опыта: требуется %s", formatMonths(minimum))
	return analysis
}

func formatMonths(months int) string {
	if months%12 == 0 {
		return strconv.Itoa(months/12) + " лет"
	}
	return strconv.Itoa(months) + " мес."
}

func analyzeLocationRisk(v Vacancy, kb *CandidateKnowledgeBase) string {
	required := firstNonEmpty(v.Location, v.Area.Name)
	if strings.TrimSpace(required) == "" || kb == nil || !kb.Profile.Identity.Location.Confirmed || strings.TrimSpace(kb.Profile.Identity.Location.Value) == "" {
		return ""
	}
	candidate := kb.Profile.Identity.Location.Value
	if contextCanonical(required) == contextCanonical(candidate) || strings.Contains(strings.ToLower(required), strings.ToLower(candidate)) || strings.Contains(strings.ToLower(candidate), strings.ToLower(required)) {
		return ""
	}
	return fmt.Sprintf("требуется офис/локация %q, подтверждённая локация кандидата — %q", required, candidate)
}

func analyzeWorkFormatRisk(v Vacancy, kb *CandidateKnowledgeBase) string {
	required := firstNonEmpty(v.WorkFormat, v.WorkSchedule)
	if strings.TrimSpace(required) == "" || kb == nil || !kb.Profile.WorkPreferences.WorkMode.Confirmed {
		return ""
	}
	preferred := kb.Profile.WorkPreferences.WorkMode.Value
	if contextCanonical(required) == contextCanonical(preferred) || strings.Contains(strings.ToLower(preferred), strings.ToLower(required)) || strings.Contains(strings.ToLower(required), strings.ToLower(preferred)) {
		return ""
	}
	return fmt.Sprintf("формат работы %q отличается от подтверждённого предпочтения кандидата %q", required, preferred)
}

func calculateVacancyScore(result MatchResult, requiredSkills, roles []string, title string, experience experienceAnalysis, skills []CandidateSkillDetailed) int {
	skillScore := 0.5
	if len(requiredSkills) > 0 {
		skillScore = 0
		for _, required := range requiredSkills {
			weight := candidateSkillEvidenceWeight(required, skills)
			if weight == 0.25 {
				weight = 0.55
			}
			skillScore += weight
		}
		skillScore /= float64(len(requiredSkills))
	}
	roleScore := 0.5
	if len(roles) > 0 {
		roleScore = 0
		if len(result.MatchedRoles) > 0 {
			roleScore = 1
		}
	} else if strings.TrimSpace(title) == "" {
		roleScore = 0.5
	}
	experienceScore := 0.5
	if experience.known {
		switch {
		case experience.candidateMonths >= experience.minimumMonths:
			experienceScore = 1
		case experience.close:
			experienceScore = 0.8
		default:
			experienceScore = 0.4
		}
	}
	locationScore := 1.0
	for _, risk := range result.RiskDetails {
		if risk.Type == "location" || risk.Type == "work_format" {
			locationScore = 0.5
		}
	}
	score := 60*skillScore + 20*roleScore + 15*experienceScore + 5*locationScore
	return clampInt(int(math.Round(score)), 0, 100)
}

func calculateConfidence(requiredSkills []string, skills []CandidateSkillDetailed) float64 {
	if len(requiredSkills) == 0 {
		return 0.5
	}
	confidence := 0.0
	for _, required := range requiredSkills {
		confidence += candidateSkillEvidenceWeight(required, skills)
	}
	return math.Round(confidence/float64(len(requiredSkills))*100) / 100
}

func candidateSkillEvidenceWeight(required string, skills []CandidateSkillDetailed) float64 {
	for _, skill := range skills {
		if contextCanonical(skill.Name) != contextCanonical(required) {
			continue
		}
		if skill.Negative {
			return 1
		}
		switch skill.TruthStatus {
		case TruthStatusConfirmed:
			if skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
				return 1
			}
		case TruthStatusVerified:
			if skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
				return 0.9
			}
		}
		return 0.25
	}
	return 0.25
}

func recommendationFor(result MatchResult, roles []string) ApplicationRecommendation {
	if len(result.MissingSkills) > 0 {
		return ApplicationRecommendation{Decision: RecommendationSkip, Reason: "Есть явно отсутствующие обязательные навыки; решение требует ручной проверки."}
	}
	if result.Score >= 75 && len(result.UnknownSkills) == 0 && (len(roles) == 0 || len(result.MatchedRoles) > 0) {
		return ApplicationRecommendation{Decision: RecommendationApply, Reason: "Сильное совпадение подтверждённых навыков и релевантной роли."}
	}
	if result.Score >= 60 {
		return ApplicationRecommendation{Decision: RecommendationMaybe, Reason: "Есть релевантное совпадение, но часть требований или контекста требует проверки."}
	}
	return ApplicationRecommendation{Decision: RecommendationSkip, Reason: "Совпадение недостаточно сильное для положительной рекомендации."}
}

func buildMatchExplanation(result MatchResult) string {
	parts := []string{}
	if len(result.MatchedSkills) > 0 {
		parts = append(parts, "подтверждённые навыки: "+strings.Join(result.MatchedSkills, ", "))
	}
	if len(result.UnknownSkills) > 0 {
		parts = append(parts, "неизвестные навыки: "+strings.Join(result.UnknownSkills, ", "))
	}
	if len(result.MissingSkills) > 0 {
		parts = append(parts, "отсутствующие навыки: "+strings.Join(result.MissingSkills, ", "))
	}
	if len(result.MatchedRoles) > 0 {
		parts = append(parts, "релевантная роль: "+strings.Join(result.MatchedRoles, ", "))
	}
	if result.ExperienceNote != "" {
		parts = append(parts, result.ExperienceNote)
	}
	if len(parts) == 0 {
		return "Данных для детерминированного сопоставления недостаточно."
	}
	return strings.Join(parts, "; ")
}

func addRisk(result *MatchResult, risk MatchRisk) {
	if result == nil || strings.TrimSpace(risk.Description) == "" {
		return
	}
	for _, existing := range result.RiskDetails {
		if existing.Type == risk.Type && existing.Description == risk.Description {
			return
		}
	}
	result.RiskDetails = append(result.RiskDetails, risk)
	result.Risks = appendUniqueCanonical(result.Risks, risk.Description)
}

func appendUniqueCanonical(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if contextCanonical(existing) == contextCanonical(value) {
			return values
		}
	}
	return append(values, value)
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
