package candidateinterpretation

import (
	"context"
	"errors"
	"fmt"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
)

type Service struct {
	completion  llmCompletion
	model       string
	attempts    int
	maxTokens   int
	temperature float64
	retryDelay  time.Duration
}

type llmCompletion interface {
	Complete(context.Context, llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error)
}

func NewService(dependencies Dependencies, options Options) *Service {
	attempts := options.Attempts
	if attempts < 1 {
		attempts = 1
	}
	maxTokens := options.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 700
	}
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.1
	}
	return &Service{completion: dependencies.Completion, model: options.Model, attempts: attempts, maxTokens: maxTokens, temperature: temperature, retryDelay: options.SemanticRetryDelay}
}

// Interpret performs only untrusted structured interpretation. It never
// creates a mutation intent, changes unknown/clarification lifecycle, or
// writes Candidate state.
func (s *Service) Interpret(ctx context.Context, input Input) (Interpretation, error) {
	if s == nil || s.completion == nil {
		return Interpretation{}, errors.New("candidate interpretation completion provider is not configured")
	}
	if ctx == nil {
		return Interpretation{}, errors.New("candidate interpretation context is nil")
	}
	systemPrompt, userPrompt := BuildPrompt(input)
	request := llmvalue.CompletionRequest{
		Model: s.model,
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: systemPrompt},
			{Role: llmvalue.RoleUser, Content: userPrompt},
		},
		MaxTokens:      s.maxTokens,
		Temperature:    s.temperature,
		ResponseFormat: JSONResponseFormat(),
	}

	var lastErr error
	for attempt := 1; attempt <= s.attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Interpretation{}, err
		}
		response, err := s.completion.Complete(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return Interpretation{}, ctx.Err()
			}
			return Interpretation{}, fmt.Errorf("candidate interpretation completion failed: %w", err)
		}
		value, parseErr := Parse(response.Content, input)
		if parseErr == nil {
			return value, nil
		}
		lastErr = parseErr
		if attempt == s.attempts || ctx.Err() != nil {
			break
		}
		if s.retryDelay > 0 {
			timer := time.NewTimer(s.retryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return Interpretation{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("candidate interpretation returned no valid result")
	}
	return Interpretation{}, fmt.Errorf("candidate interpretation failed: %w", lastErr)
}
