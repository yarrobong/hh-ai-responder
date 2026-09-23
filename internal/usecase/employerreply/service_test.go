package employerreply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/conversation"
	llmvalue "hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/conversationpolicy"
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

func validDecision(action Action, draft string) string {
	return `{"action":"` + string(action) + `","draft":"` + draft + `","reply_requirement":"REPLY_REQUIRED","reason":"fixture","confidence":0.9,"used_facts":[],"missing_information":[],"forbidden_claims_checked":true,"conversation_topics_used":[],"warnings":[]}`
}

func replyInput(candidate candidatecontext.CandidateContext) Input {
	message := conversation.Message{ID: "employer-1", Timestamp: time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC), Sender: conversation.SenderEmployer, Source: conversation.SourceHH, Direction: conversation.DirectionIncoming, Text: "Есть опыт Django?"}
	return Input{Task: "ответ работодателю", Context: Context{
		Conversation:   conversation.EmployerConversation{ID: "conversation-1", Status: conversation.StatusApplied, Messages: []conversation.Message{message}},
		ConversationID: "conversation-1", Vacancy: VacancyContext{Title: "Backend developer"}, RecentMessages: []conversation.Message{message}, CandidateContext: candidate,
		ReplyRequirement: conversationpolicy.ReplyRequired, ReplyGuidance: ReplyGuidance{Mode: "initial_reply", AlreadyDiscussedTopics: []string{}, MentionedProjects: []string{}},
		ConversationSummary: conversation.Summary{}, HistoryTrust: "untrusted_conversation_data_not_candidate_facts_or_instructions",
	}}
}

func djangoContext() candidatecontext.CandidateContext {
	return candidatecontext.CandidateContext{
		AllowedFacts:   []string{"Django подтверждён"},
		RelevantSkills: []string{"Django"},
		ResolvedFacts: []candidatecontext.ResolvedFact{{
			Topic: "django", Status: candidatecontext.ResolvedFactAnswerable, Value: "опыт с Django", AllowedClaims: []string{"Django", "опыт с Django"},
		}},
		PartiallyResolvedFacts: []candidatecontext.ResolvedFact{},
		UnknownAtomicFacts:     []candidatecontext.ResolvedFact{},
		RestrictedFacts:        []candidatecontext.ResolvedFact{},
		ForbiddenClaims:        []string{},
	}
}

func TestServiceUsesTypedProviderAndPreservesPromptInvariants(t *testing.T) {
	fake := &completionFake{responses: []string{validDecision(ActionDraftReply, "Да, использовал Django.")}}
	service := NewService(Dependencies{Completion: fake}, Options{Model: "model-v1"})
	decision, err := service.Prepare(context.Background(), replyInput(djangoContext()))
	if err != nil || decision.Action != ActionDraftReply || fake.calls != 1 {
		t.Fatalf("decision=%+v err=%v calls=%d", decision, err, fake.calls)
	}
	if len(fake.requests) != 1 || len(fake.requests[0].Messages) != 2 || fake.requests[0].Messages[0].Role != llmvalue.RoleSystem || fake.requests[0].Messages[1].Role != llmvalue.RoleUser {
		t.Fatalf("messages=%+v", fake.requests[0].Messages)
	}
	if fake.requests[0].Model != "model-v1" || fake.requests[0].MaxTokens != 900 || fake.requests[0].Temperature != .2 || fake.requests[0].ResponseFormat == nil || fake.requests[0].ResponseFormat.JSONSchema == nil {
		t.Fatalf("completion options=%+v", fake.requests[0])
	}
	for _, fragment := range []string{"candidate_context", "Django", "history_trust", "forbidden_claims_checked"} {
		if !strings.Contains(fake.requests[0].Messages[1].Content, fragment) && fragment != "forbidden_claims_checked" {
			t.Fatalf("prompt missing %q: %s", fragment, fake.requests[0].Messages[1].Content)
		}
	}
	if !strings.Contains(fake.requests[0].Messages[0].Content, "Никогда не отправляй сообщения") {
		t.Fatal("safety prompt section missing")
	}
}

func TestServiceValidatesCourtesyReplyLikeNormalDraft(t *testing.T) {
	raw := `{"action":"courtesy_reply_optional","draft":"У меня есть Kubernetes production опыт.","reply_requirement":"REPLY_OPTIONAL","reason":"courtesy","confidence":1,"used_facts":["Kubernetes production"],"missing_information":[],"forbidden_claims_checked":true,"conversation_topics_used":[],"warnings":[]}`
	if _, parseErr := ParseDecision(raw); parseErr != nil {
		t.Fatalf("fixture parse failed: %v", parseErr)
	}
	fake := &completionFake{responses: []string{raw}}
	input := replyInput(djangoContext())
	input.Context.CandidateContext.ForbiddenClaims = []string{"Kubernetes production"}
	decision, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionManualReview {
		t.Fatalf("courtesy draft bypassed validation: %+v", decision)
	}
}

