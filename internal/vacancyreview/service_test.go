package vacancyreview

import (
	"context"
	"testing"
	"time"

	"hh-ai-responder/internal/application"
	"hh-ai-responder/internal/ports"
	"hh-ai-responder/internal/vacancy"
)

type memoryStore struct {
	freshness map[int]vacancy.Freshness
	states    map[int]ReviewState
	events    map[int][]Event
}

func (m *memoryStore) GetFreshness(_ context.Context, id int) (vacancy.Freshness, error) {
	return m.freshness[id], nil
}
func (m *memoryStore) ListFreshness(_ context.Context, ids []int) (map[int]vacancy.Freshness, error) {
	result := map[int]vacancy.Freshness{}
	for _, id := range ids {
		if value, ok := m.freshness[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (m *memoryStore) GetReviewState(_ context.Context, id int) (ReviewState, error) {
	value, ok := m.states[id]
	if !ok {
		return ReviewState{}, ErrReviewStateNotFound
	}
	return value, nil
}
func (m *memoryStore) ListReviewStates(_ context.Context, ids []int) (map[int]ReviewState, error) {
	result := map[int]ReviewState{}
	for _, id := range ids {
		if value, ok := m.states[id]; ok {
			result[id] = value
		}
	}
	return result, nil
}
func (m *memoryStore) RecordReviewAction(_ context.Context, action Action) error {
	current, ok := m.states[action.VacancyID]
	var previous *State
	if ok {
		previous = &current.State
	}
	if !CanTransition(previous, action.State) {
		return context.Canceled
	}
	fresh := m.freshness[action.VacancyID]
	if ok && current.State == action.State && current.DecisionSourceFingerprint == fresh.SourceFingerprint && current.DecisionMaterialFingerprint == fresh.MaterialFingerprint {
		return nil
	}
	m.events[action.VacancyID] = append(m.events[action.VacancyID], Event{VacancyID: action.VacancyID, Type: action.State, OccurredAt: action.OccurredAt, SourceFingerprint: fresh.SourceFingerprint, MaterialFingerprint: fresh.MaterialFingerprint, FingerprintVersion: fresh.FingerprintVersion, Reason: action.Reason, Source: action.Source})
	m.states[action.VacancyID] = ReviewState{VacancyID: action.VacancyID, State: action.State, StateChangedAt: action.OccurredAt, DecisionSourceFingerprint: fresh.SourceFingerprint, DecisionMaterialFingerprint: fresh.MaterialFingerprint, FingerprintVersion: fresh.FingerprintVersion, Reason: action.Reason, UpdatedAt: action.OccurredAt}
	return nil
}
func (m *memoryStore) ListReviewEvents(_ context.Context, id int) ([]Event, error) {
	return append([]Event{}, m.events[id]...), nil
}

var _ Store = (*memoryStore)(nil)

type memoryApplications struct{ values []application.JobApplication }

func (m memoryApplications) Get(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, application.ErrApplicationNotFound
}
func (m memoryApplications) GetByExternalID(context.Context, string) (application.JobApplication, error) {
	return application.JobApplication{}, application.ErrApplicationNotFound
}
func (m memoryApplications) List(context.Context) ([]application.JobApplication, error) {
	return m.values, nil
}
func (m memoryApplications) Timeline(context.Context, string) ([]application.Event, error) {
	return nil, nil
}

var _ ports.ApplicationReader = memoryApplications{}

func TestReviewLifecycleAndIdempotency(t *testing.T) {
	at := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{freshness: map[int]vacancy.Freshness{7: {VacancyID: 7, SourceFingerprint: "s1", MaterialFingerprint: "m1", FingerprintVersion: vacancy.FingerprintVersion}}, states: map[int]ReviewState{}, events: map[int][]Event{}}
	service := NewService(store)
	for _, action := range []State{StateSeen, StateInteresting, StatePrepared, StateApplied} {
		if err := service.Record(context.Background(), Action{VacancyID: 7, State: action, OccurredAt: at}); err != nil {
			t.Fatalf("record %s: %v", action, err)
		}
	}
	if err := service.MarkApplied(context.Background(), 7, at); err != nil {
		t.Fatal(err)
	}
	if len(store.events[7]) != 4 || store.states[7].State != StateApplied {
		t.Fatalf("state=%+v events=%d", store.states[7], len(store.events[7]))
	}
}

func TestDismissedMaterialUpdateRemainsDetectable(t *testing.T) {
	at := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{freshness: map[int]vacancy.Freshness{7: {VacancyID: 7, SourceFingerprint: "s1", MaterialFingerprint: "m1", FingerprintVersion: 1}}, states: map[int]ReviewState{}, events: map[int][]Event{}}
	service := NewService(store)
	if err := service.Dismiss(context.Background(), 7, "role mismatch", at); err != nil {
		t.Fatal(err)
	}
	store.freshness[7] = vacancy.Freshness{VacancyID: 7, SourceFingerprint: "s2", MaterialFingerprint: "m2", FingerprintVersion: 1}
	state, err := service.Effective(context.Background(), 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.ChangedSinceReview == nil || !*state.ChangedSinceReview || state.State != StateDismissed || len(store.events[7]) != 1 {
		t.Fatalf("effective=%+v events=%d", state, len(store.events[7]))
	}
}

func TestAlgorithmVersionMismatchDoesNotCreateFakeMaterialChange(t *testing.T) {
	at := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{freshness: map[int]vacancy.Freshness{7: {VacancyID: 7, SourceFingerprint: "v2-source", MaterialFingerprint: "v2-material", FingerprintVersion: vacancy.FingerprintVersion}}, states: map[int]ReviewState{7: {VacancyID: 7, State: StateDismissed, DecisionSourceFingerprint: "v1-source", DecisionMaterialFingerprint: "v1-material", FingerprintVersion: vacancy.LegacyFingerprintVersion}}, events: map[int][]Event{}}
	state, err := NewService(store).Effective(context.Background(), 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.ChangedSinceReview != nil || state.State != StateDismissed {
		t.Fatalf("version migration was treated as provider change: %+v", state)
	}
	if err := NewService(store).Dismiss(context.Background(), 7, "reconfirmed", at); err != nil {
		t.Fatal(err)
	}
	if got := store.states[7].FingerprintVersion; got != vacancy.FingerprintVersion {
		t.Fatalf("explicit post-migration decision stored version %d, want %d", got, vacancy.FingerprintVersion)
	}
}

func TestSameAlgorithmVersionStillDetectsMaterialChange(t *testing.T) {
	store := &memoryStore{freshness: map[int]vacancy.Freshness{7: {VacancyID: 7, MaterialFingerprint: "new", FingerprintVersion: vacancy.FingerprintVersion}}, states: map[int]ReviewState{7: {VacancyID: 7, State: StateDismissed, DecisionMaterialFingerprint: "old", FingerprintVersion: vacancy.FingerprintVersion}}, events: map[int][]Event{}}
	state, err := NewService(store).Effective(context.Background(), 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.ChangedSinceReview == nil || !*state.ChangedSinceReview {
		t.Fatalf("same-version material change was not detected: %+v", state)
	}
}

func TestApplicationLinkedOverridesStoredReviewWithoutInventingEvent(t *testing.T) {
	store := &memoryStore{freshness: map[int]vacancy.Freshness{}, states: map[int]ReviewState{7: {VacancyID: 7, State: StateDismissed}}, events: map[int][]Event{}}
	service := NewService(store)
	state, err := service.Effective(context.Background(), 7, memoryApplications{values: []application.JobApplication{{ID: "a", VacancyID: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	if !state.ApplicationLinked || state.State != StateApplied || len(store.events[7]) != 0 {
		t.Fatalf("effective=%+v events=%d", state, len(store.events[7]))
	}
}
