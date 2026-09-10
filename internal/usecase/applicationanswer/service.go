package applicationanswer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	llmport "hh-ai-responder/internal/ports/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/employerreply"
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
	return &Service{completion: dependencies.Completion, model: options.Model, attempts: attempts, temperature: temperature, maxTokens: maxTokens, extraPrompt: options.ExtraPrompt, retryDelay: options.SemanticRetryDelay}
}

// Prepare generates a validated proposed answer. It never persists,
// approves, submits, or sends the result.
func (s *Service) Prepare(ctx context.Context, input Input) (Result, error) {
	if s == nil || s.completion == nil {
		return Result{}, errors.New("application answer completion provider is not configured")
	}
	if ctx == nil {
		return Result{}, errors.New("application answer context is nil")
	}
	if input.Question == "" || isBlank(input.Question) {
		return Result{}, errors.New("application question is required")
	}
	if len(input.CandidateContext.MissingInformation) > 0 {
		return resultForMissing(input), nil
	}

	decision, err := s.complete(ctx, input)
	if err != nil {
		return Result{}, err
	}
	if decision.Action == employerreply.ActionDraftReply {
		if err := employerreply.ValidateUsedFacts(decision.UsedFacts, input.CandidateContext); err != nil {
			return manualReviewResult("черновик содержит неподтверждённые использованные факты", err), nil
		}
		if err := employerreply.ValidateDraft(decision.Draft, input.CandidateContext); err != nil {
			return manualReviewResult("черновик не прошёл проверку фактов", err), nil
		}
		if err := validateApplicationDraft(decision.Draft, input.CandidateContext); err != nil {
			return manualReviewResult("черновик не прошёл application-answer проверку фактов", err), nil
		}
		if err := ValidateStoryClaims(decision.Draft, input); err != nil {
			return manualReviewResult("черновик использует неподтверждённое содержание story", err), nil
		}
	}
	return Result{Decision: decision, Outcome: outcomeFor(decision)}, nil
}

// Generate is a descriptive alias for callers that prefer generation
// terminology.
func (s *Service) Generate(ctx context.Context, input Input) (Result, error) {
	return s.Prepare(ctx, input)
}

func (s *Service) complete(ctx context.Context, input Input) (employerreply.Decision, error) {
	request := completionRequest(input, s.extraPrompt, s.model, s.maxTokens, s.temperature)
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
		if parseErr == nil && decision.Action == employerreply.ActionDraftReply && len(decision.MissingInformation) > 0 {
			parseErr = errors.New("draft_reply cannot contain unresolved missing information")
		}
		if parseErr == nil {
			return decision, nil
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
				return employerreply.Decision{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("structured AI decision failed")
	}
	return employerreply.Decision{}, fmt.Errorf("structured AI decision failed: %w", lastErr)
}

func resultForMissing(input Input) Result {
	missing := make([]employerreply.MissingInformation, 0, len(input.CandidateContext.MissingInformation))
	for _, value := range input.CandidateContext.MissingInformation {
		if question := value.Question; !isBlank(question) {
			missing = append(missing, employerreply.MissingInformation{Topic: clarificationTopic(question), Question: question})
		}
	}
	return Result{Decision: employerreply.Decision{Action: employerreply.ActionNeedCandidate, Reason: "В безопасном контексте недостаточно подтверждённых сведений для подготовки ответа.", Confidence: 1, UsedFacts: []string{}, MissingInformation: missing, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: []string{}}, Outcome: OutcomeNeedCandidate}
}

func manualReviewResult(reason string, validationErr error) Result {
	warning := ""
	if validationErr != nil {
		warning = validationErr.Error()
	}
	warnings := []string{}
	if warning != "" {
		warnings = append(warnings, warning)
	}
	return Result{Decision: employerreply.Decision{Action: employerreply.ActionManualReview, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: []employerreply.MissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: []string{}, Warnings: warnings}, Outcome: OutcomeManualReview}
}

func outcomeFor(decision employerreply.Decision) Outcome {
	switch decision.Action {
	case employerreply.ActionDraftReply:
		return OutcomeDraft
	case employerreply.ActionNeedCandidate:
		return OutcomeNeedCandidate
	case employerreply.ActionManualReview:
		return OutcomeManualReview
	case employerreply.ActionCourtesyReply:
		return OutcomeCourtesyReply
	default:
		return OutcomeNoReply
	}
}

func isBlank(value string) bool {
	for _, r := range value {
		switch r {
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return true
}

func clarificationTopic(question string) string {
	for _, name := range candidatecontext.TechnologyNames() {
		if candidatecontext.Mentions(question, name) {
			return name
		}
	}
	words := strings.Fields(question)
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}
