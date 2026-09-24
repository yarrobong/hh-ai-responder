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

func TestServiceGenerateWithFallbackReturnsReviewableDeterministicDraft(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "   "}}
	input := baseInput()
	result, err := NewService(Dependencies{Completion: fake}, Options{}).GenerateWithFallback(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DraftStatusReviewRequired || result.Confidence != ConfidenceDeterministicFallback {
		t.Fatalf("status=%q confidence=%q, want review-required deterministic fallback", result.Status, result.Confidence)
	}
	if !strings.Contains(result.Letter, input.Vacancy.Name) || strings.Contains(strings.ToLower(result.Letter), "kubernetes") {
		t.Fatalf("fallback letter=%q", result.Letter)
	}
	if result.FallbackReason == "" || len(result.Evidence) == 0 {
		t.Fatalf("fallback metadata=%+v", result)
	}
}

func TestServiceGenerateWithFallbackHandlesMalformedPresentation(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "```json\n{\"letter\":\"claim\"}\n```"}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).GenerateWithFallback(context.Background(), baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DraftStatusReviewRequired || strings.Contains(result.Letter, "```") {
		t.Fatalf("result=%+v", result)
	}
}

func TestServiceEvidenceAlignsWithSelectedResumeAndRelevantStory(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "Работал с Django и создавал API."}}
	input := baseInput()
	input.Stories = []candidate.CandidateStory{{ID: "bizonvr", Title: "Интеграция CRM", Keywords: []string{"Django"}, Summary: "Нарративный контекст"}}
	result, err := NewService(Dependencies{Completion: fake}, Options{}).Generate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DraftStatusValid || len(result.UsedStoryIDs) != 1 || result.UsedStoryIDs[0] != "bizonvr" {
		t.Fatalf("result=%+v", result)
	}
	if !hasEvidence(result.Evidence, "resume", "resume:"+input.Candidate.ResumeTitle) || !hasEvidence(result.Evidence, "candidate_story", "bizonvr") {
		t.Fatalf("evidence=%+v", result.Evidence)
	}
	for _, item := range result.Evidence {
		if item.Kind == "candidate_story" && item.Claim != "narrative context only" {
			t.Fatalf("story evidence overstates provenance: %+v", item)
		}
	}
}

func TestValidateLetterRejectsContradictoryResumeAndKnowledge(t *testing.T) {
	input := baseInput()
	input.Candidate.SafeKnowledge.Skills = []candidate.CandidateSkillDetailed{{Name: "Kubernetes", Level: candidate.SkillLevelUnknown, Negative: true}}
	input.Candidate.Profile.Skills = []candidate.CandidateSkill{{Name: "Kubernetes", Level: candidate.SkillLevelWorking, ProfileFact: candidate.ProfileFact{Source: candidate.CandidateSourceHHResume, Confirmed: true}}}
	err := ValidateLetter(input.Candidate, "Имею опыт работы с Kubernetes.")
	if err == nil || !strings.Contains(err.Error(), "contradictory") {
		t.Fatalf("err=%v, want contradictory-source rejection", err)
	}
}

func TestServiceGenerateWithFallbackRejectsUnsupportedDurationClaim(t *testing.T) {
	fake := &completionFake{response: llmvalue.CompletionResponse{Content: "Есть 5 лет опыта с Django."}}
	input := baseInput()
	input.Candidate.TotalExperienceMonthsKnown = true
	input.Candidate.TotalExperienceMonths = 24
	_, err := NewService(Dependencies{Completion: fake}, Options{}).GenerateWithFallback(context.Background(), input)
	if err == nil || !errors.Is(err, ErrUnsupportedCandidateFact) {
		t.Fatalf("err=%v, want hard unsupported-fact error", err)
	}
}

func TestServiceFallbackIsDeterministicForSameInput(t *testing.T) {
	input := baseInput()
	first, err := NewService(Dependencies{Completion: &completionFake{response: llmvalue.CompletionResponse{Content: ""}}}, Options{}).GenerateWithFallback(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewService(Dependencies{Completion: &completionFake{response: llmvalue.CompletionResponse{Content: ""}}}, Options{}).GenerateWithFallback(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Letter != second.Letter || first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("fallback is not idempotent: first=%+v second=%+v", first, second)
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

func hasEvidence(values []DraftEvidence, kind, reference string) bool {
	for _, value := range values {
		if value.Kind == kind && value.Reference == reference {
			return true
		}
	}
	return false
}
