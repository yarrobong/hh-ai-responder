package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func TestDailyCareerAgentAmbiguousRouteReachesBoundedAdvisoryAIWithoutWrites(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var aiCalls atomic.Int32
	var hhWrites atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			hhWrites.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/search/vacancy":
			if r.URL.Query().Get("page") == "0" {
				_, _ = io.WriteString(w, `prefix,"vacancies":[{"vacancyId":9101,"name":"Python backend","links":{"desktop":"https://example.test/vacancy/9101"},"company":{"name":"Example"}}]}`)
			} else {
				_, _ = io.WriteString(w, `prefix,"vacancies":[]}`)
			}
		case "/vacancy/9101":
			_, _ = io.WriteString(w, `{"redirectConfig":{},"vacancyView":{"description":"Python backend API"}}`)
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true,"area":{"name":"Екатеринбург"},"workSchedule":"Можно удалённо","workExperience":"Без опыта"}}`)
		default:
			t.Fatalf("unexpected HH endpoint: %s", r.URL.Path)
		}
	}))
	defer hhServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"recommendation":"APPLY","reasons":["advisory fit"],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	var events bytes.Buffer
	ctx := context.Background()
	responder := &HHAIResponder{
		ctx: ctx, baseURL: mustURL(t, hhServer.URL), requester: NewHHRequester(ctx, hhServer.Client(), 0),
		ai:        NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		autoApply: true, dryRun: true, hhWriteEnabled: false, careerAgentMode: "shadow", minMatchScore: 65,
		resumeHash: "hash-a", resumes: []ResumeItem{{Hash: "hash-a", Title: "Python backend"}, {Hash: "hash-b", Title: "Python backend"}},
		careerAgentResumes: []careeragent.ResumeProfile{
			{ID: "resume-a", Hash: "hash-a", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
			{ID: "resume-b", Hash: "hash-b", Title: "Python backend", Skills: []string{"Python"}, Enabled: true},
		},
		resumeFactsByHash: map[string]ResumeFacts{"hash-a": {ExperienceText: "Python backend"}, "hash-b": {ExperienceText: "Python backend"}},
		eventWriter:       &events,
	}
	if err := responder.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if got := aiCalls.Load(); got != 1 {
		t.Fatalf("ambiguous route advisory AI calls=%d, want 1; events=%s", got, events.String())
	}
	if got := hhWrites.Load(); got != 0 {
		t.Fatalf("ambiguous route issued %d HH writes", got)
	}
	if !strings.Contains(events.String(), `"final_ambiguous":1`) || !strings.Contains(events.String(), `"ai_evaluated":1`) {
		t.Fatalf("ambiguous advisory telemetry missing: %s", events.String())
	}
	if strings.Contains(events.String(), `"type":"application"`) || strings.Contains(events.String(), `"type":"application_preview"`) {
		t.Fatalf("ambiguous advisory unexpectedly prepared/submitted application: %s", events.String())
	}
}
