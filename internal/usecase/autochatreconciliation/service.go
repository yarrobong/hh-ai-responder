package autochatreconciliation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
	reliabilitynotifications "hh-ai-responder/internal/usecase/reliabilitynotifications"
)

var (
	ErrNotConfigured       = errors.New("auto-chat reconciliation is not configured")
	ErrNotReconcilable     = errors.New("auto-chat attempt is not reconcilable")
	ErrEvidenceUnavailable = errors.New("auto-chat reconciliation evidence is unavailable")
	ErrPersistence         = errors.New("auto-chat reconciliation persistence failed")
)

type Dependencies struct {
	Attempts      AttemptReader
	Reader        HistoryReader
	Writer        ReconciliationWriter
	Now           func() time.Time
	Notifications reliabilitynotifications.Sink
}

type Service struct{ deps Dependencies }

func NewService(deps Dependencies) *Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{deps: deps}
}

func (s *Service) Reconcile(ctx context.Context, attemptID string) (Result, error) {
	if ctx == nil {
		return Result{}, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if s == nil || s.deps.Attempts == nil || s.deps.Reader == nil || s.deps.Writer == nil {
		return Result{}, ErrNotConfigured
	}
	attempt, err := s.deps.Attempts.GetByID(ctx, strings.TrimSpace(attemptID))
	if err != nil {
		return Result{}, err
	}
	result := Result{Attempt: attempt, PreviousState: attempt.State, NewState: attempt.State}
	if attempt.ActionType != domain.ActionReply || !replyReconcilable(attempt.State) {
		result.Status = statusForState(attempt)
		result.Reason = "auto-chat attempt is not an eligible reply reconciliation target"
		if attempt.ActionType == domain.ActionLeave && replyReconcilable(attempt.State) {
			result.Status = StatusUnsupported
			result.Reason = "HH does not expose strong positive leave evidence"
		}
		s.project(ctx, reliabilitynotifications.AutoChatReconciliation{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), Status: string(result.Status)})
		return result, nil
	}

	result.ReadAttempted = true
	snapshot, readErr := s.deps.Reader.ReadConversationHistory(ctx, attempt.ConversationID)
	if readErr != nil {
		result.Status = StatusUnavailable
		result.Reason = readErr.Error()
		return result, fmt.Errorf("%w: %v", ErrEvidenceUnavailable, readErr)
	}
	evidence, status, reason := classify(attempt, snapshot, s.deps.Now)
	result.Evidence, result.Status, result.Reason = evidence, status, reason
	if status != StatusConfirmed && status != StatusInsufficient && status != StatusConflicting {
		return result, nil
	}
	if err := s.deps.Writer.RecordReconciliation(ctx, attempt.AttemptID, evidence, s.deps.Now().UTC()); err != nil {
		result.Status = StatusPersistence
		result.Reason = err.Error()
		s.project(ctx, reliabilitynotifications.AutoChatReconciliation{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), Status: string(result.Status), EvidenceKind: string(result.Evidence.Kind)})
		return result, fmt.Errorf("%w: %v", ErrPersistence, err)
	}
	if updated, err := s.deps.Attempts.GetByID(ctx, attempt.AttemptID); err == nil {
		result.Attempt, result.NewState = updated, updated.State
	} else if status == StatusConfirmed {
		result.NewState = domain.StateTargetReplyConfirmed
	}
	s.project(ctx, reliabilitynotifications.AutoChatReconciliation{AttemptID: attempt.AttemptID, ConversationID: attempt.ConversationID, TriggerMessageID: attempt.TriggerMessageID, ActionType: string(attempt.ActionType), Status: string(result.Status), EvidenceKind: string(result.Evidence.Kind)})
	return result, nil
}

func (s *Service) project(ctx context.Context, event reliabilitynotifications.AutoChatReconciliation) {
	if s != nil && s.deps.Notifications != nil {
		_ = s.deps.Notifications.ProjectAutoChatReconciliation(ctx, event)
	}
}

func replyReconcilable(state domain.State) bool {
	return state == domain.StateSending || state == domain.StateAccepted || state == domain.StateDeliveryUncertain || state == domain.StateTargetReplyConfirmed
}

func statusForState(attempt domain.Attempt) Status {
	if attempt.State == domain.StateTargetReplyConfirmed || attempt.State == domain.StateTargetLeaveConfirmed {
		return StatusConfirmed
	}
	return StatusNotApplicable
}

