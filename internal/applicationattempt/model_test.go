package applicationattempt

import (
	"errors"
	"testing"
	"time"
)

func testAttempt(state State) Attempt {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	return Attempt{AttemptID: "attempt-1", VacancyID: 42, ResumeID: "resume-1", State: state, CreatedAt: at, UpdatedAt: at}
}

func TestStateTransitionsAndBlocking(t *testing.T) {
	for _, state := range []State{StateAccepted, StateRejected, StateNotSent, StateDeliveryUncertain} {
		if !ValidTransition(StateSending, state) {
			t.Fatalf("sending -> %s rejected", state)
		}
	}
	for _, state := range []State{StateSending, StateAccepted, StateDeliveryUncertain, StateTargetResponseConfirmed} {
		if !IsBlocking(state) {
			t.Fatalf("%s should block", state)
		}
	}
	if IsBlocking(StateRejected) || IsBlocking(StateNotSent) || !IsReplayable(StateRejected) || !IsReplayable(StateNotSent) {
		t.Fatal("replayability/blocking policy changed")
	}
	if ValidTransition(StateAccepted, StateSending) || ValidTransition(StateDeliveryUncertain, StateSending) {
		t.Fatal("terminal attempt became reusable")
	}
}

func TestReconciliationConfirmsTargetWithoutClaimingAttemptCausality(t *testing.T) {
	before := testAttempt(StateDeliveryUncertain)
	observed := before.UpdatedAt.Add(time.Minute)
	providerAt := observed.Add(-time.Second)
	after, err := before.WithReconciliation(ReconciliationEvidence{
		Kind: EvidenceConfirmedResponseExists, Strength: EvidenceStrong, Source: "negotiation", ProviderNegotiationID: "topic-7", ProviderResponseAt: &providerAt,
	}, observed)
	if err != nil || after.State != StateTargetResponseConfirmed || !IsBlocking(after.State) || after.AttemptID != before.AttemptID || after.VacancyID != before.VacancyID || after.ResumeID != before.ResumeID {
		t.Fatalf("unexpected reconciliation result: before=%+v after=%+v err=%v", before, after, err)
	}
	if !ValidTransition(StateAccepted, StateTargetResponseConfirmed) || ValidTransition(StateTargetResponseConfirmed, StateSending) {
		t.Fatal("reconciliation transition policy is unsafe")
	}
}

func TestOutcomePreservesIdentity(t *testing.T) {
	before := testAttempt(StateSending)
	after, err := before.WithOutcome(StateAccepted, before.UpdatedAt.Add(time.Minute), 200, "")
	if err != nil || !SameIdentity(before, after) || after.State != StateAccepted || after.ProviderStatus != 200 {
		t.Fatalf("outcome changed identity or metadata: before=%+v after=%+v err=%v", before, after, err)
	}
	if _, err := before.WithOutcome(StateSending, before.UpdatedAt, 0, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error=%v", err)
	}
}
