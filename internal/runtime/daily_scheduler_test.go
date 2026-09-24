package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/platform/scheduler"
)

type dailySchedulerTimer struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (t *dailySchedulerTimer) Channel() <-chan time.Time { return t.ch }
func (t *dailySchedulerTimer) Stop() bool                { return t.stopped.CompareAndSwap(false, true) }

type dailySchedulerClock struct {
	mu     sync.Mutex
	timers []*dailySchedulerTimer
}

func (c *dailySchedulerClock) NewTimer(time.Duration) scheduler.Timer {
	t := &dailySchedulerTimer{ch: make(chan time.Time, 1)}
	c.mu.Lock()
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return t
}

func (c *dailySchedulerClock) timer(index int) *dailySchedulerTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.timers) {
		return nil
	}
	return c.timers[index]
}

func TestDailyCareerAgentSchedulerUsesSharedServiceAndCadence(t *testing.T) {
	store := newDailyWorkflowStoreFixture()
	var calls atomic.Int32
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{
		Workflow: store,
		Vacancy: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
			calls.Add(1)
			return careeragent.DailyStageResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := &dailySchedulerClock{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (DailyCareerAgentScheduler{Service: service, Interval: time.Hour, Clock: clock}).Run(ctx)
	}()
	for clock.timer(0) == nil {
		select {
		case err := <-done:
			t.Fatalf("scheduler exited early: %v", err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("initial service calls=%d, want one", got)
	}
	clock.timer(0).ch <- time.Now()
	for clock.timer(1) == nil {
		select {
		case err := <-done:
			t.Fatalf("scheduler exited after timer: %v", err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := calls.Load(); got != 1 || store.starts != 1 {
		t.Fatalf("repeated same-day schedule bypassed idempotency: stage_calls=%d starts=%d", got, store.starts)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("scheduler error=%v, want context canceled", err)
	}
}

func TestDailyCareerAgentSchedulerRejectsInvalidConfiguration(t *testing.T) {
	if err := (DailyCareerAgentScheduler{}).Run(context.Background()); err == nil {
		t.Fatal("nil service was accepted")
	}
	service, err := NewDailyCareerAgentService(DailyCareerAgentDependencies{Workflow: newDailyWorkflowStoreFixture(), Vacancy: func(context.Context, time.Time, string) (careeragent.DailyStageResult, error) {
		return careeragent.DailyStageResult{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := (DailyCareerAgentScheduler{Service: service}).Run(context.Background()); err == nil {
		t.Fatal("zero interval was accepted")
	}
}
