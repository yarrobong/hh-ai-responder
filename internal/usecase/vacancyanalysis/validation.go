package vacancyanalysis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/vacancy"
)

var hardRequirementCategories = map[string]struct{}{
	HardRequirementCategoryEducation: {}, HardRequirementCategoryLocation: {}, HardRequirementCategoryExperienceYears: {},
	HardRequirementCategorySkill: {}, HardRequirementCategoryLanguage: {}, HardRequirementCategoryLicense: {},
	HardRequirementCategoryCitizenship: {}, HardRequirementCategoryOther: {},
}

var hardRequirementStatuses = map[string]struct{}{
	HardRequirementStatusMet: {}, HardRequirementStatusMissing: {}, HardRequirementStatusUnknown: {},
}

func ParseAIResponse(answer string) (AIResponse, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(answer))
	if err := decoder.Decode(&raw); err != nil {
		return AIResponse{}, fmt.Errorf("invalid vacancy evaluation JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AIResponse{}, errors.New("invalid vacancy evaluation JSON: trailing data")
	}
	for _, field := range []string{"score", "apply", "reasons", "missing", "hard_requirements"} {
		if value, ok := raw[field]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return AIResponse{}, fmt.Errorf("vacancy evaluation is incomplete: missing %s", field)
		}
	}
	if err := validateHardRequirementJSONShape(raw["hard_requirements"]); err != nil {
		return AIResponse{}, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return AIResponse{}, fmt.Errorf("invalid vacancy evaluation JSON: %w", err)
	}
	result := AIResponse{}
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&result); err != nil {
		return AIResponse{}, fmt.Errorf("invalid vacancy evaluation JSON: %w", err)
	}
	if err := ValidateAIResponse(result); err != nil {
		return AIResponse{}, err
	}
	return result, nil
}

func ValidateAIResponse(value AIResponse) error {
	if value.Score < 0 || value.Score > 100 {
		return fmt.Errorf("vacancy evaluation score must be between 0 and 100, got %d", value.Score)
	}
	if value.Reasons == nil || value.Missing == nil || value.HardRequirements == nil {
		return errors.New("vacancy evaluation is incomplete: reasons, missing, and hard_requirements are required arrays")
	}
	if value.Recommendation != "" && !validRecommendation(value.Recommendation) {
		return fmt.Errorf("invalid AI recommendation %q", value.Recommendation)
	}
	if err := validateRecommendationReasons(value.RecommendationReasons); err != nil {
		return err
	}
	for _, requirement := range value.HardRequirements {
		if err := validateHardRequirementCandidateShape(requirement); err != nil {
			return err
		}
	}
	return nil
}

func validRecommendation(value string) bool {
	switch value {
	case RecommendationApply, RecommendationDoNotApply, RecommendationUncertain:
		return true
	default:
		return false
	}
}

func validateRecommendationReasons(values []string) error {
	if len(values) > 3 {
		return fmt.Errorf("too many AI recommendation reasons: %d", len(values))
	}
	for _, value := range values {
		switch value {
		case RecommendationReasonRoleMismatch, RecommendationReasonStackMismatch, RecommendationReasonSeniorityGap,
			RecommendationReasonHardRequirement, RecommendationReasonLowOverallFit, RecommendationReasonLocationConcern,
			RecommendationReasonOther:
		default:
			return fmt.Errorf("invalid AI recommendation reason %q", value)
		}
	}
	return nil
}

func ValidateAssessment(value Assessment) error {
	if value.Score < 0 || value.Score > 100 {
		return fmt.Errorf("vacancy evaluation score must be between 0 and 100, got %d", value.Score)
	}
	if value.Reasons == nil || value.Missing == nil || value.HardRequirements == nil {
		return errors.New("vacancy evaluation is incomplete: reasons, missing, and hard_requirements are required arrays")
	}
	if value.Recommendation != "" && !validRecommendation(value.Recommendation) {
		return fmt.Errorf("invalid AI recommendation %q", value.Recommendation)
	}
	if err := validateRecommendationReasons(value.RecommendationReasons); err != nil {
		return err
	}
	for _, requirement := range value.HardRequirements {
		if err := validateHardRequirementShape(requirement); err != nil {
			return err
		}
	}
	return nil
}

func IsOptionalRequirement(value HardRequirementEvaluation) bool {
	return isOptionalRequirement(value)
}

func LocationRequirementMatchesCandidate(candidateLocation, requirement, evidence string) bool {
	return locationRequirementMatchesCandidate(candidateLocation, requirement, evidence)
}

