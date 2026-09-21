package vacancyanalysis

import (
	"context"
	"errors"
	"fmt"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
)

type Options struct {
	Model              string
	Attempts           int
	MaxTokens          int
	Temperature        float64
	TemperatureSet     bool
	SemanticRetryDelay time.Duration
}

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
		maxTokens = 1024
	}
	temperature := options.Temperature
	temperatureSet := options.TemperatureSet || temperature != 0
	if !temperatureSet {
		temperature = 0.1
		temperatureSet = true
	}
	return &Service{
		completion:  dependencies.Completion,
		model:       options.Model,
		attempts:    attempts,
		maxTokens:   maxTokens,
		temperature: temperature,
		retryDelay:  options.SemanticRetryDelay,
	}
}

// Analyze performs the LLM-assisted assessment. Provider failures are
// returned immediately; only malformed or business-invalid completions use
// the vacancy-analysis semantic retry budget.
func (s *Service) Analyze(ctx context.Context, input Input) (Assessment, error) {
	if s == nil || s.completion == nil {
		return Assessment{}, errors.New("vacancy analysis completion provider is not configured")
	}
	if ctx == nil {
		return Assessment{}, errors.New("vacancy analysis context is nil")
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
		TemperatureSet: true,
		ResponseFormat: JSONResponseFormat(),
	}

	var lastErr error
	for attempt := 1; attempt <= s.attempts; attempt++ {
		response, err := s.completion.Complete(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return Assessment{}, ctx.Err()
			}
			return Assessment{}, fmt.Errorf("vacancy analysis completion failed: %w", err)
		}
		parsed, parseErr := ParseAIResponse(response.Content)
		if parseErr == nil {
			return buildAssessment(input, parsed), nil
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
				return Assessment{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("vacancy analysis returned no valid assessment")
	}
	return Assessment{}, fmt.Errorf("vacancy analysis failed: %w", lastErr)
}

func buildAssessment(input Input, response AIResponse) Assessment {
	recommendation := response.Recommendation
	if recommendation == "" {
		// Old persisted/provider JSON has only apply. Preserve its meaning as an
		// advisory recommendation, never as a terminal decision.
		if response.Apply {
			recommendation = RecommendationApply
		} else {
			recommendation = RecommendationDoNotApply
		}
	}
	assessment := Assessment{
		Score:                 response.Score,
		Apply:                 response.Apply,
		Recommendation:        recommendation,
		RecommendationReasons: append([]string(nil), response.RecommendationReasons...),
		Reasons:               response.Reasons,
		Missing:               response.Missing,
		HardRequirements:      DeriveHardRequirements(input.Candidate, input.Vacancy, input.Description, response.HardRequirements),
		StrongMatch:           response.StrongMatch,
	}
	assessment.Reasons = filterUnsupportedPositiveClaims(input.Candidate, assessment.Reasons, response.HardRequirements)
	assessment.StrongMatch = filterUnsupportedPositiveClaims(input.Candidate, assessment.StrongMatch, response.HardRequirements)
	return assessment
}
