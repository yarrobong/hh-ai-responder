package employerreply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hh-ai-responder/internal/conversation"
	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
	candidatecontext "hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/conversationpolicy"
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
	Now                func() time.Time
}

type Service struct {
	completion  llmport.CompletionProvider
	model       string
	attempts    int
	temperature float64
	maxTokens   int
	extraPrompt string
	retryDelay  time.Duration
	now         func() time.Time
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
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{completion: dependencies.Completion, model: options.Model, attempts: attempts, temperature: temperature, maxTokens: maxTokens, extraPrompt: options.ExtraPrompt, retryDelay: options.SemanticRetryDelay, now: now}
}

// Prepare produces a validated employer-reply decision. It never persists,
// approves, or sends the returned draft.
func (s *Service) Prepare(ctx context.Context, input Input) (Decision, error) {
	if s == nil || s.completion == nil {
		return Decision{}, errors.New("employer reply completion provider is not configured")
	}
	if ctx == nil {
		return Decision{}, errors.New("employer reply context is nil")
	}
	value := input.Context
	requirement := value.ReplyRequirement
	if requirement == "" {
		requirement = evaluateRequirement(value)
	}
	if requirement != conversationpolicy.ReplyRequired {
		return noReplyDecision(requirement, value.ReplyGuidance.AlreadyDiscussedTopics, replyReason(requirement)), nil
	}
	latest := latestEmployerMessage(value.RecentMessages)
	if latest == nil || latestCandidateAfter(value) {
		return noReplyDecision(conversationpolicy.NoReplyNeeded, value.ReplyGuidance.AlreadyDiscussedTopics, "Последнее сообщение не требует ответа кандидата."), nil
	}
	if external := conversationpolicy.ExternalInterviewAction(latest.Text, latest.Timestamp, s.now()); external != nil {
		return manualReviewDecision(external.Classification+" / "+external.Type, []string{external.RequiredAction}, value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
	}
	if len(value.ConsistencyWarnings) > 0 {
		warnings := make([]string, 0, len(value.ConsistencyWarnings))
		for _, warning := range value.ConsistencyWarnings {
			warnings = append(warnings, warning.Message)
		}
		return manualReviewDecision("история разговора содержит конфликтующие утверждения", warnings, value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
	}
	if value.ReplyGuidance.Mode == "waiting_employer" {
		return noReplyDecision(conversationpolicy.NoReplyNeeded, value.ReplyGuidance.AlreadyDiscussedTopics, "Кандидат уже ответил на последнее сообщение работодателя."), nil
	}
	if value.CandidateContext.UnsupportedIntent {
		return manualReviewDecision("вопрос работодателя не удалось разложить на поддержанные атомарные факты", nil, value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
	}
	if value.CandidateContext.RequiresCandidateInput() {
		missing := append([]candidatecontext.CandidateMissingInformation{}, value.CandidateContext.MissingInformation...)
		if len(missing) == 0 && strings.TrimSpace(value.CandidateContext.UserConfirmationQuestion) != "" {
			missing = append(missing, candidatecontext.CandidateMissingInformation{Question: value.CandidateContext.UserConfirmationQuestion})
		}
		return missingDecision(missing, "В безопасном контексте недостаточно подтверждённых сведений для ответа.", value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
	}
	decision, err := s.complete(ctx, input)
	if err != nil {
		return Decision{}, err
	}
	decision.ReplyRequirement = requirement
	decision.ConversationTopicsUsed = uniqueStrings(append(decision.ConversationTopicsUsed, value.ReplyGuidance.AlreadyDiscussedTopics...))
	if decision.Action == ActionDraftReply {
		if err := ValidateUsedFacts(decision.UsedFacts, value.CandidateContext); err != nil {
			return manualReviewDecision("черновик содержит неподтверждённые использованные факты", []string{err.Error()}, value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
		}
		if err := ValidateDraft(decision.Draft, value.CandidateContext); err != nil {
			return manualReviewDecision("черновик не прошёл проверку фактов", []string{err.Error()}, value.ReplyGuidance.AlreadyDiscussedTopics, requirement), nil
		}
	}
	return decision, nil
}

func (s *Service) complete(ctx context.Context, input Input) (Decision, error) {
	request := llmvalue.CompletionRequest{Model: s.model, Messages: []llmvalue.Message{{Role: llmvalue.RoleSystem, Content: SystemPrompt(input.Task, s.extraPrompt)}, {Role: llmvalue.RoleUser, Content: MarshalContext(input.Context)}}, MaxTokens: s.maxTokens, Temperature: s.temperature, ResponseFormat: ResponseFormat()}
	var lastErr error
	for attempt := 1; attempt <= s.attempts; attempt++ {
		response, err := s.completion.Complete(ctx, request)
		if err != nil {
			// Provider/transport retry belongs to R10.1. A business-layer
			// semantic attempt must never multiply it or hide its error class.
			if ctx.Err() != nil {
				return Decision{}, ctx.Err()
			}
			return Decision{}, fmt.Errorf("structured AI decision failed: %w", err)
		}
		decision, parseErr := ParseDecision(response.Content)
		if parseErr == nil {
			if decision.Action == ActionDraftReply && len(decision.MissingInformation) > 0 {
				parseErr = errors.New("draft_reply cannot contain unresolved missing information")
			}
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
				return Decision{}, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("structured AI decision failed")
	}
	return Decision{}, fmt.Errorf("structured AI decision failed: %w", lastErr)
}

func evaluateRequirement(value Context) conversationpolicy.ReplyRequirement {
	latest := latestEmployerMessage(value.RecentMessages)
	return conversationpolicy.EvaluateReplyPolicy(conversationpolicy.ReplyPolicyInput{Conversation: value.Conversation, Latest: latest, Intent: value.CandidateContext.MessageIntent}).Requirement
}

func latestCandidateAfter(value Context) bool {
	messages := conversation.DeliveredMessages(value.Conversation.Messages)
	if len(messages) == 0 {
		return false
	}
	return messages[len(messages)-1].Sender == conversation.SenderCandidate
}

func latestEmployerMessage(values []conversation.Message) *conversation.Message {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].Sender == conversation.SenderEmployer {
			value := values[i]
			return &value
		}
	}
	return nil
}

func noReplyDecision(requirement conversationpolicy.ReplyRequirement, topics []string, reason string) Decision {
	if reason == "" {
		reason = replyReason(requirement)
	}
	return Decision{Action: ActionNoReplyNeeded, ReplyRequirement: requirement, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: []MissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: uniqueStrings(topics), Warnings: []string{}}
}

func replyReason(requirement conversationpolicy.ReplyRequirement) string {
	if requirement == conversationpolicy.ReplyOptional {
		return "Последнее сообщение допускает только отдельный courtesy-ответ; обычный reply не требуется."
	}
	return "Последнее сообщение не требует ответа кандидата."
}

func manualReviewDecision(reason string, warnings, topics []string, requirement conversationpolicy.ReplyRequirement) Decision {
	return Decision{Action: ActionManualReview, ReplyRequirement: requirement, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: []MissingInformation{}, ForbiddenClaimsChecked: true, ConversationTopicsUsed: uniqueStrings(topics), Warnings: uniqueStrings(warnings)}
}

func missingDecision(values []candidatecontext.CandidateMissingInformation, reason string, topics []string, requirement conversationpolicy.ReplyRequirement) Decision {
	missing := make([]MissingInformation, 0, len(values))
	for _, value := range values {
		question := strings.TrimSpace(value.Question)
		if question == "" {
			continue
		}
		missing = append(missing, MissingInformation{Topic: clarificationTopic(question), Question: question})
	}
	if len(missing) == 0 {
		missing = append(missing, MissingInformation{Topic: "candidate_context", Question: "Подтверди, пожалуйста, что именно нужно ответить работодателю."})
	}
	return Decision{Action: ActionNeedCandidate, ReplyRequirement: requirement, Reason: reason, Confidence: 1, UsedFacts: []string{}, MissingInformation: missing, ForbiddenClaimsChecked: true, ConversationTopicsUsed: uniqueStrings(topics), Warnings: []string{}}
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

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
