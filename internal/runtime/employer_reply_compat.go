package runtime

import (
	"context"
	"encoding/json"
	"errors"

	llmvalue "hh-ai-responder/internal/llm"
	llmport "hh-ai-responder/internal/ports/llm"
	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

func employerReplyInput(value ConversationContext, task string) employerreply.Input {
	knowledge, _ := json.Marshal(value.RelevantKnowledge)
	examples := make([]employerreply.SemanticHint, 0, len(value.RelevantExamples))
	for _, example := range value.RelevantExamples {
		examples = append(examples, employerreply.SemanticHint{EntityType: example.EntityType, EntityID: example.EntityID, Score: example.Score, Title: example.Title, Text: example.Text, ContentHash: example.ContentHash, EmbeddingModel: example.EmbeddingModel, EvidenceRefs: append([]CandidateSemanticEvidenceRef{}, example.EvidenceRefs...)})
	}
	clarifications := make([]employerreply.Clarification, 0, len(value.UnresolvedQuestions))
	for _, item := range value.UnresolvedQuestions {
		clarifications = append(clarifications, employerreply.Clarification{Question: item.Question, Status: item.Status, MessageID: item.MessageID, Source: item.Source})
	}
	return employerreply.Input{Context: employerreply.Context{
		Conversation: value.Conversation, ConversationID: value.ConversationID, Status: value.Status, NextAction: value.NextAction,
		Vacancy:        employerreply.VacancyContext{VacancyID: value.VacancyContext.VacancyID, CompanyName: value.VacancyContext.CompanyName, Title: value.VacancyContext.Title, Description: value.VacancyContext.Description},
		RecentMessages: value.RecentMessages, ConversationSummary: value.ConversationSummary, CandidateContext: value.CandidateContext,
		UnresolvedQuestions: clarifications, ForbiddenClaims: append([]string{}, value.ForbiddenClaims...), ConsistencyWarnings: value.ConsistencyWarnings,
		ReplyGuidance:    employerreply.ReplyGuidance{Mode: value.ReplyGuidance.Mode, AvoidReintroduction: value.ReplyGuidance.AvoidReintroduction, AlreadyDiscussedTopics: append([]string{}, value.ReplyGuidance.AlreadyDiscussedTopics...), MentionedProjects: append([]string{}, value.ReplyGuidance.MentionedProjects...)},
		ReplyRequirement: value.ReplyRequirement, HistoryTrust: value.HistoryTrust, RelevantExamples: examples, RelevantKnowledge: knowledge,
	}, Task: task}
}

func employerReplySchema() *ChatJSONSchema {
	format := employerreply.ResponseFormat()
	if format == nil || format.JSONSchema == nil {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(format.JSONSchema.Schema, &schema); err != nil {
		return nil
	}
	return &ChatJSONSchema{Name: format.JSONSchema.Name, Schema: schema, Strict: format.JSONSchema.Strict}
}

// Root aliases preserve the existing dashboard, draft-store and JSON API
// contract while the employer-reply decision value is owned by its use case.
type AIResponseAction = employerreply.Action
type AIMissingInformation = employerreply.MissingInformation
type AIResponseDecision = employerreply.Decision

const (
	AIActionDraftReply    = employerreply.ActionDraftReply
	AIActionNeedCandidate = employerreply.ActionNeedCandidate
	AIActionNoReplyNeeded = employerreply.ActionNoReplyNeeded
	AIActionCourtesyReply = employerreply.ActionCourtesyReply
	AIActionManualReview  = employerreply.ActionManualReview

	AIResponseActionDraftReply    = employerreply.ActionDraftReply
	AIResponseActionNeedCandidate = employerreply.ActionNeedCandidate
	AIResponseActionNoReplyNeeded = employerreply.ActionNoReplyNeeded
	AIResponseActionCourtesyReply = employerreply.ActionCourtesyReply
	AIResponseActionManualReview  = employerreply.ActionManualReview
	AIActionCourtesyReplyOptional = employerreply.ActionCourtesyReply
)

// legacyCompletionProvider is only a source-compatibility bridge for tests
// and older callers that still provide StructuredAIClient. The importable
// employerreply package never sees this interface.
type legacyCompletionProvider struct{ client StructuredAIClient }

// Complete exposes the already-constructed R10.1 provider to the typed
// employer-reply use case without reintroducing HTTP or provider knowledge
// there. It performs exactly one provider operation; semantic regeneration is
// owned by employerreply.Service.
func (c *AIClient) Complete(ctx context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	if c == nil || c.provider == nil {
		return llmvalue.CompletionResponse{}, errors.New("AI completion provider is not configured")
	}
	if ctx == nil {
		ctx = c.ctx
	}
	if ctx == nil {
		return llmvalue.CompletionResponse{}, errors.New("completion context is nil")
	}
	return c.provider.Complete(ctx, request)
}

func (p legacyCompletionProvider) Complete(ctx context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	if p.client == nil {
		return llmvalue.CompletionResponse{}, errors.New("legacy completion client is not configured")
	}
	if ctx == nil {
		return llmvalue.CompletionResponse{}, errors.New("completion context is nil")
	}
	if len(request.Messages) < 2 {
		return llmvalue.CompletionResponse{}, errors.New("legacy completion request requires system and user messages")
	}
	var schema *ChatJSONSchema
	if request.ResponseFormat != nil && request.ResponseFormat.JSONSchema != nil {
		var fields map[string]any
		if err := json.Unmarshal(request.ResponseFormat.JSONSchema.Schema, &fields); err != nil {
			return llmvalue.CompletionResponse{}, err
		}
		schema = &ChatJSONSchema{Name: request.ResponseFormat.JSONSchema.Name, Schema: fields, Strict: request.ResponseFormat.JSONSchema.Strict}
	}
	content, err := p.client.ChatStructuredWithSchema(request.Messages[0].Content, request.Messages[1].Content, request.MaxTokens, request.Temperature, schema, func(string) error { return nil })
	if err != nil {
		return llmvalue.CompletionResponse{}, err
	}
	return llmvalue.CompletionResponse{Content: content, Model: request.Model}, nil
}

var _ llmport.CompletionProvider = legacyCompletionProvider{}
var _ llmport.CompletionProvider = (*AIClient)(nil)
