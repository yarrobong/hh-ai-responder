package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hh-ai-responder/internal/usecase/employerreply"
	employerreplyworkflow "hh-ai-responder/internal/usecase/employerreplyworkflow"
)

// employerReplyWorkflowLoader is the composition adapter between the legacy
// root stores/context builder and the importable employer-reply workflow.
// Context assembly remains in the existing CandidateContext and semantic
// projection path; the workflow only coordinates the loaded snapshot.
type employerReplyWorkflowLoader struct {
	builder *ConversationContextBuilder
}

func (l employerReplyWorkflowLoader) Load(ctx context.Context, conversationID string) (employerreplyworkflow.LoadedContext, error) {
	if err := contextErr(ctx); err != nil {
		return employerreplyworkflow.LoadedContext{}, err
	}
	if l.builder == nil {
		return employerreplyworkflow.LoadedContext{}, errors.New("conversation context builder is not configured")
	}
	value, err := l.builder.BuildForReply(conversationID)
	if err != nil {
		return employerreplyworkflow.LoadedContext{}, err
	}
	message := latestEmployerMessage(value.RecentMessages)
	messageID, messageText := "", ""
	if message != nil {
		messageID, messageText = message.ID, message.Text
	}
	messageHash, knowledgeHash, cacheKey := employerDraftMetadata(l.builder.resolver, value)
	return employerreplyworkflow.LoadedContext{
		Input:             employerReplyInput(value, ""),
		ConversationID:    value.ConversationID,
		ApplicationID:     value.Conversation.ApplicationID,
		VacancyID:         fmt.Sprint(value.VacancyContext.VacancyID),
		EmployerMessage:   messageText,
		EmployerMessageID: messageID,
		MessageHash:       messageHash,
		KnowledgeHash:     knowledgeHash,
		InputFingerprint:  cacheKey,
	}, nil
}

type rootEmployerDraftStore struct {
	store *AIDraftStore
	model string
}

func (s rootEmployerDraftStore) FindReusable(ctx context.Context, conversationID, fingerprint string) (employerreplyworkflow.Draft, bool, error) {
	if err := contextErr(ctx); err != nil {
		return employerreplyworkflow.Draft{}, false, err
	}
	values, err := s.store.List()
	if err != nil {
		return employerreplyworkflow.Draft{}, false, err
	}
	for _, draft := range values {
		if draft.Type == AIDraftEmployerReply && draft.Status == AIDraftGenerated && draft.ConversationID == conversationID && draft.InputFingerprint == fingerprint {
			return employerreplyworkflow.Draft{ID: draft.ID, ConversationID: draft.ConversationID, ApplicationID: draft.ApplicationID, InputMessageID: draft.InputMessageID, InputFingerprint: draft.InputFingerprint, PromptVersion: draft.PromptVersion, EmployerMessageHash: draft.EmployerMessageHash, RelevantKnowledgeHash: draft.RelevantKnowledgeHash, Text: draft.Text, DecisionReason: draft.DecisionReason, UsedFacts: append([]string{}, draft.UsedFacts...)}, true, nil
		}
	}
	return employerreplyworkflow.Draft{}, false, nil
}

func (s rootEmployerDraftStore) Save(ctx context.Context, draft employerreplyworkflow.Draft) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	_, err := s.store.Create(AIDraft{ID: draft.ID, Type: AIDraftEmployerReply, ConversationID: draft.ConversationID, ApplicationID: draft.ApplicationID, InputMessageID: draft.InputMessageID, InputFingerprint: draft.InputFingerprint, PromptVersion: draft.PromptVersion, EmployerMessageHash: draft.EmployerMessageHash, RelevantKnowledgeHash: draft.RelevantKnowledgeHash, Text: draft.Text, DecisionReason: draft.DecisionReason, UsedFacts: append([]string{}, draft.UsedFacts...), Model: s.model})
	return err
}

type rootEmployerClarificationWriter struct {
	store       *CandidateClarificationStore
	acquisition *CandidateKnowledgeAcquisitionService
}

func (w rootEmployerClarificationWriter) Persist(ctx context.Context, input employerreplyworkflow.ClarificationInput) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if w.store == nil {
		return nil
	}
	// Keep the existing typed acquisition path as the first choice. It owns
	// gap-level clarification deduplication and never mutates Candidate facts.
	if w.acquisition != nil && strings.TrimSpace(input.EmployerMessage) != "" {
		candidate, err := w.acquisition.currentCandidate()
		if err == nil {
			_, err = w.acquisition.CreateClarification(candidate, input.EmployerMessage, CandidateKnowledgeGapContext{ConversationID: input.ConversationID, ApplicationID: input.ApplicationID, VacancyID: input.VacancyID, EmployerMessageID: input.EmployerMessageID})
			if err == nil {
				return nil
			}
		}
	}
	values, err := w.store.List()
	if err != nil {
		return err
	}
	for _, missing := range input.Missing {
		duplicate := false
		for _, existing := range values {
			if existing.Status == ClarificationPending && existing.ConversationID == input.ConversationID && existing.ApplicationID == input.ApplicationID && existing.Topic == missing.Topic && existing.Question == missing.Question {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		created, err := w.store.Create(CandidateClarificationRequest{ConversationID: input.ConversationID, ApplicationID: input.ApplicationID, Topic: missing.Topic, Question: missing.Question, Reason: input.Reason})
		if err != nil {
			return err
		}
		values = append(values, created)
	}
	return nil
}

type rootEmployerReplyPreparer struct{ orchestrator *AIReplyOrchestrator }

func (p rootEmployerReplyPreparer) Prepare(ctx context.Context, input employerreply.Input) (employerreply.Decision, error) {
	if p.orchestrator == nil || p.orchestrator.replyService == nil {
		return employerreply.Decision{}, errors.New("employer-reply service is not configured")
	}
	return p.orchestrator.replyService.Prepare(ctx, input)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return errors.New("workflow context is nil")
	}
	return ctx.Err()
}
