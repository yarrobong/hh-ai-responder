package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCandidateContextKeepsSalaryAndExperienceMapping(t *testing.T) {
	r := &HHAIResponder{
		firstName:        "Имя",
		lastName:         "Фамилия",
		resumeExperience: "реальный опыт",
		contacts:         "contacts",
	}
	resume := ResumeItem{
		Title:  "Python developer",
		Salary: "100000 руб",
		Skills: "Python, Django",
		Area:   "Екатеринбург",
	}

	candidate := r.candidateContext(resume)
	if candidate.Salary != resume.Salary {
		t.Fatalf("salary mismatch: got %q, want %q", candidate.Salary, resume.Salary)
	}
	if candidate.Experience != r.resumeExperience {
		t.Fatalf("experience mismatch: got %q, want %q", candidate.Experience, r.resumeExperience)
	}
	if candidate.Location != resume.Area {
		t.Fatalf("location mismatch: got %q, want %q", candidate.Location, resume.Area)
	}

	prompt := buildLetterSystemPrompt(candidate, "")
	if !strings.Contains(prompt, "Зарплата: 100000 руб") {
		t.Errorf("letter prompt does not contain salary: %s", prompt)
	}
	if !strings.Contains(prompt, "реальный опыт") {
		t.Errorf("letter prompt does not contain experience: %s", prompt)
	}
}

func TestVacancyDecisionUsesTriStateHardRequirements(t *testing.T) {
	tests := []struct {
		name       string
		evaluation VacancyEvaluation
		want       VacancyDecision
	}{
		{
			name:       "known contradiction rejects",
			evaluation: VacancyEvaluation{Score: 99, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "English C1", Category: hardRequirementCategoryLanguage, Status: hardRequirementStatusMissing, VacancyEvidence: "English C1 required", CandidateEvidence: "Candidate explicitly has no English C1"}}},
			want:       VacancyReject,
		},
		{
			name:       "unknown requires review",
			evaluation: VacancyEvaluation{Score: 99, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "education", Category: hardRequirementCategoryEducation, Status: hardRequirementStatusUnknown, VacancyEvidence: "Higher education required", CandidateEvidence: "not provided"}}},
			want:       VacancyReviewRequired,
		},
		{
			name:       "unknown takes precedence over apply false",
			evaluation: VacancyEvaluation{Score: 99, Apply: false, HardRequirements: []HardRequirementEvaluation{{Requirement: "location", Category: hardRequirementCategoryLocation, Status: hardRequirementStatusUnknown, VacancyEvidence: "Office location required", CandidateEvidence: "not provided"}}},
			want:       VacancyReviewRequired,
		},
		{
			name:       "apply false rejects",
			evaluation: VacancyEvaluation{Score: 99, Apply: false},
			want:       VacancyReject,
		},
		{
			name:       "score threshold rejects",
			evaluation: VacancyEvaluation{Score: 64, Apply: true},
			want:       VacancyReject,
		},
		{
			name:       "confirmed match applies",
			evaluation: VacancyEvaluation{Score: 65, Apply: true},
			want:       VacancyMatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vacancyDecision(test.evaluation, 65); got != test.want {
				t.Fatalf("decision: got %s, want %s", got, test.want)
			}
			if got := finalApplyDecision(test.evaluation, 65); got != (test.want == VacancyMatch) {
				t.Fatalf("final apply decision: got %t for %s", got, test.want)
			}
		})
	}
}

func TestNoExperienceRequirementDoesNotConflictWithCandidateExperience(t *testing.T) {
	systemPrompt, userPrompt := buildVacancyEvaluationPrompt(vacancyEvaluationInput{
		Candidate:   CandidateContext{Experience: "5 лет опыта в backend-разработке"},
		Vacancy:     Vacancy{Name: "Python developer"},
		Description: "Опыт не требуется. Обучение на месте.",
	})
	if !strings.Contains(systemPrompt, "«без опыта»") || !strings.Contains(systemPrompt, "наличие опыта кандидата не является hard mismatch") {
		t.Fatalf("prompt does not protect no-experience semantics: %s", systemPrompt)
	}
	if !strings.Contains(userPrompt, "5 лет опыта") {
		t.Fatalf("candidate experience was not included in prompt: %s", userPrompt)
	}
	if got := vacancyDecision(VacancyEvaluation{Score: 80, Apply: true}, 65); got != VacancyMatch {
		t.Fatalf("candidate with experience was rejected for a no-experience vacancy: %s", got)
	}
}

func TestVacancyEvaluationRequiresUnknownRequirementsArray(t *testing.T) {
	if _, err := parseVacancyEvaluationJSON(`{"score":82,"apply":true,"reasons":[],"missing":[]}`); err == nil {
		t.Fatal("evaluation without hard_requirements was accepted")
	}
}

func TestVacancyResponseCountKnownness(t *testing.T) {
	var known Vacancy
	if err := json.Unmarshal([]byte(`{"vacancyId":1,"totalResponsesCount":0}`), &known); err != nil {
		t.Fatal(err)
	}
	if !known.TotalResponsesCountKnown || known.TotalResponsesCount != 0 {
		t.Fatalf("explicit zero response count was not preserved: %+v", known)
	}

	var unknown Vacancy
	if err := json.Unmarshal([]byte(`{"vacancyId":2}`), &unknown); err != nil {
		t.Fatal(err)
	}
	if unknown.TotalResponsesCountKnown {
		t.Fatalf("absent response count was treated as known: %+v", unknown)
	}
	var nullCount Vacancy
	if err := json.Unmarshal([]byte(`{"vacancyId":3,"totalResponsesCount":null}`), &nullCount); err != nil {
		t.Fatal(err)
	}
	if nullCount.TotalResponsesCountKnown {
		t.Fatalf("null response count was treated as known: %+v", nullCount)
	}

	preview, err := json.Marshal(ApplyResult{Type: "application_preview"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), "responses_count") {
		t.Fatalf("unknown response count was fabricated in preview: %s", preview)
	}
	count := 2
	preview, err = json.Marshal(ApplyResult{Type: "application_preview", ResponsesCount: &count})
	if err != nil || !strings.Contains(string(preview), `"responses_count":2`) {
		t.Fatalf("known response count was not serialized: %s (err=%v)", preview, err)
	}
}

