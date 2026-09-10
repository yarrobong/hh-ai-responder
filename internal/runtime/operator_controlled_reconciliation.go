package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
	"hh-ai-responder/internal/usecase/hhwritereconcile"
)

// ControlledReconciler is the dashboard/CLI-facing read-only boundary. The
// implementation below owns only local action evidence persistence and the
// existing hhwritereconcile matcher; it has no HHWriteClient field.
type ControlledReconciler interface {
	ReconcileDelivery(context.Context, string) (HHWriteResult, error)
}

type operatorControlledReconciler struct {
	actions       *ApprovedHHActionStore
	conversations *ConversationStore
	reader        hhwritereconcile.ChatDeliveryReader
}

func (r *operatorControlledReconciler) ReconcileDelivery(ctx context.Context, actionID string) (HHWriteResult, error) {
	result := HHWriteResult{ActionID: actionID, Timestamp: time.Now().UTC()}
	if r == nil || r.actions == nil || r.conversations == nil || r.reader == nil {
		result.Error = "controlled reconciliation is unavailable"
		return result, errors.New(result.Error)
	}
	action, err := r.actions.Get(strings.TrimSpace(actionID))
	if err != nil {
		result.Error = "action not found"
		return result, err
	}
	result.Status, result.ExternalMessageID = action.Status, action.ExternalMessageID
	if !controlledReconciliationState(action.Status) {
		result.Success = action.Status == HHWriteDeliveryConfirmed
		return result, nil
	}
	conversation, err := r.conversations.GetConversation(action.ConversationID)
	if err != nil {
		result.Error = "conversation not found for reconciliation"
		return result, err
	}
	outcome := hhwrite.OutcomeAmbiguous
	if action.Status == HHWriteSent || action.Status == HHWriteSentUnconfirmed {
		outcome = hhwrite.OutcomeAccepted
	}
	decision, readErr := hhwritereconcile.NewService(hhwritereconcile.Dependencies{ChatDeliveryReader: r.reader}).Reconcile(ctx, hhwritereconcile.AttemptEvidence{
		ActionID: action.ID, Operation: hhwritereconcile.OperationChatMessage,
		TargetConversationID: conversation.HHConversationID, ProviderMessageID: action.ExternalMessageID,
		AttemptedAt: derefWriteAttempt(action.SentAt, action.UpdatedAt), TransportOutcome: hhwritereconcileTransportOutcome(outcome), ExistingActionState: hhwritereconcile.ActionState(action.Status),
	})
	result.Status = HHWriteActionStatus(decision.Transition.To)
	if result.Status == "" {
		result.Status = action.Status
	}
	result.Success = decision.Status == hhwritereconcile.StatusConfirmed
	if decision.Transition.To != "" && decision.Transition.To != hhwritereconcile.ActionState(action.Status) {
		action.Status = HHWriteActionStatus(decision.Transition.To)
		action.Error = ""
		if !result.Success {
			action.Error = decision.Transition.Reason
		}
		action.UpdatedAt = time.Now().UTC()
		if err := r.actions.put(action); err != nil {
			result.Error = err.Error()
			return result, err
		}
		if err := r.actions.Save(); err != nil {
			result.Error = err.Error()
			return result, err
		}
	}
	if readErr != nil {
		result.Error = readErr.Error()
		return result, readErr
	}
	return result, nil
}

func controlledReconciliationState(state HHWriteActionStatus) bool {
	switch state {
	case HHWriteSending, HHWriteSent, HHWriteSentUnconfirmed, HHWriteDeliveryUncertain, HHWriteManualReview:
		return true
	default:
		return false
	}
}
