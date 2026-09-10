package applicationreconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "hh-ai-responder/internal/applicationattempt"
)

type attemptStoreFake struct {
	attempt  domain.Attempt
	findErr  error
	getErr   error
	writeErr error
	writes   int
}

func (f *attemptStoreFake) Get(context.Context, string) (domain.Attempt, error) {
	if f.getErr != nil {
		return domain.Attempt{}, f.getErr
	}
	return f.attempt, nil
}
func (f *attemptStoreFake) FindBlocking(context.Context, int) (domain.Attempt, error) {
	if f.findErr != nil {
		return domain.Attempt{}, f.findErr
	}
	return f.attempt, nil
}
func (f *attemptStoreFake) RecordReconciliation(_ context.Context, id string, evidence domain.ReconciliationEvidence, now time.Time) error {
	f.writes++
	if f.writeErr != nil {
		return f.writeErr
	}
	if id != f.attempt.AttemptID {
		return domain.ErrAttemptNotFound
	}
	updated, err := f.attempt.WithReconciliation(evidence, now)
	if err == nil {
		f.attempt = updated
	}
	return err
}

type evidenceReaderFake struct {
	snapshot EvidenceSnapshot
	err      error
	calls    int
}

func (f *evidenceReaderFake) ReadVacancyResponseEvidence(context.Context, Target) (EvidenceSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func reconcileAttemptFixture(t *testing.T, state domain.State) domain.Attempt {
	t.Helper()
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	value, err := domain.New(42, "resume-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if state != domain.StateSending {
		value, err = value.WithOutcome(state, now.Add(time.Minute), 500, "ambiguous")
	}
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestServiceConfirmsFreshPositiveEvidenceWithoutWriter(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateDeliveryUncertain)
	reader := &evidenceReaderFake{snapshot: EvidenceSnapshot{VacancyID: 42, ObservedAt: attempt.UpdatedAt.Add(time.Minute), PreflightAvailable: true, ApplicationsAvailable: true, Applications: []ProviderResponse{{VacancyID: 42, NegotiationID: "topic-42", ResponseByApplicant: true}}}}
	store := &attemptStoreFake{attempt: attempt}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Now: func() time.Time { return attempt.UpdatedAt.Add(time.Minute) }}).Reconcile(context.Background(), attempt.AttemptID)
	if err != nil || result.Status != StatusConfirmed || result.Attempt.State != domain.StateTargetResponseConfirmed || store.writes != 1 || reader.calls != 1 {
		t.Fatalf("result=%+v err=%v writes=%d reads=%d", result, err, store.writes, reader.calls)
	}
	if result.Evidence.ProviderNegotiationID != "topic-42" {
		t.Fatalf("provider id not preserved: %+v", result.Evidence)
	}
}

func TestServiceAbsenceLeavesAttemptBlocking(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateSending)
	reader := &evidenceReaderFake{snapshot: EvidenceSnapshot{VacancyID: 42, PreflightAvailable: true, ApplicationsAvailable: true, Preflight: PreflightEvidence{AlreadyRespondedKnown: true, AlreadyResponded: false}}}
	store := &attemptStoreFake{attempt: attempt}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader}).Reconcile(context.Background(), attempt.AttemptID)
	if err != nil || result.Status != StatusInsufficient || result.Attempt.State != domain.StateSending || store.attempt.State == domain.StateNotSent {
		t.Fatalf("absence changed safety state: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}
}

func TestServiceReadFailureLeavesAttemptBlocking(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateDeliveryUncertain)
	store := &attemptStoreFake{attempt: attempt}
	result, err := NewService(Dependencies{Attempts: store, Reader: &evidenceReaderFake{err: errors.New("timeout")}}).Reconcile(context.Background(), attempt.AttemptID)
	if !errors.Is(err, ErrEvidenceUnavailable) || result.Attempt.State != domain.StateDeliveryUncertain || store.attempt.State == domain.StateNotSent {
		t.Fatalf("read failure changed safety state: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}
}

func TestServiceConflictAndDifferentVacancyDoNotConfirm(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateSending)
	for name, snapshot := range map[string]EvidenceSnapshot{
		"conflict":          {VacancyID: 42, PreflightAvailable: true, ApplicationsAvailable: true, Preflight: PreflightEvidence{AlreadyRespondedKnown: true}, Applications: []ProviderResponse{{VacancyID: 42, NegotiationID: "topic", ResponseByApplicant: true}}},
		"different vacancy": {VacancyID: 99, PreflightAvailable: true, ApplicationsAvailable: true, Applications: []ProviderResponse{{VacancyID: 99, NegotiationID: "other", ResponseByApplicant: true}}},
	} {
		t.Run(name, func(t *testing.T) {
			store := &attemptStoreFake{attempt: attempt}
			result, err := NewService(Dependencies{Attempts: store, Reader: &evidenceReaderFake{snapshot: snapshot}}).Reconcile(context.Background(), attempt.AttemptID)
			if err != nil || result.Attempt.State != domain.StateSending || result.Status == StatusConfirmed {
				t.Fatalf("unsafe confirmation: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestServicePersistenceFailureKeepsBlockingAttempt(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateSending)
	store := &attemptStoreFake{attempt: attempt, writeErr: errors.New("disk full")}
	reader := &evidenceReaderFake{snapshot: EvidenceSnapshot{VacancyID: 42, PreflightAvailable: true, Preflight: PreflightEvidence{AlreadyRespondedKnown: true, AlreadyResponded: true}}}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader}).Reconcile(context.Background(), attempt.AttemptID)
	if !errors.Is(err, ErrPersistenceUncertain) || result.Status != StatusConfirmed || store.attempt.State != domain.StateSending {
		t.Fatalf("persistence uncertainty was not fail-closed: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}
}

func TestServiceAcceptedCanUpgradeButRemainsBlocking(t *testing.T) {
	attempt := reconcileAttemptFixture(t, domain.StateAccepted)
	store := &attemptStoreFake{attempt: attempt}
	reader := &evidenceReaderFake{snapshot: EvidenceSnapshot{VacancyID: 42, PreflightAvailable: true, Preflight: PreflightEvidence{AlreadyRespondedKnown: true, AlreadyResponded: true}}}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader}).Reconcile(context.Background(), attempt.AttemptID)
	if err != nil || result.Attempt.State != domain.StateTargetResponseConfirmed || !domain.IsBlocking(result.Attempt.State) {
		t.Fatalf("accepted upgrade invalid: result=%+v err=%v", result, err)
	}
}
