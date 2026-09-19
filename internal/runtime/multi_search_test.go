package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
)

func newMultiSearchResponder(t *testing.T, pages map[int][]Vacancy) (*HHAIResponder, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/search/vacancy" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		page, err := strconv.Atoi(req.URL.Query().Get("page"))
		if err != nil {
			page = 0
		}
		items := pages[page]
		records := make([]string, 0, len(items))
		for _, item := range items {
			records = append(records, fmt.Sprintf(`{"vacancyId":%d,"name":"vacancy-%d","links":{"desktop":"/vacancy/%d"}}`, item.ID, item.ID, item.ID))
		}
		_, _ = fmt.Fprintf(w, `prefix,"vacancies":[%s]}`, strings.Join(records, ","))
	}))
	base := mustURL(t, server.URL)
	profiles := []vacancySearchProfile{
		{ID: "a", Name: "profile-a", BaseURL: base, Params: url.Values{"profile": {"a"}, "items_on_page": {"50"}}},
		{ID: "b", Name: "profile-b", BaseURL: base, Params: url.Values{"profile": {"b"}, "items_on_page": {"50"}}},
	}
	ctx := context.Background()
	responder := &HHAIResponder{
		ctx: ctx, baseURL: base, requester: NewHHRequester(ctx, server.Client(), 0), searchProfiles: profiles,
		maxSearchPagesPerProfile: 3, maxSearchPagesPerRun: 48,
	}
	return responder, server
}

func TestProfilePageCapStopsOnlyCurrentProfile(t *testing.T) {
	responder, server := newMultiSearchResponder(t, map[int][]Vacancy{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 3}, {ID: 4}},
		2: {{ID: 5}, {ID: 6}},
		3: {{ID: 7}},
	})
	defer server.Close()
	responder.maxSearchPagesPerProfile = 2
	responder.maxSearchPagesPerRun = 48
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.DiscoveryTruncated || summary.DiscoveryComplete || summary.SearchPagesTruncated == 0 {
		t.Fatalf("profile-cap truncation was not explicit: %+v", summary)
	}
	if len(summary.SearchProfiles) != 2 || summary.SearchProfiles[0].PagesFetched != 2 || summary.SearchProfiles[1].PagesFetched != 2 {
		t.Fatalf("later profile did not continue after profile cap: %+v", summary.SearchProfiles)
	}
}

func TestRunPageCapMarksUnstartedProfiles(t *testing.T) {
	responder, server := newMultiSearchResponder(t, map[int][]Vacancy{
		0: {{ID: 1}, {ID: 2}},
		1: {{ID: 3}, {ID: 4}},
		2: {{ID: 5}},
	})
	defer server.Close()
	responder.maxSearchPagesPerProfile = 48
	responder.maxSearchPagesPerRun = 2
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.DiscoveryTruncated || summary.DiscoveryComplete || summary.SearchPagesTruncated == 0 {
		t.Fatalf("run-cap truncation was not explicit: %+v", summary)
	}
	if len(summary.SearchProfiles) != 2 || summary.SearchProfiles[0].PagesFetched != 2 || summary.SearchProfiles[1].PagesFetched != 0 || !summary.SearchProfiles[1].Truncated {
		t.Fatalf("unstarted profile accounting is wrong: %+v", summary.SearchProfiles)
	}
}

type profilePage struct {
	ProfileID string
	Items     []Vacancy
}

