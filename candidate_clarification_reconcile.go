package main

import (
	"errors"
	"strings"
	"time"
)

// ReconcileCandidateClarifications closes stale or invalid candidate-data
// requests without deleting their history. Existing records remain auditable;
// no new question is created and no fact is asserted by this operation.
func (s *CandidateClarificationStore) ReconcileCandidateClarifications(conversations *ConversationStore, resolver *CandidateContextResolver) (int, error) {
	if s == nil {
		return 0, errors.New("clarification store is nil")
	}
	if resolver == nil {
		return 0, errors.New("clarification reconciliation requires a resolver")
	}
	changed := 0
	for i := range s.clarifications {
		request := &s.clarifications[i]
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
		if context.CandidateContext.MessageIntent != EmployerMessageIntentFactualQuestion && context.CandidateContext.MessageIntent != EmployerMessageIntentCompound {
			request.Status = ClarificationResolvedExistingKnowledge
			request.ResolutionReason = "invalid clarification type: employer message is not a candidate factual question"
		} else if !context.CandidateContext.RequiresCandidateInput() {
			request.Status = ClarificationResolvedExistingKnowledge
			request.ResolutionReason = "resolved from existing employer-safe candidate knowledge"
		} else {
			continue
		}
		now := time.Now().UTC()
		request.ResolvedAt = &now
		changed++
	}
	return changed, nil
}