func TestPromptsRequireTruthfulAnswersAndOptionalGitHub(t *testing.T) {
	letter := buildLetterSystemPrompt(CandidateContext{FullName: "Имя"}, "")
	if strings.Contains(letter, "Утверждай") || strings.Contains(letter, "всеми необходимыми навыками") {
		t.Fatalf("letter prompt still encourages fabricated skills: %s", letter)
	}

	testPromptWithoutGitHub := buildTestSystemPrompt("", "", "")
	if strings.Contains(testPromptWithoutGitHub, "github.com") {
		t.Fatalf("test prompt contains an unconfigured GitHub URL: %s", testPromptWithoutGitHub)
	}
	if strings.Contains(testPromptWithoutGitHub, "как будто знаком") {
		t.Fatalf("test prompt still encourages fabricated experience: %s", testPromptWithoutGitHub)
	}

	testPromptWithGitHub := buildTestSystemPrompt("", "https://github.com/example", "")
	if !strings.Contains(testPromptWithGitHub, "https://github.com/example") {
		t.Fatalf("configured GitHub URL was not included: %s", testPromptWithGitHub)
	}
}

func TestSolveTestsUsesTextSolutionContract(t *testing.T) {
	prompt := buildTestSystemPrompt("", "", "")
	if !strings.Contains(prompt, "text_solution") {
		t.Fatal("test prompt does not require text_solution")
	}
	if strings.Contains(prompt, "text_answer") {
		t.Fatal("test prompt still uses the old text_answer contract")
	}

	var parsed TestSolutionsResponse
	if err := json.Unmarshal([]byte(`{"solutions":[{"task_id":1,"text_solution":"ответ"}]}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if got := parsed.Solutions[0].TextSolution; got != "ответ" {
		t.Fatalf("text_solution was not decoded: %q", got)
	}
}

func TestVacancyEvaluationJSONValidation(t *testing.T) {
	valid, err := parseVacancyEvaluationJSON(`{"score":82,"apply":true,"reasons":["Python"],"missing":["FastAPI"],"hard_requirements":[],"strong_match":["REST API"]}`)
	if err != nil {
		t.Fatalf("valid evaluation rejected: %v", err)
	}
	if valid.Score != 82 || !valid.Apply || valid.StrongMatch[0] != "REST API" {
		t.Fatalf("evaluation decoded incorrectly: %+v", valid)
	}

	for _, raw := range []string{
		`{"score":-1,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`,
		`{"score":101,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`,
		`{"score":82,"apply":true,"reasons":[],"missing":[],"hard_requirements":[] trailing}`,
		`{"score":82,"apply":true,"reasons":null,"missing":[],"hard_requirements":[]}`,
		`{"score":82,"apply":true,"reasons":["Python"],"missing":[{"skill":"FastAPI","reason":"not confirmed"}],"hard_requirements":[]}`,
		`{"score":82,"apply":true,"reasons":[],"missing":[],"hard_requirements":[],"unexpected":true}`,
		`{"score":82,"apply":true,"reasons":[],"missing":[]}`,
	} {
		if _, err := parseVacancyEvaluationJSON(raw); err == nil {
			t.Errorf("invalid evaluation was accepted: %s", raw)
		}
	}
}

func TestFinalApplyDecisionUsesAIFlagAndThreshold(t *testing.T) {
	if !finalApplyDecision(VacancyEvaluation{Score: 65, Apply: true}, 65) {
		t.Fatal("score at threshold should be accepted")
	}
	if finalApplyDecision(VacancyEvaluation{Score: 64, Apply: true}, 65) {
		t.Fatal("below-threshold score should be rejected")
	}
	if finalApplyDecision(VacancyEvaluation{Score: 99, Apply: false}, 65) {
		t.Fatal("AI apply=false must reject even a high score")
	}
	if finalApplyDecision(VacancyEvaluation{Score: 95, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "minimum 3 years commercial Python experience", Category: hardRequirementCategoryExperienceYears, Status: hardRequirementStatusMissing, VacancyEvidence: "3 years required", CandidateEvidence: "Explicitly no such experience"}}}, 65) {
		t.Fatal("known hard requirement mismatch must reject regardless of score and AI apply flag")
	}
	if !finalApplyDecision(VacancyEvaluation{Score: 70, Apply: true, Missing: []string{"FastAPI"}, HardRequirements: []HardRequirementEvaluation{}}, 65) {
		t.Fatal("score above threshold with no hard requirement mismatch should match")
	}
	if got := vacancyEvaluationRejectReason(VacancyEvaluation{Score: 65, Apply: false}, 65); got != "AI recommended not applying (65/100)" {
		t.Fatalf("unexpected apply=false diagnostic: %q", got)
	}
	if got := vacancyEvaluationRejectReason(VacancyEvaluation{Score: 60, Apply: true}, 65); got != "AI score below threshold (60/100, minimum 65)" {
		t.Fatalf("unexpected threshold diagnostic: %q", got)
	}
	if got := vacancyEvaluationRejectReason(VacancyEvaluation{Score: 72, Apply: true, HardRequirements: []HardRequirementEvaluation{{Requirement: "minimum 3 years commercial Python experience", Category: hardRequirementCategoryOther, Status: hardRequirementStatusMissing, VacancyEvidence: "3 years required", CandidateEvidence: "Explicitly no such experience"}}}, 65); got != "hard requirements not met: minimum 3 years commercial Python experience" {
		t.Fatalf("unexpected hard requirement diagnostic: %q", got)
	}
}

func TestVacancyEvaluationPromptContainsCandidateAndVacancyFacts(t *testing.T) {
	systemPrompt, userPrompt := buildVacancyEvaluationPrompt(vacancyEvaluationInput{
		Candidate: CandidateContext{
			FullName:    "Имя Кандидата",
			ResumeTitle: "Python developer",
			Salary:      "100000 руб",
			Skills:      "Python, Django",
			Experience:  "Реальный опыт с API",
			Location:    "Екатеринбург",
		},
		Vacancy: Vacancy{
			Name:           "Backend developer",
			WorkExperience: "Опыт 1–3 года",
			Company:        Company{Name: "Example"},
		},
		Description:     "Разработка REST API",
		Salary:          "120000-150000 руб",
		Location:        "Екатеринбург",
		WorkSchedule:    "удаленная работа",
		IncludeKeywords: []string{"Django", "REST API"},
	})
	for _, expected := range []string{"Имя Кандидата", "Python developer", "100000 руб", "Python, Django", "Реальный опыт с API", "Локация кандидата: Екатеринбург", "Образование кандидата: не передано", "Структурированная длительность опыта кандидата: не передана; не вычисляй её по датам", "Backend developer", "Example", "Опыт 1–3 года", "Требуемый опыт из карточки HH: Опыт 1–3 года", "Разработка REST API", "120000-150000 руб", "удаленная работа", "Django, REST API"} {
		if !strings.Contains(userPrompt, expected) {
			t.Errorf("evaluation prompt does not contain %q: %s", expected, userPrompt)
		}
	}
	for _, expected := range []string{"только валидный JSON", "Не выдумывай", "основной стек подходит", "Название должности НЕ подтверждает образование", "hard_requirements", "candidate_evidence", "vacancy_evidence"} {
		if !strings.Contains(systemPrompt, expected) {
			t.Errorf("evaluation system prompt does not contain %q: %s", expected, systemPrompt)
		}
	}
}

func TestVacancyKeywordFilters(t *testing.T) {
	if got := parseKeywordList(" Python, , Django ,python "); len(got) != 3 || got[1] != "Django" {
		t.Fatalf("keyword list parsed incorrectly: %#v", got)
	}
	vacancy := Vacancy{
		ID:   123,
		Name: "Backend developer",
		Company: Company{
			Name: "Example",
		},
	}
	if reason := deterministicVacancyRejectReason(vacancy, "Работа с PHP и legacy", 0, []string{" php "}); reason == "" {
		t.Fatal("exclude keyword was not matched case-insensitively")
	}
	if reason := deterministicVacancyRejectReason(vacancy, "Работа с Python", 0, []string{" PHP "}); reason != "" {
		t.Fatalf("unmatched exclude keyword rejected vacancy: %s", reason)
	}
	low := 90000
	vacancy.Compensation = Compensation{To: &low, Currency: "RUR"}
	if reason := deterministicVacancyRejectReason(vacancy, "Python", 100000, nil); reason == "" {
		t.Fatal("salary below minimum was not rejected")
	}
	fromOnly := 90000
	vacancy.Compensation = Compensation{From: &fromOnly}
	if reason := deterministicVacancyRejectReason(vacancy, "Python", 100000, nil); reason != "" {
		t.Fatalf("from-only salary was rejected without a known ceiling: %s", reason)
	}
}

func TestHighRiskChatContextBlocksAutomaticReply(t *testing.T) {
	reason := chatReplyReviewReasonForContext(
		"auto",
		false,
		"Подходит?",
		"Готовы выйти 10 сентября на зарплату 80000?",
		nil,
		"Да, подходит",
	)
	if reason == "" || !strings.Contains(reason, "chat history") {
		t.Fatalf("history-only high-risk context was not routed to review: %q", reason)
	}
	if got := chatReplyReviewReasonForContext("auto", false, "Подходит?", "", []string{"Установите неизвестное приложение"}, "Да, подходит"); got == "" {
		t.Fatal("reply option with a software installation request was not routed to review")
	}
	if got := chatReplyReviewReasonForContext("auto", false, "Подходит?", "", nil, "Да, вот https://example.test"); got == "" {
		t.Fatal("high-risk generated reply was not routed to review")
	}
}

func TestValidateTestSolutionsAgainstTasks(t *testing.T) {
	tasks := []Task{
		{ID: 1, Description: "Выберите вариант", CandidateSolutions: []Solution{{ID: "10", Text: "Да"}, {ID: "20", Text: "Нет"}}},
		{ID: 2, Description: "Открытый вопрос"},
	}

	var valid TestSolutionsResponse
	if err := json.Unmarshal([]byte(`{"solutions":[{"task_id":1,"solution_id":10},{"task_id":2,"text_solution":"Краткий ответ"}]}`), &valid); err != nil {
		t.Fatal(err)
	}
	answers, err := validateTestSolutions(tasks, valid)
	if err != nil || len(answers) != 2 || !answers[1].HasChoice || answers[2].TextSolution != "Краткий ответ" {
		t.Fatalf("valid test answers rejected: answers=%+v err=%v", answers, err)
	}

	invalidResponses := []string{
		`{"solutions":[{"task_id":1,"text_solution":"Да"},{"task_id":2,"text_solution":"Ответ"}]}`,
		`{"solutions":[{"task_id":1,"solution_id":999},{"task_id":2,"text_solution":"Ответ"}]}`,
		`{"solutions":[{"task_id":1,"solution_id":10},{"task_id":1,"solution_id":20}]}`,
		`{"solutions":[{"task_id":1,"solution_id":10},{"task_id":99,"text_solution":"Ответ"}]}`,
		`{"solutions":[{"task_id":1,"solution_id":10},{"task_id":2,"solution_id":20}]}`,
		`{"solutions":[{"task_id":1,"solution_id":10,"text_solution":"конфликт"},{"task_id":2,"text_solution":"Ответ"}]}`,
		`{"solutions":[{"task_id":1,"solution_id":10},{"task_id":2,"text_solution":"  "}]}`,
	}
	for _, raw := range invalidResponses {
		var parsed TestSolutionsResponse
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			// A malformed/strictly rejected AI response is also fail-closed.
			continue
		}
		if _, err := validateTestSolutions(tasks, parsed); err == nil {
			t.Errorf("invalid test answer was accepted: %s", raw)
		}
	}
}

func TestSalaryFilterIsCurrencyAware(t *testing.T) {
	amount := 70000
	vacancy := Vacancy{Compensation: Compensation{To: &amount, Currency: "RUR"}}
	if reason := deterministicVacancyRejectReasonWithCurrency(vacancy, "Python", 80000, "RUR", nil); reason == "" {
		t.Fatal("matching-currency salary below minimum was not rejected")
	}
	vacancy.Compensation.Currency = "USD"
	if reason := deterministicVacancyRejectReasonWithCurrency(vacancy, "Python", 80000, "RUR", nil); reason != "" {
		t.Fatalf("incompatible currency was hard-rejected: %s", reason)
	}
	vacancy.Compensation.Currency = ""
	if reason := deterministicVacancyRejectReasonWithCurrency(vacancy, "Python", 80000, "RUR", nil); reason != "" {
		t.Fatalf("missing currency was hard-rejected: %s", reason)
	}
	if got, err := normalizeSalaryCurrency(""); err != nil || got != "RUR" {
		t.Fatalf("default salary currency: got=%q err=%v", got, err)
	}
	if got, err := parseNonNegativeInt("0", "TEST_LIMIT", 20); err != nil || got != 0 {
		t.Fatalf("zero limit was not accepted: got=%d err=%v", got, err)
	}
	if _, err := parseNonNegativeInt("-1", "TEST_LIMIT", 20); err == nil {
		t.Fatal("negative limit was accepted")
	}
}

func TestRunOnceWithDisabledTasksReturns(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	r := &HHAIResponder{
		ctx:           context.Background(),
		runOnce:       true,
		autoApply:     false,
		autoTouch:     false,
		autoJobStatus: false,
		chatMode:      "off",
	}
	done := make(chan struct{})
	go func() {
		r.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run-once did not finish")
	}
}

func TestMinMatchScoreValidation(t *testing.T) {
	if score, err := parseMinMatchScore(""); err != nil || score != 65 {
		t.Fatalf("default score incorrect: %d %v", score, err)
	}
	for _, value := range []string{"-1", "101", "not-a-score"} {
		if _, err := parseMinMatchScore(value); err == nil {
			t.Errorf("invalid score accepted: %s", value)
		}
	}
}

func TestVacancyEvaluationAIErrorAndMalformedJSONFailClosed(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	for _, response := range []struct {
		status int
		body   string
	}{
		{http.StatusInternalServerError, "error"},
		{http.StatusOK, `{"choices":[{"message":{"role":"assistant","content":"not json"}}]}`},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(response.status)
			_, _ = io.WriteString(w, response.body)
		}))
		client := NewAIClient(context.Background(), server.URL, "test-model", "secret-key", time.Second, time.Second, 1)
		_, err := client.EvaluateVacancy(vacancyEvaluationInput{Description: "Python"})
		server.Close()
		if err == nil {
			t.Errorf("AI response should fail closed: status=%d", response.status)
		}
		if finalApplyDecision(VacancyEvaluation{}, 65) {
			t.Fatal("failed AI evaluation must never produce apply=true")
		}
	}
}

func TestStructuredVacancyRetriesInvalidResponses(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() {
		logger = previousLogger
		aiRetryDelay = previousDelay
	})

	valid := `{"score":88,"apply":true,"reasons":["Python matches"],"missing":[],"hard_requirements":[]}`
	tests := []struct {
		name      string
		first     string
		second    string
		wantErr   bool
		wantCalls int
	}{
		{name: "empty then valid", first: "", second: valid},
		{name: "malformed then valid", first: "not json", second: valid},
		{name: "incomplete then valid", first: `{"score":88,"apply":true}`, second: valid},
		{name: "score outside range then valid", first: `{"score":101,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`, second: valid},
		{name: "all attempts malformed", first: "not json", second: `{"score":88}`, wantErr: true, wantCalls: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload ChatCompletionRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("request JSON is invalid: %v", err)
				}
				if payload.ResponseFormat == nil || payload.ResponseFormat.Type != "json_object" {
					t.Errorf("structured request missing json_object response format: %+v", payload)
				}
				if payload.ResponseFormat != nil && payload.ResponseFormat.JSONSchema != nil {
					t.Errorf("generic endpoint received Mistral-specific JSON schema: %+v", payload.ResponseFormat)
				}
				if payload.ReasoningEffort != "" || payload.IncludeReasoning != nil {
					t.Errorf("generic endpoint received provider-specific reasoning options: %+v", payload)
				}
				if payload.MaxTokens != 1024 {
					t.Errorf("unexpected structured token budget: %d", payload.MaxTokens)
				}
				content := test.first
				if calls.Add(1) > 1 {
					content = test.second
				}
				_, _ = io.WriteString(w, aiCompletionResponse(content))
			}))
			defer server.Close()

			client := NewAIClient(context.Background(), server.URL, "test-model", "secret-key", time.Second, time.Second, 2)
			_, err := client.EvaluateVacancy(vacancyEvaluationInput{Description: "Python"})
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected error state: %v", err)
			}
			wantCalls := test.wantCalls
			if wantCalls == 0 {
				wantCalls = 2
			}
			if got := int(calls.Load()); got != wantCalls {
				t.Fatalf("unexpected attempt count: got %d, want %d", got, wantCalls)
			}
		})
	}
}

func aiCompletionResponse(content string) string {
	data, _ := json.Marshal(ChatCompletionResponse{
		Choices: []ChatCompletionChoice{{Message: AIMessage{Content: content}}},
	})
	return string(data)
}

func TestStructuredDiagnosticsExcludePayloads(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","reasoning":"private reasoning"},"finish_reason":"length"}],"usage":{"completion_tokens":7,"total_tokens":9}}`)
	}))
	defer server.Close()
	client := NewAIClient(context.Background(), server.URL, "private-model", "secret-key", time.Second, time.Second, 1)
	_, err := client.EvaluateVacancy(vacancyEvaluationInput{Description: "private prompt"})
	if err == nil {
		t.Fatal("empty structured response should fail")
	}
	for _, forbidden := range []string{"private prompt", "private reasoning", "secret-key", `"content"`} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("diagnostic contains sensitive data %q: %v", forbidden, err)
		}
	}
}