func validateHardRequirementJSONShape(raw json.RawMessage) error {
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return fmt.Errorf("invalid hard_requirements: %w", err)
	}
	if values == nil {
		return errors.New("hard_requirements must be an array")
	}
	for index, rawValue := range values {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawValue, &fields); err != nil {
			return fmt.Errorf("invalid hard_requirements[%d]: %w", index, err)
		}
		for _, field := range []string{"requirement", "category", "vacancy_evidence"} {
			value, ok := fields[field]
			if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return fmt.Errorf("hard_requirements[%d] is missing %s", index, field)
			}
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("invalid hard_requirements[%d]: %w", index, err)
		}
		var requirement HardRequirementCandidate
		strict := json.NewDecoder(bytes.NewReader(encoded))
		strict.DisallowUnknownFields()
		if err := strict.Decode(&requirement); err != nil {
			return fmt.Errorf("invalid hard_requirements[%d]: %w", index, err)
		}
	}
	return nil
}

func validateHardRequirementCandidateShape(value HardRequirementCandidate) error {
	if strings.TrimSpace(value.Requirement) == "" {
		return errors.New("hard requirement candidate must have a non-empty requirement")
	}
	if _, ok := hardRequirementCategories[value.Category]; !ok {
		return fmt.Errorf("invalid hard requirement category %q", value.Category)
	}
	if strings.TrimSpace(value.VacancyEvidence) == "" {
		return fmt.Errorf("hard requirement candidate %q has empty vacancy_evidence", value.Requirement)
	}
	return nil
}

func validateHardRequirementShape(value HardRequirementEvaluation) error {
	if strings.TrimSpace(value.Requirement) == "" {
		return errors.New("hard requirement must have a non-empty requirement")
	}
	if _, ok := hardRequirementCategories[value.Category]; !ok {
		return fmt.Errorf("invalid hard requirement category %q", value.Category)
	}
	if _, ok := hardRequirementStatuses[value.Status]; !ok {
		return fmt.Errorf("invalid hard requirement status %q", value.Status)
	}
	if strings.TrimSpace(value.VacancyEvidence) == "" {
		return fmt.Errorf("hard requirement %q has empty vacancy_evidence", value.Requirement)
	}
	if value.Status != HardRequirementStatusUnknown && strings.TrimSpace(value.CandidateEvidence) == "" {
		return fmt.Errorf("hard requirement %q has empty candidate_evidence for status %s", value.Requirement, value.Status)
	}
	return nil
}

var numericExperiencePattern = regexp.MustCompile(`(?i)([0-9]+)\s*(?:\+\s*)?(лет|года|год|месяц(?:а|ев|ы)?|years?|months?)`)
var experienceRangePattern = regexp.MustCompile(`(?i)([0-9]+)\s*-\s*[0-9]+\s*(лет|года|год|месяц(?:а|ев|ы)?|years?|months?)`)

var roleSpecificExperienceMarkers = []string{"sre", "devops", "devsecops", "java", "python", "django", "fastapi", "golang", "go ", "php", "ruby", "kotlin", "swift", "javascript", "typescript", "c#", "c++", "kubernetes", "qa", "тестиров", "разработчик", "разработке", "backend", "back-end", "frontend", "front-end", "fullstack", "full-stack", "data engineer", "data analyst", "аналитик", "поддержк"}

func DeriveHardRequirements(candidate CandidateFacts, value vacancy.Vacancy, description string, candidates []HardRequirementCandidate) []HardRequirementEvaluation {
	result := make([]HardRequirementEvaluation, 0, len(candidates))
	for _, requirement := range candidates {
		if validateErr := validateHardRequirementCandidateShape(requirement); validateErr != nil {
			continue
		}
		if requirement.Category == HardRequirementCategoryExperienceYears && experienceRequirementIsNonRequirement(requirement) {
			continue
		}
		if requirement.Category == HardRequirementCategoryExperienceYears && genericHHExperienceEvidenceOnly(value, description, requirement) {
			continue
		}
		if requirement.Category == HardRequirementCategoryLocation && locationRequirementIsNonBlocking(value, description, requirement) {
			continue
		}
		if !hardRequirementEvidencePresent(value, description, requirement) || isOptionalRequirementCandidate(value, description, requirement) {
			continue
		}
		status, evidence := deriveHardRequirementStatus(candidate, requirement)
		result = append(result, HardRequirementEvaluation{
			Requirement:       strings.TrimSpace(requirement.Requirement),
			Category:          requirement.Category,
			Status:            status,
			VacancyEvidence:   strings.TrimSpace(requirement.VacancyEvidence),
			CandidateEvidence: evidence,
			Soft:              requirement.Category == HardRequirementCategoryExperienceYears && status == HardRequirementStatusUnknown && genericDescriptionExperienceSoftGap(candidate, requirement),
			Telemetry:         buildRequirementTelemetry(candidate, value, description, requirement, status),
		})
	}
	return result
}

