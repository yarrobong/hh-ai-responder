package runtime

import (
	"context"
	"errors"
	"time"

	"hh-ai-responder/internal/platform/scheduler"
)

// DailyCareerAgentScheduler is a thin platform scheduler adapter. It owns no
// workflow logic and invokes the same DailyCareerAgentService used by CLI and
// dashboard. Loop's completion-based cadence and the service's durable guard
// together prevent overlapping daily runs.
type DailyCareerAgentScheduler struct {
	Service  *DailyCareerAgentService
	Interval time.Duration
	Clock    scheduler.Clock
	Logger   scheduler.Logger
}

func (s DailyCareerAgentScheduler) Run(ctx context.Context) error {
	if s.Service == nil {
		return errors.New("daily Career Agent scheduler service is required")
	}
	return (scheduler.Loop{Interval: s.Interval, Clock: s.Clock, Logger: s.Logger, Name: "career-agent daily"}).Run(ctx, func(ctx context.Context) error {
		_, err := s.Service.Run(ctx, time.Now().UTC())
		return err
	})
}
