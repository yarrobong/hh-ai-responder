package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
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
