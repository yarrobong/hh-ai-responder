package candidateinterpretation

import (
	"context"
	"errors"
	"strings"
	"testing"

	llmvalue "hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidateacquisition"
)

type completionFake struct {
	responses []string
	errors    []error
	requests  []llmvalue.CompletionRequest
}

func (f *completionFake) Complete(_ context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	if index < len(f.errors) && f.errors[index] != nil {
		return llmvalue.CompletionResponse{}, f.errors[index]
	}
	if index >= len(f.responses) {
		return llmvalue.CompletionResponse{}, errors.New("missing fixture response")
	}
	return llmvalue.CompletionResponse{Content: f.responses[index]}, nil
}

func interpretationInput() Input {
	return Input{Gap: candidateacquisition.CandidateKnowledgeGap{
		CandidateID: "candidate-1", SubjectType: candidateacquisition.KnowledgeSubjectSkill,
		Subject: "Kubernetes", Field: "usage_context", Question: "Работали ли вы с Kubernetes?",
		Context: "employer question", DeterministicKey: "skill:kubernetes:usage_context",
	}, Answer: "Использовал Kubernetes в pet-проекте", KnownContext: KnownContext{ExperienceIDs: []string{"experience-1"}, ProjectIDs: []string{"project-1"}}}
}

func validInterpretation() string {
	return `{"proposals":[{"type":"skill_usage","skill":"Kubernetes","usage_context":"pet_project","level":"unknown","truth_status":"hypothesis"}]}`
}

func newTestService(fake *completionFake, attempts int) *Service {
	return NewService(Dependencies{Completion: fake}, Options{Model: "test-model", Attempts: attempts, SemanticRetryDelay: 0})
}

func TestServicePreservesPromptAndCompletionOptions(t *testing.T) {
	fake := &completionFake{responses: []string{validInterpretation()}}
	result, err := newTestService(fake, 1).Interpret(context.Background(), interpretationInput())
	if err != nil || len(result.Proposals) != 1 {
		t.Fatalf("interpretation=%+v err=%v", result, err)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("calls=%d, want 1", len(fake.requests))
	}
	request := fake.requests[0]
	if request.Model != "test-model" || request.MaxTokens != 700 || request.Temperature != 0.1 || request.ResponseFormat == nil || request.ResponseFormat.JSONSchema == nil || !request.ResponseFormat.JSONSchema.Strict {
		t.Fatalf("request options=%+v", request)
	}
	if len(request.Messages) != 2 || request.Messages[0].Content != SystemPrompt() || !strings.Contains(request.Messages[1].Content, `"subject":"Kubernetes"`) || !strings.Contains(request.Messages[1].Content, "Использовал Kubernetes") {
		t.Fatalf("prompt=%+v", request.Messages)
	}
}

func TestServiceRetriesOnlyInvalidBusinessOutput(t *testing.T) {
	fake := &completionFake{responses: []string{"not json", validInterpretation()}}
	result, err := newTestService(fake, 2).Interpret(context.Background(), interpretationInput())
	if err != nil || len(result.Proposals) != 1 || len(fake.requests) != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, len(fake.requests))
	}

	fake = &completionFake{responses: []string{"not json", "{}", "[]"}}
	if _, err := newTestService(fake, 3).Interpret(context.Background(), interpretationInput()); err == nil || len(fake.requests) != 3 {
		t.Fatalf("all invalid result err=%v calls=%d", err, len(fake.requests))
	}
}

func TestServiceDoesNotBusinessRetryProviderFailure(t *testing.T) {
	fake := &completionFake{errors: []error{errors.New("provider offline")}}
	_, err := newTestService(fake, 3).Interpret(context.Background(), interpretationInput())
	if err == nil || !strings.Contains(err.Error(), "provider offline") || len(fake.requests) != 1 {
		t.Fatalf("provider result err=%v calls=%d", err, len(fake.requests))
	}
}

func TestServiceRejectsAuthorityClaimsAndCrossFieldOutput(t *testing.T) {
	confirmed := `{"proposals":[{"type":"skill_usage","skill":"Kubernetes","usage_context":"commercial","level":"working","truth_status":"confirmed"}]}`
	fake := &completionFake{responses: []string{confirmed}}
	if _, err := newTestService(fake, 1).Interpret(context.Background(), interpretationInput()); err == nil || err.Error() != "candidate interpretation failed: AI cannot confirm candidate knowledge" {
		t.Fatalf("confirmed output err=%v", err)
	}

	otherField := `{"proposals":[{"type":"salary","skill":"Kubernetes","usage_context":"unknown","level":"unknown","truth_status":"hypothesis"}]}`
	fake = &completionFake{responses: []string{otherField}}
	if _, err := newTestService(fake, 1).Interpret(context.Background(), interpretationInput()); err == nil {
		t.Fatal("cross-field output was accepted")
	}
}

func TestServicePreservesExplicitNegativeProposalAsUntrustedValue(t *testing.T) {
	negative := `{"proposals":[{"type":"skill_usage","skill":"Kubernetes","usage_context":"explicitly_not_used","level":"unknown","truth_status":"hypothesis"}]}`
	fake := &completionFake{responses: []string{negative}}
	result, err := newTestService(fake, 1).Interpret(context.Background(), interpretationInput())
	if err != nil || len(result.Proposals) != 1 || result.Proposals[0].UsageContext != "explicitly_not_used" {
		t.Fatalf("negative result=%+v err=%v", result, err)
	}
}

func TestServiceCancellationStopsSemanticAttempts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &completionFake{responses: []string{validInterpretation()}}
	if _, err := newTestService(fake, 3).Interpret(ctx, interpretationInput()); !errors.Is(err, context.Canceled) || len(fake.requests) != 0 {
		t.Fatalf("cancelled result err=%v calls=%d", err, len(fake.requests))
	}
}

func TestParseRejectsTrailingDataAndUnknownStoryReferences(t *testing.T) {
	input := interpretationInput()
	if _, err := Parse(validInterpretation()+" {}", input); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("trailing data err=%v", err)
	}
	story := `{"proposals":[{"type":"story","skill":"","usage_context":"unknown","level":"unknown","truth_status":"hypothesis","story":{"title":"Conflict","situation":"s","task":"t","action":"a","result":"r","referenced_experience":"missing","referenced_project":""}}]}`
	if _, err := Parse(story, input); err == nil || !strings.Contains(err.Error(), "unknown experience") {
		t.Fatalf("unknown story reference err=%v", err)
	}
}
