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
