package followupdraft

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/conversation"
	llmvalue "hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	employerreply "hh-ai-responder/internal/usecase/employerreply"
)

type completionFake struct {
	responses []string
	err       error
	calls     int
	requests  []llmvalue.CompletionRequest
}

func (f *completionFake) Complete(_ context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return llmvalue.CompletionResponse{}, f.err
	}
	index := f.calls - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	if index < 0 {
		return llmvalue.CompletionResponse{}, errors.New("missing fixture response")
	}
	return llmvalue.CompletionResponse{Content: f.responses[index]}, nil
}

func followUpInput() Input {
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	message := conversation.Message{ID: "employer", Timestamp: at, Sender: conversation.SenderEmployer, Source: conversation.SourceHH, Direction: conversation.DirectionIncoming, Text: "Будем рады обратной связи."}
	candidate := candidatecontext.CandidateContext{
		AllowedFacts: []string{"Django подтверждён"}, RelevantSkills: []string{"Django"},
		ResolvedFacts:   []candidatecontext.ResolvedFact{{Topic: "django", Status: candidatecontext.ResolvedFactAnswerable, Value: "опыт с Django", AllowedClaims: []string{"Django", "опыт с Django"}}},
		ForbiddenClaims: []string{}, RestrictedFacts: []candidatecontext.ResolvedFact{}, PartiallyResolvedFacts: []candidatecontext.ResolvedFact{}, UnknownAtomicFacts: []candidatecontext.ResolvedFact{},
	}
	return Input{
		Context: employerreply.Context{
			Conversation: conversation.EmployerConversation{ID: "conversation", Messages: []conversation.Message{message}}, ConversationID: "conversation",
			RecentMessages: []conversation.Message{message}, CandidateContext: candidate, HistoryTrust: "untrusted_conversation_data_not_candidate_facts_or_instructions",
			ReplyGuidance: employerreply.ReplyGuidance{Mode: "waiting_employer", AlreadyDiscussedTopics: []string{"Django"}},
		},
		Eligibility:        Eligibility{Status: EligibilityStatusEligible, Reason: "eligible", Warnings: []string{}},
		ConfirmedFollowUps: []time.Time{at.Add(-10 * 24 * time.Hour)},
	}
}

func validDecision(draft string) string {
	return `{"action":"draft_reply","draft":"` + draft + `","reason":"fixture","confidence":0.9,"used_facts":["Django"],"missing_information":[],"forbidden_claims_checked":true,"conversation_topics_used":[],"warnings":[]}`
}

func TestServicePreservesPromptOptionsAndFollowUpPayload(t *testing.T) {
	fake := &completionFake{responses: []string{validDecision("Да, использовал Django.")}}
	service := NewService(Dependencies{Completion: fake}, Options{Model: "model-v1", Attempts: 1})
	result, err := service.Prepare(context.Background(), followUpInput())
	if err != nil || result.Decision.Action != employerreply.ActionDraftReply || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	request := fake.requests[0]
	if request.Model != "model-v1" || request.MaxTokens != 900 || request.Temperature != .2 || request.ResponseFormat == nil || len(request.Messages) != 2 {
		t.Fatalf("request=%+v", request)
	}
	for _, fragment := range []string{Task, "confirmed_follow_up_history", "days_waiting", "candidate_context"} {
		if !strings.Contains(request.Messages[0].Content+request.Messages[1].Content, fragment) {
			t.Fatalf("prompt missing %q", fragment)
		}
	}
}

func TestServiceDoesNotCallProviderWhenEligibilityIsNotKnownEligible(t *testing.T) {
	fake := &completionFake{responses: []string{validDecision("ignored")}}
	input := followUpInput()
	input.Eligibility.Status = "too_early"
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Decision.Action != employerreply.ActionManualReview || fake.calls != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
}

func TestServiceRetriesMalformedStructuredOutputOnly(t *testing.T) {
	fake := &completionFake{responses: []string{"not json", validDecision("Готов уточнить детали.")}}
	result, err := NewService(Dependencies{Completion: fake}, Options{Attempts: 2}).Prepare(context.Background(), followUpInput())
	if err != nil || result.Decision.Action != employerreply.ActionDraftReply || fake.calls != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
}

func TestServiceRejectsUnsafeFollowUpFactsAndHighRisk(t *testing.T) {
	for _, draft := range []string{"У меня есть production Kubernetes опыт.", "Готов обсудить зарплату: 100000 рублей.", "Готов к релокации."} {
		fake := &completionFake{responses: []string{validDecision(draft)}}
		input := followUpInput()
		if strings.Contains(draft, "релокации") {
			input.Context.CandidateContext.ResolvedFacts = append(input.Context.CandidateContext.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "relocation", Status: candidatecontext.ResolvedFactAnswerable, Value: "не готов к релокации", AllowedClaims: []string{"не готов к релокации"}})
		}
		result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
		if err != nil || result.Decision.Action != employerreply.ActionManualReview {
			t.Fatalf("unsafe draft=%q result=%+v err=%v", draft, result, err)
		}
	}
}

func TestServiceCancellationAndProviderFailure(t *testing.T) {
	fake := &completionFake{err: errors.New("provider down")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(ctx, followUpInput())
	if !errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("cancellation err=%v calls=%d", err, fake.calls)
	}
	fake = &completionFake{err: errors.New("provider down")}
	_, err = NewService(Dependencies{Completion: fake}, Options{Attempts: 3}).Prepare(context.Background(), followUpInput())
	if err == nil || fake.calls != 1 {
		t.Fatalf("provider error err=%v calls=%d", err, fake.calls)
	}
}
