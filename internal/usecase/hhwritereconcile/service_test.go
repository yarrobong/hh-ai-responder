package hhwritereconcile

import (
	"context"
	"errors"
	"testing"
)

type readerFake struct {
	snapshot ChatDeliverySnapshot
	err      error
	reads    int
}

func (r *readerFake) ReadChatDelivery(context.Context, string) (ChatDeliverySnapshot, error) {
	r.reads++
	return r.snapshot, r.err
}

func TestAcceptedExactProviderIDConfirms(t *testing.T) {
	reader := &readerFake{snapshot: ChatDeliverySnapshot{ConversationID: "chat-1", Messages: []ChatDeliveryMessage{{ProviderID: "msg-1", Sender: "candidate", Direction: "outgoing"}}}}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{ActionID: "a", Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionSent, TransportOutcome: TransportAccepted})
	if err != nil || result.Status != StatusConfirmed || result.Transition.To != ActionDeliveryConfirmed || reader.reads != 1 {
		t.Fatalf("result=%+v err=%v reads=%d", result, err, reader.reads)
	}
}

func TestMissingAcceptedMessageRemainsUnconfirmed(t *testing.T) {
	reader := &readerFake{snapshot: ChatDeliverySnapshot{ConversationID: "chat-1"}}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionSent, TransportOutcome: TransportAccepted})
	if err != nil || result.Status != StatusNotConfirmed || result.Transition.To != ActionSentUnconfirmed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAmbiguousMissingMessageRemainsUncertain(t *testing.T) {
	reader := &readerFake{snapshot: ChatDeliverySnapshot{ConversationID: "chat-1"}}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: TransportAmbiguous})
	if err != nil || result.Status != StatusUncertain || result.Transition.To != ActionDeliveryUncertain {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAmbiguousExactProviderIDConfirms(t *testing.T) {
	reader := &readerFake{snapshot: ChatDeliverySnapshot{ConversationID: "chat-1", Messages: []ChatDeliveryMessage{{ProviderID: "msg-1", Sender: "candidate", Direction: "outgoing"}}}}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: TransportAmbiguous})
	if err != nil || result.Status != StatusConfirmed || result.Transition.To != ActionDeliveryConfirmed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestStrandedSendingNeverReopensAction(t *testing.T) {
	reader := &readerFake{snapshot: ChatDeliverySnapshot{ConversationID: "chat-1"}}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionSending, TransportOutcome: TransportAmbiguous})
	if err != nil || result.Status != StatusManualReview || result.Transition.To != ActionManualReview || result.Transition.To == ActionApproved {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestWrongConversationAndSenderCannotConfirm(t *testing.T) {
	tests := []ChatDeliverySnapshot{
		{ConversationID: "chat-2", Messages: []ChatDeliveryMessage{{ProviderID: "msg-1", Sender: "candidate"}}},
		{ConversationID: "chat-1", Messages: []ChatDeliveryMessage{{ProviderID: "msg-1", Sender: "employer", Direction: "incoming"}}},
	}
	for _, snapshot := range tests {
		reader := &readerFake{snapshot: snapshot}
		result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: TransportAmbiguous})
		if err != nil || result.Status != StatusUncertain || result.Transition.To != ActionDeliveryUncertain {
			t.Fatalf("snapshot=%+v result=%+v err=%v", snapshot, result, err)
		}
	}
}

func TestRejectedAndNotSentDoNotRead(t *testing.T) {
	for _, outcome := range []TransportOutcome{TransportRejected, TransportNotSent} {
		reader := &readerFake{}
		result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: outcome})
		if err != nil || result.Status != StatusNotApplicable || reader.reads != 0 || result.Transition.To != ActionDeliveryUncertain {
			t.Fatalf("outcome=%s result=%+v err=%v reads=%d", outcome, result, err, reader.reads)
		}
	}
}

func TestReadFailurePreservesUncertainty(t *testing.T) {
	reader := &readerFake{err: errors.New("timeout")}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: TransportAmbiguous})
	if err == nil || result.Status != StatusRemoteUnavailable || result.Transition.To != ActionDeliveryUncertain {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestConfirmedActionDoesNotRead(t *testing.T) {
	reader := &readerFake{}
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(context.Background(), AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryConfirmed})
	if err != nil || result.Status != StatusNotApplicable || reader.reads != 0 || result.Transition.To != ActionDeliveryConfirmed {
		t.Fatalf("result=%+v err=%v reads=%d", result, err, reader.reads)
	}
}

func TestCancelledContextDoesNotConfirm(t *testing.T) {
	reader := &readerFake{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := NewService(Dependencies{ChatDeliveryReader: reader}).Reconcile(ctx, AttemptEvidence{Operation: OperationChatMessage, TargetConversationID: "chat-1", ProviderMessageID: "msg-1", ExistingActionState: ActionDeliveryUncertain, TransportOutcome: TransportAmbiguous})
	if !errors.Is(err, context.Canceled) || result.Status != StatusRemoteUnavailable || result.Transition.To == ActionDeliveryConfirmed || reader.reads != 0 {
		t.Fatalf("result=%+v err=%v reads=%d", result, err, reader.reads)
	}
}
