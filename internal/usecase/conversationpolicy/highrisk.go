package conversationpolicy

import (
	"regexp"
	"strings"
)

var externalLinkPattern = regexp.MustCompile(`(?i)(https?://|www\.)[^\s]+|(^|[\s(])([a-z0-9-]+\.)+(ru|com|net|org|io|dev|me)(/[^\s)]*)?`)

// HighRiskMessageReason returns the conservative manual-review category used
// for employer-facing generated text and incoming conversation material.
// Keeping this classifier in the policy package lets generation use cases
// share the established safety rule without depending on the root workflow.
func HighRiskMessageReason(message string) string {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return ""
	}
	if containsAny(text, "password", "парол", "credential", "credentials", "api key", "apikey", "api-key", "token", "secret", "otp", "2fa", "код подтверждения", "логин", "учетная запись", "учётная запись") {
		return "credentials or authentication secret"
	}
	if containsAny(text, "ignore previous instructions", "ignore prior instructions", "игнорируй предыдущие инструкции", "system:") {
		return "prompt injection in employer content"
	}

	categories := []struct {
		reason   string
		keywords []string
	}{
		{"salary negotiation", []string{"зарплат", "оклад", "компенсац", "доход", "руб", "₽", "$", "salary", "compensation"}},
		{"start date or availability", []string{"дата выхода", "дата начала", "приступить к работе", "когда готовы начать", "когда сможете приступить", "выход на работу", "start date", "available to start", "availability", "when can you start"}},
		{"relocation", []string{"переезд", "релокац", "готовы ли переехать", "relocation", "relocate"}},
		{"personal documents", []string{"паспорт", "паспортные данные", "документ", "удостоверен", "passport", "personal document"}},
		{"banking details", []string{"банков", "банковская карта", "номер карты", "реквизит", "счёт", "счет", "bank details", "bank account", "iban", "swift"}},
		{"contract or employment terms", []string{"договор", "трудоустрой", "оформлени", "трудовой", "contract", "employment terms", "employment agreement"}},
		{"interview scheduling", []string{"собеседован", "интервью", "созвон", "видеозвон", "назначить", "в какое время", "interview", "call", "time slot"}},
		{"test assignment or questionnaire", []string{"тестовое", "тестовое задание", "тестовому заданию", "анкет", "заполнить форму", "тест assignment", "take-home", "questionnaire", "test assignment"}},
	}

	for _, category := range categories {
		for _, keyword := range category.keywords {
			if strings.Contains(text, keyword) {
				return category.reason
			}
		}
	}
	if externalLinkPattern.MatchString(text) || strings.Contains(text, "t.me/") || strings.Contains(text, "telegram.me/") {
		return "external link"
	}
	if strings.Contains(text, "установи") || strings.Contains(text, "установите") || strings.Contains(text, "установить") ||
		strings.Contains(text, "скачай") || strings.Contains(text, "скачайте") || strings.Contains(text, "скачать") ||
		strings.Contains(text, "install ") || strings.Contains(text, "download ") {
		return "unknown software or download request"
	}
	return ""
}
