package hhwritegateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hh-ai-responder/internal/ports/hhwrite"
)

type testActionStore struct {
	mu             sync.Mutex
	action         Action
	reserveCalls   int
	recordCalls    int
	reserveFailure error
	recordFailure  error
}

func (s *testActionStore) Load(context.Context, string) (Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.action, nil
}

func (s *testActionStore) Reserve(_ context.Context, _ string, at time.Time) (Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserveCalls++
	if s.reserveFailure != nil {
		return Action{}, s.reserveFailure
	}
	if s.action.Status != ActionApproved || s.action.NonceUsedAt != nil {
		return Action{}, ErrActionNotSendable
	}
	s.action.Status = ActionSending
	s.action.NonceUsedAt = &at
	s.action.AttemptedAt = &at
	s.action.UpdatedAt = at
	return s.action, nil
}

func (s *testActionStore) Record(_ context.Context, _ string, value ActionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordCalls++
	if s.recordFailure != nil {
		return s.recordFailure
	}
	s.action.Status = value.Status
	s.action.ProviderID = value.ProviderID
	s.action.UpdatedAt = value.Timestamp
	return nil
}

type testChatWriter struct {
	calls  atomic.Int32
	result hhwrite.WriteResult
	err    error
}

type testMutationWriters struct{ calls atomic.Int32 }

func (w *testMutationWriters) LeaveChat(context.Context, hhwrite.ChatLeaveRequest) (hhwrite.WriteResult, error) {
	w.calls.Add(1)
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderStatus: 200}, nil
}
func (w *testMutationWriters) SubmitVacancyResponse(context.Context, hhwrite.VacancyResponseRequest) (hhwrite.WriteResult, error) {
	w.calls.Add(1)
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderStatus: 200}, nil
}
func (w *testMutationWriters) TouchResume(context.Context, hhwrite.ResumeTouchRequest) (hhwrite.WriteResult, error) {
	w.calls.Add(1)
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderStatus: 200}, nil
}
func (w *testMutationWriters) SetJobSearchStatus(context.Context, hhwrite.JobSearchStatusRequest) (hhwrite.WriteResult, error) {
	w.calls.Add(1)
	return hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderStatus: 200}, nil
}

func (w *testChatWriter) SendChatMessage(context.Context, hhwrite.ChatMessageRequest) (hhwrite.WriteResult, error) {
	w.calls.Add(1)
	return w.result, w.err
}

type testAudit struct {
	mu      sync.Mutex
	events  []AuditEvent
	failure error
}

func (a *testAudit) Append(_ context.Context, event AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure != nil {
		return a.failure
	}
	a.events = append(a.events, event)
	return nil
}

func newTestService(store *testActionStore, writer *testChatWriter, audit *testAudit, enabled, dryRun bool) *Service {
	return NewService(Dependencies{ChatMessageWriter: writer, Actions: store, Audit: audit}, Options{
		WriteEnabled: enabled,
		DryRun:       dryRun,
		Now:          func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) },
	})
}

func testAction() Action {
	return Action{ID: "action-1", Operation: "conversation_reply", ConversationID: "chat-1", Text: "hello", Nonce: "nonce-123456789012345678901234567890", Status: ActionApproved}
}

func TestServiceDisabledAndDryRunDoNotInvokeCapability(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		dryRun  bool
		want    GatewayOutcome
	}{
		{name: "disabled", want: OutcomeBlocked},
		{name: "dry run", enabled: true, dryRun: true, want: OutcomeDryRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testActionStore{action: testAction()}
			writer := &testChatWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted}}
			service := newTestService(store, writer, &testAudit{}, tc.enabled, tc.dryRun)
			result, err := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
			if err == nil || result.Outcome != tc.want || writer.calls.Load() != 0 {
				t.Fatalf("unsafe capability invocation: result=%+v err=%v calls=%d", result, err, writer.calls.Load())
			}
			if store.action.Status != ActionApproved || store.action.NonceUsedAt != nil {
				t.Fatalf("policy-only execution changed durable action: %+v", store.action)
			}
		})
	}
}

