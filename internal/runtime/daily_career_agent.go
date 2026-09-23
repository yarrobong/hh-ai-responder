package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/ports"
)

var ErrDailyCareerAgentInProgress = errors.New("daily Career Agent run is already in progress")

const dailyStaleRunAfter = 6 * time.Hour

type DailyCareerAgentStage func(context.Context, time.Time, string) (careeragent.DailyStageResult, error)

// DailyCareerAgentDependencies intentionally has no HH writer or approval
// capability. It is the construction boundary shared by CLI, dashboard and
// scheduler.
type DailyCareerAgentDependencies struct {
	Workflow      ports.CareerWorkflowStore
	Vacancy       DailyCareerAgentStage
	Communication DailyCareerAgentStage
	Now           func() time.Time
}

type DailyCareerAgentService struct {
	workflow      ports.CareerWorkflowStore
	vacancy       DailyCareerAgentStage
	communication DailyCareerAgentStage
	now           func() time.Time
	running       atomic.Bool
}

func NewDailyCareerAgentService(deps DailyCareerAgentDependencies) (*DailyCareerAgentService, error) {
	if deps.Workflow == nil {
		return nil, errors.New("daily Career Agent workflow store is required")
	}
	if deps.Vacancy == nil && deps.Communication == nil {
		return nil, errors.New("daily Career Agent requires at least one stage")
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &DailyCareerAgentService{workflow: deps.Workflow, vacancy: deps.Vacancy, communication: deps.Communication, now: deps.Now}, nil
}

func (s *DailyCareerAgentService) Run(ctx context.Context, now time.Time) (careeragent.DailyCareerAgentRun, error) {
	if s == nil {
		return careeragent.DailyCareerAgentRun{}, errors.New("daily Career Agent service is nil")
	}
	if ctx == nil {
		return careeragent.DailyCareerAgentRun{}, errors.New("daily Career Agent context is nil")
	}
	if now.IsZero() {
		now = s.now().UTC()
	} else {
		now = now.UTC()
	}
	runID := careeragent.DailyRunID(now)
	if !s.running.CompareAndSwap(false, true) {
		return s.replayRunning(ctx, runID)
	}
	defer s.running.Store(false)

	if existing, err := s.workflow.GetRun(ctx, runID); err == nil {
		if existing.Status != careeragent.AgentRunStatusRunning || existing.StartedAt.After(now.Add(-dailyStaleRunAfter)) {
			return dailyReplay(existing), nil
		}
		if err := s.workflow.RecoverInterruptedRuns(ctx, now); err != nil {
			return careeragent.DailyCareerAgentRun{}, fmt.Errorf("recover stale daily Career Agent run: %w", err)
		}
	} else if !errors.Is(err, careeragent.ErrAgentRunNotFound) {
		return careeragent.DailyCareerAgentRun{}, fmt.Errorf("inspect daily Career Agent run: %w", err)
	} else if err := s.workflow.RecoverInterruptedRuns(ctx, now.Add(-dailyStaleRunAfter)); err != nil {
		return careeragent.DailyCareerAgentRun{}, fmt.Errorf("recover stale daily Career Agent runs: %w", err)
	}

	run := careeragent.NewAgentRun(runID, careeragent.AgentRunStageCareerAgent, now)
	run.RunType = "daily_career_agent"
	if coordinator, ok := s.workflow.(ports.CareerWorkflowRunCoordinator); ok {
		claimed, err := coordinator.AcquireRun(ctx, run)
		if err != nil {
			return careeragent.DailyCareerAgentRun{}, fmt.Errorf("acquire daily Career Agent run: %w", err)
		}
		if !claimed {
			existing, getErr := s.workflow.GetRun(ctx, runID)
			if getErr != nil {
				return careeragent.DailyCareerAgentRun{}, fmt.Errorf("daily Career Agent run was claimed but cannot be read: %w", getErr)
			}
			return dailyReplay(existing), nil
		}
	} else if err := s.workflow.StartRun(ctx, run); err != nil {
		return careeragent.DailyCareerAgentRun{}, fmt.Errorf("start daily Career Agent run: %w", err)
	}
	result := careeragent.DailyCareerAgentRun{Run: run, Attention: []careeragent.AttentionItem{}}
	configuredStages := 0
	failedStages := 0
	for _, stage := range []struct {
		name careeragent.AgentRunStage
		fn   DailyCareerAgentStage
	}{
		{careeragent.AgentRunStageDiscovery, s.vacancy},
		{careeragent.AgentRunStageCommunication, s.communication},
	} {
		if stage.fn == nil {
			continue
		}
		configuredStages++
		stageResult, stageErr := stage.fn(ctx, now, runID)
		careeragent.MergeDailyStageSummaryForRuntime(&result.Summary, stageResult.Summary)
		result.Attention = append(result.Attention, stageResult.Attention...)
		if stageErr != nil {
			failedStages++
			result.Summary.Failures = append(result.Summary.Failures, careeragent.RedactAgentError(stageErr))
			result.Summary.Communication.Failures += boolInt(stage.name == careeragent.AgentRunStageCommunication)
			stageResult.Items = append(stageResult.Items, careeragent.DailyStageFailureItemForRuntime(runID, stage.name, now, stageErr))
		}
		for index, item := range stageResult.Items {
			item.RunID = runID
			item.ID = careeragent.DailyItemIDForRuntime(runID, item, index)
			if item.CreatedAt.IsZero() {
				item.CreatedAt = now
			}
			item.NormalizeTarget()
			if err := s.workflow.UpsertRunItem(ctx, item); err != nil {
				return s.failRun(ctx, result, fmt.Errorf("persist daily run item: %w", err))
			}
		}
	}

	if len(result.Summary.Failures) > 0 && failedStages == configuredStages {
		result.Summary.Result = careeragent.DailyResultFailed
		if err := result.Run.Finish(careeragent.AgentRunStatusFailed, string(result.Summary.Result), now, errors.New("all configured daily stages failed")); err != nil {
			return s.failRun(ctx, result, err)
		}
	} else if len(result.Summary.Failures) > 0 {
		result.Summary.Result = careeragent.DailyResultPartialSuccess
		if err := result.Run.Finish(careeragent.AgentRunStatusPartial, string(result.Summary.Result), now, nil); err != nil {
			return s.failRun(ctx, result, err)
		}
	} else {
		result.Summary.Result = careeragent.DailyResultSuccess
		if err := result.Run.Finish(careeragent.AgentRunStatusCompleted, string(result.Summary.Result), now, nil); err != nil {
			return s.failRun(ctx, result, err)
		}
	}
	result.Summary.Attention = len(result.Attention)
	if err := s.workflow.FinishRun(ctx, result.Run); err != nil {
		return careeragent.DailyCareerAgentRun{}, fmt.Errorf("persist daily Career Agent run: %w", err)
	}
	return result, nil
}

func (s *DailyCareerAgentService) replayRunning(ctx context.Context, runID string) (careeragent.DailyCareerAgentRun, error) {
	existing, err := s.workflow.GetRun(ctx, runID)
	if err != nil {
		return careeragent.DailyCareerAgentRun{}, fmt.Errorf("%w: %v", ErrDailyCareerAgentInProgress, err)
	}
	return dailyReplay(existing), nil
}

func (s *DailyCareerAgentService) failRun(ctx context.Context, result careeragent.DailyCareerAgentRun, runErr error) (careeragent.DailyCareerAgentRun, error) {
	redacted := careeragent.RedactAgentError(runErr)
	result.Summary.Failures = append(result.Summary.Failures, redacted)
	finishedAt := result.Run.StartedAt
	_ = result.Run.Finish(careeragent.AgentRunStatusFailed, string(careeragent.DailyResultFailed), finishedAt, runErr)
	if result.Run.FinishedAt != nil {
		_ = s.workflow.FinishRun(ctx, result.Run)
	}
	return result, runErr
}

func dailyReplay(run careeragent.AgentRun) careeragent.DailyCareerAgentRun {
	return careeragent.DailyCareerAgentRun{
		Run: run, Summary: careeragent.DailyCareerAgentSummary{Result: careeragent.DailyResultForStatus(run.Status)}, IdempotentReplay: true,
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
