package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	hhapi "hh-ai-responder/internal/adapters/hh/api"
	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/hhread"
	hhreadport "hh-ai-responder/internal/ports/hhread"
)

const (
	shadowMaxPagesPerProfile = 3
	shadowMaxPagesPerRun     = 48
	shadowMaxVacancies       = 100
)

type shadowHTTPCall struct {
	Method string
	Path   string
}

type shadowHTTPRecorder struct {
	mu    sync.Mutex
	calls []shadowHTTPCall
}

func (r *shadowHTTPRecorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, shadowHTTPCall{Method: req.Method, Path: req.URL.EscapedPath()})
}

func (r *shadowHTTPRecorder) snapshot() []shadowHTTPCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]shadowHTTPCall(nil), r.calls...)
}

type shadowTokenStore struct {
	tokens hhapi.OAuthTokens
}

func (s *shadowTokenStore) Load(context.Context) (hhapi.OAuthTokens, error) {
	return s.tokens, nil
}

func (*shadowTokenStore) Save(context.Context, hhapi.OAuthTokens) error { return nil }

func (*shadowTokenStore) Delete(context.Context) error { return nil }

type shadowBrowserSource struct {
	calls int
}

func (s *shadowBrowserSource) ReadVacancies(context.Context, string) (hhread.VacancyPage, error) {
	s.calls++
	return hhread.VacancyPage{}, errors.New("browser source must not be used by explicit API shadow")
}

func (s *shadowBrowserSource) ReadApplications(context.Context, string) (hhread.ApplicationPage, error) {
	s.calls++
	return hhread.ApplicationPage{}, errors.New("browser source must not be used by explicit API shadow")
}

func (s *shadowBrowserSource) ReadConversations(context.Context, string) (hhread.ConversationPage, error) {
	s.calls++
	return hhread.ConversationPage{}, errors.New("browser source must not be used by explicit API shadow")
}

type shadowParityStatus string

const (
	shadowEqual      shadowParityStatus = "equal"
	shadowDifferent  shadowParityStatus = "different"
	shadowUnknown    shadowParityStatus = "unknown/unproven"
	shadowAIUnproven                    = "AI was not invoked by the read-only shadow"
)

type shadowFinding struct {
	Name        string
	Status      shadowParityStatus
	Explanation string
}

type shadowFixtureResult struct {
	NonGETRequests  int
	MutationPaths   []string
	UnexpectedPaths []string
	BrowserCalls    int
	RequestPaths    []string
	RawVacancyCount int
	DistinctIDs     []int
	Findings        []shadowFinding
}

func TestAPIShadowReadsAreZeroWriteAndBounded(t *testing.T) {
	result, err := runAPIShadowFixture(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.NonGETRequests != 0 || len(result.MutationPaths) != 0 || len(result.UnexpectedPaths) != 0 {
		t.Fatalf("shadow issued unsafe requests: non_get=%d mutation=%v unexpected=%v", result.NonGETRequests, result.MutationPaths, result.UnexpectedPaths)
	}
	if result.BrowserCalls != 0 {
		t.Fatalf("explicit API shadow used browser fallback %d times", result.BrowserCalls)
	}
	if result.RawVacancyCount > shadowMaxVacancies || len(result.DistinctIDs) > shadowMaxVacancies {
		t.Fatalf("shadow exceeded vacancy bound: raw=%d distinct=%d", result.RawVacancyCount, len(result.DistinctIDs))
	}
	if len(result.RequestPaths) == 0 {
		t.Fatal("shadow made no synthetic read requests")
	}
	for _, finding := range result.Findings {
		if finding.Status != shadowEqual && finding.Status != shadowUnknown {
			t.Fatalf("synthetic parity unexpectedly differs: %+v", finding)
		}
	}
}

func TestAPIShadowUnsupportedCapabilitiesFailClosed(t *testing.T) {
	setShadowSafetyEnvironment(t)
	recorder := &shadowHTTPRecorder{}
	server := httptest.NewServer(shadowFixtureHandler(recorder))
	defer server.Close()
	client := newShadowAPIClient(t, server.URL)

	before := len(recorder.snapshot())
	applications, err := client.ReadApplications(context.Background(), "")
	assertShadowCapabilityError(t, err, "applications")
	if len(applications.Items) != 0 || applications.NextCursor != "" {
		t.Fatalf("unsupported applications returned success data: %+v", applications)
	}
	conversations, err := client.ReadConversations(context.Background(), "")
	assertShadowCapabilityError(t, err, "conversations")
	if len(conversations.Items) != 0 || conversations.NextCursor != "" {
		t.Fatalf("unsupported conversations returned success data: %+v", conversations)
	}
	if after := len(recorder.snapshot()); after != before {
		t.Fatalf("unsupported capabilities made an HTTP request: before=%d after=%d", before, after)
	}

	missingRelationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/vacancies/43" {
			http.Error(w, "unexpected synthetic request", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"id":43,"name":"Integration specialist"}`)
	}))
	defer missingRelationServer.Close()
	missingRelationClient := newShadowAPIClient(t, missingRelationServer.URL)
	detail, err := missingRelationClient.ReadVacancyDetailRequiringRelation(context.Background(), 43)
	assertShadowCapabilityError(t, err, "duplicate-state")
	if detail.ID != 0 || detail.AlreadyResponded != nil {
		t.Fatalf("duplicate-state uncertainty became successful detail: %+v", detail)
	}
}

func TestAPIShadowExplicitAPINeverFallsBackToBrowser(t *testing.T) {
	setShadowSafetyEnvironment(t)
	var browserCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/me" {
			t.Fatalf("explicit API probe used unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"oauth_error":"invalid_token"}`)
	}))
	defer server.Close()
	api := newShadowAPIClient(t, server.URL)
	_, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAPI,
		API:     api,
		Browser: &shadowBrowserSource{},
		BrowserDoctor: func(context.Context) (string, error) {
			browserCalls++
			return browserAuthOK, nil
		},
	})
	if err == nil || meta.Selected != "" {
		t.Fatalf("explicit API probe unexpectedly succeeded or selected a fallback: meta=%+v err=%v", meta, err)
	}
	if browserCalls != 0 {
		t.Fatalf("explicit API mode called browser doctor %d times", browserCalls)
	}
}

