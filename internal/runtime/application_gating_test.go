package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	domain "hh-ai-responder/internal/applicationattempt"
)

func TestApplyVacanciesBlockingAttemptSkipsBeforeAIAndMutation(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var aiCalls atomic.Int32
	var mutationCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutationCalls.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/search/vacancy" {
			t.Fatalf("blocked vacancy reached unexpected read endpoint: %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "0" {
			_, _ = io.WriteString(w, `prefix,"vacancies":[{"vacancyId":901,"name":"Python backend developer","links":{"desktop":"/vacancy/901"}}]}`)
			return
		}
		_, _ = io.WriteString(w, `prefix,"vacancies":[]}`)
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":99,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	path := filepath.Join(t.TempDir(), jsonstorage.ApplicationAttemptsFilename)
	store := jsonstorage.NewApplicationAttemptRepository(path)
	if _, err := store.Reserve(context.Background(), func() domain.Attempt {
		value, createErr := domain.New(901, "resume-hash", time.Now().UTC())
		if createErr != nil {
			t.Fatal(createErr)
		}
		return value
	}()); err != nil {
		t.Fatal(err)
	}
	reserved, err := store.FindBlocking(context.Background(), 901)
	if err != nil {
		t.Fatal(err)
	}
	if reserved.State != domain.StateSending {
		t.Fatalf("unexpected reserved state: %s", reserved.State)
	}
	if err := store.RecordOutcome(context.Background(), reserved.AttemptID, domain.StateAccepted, reserved.UpdatedAt.Add(time.Minute), 200, ""); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	responder := &HHAIResponder{
		ctx: ctx, baseURL: mustURL(t, hhServer.URL), requester: NewHHRequester(ctx, hhServer.Client(), 0),
		ai: NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1), autoApply: true, dryRun: true,
		resumeHash: "resume-hash", resumes: []ResumeItem{{Hash: "resume-hash", Title: "Python developer", Skills: "Python"}},
		minMatchScore: 65, applicationAttempts: store, eventWriter: &bytes.Buffer{},
	}
	if err := responder.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if aiCalls.Load() != 0 {
		t.Fatalf("blocking vacancy reached AI: %d calls", aiCalls.Load())
	}
	if mutationCalls.Load() != 0 {
		t.Fatalf("blocking vacancy caused HH mutation: %d calls", mutationCalls.Load())
	}
	if strings.Contains(responder.eventWriter.(*bytes.Buffer).String(), `"application_preview"`) {
		t.Fatal("blocking vacancy produced an application preview")
	}
}
