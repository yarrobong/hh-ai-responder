package vacancyranking

import (
	"context"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
	"hh-ai-responder/internal/vacancyreview"
)

type queueReviewStore struct {
	states    map[int]vacancyreview.ReviewState
	freshness map[int]vacancy.Freshness
}

func (s queueReviewStore) GetFreshness(_ context.Context, id int) (vacancy.Freshness, error) {
	return s.freshness[id], nil
}
func (s queueReviewStore) ListFreshness(_ context.Context, ids []int) (map[int]vacancy.Freshness, error) {
	result := map[int]vacancy.Freshness{}
	for _, id := range ids {
		if value, ok := s.freshness[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (s queueReviewStore) GetReviewState(_ context.Context, id int) (vacancyreview.ReviewState, error) {
	value, ok := s.states[id]
	if !ok {
		return vacancyreview.ReviewState{}, vacancyreview.ErrReviewStateNotFound
	}
	return value, nil
}
func (s queueReviewStore) ListReviewStates(_ context.Context, ids []int) (map[int]vacancyreview.ReviewState, error) {
	result := map[int]vacancyreview.ReviewState{}
	for _, id := range ids {
		if value, ok := s.states[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (s queueReviewStore) RecordReviewAction(context.Context, vacancyreview.Action) error { return nil }
func (s queueReviewStore) ListReviewEvents(context.Context, int) ([]vacancyreview.Event, error) {
	return nil, nil
}

type queueApplicationReader struct{ values []application.JobApplication }

func (r queueApplicationReader) List(context.Context) ([]application.JobApplication, error) {
	return append([]application.JobApplication(nil), r.values...), nil
}
func (r queueApplicationReader) Get(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, nil
}
func (r queueApplicationReader) GetByExternalID(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, nil
}
func (r queueApplicationReader) Timeline(context.Context, string) ([]application.Event, error) {
	return nil, nil
}

func TestQueueProjectionUsesCanonicalComparatorAndBoundedPages(t *testing.T) {
	values := []vacancy.Vacancy{}
	for id := 1; id <= 5; id++ {
		value := baseVacancy()
		value.ID, value.Title, value.Name = id, "Python backend "+itoa(id), "Python backend "+itoa(id)
		values = append(values, value)
	}
	values[1].WorkExperience = "between1And3"
	store := queueReviewStore{
		states: map[int]vacancyreview.ReviewState{
			4: {VacancyID: 4, State: vacancyreview.StateDismissed, DecisionMaterialFingerprint: "old", FingerprintVersion: vacancy.FingerprintVersion},
			5: {VacancyID: 5, State: vacancyreview.StateInteresting},
		},
		freshness: map[int]vacancy.Freshness{4: {VacancyID: 4, MaterialFingerprint: "new", FingerprintVersion: vacancy.FingerprintVersion}},
	}
	model := ReadModel{
		Vacancies:    &countingVacancyReader{values: values},
		Candidate:    &countingCandidateReader{value: rankingCandidate()},
		Applications: queueApplicationReader{values: []application.JobApplication{{ID: "app", VacancyID: 3}}},
		Reviews:      store,
		Now:          func() time.Time { return rankingAt },
	}
	queue := QueueService{ReadModel: model, DefaultPageSize: 2, MaxPageSize: 10, Now: func() time.Time { return rankingAt }}
	first, err := queue.List(context.Background(), QueueFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || !first.HasMore || first.NextOffset == nil {
		t.Fatalf("first page=%+v", first)
	}
	second, err := queue.List(context.Background(), QueueFilter{Limit: 2, Offset: *first.NextOffset})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.VacancyID] {
			t.Fatalf("duplicate across pages: %d", item.VacancyID)
		}
		seen[item.VacancyID] = true
	}
	if first.Counts.ApplicationLinked != 1 || first.Counts.ClosedExcluded == 0 || first.Counts.WorthAnotherLook < 2 {
		t.Fatalf("counts=%+v", first.Counts)
	}
	if item, err := queue.Item(context.Background(), 1); err != nil || item.FreshnessKnown || item.FreshnessLabel != "freshness unknown" {
		t.Fatalf("unknown freshness item=%+v err=%v", item, err)
	}
	if got := SectionFor(modelResult(t, model, 4)); got != SectionWorthAnotherLook {
		t.Fatalf("changed dismissal section=%s", got)
	}
}

func modelResult(t *testing.T, model ReadModel, id int) Result {
	t.Helper()
	value, err := model.EvaluateVacancy(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProjectItemBoundsReasons(t *testing.T) {
	value := Result{Vacancy: vacancy.Vacancy{ID: 7, Name: "Queue"}, Eligibility: EligibilityReviewRequired, FitBand: FitCompatible, Confidence: ConfidenceLow, AnalysisState: AnalysisPartial, Rankable: true, ReviewState: vacancyreview.StateUnseen}
	for i := 0; i < 6; i++ {
		value.PositiveReasons = append(value.PositiveReasons, Reason{Code: "p"})
		value.Concerns = append(value.Concerns, Reason{Code: "c"})
		value.Unknowns = append(value.Unknowns, Reason{Code: "u"})
	}
	item := ProjectItem(value, SectionToReview, rankingAt)
	if len(item.PositiveReasons) != 3 || len(item.Concerns) != 2 || len(item.Unknowns) != 3 || item.UnknownCount != 6 {
		t.Fatalf("bounded item=%+v", item)
	}
}

var _ ports.ApplicationReader = queueApplicationReader{}
var _ ports.VacancyFreshnessReader = queueReviewStore{}
var _ ports.CandidateReader = (*countingCandidateReader)(nil)
