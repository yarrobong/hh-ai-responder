package testanswer

import (
	"context"
	"errors"
	"fmt"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
)

type Service struct {
	completion  llmport.CompletionProvider
	model       string
	attempts    int
	temperature float64
	retryDelay  time.Duration
}

func NewService(dependencies Dependencies, options Options) *Service {
	attempts := options.Attempts
	if attempts < 1 {
		attempts = 1
	}
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.2
	}
	return &Service{completion: dependencies.Completion, model: options.Model, attempts: attempts, temperature: temperature, retryDelay: options.SemanticRetryDelay}
}

func (s *Service) Generate(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.completion == nil {
		return Result{}, ErrProviderNotConfigured
	}
	if ctx == nil {
		return Result{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(input.Tasks) == 0 {
		return Result{}, nil
	}

	systemPrompt, userPrompt, err := BuildPrompt(input)
	if err != nil {
		return Result{}, err
	}
	request := llmvalue.CompletionRequest{
		Model: s.model,
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: systemPrompt},
			{Role: llmvalue.RoleUser, Content: userPrompt},
		},
		MaxTokens:      512 + len(input.Tasks)*64,
		Temperature:    s.temperature,
		ResponseFormat: ResponseFormat(),
	}

	var lastErr error
	for attempt := 1; attempt <= s.attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		response, err := s.completion.Complete(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			// Provider retry belongs to the provider boundary. A provider
			// failure is not a business-invalid answer and is returned as-is.
			return Result{}, fmt.Errorf("structured test answer completion failed: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		parsed, parseErr := ParseResponse(response.Content)
		if parseErr == nil {
			result, validationErr := Validate(input.Tasks, parsed)
			if validationErr == nil {
				return result, nil
			}
			parseErr = validationErr
		}
		lastErr = parseErr
		if attempt == s.attempts {
			break
		}
		if s.retryDelay > 0 {
			timer := time.NewTimer(s.retryDelay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return Result{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		return Result{}, errors.New("test answer generation failed")
	}
	return Result{}, lastErr
}
