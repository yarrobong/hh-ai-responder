package application

import (
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/conversation"
	"hh-ai-responder/internal/vacancy"
)

// Persistence adapters share these normalized application identity errors.
// They live beside the aggregate contract so no backend adapter owns the
// meaning of the error or has to depend on another adapter.
var (
	ErrApplicationNotFound    = errors.New("application not found")
	ErrDuplicateExternalID    = errors.New("duplicate application external id")
	ErrDuplicateApplicationID = errors.New("application id already exists")
)

// Status is the lifecycle status of a job application.
type Status string

const (
	StatusDiscovered      Status = "discovered"
	StatusAnalyzed        Status = "analyzed"
	StatusShortlisted     Status = "shortlisted"
	StatusApplied         Status = "applied"
	StatusEmployerReplied Status = "employer_replied"
	StatusInterview       Status = "interview"
	StatusOffer           Status = "offer"
	StatusRejected        Status = "rejected"
	StatusArchived        Status = "archived"
	StatusUnknown         Status = "unknown"
)

// Source identifies where an application record originated.
type Source string

const (
	SourceHH     Source = "hh"
	SourceManual Source = "manual"
)

// FollowUpState is retained as an application-package alias for source
// compatibility. The persisted value is semantically owned by conversation;
// follow-up policy and orchestration remain outside both domain packages.
type FollowUpState = conversation.FollowUpState

const (
	FollowUpNone      = conversation.FollowUpNone
	FollowUpEligible  = conversation.FollowUpEligible
	FollowUpDrafted   = conversation.FollowUpDrafted
	FollowUpSent      = conversation.FollowUpSent
	FollowUpDismissed = conversation.FollowUpDismissed
)

// NextAction values are stable application/workflow markers. The aggregate
// stores the field as a string for compatibility with existing JSON and HH
// data, so callers may also retain provider-specific values.
type NextAction string

const (
	NextActionWaitingCandidateReply NextAction = "waiting_candidate_reply"
	NextActionWaitingEmployerReply  NextAction = "waiting_employer_reply"
	NextActionPrepareInterview      NextAction = "prepare_interview"
	NextActionFollowUpPossible      NextAction = "follow_up_possible"
)

// JobApplication is the application aggregate snapshot. Vacancy and
// conversation relations are represented by identifiers or persisted value
// data; this type performs no lookups and has no transport or storage access.
type JobApplication struct {
	FollowUpState          FollowUpState                    `json:"follow_up_state,omitempty"`
	ID                     string                           `json:"id"`
	VacancyID              int                              `json:"vacancy_id"`
	ExternalID             string                           `json:"external_id,omitempty"`
	CompanyName            string                           `json:"company_name"`
	VacancyTitle           string                           `json:"vacancy_title"`
	VacancyURL             string                           `json:"vacancy_url,omitempty"`
	Source                 Source                           `json:"source"`
	CreatedAt              time.Time                        `json:"created_at"`
	UpdatedAt              time.Time                        `json:"updated_at"`
	Status                 Status                           `json:"status"`
	MatchResult            *vacancy.MatchResult             `json:"match_result,omitempty"`
	ConversationID         string                           `json:"conversation_id,omitempty"`
	Notes                  string                           `json:"notes,omitempty"`
	NextAction             string                           `json:"next_action,omitempty"`
	RawStatus              string                           `json:"raw_status,omitempty"`
	HHMetadata             map[string]string                `json:"hh_metadata,omitempty"`
	Partial                bool                             `json:"partial,omitempty"`
	DataCompleteness       vacancy.DataCompleteness         `json:"data_completeness,omitempty"`
	ReconciliationEvidence []vacancy.ReconciliationEvidence `json:"reconciliation_evidence,omitempty"`
}

// EventType identifies an entry in an application's append-only history.
type EventType string

const (
	EventCreated            EventType = "created"
	EventMatched            EventType = "matched"
	EventApplied            EventType = "applied"
	EventMessageReceived    EventType = "message_received"
	EventInterviewScheduled EventType = "interview_scheduled"
	EventStatusChanged      EventType = "status_changed"
	EventRejected           EventType = "rejected"
	EventOfferReceived      EventType = "offer_received"
	EventFollowUpSent       EventType = "follow_up_sent"
	EventLinkedConversation EventType = "application_linked_to_conversation"
	EventLinkedVacancy      EventType = "application_linked_to_vacancy"
	EventPartialCreated     EventType = "partial_application_created"
	EventVacancyEnriched    EventType = "vacancy_enriched"
	EventMatchBackfilled    EventType = "match_backfilled"
)

// Event is an application-domain history entry. Persistence of events is
// intentionally handled by the root stores and repositories.
type Event struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	Timestamp     time.Time `json:"timestamp"`
	Type          EventType `json:"type"`
	Description   string    `json:"description"`
}

// Validate checks intrinsic application identity, timestamp, enum, and
// embedded vacancy-value invariants.
func (a JobApplication) Validate() error {
	if strings.TrimSpace(a.ID) == "" || a.VacancyID < 0 || a.CreatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return errors.New("invalid application identity or timestamps")
	}
	if !IsValidSource(a.Source) || !IsValidStatus(a.Status) {
		return errors.New("invalid application source or status")
	}
	if a.MatchResult != nil {
		if err := a.MatchResult.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the intrinsic event shape. Event IDs remain generated by
// the persistence/application service boundary because generation is not a
// pure domain rule.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ApplicationID) == "" || e.Timestamp.IsZero() ||
		!IsValidEventType(e.Type) || strings.TrimSpace(e.Description) == "" {
		return errors.New("invalid application event")
	}
	return nil
}

// IsValidStatus reports whether the status is part of the persisted contract.
func IsValidStatus(value Status) bool {
	switch value {
	case StatusDiscovered, StatusAnalyzed, StatusShortlisted, StatusApplied,
		StatusEmployerReplied, StatusInterview, StatusOffer, StatusRejected,
		StatusArchived, StatusUnknown:
		return true
	default:
		return false
	}
}

// IsValidSource reports whether the source is part of the persisted contract.
func IsValidSource(value Source) bool {
	return value == SourceHH || value == SourceManual
}

// IsValidEventType reports whether the event type is part of the persisted
// application history contract.
func IsValidEventType(value EventType) bool {
	switch value {
	case EventCreated, EventMatched, EventApplied, EventMessageReceived,
		EventInterviewScheduled, EventStatusChanged, EventRejected,
		EventOfferReceived, EventFollowUpSent, EventLinkedConversation,
		EventLinkedVacancy, EventPartialCreated, EventVacancyEnriched,
		EventMatchBackfilled:
		return true
	default:
		return false
	}
}

// EventTypeForStatus maps a status transition to its intrinsic event kind.
// The mapping intentionally preserves the existing default status_changed
// behavior for statuses without a dedicated event.
func EventTypeForStatus(status Status) EventType {
	switch status {
	case StatusApplied:
		return EventApplied
	case StatusInterview:
		return EventInterviewScheduled
	case StatusOffer:
		return EventOfferReceived
	case StatusRejected:
		return EventRejected
	default:
		return EventStatusChanged
	}
}
