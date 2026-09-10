package autochatattempt

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ActionType string

const (
	ActionReply ActionType = "REPLY"
	ActionLeave ActionType = "LEAVE"
)

type State string

const (
	StateSending              State = "SENDING"
	StateAccepted             State = "ACCEPTED"
	StateRejected             State = "REJECTED"
	StateNotSent              State = "NOT_SENT"
	StateDeliveryUncertain    State = "DELIVERY_UNCERTAIN"
	StateTargetReplyConfirmed State = "TARGET_REPLY_CONFIRMED"
	StateTargetLeaveConfirmed State = "TARGET_LEAVE_CONFIRMED"
)

// EvidenceKind describes a bounded provider observation. Target-response
// evidence intentionally does not claim that a particular automatic attempt
// caused the observed candidate message.
type EvidenceKind string

const (
	EvidenceExactOutgoingMessage EvidenceKind = "EXACT_OUTGOING_MESSAGE_CONFIRMED"
	EvidenceTriggerResponse      EvidenceKind = "TRIGGER_RESPONSE_CONFIRMED"
	EvidenceInsufficient         EvidenceKind = "INSUFFICIENT"
	EvidenceUnavailable          EvidenceKind = "UNAVAILABLE"
	EvidenceConflicting          EvidenceKind = "CONFLICTING"
)

type ReconciliationEvidence struct {
	Kind              EvidenceKind `json:"kind,omitempty"`
	Source            string       `json:"source,omitempty"`
	ProviderMessageID string       `json:"provider_message_id,omitempty"`
	ObservedAt        time.Time    `json:"observed_at,omitempty"`
	CausalityNote     string       `json:"causality_note,omitempty"`
}

var (
	ErrInvalidAttempt    = errors.New("invalid auto-chat attempt")
	ErrAttemptNotFound   = errors.New("auto-chat attempt not found")
	ErrTriggerBlocked    = errors.New("auto-chat trigger is already blocked")
	ErrInvalidTransition = errors.New("invalid auto-chat attempt transition")
)

type Attempt struct {
	AttemptID                 string                  `json:"attempt_id"`
	ConversationID            string                  `json:"conversation_id"`
	TriggerMessageID          string                  `json:"trigger_message_id"`
	ActionType                ActionType              `json:"action_type"`
	State                     State                   `json:"state"`
	CreatedAt                 time.Time               `json:"created_at"`
	UpdatedAt                 time.Time               `json:"updated_at"`
	RequestKey                string                  `json:"request_key,omitempty"`
	ProviderOutgoingMessageID string                  `json:"provider_outgoing_message_id,omitempty"`
	ProviderStatus            int                     `json:"provider_status,omitempty"`
	ErrorClass                string                  `json:"error_class,omitempty"`
	Reconciliation            *ReconciliationEvidence `json:"reconciliation,omitempty"`
}