func TestStructuredTestSolutionsRetryValidation(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() {
		logger = previousLogger
		aiRetryDelay = previousDelay
	})

	responses := []string{
		`{"choices":[{"message":{"content":"{\"solutions\":[{\"task_id\":1,\"solution_id\":999}]}"}}]}`,
		`{"choices":[{"message":{"content":"{\"solutions\":[{\"task_id\":1,\"solution_id\":10}]}"}}]}`,
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("request JSON is invalid: %v", err)
		}
		if payload.ResponseFormat == nil || payload.ResponseFormat.Type != "json_object" {
			t.Errorf("test request missing json_object response format")
		}
		if payload.ResponseFormat != nil && payload.ResponseFormat.JSONSchema != nil {
			t.Errorf("generic endpoint received Mistral-specific JSON schema")
		}
		response := responses[0]
		if calls.Add(1) > 1 {
			response = responses[1]
		}
		_, _ = io.WriteString(w, response)
	}))
	defer server.Close()

	client := NewAIClient(context.Background(), server.URL, "test-model", "", time.Second, time.Second, 2)
	answers, err := client.SolveTests([]Task{{ID: 1, CandidateSolutions: []Solution{{ID: "10"}}}}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !answers[1].HasChoice || answers[1].SolutionID != 10 || calls.Load() != 2 {
		t.Fatalf("structured test retry did not produce validated answer: answers=%+v calls=%d", answers, calls.Load())
	}
}

