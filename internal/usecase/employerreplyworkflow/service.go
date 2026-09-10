package employerreplyworkflow

import (
	"context"
	"errors"
	"strings"

	"hh-ai-responder/internal/usecase/conversationpolicy"
	"hh-ai-responder/internal/usecase/employerreply"
)

type Service struct {
	conversations  ConversationLoader
	reply          ReplyPreparer
	drafts         DraftStore
	clarifications ClarificationWriter
	promptVersion  string
}

func NewService(dependencies Dependencies, options Options) *Service {
	return &Service{
		conversations:  dependencies.Conversations,
		reply:          dependencies.Reply,
		drafts:         dependencies.Drafts,
		clarifications: dependencies.Clarifications,
		promptVersion:  options.PromptVersion,
	}
}

// Prepare assembles and prepares one local reply proposal. It never approves
// or sends a draft and never calls an HH write capability.
func (s *Service) Prepare(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.conversations == nil || s.reply == nil {
		return Result{}, errors.New("employer reply workflow is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("employer reply workflow context is nil")
	}
	if strings.TrimSpace(input.ConversationID) == "" {
		return Result{}, errors.New("conversation id is required")
	}
	loaded, err := s.conversations.Load(ctx, input.ConversationID)
	if err != nil {
		return Result{}, err
	}
	loaded.Input.Task = input.Task

	if s.drafts != nil {
		if saved, ok, findErr := s.drafts.FindReusable(ctx, loaded.ConversationID, loaded.InputFingerprint); findErr != nil {
			return Result{}, findErr
		} else if ok {
			return Result{Decision: reusedDecision(saved, loaded.Input.Context.ReplyRequirement, loaded.Input.Context.ReplyGuidance.AlreadyDiscussedTopics), Outcome: OutcomeReused, Reused: true}, nil
		}
	}
	decision, err := s.reply.Prepare(ctx, loaded.Input)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if decision.Action == employerreply.ActionNeedCandidate && s.clarifications != nil {
		if err := s.clarifications.Persist(ctx, ClarificationInput{
			ConversationID: loaded.ConversationID, ApplicationID: loaded.ApplicationID,
			VacancyID: loaded.VacancyID, EmployerMessage: loaded.EmployerMessage,
			EmployerMessageID: loaded.EmployerMessageID, Reason: decision.Reason,
			Missing: append([]employerreply.MissingInformation{}, decision.MissingInformation...),
		}); err != nil {
			return Result{}, err
		}
	}
	if decision.Action == employerreply.ActionDraftReply && s.drafts != nil {
		if err := s.drafts.Save(ctx, Draft{
			ConversationID: loaded.ConversationID, ApplicationID: loaded.ApplicationID,
			InputMessageID: loaded.EmployerMessageID, InputFingerprint: loaded.InputFingerprint,
			PromptVersion: s.promptVersion, EmployerMessageHash: loaded.MessageHash,
			RelevantKnowledgeHash: loaded.KnowledgeHash, Text: decision.Draft,
			DecisionReason: decision.Reason, UsedFacts: append([]string{}, decision.UsedFacts...),
		}); err != nil {
			return Result{}, err
		}
	}
	return Result{Decision: decision, Outcome: outcomeFor(decision.Action)}, nil
}

func reusedDecision(draft Draft, requirement conversationpolicy.ReplyRequirement, topics []string) employerreply.Decision {
	return employerreply.Decision{
		Action: employerreply.ActionDraftReply, ReplyRequirement: requirement,
		Draft: draft.Text, Reason: draft.DecisionReason, Confidence: 1,
		UsedFacts: append([]string{}, draft.UsedFacts...), MissingInformation: []employerreply.MissingInformation{},
		ForbiddenClaimsChecked: true, ConversationTopicsUsed: append([]string{}, topics...), Warnings: []string{},
	}
}

func outcomeFor(action employerreply.Action) string {
	switch action {
	case employerreply.ActionDraftReply:
		return OutcomeDraftReady
	case employerreply.ActionNeedCandidate:
		return OutcomeNeedsCandidate
	case employerreply.ActionNoReplyNeeded, employerreply.ActionCourtesyReply:
		return OutcomeNoReply
	case employerreply.ActionManualReview:
		return OutcomeManualReview
	default:
		return OutcomeManualReview
	}
}
