package conversationpolicy

import (
	"strings"

	"hh-ai-responder/internal/usecase/candidatecontext"
)

// MessageType is a stable, product-facing classification of one employer
// message. It is a read-only projection and never grants write capability.
type MessageType string

const (
	MessageTypeGeneralQuestion       MessageType = "GENERAL_QUESTION"
	MessageTypeCandidateFactQuestion MessageType = "CANDIDATE_FACT_QUESTION"
	MessageTypeInterviewInvitation   MessageType = "INTERVIEW_INVITATION"
	MessageTypeInterviewScheduling   MessageType = "INTERVIEW_SCHEDULING"
	MessageTypeTestAssignment        MessageType = "TEST_ASSIGNMENT"
	MessageTypeDocumentRequest       MessageType = "DOCUMENT_REQUEST"
	MessageTypeSalaryQuestion        MessageType = "SALARY_QUESTION"
	MessageTypeRelocationQuestion    MessageType = "RELOCATION_QUESTION"
	MessageTypeWorkFormatQuestion    MessageType = "WORK_FORMAT_QUESTION"
	MessageTypeOffer                 MessageType = "OFFER"
	MessageTypeRejection             MessageType = "REJECTION"
	MessageTypeFollowUp              MessageType = "FOLLOW_UP"
	MessageTypeAdministrative        MessageType = "ADMINISTRATIVE"
	MessageTypeSuspicious            MessageType = "SUSPICIOUS"
	MessageTypeUnknown               MessageType = "UNKNOWN"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskMedium   RiskLevel = "MEDIUM"
	RiskHigh     RiskLevel = "HIGH"
	RiskCritical RiskLevel = "CRITICAL"
)

// MessageClassification is deterministic evidence for downstream read
// models. Confidence is a bounded classifier confidence, not an LLM claim.
type MessageClassification struct {
	Type                  MessageType `json:"type"`
	Confidence            float64     `json:"confidence"`
	Evidence              []string    `json:"evidence,omitempty"`
	Risk                  RiskLevel   `json:"risk"`
	RequiresCandidateFact bool        `json:"requires_candidate_fact"`
	RequiresManualReview  bool        `json:"requires_manual_review"`
}