func TestMistralStructuredVacancyUsesStrictJSONSchema(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var request ChatCompletionRequest
	client := NewAIClient(context.Background(), "https://api.mistral.ai", "ministral-8b-latest", "secret-key", time.Second, time.Second, 1)
	client.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Request:    r,
			Body:       io.NopCloser(strings.NewReader(aiCompletionResponse(`{"score":82,"apply":true,"reasons":["Python matches"],"missing":["FastAPI"],"hard_requirements":[],"strong_match":["REST API"]}`))),
			Header:     make(http.Header),
		}, nil
	})

	evaluation, err := client.EvaluateVacancy(vacancyEvaluationInput{Description: "Python"})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Score != 82 || !evaluation.Apply {
		t.Fatalf("valid Mistral evaluation was decoded incorrectly: %+v", evaluation)
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_schema" {
		t.Fatalf("Mistral request did not use json_schema: %+v", request.ResponseFormat)
	}
	jsonSchema := request.ResponseFormat.JSONSchema
	if jsonSchema == nil || jsonSchema.Name != "vacancy_evaluation" || !jsonSchema.Strict {
		t.Fatalf("Mistral schema wrapper is incorrect: %+v", jsonSchema)
	}
	if got, ok := jsonSchema.Schema["additionalProperties"].(bool); !ok || got {
		t.Fatalf("schema additionalProperties must be false: %v", jsonSchema.Schema["additionalProperties"])
	}
	requiredJSON, err := json.Marshal(jsonSchema.Schema["required"])
	if err != nil {
		t.Fatalf("schema required could not be encoded: %v", err)
	}
	var required []string
	if err := json.Unmarshal(requiredJSON, &required); err != nil {
		t.Fatalf("schema required has unexpected type: %T", jsonSchema.Schema["required"])
	}
	for _, field := range []string{"score", "apply", "reasons", "missing", "hard_requirements"} {
		if !slices.Contains(required, field) {
			t.Fatalf("schema does not require %q: %v", field, required)
		}
	}
	properties, ok := jsonSchema.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties have unexpected type: %T", jsonSchema.Schema["properties"])
	}
	score, ok := properties["score"].(map[string]any)
	if !ok || score["type"] != "integer" || score["minimum"] != float64(0) || score["maximum"] != float64(100) {
		t.Fatalf("score schema is incorrect: %v", properties["score"])
	}
	for _, field := range []string{"reasons", "missing", "hard_requirements", "strong_match"} {
		arraySchema, ok := properties[field].(map[string]any)
		if !ok || arraySchema["type"] != "array" {
			t.Fatalf("%s is not an array schema: %v", field, properties[field])
		}
		items, ok := arraySchema["items"].(map[string]any)
		if field == "hard_requirements" {
			if !ok || items["type"] != "object" || items["additionalProperties"] != false {
				t.Fatalf("hard_requirements items must be strict objects: %v", arraySchema["items"])
			}
		} else if !ok || items["type"] != "string" {
			t.Fatalf("%s items must be strings: %v", field, arraySchema["items"])
		}
	}
}

