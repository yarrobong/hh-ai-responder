package runtime

import (
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/usecase/conversationpolicy"
)

const ConversationManualReview ConversationStatus = conversationpolicy.StateManualReview

type ConversationResolution = conversationpolicy.Resolution
type ConversationStateResolver struct{}

// Resolve is a compatibility adapter. Root code still gathers application
// and HH mapping inputs, while semantic state policy has one implementation
// in conversationpolicy.
func (ConversationStateResolver) Resolve(a JobApplication, c EmployerConversation, appliedAt *time.Time, pending bool, warnings []string, now time.Time) ConversationResolution {
	evidence := []conversationpolicy.StatusEvidence{
		normalizeRawStatus(a.RawStatus, a.Source == ApplicationSourceHH),
		normalizeRawStatus(c.RawStatus, c.HHConversationID != ""),
	}
	return conversationpolicy.ResolveState(conversationpolicy.ResolveStateInput{
		Conversation:          c,
		ApplicationState:      normalizeApplicationStatus(a.Status),
		ApplicationNextAction: a.NextAction,
		RawStatusEvidence:     evidence,
		AppliedAt:             appliedAt,
		Pending:               pending,
		Warnings:              warnings,
		Now:                   now,
	})
}

func normalizeRawStatus(raw string, required bool) conversationpolicy.StatusEvidence {
	if raw == "" && !required {
		return conversationpolicy.StatusEvidence{}
	}
	status, known := MapHHApplicationStatus(raw)
	return conversationpolicy.StatusEvidence{Status: normalizeApplicationStatus(status), Known: known, Required: required}
}

func normalizeApplicationStatus(status ApplicationStatus) conversationpolicy.State {
	switch status {
	case ApplicationRejected:
		return conversation.StatusRejected
	case ApplicationOffer:
		return conversation.StatusOffer
	case ApplicationArchived:
		return conversation.StatusClosed
	case ApplicationInterview:
		return conversation.StatusInterview
	default:
		return ""
	}
}
