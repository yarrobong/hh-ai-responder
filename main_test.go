package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	}

	candidate := r.candidateContext(resume)
	if candidate.Salary != resume.Salary {
		t.Fatalf("salary mismatch: got %q, want %q", candidate.Salary, resume.Salary)
	}
	if candidate.Experience != r.resumeExperience {
		t.Fatalf("experience mismatch: got %q, want %q", candidate.Experience, r.resumeExperience)
	}

	prompt := buildLetterSystemPrompt(candidate, "")
	if !strings.Contains(prompt, "Зарплата: 100000 руб") {
		t.Errorf("letter prompt does not contain salary: %s", prompt)
	}
	if !strings.Contains(prompt, "реальный опыт") {
		t.Errorf("letter prompt does not contain experience: %s", prompt)
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
	valid, err := parseVacancyEvaluationJSON(`{"score":82,"apply":true,"reasons":["Python"],"missing":["FastAPI"],"strong_match":["REST API"]}`)
	if err != nil {
		t.Fatalf("valid evaluation rejected: %v", err)
	}
	if valid.Score != 82 || !valid.Apply || valid.StrongMatch[0] != "REST API" {
		t.Fatalf("evaluation decoded incorrectly: %+v", valid)
	}

	for _, raw := range []string{
		`{"score":-1,"apply":true,"reasons":[],"missing":[]}`,
		`{"score":101,"apply":true,"reasons":[],"missing":[]}`,
		`{"score":82,"apply":true,"reasons":[],"missing":[] trailing}`,
		`{"score":82,"apply":true,"reasons":null,"missing":[]}`,
		`{"score":82,"apply":true,"reasons":[],"missing":[],"unexpected":true}`,
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
}

func TestVacancyEvaluationPromptContainsCandidateAndVacancyFacts(t *testing.T) {
	systemPrompt, userPrompt := buildVacancyEvaluationPrompt(vacancyEvaluationInput{
		Candidate: CandidateContext{
			FullName:    "Имя Кандидата",
			ResumeTitle: "Python developer",
			Salary:      "100000 руб",
			Skills:      "Python, Django",
			Experience:  "Реальный опыт с API",
		},
		Vacancy: Vacancy{
			Name:    "Backend developer",
			Company: Company{Name: "Example"},
		},
		Description:     "Разработка REST API",
		Salary:          "120000-150000 руб",
		Location:        "Екатеринбург",
		WorkSchedule:    "удаленная работа",
		IncludeKeywords: []string{"Django", "REST API"},
	})
	for _, expected := range []string{"Имя Кандидата", "Python developer", "100000 руб", "Python, Django", "Реальный опыт с API", "Backend developer", "Example", "Разработка REST API", "120000-150000 руб", "Екатеринбург", "удаленная работа", "Django, REST API"} {
		if !strings.Contains(userPrompt, expected) {
			t.Errorf("evaluation prompt does not contain %q: %s", expected, userPrompt)
		}
	}
	for _, expected := range []string{"только валидный JSON", "Не выдумывай", "основной стек подходит"} {
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
		_, _ = io.WriteString(w, "{\"choices\":[{\"message\":{\"content\":\"{\\\"score\\\":88,\\\"apply\\\":true,\\\"reasons\\\":[\\\"Python matches\\\"],\\\"missing\\\":[],\\\"strong_match\\\":[\\\"Python\\\"]}\"}}]}")
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
		!strings.Contains(events.String(), "\"would_apply\":1") {
		t.Fatalf("dry-run summary is incomplete: %s", events.String())
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