func classify(attempt domain.Attempt, snapshot HistorySnapshot, now func() time.Time) (domain.ReconciliationEvidence, Status, string) {
	evidence := domain.ReconciliationEvidence{Source: "hh_conversation_history", ObservedAt: snapshot.ObservedAt}
	if evidence.ObservedAt.IsZero() {
		evidence.ObservedAt = now().UTC()
	}
	if strings.TrimSpace(snapshot.ConversationID) != strings.TrimSpace(attempt.ConversationID) {
		evidence.Kind = domain.EvidenceConflicting
		return evidence, StatusConflicting, "provider history belongs to a different conversation"
	}
	messages := snapshot.Messages
	triggerIndex := -1
	for i, message := range messages {
		if strings.TrimSpace(message.ProviderMessageID) != strings.TrimSpace(attempt.TriggerMessageID) {
			continue
		}
		if !isEmployer(message) {
			evidence.Kind = domain.EvidenceConflicting
			return evidence, StatusConflicting, "trigger identity has a non-employer direction"
		}
		if triggerIndex >= 0 {
			evidence.Kind = domain.EvidenceConflicting
			return evidence, StatusConflicting, "trigger identity is duplicated in provider history"
		}
		triggerIndex = i
	}
	if triggerIndex < 0 {
		evidence.Kind = domain.EvidenceInsufficient
		return evidence, StatusInsufficient, "exact trigger message is absent; absence is not proof of non-delivery"
	}

	trigger := messages[triggerIndex]
	var candidates []HistoryMessage
	for i, message := range messages {
		if i == triggerIndex || !isCandidate(message) || !strictlyAfter(message.Timestamp, trigger.Timestamp) {
			continue
		}
		blockedByLaterTrigger := false
		for j, later := range messages {
			if j == triggerIndex || !isEmployer(later) || !strictlyAfter(later.Timestamp, trigger.Timestamp) {
				continue
			}
			if !later.Timestamp.After(message.Timestamp) {
				blockedByLaterTrigger = true
				break
			}
		}
		if !blockedByLaterTrigger {
			candidates = append(candidates, message)
		}
	}
	if expected := strings.TrimSpace(attempt.ProviderOutgoingMessageID); expected != "" {
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.ProviderMessageID) == expected {
				evidence.Kind, evidence.ProviderMessageID = domain.EvidenceExactOutgoingMessage, expected
				return evidence, StatusConfirmed, "exact persisted outgoing provider ID is present after the exact trigger"
			}
		}
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate.ProviderMessageID) != "" {
				evidence.Kind, evidence.ProviderMessageID = domain.EvidenceConflicting, candidate.ProviderMessageID
				return evidence, StatusConflicting, "history contains a different outgoing provider ID; exact identity was not replaced"
			}
		}
		evidence.Kind = domain.EvidenceInsufficient
		return evidence, StatusInsufficient, "the persisted outgoing provider ID was not found; absence is not proof of non-delivery"
	}
	if len(candidates) == 0 {
		evidence.Kind = domain.EvidenceInsufficient
		return evidence, StatusInsufficient, "no candidate response is ordered after the exact trigger; absence is not proof of non-delivery"
	}
	evidence.Kind = domain.EvidenceTriggerResponse
	evidence.ProviderMessageID = candidates[0].ProviderMessageID
	evidence.CausalityNote = "target reply confirmed; this does not prove that this automatic AttemptID sent the response"
	return evidence, StatusConfirmed, "a candidate response is ordered after the exact trigger and before any later employer trigger"
}

func strictlyAfter(value, boundary time.Time) bool {
	return !value.IsZero() && !boundary.IsZero() && value.After(boundary)
}

func isCandidate(message HistoryMessage) bool {
	sender, direction := strings.ToLower(strings.TrimSpace(message.Sender)), strings.ToLower(strings.TrimSpace(message.Direction))
	return (sender == "candidate" || sender == "applicant") && (direction == "outgoing" || direction == "sent" || direction == "from_candidate")
}

func isEmployer(message HistoryMessage) bool {
	sender, direction := strings.ToLower(strings.TrimSpace(message.Sender)), strings.ToLower(strings.TrimSpace(message.Direction))
	return (sender == "employer" || sender == "recruiter" || sender == "company") && (direction == "incoming" || direction == "received" || direction == "from_employer")
}
