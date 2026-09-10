// Package scheduler contains timer mechanics for recurring in-process tasks.
// It deliberately has no knowledge of business workflows or application
// capabilities.
package scheduler

import (
	"context"
	"errors"
	"time"
)

// Task is one synchronous iteration of a recurring task. A task is never
// invoked concurrently with itself by Loop.
type Task func(context.Context) error

// Logger is the scheduler's intentionally small logging dependency. Callers
// choose the task label; the scheduler does not define business task names.
type Logger interface {
	Error(format string, args ...any)
}

// Timer is the small timer seam used by Loop and Ticker.
type Timer interface {
	Channel() <-chan time.Time
	Stop() bool
}

// Clock creates timers. RealClock is used by production composition; tests
// can provide a deterministic implementation without sleeping for hours.
type Clock interface {
	NewTimer(time.Duration) Timer
}

// RealClock delegates to the standard library timer implementation.
type RealClock struct{}

func (RealClock) NewTimer(interval time.Duration) Timer {
	return realTimer{timer: time.NewTimer(interval)}
}

type realTimer struct{ timer *time.Timer }

func (t realTimer) Channel() <-chan time.Time { return t.timer.C }
func (t realTimer) Stop() bool                { return t.timer.Stop() }

// Loop runs task immediately, then waits interval after each task returns
// before starting the next iteration. Task errors are reported and do not
// change the normal cadence.
type Loop struct {
	Interval time.Duration
	Clock    Clock
	Logger   Logger
	Name     string
}

// Run blocks until ctx is cancelled. Cancellation is checked before every
// task invocation and while waiting, so no new task starts after cancellation.
func (l Loop) Run(ctx context.Context, task Task) error {
	if ctx == nil {
		return errors.New("scheduler context is nil")
	}
	if task == nil {
		return errors.New("scheduler task is nil")
	}
	if l.Interval <= 0 {
		return errors.New("scheduler interval must be positive")
	}
	clock := l.Clock
	if clock == nil {
		clock = RealClock{}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := task(ctx); err != nil {
			l.logError("scheduled task %q failed: %v", l.Name, err)
		}

		timer := clock.NewTimer(l.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.Channel():
		}
	}
}

// Ticker runs trigger after each interval, beginning with a wait. Trigger is
// synchronous from the ticker's perspective; any asynchronous/coalescing
// behavior remains the caller's responsibility.
type Ticker struct {
	Interval time.Duration
	Clock    Clock
}

// Run blocks until ctx is cancelled.
func (t Ticker) Run(ctx context.Context, trigger func(context.Context)) error {
	if ctx == nil {
		return errors.New("scheduler context is nil")
	}
	if trigger == nil {
		return errors.New("scheduler trigger is nil")
	}
	if t.Interval <= 0 {
		return errors.New("scheduler interval must be positive")
	}
	clock := t.Clock
	if clock == nil {
		clock = RealClock{}
	}

	for {
		timer := clock.NewTimer(t.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.Channel():
			if err := ctx.Err(); err != nil {
				timer.Stop()
				return err
			}
			trigger(ctx)
		}
	}
}

func (l Loop) logError(format string, args ...any) {
	if l.Logger != nil {
		l.Logger.Error(format, args...)
	}
}
