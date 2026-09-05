package main

import "strings"

func chatReplyReviewReason(mode string, dryRun bool, employerMessage string) string {
	if reason := classifyHighRiskChatMessage(employerMessage); reason != "" {
		return reason
	}
	if mode == "review" {
		return "chat mode review"
	}
	if dryRun {
		return "dry-run"
	}
	return ""
}

// classifyHighRiskChatMessage returns a short review reason. The classifier is
// intentionally conservative: a false positive costs a manual review, while a
// false negative could send a material decision on the candidate's behalf.
func classifyHighRiskChatMessage(message string) string {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return ""
	}

	categories := []struct {
		reason   string
		keywords []string
	}{
		{"salary negotiation", []string{"зарплат", "оклад", "компенсац", "доход", "руб", "₽", "$", "salary", "compensation"}},
		{"start date or availability", []string{"дата выхода", "дата начала", "приступить к работе", "когда готовы начать", "выход на работу", "start date", "available to start"}},
		{"relocation", []string{"переезд", "релокац", "готовы ли переехать", "relocation", "relocate"}},
		{"personal documents", []string{"паспорт", "паспортные данные", "документ", "удостоверен"}},
		{"banking details", []string{"банков", "банковская карта", "номер карты", "реквизит", "счёт", "счет", "bank details"}},
		{"contract or employment terms", []string{"договор", "трудоустрой", "оформлени", "contract", "employment terms"}},
		{"interview scheduling", []string{"собеседован", "интервью", "созвон", "видеозвон", "interview", "call"}},
		{"test assignment or questionnaire", []string{"тестовое", "тестовое задание", "тестовому заданию", "анкет", "заполнить форму", "тест assignment", "take-home"}},
	}

	for _, category := range categories {
		for _, keyword := range category.keywords {
			if strings.Contains(text, keyword) {
				return category.reason
			}
		}
	}

	if strings.Contains(text, "http://") || strings.Contains(text, "https://") || strings.Contains(text, "www.") {
		return "external link"
	}
	if strings.Contains(text, "установи") || strings.Contains(text, "установите") ||
		strings.Contains(text, "скачай") || strings.Contains(text, "скачайте") ||
		strings.Contains(text, "install ") || strings.Contains(text, "download ") {
		return "unknown software or download request"
	}

	return ""
}
