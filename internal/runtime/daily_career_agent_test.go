package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jsonstorage "hh-ai-responder/internal/adapters/storage/json"
	"hh-ai-responder/internal/careeragent"
)

type dailyWorkflowStoreFixture struct {
	mu       sync.Mutex
	runs     map[string]careeragent.AgentRun
	items    []careeragent.AgentRunItem
	starts   int
	finishes int
}

func newDailyWorkflowStoreFixture() *dailyWorkflowStoreFixture {
	return &dailyWorkflowStoreFixture{runs: map[string]careeragent.AgentRun{}}
}

func (s *dailyWorkflowStoreFixture) StartRun(_ context.Context, run careeragent.AgentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.runs[run.ID]; ok {
		if old.StartedAt.Equal(run.StartedAt) {
			return nil
		}
		return careeragent.ErrWorkflowConflict
	}
	s.runs[run.ID] = run
	s.starts++
	return nil
}

func (s *dailyWorkflowStoreFixture) FinishRun(_ context.Context, run careeragent.AgentRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	s.finishes++
	return nil
}

func (s *dailyWorkflowStoreFixture) UpsertRunItem(_ context.Context, item careeragent.AgentRunItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, item)
	return nil
}

func (s *dailyWorkflowStoreFixture) ListRunItems(_ context.Context, runID string, limit int) ([]careeragent.AgentRunItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]careeragent.AgentRunItem, 0, len(s.items))
	for _, item := range s.items {
		if runID != "" && item.RunID != runID {
			continue
		}
		result = append(result, item)
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *dailyWorkflowStoreFixture) UpsertPreparation(context.Context, careeragent.ApplicationPreparation) error {
	return nil
}

func (s *dailyWorkflowStoreFixture) GetRun(_ context.Context, id string) (careeragent.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return careeragent.AgentRun{}, careeragent.ErrAgentRunNotFound
	}
	return run, nil
}

func (s *dailyWorkflowStoreFixture) ListRuns(_ context.Context, query careeragent.RunQuery) ([]careeragent.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]careeragent.AgentRun, 0)
	for _, run := range s.runs {
		if query.Status != nil && run.Status != *query.Status {
			continue
		}
		result = append(result, run)
	}
	return result, nil
}

func (s *dailyWorkflowStoreFixture) GetPreparation(context.Context, int) (careeragent.ApplicationPreparation, error) {
	return careeragent.ApplicationPreparation{}, careeragent.ErrPreparationNotFound
}

func (s *dailyWorkflowStoreFixture) ListPreparations(context.Context, careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error) {
	return nil, nil
}

func (s *dailyWorkflowStoreFixture) RecoverInterruptedRuns(context.Context, time.Time) error {
	return nil
}

func TestDailyCareerAgentPersistsPartialRunAndStageFailure(t *testing.T) {
	store := newDailyWorkflowStoreFixture()
	started := time.Date(2026, 9, 24, 9, 3, 0, 0, time.UTC)
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{
		Workflow: store,
		Vacancy: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			return careeragent.DailyStageResult{Summary: careeragent.DailyStageSummary{Vacancy: careeragent.DailyVacancySummary{Found: 2, Matched: 1}}}, nil
		},
		Communication: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			return careeragent.DailyStageResult{}, errors.New("conversation provider unavailable")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Run(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != careeragent.AgentRunStatusPartial || result.Summary.Result != careeragent.DailyResultPartialSuccess {
		t.Fatalf("result=%+v", result)
	}
	if result.Summary.Vacancy.Found != 2 || result.Summary.Vacancy.Matched != 1 || result.Summary.Communication.Failures != 1 {
		t.Fatalf("summary=%+v", result.Summary)
	}
	if store.starts != 1 || store.finishes != 1 {
		t.Fatalf("durability starts=%d finishes=%d", store.starts, store.finishes)
	}
}

func TestDailyCareerAgentMarksAllStageFailureAsFailed(t *testing.T) {
	store := newDailyWorkflowStoreFixture()
	started := time.Date(2026, 9, 24, 9, 3, 0, 0, time.UTC)
	stage := func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
		return careeragent.DailyStageResult{}, errors.New("stage unavailable")
	}
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{Workflow: store, Vacancy: stage, Communication: stage})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Run(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Status != careeragent.AgentRunStatusFailed || result.Summary.Result != careeragent.DailyResultFailed {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Summary.Failures) != 2 || len(store.items) != 2 {
		t.Fatalf("failure accounting summary=%+v items=%d", result.Summary, len(store.items))
	}
}

func TestDailyCareerAgentSecondInvocationDoesNotOverlap(t *testing.T) {
	store := newDailyWorkflowStoreFixture()
	started := time.Date(2026, 9, 24, 9, 3, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{
		Workflow: store,
		Vacancy: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			close(entered)
			<-release
			return careeragent.DailyStageResult{}, nil
		},
		Communication: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			return careeragent.DailyStageResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan careeragent.DailyCareerAgentRun, 1)
	go func() {
		result, runErr := service.Run(context.Background(), started)
		if runErr != nil {
			t.Errorf("first run: %v", runErr)
		}
		firstDone <- result
	}()
	<-entered
	second, err := service.Run(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if !second.IdempotentReplay || second.Run.ID != careeragent.DailyRunID(started) {
		t.Fatalf("second result=%+v", second)
	}
	close(release)
	<-firstDone
	if store.starts != 1 {
		t.Fatalf("starts=%d, want one durable run", store.starts)
	}
}

func TestDailyCareerAgentSharedDurableStoreClaimsOneRunAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "career-workflow.json")
	firstStore := jsonstorage.NewCareerWorkflowRepository(path)
	secondStore := jsonstorage.NewCareerWorkflowRepository(path)
	started := time.Date(2026, 9, 24, 9, 3, 0, 0, time.UTC)
	entered := make(chan struct{})
	release := make(chan struct{})
	var stageCalls atomic.Int32
	stage := func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
		stageCalls.Add(1)
		close(entered)
		<-release
		return careeragent.DailyStageResult{}, nil
	}
	first, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{Workflow: firstStore, Vacancy: stage})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{Workflow: secondStore, Vacancy: stage})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan careeragent.DailyCareerAgentRun, 1)
	firstErr := make(chan error, 1)
	go func() {
		result, runErr := first.Run(context.Background(), started)
		firstDone <- result
		firstErr <- runErr
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first shared-store run did not start")
	}
	secondResult, secondErr := second.Run(context.Background(), started)
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	if !secondResult.IdempotentReplay || secondResult.Run.ID != careeragent.DailyRunID(started) {
		t.Fatalf("second shared-store result=%+v", secondResult)
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	<-firstDone
	if got := stageCalls.Load(); got != 1 {
		t.Fatalf("stage calls=%d, want one claimed run", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("durable workflow file missing: %v", err)
	}
}
