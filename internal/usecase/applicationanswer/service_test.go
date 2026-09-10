package applicationanswer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	"hh-ai-responder/internal/usecase/employerreply"
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

func decision(action employerreply.Action, draft string, used ...string) string {
	if used == nil {
		used = []string{}
	}
	encoded, _ := json.Marshal(used)
	return `{"action":"` + string(action) + `","draft":"` + draft + `","reason":"fixture","confidence":0.9,"used_facts":` + string(encoded) + `,"missing_information":[],"forbidden_claims_checked":true,"conversation_topics_used":[],"warnings":[]}`
}

func baseInput() Input {
	return Input{
		Application:        applicationSnapshot(),
		VacancyDescription: "Python Django integrations",
		CandidateContext: candidatecontext.CandidateContext{
			AllowedFacts:           []string{"Навык: Django"},
			RelevantSkills:         []string{"Django"},
			MissingInformation:     []candidatecontext.CandidateMissingInformation{},
			ForbiddenClaims:        []string{},
			ResolvedFacts:          []candidatecontext.ResolvedFact{{Topic: "django", Status: candidatecontext.ResolvedFactAnswerable, Value: "опыт с Django", AllowedClaims: []string{"Django", "опыт с Django"}}},
			PartiallyResolvedFacts: []candidatecontext.ResolvedFact{},
			UnknownAtomicFacts:     []candidatecontext.ResolvedFact{},
			RestrictedFacts:        []candidatecontext.ResolvedFact{},
		},
		Question: "Есть ли у вас опыт Django?",
	}
}

func applicationSnapshot() application.JobApplication {
	return application.JobApplication{ID: "application-1", VacancyID: 42, CompanyName: "Fixture", VacancyTitle: "Python Django developer"}
}

func TestServicePreservesQuestionPromptAndOptions(t *testing.T) {
	fake := &completionFake{responses: []string{decision(employerreply.ActionDraftReply, "Да, использовал Django.", "Django")}}
	service := NewService(Dependencies{Completion: fake}, Options{Model: "model-v1", Attempts: 1, ExtraPrompt: "Пиши кратко."})
	input := baseInput()
	if _, err := service.Prepare(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || len(fake.requests) != 1 {
		t.Fatalf("calls=%d requests=%d", fake.calls, len(fake.requests))
	}
	request := fake.requests[0]
	if request.Model != "model-v1" || request.MaxTokens != 900 || request.Temperature != .2 || request.ResponseFormat == nil || len(request.Messages) != 2 {
		t.Fatalf("request=%+v", request)
	}
	if !strings.Contains(request.Messages[0].Content, "Задача: ответ на вопрос вакансии") || !strings.Contains(request.Messages[0].Content, "Пиши кратко.") {
		t.Fatal("application task or extra prompt missing")
	}
	if !strings.Contains(request.Messages[1].Content, input.Question) || !strings.Contains(request.Messages[1].Content, `"match_result"`) {
		t.Fatalf("question/context missing: %s", request.Messages[1].Content)
	}
}

func TestServiceReturnsDraftAndMissingInformationWithoutPersistence(t *testing.T) {
	fake := &completionFake{responses: []string{decision(employerreply.ActionDraftReply, "Да, использовал Django.", "Django")}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), baseInput())
	if err != nil || result.Outcome != OutcomeDraft || result.Decision.Draft == "" || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	input := baseInput()
	input.CandidateContext.MissingInformation = []candidatecontext.CandidateMissingInformation{{Question: "Подтверди коммерческий опыт Django"}}
	fake = &completionFake{responses: []string{decision(employerreply.ActionDraftReply, "Да.")}}
	result, err = NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
	if err != nil || result.Outcome != OutcomeNeedCandidate || result.Decision.Action != employerreply.ActionNeedCandidate || len(result.Decision.MissingInformation) != 1 || fake.calls != 0 {
		t.Fatalf("missing=%+v err=%v calls=%d", result, err, fake.calls)
	}
}

