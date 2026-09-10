package runtime

import (
	"strings"

	"hh-ai-responder/internal/usecase/autochatreply"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

func chatReplyReviewReason(mode string, dryRun bool, employerMessage string) string {
	return chatReplyReviewReasonForContext(mode, dryRun, employerMessage, "", nil, "")
}

// chatReplyReviewReasonForContext decides whether a generated reply may be
// sent. All conversation data is untrusted input, so a high-risk signal in
// any relevant source routes the reply to manual review.
func chatReplyReviewReasonForContext(mode string, dryRun bool, lastEmployerMessage, history string, replyOptions []string, generatedReply string) string {
	buttons := make([]autochatreply.Button, 0, len(replyOptions))
	for _, option := range replyOptions {
		buttons = append(buttons, autochatreply.Button{Text: option})
	}
	reason := autochatreply.Review(autochatreply.Input{
		EmployerMessage: lastEmployerMessage,
		History:         []autochatreply.HistoryMessage{{Text: history}},
		Buttons:         buttons,
	}, generatedReply)
	if reason != "" {
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

func isReplyOption(reply string, options []string) bool {
	reply = strings.TrimSpace(reply)
	for _, option := range options {
		if reply == strings.TrimSpace(option) {
			return true
		}
	}
	return false
}

// classifyHighRiskChatMessage returns a short review reason. The classifier is
// intentionally conservative: a false positive costs a manual review, while a
// false negative could send a material decision on the candidate's behalf.
func classifyHighRiskChatMessage(message string) string {
	return conversationpolicy.HighRiskMessageReason(message)
}
