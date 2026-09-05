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

func TestConfiguredSearchURLsFallsBackToLegacyURL(t *testing.T) {
	got, err := configuredSearchURLs("", "https://hh.example/search/vacancy?text=python")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "https://hh.example/search/vacancy?text=python" {
		t.Fatalf("legacy search URL fallback: got %#v", got)
	}
}

func TestBuildVacancySearchProfilesPreservesAreaAndResume(t *testing.T) {
	profiles, _, err := buildVacancySearchProfiles([]string{
		"https://hh.example/search/vacancy?text=python&area=3&resume=resume-hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles: got %d, want 1", len(profiles))
	}
	params := profiles[0].Params
	for key, want := range map[string]string{
		"area":          "3",
		"resume":        "resume-hash",
		"order_by":      "publication_time",
		"search_period": "7",
		"items_on_page": "50",
	} {
		if got := params.Get(key); got != want {
			t.Fatalf("query %s: got %q, want %q", key, got, want)
		}
	}
}

func TestMultiSearchDeduplicatesBeforeAIAndAppliesGlobalLimit(t *testing.T) {
	previousLogger := logger
	logger = NewLogger(io.Discard, LevelDebug)
	t.Cleanup(func() { logger = previousLogger })

	var aiCalls atomic.Int32
	var hhWriteCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			hhWriteCalls.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if req.URL.Path == "/search/vacancy" {
			for key, want := range map[string]string{
				"area":          "3",
				"resume":        "resume-hash",
				"order_by":      "publication_time",
				"search_period": "7",
				"items_on_page": "50",
			} {
				if got := req.URL.Query().Get(key); got != want {
					t.Fatalf("search query %s: got %q, want %q", key, got, want)
				}
			}
			if req.URL.Query().Get("page") != "0" {
				_, _ = io.WriteString(w, `prefix,"vacancies":[]}`)
				return
			}
			switch req.URL.Query().Get("profile") {
			case "one":
				_, _ = io.WriteString(w, `prefix,"vacancies":[{"vacancyId":101,"name":"Python backend","links":{"desktop":"/vacancy/101"}},{"vacancyId":102,"name":"Django backend","links":{"desktop":"/vacancy/102"}}]}`)
			case "two":
				_, _ = io.WriteString(w, `prefix,"vacancies":[{"vacancyId":101,"name":"Python backend duplicate","links":{"desktop":"/vacancy/101"}},{"vacancyId":103,"name":"Support","links":{"desktop":"/vacancy/103"}}]`)
			default:
				t.Fatalf("unexpected search profile: %q", req.URL.Query().Get("profile"))
			}
			return
		}
		switch req.URL.Path {
		case "/vacancy/101":
			_, _ = io.WriteString(w, `{"redirectConfig":{},"vacancyView":{"description":"Python, Django, REST API and SQL"}}`)
		case "/applicant/vacancy_response":
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		default:
			t.Fatalf("unexpected HH endpoint: %s", req.URL.Path)
		}
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":88,"apply":true,"reasons":["matches"],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	statePath := filepath.Join(t.TempDir(), "already-responded.json")
	if err := os.WriteFile(statePath, []byte(`{"already_responded":[102]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	baseURL := mustURL(t, hhServer.URL)
	profiles, _, err := buildVacancySearchProfiles([]string{
		hhServer.URL + "/search/vacancy?profile=one&area=3&resume=resume-hash",
		hhServer.URL + "/search/vacancy?profile=two&area=3&resume=resume-hash",
	})
	if err != nil {
		t.Fatal(err)
	}

	var events bytes.Buffer
	ctx := context.Background()
	responder := &HHAIResponder{
		ctx:                       ctx,
		baseURL:                   baseURL,
		requester:                 NewHHRequester(ctx, hhServer.Client(), 0),
		ai:                        NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1),
		searchProfiles:            profiles,
		autoApply:                 true,
		dryRun:                    true,
		resumeHash:                "resume-hash",
		resumes:                   []ResumeItem{{Hash: "resume-hash", Title: "Python developer", Skills: "Python, SQL"}},
		minMatchScore:             65,
		maxVacanciesPerRun:        1,
		alreadyRespondedStatePath: statePath,
		eventWriter:               &events,
	}

	if err := responder.ApplyVacancies(); err != nil {
		t.Fatal(err)
	}
	if got := aiCalls.Load(); got != 1 {
		t.Fatalf("AI evaluations: got %d, want 1", got)
	}
	if got := hhWriteCalls.Load(); got != 0 {
		t.Fatalf("dry-run issued %d HH writes", got)
	}
	if strings.Count(events.String(), `"type":"application_preview"`) != 1 {
		t.Fatalf("application preview count: %s", events.String())
	}

	var summary RunSummaryResult
	for _, line := range strings.Split(strings.TrimSpace(events.String()), "\n") {
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "run_summary" {
			if err := json.Unmarshal([]byte(line), &summary); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(summary.SearchProfiles) != 2 || summary.SearchProfiles[0].VacanciesFetched != 2 || summary.SearchProfiles[1].VacanciesFetched != 2 {
		t.Fatalf("profile summary: %+v", summary.SearchProfiles)
	}
	if summary.VacanciesFetchedRaw != 4 || summary.VacanciesAfterDedup != 3 || summary.DuplicatesSkipped != 1 {
		t.Fatalf("dedup summary: %+v", summary)
	}
	if summary.PreviouslyRespondedSkipped != 1 || summary.AIEvaluated != 1 || summary.Matched != 1 || summary.WouldApply != 1 || summary.VacancyLimitSkipped != 1 {
		t.Fatalf("pipeline summary: %+v", summary)
	}
}
