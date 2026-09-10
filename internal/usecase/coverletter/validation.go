package coverletter

import (
	"fmt"
	"regexp"
	"strings"

	"hh-ai-responder/internal/candidate"
)

var (
	experienceDurationClaimPattern    = regexp.MustCompile(`(?i)(?:[0-9]+(?:[.,][0-9]+)?\s*)?(?:год(?:а|ов)?|лет|year(?:s)?)\s*(?:опыта|experience)?`)
	approximateExperienceClaimPattern = regexp.MustCompile(`(?i)(?:около|примерно|приблизительно|about|approximately)\s+(?:[0-9]+(?:[.,][0-9]+)?\s*)?(?:год(?:а|ов)?|лет|year(?:s)?)`)
)

// ValidateLetter applies cover-letter-specific deterministic checks. It is
// intentionally conservative: relevance hints and unknown facts never grant
// permission to make a positive Candidate claim.
func ValidateLetter(value CandidateFacts, letter string) error {
	if len(letter) == 0 || isWhitespaceOnly(letter) {
		return ErrEmptyLetter
	}
	if err := validateExperience(value, letter); err != nil {
		return err
	}
	text := strings.ToLower(letter)
	if err := validateTechnologyClaims(value, text); err != nil {
		return err
	}
	if err := validateSeniority(value, text); err != nil {
		return err
	}
	if err := validateEducation(value, text); err != nil {
		return err
	}
	if err := validateEnglish(value, text); err != nil {
		return err
	}
	if err := validatePreferenceClaims(value, text); err != nil {
		return err
	}
	return nil
}

func validateExperience(value CandidateFacts, letter string) error {
	if !value.TotalExperienceMonthsKnown || value.TotalExperienceMonths%12 == 0 {
		return nil
	}
	claims := experienceDurationClaimPattern.FindAllStringIndex(letter, -1)
	if len(claims) == 0 {
		return nil
	}
	approximateClaims := approximateExperienceClaimPattern.FindAllStringIndex(letter, -1)
	if len(approximateClaims) < len(claims) {
		return fmt.Errorf("generated letter rounds structured experience of %d months to years", value.TotalExperienceMonths)
	}
	return nil
}

func validateTechnologyClaims(value CandidateFacts, text string) error {
	for _, technology := range []string{"kubernetes", "k8s", "kafka", "celery", "docker"} {
		if !strings.Contains(text, technology) || !positiveClaim(text, technology) {
			continue
		}
		if technology == "k8s" && hasKnownSkill(value, "kubernetes") {
			continue
		}
		canonicalName := technology
		if technology == "k8s" {
			canonicalName = "kubernetes"
		}
		if !hasKnownSkill(value, canonicalName) {
			return fmt.Errorf("%w: %s", ErrUnsupportedCandidateFact, canonicalName)
		}
		if technology == "docker" && productionClaim(text, technology) && !hasCommercialUse(value, technology) {
			return fmt.Errorf("%w: production/commercial Docker use is not confirmed", ErrUnsupportedCandidateFact)
		}
		if technology != "docker" && productionClaim(text, technology) && !hasCommercialUse(value, technology) {
			return fmt.Errorf("%w: production/commercial %s use is not confirmed", ErrUnsupportedCandidateFact, canonicalName)
		}
	}
	return nil
}

func positiveClaim(text, technology string) bool {
	position := strings.Index(text, technology)
	if position < 0 {
		return false
	}
	prefix := text[maxInt(0, position-55):position]
	if strings.Contains(prefix, "готов изуч") || strings.Contains(prefix, "готов осво") || strings.Contains(prefix, "изучу") || strings.Contains(prefix, "не использовал") || strings.Contains(prefix, "не работал") || strings.Contains(prefix, "нет опыта") || strings.Contains(prefix, "не знаком") || strings.Contains(prefix, "unknown") {
		return false
	}
	return strings.Contains(prefix, "опыт") || strings.Contains(prefix, "работ") || strings.Contains(prefix, "использ") || strings.Contains(prefix, "влад") || strings.Contains(prefix, "знаю") || strings.Contains(prefix, "умею") || strings.Contains(prefix, "выполн") || strings.Contains(prefix, "production") || strings.Contains(prefix, "продакш") || strings.Contains(prefix, "коммерч") || strings.Contains(prefix, "развёрт") || strings.Contains(prefix, "разверт") || strings.Contains(prefix, "настро") || strings.Contains(prefix, "реализ")
}

func productionClaim(text, technology string) bool {
	position := strings.Index(text, technology)
	if position < 0 {
		return false
	}
	window := text[maxInt(0, position-80):minInt(len(text), position+len(technology)+80)]
	return strings.Contains(window, "production") || strings.Contains(window, "продакш") || strings.Contains(window, "продакшен") || strings.Contains(window, "коммерч")
}

