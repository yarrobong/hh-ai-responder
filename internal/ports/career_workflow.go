package ports

import (
	"context"
	"time"

	"hh-ai-responder/internal/careeragent"
)

// CareerWorkflowStore is the narrow durability boundary for Career Agent
// telemetry and review artifacts. It has no HH, AI or approval capability.
type CareerWorkflowStore interface {
	StartRun(context.Context, careeragent.AgentRun) error
	FinishRun(context.Context, careeragent.AgentRun) error
	UpsertRunItem(context.Context, careeragent.AgentRunItem) error
	UpsertPreparation(context.Context, careeragent.ApplicationPreparation) error
	GetRun(context.Context, string) (careeragent.AgentRun, error)
	ListRuns(context.Context, careeragent.RunQuery) ([]careeragent.AgentRun, error)
	GetPreparation(context.Context, int) (careeragent.ApplicationPreparation, error)
	ListPreparations(context.Context, careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error)
	RecoverInterruptedRuns(context.Context, time.Time) error
}

type CareerWorkflowReader interface {
	GetRun(context.Context, string) (careeragent.AgentRun, error)
	ListRuns(context.Context, careeragent.RunQuery) ([]careeragent.AgentRun, error)
	GetPreparation(context.Context, int) (careeragent.ApplicationPreparation, error)
	ListPreparations(context.Context, careeragent.PreparationQuery) ([]careeragent.ApplicationPreparation, error)
}

// CareerWorkflowRunItemsReader exposes telemetry for derived operator views
// such as Attention Queue without making run items a second canonical state
// machine. Legacy test doubles and external readers may omit this optional
// capability; callers must fail closed when it is unavailable.
type CareerWorkflowRunItemsReader interface {
	ListRunItems(context.Context, string, int) ([]careeragent.AgentRunItem, error)
}

// CareerWorkflowRunCoordinator is the cross-process claim capability used by
// scheduled daily runs. A false result means another process already owns the
// deterministic run or the run has already reached a terminal state.
type CareerWorkflowRunCoordinator interface {
	AcquireRun(context.Context, careeragent.AgentRun) (bool, error)
}