func newProfileFixtureResponder(t *testing.T, pages []profilePage) (*HHAIResponder, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/search/vacancy" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		profileID := req.URL.Query().Get("profile")
		page, err := strconv.Atoi(req.URL.Query().Get("page"))
		if err != nil {
			page = 0
		}
		var items []Vacancy
		if page == 0 {
			for _, fixture := range pages {
				if fixture.ProfileID == profileID {
					items = fixture.Items
					break
				}
			}
		}
		records := make([]string, 0, len(items))
		for _, item := range items {
			records = append(records, fmt.Sprintf(`{"vacancyId":%d,"name":"vacancy-%d","links":{"desktop":"/vacancy/%d"}}`, item.ID, item.ID, item.ID))
		}
		_, _ = fmt.Fprintf(w, `prefix,"vacancies":[%s]}`, strings.Join(records, ","))
	}))
	base := mustURL(t, server.URL)
	profiles := make([]vacancySearchProfile, 0, len(pages))
	for _, fixture := range pages {
		profiles = append(profiles, vacancySearchProfile{ID: fixture.ProfileID, Name: fixture.ProfileID, BaseURL: base, Params: url.Values{"profile": {fixture.ProfileID}, "items_on_page": {"50"}}})
	}
	ctx := context.Background()
	responder := &HHAIResponder{ctx: ctx, baseURL: base, requester: NewHHRequester(ctx, server.Client(), 0), searchProfiles: profiles, maxSearchPagesPerProfile: 48, maxSearchPagesPerRun: 48}
	return responder, server
}

func searchProfileSummaryByID(summary RunSummaryResult, profileID string) *SearchProfileSummary {
	for index := range summary.SearchProfiles {
		if summary.SearchProfiles[index].ID == profileID {
			return &summary.SearchProfiles[index]
		}
	}
	return nil
}

func TestProfileTelemetrySeparatesOverlapAndUnionContribution(t *testing.T) {
	responder, server := newProfileFixtureResponder(t, []profilePage{
		{ProfileID: "a", Items: []Vacancy{{ID: 1}, {ID: 2}, {ID: 2}, {ID: 3}}},
		{ProfileID: "b", Items: []Vacancy{{ID: 2}, {ID: 3}, {ID: 4}}},
	})
	defer server.Close()
	summary := RunSummaryResult{}
	_, err := responder.fetchVacanciesFromSearchProfilesWithLimit(&summary, responder.searchProfiles, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := searchProfileSummaryByID(summary, "a")
	b := searchProfileSummaryByID(summary, "b")
	if a == nil || b == nil {
		t.Fatalf("profile summaries missing: %+v", summary.SearchProfiles)
	}
	if a.RawHits != 4 || a.DistinctProfileVacancies != 3 || a.ExclusiveVacancies != 1 || a.OverlapVacancies != 2 || a.UnionNewContribution != 3 {
		t.Fatalf("profile a accounting is wrong: %+v", a)
	}
	if b.RawHits != 3 || b.DistinctProfileVacancies != 3 || b.ExclusiveVacancies != 1 || b.OverlapVacancies != 2 || b.UnionNewContribution != 1 {
		t.Fatalf("profile b accounting is wrong: %+v", b)
	}
	if a.RawHits-a.DistinctProfileVacancies == a.OverlapVacancies {
		t.Fatalf("within-profile repeated hit was incorrectly used as overlap: %+v", a)
	}
}

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

func TestManualProfilesGetManualMetadataWithoutChangingPrecedence(t *testing.T) {
	profiles, _, err := buildVacancySearchProfilesWithOptions([]string{
		"https://hh.example/search/vacancy?text=python&area=3&resume=resume-hash",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	career := manualCareerAgentSearchProfiles(profiles)
	if len(career) != 1 || career[0].ProfileType != careeragent.SearchProfileManual || career[0].Reason != "MANUAL_PROFILE" {
		t.Fatalf("manual provenance was not explicit: %+v", career)
	}
	if career[0].Params.Get("area") != "3" || career[0].Params.Get("resume") != "resume-hash" || career[0].Params.Get("items_on_page") != "50" {
		t.Fatalf("manual provider params changed: %+v", career[0].Params)
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
	if summary.VacanciesProcessed != 3 || summary.TotalTerminal != 3 || !summary.AccountingPass || summary.TerminalOutcomes[TerminalAlreadyResponded] != 1 || summary.TerminalOutcomes[TerminalAIMatch] != 1 || summary.TerminalOutcomes[TerminalVacancyLimit] != 1 {
		t.Fatalf("accounting summary: %+v", summary)
	}
}