func hasKnownSkill(value CandidateFacts, technology string) bool {
	canonical := normalize(technology)
	for _, skill := range value.SafeKnowledge.Skills {
		if normalize(skill.Name) == canonical && !skill.Negative && skill.Level != candidate.SkillLevelUnknown && skill.Level != candidate.SkillLevelHeardOf {
			return true
		}
	}
	for _, skill := range value.Profile.Skills {
		if normalize(skill.Name) == canonical && !skill.Negative && skill.ProfileFact.Confirmed && candidate.SourceTrustedForEmployerCommunication(skill.ProfileFact.Source) && skill.Level != candidate.SkillLevelUnknown && skill.Level != candidate.SkillLevelHeardOf {
			return true
		}
	}
	for _, skill := range value.SafeContext.RelevantSkills {
		if normalize(skill) == canonical {
			return true
		}
	}
	for _, fact := range value.SafeContext.AllowedFacts {
		if strings.Contains(normalize(fact), canonical) {
			return true
		}
	}
	return containsTechnology(value.Skills, canonical)
}

func hasCommercialUse(value CandidateFacts, technology string) bool {
	canonical := normalize(technology)
	for _, skill := range value.SafeKnowledge.Skills {
		if normalize(skill.Name) != canonical || skill.Negative {
			continue
		}
		for _, use := range skill.Uses {
			if use.Context == candidate.CanonicalSkillUsageCommercial {
				return true
			}
		}
	}
	return false
}

func containsTechnology(text, technology string) bool {
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r < 'a' || r > 'z' }) {
		if normalize(token) == technology {
			return true
		}
	}
	return strings.Contains(normalize(text), technology)
}

func validateSeniority(value CandidateFacts, text string) error {
	for _, level := range []string{"senior", "lead", "middle", "мидл", "сеньор", "ведущий", "старший"} {
		if strings.Contains(text, level) && !hasSupportedSeniority(value, level) {
			return fmt.Errorf("%w: unsupported seniority %s", ErrUnsupportedCandidateFact, level)
		}
	}
	return nil
}

func hasSupportedSeniority(value CandidateFacts, level string) bool {
	for _, source := range []string{value.ResumeTitle, value.SafeKnowledge.Profile.PrimaryRoles, value.SafeKnowledge.Profile.SecondaryRoles, value.SafeKnowledge.Profile.PreferredRoles} {
		if strings.Contains(strings.ToLower(source), level) {
			return true
		}
	}
	for _, role := range value.SafeContext.RelevantSkills {
		if strings.Contains(strings.ToLower(role), level) {
			return true
		}
	}
	return false
}

func validateEducation(value CandidateFacts, text string) error {
	if !strings.Contains(text, "бакалавр") && !strings.Contains(text, "магистр") && !strings.Contains(text, "высшее образование") && !strings.Contains(text, "университет") {
		return nil
	}
	if value.EducationKnown || len(value.SafeKnowledge.Profile.Education) > 0 {
		return nil
	}
	for _, education := range value.Profile.Education {
		if education.ProfileFact.Confirmed && candidate.SourceTrustedForEmployerCommunication(education.ProfileFact.Source) {
			return nil
		}
	}
	return fmt.Errorf("%w: education is not confirmed", ErrUnsupportedCandidateFact)
}

func validateEnglish(value CandidateFacts, text string) error {
	if !strings.Contains(text, "b2") && !strings.Contains(text, "англий") && !strings.Contains(text, "english") {
		return nil
	}
	if !strings.Contains(text, "fluent") && !strings.Contains(text, "native") && !strings.Contains(text, "свобод") && !strings.Contains(text, "c1") && !strings.Contains(text, "носител") {
		return nil
	}
	languages := append([]candidate.LanguageFact{}, value.SafeKnowledge.Profile.Languages...)
	if len(languages) == 0 {
		languages = append(languages, value.Profile.Languages...)
	}
	for _, language := range languages {
		if strings.Contains(strings.ToLower(language.Name), "англ") || strings.Contains(strings.ToLower(language.Name), "english") {
			level := strings.ToLower(language.Level)
			if strings.Contains(level, "fluent") || strings.Contains(level, "c1") || strings.Contains(level, "native") || strings.Contains(level, "свобод") {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: English level is overstated", ErrUnsupportedCandidateFact)
}

func validatePreferenceClaims(value CandidateFacts, text string) error {
	relocation := strings.ToLower(safePreference(value, "relocation"))
	if (strings.Contains(text, "готов к релокац") || strings.Contains(text, "готов к переезд") || strings.Contains(text, "готов переех")) && (strings.Contains(relocation, "не") || strings.Contains(relocation, "нет")) {
		return fmt.Errorf("%w: relocation contradicts Candidate preference", ErrUnsupportedCandidateFact)
	}
	businessTrips := strings.ToLower(safePreference(value, "business_trips"))
	if strings.Contains(text, "готов к командиров") && (strings.Contains(businessTrips, "не") || strings.Contains(businessTrips, "нет")) {
		return fmt.Errorf("%w: business-trip preference is contradicted", ErrUnsupportedCandidateFact)
	}
	return nil
}

func safePreference(value CandidateFacts, kind string) string {
	var safeValue string
	var profileValue candidate.ProfileStringFact
	switch kind {
	case "relocation":
		safeValue, profileValue = value.SafeKnowledge.Profile.Relocation, value.Profile.WorkPreferences.Relocation
	case "business_trips":
		safeValue, profileValue = value.SafeKnowledge.Profile.BusinessTrips, value.Profile.WorkPreferences.BusinessTrips
	}
	if strings.TrimSpace(safeValue) != "" {
		return safeValue
	}
	if profileValue.ProfileFact.Confirmed && candidate.SourceTrustedForEmployerCommunication(profileValue.ProfileFact.Source) {
		return profileValue.Value
	}
	return ""
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
