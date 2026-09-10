package runtime

import (
	"context"
	"encoding/json"
	"errors"

	llmport "hh-ai-responder/internal/ports/llm"
	"hh-ai-responder/internal/usecase/candidateinterpretation"
)

// StructuredCandidateKnowledgeExtractor is a root compatibility adapter. The
// interpretation prompt, schema, parser, and semantic retry live in
// candidateinterpretation; this type only translates the legacy root AI seam.
type StructuredCandidateKnowledgeExtractor struct {
	AI       StructuredAIClient
	Model    string
	Attempts int
}

func (e StructuredCandidateKnowledgeExtractor) InterpretCandidateAnswer(gap CandidateKnowledgeGap, raw string, candidate Candidate) (CandidateKnowledgeInterpretation, error) {
	return e.InterpretCandidateAnswerContext(context.Background(), gap, raw, candidate)
}

func (e StructuredCandidateKnowledgeExtractor) InterpretCandidateAnswerContext(ctx context.Context, gap CandidateKnowledgeGap, raw string, candidate Candidate) (CandidateKnowledgeInterpretation, error) {
	service, err := e.service()
	if err != nil {
		return CandidateKnowledgeInterpretation{}, err
	}
	result, err := service.Interpret(ctx, candidateinterpretation.Input{
		Gap: gap, Answer: raw, KnownContext: candidateInterpretationKnownContext(candidate),
	})
	return result, err
}

func (e StructuredCandidateKnowledgeExtractor) service() (*candidateinterpretation.Service, error) {
	if e.AI == nil {
		return nil, errors.New("knowledge extraction AI is not configured")
	}
	completion, ok := any(e.AI).(llmport.CompletionProvider)
	if !ok {
		completion = legacyCompletionProvider{client: e.AI}
	}
	model := e.Model
	if model == "" {
		if named, ok := e.AI.(aiModelNamer); ok {
			model = named.ModelName()
		}
	}
	attempts := e.Attempts
	if attempts < 1 {
		if client, ok := any(e.AI).(*AIClient); ok && client != nil {
			attempts = client.attempts
		}
	}
	return candidateinterpretation.NewService(candidateinterpretation.Dependencies{Completion: completion}, candidateinterpretation.Options{
		Model: model, Attempts: attempts, MaxTokens: 700, Temperature: 0.1, SemanticRetryDelay: aiRetryDelay,
	}), nil
}

func candidateInterpretationKnownContext(value Candidate) candidateinterpretation.KnownContext {
	experienceIDs := make([]string, 0, len(value.Experience))
	for _, item := range value.Experience {
		experienceIDs = append(experienceIDs, item.ID)
	}
	projectIDs := make([]string, 0, len(value.Projects))
	for _, item := range value.Projects {
		projectIDs = append(projectIDs, item.ID)
	}
	return candidateinterpretation.KnownContext{ExperienceIDs: experienceIDs, ProjectIDs: projectIDs}
}

// The following functions are source-compatible delegates for root tests and
// older callers. They contain no interpretation implementation.
func decodeCandidateKnowledgeInterpretation(raw string, gap CandidateKnowledgeGap, candidate Candidate) (CandidateKnowledgeInterpretation, error) {
	return candidateinterpretation.Parse(raw, candidateinterpretation.Input{Gap: gap, KnownContext: candidateInterpretationKnownContext(candidate)})
}

func validateCandidateKnowledgeInterpretation(value CandidateKnowledgeInterpretation, gap CandidateKnowledgeGap, candidate Candidate) error {
	return candidateinterpretation.Validate(value, candidateinterpretation.Input{Gap: gap, KnownContext: candidateInterpretationKnownContext(candidate)})
}

func candidateKnowledgeExtractionSystemPrompt() string {
	return candidateinterpretation.SystemPrompt()
}

func candidateKnowledgeExtractionSchema() *ChatJSONSchema {
	format := candidateinterpretation.JSONResponseFormat()
	if format == nil || format.JSONSchema == nil {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(format.JSONSchema.Schema, &schema); err != nil {
		return nil
	}
	return &ChatJSONSchema{Name: format.JSONSchema.Name, Schema: schema, Strict: format.JSONSchema.Strict}
}
