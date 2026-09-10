package conversationpolicy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	domaincandidate "hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/conversation"
)

type ConsistencyStatus string

const (
	ConsistencySupported   ConsistencyStatus = "SUPPORTED"
	ConsistencyUnsupported ConsistencyStatus = "UNSUPPORTED"
	ConsistencyConflict    ConsistencyStatus = "CONFLICT"
	ConsistencyUnknown     ConsistencyStatus = "UNKNOWN"
)

type ConsistencyWarning struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	ClaimText string            `json:"claim_text"`
	MessageID string            `json:"message_id"`
	Status    ConsistencyStatus `json:"status,omitempty"`
}

type ConsistencyInput struct {
	Claims                []conversation.Claim
	Candidate             domaincandidate.EmployerSafeCandidateKnowledge
	ForbiddenClaims       []string
	TotalExperienceMonths *int
}

// CheckConsistency is read-only. Conversation claims remain conversation
// evidence and are never promoted to candidate knowledge.
func CheckConsistency(input ConsistencyInput) []ConsistencyWarning {
	result := []ConsistencyWarning{}
	for _, claim := range input.Claims {
		code, message, status := "missing_knowledge", "Полное утверждение не подтверждено текущей базой знаний; требуется проверка кандидатом.", ConsistencyUnknown
		supported := claimSupported(claim.Text, input.Candidate)
		for _, forbidden := range input.ForbiddenClaims {
			if mentions(claim.Text, forbidden) {
				supported = false
				code, message, status = "forbidden_claim", "Ранее отправленный текст содержит запрещённую формулировку; проверьте смысл и согласованность.", ConsistencyConflict
			}
		}
		for _, skill := range input.Candidate.Skills {
			if skill.Negative && (mentions(claim.Text, skill.Name) || canonical(claim.RelatedSkill) == canonical(skill.Name)) {
				supported = false
				code, message, status = "potential_knowledge_conflict", "Упомянут навык с подтверждённой отрицательной записью; требуется ручная проверка утверждения.", ConsistencyConflict
			}
		}
		if claim.Experience != nil {
			supported = false
			months, explicit := claimMonths(claim.Text)
			if explicit && months == claim.Experience.Months && claim.Experience.Scope == "total_professional" && claim.RelatedSkill == "" && claim.RelatedProject == "" &&
				(mentions(claim.Text, "общий профессиональный опыт") || mentions(claim.Text, "total professional experience")) && input.TotalExperienceMonths != nil {
				if months != *input.TotalExperienceMonths {
					code, message = "experience_mismatch", fmt.Sprintf("В сообщении указан общий профессиональный опыт %d мес., в подтверждённом профиле — %d мес. Требуется проверка, история не изменена.", months, *input.TotalExperienceMonths)
					status = ConsistencyConflict
				} else if code == "missing_knowledge" {
					message = "Число месяцев совпадает с профилем; остальные части утверждения требуют отдельной проверки."
				}
			}
		}
		if !supported {
			result = append(result, ConsistencyWarning{Code: code, Message: message, ClaimText: claim.Text, MessageID: claim.MessageID, Status: status})
		}
	}
	return result
}

func CheckConversationConsistency(input ConsistencyInput) []ConsistencyWarning {
	return CheckConsistency(input)
}

func claimSupported(text string, safe domaincandidate.EmployerSafeCandidateKnowledge) bool {
	equal := func(value string) bool {
		return strings.TrimRight(canonical(text), ".") == strings.TrimRight(canonical(value), ".") && strings.TrimSpace(value) != ""
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
			practical := false
			for _, skill := range safe.Skills {
				if canonical(skill.Name) == canonical(technology) && !skill.Negative && string(skill.Level) != "unknown" && string(skill.Level) != "heard_of" {
					practical = true
				}
			}
			if practical && (equal("Использовал "+technology+" в "+project.Name) || equal("Использовала "+technology+" в "+project.Name) || equal(technology+" использовался в проекте "+project.Name)) {
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

var experienceRangePattern = regexp.MustCompile(`(?i)\d+\s*(?:[-–]\s*\d+|\+)`)
var numericExperiencePattern = regexp.MustCompile(`(?i)(\d+)\s*(месяц\w*|month\w*|год\w*|year\w*)`)

func claimMonths(text string) (int, bool) {
	if experienceRangePattern.MatchString(text) {
		return 0, false
	}
	for _, marker := range []string{"не", "not", "около", "примерно", "более", "менее", "about", "over", "under"} {
		if mentions(text, marker) {
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

func canonical(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
func mentions(text, value string) bool {
	return strings.Contains(canonical(text), canonical(value)) && strings.TrimSpace(value) != ""
}
