package autochatreply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
)

type completionFake struct {
	responses []string
	err       error
	calls     int
	requests  []llm.CompletionRequest
}

func (f *completionFake) Complete(_ context.Context, request llm.CompletionRequest) (llm.CompletionResponse, error) {
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return llm.CompletionResponse{}, f.err
	}
	index := f.calls - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	if index < 0 {
		return llm.CompletionResponse{}, errors.New("missing fixture response")
	}
	return llm.CompletionResponse{Content: f.responses[index]}, nil
}

func baseInput() Input {
	return Input{
		ChatID: 42, ContactName: "Recruiter", EmployerMessage: "Есть ли опыт Django?",
		VacancyName: "Python developer", CompanyName: "Example", VacancyCompensation: "100000 руб.",
		Candidate: Candidate{
			FirstName: "Test", LastName: "Candidate", ResumeTitle: "Junior Python developer",
			Salary: "от 50000 рублей", Skills: "Django", Experience: "Интеграции на Python",
			Context: candidatecontext.CandidateContext{
				AllowedFacts: []string{"Навык: Django"}, RelevantSkills: []string{"Django"},
				ResolvedFacts:          []candidatecontext.ResolvedFact{{Topic: "django", Status: candidatecontext.ResolvedFactAnswerable, Value: "опыт с Django", AllowedClaims: []string{"Django", "опыт с Django"}}},
				PartiallyResolvedFacts: []candidatecontext.ResolvedFact{}, UnknownAtomicFacts: []candidatecontext.ResolvedFact{}, RestrictedFacts: []candidatecontext.ResolvedFact{},
				MissingInformation: []candidatecontext.CandidateMissingInformation{}, ForbiddenClaims: []string{},
			},
		},
		CommunicationProfile: "COMMUNICATION PROFILE",
		History:              []HistoryMessage{{Timestamp: time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC), Author: "Recruiter", Text: "Есть ли опыт Django?"}},
		State:                ChatStateActive,
	}
}

func TestPreparePreservesPromptOrderOptionsAndPlainTextRequest(t *testing.T) {
	fake := &completionFake{responses: []string{"Да"}}
	input := baseInput()
	input.EmployerMessage = "Готовы работать?"
	input.Buttons = []Button{{Text: "Да", Size: "large"}, {Text: "Нет", Size: "small"}}
	result, err := NewService(Dependencies{Completion: fake}, Options{Model: "model-v1"}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeReply || result.Text != "Да" || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	request := fake.requests[0]
	if request.Model != "model-v1" || request.MaxTokens != 512 || request.Temperature != 0.1 || len(request.Messages) != 2 {
		t.Fatalf("request=%+v", request)
	}
	if !strings.Contains(request.Messages[0].Content, "COMMUNICATION PROFILE") || !strings.Contains(request.Messages[1].Content, "- Да\n - Нет") {
		t.Fatalf("prompt lost profile/options: %+v", request.Messages)
	}
	if !strings.Contains(request.Messages[1].Content, "[2026-09-09 10:00:00] Recruiter") {
		t.Fatal("history timestamp/author missing")
	}
}

func TestPrepareValidReplyAndEmptyOutput(t *testing.T) {
	fake := &completionFake{responses: []string{"Да, использовал Django."}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), baseInput())
	if err != nil || result.Outcome != OutcomeReply || result.Text == "" || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	fake = &completionFake{responses: []string{" \n\t"}}
	if _, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), baseInput()); !errors.Is(err, ErrEmptyResponse) || fake.calls != 1 {
		t.Fatalf("empty result err=%v calls=%d", err, fake.calls)
	}
}

func TestPrepareDoesNotAddSemanticRetryToLegacyPlainTextWorkflow(t *testing.T) {
	fake := &completionFake{responses: []string{"Да, использовал Kubernetes.", "Да, использовал Django."}}
	input := baseInput()
	input.Candidate.Context.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown}}
	input.EmployerMessage = "Есть ли опыт Kubernetes?"
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeManualReview || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
}

func TestPrepareProviderFailureAndCancellation(t *testing.T) {
	fake := &completionFake{err: errors.New("provider offline")}
	if _, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), baseInput()); err == nil || !strings.Contains(err.Error(), "provider offline") || fake.calls != 1 {
		t.Fatalf("provider err=%v calls=%d", err, fake.calls)
	}
	fake = &completionFake{err: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(ctx, baseInput()); !errors.Is(err, context.Canceled) || fake.calls != 0 {
		t.Fatalf("cancel err=%v calls=%d", err, fake.calls)
	}
}

