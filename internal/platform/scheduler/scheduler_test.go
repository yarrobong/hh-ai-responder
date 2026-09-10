package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTimer struct {
	ch      chan time.Time
	stopped chan struct{}
}

func (t *fakeTimer) Channel() <-chan time.Time { return t.ch }
func (t *fakeTimer) Stop() bool {
	select {
	case <-t.stopped:
		return false
	default:
		close(t.stopped)
		return true
	}
}

type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

func (c *fakeClock) NewTimer(time.Duration) Timer {
	t := &fakeTimer{ch: make(chan time.Time, 1), stopped: make(chan struct{})}
	c.mu.Lock()
	c.timers = append(c.timers, t)
	c.mu.Unlock()
	return t
}

func (c *fakeClock) timer(index int) *fakeTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index >= len(c.timers) {
		return nil
	}
	return c.timers[index]
}

type errorLogger struct {
	mu   sync.Mutex
	seen []string
}

func (l *errorLogger) Error(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, format)
}

func TestLoopWaitsAfterCompletionWithoutOverlap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeClock{}
	started := make(chan int, 2)
	finishFirst := make(chan struct{})
	var first atomic.Bool
	first.Store(true)

	done := make(chan error, 1)
	go func() {
		done <- (Loop{Interval: time.Hour, Clock: clock}).Run(ctx, func(context.Context) error {
			if first.CompareAndSwap(true, false) {
				started <- 1
				<-finishFirst
			} else {
				started <- 2
			}
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first task did not start")
	}
	close(finishFirst)
	for clock.timer(0) == nil {
		time.Sleep(time.Millisecond)
	}
	select {
	case second := <-started:
		t.Fatalf("task started before interval: %d", second)
	case <-time.After(10 * time.Millisecond):
	}
	clock.timer(0).ch <- time.Now()
	select {
	case second := <-started:
		if second != 2 {
			t.Fatalf("unexpected task marker %d", second)
		}
	case <-time.After(time.Second):
		t.Fatal("second task did not start after interval")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
}

func TestLoopErrorUsesNormalCadence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock := &fakeClock{}
	logger := &errorLogger{}
	calls := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- (Loop{Interval: time.Hour, Clock: clock, Logger: logger, Name: "opaque"}).Run(ctx, func(context.Context) error {
			calls <- struct{}{}
			return errors.New("expected")
		})
	}()
	<-calls
	for clock.timer(0) == nil {
		time.Sleep(time.Millisecond)
	}
	select {
	case <-calls:
		t.Fatal("error caused an immediate retry")
	case <-time.After(10 * time.Millisecond):
	}
	clock.timer(0).ch <- time.Now()
	<-calls
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
}

func TestLoopCancellationWhileWaitingStopsFutureInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{}
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- (Loop{Interval: time.Hour, Clock: clock}).Run(ctx, func(context.Context) error {
			calls.Add(1)
			return nil
		})
	}()
	for clock.timer(0) == nil {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want one initial call", calls.Load())
	}
}

func TestLoopCancellationDuringTaskStopsNextInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{}
	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- (Loop{Interval: time.Hour, Clock: clock}).Run(ctx, func(context.Context) error {
			calls.Add(1)
			cancel()
			return nil
		})
	}()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want one call", calls.Load())
	}
}

func TestTickerWaitsBeforeTrigger(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &fakeClock{}
	var triggers atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- (Ticker{Interval: time.Minute, Clock: clock}).Run(ctx, func(context.Context) { triggers.Add(1) })
	}()
	for clock.timer(0) == nil {
		time.Sleep(time.Millisecond)
	}
	if triggers.Load() != 0 {
		t.Fatalf("triggers = %d before interval", triggers.Load())
	}
	clock.timer(0).ch <- time.Now()
	for triggers.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
}
