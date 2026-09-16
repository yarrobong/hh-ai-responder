package runtime

import (
	"context"
	"net/http/httptest"
	"os"
	"sort"
	"testing"
	"time"

	"hh-ai-responder/internal/candidate"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyranking"
	"hh-ai-responder/internal/vacancyreview"
)

type dashboardQueueCandidate struct{ value candidate.Candidate }

func (r dashboardQueueCandidate) CurrentCandidate(context.Context) (candidate.Candidate, error) {
	return r.value, nil
}

type dashboardQueueReviews struct {
	states    map[int]vacancyreview.ReviewState
	freshness map[int]vacancy.Freshness
	events    map[int][]vacancyreview.Event
}

func (r *dashboardQueueReviews) GetFreshness(_ context.Context, id int) (vacancy.Freshness, error) {
	return r.freshness[id], nil
}
func (r *dashboardQueueReviews) ListFreshness(_ context.Context, ids []int) (map[int]vacancy.Freshness, error) {
	result := map[int]vacancy.Freshness{}
	for _, id := range ids {
		if value, ok := r.freshness[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (r *dashboardQueueReviews) GetReviewState(_ context.Context, id int) (vacancyreview.ReviewState, error) {
	value, ok := r.states[id]
	if !ok {
		return vacancyreview.ReviewState{}, vacancyreview.ErrReviewStateNotFound
	}
	return value, nil
}
func (r *dashboardQueueReviews) ListReviewStates(_ context.Context, ids []int) (map[int]vacancyreview.ReviewState, error) {
	result := map[int]vacancyreview.ReviewState{}
	for _, id := range ids {
		if value, ok := r.states[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (r *dashboardQueueReviews) RecordReviewAction(_ context.Context, action vacancyreview.Action) error {
	r.states[action.VacancyID] = vacancyreview.ReviewState{VacancyID: action.VacancyID, State: action.State, StateChangedAt: action.OccurredAt, UpdatedAt: action.OccurredAt}
	r.events[action.VacancyID] = append(r.events[action.VacancyID], vacancyreview.Event{VacancyID: action.VacancyID, Type: action.State, OccurredAt: action.OccurredAt, Reason: action.Reason})
	return nil
}
func (r *dashboardQueueReviews) ListReviewEvents(_ context.Context, id int) ([]vacancyreview.Event, error) {
	return append([]vacancyreview.Event(nil), r.events[id]...), nil
}

func TestRankedQueueAPIAndReviewAction(t *testing.T) {
	s := dashboardTestServer(t)
	_, err := s.Vacancies.Create(Vacancy{ID: 101, Name: "Python integration", Title: "Python integration", Description: "Python API support", WorkFormat: "remote"})
	if err != nil {
		t.Fatal(err)
	}
	reviews := &dashboardQueueReviews{states: map[int]vacancyreview.ReviewState{}, freshness: map[int]vacancy.Freshness{}, events: map[int][]vacancyreview.Event{}}
	s.VacancyReviews = reviews
	s.RankedQueue = &vacancyranking.QueueService{ReadModel: vacancyranking.ReadModel{
		Vacancies:    NewJSONVacancyRepository(s.Vacancies),
		Candidate:    dashboardQueueCandidate{value: candidate.Candidate{ID: "candidate-local"}},
		Applications: NewJSONApplicationRepository(s.Applications),
		Reviews:      reviews,
		Now:          func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) },
	}, DefaultPageSize: 25, MaxPageSize: 100}
	page := dashboardDecode[vacancyranking.QueuePage](t, dashboardRequest(s, "GET", "/api/vacancies/ranked?limit=1", ""))
	if len(page.Items) != 1 || page.Items[0].VacancyID != 101 || page.Items[0].FreshnessKnown || page.Items[0].FreshnessLabel != "freshness unknown" {
		t.Fatalf("queue page=%+v", page)
	}
	response := dashboardRequest(s, "POST", "/api/vacancies/101/review/interesting", `{}`)
	if response.Code != 200 {
		t.Fatalf("review action=%d %s", response.Code, response.Body.String())
	}
	updated := dashboardDecode[map[string]any](t, response)
	if updated["review_state"] != string(vacancyreview.StateInteresting) {
		t.Fatalf("review response=%+v", updated)
	}
	page = dashboardDecode[vacancyranking.QueuePage](t, dashboardRequest(s, "GET", "/api/vacancies/ranked?section=worth_another_look", ""))
	if len(page.Items) != 1 || page.Items[0].ReviewState != vacancyreview.StateInteresting {
		t.Fatalf("interesting queue=%+v", page)
	}
	if len(reviews.events[101]) != 1 {
		t.Fatalf("review event count=%d", len(reviews.events[101]))
	}
	if response := dashboardRequest(s, "POST", "/api/vacancies/101/review/invalid", `{}`); response.Code != 404 {
		t.Fatalf("invalid review route=%d", response.Code)
	}
	if response := dashboardRequest(s, "GET", "/api/vacancies/ranked?limit=101", ""); response.Code != 400 {
		t.Fatalf("invalid queue filter status=%d", response.Code)
	}
}

func TestPostgresRankedQueueEndpointBenchmark(t *testing.T) {
	if os.Getenv("P1_3_QUEUE_API_BENCHMARK") != "1" {
		t.Skip("P1_3_QUEUE_API_BENCHMARK is not enabled")
	}
	dsn := os.Getenv("POSTGRES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DATABASE_URL is not configured")
	}
	dashboard, err := loadDashboard(context.Background(), t.TempDir(), Config{StorageBackend: storageBackendPostgres, DatabaseURL: dsn, CandidateID: "candidate-local", AIBaseURL: "http://127.0.0.1:1", AIModel: "fixture", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.CareerClose != nil {
		defer dashboard.CareerClose()
	}
	if dashboard.CandidateClose != nil {
		defer dashboard.CandidateClose()
	}
	durations := make([]time.Duration, 0, 10)
	bytes := 0
	for i := 0; i < 10; i++ {
		started := time.Now()
		request := httptest.NewRequest("GET", "http://127.0.0.1/api/vacancies/ranked?limit=25", nil)
		response := httptest.NewRecorder()
		dashboard.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("request %d status=%d body=%s", i, response.Code, response.Body.String())
		}
		durations = append(durations, time.Since(started))
		bytes = response.Body.Len()
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("ranked API requests=10 min=%s median=%s p90=%s max=%s payload_bytes=%d", durations[0], durations[4], durations[8], durations[9], bytes)
}

var _ ports.CandidateReader = dashboardQueueCandidate{}
