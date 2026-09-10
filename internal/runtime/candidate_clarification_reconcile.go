package runtime

import (
	"errors"
	"strings"
	"time"

	"hh-ai-responder/internal/usecase/candidateacquisition"
)

// ReconcileCandidateClarifications closes stale or invalid candidate-data
// requests without deleting their history. Existing records remain auditable;
// no new question is created and no fact is asserted by this operation.
func ReconcileCandidateClarifications(s *CandidateClarificationStore, conversations *ConversationStore, resolver *CandidateContextResolver) (int, error) {
	if s == nil {
		return 0, errors.New("clarification store is nil")
	}
	if resolver == nil {
		return 0, errors.New("clarification reconciliation requires a resolver")
	}
	values, err := s.List()
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, value := range values {
		request := value
		if request.Status != ClarificationPending {
			continue
		}
		var context ConversationContext
		var err error
		if strings.TrimSpace(request.ConversationID) != "" && conversations != nil {
			conversation, getErr := conversations.GetConversation(request.ConversationID)
			if getErr != nil {
				continue
			}
			builder := NewConversationContextBuilder(conversations, resolver)
			context, err = builder.BuildForReply(conversation.ID)
		} else {
			context.CandidateContext, err = resolver.ResolveForEmployerMessage(request.Question, nil)
		}
		if err != nil {
			continue
		}
		candidate, _, candidateErr := resolver.canonicalCandidate()
		if candidateErr != nil {
			continue
		}
		reconciled, reconcileErr := candidateacquisition.Reconcile(candidateacquisition.ReconciliationInput{
			Candidate: candidate, Request: request, Context: &context.CandidateContext,
		})
		if reconcileErr != nil || !reconciled.Changed {
			continue
		}
		request.Status = reconciled.Status
		request.ResolutionReason = reconciled.ResolutionReason
		now := time.Now().UTC()
		request.ResolvedAt = &now
		if err := s.Replace(request); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}
