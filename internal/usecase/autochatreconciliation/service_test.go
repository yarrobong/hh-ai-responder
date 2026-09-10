package autochatreconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "hh-ai-responder/internal/autochatattempt"
)

type attemptStoreFake struct {
	attempt    domain.Attempt
	readErr    error
	writeErr   error
	writes     int
	history    HistorySnapshot
	historyErr error
}

func (f *attemptStoreFake) GetByID(context.Context, string) (domain.Attempt, error) {
	if f.readErr != nil {
		return domain.Attempt{}, f.readErr
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

type historyReaderFake struct {
	snapshot HistorySnapshot
	err      error
	calls    int
}

func (f *historyReaderFake) ReadConversationHistory(context.Context, string) (HistorySnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func fixtureAttempt(t *testing.T, state domain.State, action domain.ActionType) domain.Attempt {
	t.Helper()
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	value := domain.Attempt{AttemptID: "A1", ConversationID: "C", TriggerMessageID: "M1", ActionType: action, State: state, CreatedAt: at, UpdatedAt: at, RequestKey: "A1"}
	if action == domain.ActionLeave {
		value.RequestKey = ""
	}
	return value
}

func msg(id, sender, direction string, at time.Time) HistoryMessage {
	return HistoryMessage{ProviderMessageID: id, Sender: sender, Direction: direction, Timestamp: at}
}

func TestExactOutgoingIDConfirmsWithoutTextMatching(t *testing.T) {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	store := &attemptStoreFake{attempt: fixtureAttempt(t, domain.StateDeliveryUncertain, domain.ActionReply)}
	store.attempt.ProviderOutgoingMessageID = "O1"
	reader := &historyReaderFake{snapshot: HistorySnapshot{ConversationID: "C", Messages: []HistoryMessage{msg("M1", "employer", "incoming", at), msg("O1", "candidate", "outgoing", at.Add(time.Minute))}}}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store, Now: func() time.Time { return at.Add(2 * time.Minute) }}).Reconcile(context.Background(), "A1")
	if err != nil || result.Status != StatusConfirmed || store.attempt.State != domain.StateTargetReplyConfirmed || store.attempt.Reconciliation.Kind != domain.EvidenceExactOutgoingMessage || store.writes != 1 {
		t.Fatalf("result=%+v err=%v stored=%+v writes=%d", result, err, store.attempt, store.writes)
	}
}

func TestWrongOutgoingIDIsConflictingAndNotFallbackConfirmed(t *testing.T) {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	store := &attemptStoreFake{attempt: fixtureAttempt(t, domain.StateDeliveryUncertain, domain.ActionReply)}
	store.attempt.ProviderOutgoingMessageID = "O1"
	reader := &historyReaderFake{snapshot: HistorySnapshot{ConversationID: "C", Messages: []HistoryMessage{msg("M1", "employer", "incoming", at), msg("O2", "candidate", "outgoing", at.Add(time.Minute))}}}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
	if err != nil || result.Status != StatusConflicting || store.attempt.State != domain.StateDeliveryUncertain || store.writes != 1 || store.attempt.Reconciliation == nil || store.attempt.Reconciliation.Kind != domain.EvidenceConflicting {
		t.Fatalf("wrong ID was treated as confirmation: result=%+v err=%v stored=%+v writes=%d", result, err, store.attempt, store.writes)
	}
}

func TestTargetResponseConfirmsWithoutAttemptCausality(t *testing.T) {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	store := &attemptStoreFake{attempt: fixtureAttempt(t, domain.StateAccepted, domain.ActionReply)}
	reader := &historyReaderFake{snapshot: HistorySnapshot{ConversationID: "C", Messages: []HistoryMessage{msg("M1", "employer", "incoming", at), msg("manual", "candidate", "outgoing", at.Add(time.Minute)), msg("M2", "employer", "incoming", at.Add(2*time.Minute))}}}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
	if err != nil || result.Status != StatusConfirmed || result.Evidence.Kind != domain.EvidenceTriggerResponse || store.attempt.State != domain.StateTargetReplyConfirmed || store.attempt.Reconciliation.ProviderMessageID != "manual" {
		t.Fatalf("target response was not confirmed: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}
	if result.Evidence.CausalityNote == "" || store.attempt.ProviderOutgoingMessageID != "" {
		t.Fatalf("target-only evidence overclaimed causality: %+v stored=%+v", result.Evidence, store.attempt)
	}
}

func TestTriggerBoundariesAndAbsenceRemainSafe(t *testing.T) {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		messages []HistoryMessage
		want     Status
	}{
		{"before trigger", []HistoryMessage{msg("O0", "candidate", "outgoing", at), msg("M1", "employer", "incoming", at.Add(time.Minute))}, StatusInsufficient},
		{"new trigger is separate", []HistoryMessage{msg("M1", "employer", "incoming", at), msg("O1", "candidate", "outgoing", at.Add(time.Minute)), msg("M2", "employer", "incoming", at.Add(2*time.Minute))}, StatusConfirmed},
		{"absence", []HistoryMessage{msg("M1", "employer", "incoming", at)}, StatusInsufficient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &attemptStoreFake{attempt: fixtureAttempt(t, domain.StateDeliveryUncertain, domain.ActionReply)}
			reader := &historyReaderFake{snapshot: HistorySnapshot{ConversationID: "C", Messages: test.messages}}
			result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
			if err != nil || result.Status != test.want || (test.want == StatusInsufficient && store.attempt.State != domain.StateDeliveryUncertain) {
				t.Fatalf("result=%+v err=%v stored=%+v", result, err, store.attempt)
			}
		})
	}
}

func TestReadAndPersistenceFailuresDoNotReleaseBlock(t *testing.T) {
	at := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	base := fixtureAttempt(t, domain.StateDeliveryUncertain, domain.ActionReply)
	store := &attemptStoreFake{attempt: base}
	reader := &historyReaderFake{err: errors.New("timeout")}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
	if !errors.Is(err, ErrEvidenceUnavailable) || result.Status != StatusUnavailable || store.writes != 0 || store.attempt.State != domain.StateDeliveryUncertain {
		t.Fatalf("read failure was not fail-closed: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}

	store = &attemptStoreFake{attempt: base, writeErr: errors.New("disk full")}
	reader = &historyReaderFake{snapshot: HistorySnapshot{ConversationID: "C", Messages: []HistoryMessage{msg("M1", "employer", "incoming", at), msg("O1", "candidate", "outgoing", at.Add(time.Minute))}}}
	result, err = NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
	if !errors.Is(err, ErrPersistence) || result.Status != StatusPersistence || store.attempt.State != domain.StateDeliveryUncertain {
		t.Fatalf("persistence failure released block: result=%+v err=%v stored=%+v", result, err, store.attempt)
	}
}

func TestLeaveReconciliationIsExplicitlyUnsupported(t *testing.T) {
	store := &attemptStoreFake{attempt: fixtureAttempt(t, domain.StateDeliveryUncertain, domain.ActionLeave)}
	reader := &historyReaderFake{}
	result, err := NewService(Dependencies{Attempts: store, Reader: reader, Writer: store}).Reconcile(context.Background(), "A1")
	if err != nil || result.Status != StatusUnsupported || reader.calls != 0 || store.writes != 0 || store.attempt.State != domain.StateDeliveryUncertain {
		t.Fatalf("leave was not safely unsupported: result=%+v err=%v reads=%d writes=%d", result, err, reader.calls, store.writes)
	}
}
