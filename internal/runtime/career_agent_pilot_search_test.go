package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
	"hh-ai-responder/internal/careeragent"
	attemptport "hh-ai-responder/internal/ports/applicationattempt"
)

func TestPilotSearchDoesNotSpendDeepBudgetOnKnownRespondedVacancies(t *testing.T) {
	var detailCalls, preflightCalls, aiCalls, hhWrites atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			hhWrites.Add(1)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch req.URL.Path {
		case "/search/vacancy":
			if got := req.URL.Query().Get("search_period"); got != "3" {
				t.Errorf("pilot recent search period=%q, want 3", got)
			}
			_, _ = io.WriteString(w, pilotSearchPayload(append(pilotIDs(1, 20), 21)))
		case "/applicant/vacancy_response":
			preflightCalls.Add(1)
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		case "/vacancy/21":
			detailCalls.Add(1)
			_, _ = io.WriteString(w, `{"redirectConfig":{},"vacancyView":{"name":"Python backend developer 21","description":"Python backend integration"}}`)
		default:
			t.Fatalf("unexpected HH endpoint: %s", req.URL.Path)
		}
	}))
	defer hhServer.Close()

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		aiCalls.Add(1)
		var payload ChatCompletionRequest
		_ = json.NewDecoder(req.Body).Decode(&payload)
		content := `{"score":90,"apply":true,"recommendation":"APPLY","reasons":["matches"],"missing":[],"hard_requirements":[]}`
		if payload.ResponseFormat == nil {
			content = "Здравствуйте! Мой опыт Python соответствует задачам вакансии."
		}
		_, _ = io.WriteString(w, aiCompletionResponse(content))
	}))
	defer aiServer.Close()

	dir := t.TempDir()
	respondedPath := filepath.Join(dir, "already-responded.json")
	ids := pilotIDs(1, 20)
	raw, _ := json.Marshal(alreadyRespondedStateFile{VacancyIDs: ids})
	if err := os.WriteFile(respondedPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	responder := pilotSearchTestResponder(t, hhServer, aiServer, dir)
	responder.alreadyRespondedStatePath = respondedPath
	preview, err := responder.findFirstCareerAgentPilotCandidate(21, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != pilotReadyStatus || preview.Artifact.VacancyID != 21 {
		t.Fatalf("candidate after local responded prefix was not selected: status=%s artifact=%+v", preview.Status, preview.Artifact)
	}
	stats := preview.SearchStats
	if stats.Scanned != 21 || stats.KnownRespondedSkipped != 20 || stats.FreshAlreadyRespondedSkipped != 0 || stats.UnrespondedFound != 1 || stats.UnrespondedEvaluated != 1 || stats.DetailReads != 1 || stats.AIEvaluations != 1 {
		t.Fatalf("unexpected pilot counters: %+v", stats)
	}
	if detailCalls.Load() != 1 || preflightCalls.Load() != 1 || aiCalls.Load() != 2 || hhWrites.Load() != 0 {
		t.Fatalf("read/write counters detail=%d preflight=%d ai=%d writes=%d", detailCalls.Load(), preflightCalls.Load(), aiCalls.Load(), hhWrites.Load())
	}
}

func TestPilotSearchFreshAlreadyRespondedSkipsDetailAIAndNonce(t *testing.T) {
	var detailCalls, preflightCalls, aiCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected HH write: %s %s", req.Method, req.URL.Path)
		}
		switch req.URL.Path {
		case "/search/vacancy":
			_, _ = io.WriteString(w, pilotSearchPayload([]int{301}))
		case "/applicant/vacancy_response":
			preflightCalls.Add(1)
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":true,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
		case "/vacancy/301":
			detailCalls.Add(1)
			t.Fatalf("fresh responded vacancy reached detail")
		default:
			t.Fatalf("unexpected HH endpoint: %s", req.URL.Path)
		}
	}))
	defer hhServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	preview, err := pilotSearchTestResponder(t, hhServer, aiServer, t.TempDir()).findFirstCareerAgentPilotCandidate(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Status != "BLOCKED" || preview.SearchStats.FreshAlreadyRespondedSkipped != 1 || preview.SearchStats.DetailReads != 0 || preview.SearchStats.AIEvaluations != 0 || preview.Artifact.Nonce != "" {
		t.Fatalf("fresh responded vacancy was not cheaply skipped: %+v", preview)
	}
	if detailCalls.Load() != 0 || preflightCalls.Load() != 1 || aiCalls.Load() != 0 {
		t.Fatalf("fresh responded work counters detail=%d preflight=%d ai=%d", detailCalls.Load(), preflightCalls.Load(), aiCalls.Load())
	}
}