// A city in HH's area field is not by itself a relocation or onsite
// requirement. Only explicit workplace/relocation evidence can enter the hard
// requirement set; a remote vacancy must not manufacture a city blocker.
func locationRequirementIsNonBlocking(value vacancy.Vacancy, description string, requirement HardRequirementCandidate) bool {
	text := strings.ToLower(strings.Join([]string{requirement.Requirement, requirement.VacancyEvidence, value.WorkSchedule, description}, " "))
	if containsAnyText(text, "релокац", "переезд", "relocat") {
		return false
	}
	mode := classifyWorkMode(strings.Join([]string{requirement.Requirement, requirement.VacancyEvidence, value.WorkSchedule, description}, " "))
	if mode == WorkModeOfficeAvailable || mode == WorkModeRemoteAvailable {
		return true
	}
	if containsAnyText(text, "удалён", "удален", "remote", "дистанцион") && !containsAnyText(text, "офис", "office", "onsite", "on-site", "на месте") {
		return true
	}
	return !containsAnyText(requirement.Requirement+" "+requirement.VacancyEvidence, "офис", "office", "onsite", "on-site", "на месте")
}

func containsAnyText(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func DeriveHardRequirementStatus(candidate CandidateFacts, requirement HardRequirementCandidate) (string, string) {
	return deriveHardRequirementStatus(candidate, requirement)
}

func deriveHardRequirementStatus(candidate CandidateFacts, requirement HardRequirementCandidate) (string, string) {
	unknown := "not provided"
	switch requirement.Category {
	case HardRequirementCategoryEducation:
		if !candidate.EducationKnown {
			return HardRequirementStatusUnknown, unknown
		}
		matches, supported := educationRequirementMatches(candidate.EducationLevel, requirement.Requirement)
		if !supported {
			return HardRequirementStatusUnknown, unknown
		}
		if matches {
			return HardRequirementStatusMet, candidateEducationEvidence(candidate)
		}
		return HardRequirementStatusMissing, candidateEducationEvidence(candidate)
	case HardRequirementCategoryExperienceYears:
		minimum, supported, generic := descriptionExperienceMinimumMonths(requirement.Requirement, requirement.VacancyEvidence)
		if supported && generic {
			return genericDescriptionExperienceStatus(candidate, minimum)
		}
		return HardRequirementStatusUnknown, unknown
	case HardRequirementCategoryLocation:
		if locationRequirementMatchesCandidate(candidate.Location, requirement.Requirement, requirement.VacancyEvidence) {
			return HardRequirementStatusMet, "Candidate location: " + candidate.Location
		}
		return HardRequirementStatusUnknown, unknown
	case HardRequirementCategorySkill, HardRequirementCategoryLanguage, HardRequirementCategoryOther:
		if candidateRequirementMentioned(candidate, requirement.Requirement) {
			return HardRequirementStatusMet, "Candidate skills/experience mention: " + requirement.Requirement
		}
		if explicitNegativeCandidateFact(candidate, requirement.Requirement) {
			return HardRequirementStatusMissing, "Explicit negative candidate fact for: " + requirement.Requirement
		}
		return HardRequirementStatusUnknown, unknown
	default:
		return HardRequirementStatusUnknown, unknown
	}
}

func hardRequirementEvidencePresent(value vacancy.Vacancy, description string, requirement HardRequirementCandidate) bool {
	switch requirement.Category {
	case HardRequirementCategoryLocation:
		return containsNormalizedText(value.WorkSchedule, requirement.VacancyEvidence) || containsNormalizedText(description, requirement.VacancyEvidence)
	case HardRequirementCategoryExperienceYears:
		return containsNormalizedText(value.WorkExperience, requirement.VacancyEvidence) || containsNormalizedText(description, requirement.VacancyEvidence)
	default:
		return containsNormalizedText(description, requirement.VacancyEvidence)
	}
}

func isOptionalRequirementCandidate(value vacancy.Vacancy, description string, requirement HardRequirementCandidate) bool {
	source := description
	if requirement.Category == HardRequirementCategoryLocation {
		source = strings.Join([]string{value.Area.Name, value.WorkSchedule, description}, " | ")
	}
	if requirement.Category == HardRequirementCategoryExperienceYears {
		source = strings.Join([]string{value.WorkExperience, description}, " | ")
	}
	classification := classifyRequirementContext(source, requirement.VacancyEvidence)
	return classification.Classification == RequirementExtractionPreference
}

func normalizeEvidenceText(value string) string {
	value = strings.ReplaceAll(value, "–", "-")
	value = strings.ReplaceAll(value, "—", "-")
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func containsNormalizedText(haystack, needle string) bool {
	needle = normalizeEvidenceText(needle)
	return needle != "" && strings.Contains(normalizeEvidenceText(haystack), needle)
}

func candidateRequirementMentioned(candidate CandidateFacts, requirement string) bool {
	return resolveCandidateEvidence(candidate, requirement).Matched
}

type CandidateEvidenceMatch struct {
	Matched    bool
	Provenance string
}

func resolveCandidateEvidence(candidate CandidateFacts, requirement string) CandidateEvidenceMatch {
	if skill, ok := candidate.Profile.ResolveSkill(requirement); ok {
		if skill.Negative || skill.Level == domaincandidate.SkillLevelUnknown || skill.Level == domaincandidate.SkillLevelHeardOf {
			return CandidateEvidenceMatch{}
		}
		return CandidateEvidenceMatch{Matched: true, Provenance: profileFactProvenance(skill.ProfileFact)}
	}
	if explicitTermMentioned(candidate.Skills, requirement) || explicitTermMentioned(candidate.Experience, requirement) {
		return CandidateEvidenceMatch{Matched: true, Provenance: CandidateEvidenceLegacyAggregate}
	}
	return CandidateEvidenceMatch{}
}

func candidateEvidenceProvenance(match CandidateEvidenceMatch) string {
	if !match.Matched {
		return CandidateEvidenceNone
	}
	if match.Provenance == "" {
		return CandidateEvidenceUnknownSource
	}
	return match.Provenance
}

func profileFactProvenance(fact domaincandidate.ProfileFact) string {
	switch fact.Source {
	case domaincandidate.CandidateSourceHHResume:
		return CandidateEvidenceHHResume
	case domaincandidate.CandidateSourceUserConfirmed:
		return CandidateEvidenceProfile
	case domaincandidate.CandidateSourceGithubVerified:
		return CandidateEvidenceTrustedProject
	case domaincandidate.CandidateSourceDerived:
		return CandidateEvidenceUnknownSource
	default:
		return CandidateEvidenceUnknownSource
	}
}

func buildRequirementTelemetry(candidate CandidateFacts, value vacancy.Vacancy, description string, requirement HardRequirementCandidate, status string) *RequirementTelemetry {
	source, sourceField := requirementSource(value, description, requirement)
	classification := classifyRequirementContext(source, requirement.VacancyEvidence)
	telemetry := &RequirementTelemetry{
		SourceContext:               classification.SourceContext,
		SourceField:                 sourceField,
		ExtractionClassification:    classification.Classification,
		MandatoryCue:                classification.MandatoryCue,
		CandidateEvidenceProvenance: requirementCandidateProvenance(candidate, requirement, status),
		ExperienceClassification:    experienceRequirementClassification(requirement),
		ClassificationDiagnostics:   append([]string(nil), classification.ClassificationDiagnostics...),
	}
	if telemetry.ExperienceClassification == ExperienceClassificationTechnology && containsAIMLNLPMarker(requirement.Requirement+" "+requirement.VacancyEvidence) {
		telemetry.ClassificationDiagnostics = append(telemetry.ClassificationDiagnostics, "role_marker:ai_ml_nlp")
	}
	return telemetry
}

func containsAIMLNLPMarker(value string) bool {
	normalized := strings.ToLower(strings.NewReplacer("/", " ", "-", " ", "_", " ").Replace(value))
	return containsExactToken(normalized, "ai") || containsExactToken(normalized, "ml") || containsExactToken(normalized, "nlp")
}

func requirementSource(value vacancy.Vacancy, description string, requirement HardRequirementCandidate) (string, string) {
	switch requirement.Category {
	case HardRequirementCategoryExperienceYears:
		if containsNormalizedText(value.WorkExperience, requirement.VacancyEvidence) {
			return value.WorkExperience, "work_experience"
		}
		return description, "description"
	case HardRequirementCategoryLocation:
		if containsNormalizedText(value.WorkSchedule, requirement.VacancyEvidence) {
			return value.WorkSchedule, "work_schedule"
		}
		return description, "description"
	default:
		return description, "description"
	}
}

func requirementCandidateProvenance(candidate CandidateFacts, requirement HardRequirementCandidate, status string) string {
	if status == HardRequirementStatusUnknown {
		return CandidateEvidenceNone
	}
	if requirement.Category == HardRequirementCategoryExperienceYears {
		if !candidate.TotalExperienceMonthsKnown {
			return CandidateEvidenceNone
		}
		if candidate.Profile.TotalExperienceMonths.Source != "" {
			return profileFactProvenance(candidate.Profile.TotalExperienceMonths.ProfileFact)
		}
		return CandidateEvidenceLegacyAggregate
	}
	if requirement.Category == HardRequirementCategoryEducation {
		if !candidate.EducationKnown {
			return CandidateEvidenceNone
		}
		if len(candidate.Profile.Education) > 0 && candidate.Profile.Education[0].Source != "" {
			return profileFactProvenance(candidate.Profile.Education[0].ProfileFact)
		}
		return CandidateEvidenceUnknownSource
	}
	return candidateEvidenceProvenance(resolveCandidateEvidence(candidate, requirement.Requirement))
}

func experienceRequirementClassification(requirement HardRequirementCandidate) string {
	if requirement.Category != HardRequirementCategoryExperienceYears {
		return ExperienceClassificationNotDuration
	}
	_, supported, generic := descriptionExperienceMinimumMonths(requirement.Requirement, requirement.VacancyEvidence)
	if !supported {
		return ExperienceClassificationNotDuration
	}
	if generic {
		return ExperienceClassificationGenericTotal
	}
	if containsRoleSpecificExperienceMarker(normalizeEvidenceText(requirement.Requirement + " " + requirement.VacancyEvidence)) {
		return ExperienceClassificationTechnology
	}
	return ExperienceClassificationRoleSpecific
}

func filterUnsupportedPositiveClaims(candidate CandidateFacts, claims []string, requirements []HardRequirementCandidate) []string {
	if claims == nil {
		return nil
	}
	result := make([]string, 0, len(claims))
	for _, claim := range claims {
		unsupported := false
		lowerClaim := strings.ToLower(claim)
		for _, requirement := range requirements {
			term := strings.ToLower(strings.TrimSpace(requirement.Requirement))
			if term == "" || !strings.Contains(lowerClaim, term) {
				continue
			}
			if candidateRequirementMentioned(candidate, requirement.Requirement) {
				continue
			}
			if containsNegativeMarker(lowerClaim) || strings.Contains(lowerClaim, "unknown") || strings.Contains(lowerClaim, "неизвест") || strings.Contains(lowerClaim, "не подтверж") {
				continue
			}
			unsupported = true
			break
		}
		if !unsupported {
			result = append(result, claim)
		}
	}
	return result
}

func containsNegativeMarker(value string) bool {
	for _, marker := range []string{"не владе", "не знаю", "нет опыта", "нет навыка", "не подтверж", "unknown", "неизвест"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func explicitTermMentioned(text, requirement string) bool {
	want := strings.Fields(canonicalSkillName(requirement))
	if len(want) == 0 {
		return false
	}
	text = strings.ToLower(text)
	text = strings.NewReplacer("/", " ", ",", " ", ";", " ", "(", " ", ")", " ", "\n", " ", "•", " ", "|", " ").Replace(text)
	have := strings.Fields(text)
	for start := 0; start+len(want) <= len(have); start++ {
		matched := true
		for offset, word := range want {
			if have[start+offset] != word {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func canonicalSkillName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("ё", "е", "/", " ", "-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func descriptionExperienceMinimumMonths(requirement, evidence string) (int, bool, bool) {
	text := normalizeEvidenceText(strings.Join([]string{requirement, evidence}, " "))
	if match := experienceRangePattern.FindStringSubmatch(text); len(match) == 3 {
		months, err := strconv.Atoi(match[1])
		if err != nil || months < 0 {
			return 0, false, false
		}
		unit := strings.ToLower(match[2])
		if strings.Contains(unit, "месяц") || strings.Contains(unit, "month") {
			return months, true, !containsRoleSpecificExperienceMarker(text)
		}
		return months * 12, true, !containsRoleSpecificExperienceMarker(text)
	}
	match := numericExperiencePattern.FindStringSubmatch(text)
	if len(match) != 3 {
		return 0, false, false
	}
	months, err := strconv.Atoi(match[1])
	if err != nil || months < 0 {
		return 0, false, false
	}
	unit := strings.ToLower(match[2])
	if strings.Contains(unit, "месяц") || strings.Contains(unit, "month") {
		return months, true, !containsRoleSpecificExperienceMarker(text)
	}
	return months * 12, true, !containsRoleSpecificExperienceMarker(text)
}

func containsRoleSpecificExperienceMarker(text string) bool {
	normalized := strings.ToLower(strings.NewReplacer("/", " ", "-", " ", "_", " ").Replace(text))
	for _, marker := range []string{"ai", "ml", "nlp"} {
		if containsExactToken(normalized, marker) {
			return true
		}
	}
	for _, marker := range roleSpecificExperienceMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func containsExactToken(text, want string) bool {
	for _, token := range strings.Fields(text) {
		if token == want {
			return true
		}
	}
	return false
}

func genericDescriptionExperienceStatus(candidate CandidateFacts, minimumMonths int) (string, string) {
	if !candidate.TotalExperienceMonthsKnown {
		return HardRequirementStatusUnknown, "not provided"
	}
	evidence := fmt.Sprintf("Candidate total experience: %d months", candidate.TotalExperienceMonths)
	if candidate.TotalExperienceMonths >= minimumMonths {
		return HardRequirementStatusMet, evidence
	}
	if minimumMonths <= 12 && candidate.TotalExperienceMonths >= 9 {
		return HardRequirementStatusUnknown, evidence
	}
	return HardRequirementStatusMissing, evidence
}

func genericDescriptionExperienceSoftGap(candidate CandidateFacts, requirement HardRequirementCandidate) bool {
	minimumMonths, supported, generic := descriptionExperienceMinimumMonths(requirement.Requirement, requirement.VacancyEvidence)
	return supported && generic && candidate.TotalExperienceMonthsKnown && minimumMonths <= 12 && candidate.TotalExperienceMonths < minimumMonths && candidate.TotalExperienceMonths >= 9
}

func experienceRequirementIsNonRequirement(requirement HardRequirementCandidate) bool {
	return vacancyDoesNotRequireExperience(requirement.Requirement) || vacancyDoesNotRequireExperience(requirement.VacancyEvidence)
}

func vacancyDoesNotRequireExperience(value string) bool {
	text := strings.ToLower(strings.Join(strings.Fields(value), " "))
	compact := strings.ReplaceAll(text, " ", "")
	return compact == "noexperience" || strings.Contains(text, "без опыта") || strings.Contains(text, "опыт не требуется")
}

func genericHHExperienceEvidenceOnly(value vacancy.Vacancy, description string, requirement HardRequirementCandidate) bool {
	_, supported, noExperience := genericWorkExperienceMinimumMonths(value.WorkExperience)
	return supported && !noExperience && containsNormalizedText(value.WorkExperience, requirement.VacancyEvidence) && !containsNormalizedText(description, requirement.VacancyEvidence)
}

func genericWorkExperienceMinimumMonths(value string) (int, bool, bool) {
	text := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	text = strings.ReplaceAll(strings.ReplaceAll(text, "–", "-"), "—", "-")
	compact := strings.ReplaceAll(text, " ", "")
	if compact == "noexperience" || vacancyDoesNotRequireExperience(text) {
		return 0, true, true
	}
	switch {
	case strings.Contains(compact, "between1and3") || strings.Contains(compact, "1-3"):
		return 12, true, false
	case strings.Contains(compact, "between3and6") || strings.Contains(compact, "3-6"):
		return 36, true, false
	case strings.Contains(compact, "morethan6") || strings.Contains(compact, "более6") || strings.Contains(compact, "6+") || strings.Contains(compact, "от6"):
		return 72, true, false
	default:
		return 0, false, false
	}
}

func educationRequirementMatches(candidateLevel, requirement string) (bool, bool) {
	text := strings.ToLower(strings.ReplaceAll(strings.Join(strings.Fields(strings.TrimSpace(requirement)), " "), "ё", "е"))
	if text == "" || containsEducationSpecialization(text) {
		return false, false
	}
	acceptsHigher := strings.Contains(text, "высш")
	acceptsIncompleteHigher := acceptsHigher && (strings.Contains(text, "незакончен") || strings.Contains(text, "неполное"))
	acceptsSecondaryProfessional := strings.Contains(text, "средн") && (strings.Contains(text, "профессион") || strings.Contains(text, "специальн"))
	acceptsSecondary := strings.Contains(text, "среднее") && !acceptsSecondaryProfessional
	if !acceptsHigher && !acceptsSecondaryProfessional && !acceptsSecondary {
		return false, false
	}
	if candidateLevel == "higher" && acceptsHigher || candidateLevel == "incomplete_higher" && acceptsIncompleteHigher || candidateLevel == "secondary_professional" && acceptsSecondaryProfessional || candidateLevel == "secondary" && acceptsSecondary {
		return true, true
	}
	return false, true
}

func containsEducationSpecialization(text string) bool {
	return strings.Contains(text, "по иб") || strings.Contains(text, "по информационной безопасности") || strings.Contains(text, "техническ") || strings.Contains(text, "специальност") || strings.Contains(text, "направлен")
}

func candidateEducationEvidence(candidate CandidateFacts) string {
	if details := strings.TrimSpace(candidate.EducationDetails); details != "" {
		return fmt.Sprintf("Candidate education: %s (%s)", candidate.EducationLevel, details)
	}
	return "Candidate education: " + candidate.EducationLevel
}

// ValidateHardRequirements rechecks model-derived statuses against trusted
// candidate facts. A high model score cannot bypass this validation.
func ValidateHardRequirements(candidate CandidateFacts, value vacancy.Vacancy, assessment Assessment) error {
	if err := ValidateAssessment(assessment); err != nil {
		return err
	}
	for _, requirement := range assessment.HardRequirements {
		if isOptionalRequirement(requirement) {
			return fmt.Errorf("optional requirement %q must not be included in hard_requirements", requirement.Requirement)
		}
		switch requirement.Category {
		case HardRequirementCategoryEducation:
			if !candidate.EducationKnown {
				if requirement.Status != HardRequirementStatusUnknown {
					return fmt.Errorf("education requirement %q cannot be %s without structured candidate education", requirement.Requirement, requirement.Status)
				}
				continue
			}
			matches, supported := educationRequirementMatches(candidate.EducationLevel, requirement.Requirement)
			if !supported {
				if requirement.Status != HardRequirementStatusUnknown {
					return fmt.Errorf("education requirement %q cannot be resolved from supported facts", requirement.Requirement)
				}
				continue
			}
			expected := HardRequirementStatusMissing
			if matches {
				expected = HardRequirementStatusMet
			}
			if requirement.Status != expected || !strings.HasPrefix(requirement.CandidateEvidence, "Candidate education:") {
				return fmt.Errorf("education requirement %q has status %s, want trusted status %s", requirement.Requirement, requirement.Status, expected)
			}
		case HardRequirementCategoryExperienceYears:
			if vacancyDoesNotRequireExperience(requirement.Requirement) || vacancyDoesNotRequireExperience(requirement.VacancyEvidence) {
				return fmt.Errorf("vacancy does not require experience, so experience requirement %q must not be emitted", requirement.Requirement)
			}
			minimum, supported, generic := descriptionExperienceMinimumMonths(requirement.Requirement, requirement.VacancyEvidence)
			if !supported || !generic || !candidate.TotalExperienceMonthsKnown || !strings.HasPrefix(requirement.CandidateEvidence, "Candidate total experience:") {
				if requirement.Status != HardRequirementStatusUnknown {
					return fmt.Errorf("experience requirement %q must be unknown without trusted generic description duration", requirement.Requirement)
				}
				continue
			}
			expected, _ := genericDescriptionExperienceStatus(candidate, minimum)
			if requirement.Status != expected {
				return fmt.Errorf("experience requirement %q has status %s, want trusted status %s", requirement.Requirement, requirement.Status, expected)
			}
		case HardRequirementCategoryLocation:
			if err := validateLocationRequirement(candidate.Location, requirement); err != nil {
				return err
			}
		case HardRequirementCategorySkill:
			if requirement.Status == HardRequirementStatusMissing {
				return fmt.Errorf("skill requirement %q cannot be missing without an explicit negative candidate fact", requirement.Requirement)
			}
			if requirement.Status == HardRequirementStatusMet && !candidateEvidenceGrounded(candidate, requirement.Requirement+" "+requirement.CandidateEvidence) {
				return fmt.Errorf("skill requirement %q has ungrounded candidate evidence", requirement.Requirement)
			}
		default:
			if requirement.Status == HardRequirementStatusMissing && !explicitNegativeCandidateFact(candidate, requirement.Requirement) {
				return fmt.Errorf("requirement %q cannot be missing without an explicit negative candidate fact", requirement.Requirement)
			}
			if requirement.Status == HardRequirementStatusMet && !candidateEvidenceGrounded(candidate, requirement.Requirement+" "+requirement.CandidateEvidence) {
				return fmt.Errorf("requirement %q has ungrounded candidate evidence", requirement.Requirement)
			}
		}
	}
	return nil
}

func isOptionalRequirement(value HardRequirementEvaluation) bool {
	classification := classifyRequirementContext(value.Requirement+" "+value.VacancyEvidence, value.VacancyEvidence)
	return classification.Classification == RequirementExtractionPreference
}

func validateLocationRequirement(candidateLocationValue string, requirement HardRequirementEvaluation) error {
	candidateLocationValue = strings.TrimSpace(candidateLocationValue)
	if candidateLocationValue == "" {
		if requirement.Status != HardRequirementStatusUnknown {
			return fmt.Errorf("location requirement %q must be unknown when location is not fully provided", requirement.Requirement)
		}
		return nil
	}
	if locationRequirementMatchesCandidate(candidateLocationValue, requirement.Requirement, requirement.VacancyEvidence) {
		if requirement.Status != HardRequirementStatusMet {
			return fmt.Errorf("same-city location requirement %q must be met, not %s", requirement.Requirement, requirement.Status)
		}
		return nil
	}
	if requirement.Status != HardRequirementStatusUnknown {
		return fmt.Errorf("different-city location requirement %q must be unknown while relocation is unspecified", requirement.Requirement)
	}
	return nil
}

func locationRequirementMatchesCandidate(candidateLocation, requirement, evidence string) bool {
	candidateWords := strings.Fields(normalizeLocationText(candidateLocation))
	if len(candidateWords) == 0 {
		return false
	}
	locationText := normalizeLocationText(strings.Join([]string{requirement, evidence}, " "))
	if locationText == "" || isLocationAvailabilityRequirement(locationText) {
		return false
	}
	evidenceWords := strings.Fields(locationText)
	for start := 0; start+len(candidateWords) <= len(evidenceWords); start++ {
		matched := true
		for offset, candidateWord := range candidateWords {
			if !locationWordMatches(candidateWord, evidenceWords[start+offset]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func normalizeLocationText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func locationWordMatches(candidateWord, evidenceWord string) bool {
	if candidateWord == evidenceWord {
		return true
	}
	for _, base := range []string{candidateWord, trimLocationInflection(candidateWord)} {
		if base == "" || !strings.HasPrefix(evidenceWord, base) {
			continue
		}
		if isLocationInflectionSuffix(strings.TrimPrefix(evidenceWord, base)) {
			return true
		}
	}
	return false
}

func trimLocationInflection(value string) string {
	for _, suffix := range []string{"а", "я", "ь"} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return ""
}

func isLocationInflectionSuffix(value string) bool {
	switch value {
	case "а", "е", "и", "о", "у", "ы", "ю", "я", "ом", "ем", "ой", "ей", "ым", "им":
		return true
	default:
		return false
	}
}

func isLocationAvailabilityRequirement(value string) bool {
	for _, marker := range []string{"выезд", "командировк", "релокац", "переезд", "объект заказчика", "объекта заказчика", "объектам заказчика", "объектах заказчика"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func candidateEvidenceGrounded(candidate CandidateFacts, evidence string) bool {
	for _, word := range meaningfulWords(evidence) {
		if skill, ok := candidate.Profile.ResolveSkill(word); ok && !skill.Negative && skill.Level != domaincandidate.SkillLevelUnknown && skill.Level != domaincandidate.SkillLevelHeardOf {
			return true
		}
	}
	if explicitTermMentioned(candidate.Skills, evidence) || explicitTermMentioned(candidate.Experience, evidence) {
		return true
	}
	context := strings.ToLower(strings.Join([]string{candidate.Skills, candidate.Experience, candidate.Location, candidate.Contacts}, "\n"))
	for _, word := range meaningfulWords(evidence) {
		if strings.Contains(context, word) {
			return true
		}
	}
	return false
}

func explicitNegativeCandidateFact(candidate CandidateFacts, requirement string) bool {
	if skill, ok := candidate.Profile.ResolveSkill(requirement); ok && skill.Negative {
		return true
	}
	context := strings.ToLower(strings.Join([]string{candidate.Skills, candidate.Experience, candidate.Location}, "\n"))
	for _, marker := range []string{"не владе", "не знаю", "нет опыта", "нет навыка", "нет знаний", "— нет", "- нет", ": нет", "отсутствует", "не готов", "не имею", "не работал"} {
		if strings.Contains(context, marker) && candidateEvidenceGrounded(candidate, requirement) {
			return true
		}
	}
	return false
}

func meaningfulWords(value string) []string {
	value = strings.ToLower(value)
	value = strings.NewReplacer("/", " ", "-", " ", ",", " ", ".", " ", ":", " ", "(", " ", ")", " ").Replace(value)
	stopWords := map[string]struct{}{"и": {}, "с": {}, "для": {}, "опыт": {}, "знание": {}, "работа": {}, "умение": {}, "навык": {}, "кандидат": {}, "имеет": {}, "требуется": {}}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, word := range strings.Fields(value) {
		if len([]rune(word)) < 3 {
			continue
		}
		if _, stop := stopWords[word]; stop {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		result = append(result, word)
	}
	return result
}
