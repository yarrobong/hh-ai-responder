package coverletter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"hh-ai-responder/internal/candidate"
	llmvalue "hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/usecase/candidatecontext"
	vacancyanalysis "hh-ai-responder/internal/usecase/vacancyanalysis"
	"hh-ai-responder/internal/vacancy"
)

type completionFake struct {
	response llmvalue.CompletionResponse
	err      error
	calls    int
	request  llmvalue.CompletionRequest
}

func (f *completionFake) Complete(ctx context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	f.calls++
	f.request = request
	if f.err != nil {
		return llmvalue.CompletionResponse{}, f.err
	}
	return f.response, nil
}

func baseInput() Input {
	return Input{
		Candidate: CandidateFacts{
			FullName: "Иван Иванов", ResumeTitle: "Backend developer", Skills: "Python, Django",
			Experience: "Разрабатывал API и интеграции.", Location: "Екатеринбург",
			SafeContext: candidatecontext.CandidateContext{AllowedFacts: []string{"Django подтверждён"}, RelevantSkills: []string{"Django"}},
		},
		Vacancy:     vacancy.Vacancy{Name: "Backend developer", Company: vacancy.Company{Name: "Example"}},
		Description: "Разработка интеграций на Python и Django",
	}
}

func TestServiceUsesPlainCompletionAndPreservesPromptOptions(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "Готов откликнуться на вакансию."}}
	input := baseInput()
	input.Assessment = &vacancyanalysis.Assessment{StrongMatch: []string{"Django"}, Missing: []string{"Kubernetes"}, Reasons: []string{"релевантный стек"}}
	input.SemanticHints = []SemanticHint{{EntityType: "project", Title: "Интеграция", Text: "пример данных, не факт"}}
	service := NewService(Dependencies{Completion: fake}, Options{Model: "model-v1", MaxTokens: 512, Temperature: 0.5})
	result, err := service.Generate(context.Background(), input)
	if err != nil || result.Letter != fake.response.Content || fake.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, fake.calls)
	}
	if len(fake.request.Messages) != 2 || fake.request.Messages[0].Role != llmvalue.RoleSystem || fake.request.Messages[1].Role != llmvalue.RoleUser {
		t.Fatalf("messages=%+v", fake.request.Messages)
	}
	if fake.request.Model != "model-v1" || fake.request.MaxTokens != 512 || fake.request.Temperature != 0.5 || fake.request.ResponseFormat != nil {
		t.Fatalf("request options=%+v", fake.request)
	}
	for _, fragment := range []string{"Example", "Backend developer", "Django", "Kubernetes", "RELEVANT REAL EXAMPLES"} {
		if !strings.Contains(fake.request.Messages[0].Content+fake.request.Messages[1].Content, fragment) {
			t.Fatalf("prompt missing %q", fragment)
		}
	}
}

func TestServiceRejectsEmptyAndProviderErrors(t *testing.T) {
	for _, response := range []string{"", " \n\t"} {
		fake := &completionFake{response: llmvalue.CompletionResponse{Content: response}}
		_, err := NewService(Dependencies{Completion: fake}, Options{}).Generate(context.Background(), baseInput())
		if !errors.Is(err, ErrEmptyLetter) || fake.calls != 1 {
			t.Fatalf("empty response=%q err=%v calls=%d", response, err, fake.calls)
		}
	}

	fake := &completionFake{err: errors.New("provider offline")}
	_, err := NewService(Dependencies{Completion: fake}, Options{}).Generate(context.Background(), baseInput())
	if err == nil || !strings.Contains(err.Error(), "provider offline") || fake.calls != 1 || errors.Is(err, ErrEmptyLetter) {
		t.Fatalf("provider error=%v calls=%d", err, fake.calls)
	}
}

func TestServiceStopsBeforeProviderOnCancellation(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "letter"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewService(Dependencies{Completion: fake}, Options{}).Generate(ctx, baseInput())
	if !errors.Is(err, context.Canceled) || fake.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestValidateLetterSafetyRegressions(t *testing.T) {
	input := baseInput()
	tests := []struct {
		name    string
		value   CandidateFacts
		letter  string
		wantErr string
	}{
		{name: "unknown kubernetes", value: input.Candidate, letter: "Имею production-опыт с Kubernetes", wantErr: "unsupported candidate fact"},
		{name: "partial docker production overclaim", value: withDockerBasic(input.Candidate), letter: "Уверенно использую Docker в production", wantErr: "production/commercial Docker"},
		{name: "known django remains usable", value: input.Candidate, letter: "Работал с Django и создавал API", wantErr: ""},
		{name: "eleven months cannot become one year", value: withMonths(input.Candidate, 11), letter: "У меня год коммерческого опыта", wantErr: "rounds structured experience"},
		{name: "unsupported seniority", value: input.Candidate, letter: "Я Senior разработчик", wantErr: "unsupported seniority"},
		{name: "relocation contradiction", value: withPreferences(input.Candidate, "не готов к релокации", ""), letter: "Готов к релокации", wantErr: "relocation contradicts"},
		{name: "semantic hint is not evidence", value: input.Candidate, letter: "Выполнял задачи с Kubernetes", wantErr: "unsupported candidate fact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateLetter(test.value, test.letter)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err=%v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func withDockerBasic(value CandidateFacts) CandidateFacts {
	value.SafeKnowledge.Skills = []candidate.CandidateSkillDetailed{{Name: "Docker", Level: candidate.SkillLevelBasic}}
	return value
}

func withMonths(value CandidateFacts, months int) CandidateFacts {
	value.TotalExperienceMonthsKnown = true
	value.TotalExperienceMonths = months
	return value
}

func withPreferences(value CandidateFacts, relocation, trips string) CandidateFacts {
	value.SafeKnowledge.Profile.Relocation = relocation
	value.SafeKnowledge.Profile.BusinessTrips = trips
	return value
}