func TestPilotKnownRespondedVacanciesUseApplicationAndNegotiationHistory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	application := JobApplication{ID: "app-1", VacancyID: 401, Source: ApplicationSourceHH, Status: ApplicationApplied, CreatedAt: now, UpdatedAt: now}
	applicationStore := NewApplicationStore(filepath.Join(dir, JobApplicationsFilename))
	if _, err := applicationStore.CreateApplication(application); err != nil {
		t.Fatal(err)
	}
	if err := applicationStore.Save(); err != nil {
		t.Fatal(err)
	}
	conversation := EmployerConversation{ID: "conversation-1", VacancyID: 402, HHConversationID: "hh-topic-402", Status: ConversationApplied, CreatedAt: now, UpdatedAt: now}
	conversationStore := NewConversationStore(filepath.Join(dir, EmployerConversationsFilename))
	if _, err := conversationStore.UpsertConversation(conversation); err != nil {
		t.Fatal(err)
	}
	if err := conversationStore.Save(); err != nil {
		t.Fatal(err)
	}
	responder := &HHAIResponder{candidateProfilePath: filepath.Join(dir, "candidate_profile.json"), alreadyResponded: map[int]struct{}{403: {}}}
	known := responder.pilotKnownRespondedVacancies(context.Background())
	for _, id := range []int{401, 402, 403} {
		if _, ok := known[id]; !ok {
			t.Fatalf("vacancy %d was not included in known responded set: %v", id, known)
		}
	}
}

func TestPilotSearchPeriodsAndBounds(t *testing.T) {
	if got := pilotSearchPeriods(7); !sameInts(got, []int{3, 7}) {
		t.Fatalf("search periods=%v, want [3 7]", got)
	}
	if got := pilotSearchPeriods(2); !sameInts(got, []int{2}) {
		t.Fatalf("search periods=%v, want [2]", got)
	}
	if got := pilotSearchPeriods(0); !sameInts(got, []int{3, 7}) {
		t.Fatalf("default search periods=%v, want [3 7]", got)
	}
	if _, err := (&HHAIResponder{}).findFirstCareerAgentPilotCandidate(0, 1); err == nil {
		t.Fatal("zero scan bound was accepted")
	}
	if _, err := (&HHAIResponder{}).findFirstCareerAgentPilotCandidate(100, 21); err == nil {
		t.Fatal("deep candidate bound above 20 was accepted")
	}
}

func TestPilotSearchSkipsUnresolvedAttemptBeforeProviderPreflight(t *testing.T) {
	var preflightCalls, detailCalls, aiCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected HH write: %s", req.Method)
		}
		switch req.URL.Path {
		case "/search/vacancy":
			_, _ = io.WriteString(w, pilotSearchPayload([]int{501}))
		case "/applicant/vacancy_response":
			preflightCalls.Add(1)
			t.Fatalf("unresolved attempt reached fresh provider preflight")
		case "/vacancy/501":
			detailCalls.Add(1)
			t.Fatalf("unresolved attempt reached detail")
		default:
			t.Fatalf("unexpected HH endpoint: %s", req.URL.Path)
		}
	}))
	defer hhServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		aiCalls.Add(1)
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":90,"apply":true,"reasons":[],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()

	responder := pilotSearchTestResponder(t, hhServer, aiServer, t.TempDir())
	responder.applicationAttempts = pilotBlockingAttemptStore{}
	preview, err := responder.findFirstCareerAgentPilotCandidate(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SearchStats.UnresolvedAttemptSkipped != 1 || preview.SearchStats.DetailReads != 0 || preview.SearchStats.AIEvaluations != 0 {
		t.Fatalf("unresolved attempt was not skipped: %+v", preview.SearchStats)
	}
	if preflightCalls.Load() != 0 || detailCalls.Load() != 0 || aiCalls.Load() != 0 {
		t.Fatalf("unresolved attempt performed provider work preflight=%d detail=%d ai=%d", preflightCalls.Load(), detailCalls.Load(), aiCalls.Load())
	}
}

