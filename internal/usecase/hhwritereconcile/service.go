package hhwritereconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Dependencies struct {
	ChatDeliveryReader ChatDeliveryReader
	Now                func() time.Time
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}
}

// Reconcile performs one bounded targeted read, or no read for an action that
// is already terminal/non-reconcilable. It never changes local state itself.
func (s *Service) Reconcile(ctx context.Context, attempt AttemptEvidence) (Result, error) {
	result := Result{
		ActionID:      attempt.ActionID,
		Operation:     attempt.Operation,
		MatchStrength: MatchNoMatch,
		Transition:    TransitionIntent{From: attempt.ExistingActionState, To: attempt.ExistingActionState},
	}
	if result.Operation == "" {
		result.Operation = OperationChatMessage
	}
	if ctx == nil {
		return result, errors.New("reconciliation context is nil")
	}
	if err := ctx.Err(); err != nil {
		result.Status = StatusRemoteUnavailable
		result.MatchStrength = MatchRemoteUnavailable
		return result, err
	}
	if attempt.Operation != "" && attempt.Operation != OperationChatMessage {
		result.Status = StatusNotApplicable
		result.MatchStrength = MatchNoMatch
		result.Transition.Reason = "operation has no reconciliation contract"
		return result, nil
	}
	if attempt.TransportOutcome == TransportRejected || attempt.TransportOutcome == TransportNotSent {
		result.Status = StatusNotApplicable
		result.Transition.Reason = "transport proved that the mutation was not dispatched"
		return result, nil
	}
	if !reconcilable(attempt.ExistingActionState) {
		result.Status = StatusNotApplicable
		result.Transition.Reason = "action state is not reconcilable"
		if attempt.ExistingActionState == ActionDeliveryConfirmed {
			result.MatchStrength = MatchStrongExisting
		}
		return result, nil
	}

	if s == nil || s.deps.ChatDeliveryReader == nil {
		return s.unavailable(result, attempt, errors.New("chat delivery reader is unavailable"))
	}
	if strings.TrimSpace(attempt.TargetConversationID) == "" {
		return s.unavailable(result, attempt, errors.New("target conversation identity is unavailable"))
	}
	if strings.TrimSpace(attempt.ProviderMessageID) == "" {
		result.Status = uncertaintyStatus(attempt)
		result.MatchStrength = MatchNoMatch
		result.Transition = safeTransition(attempt, Evidence{ConversationID: attempt.TargetConversationID, MatchStrength: MatchNoMatch, ObservedAt: s.now()}, "provider message identity is unavailable; delivery cannot be confirmed")
		return result, nil
	}

	result.ReadAttempted = true
	snapshot, err := s.deps.ChatDeliveryReader.ReadChatDelivery(ctx, attempt.TargetConversationID)
	if err != nil {
		return s.unavailable(result, attempt, err)
	}
	observedAt := snapshot.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.deps.Now().UTC()
	}
	if strings.TrimSpace(snapshot.ConversationID) != attempt.TargetConversationID {
		result.Status = uncertaintyStatus(attempt)
		result.MatchStrength = MatchConflict
		result.Transition = safeTransition(attempt, Evidence{ConversationID: snapshot.ConversationID, MatchStrength: MatchConflict, ObservedAt: observedAt}, "provider read returned a different conversation")
		return result, nil
	}

	message, found := matchingMessage(snapshot.Messages, attempt)
	if found {
		result.Status = StatusConfirmed
		result.MatchStrength = MatchExactProviderID
		result.Evidence = Evidence{ConversationID: snapshot.ConversationID, ProviderID: message.ProviderID, MatchStrength: MatchExactProviderID, Sender: message.Sender, Direction: message.Direction, ObservedAt: observedAt}
		result.Transition = TransitionIntent{From: attempt.ExistingActionState, To: ActionDeliveryConfirmed, Reason: "fresh targeted provider message matched exact provider ID", Evidence: result.Evidence}
		return result, nil
	}

	result.Status = uncertaintyStatus(attempt)
	result.MatchStrength = MatchNoMatch
	result.Transition = safeTransition(attempt, Evidence{ConversationID: snapshot.ConversationID, ProviderID: attempt.ProviderMessageID, MatchStrength: MatchNoMatch, ObservedAt: observedAt}, "fresh read did not expose the expected message; absence is not proof of non-delivery")
	return result, nil
}

func (s *Service) unavailable(result Result, attempt AttemptEvidence, err error) (Result, error) {
	result.Status = StatusRemoteUnavailable
	result.MatchStrength = MatchRemoteUnavailable
	result.Transition = safeTransition(attempt, Evidence{ConversationID: attempt.TargetConversationID, ProviderID: attempt.ProviderMessageID, MatchStrength: MatchRemoteUnavailable, ObservedAt: s.now()}, "targeted delivery read failed; provider state remains unknown")
	return result, fmt.Errorf("reconcile chat delivery: %w", err)
}

func (s *Service) now() time.Time {
	if s == nil || s.deps.Now == nil {
		return time.Now().UTC()
	}
	return s.deps.Now().UTC()
}

func reconcilable(state ActionState) bool {
	switch state {
	case ActionSending, ActionSent, ActionSentUnconfirmed, ActionDeliveryUncertain, ActionManualReview:
		return true
	default:
		return false
	}
}

func matchingMessage(messages []ChatDeliveryMessage, attempt AttemptEvidence) (ChatDeliveryMessage, bool) {
	expected := strings.TrimSpace(attempt.ProviderMessageID)
	if expected == "" {
		return ChatDeliveryMessage{}, false
	}
	for _, message := range messages {
		if strings.TrimSpace(message.ProviderID) != expected {
			continue
		}
		if knownWrongSender(message.Sender) || knownWrongDirection(message.Direction) {
			return ChatDeliveryMessage{}, false
		}
		return message, true
	}
	return ChatDeliveryMessage{}, false
}

func knownWrongSender(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "employer", "recruiter", "company", "system":
		return true
	default:
		return false
	}
}

func knownWrongDirection(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "incoming", "received", "from_employer":
		return true
	default:
		return false
	}
}

func uncertaintyStatus(attempt AttemptEvidence) DecisionStatus {
	if attempt.ExistingActionState == ActionManualReview || attempt.ExistingActionState == ActionSending {
		return StatusManualReview
	}
	if attempt.TransportOutcome == TransportAccepted || attempt.ExistingActionState == ActionSent || attempt.ExistingActionState == ActionSentUnconfirmed {
		return StatusNotConfirmed
	}
	return StatusUncertain
}

func safeTransition(attempt AttemptEvidence, evidence Evidence, reason string) TransitionIntent {
	to := attempt.ExistingActionState
	switch {
	case attempt.ExistingActionState == ActionSending:
		to = ActionManualReview
	case attempt.ExistingActionState == ActionSent || attempt.TransportOutcome == TransportAccepted:
		to = ActionSentUnconfirmed
	case attempt.ExistingActionState == ActionSentUnconfirmed:
		to = ActionSentUnconfirmed
	case attempt.ExistingActionState == ActionDeliveryUncertain:
		to = ActionDeliveryUncertain
	case attempt.ExistingActionState == ActionManualReview:
		to = ActionManualReview
	case attempt.TransportOutcome == TransportAmbiguous || attempt.TransportOutcome == TransportPersistenceUncertain:
		to = ActionDeliveryUncertain
	}
	return TransitionIntent{From: attempt.ExistingActionState, To: to, Reason: reason, Evidence: evidence}
}