func TestMistralVacancyObjectWithUnknownFieldFailsLocalValidation(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	client := NewAIClient(context.Background(), "https://api.mistral.ai", "ministral-8b-latest", "", time.Second, time.Second, 1)
	client.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Request:    r,
			Body: io.NopCloser(strings.NewReader(aiCompletionResponse(
				`{"score":82,"apply":true,"reasons":["Python matches"],"missing":[],"hard_requirements":[{"requirement":"minimum 3 years","category":"experience_years","status":"unknown","vacancy_evidence":"3 years required","candidate_evidence":"not provided","unexpected":true}]}`,
			))),
			Header: make(http.Header),
		}, nil
	})

	if _, err := client.EvaluateVacancy(vacancyEvaluationInput{Description: "Python"}); err == nil {
		t.Fatal("vacancy evaluation with an unknown hard requirement field passed local validation")
	}
}

func TestHardRequirementSkipEventIncludesDiagnosticDetails(t *testing.T) {
	previousLogger := logger
	var logs bytes.Buffer
	logger = NewLogger(&logs, LevelInfo)
	t.Cleanup(func() { logger = previousLogger })

	var events bytes.Buffer
	r := &HHAIResponder{eventWriter: &events}
	evaluation := VacancyEvaluation{
		Score:            72,
		Apply:            true,
		HardRequirements: []HardRequirementEvaluation{{Requirement: "minimum 3 years commercial Python experience", Category: hardRequirementCategoryOther, Status: hardRequirementStatusMissing, VacancyEvidence: "3 years required", CandidateEvidence: "Explicit negative fact"}},
	}
	score := evaluation.Score
	reason := vacancyEvaluationRejectReason(evaluation, 65)
	r.skipVacancyWithEvaluation(Vacancy{ID: 123, Name: "Python backend"}, "https://example.test/vacancy/123", reason, &score, hardRequirementsMissing(evaluation), evaluation.HardRequirements)

	if !strings.Contains(logs.String(), "SKIP — hard requirements not met (score 72/100): minimum 3 years commercial Python experience") {
		t.Fatalf("hard requirement diagnostic was not logged: %s", logs.String())
	}
	var event VacancySkippedResult
	if err := json.Unmarshal(events.Bytes(), &event); err != nil {
		t.Fatalf("skip event is not valid JSON: %v", err)
	}
	if event.Reason != reason || !slices.Equal(event.HardRequirementsMissing, hardRequirementsMissing(evaluation)) || len(event.HardRequirements) != 1 {
		t.Fatalf("hard requirement details were not preserved in event: %+v", event)
	}
}