func (a Attempt) Validate() error {
	if strings.TrimSpace(a.AttemptID) == "" || strings.TrimSpace(a.ConversationID) == "" || strings.TrimSpace(a.TriggerMessageID) == "" {
		return fmt.Errorf("%w: attempt, conversation and trigger IDs are required", ErrInvalidAttempt)
	}
	if a.ActionType != ActionReply && a.ActionType != ActionLeave {
		return fmt.Errorf("%w: unsupported action type %q", ErrInvalidAttempt, a.ActionType)
	}
	if a.State != StateSending && a.State != StateAccepted && a.State != StateRejected && a.State != StateNotSent && a.State != StateDeliveryUncertain && a.State != StateTargetReplyConfirmed && a.State != StateTargetLeaveConfirmed {
		return fmt.Errorf("%w: unsupported state %q", ErrInvalidAttempt, a.State)
	}
	if a.ActionType == ActionReply && strings.TrimSpace(a.RequestKey) == "" {
		return fmt.Errorf("%w: reply request key is required", ErrInvalidAttempt)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() || a.UpdatedAt.Before(a.CreatedAt) {
		return fmt.Errorf("%w: timestamps are invalid", ErrInvalidAttempt)
	}
	if a.ActionType == ActionReply && a.State == StateTargetLeaveConfirmed || a.ActionType == ActionLeave && a.State == StateTargetReplyConfirmed {
		return fmt.Errorf("%w: target confirmation does not match action type", ErrInvalidAttempt)
	}
	return nil
}

func ConflictKey(conversationID, triggerMessageID string) string {
	return strings.TrimSpace(conversationID) + "\x00" + strings.TrimSpace(triggerMessageID)
}

func IsBlocking(state State) bool {
	switch state {
	case StateSending, StateAccepted, StateDeliveryUncertain, StateTargetReplyConfirmed, StateTargetLeaveConfirmed:
		return true
	default:
		return false
	}
}

func IsReplayable(state State) bool {
	return state == StateRejected || state == StateNotSent
}

func (a Attempt) WithOutcome(state State, updatedAt time.Time, providerID string, providerStatus int, errorClass string) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if updatedAt.Before(a.CreatedAt) {
		return Attempt{}, fmt.Errorf("%w: outcome timestamp precedes creation", ErrInvalidTransition)
	}
	if !validTransition(a.ActionType, a.State, state) {
		return Attempt{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.State, state)
	}
	a.State, a.UpdatedAt = state, updatedAt
	if providerID != "" {
		a.ProviderOutgoingMessageID = providerID
	}
	a.ProviderStatus, a.ErrorClass = providerStatus, errorClass
	return a, nil
}

// WithReconciliation records one provider observation and applies only the
// existing target-confirmation transition. It never releases a blocking
// attempt and never replaces the persisted outgoing identity.
func (a Attempt) WithReconciliation(evidence ReconciliationEvidence, updatedAt time.Time) (Attempt, error) {
	if err := a.Validate(); err != nil {
		return Attempt{}, err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if updatedAt.Before(a.CreatedAt) {
		updatedAt = a.CreatedAt
	}
	if evidence.ObservedAt.IsZero() {
		evidence.ObservedAt = updatedAt.UTC()
	}
	if evidence.Kind == EvidenceExactOutgoingMessage && strings.TrimSpace(a.ProviderOutgoingMessageID) != strings.TrimSpace(evidence.ProviderMessageID) {
		return Attempt{}, fmt.Errorf("%w: exact outgoing provider identity changed", ErrInvalidTransition)
	}
	if a.ActionType == ActionReply && (evidence.Kind == EvidenceExactOutgoingMessage || evidence.Kind == EvidenceTriggerResponse) {
		if !validTransition(a.ActionType, a.State, StateTargetReplyConfirmed) {
			if a.State != StateTargetReplyConfirmed {
				return Attempt{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.State, StateTargetReplyConfirmed)
			}
		} else {
			a.State = StateTargetReplyConfirmed
		}
	}
	if a.ActionType == ActionLeave && evidence.Kind == EvidenceExactOutgoingMessage {
		return Attempt{}, fmt.Errorf("%w: leave has no exact outgoing message contract", ErrInvalidTransition)
	}
	if a.State != StateTargetReplyConfirmed && a.State != StateTargetLeaveConfirmed || evidence.Kind == EvidenceExactOutgoingMessage || evidence.Kind == EvidenceTriggerResponse {
		a.Reconciliation = &evidence
	}
	a.UpdatedAt = updatedAt.UTC()
	return a, nil
}

func validTransition(action ActionType, from, to State) bool {
	if from == to {
		return true
	}
	switch from {
	case StateSending:
		return to == StateAccepted || to == StateRejected || to == StateNotSent || to == StateDeliveryUncertain || (action == ActionReply && to == StateTargetReplyConfirmed) || (action == ActionLeave && to == StateTargetLeaveConfirmed)
	case StateAccepted, StateDeliveryUncertain:
		return (action == ActionReply && to == StateTargetReplyConfirmed) || (action == ActionLeave && to == StateTargetLeaveConfirmed)
	default:
		return false
	}
}
