package main

import (
	"fmt"
	"regexp"
	"strings"
)

var externalLinkPattern = regexp.MustCompile(`(?i)(https?://|www\.)[^\s]+|(^|[\s(])([a-z0-9-]+\.)+(ru|com|net|org|io|dev|me)(/[^\s)]*)?`)

func chatReplyReviewReason(mode string, dryRun bool, employerMessage string) string {
	return chatReplyReviewReasonForContext(mode, dryRun, employerMessage, "", nil, "")
}

// chatReplyReviewReasonForContext decides whether a generated reply may be
// sent. All conversation data is untrusted input, so a high-risk signal in
// any relevant source routes the reply to manual review.
func chatReplyReviewReasonForContext(mode string, dryRun bool, lastEmployerMessage, history string, replyOptions []string, generatedReply string) string {
	for _, source := range []struct {
		name string
		text string
	}{
		{name: "last employer message", text: lastEmployerMessage},
		{name: "chat history", text: history},
		{name: "reply option", text: strings.Join(replyOptions, "\n")},
		{name: "generated reply", text: generatedReply},
	} {
		if reason := classifyHighRiskChatMessage(source.text); reason != "" {
			return fmt.Sprintf("high-risk %s in %s", reason, source.name)
		}
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