func TestAPIShadowBoundedParityClassifiesSafeFields(t *testing.T) {
	apiRecord := hhread.VacancyRecord{ID: 42, ExternalID: "42", Title: "Integration specialist", Company: "Fixture employer", Salary: "100000–120000", Location: "Yekaterinburg", Experience: "between1And3", Schedule: "Flexible", EmploymentType: "Full time", WorkFormat: "hybrid", ProfessionalRoles: []string{"Developer"}}
	browserRecord := apiRecord
	browserRecord.Salary = "90000–120000"
	browserRecord.AlreadyResponded = nil
	browserRecord.Description = "private-looking but intentionally excluded from parity"

	findings := compareShadowVacancy(apiRecord, browserRecord)
	if finding := findShadowFinding(findings, "vacancy.salary"); finding.Status != shadowDifferent || finding.Explanation == "" {
		t.Fatalf("salary difference was not explained: %+v", finding)
	}
	if finding := findShadowFinding(findings, "vacancy.response_state"); finding.Status != shadowUnknown {
		t.Fatalf("unknown response state was overclaimed: %+v", finding)
	}
	for _, finding := range findings {
		if strings.Contains(finding.Name, "description") || strings.Contains(finding.Name, "prompt") || strings.Contains(finding.Explanation, "private-looking") {
			t.Fatalf("bounded comparator exposed an unsafe field: %+v", finding)
		}
	}
}

func TestAPIShadowPipelineComparisonKeepsAIUnproven(t *testing.T) {
	setShadowSafetyEnvironment(t)
	resume := candidate.ResumeItem{Id: 42, Hash: "resume-hash", Title: "Python backend", Skills: "Python, Django", Area: "Yekaterinburg", Salary: "100000"}
	plannerAPI := shadowPlannerSummary([]candidate.ResumeItem{resume})
	plannerBrowser := shadowPlannerSummary([]candidate.ResumeItem{resume})
	if !equalShadowPlanner(plannerAPI, plannerBrowser) {
		t.Fatalf("planner profile/query intent changed across identical normalized inputs: api=%+v browser=%+v", plannerAPI, plannerBrowser)
	}

	vacancy := hhread.VacancyRecord{ID: 42, Title: "Python backend", Description: "Backend integrations", KeySkills: []string{"Python", "Django"}, ProfessionalRoles: []string{"Developer"}, Experience: "between1And3", Location: "Yekaterinburg", WorkFormat: "remote"}
	routeAPI := shadowRouterSummary(vacancy, []candidate.ResumeItem{resume})
	routeBrowser := shadowRouterSummary(vacancy, []candidate.ResumeItem{resume})
	if routeAPI != routeBrowser {
		t.Fatalf("router outcome changed across identical normalized inputs: api=%+v browser=%+v", routeAPI, routeBrowser)
	}

	findings := []shadowFinding{{Name: "planner.profiles", Status: shadowEqual}, {Name: "query.intent", Status: shadowEqual}, {Name: "router.outcome", Status: shadowEqual}, {Name: "ai.rows", Status: shadowUnknown, Explanation: shadowAIUnproven}}
	if finding := findShadowFinding(findings, "ai.rows"); finding.Status != shadowUnknown || finding.Explanation != shadowAIUnproven {
		t.Fatalf("AI parity was silently overclaimed: %+v", finding)
	}
}

