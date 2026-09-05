package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfirmedAlreadyRespondedStateSavedAndFalseOrUnknownIgnored(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	dir := t.TempDir()
	confirmedPath := filepath.Join(dir, "confirmed.json")
	r := &HHAIResponder{alreadyRespondedStatePath: confirmedPath}
	r.rememberConfirmedPreflight(VacancyPreflight{VacancyID: 42, AlreadyRespondedKnown: true, AlreadyResponded: true})

	data, err := os.ReadFile(confirmedPath)
	if err != nil {
		t.Fatal(err)
	}
	var state alreadyRespondedStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.VacancyIDs) != 1 || state.VacancyIDs[0] != 42 {
		t.Fatalf("unexpected confirmed state: %+v", state)
	}

	for _, preflight := range []VacancyPreflight{
		{VacancyID: 43, AlreadyRespondedKnown: true, AlreadyResponded: false},
		{VacancyID: 44, AlreadyRespondedKnown: false, AlreadyResponded: true},
	} {
		r.rememberConfirmedPreflight(preflight)
	}
	data, err = os.ReadFile(confirmedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.VacancyIDs) != 1 || state.VacancyIDs[0] != 42 {
		t.Fatalf("false/unknown preflight changed state: %+v", state)
	}
}

func TestPreviouslyRespondedStateSkipsBeforeAI(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var aiCalls atomic.Int32
	var hhWriteCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			hhWriteCalls.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/search/vacancy":
			if r.URL.Query().Get("page") == "0" {
				_, _ = io.WriteString(w, `prefix,"vacancies":[{"vacancyId":777,"name":"Python backend developer","links":{"desktop":"/vacancy/777"}}]}`)
			} else {
				_, _ = io.WriteString(w, `prefix,"vacancies":[]}`)
			}
		case "/vacancy/777":
			_, _ = io.WriteString(w, `{"redirectConfig":{},"vacancyView":{"description":"Python, REST API and SQL"}}`)
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":true,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		default:
			t.Fatalf("unexpected HH endpoint: %s", r.URL.Path)
		}
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":88,"apply":true,"reasons":["Python matches"],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	statePath := filepath.Join(t.TempDir(), "already-responded.json")
	ctx := context.Background()
	responder := &HHAIResponder{
		ctx:                       ctx,
		baseURL:                   mustURL(t, hhServer.URL),
		requester:                 NewHHRequester(ctx, hhServer.Client(), 0),
		ai:                        NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		autoApply:                 true,
		dryRun:                    true,
		resumeHash:                "resume-hash",
		resumes:                   []ResumeItem{{Hash: "resume-hash", Title: "Python developer", Skills: "Python, SQL"}},
		minMatchScore:             65,
		alreadyRespondedStatePath: statePath,
	}

	var firstEvents bytes.Buffer
	responder.eventWriter = &firstEvents
	if err := responder.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if got := aiCalls.Load(); got != 1 {
		t.Fatalf("first run AI calls: got %d, want 1", got)
	}
	if !responder.isAlreadyResponded(777) {
		t.Fatal("confirmed preflight vacancy was not loaded into local state")
	}
	if strings.Contains(firstEvents.String(), `"type":"application_preview"`) {
		t.Fatal("already responded vacancy produced application preview")
	}

	var secondEvents bytes.Buffer
	responder.eventWriter = &secondEvents
	if err := responder.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if got := aiCalls.Load(); got != 1 {
		t.Fatalf("previously responded vacancy reached AI on second run: calls=%d", got)
	}
	if !strings.Contains(secondEvents.String(), `"previously_responded_skipped":1`) {
		t.Fatalf("second run summary did not count local skip: %s", secondEvents.String())
	}
	if hhWriteCalls.Load() != 0 {
		t.Fatalf("dry-run issued %d HH writes", hhWriteCalls.Load())
	}
}

func TestCorruptAlreadyRespondedStateFailsSafe(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	path := filepath.Join(t.TempDir(), "already-responded.json")
	if err := os.WriteFile(path, []byte(`{"already_responded":[777]`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &HHAIResponder{alreadyRespondedStatePath: path}
	r.loadAlreadyRespondedState()
	if r.isAlreadyResponded(777) {
		t.Fatal("corrupt state marked vacancy as already responded")
	}
}
