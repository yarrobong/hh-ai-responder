package followupdraft

import (
	"context"
	"errors"
	"fmt"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

type Dependencies struct {
	Completion llmport.CompletionProvider
}

type Options struct {
	Model              string
	Attempts           int
	Temperature        float64
	MaxTokens          int
	ExtraPrompt        string
	SemanticRetryDelay time.Duration
}

type Service struct {
	completion  llmport.CompletionProvider
	model       string
	attempts    int
	temperature float64
	maxTokens   int
	extraPrompt string
	retryDelay  time.Duration
}

func NewService(dependencies Dependencies, options Options) *Service {
	attempts := options.Attempts
	if attempts < 1 {
		attempts = 1
	}
	maxTokens := options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 900
	}
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.2
	}
	return &Service{
		completion:  dependencies.Completion,
		model:       options.Model,
		attempts:    attempts,
		temperature: temperature,
		maxTokens:   maxTokens,
		extraPrompt: options.ExtraPrompt,
		retryDelay:  options.SemanticRetryDelay,
	}
}

// Prepare generates and validates one follow-up proposal. It never persists,
// approves, sends, or rechecks the supplied snapshot.
func (s *Service) Prepare(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.completion == nil {
		return Result{}, errors.New("follow-up draft completion provider is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("follow-up draft context is nil")
	}
	if !input.EligibilityEligible() {
		return Result{Decision: manualReview(input.Eligibility.Reason, input.Eligibility.Warnings, input.Context.ReplyGuidance.AlreadyDiscussedTopics)}, nil
	}

	request := llmvalue.CompletionRequest{
		Model: s.model,
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: SystemPrompt(s.extraPrompt)},
			{Role: llmvalue.RoleUser, Content: MarshalInput(input)},
		},
		MaxTokens:      s.maxTokens,
		Temperature:    s.temperature,
		ResponseFormat: employerreply.ResponseFormat(),
	}
	decision, err := s.complete(ctx, request)
	if err != nil {
		return Result{}, err
	}
	decision.ConversationTopicsUsed = uniqueStrings(append(decision.ConversationTopicsUsed, input.Context.ReplyGuidance.AlreadyDiscussedTopics...))
	if decision.Action == employerreply.ActionDraftReply {
		if err := validateGeneratedDraft(decision, input.Context); err != nil {
			return Result{Decision: manualReview("Follow-up draft requires review", []string{"draft_validation_failed"}, decision.ConversationTopicsUsed)}, nil
		}
	}
	return Result{Decision: decision}, nil
}

func (i Input) EligibilityEligible() bool {
	return i.Eligibility.Status == EligibilityStatusEligible
}

func (s *Service) complete(ctx context.Context, request llmvalue.CompletionRequest) (employerreply.Decision, error) {
	var lastErr error
	for attempt := 1; attempt <= s.attempts; attempt++ {
		response, err := s.completion.Complete(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return employerreply.Decision{}, ctx.Err()
			}
			return employerreply.Decision{}, fmt.Errorf("structured AI decision failed: %w", err)
		}
		if ctx.Err() != nil {
			return employerreply.Decision{}, ctx.Err()
		}
		decision, parseErr := employerreply.ParseDecision(response.Content)
		if parseErr != nil {
			lastErr = parseErr
		} else if decision.Action == employerreply.ActionDraftReply && len(decision.MissingInformation) > 0 {
			// This was a valid shared decision but not a valid follow-up
			// decision. Preserve the old non-retry business error boundary.
			return employerreply.Decision{}, errors.New("draft_reply cannot contain unresolved missing information")
		} else {
			return decision, nil
		}
		if attempt == s.attempts || ctx.Err() != nil {
			break
		}
		if s.retryDelay > 0 {
			timer := time.NewTimer(s.retryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return employerreply.Decision{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("structured AI decision failed")
	}
	return employerreply.Decision{}, fmt.Errorf("structured AI decision failed: %w", lastErr)
}