func TestServiceBusinessRetryAndProviderFailureBoundaries(t *testing.T) {
	fake := &completionFake{responses: []string{"not json", decision(employerreply.ActionDraftReply, "Да, Django.", "Django")}}
	service := NewService(Dependencies{Completion: fake}, Options{Attempts: 2})
	if result, err := service.Prepare(context.Background(), baseInput()); err != nil || result.Outcome != OutcomeDraft || fake.calls != 2 {
		t.Fatalf("retry result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	fake = &completionFake{responses: []string{"not json"}}
	service = NewService(Dependencies{Completion: fake}, Options{Attempts: 2})
	if _, err := service.Prepare(context.Background(), baseInput()); err == nil || fake.calls != 2 {
		t.Fatalf("malformed output err=%v calls=%d", err, fake.calls)
	}
	fake = &completionFake{err: errors.New("provider offline")}
	service = NewService(Dependencies{Completion: fake}, Options{Attempts: 3})
	if _, err := service.Prepare(context.Background(), baseInput()); err == nil || !strings.Contains(err.Error(), "provider offline") || fake.calls != 1 {
		t.Fatalf("provider err=%v calls=%d", err, fake.calls)
	}
}

func TestServiceCancellationStopsSemanticAttempts(t *testing.T) {
	fake := &completionFake{err: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService(Dependencies{Completion: fake}, Options{Attempts: 3}).Prepare(ctx, baseInput())
	if !errors.Is(err, context.Canceled) || fake.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestServiceCandidateSafetyRegressions(t *testing.T) {
	cases := []struct {
		name    string
		input   func() Input
		draft   string
		outcome Outcome
	}{
		{"unknown kubernetes", func() Input {
			value := baseInput()
			value.Question = "Есть опыт Kubernetes?"
			value.CandidateContext.MissingInformation = []candidatecontext.CandidateMissingInformation{{Question: "Подтверди опыт Kubernetes"}}
			value.CandidateContext.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown}}
			return value
		}, "Да, Kubernetes в production.", OutcomeNeedCandidate},
		{"unknown is not false", func() Input {
			value := baseInput()
			value.Question = "Есть опыт Kubernetes?"
			value.CandidateContext.UnknownAtomicFacts = []candidatecontext.ResolvedFact{{Topic: "kubernetes", Status: candidatecontext.ResolvedFactUnknown}}
			return value
		}, "Нет, не использовал Kubernetes.", OutcomeManualReview},
		{"partial docker", func() Input {
			value := baseInput()
			value.Question = "Есть опыт Docker?"
			value.CandidateContext.AllowedFacts = []string{"Навык: Docker; базовый уровень"}
			value.CandidateContext.RelevantSkills = []string{"Docker"}
			value.CandidateContext.ResolvedFacts = []candidatecontext.ResolvedFact{}
			value.CandidateContext.PartiallyResolvedFacts = []candidatecontext.ResolvedFact{{Topic: "docker", Status: candidatecontext.ResolvedFactPartiallyAnswerable, Value: "базовый опыт подтверждён", AllowedClaims: []string{"Docker"}}}
			return value
		}, "Уверенно использую Docker в production.", OutcomeManualReview},
		{"exact 11 months", func() Input {
			value := baseInput()
			value.Question = "Сколько у вас опыта?"
			value.CandidateContext.ResolvedFacts = append(value.CandidateContext.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "total_experience", Status: candidatecontext.ResolvedFactAnswerable, Value: "11 месяцев", AllowedClaims: []string{"11 месяцев"}})
			return value
		}, "У меня 1 год коммерческого опыта.", OutcomeManualReview},
		{"salary", func() Input {
			value := baseInput()
			value.Question = "Какие зарплатные ожидания?"
			value.CandidateContext.ResolvedFacts = append(value.CandidateContext.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "salary", Status: candidatecontext.ResolvedFactAnswerable, Value: "минимум 50000 рублей", AllowedClaims: []string{"минимум 50000 рублей"}})
			return value
		}, "Ожидаю 100000 рублей.", OutcomeManualReview},
		{"relocation", func() Input {
			value := baseInput()
			value.Question = "Готовы к релокации?"
			value.CandidateContext.ResolvedFacts = append(value.CandidateContext.ResolvedFacts, candidatecontext.ResolvedFact{Topic: "relocation", Status: candidatecontext.ResolvedFactAnswerable, Value: "не готов к релокации", AllowedClaims: []string{"не готов к релокации"}})
			return value
		}, "Да, готов к релокации.", OutcomeManualReview},
		{"restricted", func() Input {
			value := baseInput()
			value.CandidateContext.RestrictedFacts = []candidatecontext.ResolvedFact{{Topic: "private", Status: candidatecontext.ResolvedFactRestricted, Value: "confidential employer detail"}}
			return value
		}, "confidential employer detail", OutcomeManualReview},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fake := &completionFake{responses: []string{decision(employerreply.ActionDraftReply, test.draft)}}
			result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), test.input())
			if err != nil || result.Outcome != test.outcome {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
			}
		})
	}
}

func TestServiceStorySafetyRegressions(t *testing.T) {
	story := candidate.CandidateStory{Title: "Интеграции", Keywords: []string{"integrations"}, Situation: "В команде автоматизировал ручную работу", Technologies: []string{"Python"}, Result: "Сократил ручную работу на 80%"}
	known := baseInput()
	known.Stories = []candidate.CandidateStory{story}
	known.CandidateContext.AllowedFacts = []string{"Навык: Django", "В команде автоматизировал ручную работу", "Python", "Сократил ручную работу на 80%"}
	for _, test := range []struct {
		name, draft string
		want        Outcome
	}{
		{"valid known story", "В команде автоматизировал ручную работу на Python и сократил ручную работу на 80%.", OutcomeDraft},
		{"invented story number", "Сократил ручную работу на 80%.", OutcomeManualReview},
		{"invented technology", "Выполнил интеграции на Python.", OutcomeManualReview},
		{"invented result", "Сократил ручную работу на 80%.", OutcomeManualReview},
		{"unsupported detail", "В команде автоматизировал ручную работу в Kubernetes.", OutcomeManualReview},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := known
			switch test.name {
			case "invented technology":
				input.CandidateContext.AllowedFacts = []string{"Навык: Django", "В команде автоматизировал ручную работу", "Сократил ручную работу на 80%"}
			case "invented story number", "invented result":
				input.CandidateContext.AllowedFacts = []string{"Навык: Django", "В команде автоматизировал ручную работу", "Python"}
			case "unsupported detail":
				input.CandidateContext.AllowedFacts = []string{"Навык: Django", "Python", "Сократил ручную работу на 80%"}
			}
			fake := &completionFake{responses: []string{decision(employerreply.ActionDraftReply, test.draft)}}
			result, err := NewService(Dependencies{Completion: fake}, Options{}).Prepare(context.Background(), input)
			if err != nil || result.Outcome != test.want {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