func TestPilotSearchEnforcesScanAndDeepCandidateLimits(t *testing.T) {
	var preflightCalls atomic.Int32
	hhServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/search/vacancy" {
			_, _ = io.WriteString(w, pilotSearchPayload([]int{601, 602, 603}))
			return
		}
		if req.URL.Path == "/applicant/vacancy_response" {
			preflightCalls.Add(1)
			_, _ = io.WriteString(w, `{"redirectConfig":{"archived":false,"alreadyResponded":false,"testPresent":false,"responseLetterRequired":false,"canApply":true}}`)
			return
		}
		if req.URL.Path == "/vacancy/601" {
			_, _ = io.WriteString(w, `{"redirectConfig":{},"vacancyView":{"name":"Python backend developer","description":"Python backend integration"}}`)
			return
		}
		t.Fatalf("unexpected HH endpoint: %s", req.URL.Path)
	}))
	defer hhServer.Close()
	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.WriteString(w, aiCompletionResponse(`{"score":40,"apply":false,"reasons":[],"missing":[],"hard_requirements":[]}`))
	}))
	defer aiServer.Close()
	responder := pilotSearchTestResponder(t, hhServer, aiServer, t.TempDir())
	preview, err := responder.findFirstCareerAgentPilotCandidate(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SearchStats.Scanned != 2 || preview.SearchStats.UnrespondedFound != 2 || preview.SearchStats.UnrespondedEvaluated != 1 || preview.SearchStats.DetailReads != 1 || preflightCalls.Load() != 2 {
		t.Fatalf("scan/deep limits were not enforced: stats=%+v preflight=%d", preview.SearchStats, preflightCalls.Load())
	}
}

func pilotSearchTestResponder(t *testing.T, hhServer, aiServer *httptest.Server, dir string) *HHAIResponder {
	t.Helper()
	ctx := context.Background()
	baseURL := mustURL(t, hhServer.URL)
	profiles, _, err := buildVacancySearchProfiles([]string{hhServer.URL + "/search/vacancy?text=python"})
	if err != nil {
		t.Fatal(err)
	}
	return &HHAIResponder{
		ctx: ctx, baseURL: baseURL, requester: NewHHRequester(ctx, hhServer.Client(), 0), ai: NewAIClient(ctx, aiServer.URL, "test-model", "", time.Second, time.Second, 1), searchProfiles: profiles,
		resumes:              []ResumeItem{{Hash: "resume-hash", Title: "Python backend developer", Skills: "Python"}},
		careerAgentResumes:   []careeragent.ResumeProfile{{ID: "resume-1", Hash: "resume-hash", Title: "Python backend developer", Skills: []string{"Python"}, Enabled: true}},
		resumeFactsByHash:    map[string]ResumeFacts{"resume-hash": {ExperienceText: "Python backend"}},
		candidateProfilePath: dir + "/candidate_profile.json", dryRun: true, minMatchScore: 65,
	}
}

type pilotBlockingAttemptStore struct{}

func (pilotBlockingAttemptStore) Reserve(context.Context, domain.Attempt) (attemptport.ReserveResult, error) {
	return attemptport.ReserveResult{}, nil
}

func (pilotBlockingAttemptStore) RecordOutcome(context.Context, string, domain.State, time.Time, int, string) error {
	return nil
}

func (pilotBlockingAttemptStore) Get(context.Context, string) (domain.Attempt, error) {
	return domain.Attempt{}, domain.ErrAttemptNotFound
}

func (pilotBlockingAttemptStore) FindBlocking(_ context.Context, vacancyID int) (domain.Attempt, error) {
	now := time.Now().UTC()
	return domain.Attempt{AttemptID: "attempt-501", VacancyID: vacancyID, ResumeID: "resume-hash", State: domain.StateDeliveryUncertain, CreatedAt: now, UpdatedAt: now}, nil
}

func pilotSearchPayload(ids []int) string {
	var builder strings.Builder
	builder.WriteString(`prefix,"vacancies":[`)
	for index, id := range ids {
		if index > 0 {
			builder.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&builder, `{"vacancyId":%d,"name":"Python backend developer %d","links":{"desktop":"/vacancy/%d"}}`, id, id, id)
	}
	builder.WriteString(`]}`)
	return builder.String()
}

func pilotIDs(first, last int) []int {
	ids := make([]int, 0, last-first+1)
	for id := first; id <= last; id++ {
		ids = append(ids, id)
	}
	return ids
}

func sameInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
