package runtime

import (
	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/conversation"
)

// These aliases keep staged root consumers source-compatible while the
// application package owns the aggregate and its intrinsic value types.
// They are transitional and should disappear as later boundaries migrate
// callers directly to internal/application.
type JobApplication = application.JobApplication
type ApplicationStatus = application.Status
type ApplicationSource = application.Source
type ApplicationEventType = application.EventType
type ApplicationEvent = application.Event
type ConversationFollowUpState = conversation.FollowUpState
type NextAction = application.NextAction

const (
	ApplicationDiscovered      = application.StatusDiscovered
	ApplicationAnalyzed        = application.StatusAnalyzed
	ApplicationShortlisted     = application.StatusShortlisted
	ApplicationApplied         = application.StatusApplied
	ApplicationEmployerReplied = application.StatusEmployerReplied
	ApplicationInterview       = application.StatusInterview
	ApplicationOffer           = application.StatusOffer
	ApplicationRejected        = application.StatusRejected
	ApplicationArchived        = application.StatusArchived
	ApplicationUnknown         = application.StatusUnknown

	ApplicationSourceHH     = application.SourceHH
	ApplicationSourceManual = application.SourceManual

	ConversationFollowUpNone      = conversation.FollowUpNone
	ConversationFollowUpEligible  = conversation.FollowUpEligible
	ConversationFollowUpDrafted   = conversation.FollowUpDrafted
	ConversationFollowUpSent      = conversation.FollowUpSent
	ConversationFollowUpDismissed = conversation.FollowUpDismissed

	NextActionWaitingCandidateReply = application.NextActionWaitingCandidateReply
	NextActionWaitingEmployerReply  = application.NextActionWaitingEmployerReply
	NextActionPrepareInterview      = application.NextActionPrepareInterview
	NextActionFollowUpPossible      = application.NextActionFollowUpPossible

	ApplicationEventCreated            = application.EventCreated
	ApplicationEventMatched            = application.EventMatched
	ApplicationEventApplied            = application.EventApplied
	ApplicationEventMessageReceived    = application.EventMessageReceived
	ApplicationEventInterviewScheduled = application.EventInterviewScheduled
	ApplicationEventStatusChanged      = application.EventStatusChanged
	ApplicationEventRejected           = application.EventRejected
	ApplicationEventOfferReceived      = application.EventOfferReceived
	ApplicationEventFollowUpSent       = application.EventFollowUpSent
	ApplicationEventLinkedConversation = application.EventLinkedConversation
	ApplicationEventLinkedVacancy      = application.EventLinkedVacancy
	ApplicationEventPartialCreated     = application.EventPartialCreated
	ApplicationEventVacancyEnriched    = application.EventVacancyEnriched
	ApplicationEventMatchBackfilled    = application.EventMatchBackfilled
)

func validateApplication(value JobApplication) error { return value.Validate() }

func validateApplicationEvent(value ApplicationEvent) error { return value.Validate() }

func applicationStatusValid(value ApplicationStatus) bool { return application.IsValidStatus(value) }

func applicationEventTypeForStatus(status ApplicationStatus) ApplicationEventType {
	return application.EventTypeForStatus(status)
}
