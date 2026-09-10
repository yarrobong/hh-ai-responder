package runtime

import (
	"time"

	candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

// Deterministic candidate-question classification remains owned by the
// candidate-context use case. Conversation terminal state and reply policy
// are delegated to conversationpolicy.
func classifyEmployerMessage(text string) EmployerMessageIntent {
	return candidatecontext.ClassifyEmployerMessage(text)
}
func instructionNeedsUserConfirmation(text string) bool {
	return candidatecontext.InstructionNeedsUserConfirmation(text)
}
func userConfirmationQuestion(text string) string {
	return candidatecontext.UserConfirmationQuestion(text)
}
func candidateContainsAny(text string, markers ...string) bool {
	return candidatecontext.ContainsAny(text, markers...)
}
func resolveAtomicFacts(query string, view EmployerSafeCandidateKnowledge) []ResolvedFact {
	return candidatecontext.ResolveAtomicFacts(query, view)
}

func externalInterviewAction(text string, at, now time.Time) *ExternalActionRequirement {
	result := conversationpolicy.ExternalInterviewAction(text, at, now)
	if result != nil && !at.IsZero() && !now.IsZero() {
		result.Aging = pilotFreshness(at, now)
	}
	return result
}

func terminalEmployerMessage(text string) bool {
	return conversationpolicy.TerminalEmployerMessage(text)
}

func terminalConversationState(c EmployerConversation) (ConversationStatus, bool) {
	// HH raw-status mapping remains an adapter concern. The normalized terminal
	// decision itself is delegated to conversationpolicy.
	if status, known := MapHHApplicationStatus(c.RawStatus); known {
		switch status {
		case ApplicationRejected:
			return ConversationRejected, true
		case ApplicationArchived:
			return ConversationClosed, true
		}
	}
	return conversationpolicy.TerminalConversationState(c)
}

func conversationReplyRequirement(c EmployerConversation, latest *ConversationMessage, intent EmployerMessageIntent) ConversationReplyRequirement {
	return ConversationReplyRequirement(conversationpolicy.EvaluateReplyPolicy(conversationpolicy.ReplyPolicyInput{Conversation: c, Latest: latest, Intent: intent}).Requirement)
}
