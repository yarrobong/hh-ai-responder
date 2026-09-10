package runtime

import candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
import "hh-ai-responder/internal/usecase/conversationpolicy"

// Transitional aliases keep the root API stable while candidate-context policy
// is owned by the cross-domain use case package.
type CandidateContext = candidatecontext.CandidateContext
type CandidateContextProject = candidatecontext.CandidateContextProject
type CandidateContextAchievement = candidatecontext.CandidateContextAchievement
type CandidateMissingInformation = candidatecontext.CandidateMissingInformation
type ResolvedFact = candidatecontext.ResolvedFact
type ResolvedFactStatus = candidatecontext.ResolvedFactStatus
type EmployerMessageIntent = candidatecontext.EmployerMessageIntent

type ExternalActionRequirement = conversationpolicy.ExternalActionRequirement
type ConversationReplyRequirement = conversationpolicy.ReplyRequirement

const (
	ReplyRequired             = conversationpolicy.ReplyRequired
	ReplyOptional             = conversationpolicy.ReplyOptional
	NoReplyNeeded             = conversationpolicy.NoReplyNeeded
	ConversationReplyRequired = ReplyRequired
	ConversationReplyOptional = ReplyOptional
	ConversationNoReplyNeeded = NoReplyNeeded
)

const (
	CandidateContextStatusAnswerable               = candidatecontext.CandidateContextStatusAnswerable
	CandidateContextStatusUserConfirmationRequired = candidatecontext.CandidateContextStatusUserConfirmationRequired
	ResolvedFactAnswerable                         = candidatecontext.ResolvedFactAnswerable
	ResolvedFactPartiallyAnswerable                = candidatecontext.ResolvedFactPartiallyAnswerable
	ResolvedFactUnknown                            = candidatecontext.ResolvedFactUnknown
	ResolvedFactRestricted                         = candidatecontext.ResolvedFactRestricted
	EmployerMessageIntentFactualQuestion           = candidatecontext.EmployerMessageIntentFactualQuestion
	EmployerMessageIntentInstruction               = candidatecontext.EmployerMessageIntentInstruction
	EmployerMessageIntentInterviewInvitation       = candidatecontext.EmployerMessageIntentInterviewInvitation
	EmployerMessageIntentStatusMessage             = candidatecontext.EmployerMessageIntentStatusMessage
	EmployerMessageIntentRejection                 = candidatecontext.EmployerMessageIntentRejection
	EmployerMessageIntentAcknowledgement           = candidatecontext.EmployerMessageIntentAcknowledgement
	EmployerMessageIntentGeneralMessage            = candidatecontext.EmployerMessageIntentGeneralMessage
	EmployerMessageIntentCompound                  = candidatecontext.EmployerMessageIntentCompound
	EmployerMessageIntentTerminal                  = candidatecontext.EmployerMessageIntentTerminal
	ExternalActionRequired                         = conversationpolicy.ExternalActionRequired
	ExternalActionInterviewInvitation              = conversationpolicy.ExternalActionInterviewInvitation
)

func emptyCandidateContext() CandidateContext { return candidatecontext.Empty() }
