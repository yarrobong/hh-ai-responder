package runtime

import (
	"time"

	"hh-ai-responder/internal/conversation"
)

// Conversation model aliases preserve the staged root-package API while the
// persisted aggregate and its intrinsic values live in internal/conversation.
// Storage, HH import, AI, eligibility, and follow-up policy remain root-owned.
type ConversationStatus = conversation.Status
type ConversationSender = conversation.Sender
type ConversationSource = conversation.Source
type ConversationDirection = conversation.Direction
type ConversationMessage = conversation.Message
type CandidateConversationClaim = conversation.Claim
type ConversationExperienceClaim = conversation.ExperienceClaim
type ConversationSummary = conversation.Summary
type EmployerConversation = conversation.EmployerConversation
type ConversationState = conversation.State

const (
	ConversationApplied                 = conversation.StatusApplied
	ConversationEmployerReplied         = conversation.StatusEmployerReplied
	ConversationCandidateActionRequired = conversation.StatusCandidateActionRequired
	ConversationWaitingEmployer         = conversation.StatusWaitingEmployer
	ConversationInterview               = conversation.StatusInterview
	ConversationOffer                   = conversation.StatusOffer
	ConversationRejected                = conversation.StatusRejected
	ConversationClosed                  = conversation.StatusClosed

	ConversationSenderUnknown    = conversation.SenderUnknown
	ConversationSenderCandidate  = conversation.SenderCandidate
	ConversationSenderEmployer   = conversation.SenderEmployer
	ConversationSenderSystem     = conversation.SenderSystem
	ConversationSourceHH         = conversation.SourceHH
	ConversationSourceHHWrite    = conversation.SourceHHWrite
	ConversationSourceManual     = conversation.SourceManual
	ConversationSourceAIDraft    = conversation.SourceAIDraft
	ConversationDirectionUnknown = conversation.DirectionUnknown
	ConversationIncoming         = conversation.DirectionIncoming
	ConversationOutgoing         = conversation.DirectionOutgoing
)

// sameConversationMessage is a narrow compatibility wrapper for root storage
// and PostgreSQL code. The identity semantics are owned by the model package.
func sameConversationMessage(a, b ConversationMessage) bool {
	return conversation.SameMessage(a, b)
}

func sameConversationTime(a, b *time.Time) bool {
	return conversation.SameTime(a, b)
}

// deliveredMessages is retained for root storage/workflow compatibility; the
// timeline filtering itself belongs to the conversation model.
func deliveredMessages(values []ConversationMessage) []ConversationMessage {
	return conversation.DeliveredMessages(values)
}