func TestServiceAcceptedAndAmbiguousReplayNeverResends(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome hhwrite.Outcome
		status  ActionStatus
		want    GatewayOutcome
	}{
		{name: "accepted", outcome: hhwrite.OutcomeAccepted, status: ActionSent, want: OutcomeAccepted},
		{name: "ambiguous", outcome: hhwrite.OutcomeAmbiguous, status: ActionDeliveryUncertain, want: OutcomeDeliveryUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &testActionStore{action: testAction()}
			writer := &testChatWriter{result: hhwrite.WriteResult{Outcome: tc.outcome, ProviderID: "provider-1"}}
			service := newTestService(store, writer, &testAudit{}, true, false)
			first, firstErr := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
			if tc.outcome == hhwrite.OutcomeAccepted && firstErr != nil || first.Outcome != tc.want || writer.calls.Load() != 1 || store.action.Status != tc.status {
				t.Fatalf("first execution mismatch: result=%+v err=%v calls=%d action=%+v", first, firstErr, writer.calls.Load(), store.action)
			}
			second, secondErr := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
			if tc.outcome == hhwrite.OutcomeAmbiguous && secondErr == nil || writer.calls.Load() != 1 || second.Outcome != tc.want {
				t.Fatalf("replay was not blocked: result=%+v err=%v calls=%d", second, secondErr, writer.calls.Load())
			}
		})
	}
}

func TestServiceStoreFailureBeforeTransportFailsClosed(t *testing.T) {
	store := &testActionStore{action: testAction(), reserveFailure: errors.New("disk unavailable")}
	writer := &testChatWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted}}
	service := newTestService(store, writer, &testAudit{}, true, false)
	result, err := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
	if err == nil || !errors.Is(err, ErrPreTransportPersistence) || writer.calls.Load() != 0 || result.TransportAttempted {
		t.Fatalf("store pre-failure did not fail closed: result=%+v err=%v calls=%d", result, err, writer.calls.Load())
	}
}

func TestServicePostTransportPersistenceFailureCannotResend(t *testing.T) {
	store := &testActionStore{action: testAction(), recordFailure: errors.New("disk unavailable")}
	writer := &testChatWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted, ProviderID: "provider-1"}}
	service := newTestService(store, writer, &testAudit{}, true, false)
	result, err := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
	if err == nil || !errors.Is(err, ErrPostTransportPersistence) || writer.calls.Load() != 1 || !result.TransportAttempted || !result.NeedsReconciliation {
		t.Fatalf("post-write failure was not surfaced safely: result=%+v err=%v calls=%d", result, err, writer.calls.Load())
	}
	if _, err := service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"}); err == nil || writer.calls.Load() != 1 {
		t.Fatalf("post-write failure enabled a resend: err=%v calls=%d", err, writer.calls.Load())
	}
}

func TestServiceConcurrentReservationAllowsAtMostOneCapabilityCall(t *testing.T) {
	store := &testActionStore{action: testAction()}
	writer := &testChatWriter{result: hhwrite.WriteResult{Outcome: hhwrite.OutcomeAccepted}}
	service := newTestService(store, writer, &testAudit{}, true, false)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = service.SendChatMessage(context.Background(), ChatMessageRequest{ActionID: "action-1"})
		}()
	}
	wg.Wait()
	if writer.calls.Load() > 1 {
		t.Fatalf("concurrent replay reached capability %d times", writer.calls.Load())
	}
}

func TestServiceMaintenanceOperationsUseLighterPolicyWithoutActionState(t *testing.T) {
	writers := &testMutationWriters{}
	service := NewService(Dependencies{
		ChatLeaveWriter:       writers,
		VacancyResponseWriter: writers,
		ResumeWriter:          writers,
		JobSearchStatusWriter: writers,
	}, Options{WriteEnabled: true})
	if _, err := service.LeaveChat(context.Background(), LeaveChatRequest{ConversationID: "chat-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitVacancyResponse(context.Background(), VacancyResponseRequest{VacancyID: 1, ResumeHash: "resume"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TouchResume(context.Background(), ResumeTouchRequest{ResumeHash: "resume"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetJobSearchStatus(context.Background(), JobSearchStatusRequest{Status: "looking_for_offers"}); err != nil {
		t.Fatal(err)
	}
	if got := writers.calls.Load(); got != 4 {
		t.Fatalf("maintenance operations did not use their own explicit calls: %d", got)
	}
}
