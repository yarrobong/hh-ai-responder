package applicationattemptpolicy

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
)

type blockingReaderFake struct {
	attempt domain.Attempt
	err     error
}

func (f blockingReaderFake) FindBlocking(context.Context, int) (domain.Attempt, error) {
	return f.attempt, f.err
}

func policyAttempt(t *testing.T, state domain.State) domain.Attempt {
	t.Helper()
	attempt, err := domain.New(42, "resume-1", time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if state == domain.StateTargetResponseConfirmed {
		attempt, err = attempt.WithOutcome(domain.StateAccepted, attempt.UpdatedAt.Add(time.Minute), 200, "")
		if err == nil {
			attempt, err = attempt.WithOutcome(state, attempt.UpdatedAt.Add(time.Minute), 200, "")
		}
	} else if state != domain.StateSending {
		attempt, err = attempt.WithOutcome(state, attempt.UpdatedAt.Add(time.Minute), 0, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return attempt
}

func TestClassifyState(t *testing.T) {
	tests := map[domain.State]Classification{
		domain.StateTargetResponseConfirmed: BlockingConfirmed,
		domain.StateAccepted:                BlockingConfirmed,
		domain.StateSending:                 BlockingUnresolved,
		domain.StateDeliveryUncertain:       BlockingUnresolved,
		domain.StateRejected:                ReplayableHistory,
		domain.StateNotSent:                 ReplayableHistory,
	}
	for state, want := range tests {
		if got := ClassifyState(state); got != want {
			t.Errorf("state %s: got %s, want %s", state, got, want)
		}
	}
}

func TestGateBlocksOnlyDurableBlockingStates(t *testing.T) {
	for _, state := range []domain.State{domain.StateSending, domain.StateDeliveryUncertain, domain.StateAccepted, domain.StateTargetResponseConfirmed} {
		t.Run(string(state), func(t *testing.T) {
			decision, err := NewGate(blockingReaderFake{attempt: policyAttempt(t, state)}).Evaluate(context.Background(), 42)
			if err != nil || decision.Classification == NoAttempt || decision.Attempt == nil {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
			if decision.Classification != ClassifyState(state) {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func TestGateTreatsMissingAndStoreFailureDifferently(t *testing.T) {
	missing, err := NewGate(blockingReaderFake{err: domain.ErrAttemptNotFound}).Evaluate(context.Background(), 42)
	if err != nil || missing.Classification != NoAttempt {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	failure := errors.New("disk unavailable")
	decision, err := NewGate(blockingReaderFake{err: failure}).Evaluate(context.Background(), 42)
	if !errors.Is(err, ErrAttemptStoreUnavailable) || !errors.Is(err, failure) || decision.Classification != StoreUnavailable {
		t.Fatalf("failure decision=%+v err=%v", decision, err)
	}
}