func runAPIShadowFixture(t *testing.T) (*shadowFixtureResult, error) {
	t.Helper()
	setShadowSafetyEnvironment(t)
	recorder := &shadowHTTPRecorder{}
	server := httptest.NewServer(shadowFixtureHandler(recorder))
	defer server.Close()
	api := newShadowAPIClient(t, server.URL)
	browser := &shadowBrowserSource{}
	selected, meta, err := selectHHReadSource(context.Background(), TransportOptions{
		Mode:    transportAPI,
		API:     api,
		Browser: browser,
		BrowserDoctor: func(context.Context) (string, error) {
			return browserAuthOK, nil
		},
	})
	if err != nil {
		return nil, err
	}
	if meta.Selected != transportAPI || selected != api {
		return nil, errors.New("explicit API shadow did not select the API source")
	}

	resumeSource, ok := selected.(hhreadport.ResumeReadSource)
	if !ok {
		return nil, errors.New("selected API source has no resume read capability")
	}
	resumes, err := resumeSource.ReadResumes(context.Background())
	if err != nil {
		return nil, err
	}
	if len(resumes) == 0 {
		return nil, errors.New("synthetic API returned no resumes")
	}
	if _, err := resumeSource.ReadResume(context.Background(), resumes[0].ID); err != nil {
		return nil, err
	}
	vacancyPage, err := selected.ReadVacancies(context.Background(), "")
	if err != nil {
		return nil, err
	}
	if len(vacancyPage.Items) > shadowMaxVacancies {
		return nil, errors.New("synthetic API exceeded vacancy bound")
	}
	if _, err := selected.(hhreadport.VacancyDetailSource).ReadVacancyDetail(context.Background(), 42); err != nil {
		return nil, err
	}

	browserResume := candidate.ResumeItem{Id: 42, Hash: "resume-hash", Title: "Python backend", Skills: "Python, Django", Area: "Yekaterinburg", Salary: "100000"}
	apiResume, err := apiResumeItem(resumes[0])
	if err != nil {
		return nil, err
	}
	browserVacancies := shadowBrowserVacancies()
	findings := compareShadowIDs(vacancyPage.Items, browserVacancies)
	findings = append(findings, compareShadowVacancy(vacancyPage.Items[0], browserVacancies[0])...)
	findings = append(findings, compareShadowPlannerAndRouter(apiResume, browserResume, vacancyPage.Items[0], browserVacancies[0])...)
	findings = append(findings, shadowFinding{Name: "ai.rows", Status: shadowUnknown, Explanation: shadowAIUnproven})

	calls := recorder.snapshot()
	result := &shadowFixtureResult{BrowserCalls: browser.calls, RawVacancyCount: len(vacancyPage.Items), Findings: findings}
	for _, call := range calls {
		result.RequestPaths = append(result.RequestPaths, call.Path)
		if call.Method != http.MethodGet {
			result.NonGETRequests++
		}
		if shadowMutationPath(call.Method, call.Path) {
			result.MutationPaths = append(result.MutationPaths, call.Method+" "+call.Path)
		}
		if !shadowReadPath(call.Path) {
			result.UnexpectedPaths = append(result.UnexpectedPaths, call.Path)
		}
	}
	result.DistinctIDs = distinctShadowIDs(vacancyPage.Items)
	sort.Strings(result.RequestPaths)
	return result, nil
}

func setShadowSafetyEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("HH_DRY_RUN", "true")
	t.Setenv("HH_WRITE_ENABLED", "false")
	t.Setenv("HH_MAX_SEARCH_PAGES_PER_PROFILE", strconv.Itoa(shadowMaxPagesPerProfile))
	t.Setenv("HH_MAX_SEARCH_PAGES_PER_RUN", strconv.Itoa(shadowMaxPagesPerRun))
	t.Setenv("HH_MAX_VACANCIES_PER_RUN", strconv.Itoa(shadowMaxVacancies))
	t.Setenv("STORAGE_BACKEND", "json")
	for key, want := range map[string]string{
		"HH_DRY_RUN":                      "true",
		"HH_WRITE_ENABLED":                "false",
		"HH_MAX_SEARCH_PAGES_PER_PROFILE": strconv.Itoa(shadowMaxPagesPerProfile),
		"HH_MAX_SEARCH_PAGES_PER_RUN":     strconv.Itoa(shadowMaxPagesPerRun),
		"HH_MAX_VACANCIES_PER_RUN":        strconv.Itoa(shadowMaxVacancies),
		"STORAGE_BACKEND":                 "json",
	} {
		if got := os.Getenv(key); got != want {
			t.Fatalf("unsafe shadow environment %s=%q, want %q", key, got, want)
		}
	}
}