func TestMistralTestSolutionsUseSchemaAndSemanticValidation(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	previousDelay := aiRetryDelay
	aiRetryDelay = 0
	t.Cleanup(func() {
		logger = previousLogger
		aiRetryDelay = previousDelay
	})

	var calls atomic.Int32
	var request ChatCompletionRequest
	client := NewAIClient(context.Background(), "https://api.mistral.ai", "ministral-8b-latest", "", time.Second, time.Second, 2)
	client.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		content := `{"solutions":[{"task_id":1,"solution_id":999}]}`
		if calls.Add(1) > 1 {
			content = `{"solutions":[{"task_id":1,"solution_id":10}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Request:    r,
			Body:       io.NopCloser(strings.NewReader(aiCompletionResponse(content))),
			Header:     make(http.Header),
		}, nil
	})

	answers, err := client.SolveTests([]Task{{ID: 1, CandidateSolutions: []Solution{{ID: "10"}}}}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !answers[1].HasChoice || answers[1].SolutionID != 10 || calls.Load() != 2 {
		t.Fatalf("Mistral test solution validation did not fail closed then recover: answers=%+v calls=%d", answers, calls.Load())
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema == nil || request.ResponseFormat.JSONSchema.Name != "test_solutions" {
		t.Fatalf("Mistral test request did not use test_solutions schema: %+v", request.ResponseFormat)
	}
}

func TestStructuredGroqGPTOSSOptions(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var request ChatCompletionRequest
	client := NewAIClient(context.Background(), "https://api.groq.com/openai", "openai/gpt-oss-120b", "secret-key", time.Second, time.Second, 1)
	client.client.Transport = aiRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Request:    r,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	if _, err := client.ChatStructuredWithSchema("system", "user", 1024, 0.1, vacancyEvaluationJSONSchema(), func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
		t.Fatalf("missing Groq JSON mode: %+v", request)
	}
	if request.ResponseFormat.JSONSchema != nil {
		t.Fatalf("Groq request unexpectedly received Mistral JSON schema: %+v", request.ResponseFormat)
	}
	if request.ReasoningEffort != "low" || request.IncludeReasoning == nil || *request.IncludeReasoning {
		t.Fatalf("missing safe GPT-OSS reasoning options: %+v", request)
	}
}

func TestOrdinaryLetterDoesNotForceJSONMode(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var request ChatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("request JSON is invalid: %v", err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Письмо"}}]}`)
	}))
	defer server.Close()
	client := NewAIClient(context.Background(), server.URL, "test-model", "", time.Second, time.Second, 1)
	letter, err := client.GenerateLetter(Vacancy{Name: "Backend"}, "Python", CandidateContext{ResumeTitle: "Python developer"}, "")
	if err != nil || letter != "Письмо" {
		t.Fatalf("ordinary letter failed: letter=%q err=%v", letter, err)
	}
	if request.ResponseFormat != nil || request.ReasoningEffort != "" || request.IncludeReasoning != nil {
		t.Fatalf("ordinary letter unexpectedly used structured options: %+v", request)
	}
}

type aiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f aiRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDryRunVacancyAboveThresholdProducesPreviewWithoutWrite(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var events bytes.Buffer
	var hhCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hhCalls.Add(1)
		if r.Method != http.MethodGet {
			t.Fatalf("dry-run used HH write method %s", r.Method)
		}
		switch r.URL.Path {
		case "/search/vacancy":
			if r.URL.Query().Get("page") == "1" {
				_, _ = io.WriteString(w, "prefix,\"vacancies\":[]}")
				return
			}
			_, _ = io.WriteString(w, "prefix,\"vacancies\":[{\"vacancyId\":123,\"name\":\"Python backend developer\",\"links\":{\"desktop\":\"https://example.test/vacancy/123\"},\"company\":{\"name\":\"Example\"},\"responseLetterRequired\":false}]}")
		case "/vacancy/123":
			_, _ = io.WriteString(w, "{\"redirectConfig\":{},\"vacancyView\":{\"description\":\"Python, REST API and SQL\"}}")
		default:
			t.Fatalf("unexpected HH endpoint: %s", r.URL.Path)
		}
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"choices\":[{\"message\":{\"content\":\"{\\\"score\\\":88,\\\"apply\\\":true,\\\"reasons\\\":[\\\"Python matches\\\"],\\\"missing\\\":[],\\\"hard_requirements\\\":[],\\\"strong_match\\\":[\\\"Python\\\"]}\"}}]}")
	}))
	defer aiServer.Close()

	ctx := context.Background()
	r := &HHAIResponder{
		ctx:           ctx,
		baseURL:       mustURL(t, hhServer.URL),
		requester:     NewHHRequester(ctx, hhServer.Client(), 0),
		ai:            NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		autoApply:     true,
		dryRun:        true,
		resumeHash:    "resume-hash",
		resumes:       []ResumeItem{{Hash: "resume-hash", Title: "Python developer", Skills: "Python, SQL"}},
		minMatchScore: 65,
		eventWriter:   &events,
	}

	if err := r.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if got := hhCalls.Load(); got != 3 {
		t.Fatalf("unexpected number of HH read requests: got %d, want 3", got)
	}
	if !strings.Contains(events.String(), "\"type\":\"vacancy_match\"") || !strings.Contains(events.String(), "\"type\":\"application_preview\"") {
		t.Fatalf("dry-run did not emit match and application preview events: %s", events.String())
	}
	if !strings.Contains(events.String(), "\"type\":\"run_summary\"") ||
		!strings.Contains(events.String(), "\"ai_evaluated\":1") ||
		!strings.Contains(events.String(), "\"would_apply\":1") ||
		!strings.Contains(events.String(), "\"vacancies_fetched\":1") ||
		!strings.Contains(events.String(), "\"vacancies_processed\":1") ||
		!strings.Contains(events.String(), "\"vacancies_seen\":1") {
		t.Fatalf("dry-run summary is incomplete: %s", events.String())
	}
	if strings.Contains(events.String(), "responses_count") {
		t.Fatalf("dry-run fabricated unknown response count: %s", events.String())
	}
}

func TestDryRunUnknownHardRequirementRequiresReview(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var events bytes.Buffer
	var writeCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCalls.Add(1)
		}
		switch r.URL.Path {
		case "/search/vacancy":
			if r.URL.Query().Get("page") == "1" {
				_, _ = io.WriteString(w, "prefix,\"vacancies\":[]}")
				return
			}
			_, _ = io.WriteString(w, "prefix,\"vacancies\":[{\"vacancyId\":456,\"name\":\"Python backend developer\",\"links\":{\"desktop\":\"https://example.test/vacancy/456\"},\"company\":{\"name\":\"Example\"}}]}")
		case "/vacancy/456":
			_, _ = io.WriteString(w, "{\"redirectConfig\":{},\"vacancyView\":{\"description\":\"Требуется высшее образование и работа в офисе Ташкента\"}}")
		default:
			t.Fatalf("unexpected HH endpoint: %s", r.URL.Path)
		}
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":92,"apply":true,"reasons":["Python matches"],"missing":[],"hard_requirements":[{"requirement":"высшее образование","category":"education","status":"unknown","vacancy_evidence":"Требуется высшее образование","candidate_evidence":"not provided"},{"requirement":"офис в Ташкенте","category":"location","status":"unknown","vacancy_evidence":"Работа в офисе Ташкента","candidate_evidence":"not provided"}]}`))
	}))
	defer aiServer.Close()

	ctx := context.Background()
	r := &HHAIResponder{
		ctx:           ctx,
		baseURL:       mustURL(t, hhServer.URL),
		requester:     NewHHRequester(ctx, hhServer.Client(), 0),
		ai:            NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		autoApply:     true,
		dryRun:        true,
		resumeHash:    "resume-hash",
		resumes:       []ResumeItem{{Hash: "resume-hash", Title: "Python developer", Skills: "Python, SQL"}},
		minMatchScore: 65,
		eventWriter:   &events,
	}

	if err := r.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if writeCalls.Load() != 0 {
		t.Fatalf("review-only dry-run used HH write requests: %d", writeCalls.Load())
	}
	got := events.String()
	if !strings.Contains(got, `"type":"vacancy_review_required"`) ||
		strings.Contains(got, `"type":"vacancy_match"`) ||
		strings.Contains(got, `"type":"application_preview"`) ||
		!strings.Contains(got, `"review_required":1`) ||
		!strings.Contains(got, `"matched":0`) ||
		!strings.Contains(got, `"would_apply":0`) {
		t.Fatalf("unknown hard requirement was not isolated as review: %s", got)
	}
}

func TestAIDebugLogContainsMetadataButNoPayload(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := logger
	logger = NewLogger(&logs, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer server.Close()
	client := NewAIClient(context.Background(), server.URL, "private-model", "secret-key", time.Second, time.Second, 1)
	if _, err := client.Chat("ФИО Иван Иванов, резюме и контакты", "переписка работодателя", 321, 0.1); err != nil {
		t.Fatal(err)
	}
	got := logs.String()
	for _, forbidden := range []string{"ФИО Иван Иванов", "переписка работодателя", "secret-key", `"content"`} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("debug log contains sensitive payload fragment %q: %s", forbidden, got)
		}
	}
	for _, expected := range []string{"endpoint=", "model=private-model", "messages=2", "max_tokens=321", "temperature=0.10", "payload_bytes="} {
		if !strings.Contains(got, expected) {
			t.Fatalf("debug metadata %q missing from log: %s", expected, got)
		}
	}
}

