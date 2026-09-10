package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
	"hh-ai-responder/internal/usecase/hhwritereconcile"
)

// hhWriteChatDeliveryReader is the composition adapter between the existing
// HH read client and the detached reconciliation use case. A full targeted
// conversation reader is preferred. The state-only fallback preserves the
// legacy read contract used by compatibility fakes and still carries only
// exact provider message identities.
type hhWriteChatDeliveryReader struct {
	source any
}

func (r hhWriteChatDeliveryReader) ReadChatDelivery(ctx context.Context, conversationID string) (hhwritereconcile.ChatDeliverySnapshot, error) {
	if reader, ok := r.source.(HHConversationRecordReader); ok {
		record, err := reader.ReadConversation(ctx, conversationID)
		if err != nil {
			return hhwritereconcile.ChatDeliverySnapshot{}, err
		}
		messages := make([]hhwritereconcile.ChatDeliveryMessage, 0, len(record.Messages))
		for _, message := range record.Messages {
			messages = append(messages, hhwritereconcile.ChatDeliveryMessage{
				ProviderID: message.ExternalID,
				Sender:     message.Sender,
				Direction:  message.Direction,
				Text:       message.Text,
				Timestamp:  message.Timestamp,
			})
		}
		return hhwritereconcile.ChatDeliverySnapshot{ConversationID: record.ExternalID, Messages: messages, ObservedAt: record.UpdatedAt}, nil
	}
	if reader, ok := r.source.(HHConversationPreflightReader); ok {
		state, err := reader.ReadConversationState(ctx, conversationID)
		if err != nil {
			return hhwritereconcile.ChatDeliverySnapshot{}, err
		}
		messages := make([]hhwritereconcile.ChatDeliveryMessage, 0, len(state.MessageIDs)+1)
		seen := make(map[string]struct{}, len(state.MessageIDs)+1)
		for _, id := range state.MessageIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			seen[id] = struct{}{}
			messages = append(messages, hhwritereconcile.ChatDeliveryMessage{ProviderID: id})
		}
		if id := strings.TrimSpace(state.LastMessageID); id != "" {
			if _, exists := seen[id]; !exists {
				messages = append(messages, hhwritereconcile.ChatDeliveryMessage{ProviderID: id})
			}
		}
		return hhwritereconcile.ChatDeliverySnapshot{ConversationID: state.ExternalID, Messages: messages}, nil
	}
	return hhwritereconcile.ChatDeliverySnapshot{}, errors.New("HH targeted chat delivery reader is unavailable")
}

func (g *HHWriteGateway) reconcileDeliveryAttempt(ctx context.Context, action ApprovedHHAction, targetConversationID string, outcome hhwrite.Outcome, providerID string, state HHWriteActionStatus) (hhwritereconcile.Result, error) {
	return hhwritereconcile.NewService(hhwritereconcile.Dependencies{
		ChatDeliveryReader: hhWriteChatDeliveryReader{source: g.ReadClient},
	}).Reconcile(ctx, hhwritereconcile.AttemptEvidence{
		ActionID:             action.ID,
		Operation:            hhwritereconcile.OperationChatMessage,
		TargetConversationID: targetConversationID,
		ProviderMessageID:    providerID,
		ExactText:            action.ApprovedText,
		AttemptedAt:          derefWriteAttempt(action.SentAt, action.UpdatedAt),
		TransportOutcome:     hhwritereconcileTransportOutcome(outcome),
		ExistingActionState:  hhwritereconcile.ActionState(state),
	})
}

func hhwritereconcileTransportOutcome(outcome hhwrite.Outcome) hhwritereconcile.TransportOutcome {
	switch outcome {
	case hhwrite.OutcomeAccepted:
		return hhwritereconcile.TransportAccepted
	case hhwrite.OutcomeRejected:
		return hhwritereconcile.TransportRejected
	case hhwrite.OutcomeNotSent:
		return hhwritereconcile.TransportNotSent
	case hhwrite.OutcomeAmbiguous:
		return hhwritereconcile.TransportAmbiguous
	default:
		return ""
	}
}

func derefWriteAttempt(value *time.Time, fallback time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	return fallback
}

func (g *HHWriteGateway) persistReconciliationTransition(action *ApprovedHHAction, decision hhwritereconcile.Result) error {
	if action == nil || decision.Transition.To == "" || decision.Transition.To == hhwritereconcile.ActionState(action.Status) {
		return nil
	}
	action.Status = HHWriteActionStatus(decision.Transition.To)
	action.Error = ""
	if decision.Status != hhwritereconcile.StatusConfirmed {
		action.Error = decision.Transition.Reason
	}
	action.UpdatedAt = time.Now().UTC()
	if err := g.Actions.put(*action); err != nil {
		return err
	}
	return g.Actions.Save()
}
