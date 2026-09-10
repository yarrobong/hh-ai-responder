package autochatreply

import (
	"context"
	"errors"
	"fmt"

	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
)

type Dependencies struct {
	Completion llmport.CompletionProvider
}

type Options struct {
	Model       string
	MaxTokens   int
	Temperature float64
}

type Service struct {
	completion  llmport.CompletionProvider
	model       string
	maxTokens   int
	temperature float64
}

func NewService(dependencies Dependencies, options Options) *Service {
	maxTokens := options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.5
	}
	return &Service{completion: dependencies.Completion, model: options.Model, maxTokens: maxTokens, temperature: temperature}
}

var ErrEmptyResponse = errors.New("auto-chat completion is empty")

// Prepare creates a proposed plain-text response. It never sends, leaves,
// persists, approves, preflights, or reconciles an HH action.
func (s *Service) Prepare(ctx context.Context, input Input) (ProposedResult, error) {
	if s == nil || s.completion == nil {
		return ProposedResult{}, errors.New("auto-chat completion provider is not configured")
	}
	if ctx == nil {
		return ProposedResult{}, errors.New("auto-chat context is nil")
	}
	if err := ctx.Err(); err != nil {
		return ProposedResult{}, err
	}
	switch input.State {
	case ChatStateTerminal, ChatStateIneligible, ChatStateCandidateReplied:
		return ProposedResult{Outcome: OutcomeNoReply}, nil
	case ChatStateDiscarded:
		return ProposedResult{Outcome: OutcomeLeaveRecommended}, nil
	}
	systemPrompt, userPrompt := BuildPrompt(input)
	temperature := s.temperature
	if len(input.Buttons) > 0 {
		temperature = 0.1
	}
	response, err := s.completion.Complete(ctx, llmvalue.CompletionRequest{
		Model: s.model,
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: systemPrompt},
			{Role: llmvalue.RoleUser, Content: userPrompt},
		},
		MaxTokens: s.maxTokens, Temperature: temperature,
	})
	if err != nil {
		if ctx.Err() != nil {
			return ProposedResult{}, ctx.Err()
		}
		return ProposedResult{}, fmt.Errorf("auto-chat completion failed: %w", err)
	}
	if ctx.Err() != nil {
		return ProposedResult{}, ctx.Err()
	}
	if response.Content == "" || isWhitespaceOnly(response.Content) {
		return ProposedResult{}, ErrEmptyResponse
	}
	if reason := Review(input, response.Content); reason != "" {
		return ProposedResult{Outcome: OutcomeManualReview, Text: response.Content, ReviewReason: reason}, nil
	}
	return ProposedResult{Outcome: OutcomeReply, Text: response.Content}, nil
}

// Generate is a descriptive alias for callers that use generation
// terminology; it retains the proposal-only contract.
func (s *Service) Generate(ctx context.Context, input Input) (ProposedResult, error) {
	return s.Prepare(ctx, input)
}

func isWhitespaceOnly(value string) bool {
	for _, r := range value {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return true
}