func TestChatModesAndHighRiskReview(t *testing.T) {
	if got := classifyHighRiskChatMessage("Пришлите паспорт и банковские реквизиты"); got == "" {
		t.Fatal("high-risk message was not classified")
	}
	if got := chatReplyReviewReason("auto", false, "Пришлите паспорт"); got == "" {
		t.Fatal("high-risk auto reply was not routed to review")
	}
	if got := chatReplyReviewReason("review", false, "Добрый день"); got != "chat mode review" {
		t.Fatalf("review mode reason: %q", got)
	}

	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	ctx := context.Background()
	for name, responder := range map[string]*HHAIResponder{
		"off":          {ctx: ctx, chatMode: "off", autoChat: true, baseURL: mustURL(t, server.URL), requester: NewHHRequester(ctx, server.Client(), 0)},
		"review":       {ctx: ctx, chatMode: "review", autoChat: true, baseURL: mustURL(t, server.URL), requester: NewHHRequester(ctx, server.Client(), 0)},
		"dry-run auto": {ctx: ctx, chatMode: "auto", autoChat: true, dryRun: true, baseURL: mustURL(t, server.URL), requester: NewHHRequester(ctx, server.Client(), 0)},
	} {
		if name == "off" {
			if err := responder.AutoRespondChats(); err != nil {
				t.Fatalf("off mode: %v", err)
			}
			continue
		}
		result, err := responder.SendChatMessage(1, "reply")
		if err != nil {
			t.Fatalf("%s mode: %v", name, err)
		}
		if name == "review" && result["disabled"] != true {
			t.Fatalf("review mode sent a message: %v", result)
		}
		if name == "dry-run auto" && result["dry_run"] != true {
			t.Fatalf("dry-run auto mode did not return preview marker: %v", result)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("chat mode guards issued %d HTTP requests", got)
	}
}

func TestDryRunBlocksAllHHWrites(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx := context.Background()
	r := &HHAIResponder{
		ctx:           ctx,
		baseURL:       mustURL(t, server.URL),
		dryRun:        true,
		autoApply:     true,
		autoChat:      true,
		autoTouch:     true,
		autoJobStatus: true,
		resumeHash:    "resume-hash",
		requester:     NewHHRequester(ctx, server.Client(), 0),
	}

	for name, call := range map[string]func() error{
		"response": func() error {
			result, err := r.SendResponse(url.Values{}, server.URL)
			if err == nil && result["dry_run"] != true {
				return errUnexpectedDryRunMarker
			}
			return err
		},
		"application": func() error {
			result, err := r.ApplyVacancy(1, server.URL, "letter")
			if err == nil && result["dry_run"] != true {
				return errUnexpectedDryRunMarker
			}
			return err
		},
		"chat message": func() error {
			result, err := r.SendChatMessage(1, "reply")
			if err == nil && result["dry_run"] != true {
				return errUnexpectedDryRunMarker
			}
			return err
		},
		"leave chat": func() error {
			result, err := r.LeaveChat(1)
			if err == nil && result["dry_run"] != true {
				return errUnexpectedDryRunMarker
			}
			return err
		},
		"job status": func() error {
			ok, err := r.SetActiveJobSearchStatus()
			if err == nil && !ok {
				return errUnexpectedDryRunMarker
			}
			return err
		},
		"touch resume": func() error {
			ok, err := r.TouchResume()
			if err == nil && !ok {
				return errUnexpectedDryRunMarker
			}
			return err
		},
	} {
		if err := call(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("dry-run issued %d HTTP requests to a write endpoint", got)
	}
}

func TestIndependentActionFlagsBlockWrites(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx := context.Background()
	r := &HHAIResponder{
		ctx:           ctx,
		baseURL:       mustURL(t, server.URL),
		autoApply:     false,
		autoChat:      false,
		autoTouch:     false,
		autoJobStatus: false,
		resumeHash:    "resume-hash",
		requester:     NewHHRequester(ctx, server.Client(), 0),
	}

	if result, err := r.SendResponse(url.Values{}, server.URL); err != nil || result["disabled"] != true {
		t.Fatalf("response was not blocked: result=%v err=%v", result, err)
	}
	if result, err := r.SendChatMessage(1, "reply"); err != nil || result["disabled"] != true {
		t.Fatalf("chat message was not blocked: result=%v err=%v", result, err)
	}
	if result, err := r.LeaveChat(1); err != nil || result["disabled"] != true {
		t.Fatalf("leave chat was not blocked: result=%v err=%v", result, err)
	}
	if ok, err := r.TouchResume(); err != nil || ok {
		t.Fatalf("touch was not blocked: ok=%v err=%v", ok, err)
	}
	if ok, err := r.SetActiveJobSearchStatus(); err != nil || ok {
		t.Fatalf("job status update was not blocked: ok=%v err=%v", ok, err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("disabled action issued %d HTTP requests", got)
	}
}

func TestGetEnvBool(t *testing.T) {
	t.Setenv("HH_TEST_BOOL", "false")
	value, err := getEnvBool("HH_TEST_BOOL", true)
	if err != nil || value {
		t.Fatalf("false env value parsed incorrectly: value=%v err=%v", value, err)
	}

	t.Setenv("HH_TEST_BOOL", "not-a-bool")
	if _, err := getEnvBool("HH_TEST_BOOL", true); err == nil {
		t.Fatal("invalid boolean env value was accepted")
	}
}

var errUnexpectedDryRunMarker = &testError{"unexpected dry-run marker"}

type testError struct{ message string }

func (e *testError) Error() string { return e.message }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
