package runtime

import (
	"context"
	"time"

	"hh-ai-responder/internal/platform/scheduler"
)

// schedulerLogger adapts the root logger without making platform scheduler
// depend on the application package.
type schedulerLogger struct{}

const (
	autoTouchInterval     = 4 * time.Hour
	autoJobStatusInterval = 24 * time.Hour
	autoApplyInterval     = 12 * time.Hour
	autoChatInterval      = 15 * time.Minute
)

func (schedulerLogger) Error(format string, args ...any) {
	if logger != nil {
		logger.Error(format, args...)
	}
}

func (r *HHAIResponder) startRecurringTask(name string, interval time.Duration, task scheduler.Task) {
	if r == nil || r.ctx == nil {
		return
	}
	loop := scheduler.Loop{
		Interval: interval,
		Clock:    scheduler.RealClock{},
		Logger:   schedulerLogger{},
		Name:     name,
	}
	go func() {
		_ = loop.Run(r.ctx, task)
	}()
}

func (r *HHAIResponder) startRecurringTasks() {
	// Keep registration order and per-feature gates stable. Each task remains
	// synchronous inside its completion-based loop; only the independent loops
	// themselves run concurrently, as before.
	if r.autoTouch {
		r.startRecurringTask("resume touch", autoTouchInterval, func(context.Context) error {
			updated, err := r.TouchResume()
			if err != nil {
				logger.Error("Touch resume error: %v", err)
				return err
			}
			if updated {
				if r.dryRun {
					logger.Info("DRY-RUN: resume touch preview generated")
				} else {
					logger.Info("Resume updated")
				}
			} else {
				logger.Warn("Resume not updated")
			}
			return nil
		})
	} else {
		logger.Info("Resume touching disabled by configuration")
	}

	if r.autoJobStatus {
		r.startRecurringTask("job-search status", autoJobStatusInterval, func(context.Context) error {
			success, err := r.SetActiveJobSearchStatus()
			if err != nil {
				logger.Warn("Can't change job search status")
				return err
			}
			if success {
				if r.dryRun {
					logger.Info("DRY-RUN: job-search status preview generated")
				} else {
					logger.Info("Job search status is active")
				}
			} else {
				logger.Warn("Can't change job search status")
			}
			return nil
		})
	} else {
		logger.Info("Job-search status updates disabled by configuration")
	}

	if r.autoApply {
		r.startRecurringTask("automatic applications", autoApplyInterval, func(context.Context) error {
			if err := r.ApplyVacancies(); err != nil {
				logger.Error("Apply error: %v", err)
				return err
			}
			return nil
		})
	} else {
		logger.Info("Automatic applications disabled by configuration")
	}

	if r.effectiveChatMode() != "off" {
		r.startRecurringTask("auto chat", autoChatInterval, func(context.Context) error {
			if err := r.AutoRespondChats(); err != nil {
				logger.Error("Auto chat error: %v", err)
				return err
			}
			return nil
		})
	} else {
		logger.Info("Chat processing disabled by configuration")
	}
}