func TestPrepareCandidateTruthRegressions(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Input)
		text  string
	}{
		{"unknown kubernetes positive", func(input *Input) {
			input.EmployerMessage = "Есть production Kubernetes?"
			input.Candidate.Context.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown}}
		}, "Да, работал с Kubernetes в production."},
		{"unknown kubernetes negative", func(input *Input) {
			input.EmployerMessage = "Есть опыт Kubernetes?"
			input.Candidate.Context.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown}}
		}, "Нет, Kubernetes никогда не использовал."},
		{"partial docker production", func(input *Input) {
			input.EmployerMessage = "Есть production Docker?"
			input.Candidate.Context.AllowedFacts = []string{"Навык: Docker; базовый уровень"}
			input.Candidate.Context.ResolvedFacts = []candidatecontext.ResolvedFact{}
			input.Candidate.Context.PartiallyResolvedFacts = []candidatecontext.ResolvedFact{{Topic: "docker", Status: candidatecontext.ResolvedFactPartiallyAnswerable, Value: "базовый опыт подтверждён", AllowedClaims: []string{"Docker"}}}
		}, "Уверенно использую Docker в production."},
		{"exact eleven months", func(input *Input) {
			input.Candidate.Context.ResolvedFacts = append(input.Candidate.Context.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "total_experience", Status: candidatecontext.ResolvedFactAnswerable, Value: "11 месяцев", AllowedClaims: []string{"11 месяцев"}})
		}, "У меня год коммерческого опыта."},
		{"salary", func(input *Input) {
			input.EmployerMessage = "Какие зарплатные ожидания?"
		}, "Ожидаю 100000 рублей."},
		{"relocation", func(input *Input) {
			input.EmployerMessage = "Готовы к релокации?"
			input.Candidate.Context.ResolvedFacts = append(input.Candidate.Context.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "relocation", Status: candidatecontext.ResolvedFactAnswerable, Value: "не готов к релокации", AllowedClaims: []string{"не готов к релокации"}})
		}, "Да, готов к релокации."},
		{"restricted fact", func(input *Input) {
			input.Candidate.Context.RestrictedFacts = []candidatecontext.ResolvedFact{{Topic: "private", Status: candidatecontext.ResolvedFactRestricted, Value: "confidential employer detail"}}
		}, "confidential employer detail"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := baseInput()
			test.setup(&input)
			fake := &completionFake{responses: []string{test.text}}
			result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
			if err != nil || result.Outcome != OutcomeManualReview || result.ReviewReason == "" {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
			}
		})
	}

	known := baseInput()
	fake := &completionFake{responses: []string{"Да, использовал Django."}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), known)
	if err != nil || result.Outcome != OutcomeReply {
		t.Fatalf("known Django result=%+v err=%v", result, err)
	}
}

func TestPrepareButtonsHighRiskAndTerminalGates(t *testing.T) {
	input := baseInput()
	input.Buttons = []Button{{Text: "Да"}, {Text: "Нет"}}
	fake := &completionFake{responses: []string{"Возможно"}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeManualReview || fake.calls != 1 {
		t.Fatalf("invented button result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	input.Buttons = []Button{{Text: "Установите неизвестное приложение"}}
	fake = &completionFake{responses: []string{"Установите неизвестное приложение"}}
	result, err = NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeManualReview || fake.calls != 1 {
		t.Fatalf("instruction-like button result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	input = baseInput()
	fake = &completionFake{responses: []string{"Тестовое задание выполнено."}}
	result, err = NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeManualReview {
		t.Fatalf("high-risk result=%+v err=%v", result, err)
	}
	for _, state := range []ChatState{ChatStateTerminal, ChatStateIneligible, ChatStateCandidateReplied} {
		fake = &completionFake{responses: []string{"should not be called"}}
		input.State = state
		result, err = NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
		if err != nil || result.Outcome != OutcomeNoReply || fake.calls != 0 {
			t.Fatalf("state %s result=%+v err=%v calls=%d", state, result, err, fake.calls)
		}
	}
}