// ClassifyMessage applies conservative deterministic precedence. Employer
// text is treated only as data; this function has no side effects and no AI or
// HH capability.
func ClassifyMessage(text string) MessageClassification {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return unknownClassification("empty employer message")
	}

	if reason := suspiciousReason(lower); reason != "" {
		return classification(MessageTypeSuspicious, RiskCritical, false, true, reason)
	}
	if containsAny(lower, "паспорт", "паспортные данные", "удостоверен", "личные документы", "personal document", "bank details", "банковская карта", "номер карты", "реквизит", "iban", "swift") {
		return classification(MessageTypeDocumentRequest, RiskHigh, false, true, "sensitive document or banking request")
	}
	if containsAny(lower, "зарплат", "зп ", "оклад", "компенсац", "доход", "salary", "compensation") {
		return classification(MessageTypeSalaryQuestion, RiskHigh, true, true, "salary or compensation")
	}
	if containsAny(lower, "релокац", "переезд", "переехать", "relocation", "relocate") {
		return classification(MessageTypeRelocationQuestion, RiskHigh, true, true, "relocation or move")
	}
	if containsAny(lower, "удалён", "удален", "гибрид", "формат работы", "офис", "work format", "remote") {
		return classification(MessageTypeWorkFormatQuestion, RiskHigh, true, true, "work format")
	}
	if containsAny(lower, "когда готовы выйти", "когда сможете приступить", "дата выхода", "дата начала", "available to start", "when can you start") {
		return classification(MessageTypeAdministrative, RiskHigh, true, true, "availability or start date")
	}
	if containsAny(lower, "тестовое", "тестовое задание", "тест assignment", "take-home", "questionnaire", "пройдите тест") {
		return classification(MessageTypeTestAssignment, RiskHigh, false, true, "test assignment")
	}
	if containsAny(lower, "оффер", "offer", "предлагаем работу", "готовы сделать вам") {
		return classification(MessageTypeOffer, RiskHigh, false, true, "explicit offer")
	}
	if candidatecontext.ClassifyEmployerMessage(lower) == candidatecontext.EmployerMessageIntentTerminal ||
		containsAny(lower, "выбрали другого кандидата", "позиция закрыта", "we chose another candidate", "position has been filled") {
		return classification(MessageTypeRejection, RiskLow, false, false, "explicit terminal employer message")
	}
	if containsAny(lower, "назначить интервью", "назначить собеседован", "в какое время", "подтвердите время", "time slot") && containsAny(lower, "интервью", "собеседован", "созвон", "call") {
		return classification(MessageTypeInterviewScheduling, RiskHigh, false, true, "interview scheduling requires candidate confirmation")
	}
	if candidatecontext.ClassifyEmployerMessage(lower) == candidatecontext.EmployerMessageIntentInterviewInvitation ||
		containsAny(lower, "приглашаем завтра", "приглашаем вас", "приглашаем на") {
		return classification(MessageTypeInterviewInvitation, RiskHigh, false, true, "interview invitation requires candidate confirmation")
	}
	if containsAny(lower, "напоминаю", "напоминаем", "есть новости", "возвращаюсь по вашему отклику", "follow up", "following up") {
		return classification(MessageTypeFollowUp, RiskMedium, false, true, "follow-up message")
	}

	intent := candidatecontext.ClassifyEmployerMessage(lower)
	if intent == candidatecontext.EmployerMessageIntentFactualQuestion && candidatecontext.CountQuestionTopics(lower) > 0 {
		return classification(MessageTypeCandidateFactQuestion, RiskLow, true, false, "deterministic candidate fact question")
	}
	if intent == candidatecontext.EmployerMessageIntentFactualQuestion || containsAny(lower, "расскажите", "какой проект", "какие задачи", "what project") {
		return classification(MessageTypeGeneralQuestion, RiskLow, false, false, "general employer question")
	}
	if intent == candidatecontext.EmployerMessageIntentAcknowledgement || intent == candidatecontext.EmployerMessageIntentStatusMessage || intent == candidatecontext.EmployerMessageIntentInstruction {
		return classification(MessageTypeAdministrative, RiskMedium, false, true, "administrative or instruction message")
	}
	if containsAny(lower, "?") {
		return classification(MessageTypeGeneralQuestion, RiskLow, false, false, "question marker")
	}
	return unknownClassification("no deterministic classification rule matched")
}

func classification(kind MessageType, risk RiskLevel, candidateFact, manual bool, evidence string) MessageClassification {
	return MessageClassification{Type: kind, Confidence: 1, Evidence: []string{evidence}, Risk: risk, RequiresCandidateFact: candidateFact, RequiresManualReview: manual}
}

func unknownClassification(evidence string) MessageClassification {
	return classification(MessageTypeUnknown, RiskMedium, false, true, evidence)
}

func suspiciousReason(text string) string {
	switch {
	case containsAny(text, "ignore previous instructions", "ignore prior instructions", "system:", "игнорируй предыдущие инструкции", "предыдущие инструкции"):
		return "prompt-injection-like instruction in employer content"
	case containsAny(text, "password", "парол", "credential", "credentials", "api key", "apikey", "api-key", "token", "secret", "otp", "2fa", "код подтверждения", "логин", "учетная запись", "учётная запись"):
		return "credential or authentication secret request"
	case containsAny(text, ".exe", "скачай", "скачайте", "скачать", "запусти", "запустите", "установи", "установите", "install ", "download "):
		return "software download or execution request"
	default:
		return ""
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, strings.ToLower(value)) {
			return true
		}
	}
	return false
}
