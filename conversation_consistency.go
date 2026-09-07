package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ConversationConsistencyWarning struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	ClaimText string `json:"claim_text"`
	MessageID string `json:"message_id"`
}

// This is a conservative check, not semantic entailment. Only complete exact
// trusted assertions or narrowly defined project-use wordings are covered.
// Recognizing Python or a project name never verifies an entire sentence.
func conversationConsistencyWarnings(claims []CandidateConversationClaim, safe EmployerSafeKnowledge, resolver *CandidateContextResolver) ([]ConversationConsistencyWarning, error) {
	warnings := []ConversationConsistencyWarning{}
	for _, claim := range claims {
		context, err := resolver.GetEmployerSafeContext(claim.Text)
		if err != nil {
			return nil, err
		}
		code, message := "missing_knowledge", "Полное утверждение не подтверждено текущей базой знаний; требуется проверка кандидатом."
		supported := conversationClaimSupported(claim.Text, safe)
		for _, forbidden := range context.ForbiddenClaims {
			if contextMentions(claim.Text, forbidden) {
				supported = false
				code, message = "forbidden_claim", "Ранее отправленный текст содержит запрещённую формулировку; проверьте смысл и согласованность."
			}
		}
		for _, skill := range safe.Skills {
			if skill.Negative && (contextMentions(claim.Text, skill.Name) || contextCanonical(claim.RelatedSkill) == contextCanonical(skill.Name)) {
				supported = false
				code, message = "potential_knowledge_conflict", "Упомянут навык с подтверждённой отрицательной записью; требуется ручная проверка утверждения."
			}
		}
		// Compare like-for-like totals only. Technology-specific, commercial,
		// project and total experience must not be silently treated as equal.
		if claim.Experience != nil {
			supported = false
			months, explicit := conversationClaimMonths(claim.Text)
			if explicit && months == claim.Experience.Months && claim.Experience.Scope == "total_professional" && claim.RelatedSkill == "" && claim.RelatedProject == "" &&
				(contextMentions(claim.Text, "общий профессиональный опыт") || contextMentions(claim.Text, "total professional experience")) {
				total, known, totalErr := resolver.canonicalTotalExperience()
				if totalErr != nil {
					return nil, errors.New("invalid trusted total experience; conversation context withheld")
				}
				if known {
					if months != total.Value {
						code = "experience_mismatch"
						message = fmt.Sprintf("В сообщении указан общий профессиональный опыт %d мес., в подтверждённом профиле — %d мес. Требуется проверка, история не изменена.", months, total.Value)
					} else if code == "missing_knowledge" {
						// An annotation checks only the duration, not any other
						// assertions that may occur in the same message excerpt.
						message = "Число месяцев совпадает с профилем; остальные части утверждения требуют отдельной проверки."
					}
				}
			}
		}
		if !supported {
			warnings = append(warnings, ConversationConsistencyWarning{Code: code, Message: message, ClaimText: claim.Text, MessageID: claim.MessageID})
		}
	}
	return warnings, nil
}

func conversationClaimSupported(text string, safe EmployerSafeKnowledge) bool {
	equal := func(value string) bool {
		return strings.TrimRight(contextCanonical(text), ".") == strings.TrimRight(contextCanonical(value), ".") && strings.TrimSpace(value) != ""
	}
	for _, skill := range safe.Skills {
		if skill.Negative {
			continue
		}
		for _, wording := range skill.CanDo {
			if equal(wording) || equal(skill.Name+": "+wording) {
				return true
			}
		}
	}
	for _, project := range safe.Projects {
		for _, value := range append(append([]string{project.Description}, project.Tasks...), project.Results...) {
			if equal(value) {
				return true
			}
		}
		for _, technology := range project.Technologies {
			practicalSkill := false
			for _, skill := range safe.Skills {
				if contextCanonical(skill.Name) == contextCanonical(technology) && !skill.Negative && skill.Level != SkillLevelUnknown && skill.Level != SkillLevelHeardOf {
					practicalSkill = true
				}
			}
			if !practicalSkill {
				continue
			}
			if equal("Использовал "+technology+" в "+project.Name) || equal("Использовала "+technology+" в "+project.Name) ||
				equal(technology+" использовался в проекте "+project.Name) {
				return true
			}
		}
	}
	for _, achievement := range safe.Achievements {
		values := append([]string{achievement.Title, achievement.Problem}, achievement.Solution...)
		values = append(values, achievement.Actions...)
		values = append(values, achievement.Result...)
		for _, value := range values {
			if equal(value) {
				return true
			}
		}
	}
	return false
}

func conversationClaimMonths(text string) (int, bool) {
	// Ranges, approximations and negations are not exact comparable totals.
	if experienceRangePattern.MatchString(text) {
		return 0, false
	}
	for _, marker := range []string{"не", "not", "около", "примерно", "более", "менее", "about", "over", "under"} {
		if contextMentions(text, marker) {
			return 0, false
		}
	}
	matches := numericExperiencePattern.FindAllStringSubmatch(text, -1)
	if len(matches) != 1 {
		return 0, false
	}
	positions := numericExperiencePattern.FindStringIndex(text)
	if strings.Contains(matches[0][0], "+") || (positions[0] > 0 && strings.ContainsRune("0123456789.,-+", rune(text[positions[0]-1]))) {
		return 0, false
	}
	count, err := strconv.Atoi(matches[0][1])
	if err != nil || count < 0 || count > 1200 {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(matches[0][2]), "месяц") || strings.HasPrefix(strings.ToLower(matches[0][2]), "month") {
		return count, true
	}
	return count * 12, true
}