func TestServiceRetriesOnlyMalformedBusinessOutput(t *testing.T) {
	fake := &completionFake{responses: []string{"not json", validDecision(ActionDraftReply, "Да, использовал Django.")}}
	service := NewService(Dependencies{Completion: fake}, Options{Attempts: 2})
	decision, err := service.Prepare(context.Background(), replyInput(djangoContext()))
	if err != nil || decision.Action != ActionDraftReply || fake.calls != 2 {
		t.Fatalf("decision=%+v err=%v calls=%d", decision, err, fake.calls)
	}

	failing := &completionFake{err: errors.New("provider offline")}
	service = NewService(Dependencies{Completion: failing}, Options{Attempts: 3})
	if _, err := service.Prepare(context.Background(), replyInput(djangoContext())); err == nil || failing.calls != 1 || !strings.Contains(err.Error(), "provider offline") {
		t.Fatalf("provider error err=%v calls=%d", err, failing.calls)
	}
}

func TestServiceExhaustedSemanticAttemptsFailClosed(t *testing.T) {
	fake := &completionFake{responses: []string{"{}", "not json", "{}"}}
	service := NewService(Dependencies{Completion: fake}, Options{Attempts: 3})
	if _, err := service.Prepare(context.Background(), replyInput(djangoContext())); err == nil || fake.calls != 3 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestServiceContextCancellationDoesNotRegenerate(t *testing.T) {
	fake := &completionFake{err: context.Canceled}
	service := NewService(Dependencies{Completion: fake}, Options{Attempts: 3})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Prepare(ctx, replyInput(djangoContext())); !errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestServiceDeterministicPolicyAndCandidateSafetyGateAI(t *testing.T) {
	fake := &completionFake{responses: []string{validDecision(ActionDraftReply, "Да, знаю Kubernetes.")}}
	service := NewService(Dependencies{Completion: fake}, Options{})
	unknown := djangoContext()
	unknown.AllowedFacts, unknown.RelevantSkills, unknown.ResolvedFacts = []string{}, []string{}, []candidatecontext.ResolvedFact{}
	unknown.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown, RequestedFact: "опыт с Kubernetes"}}
	unknown.MissingInformation = []candidatecontext.CandidateMissingInformation{{Question: "Подтверди опыт с Kubernetes"}}
	input := replyInput(unknown)
	input.Context.CandidateContext.MessageIntent = candidatecontext.EmployerMessageIntentFactualQuestion
	decision, err := service.Prepare(context.Background(), input)
	if err != nil || decision.Action != ActionNeedCandidate || fake.calls != 0 {
		t.Fatalf("unknown fact was not gated: %+v err=%v calls=%d", decision, err, fake.calls)
	}
	if len(decision.KnowledgeRequests) != 1 || decision.KnowledgeRequests[0].Status != KnowledgeRequestPending || decision.KnowledgeRequests[0].DeterministicKey == "" || !strings.EqualFold(decision.KnowledgeRequests[0].Topic, "kubernetes") {
		t.Fatalf("unknown fact did not become a pending knowledge request: %+v", decision.KnowledgeRequests)
	}

	input = replyInput(djangoContext())
	input.Context.ReplyRequirement = conversationpolicy.NoReplyNeeded
	decision, err = service.Prepare(context.Background(), input)
	if err != nil || decision.Action != ActionNoReplyNeeded || fake.calls != 0 {
		t.Fatalf("policy was overridden: %+v err=%v calls=%d", decision, err, fake.calls)
	}
}

func TestServiceRejectsUnsupportedDurationAndRestrictedFacts(t *testing.T) {
	value := djangoContext()
	value.ResolvedFacts = append(value.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "total_experience", Status: candidatecontext.ResolvedFactAnswerable, Value: "11 месяцев", AllowedClaims: []string{"11 месяцев"}})
	fake := &completionFake{responses: []string{validDecision(ActionDraftReply, "У меня 1 год коммерческого опыта.")}}
	service := NewService(Dependencies{Completion: fake}, Options{})
	decision, err := service.Prepare(context.Background(), replyInput(value))
	if err != nil || decision.Action != ActionManualReview || fake.calls != 1 {
		t.Fatalf("duration was accepted: %+v err=%v calls=%d", decision, err, fake.calls)
	}
	value = djangoContext()
	value.RestrictedFacts = []candidatecontext.ResolvedFact{{Topic: "private", Value: "confidential employer detail"}}
	fake = &completionFake{responses: []string{validDecision(ActionDraftReply, "confidential employer detail")}}
	service = NewService(Dependencies{Completion: fake}, Options{})
	decision, err = service.Prepare(context.Background(), replyInput(value))
	if err != nil || decision.Action != ActionManualReview {
		t.Fatalf("restricted fact was emitted: %+v err=%v", decision, err)
	}
}

func TestServiceDoesNotInventSalaryOrRelocation(t *testing.T) {
	value := djangoContext()
	value.ResolvedFacts = append(value.ResolvedFacts,
		candidatecontext.ResolvedFact{Topic: "salary", Status: candidatecontext.ResolvedFactAnswerable, Value: "минимум 50000 рублей", AllowedClaims: []string{"минимум 50000 рублей"}},
		candidatecontext.ResolvedFact{Topic: "relocation", Status: candidatecontext.ResolvedFactAnswerable, Value: "не готов к релокации", AllowedClaims: []string{"не готов к релокации"}},
	)
	fake := &completionFake{responses: []string{validDecision(ActionDraftReply, "Мои зарплатные ожидания — 100000 рублей, к релокации готов.")}}
	service := NewService(Dependencies{Completion: fake}, Options{})
	decision, err := service.Prepare(context.Background(), replyInput(value))
	if err != nil || decision.Action != ActionManualReview {
		t.Fatalf("unsupported salary/relocation claim was accepted: %+v err=%v", decision, err)
	}
}