func newShadowAPIClient(t *testing.T, serverURL string) *hhapi.APIHHClient {
	t.Helper()
	baseURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := hhapi.NewAPIHHClient(hhapi.APIClientOptions{
		BaseURL: baseURL, HTTPClient: http.DefaultClient,
		TokenStore: &shadowTokenStore{tokens: hhapi.OAuthTokens{
			AccessToken: "fixture-access", TokenType: "Bearer", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
		UserAgent:    "hh-ai-responder/reset-api-shadow-test",
		SearchParams: url.Values{"text": {"Python backend"}, "area": {"3"}, "search_period": {"7"}, "items_on_page": {"50"}},
		Now:          func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func shadowFixtureHandler(recorder *shadowHTTPRecorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recorder.add(r)
		if r.Method != http.MethodGet {
			http.Error(w, "synthetic shadow rejects non-GET", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/me":
			_, _ = io.WriteString(w, `{"id":"candidate-fixture","type":"applicant"}`)
		case "/resumes/mine":
			_, _ = io.WriteString(w, `{"items":[{"id":"42","hash":"resume-hash","title":"Python backend","skill_set":["Python","Django"],"area":{"name":"Yekaterinburg"},"salary":{"amount":100000,"currency":"RUR"},"total_experience":{"months":36}}]}`)
		case "/resumes/42":
			_, _ = io.WriteString(w, `{"id":"42","hash":"resume-hash","title":"Python backend","description":"Backend integrations","skill_set":["Python","Django"],"area":{"name":"Yekaterinburg"},"salary":{"amount":100000,"currency":"RUR"},"total_experience":{"months":36}}`)
		case "/vacancies":
			_, _ = io.WriteString(w, `{"items":[{"id":"42","name":"Python backend","description":"Backend integrations","employer":{"name":"Fixture employer"},"area":{"name":"Yekaterinburg"},"salary":{"from":100000,"to":120000,"currency":"RUR"},"professional_roles":[{"name":"Developer"}],"key_skills":["Python","Django"],"experience":{"id":"between1And3","name":"1-3 years"},"employment":{"name":"Full time"},"schedule":{"name":"Flexible"},"work_format":[{"name":"Из дома"},{"name":"На месте работодателя"}],"responses_count":4,"archived":false,"response_letter_required":true,"has_test":false,"relations":{"already_responded":false}},{"id":"43","name":"Technical support","employer":{"name":"Fixture employer"},"area":{"name":"Yekaterinburg"}}],"page":0,"pages":1}`)
		case "/vacancies/42":
			_, _ = io.WriteString(w, `{"id":"42","name":"Python backend","description":"Backend integrations","employer":{"name":"Fixture employer"},"area":{"name":"Yekaterinburg"},"salary":{"from":100000,"to":120000,"currency":"RUR"},"professional_roles":[{"name":"Developer"}],"key_skills":["Python","Django"],"experience":{"id":"between1And3","name":"1-3 years"},"employment":{"name":"Full time"},"schedule":{"name":"Flexible"},"work_format":[{"name":"Из дома"},{"name":"На месте работодателя"}],"responses_count":4,"archived":false,"response_letter_required":true,"has_test":false,"relations":{"already_responded":false}}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func shadowBrowserVacancies() []hhread.VacancyRecord {
	responded := false
	return []hhread.VacancyRecord{
		{ID: 42, ExternalID: "42", Title: "Python backend", Company: "Fixture employer", Description: "Backend integrations", KeySkills: []string{"Python", "Django"}, Salary: "100000–120000", Currency: "RUR", Location: "Yekaterinburg", Experience: "between1And3", EmploymentType: "Full time", Schedule: "Flexible", WorkFormat: "hybrid", ProfessionalRoles: []string{"Developer"}, TotalResponsesCount: 4, TotalResponsesCountKnown: true, ArchivedKnown: true, ResponseLetterRequired: true, ResponseLetterRequiredKnown: true, UserTestPresentKnown: true, ResponseURL: "https://hh.example/apply/42", AlreadyResponded: &responded},
		{ID: 43, ExternalID: "43", Title: "Technical support", Company: "Fixture employer", Location: "Yekaterinburg"},
	}
}

func compareShadowIDs(apiValues, browserValues []hhread.VacancyRecord) []shadowFinding {
	apiIDs := distinctShadowIDs(apiValues)
	browserIDs := distinctShadowIDs(browserValues)
	if equalIntSlices(apiIDs, browserIDs) {
		return []shadowFinding{{Name: "vacancy.raw_and_distinct_ids", Status: shadowEqual, Explanation: "bounded normalized ID sets match"}}
	}
	return []shadowFinding{{Name: "vacancy.raw_and_distinct_ids", Status: shadowDifferent, Explanation: "bounded normalized ID sets differ"}}
}

func compareShadowVacancy(apiValue, browserValue hhread.VacancyRecord) []shadowFinding {
	findings := []shadowFinding{
		compareShadowString("vacancy.title", apiValue.Title, browserValue.Title),
		compareShadowString("vacancy.company", apiValue.Company, browserValue.Company),
		compareShadowStringList("vacancy.professional_roles", apiValue.ProfessionalRoles, browserValue.ProfessionalRoles),
		compareShadowString("vacancy.experience", apiValue.Experience, browserValue.Experience),
		compareShadowString("vacancy.schedule", apiValue.Schedule, browserValue.Schedule),
		compareShadowString("vacancy.employment", apiValue.EmploymentType, browserValue.EmploymentType),
		compareShadowString("vacancy.work_format", apiValue.WorkFormat, browserValue.WorkFormat),
		compareShadowString("vacancy.location", apiValue.Location, browserValue.Location),
		compareShadowString("vacancy.salary", apiValue.Salary, browserValue.Salary),
		compareShadowResponseState(apiValue, browserValue),
	}
	return findings
}

func compareShadowString(name, apiValue, browserValue string) shadowFinding {
	apiValue, browserValue = strings.TrimSpace(apiValue), strings.TrimSpace(browserValue)
	if apiValue == browserValue {
		return shadowFinding{Name: name, Status: shadowEqual, Explanation: "bounded normalized values match"}
	}
	if apiValue == "" || browserValue == "" {
		return shadowFinding{Name: name, Status: shadowUnknown, Explanation: "one transport did not prove this optional normalized field"}
	}
	return shadowFinding{Name: name, Status: shadowDifferent, Explanation: "bounded normalized values differ"}
}

func compareShadowStringList(name string, apiValues, browserValues []string) shadowFinding {
	apiValues, browserValues = boundedSortedStrings(apiValues), boundedSortedStrings(browserValues)
	if equalStringSlices(apiValues, browserValues) {
		return shadowFinding{Name: name, Status: shadowEqual, Explanation: "bounded normalized lists match"}
	}
	if len(apiValues) == 0 || len(browserValues) == 0 {
		return shadowFinding{Name: name, Status: shadowUnknown, Explanation: "one transport did not prove this optional normalized list"}
	}
	return shadowFinding{Name: name, Status: shadowDifferent, Explanation: "bounded normalized lists differ"}
}

func compareShadowResponseState(apiValue, browserValue hhread.VacancyRecord) shadowFinding {
	if apiValue.AlreadyResponded == nil || browserValue.AlreadyResponded == nil {
		return shadowFinding{Name: "vacancy.response_state", Status: shadowUnknown, Explanation: "duplicate/application relation is unknown or unproven"}
	}
	if *apiValue.AlreadyResponded == *browserValue.AlreadyResponded {
		return shadowFinding{Name: "vacancy.response_state", Status: shadowEqual, Explanation: "safe response-state classification matches"}
	}
	return shadowFinding{Name: "vacancy.response_state", Status: shadowDifferent, Explanation: "safe response-state classifications differ"}
}

type shadowPlannerRow struct {
	ID    string
	Query string
	Role  careeragent.RoleFamily
	Type  careeragent.SearchProfileType
}

type shadowRouterRow struct {
	Status     string
	SelectedID string
	Reason     string
	Confidence string
	Score      int
}

func compareShadowPlannerAndRouter(apiResume, browserResume candidate.ResumeItem, apiValue, browserValue hhread.VacancyRecord) []shadowFinding {
	apiPlanner := shadowPlannerSummary([]candidate.ResumeItem{apiResume})
	browserPlanner := shadowPlannerSummary([]candidate.ResumeItem{browserResume})
	findings := []shadowFinding{}
	if equalShadowPlanner(apiPlanner, browserPlanner) {
		findings = append(findings, shadowFinding{Name: "planner.profiles_and_query_intent", Status: shadowEqual, Explanation: "planner output matches for bounded normalized resume inputs"})
	} else {
		findings = append(findings, shadowFinding{Name: "planner.profiles_and_query_intent", Status: shadowDifferent, Explanation: "planner profiles or query intent differ"})
	}
	apiRoute := shadowRouterSummary(apiValue, []candidate.ResumeItem{apiResume})
	browserRoute := shadowRouterSummary(browserValue, []candidate.ResumeItem{browserResume})
	if apiRoute == browserRoute {
		findings = append(findings, shadowFinding{Name: "router.outcome", Status: shadowEqual, Explanation: "router outcome matches for bounded normalized vacancy inputs"})
	} else {
		findings = append(findings, shadowFinding{Name: "router.outcome", Status: shadowDifferent, Explanation: "router status or safe score fields differ"})
	}
	return findings
}

func shadowPlannerSummary(values []candidate.ResumeItem) []shadowPlannerRow {
	profiles := careeragent.NormalizeResumes(values)
	planned := careeragent.PlanSearches(profiles, careeragent.CandidateSignals{Roles: []string{"Python backend"}, Skills: []string{"Python", "Django"}, PreferredLocation: "Yekaterinburg"}, careeragent.SearchConstraints{MaxProfiles: shadowMaxPagesPerProfile, SearchPeriodDays: 7})
	result := make([]shadowPlannerRow, 0, len(planned))
	for _, profile := range planned {
		result = append(result, shadowPlannerRow{ID: profile.ID, Query: profile.Query, Role: profile.RoleFamily, Type: profile.ProfileType})
	}
	return result
}

func shadowRouterSummary(value hhread.VacancyRecord, resumes []candidate.ResumeItem) shadowRouterRow {
	profiles := careeragent.NormalizeResumes(resumes)
	decision := careeragent.RouteResume(careeragent.VacancyInput{ID: value.ID, Title: value.Title, Description: value.Description, RequiredSkills: append([]string(nil), value.Requirements...), KeySkills: append([]string(nil), value.KeySkills...), ProfessionalRoles: append([]string(nil), value.ProfessionalRoles...), Experience: value.Experience, Employment: value.EmploymentType, Schedule: value.Schedule, Salary: value.Salary, Location: value.Location, WorkFormat: value.WorkFormat, DetailAvailable: true}, profiles)
	return shadowRouterRow{Status: decision.Status, SelectedID: decision.SelectedResumeID, Reason: decision.ReasonCode, Confidence: decision.Confidence, Score: decision.Score}
}

func equalShadowPlanner(left, right []shadowPlannerRow) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func findShadowFinding(findings []shadowFinding, name string) shadowFinding {
	for _, finding := range findings {
		if finding.Name == name {
			return finding
		}
	}
	return shadowFinding{Name: name, Status: shadowUnknown, Explanation: "finding was not produced"}
}

func assertShadowCapabilityError(t *testing.T, err error, capability string) {
	t.Helper()
	var capabilityErr *hhapi.CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != capability {
		t.Fatalf("error=%T %v, want typed %s capability error", err, err, capability)
	}
}

func shadowMutationPath(method, path string) bool {
	path = strings.ToLower(path)
	if method != http.MethodGet {
		return true
	}
	for _, token := range []string{"/applications", "/conversations", "/messages", "/negotiations", "/status", "/apply"} {
		if strings.Contains(path, token) {
			return true
		}
	}
	return false
}

func shadowReadPath(path string) bool {
	switch path {
	case "/me", "/resumes/mine", "/resumes/42", "/vacancies", "/vacancies/42":
		return true
	default:
		return false
	}
}

func distinctShadowIDs(values []hhread.VacancyRecord) []int {
	seen := map[int]struct{}{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value.ID]; ok {
			continue
		}
		seen[value.ID] = struct{}{}
		result = append(result, value.ID)
	}
	sort.Ints(result)
	return result
}

func boundedSortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	if len(result) > 16 {
		result = result[:16]
	}
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	sort.Strings(result)
	return result
}

func equalIntSlices(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
