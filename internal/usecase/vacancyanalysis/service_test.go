package vacancyanalysis

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	llmvalue "hh-ai-responder/internal/llm"
	"hh-ai-responder/internal/vacancy"
)

type fakeCompletion struct {
	responses []string
	err       error
	calls     int
	requests  []llmvalue.CompletionRequest
}

func (f *fakeCompletion) Complete(_ context.Context, request llmvalue.CompletionRequest) (llmvalue.CompletionResponse, error) {
	f.calls++
	f.requests = append(f.requests, request)
	if f.err != nil {
		return llmvalue.CompletionResponse{}, f.err
	}
	index := f.calls - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	return llmvalue.CompletionResponse{Content: f.responses[index]}, nil
}

func validResponse(score int, hardRequirements, strongMatch string) string {
	return `{"score":` + itoa(score) + `,"apply":true,"reasons":["` + strongMatch + `"],"missing":[],"hard_requirements":` + hardRequirements + `,"strong_match":["` + strongMatch + `"]}`
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	if value < 0 {
		return "-" + itoa(-value)
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}

func newTestService(provider *fakeCompletion, attempts int) *Service {
	return NewService(Dependencies{Completion: provider}, Options{Model: "test-model", Attempts: attempts, SemanticRetryDelay: 0})
}

func TestServiceRetriesOnlyBusinessInvalidOutput(t *testing.T) {
	tests := []struct {
		name      string
		responses []string
		wantCalls int
		wantErr   bool
	}{
		{name: "valid first", responses: []string{validResponse(82, "[]", "Python")}, wantCalls: 1},
		{name: "malformed then valid", responses: []string{"not json", validResponse(82, "[]", "Python")}, wantCalls: 2},
		{name: "schema invalid then valid", responses: []string{`{"score":101,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`, validResponse(82, "[]", "Python")}, wantCalls: 2},
		{name: "all invalid", responses: []string{"not json", "still not json"}, wantCalls: 2, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeCompletion{responses: test.responses}
			assessment, err := newTestService(provider, 2).Analyze(context.Background(), Input{})
			if test.wantErr == (err == nil) {
				t.Fatalf("Analyze() error=%v, wantErr=%v", err, test.wantErr)
			}
			if provider.calls != test.wantCalls {
				t.Fatalf("provider calls=%d, want %d", provider.calls, test.wantCalls)
			}
			if !test.wantErr && assessment.Score != 82 {
				t.Fatalf("score=%d, want 82", assessment.Score)
			}
		})
	}
}

func TestServiceProviderErrorIsNotCandidateMismatch(t *testing.T) {
	provider := &fakeCompletion{err: errors.New("provider unavailable")}
	_, err := newTestService(provider, 3).Analyze(context.Background(), Input{})
	if err == nil || !strings.Contains(err.Error(), "completion failed") {
		t.Fatalf("error=%v, want provider error", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d, want 1", provider.calls)
	}
}

func TestServicePreservesTransportOptionsAndPromptFacts(t *testing.T) {
	provider := &fakeCompletion{responses: []string{validResponse(80, "[]", "Django")}}
	input := Input{Candidate: CandidateFacts{Skills: "Django", TotalExperienceMonthsKnown: true, TotalExperienceMonths: 11}, Vacancy: vacancy.Vacancy{Name: "Backend", Description: "Python"}, Description: "Python", Salary: "100000 RUR", Location: "Екатеринбург", WorkSchedule: "Удалённо"}
	_, err := newTestService(provider, 1).Analyze(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	request := provider.requests[0]
	if request.MaxTokens != 1024 || request.Temperature != 0.1 || request.Model != "test-model" {
		t.Fatalf("request options=%+v", request)
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema == nil || !request.ResponseFormat.JSONSchema.Strict {
		t.Fatalf("response format=%+v", request.ResponseFormat)
	}
	userPrompt := request.Messages[1].Content
	for _, fragment := range []string{"11 months", "Backend", "Python", "Удалённо"} {
		if !strings.Contains(userPrompt, fragment) {
			t.Fatalf("prompt missing %q: %s", fragment, userPrompt)
		}
	}
}

func TestServiceDoesNotPromoteUnknownSkillOrHallucinatedRequirement(t *testing.T) {
	provider := &fakeCompletion{responses: []string{validResponse(99, `[{"requirement":"Kubernetes","category":"skill","vacancy_evidence":"Kubernetes обязателен"}]`, "Kubernetes")}}
	assessment, err := newTestService(provider, 1).Analyze(context.Background(), Input{Candidate: CandidateFacts{Skills: "Docker"}, Vacancy: vacancy.Vacancy{Name: "Backend"}, Description: "Kubernetes обязателен"})
	if err != nil {
		t.Fatal(err)
	}
	if len(assessment.HardRequirements) != 1 || assessment.HardRequirements[0].Status != HardRequirementStatusUnknown {
		t.Fatalf("hard requirements=%+v, want one unknown requirement", assessment.HardRequirements)
	}
	if len(assessment.StrongMatch) != 0 || len(assessment.Reasons) != 0 {
		t.Fatalf("unsupported positive claims accepted: strong=%v reasons=%v", assessment.StrongMatch, assessment.Reasons)
	}

	provider = &fakeCompletion{responses: []string{validResponse(99, `[{"requirement":"Kafka","category":"skill","vacancy_evidence":"Kafka обязателен"}]`, "Kafka")}}
	assessment, err = newTestService(provider, 1).Analyze(context.Background(), Input{Vacancy: vacancy.Vacancy{Name: "Backend"}, Description: "Python обязателен"})
	if err != nil {
		t.Fatal(err)
	}
	if len(assessment.HardRequirements) != 0 || len(assessment.StrongMatch) != 0 {
		t.Fatalf("unsupported vacancy requirement accepted: %+v", assessment)
	}
}

func TestServiceKeepsDjangoAndElevenMonthSoftExperience(t *testing.T) {
	provider := &fakeCompletion{responses: []string{validResponse(86, `[{"requirement":"Django","category":"skill","vacancy_evidence":"Django обязателен"},{"requirement":"от 1 года","category":"experience_years","vacancy_evidence":"от 1 года"}]`, "Django")}}
	assessment, err := newTestService(provider, 1).Analyze(context.Background(), Input{Candidate: CandidateFacts{Skills: "Django", TotalExperienceMonthsKnown: true, TotalExperienceMonths: 11}, Vacancy: vacancy.Vacancy{Name: "Django developer"}, Description: "Django обязателен. Опыт от 1 года."})
	if err != nil {
		t.Fatal(err)
	}
	if assessment.HardRequirements[0].Status != HardRequirementStatusMet {
		t.Fatalf("Django status=%s, want met", assessment.HardRequirements[0].Status)
	}
	if assessment.HardRequirements[1].Status != HardRequirementStatusUnknown || !assessment.HardRequirements[1].Soft {
		t.Fatalf("experience requirement=%+v, want soft unknown", assessment.HardRequirements[1])
	}
}

func TestServiceHonorsCancellationDuringSemanticRetry(t *testing.T) {
	provider := &fakeCompletion{responses: []string{"not json", validResponse(80, "[]", "Python")}}
	ctx, cancel := context.WithCancel(context.Background())
	service := NewService(Dependencies{Completion: provider}, Options{Attempts: 2, SemanticRetryDelay: time.Second})
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, err := service.Analyze(ctx, Input{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want cancellation", err)
	}
}
