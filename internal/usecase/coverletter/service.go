package coverletter

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

// Generate creates one plain-text, fact-validated cover-letter draft. It does
// not retry business validation because the established cover-letter path did
// not retry after generation. Provider transport retry remains R10.1.
func (s *Service) Generate(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.completion == nil {
		return Result{}, errors.New("cover letter completion provider is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("cover letter context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	systemPrompt, userPrompt := BuildPrompt(input)
	response, err := s.completion.Complete(ctx, llmvalue.CompletionRequest{
		Model: s.model,
		Messages: []llmvalue.Message{
			{Role: llmvalue.RoleSystem, Content: systemPrompt},
			{Role: llmvalue.RoleUser, Content: userPrompt},
		},
		MaxTokens: s.maxTokens, Temperature: s.temperature,
	})
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, fmt.Errorf("cover letter completion failed: %w", err)
	}
	if len(response.Content) == 0 || isWhitespaceOnly(response.Content) {
		return Result{}, ErrEmptyLetter
	}
	if err := ValidateLetter(input.Candidate, response.Content); err != nil {
		return Result{}, err
	}
	return Result{Letter: response.Content}, nil
}

// GenerateLetter is a descriptive alias for callers that prefer the domain
// operation name.
func (s *Service) GenerateLetter(ctx context.Context, input Input) (Result, error) {
	return s.Generate(ctx, input)
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
