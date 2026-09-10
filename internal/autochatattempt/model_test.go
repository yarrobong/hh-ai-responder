package autochatattempt

import (
	"errors"
	"testing"
	"time"
)

func validAttempt(action ActionType) Attempt {
	now := time.Unix(100, 0).UTC()
	value := Attempt{AttemptID: "attempt-1", ConversationID: "conversation-1", TriggerMessageID: "message-1", ActionType: action, State: StateSending, CreatedAt: now, UpdatedAt: now}
	if action == ActionReply {
		value.RequestKey = "request-1"
	}
	return value
}

func TestAttemptStateTransitionsAndBlocking(t *testing.T) {
	value := validAttempt(ActionReply)
	updated, err := value.WithOutcome(StateAccepted, time.Unix(101, 0).UTC(), "", 200, "")
	if err != nil || updated.State != StateAccepted || !IsBlocking(updated.State) {
		t.Fatalf("accepted transition: %+v err=%v", updated, err)
	}
	updated, err = updated.WithOutcome(StateTargetReplyConfirmed, time.Unix(102, 0).UTC(), "out-1", 200, "")
	if err != nil || updated.ProviderOutgoingMessageID != "out-1" || !IsBlocking(updated.State) {
		t.Fatalf("confirmation transition: %+v err=%v", updated, err)
	}
	if !IsReplayable(StateRejected) || !IsReplayable(StateNotSent) || IsBlocking(StateRejected) {
		t.Fatal("replayable state classification is wrong")
	}
	if _, err := updated.WithOutcome(StateRejected, time.Unix(103, 0).UTC(), "", 400, "rejected"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal blocking state became replayable: %v", err)
	}
}

func TestAttemptValidationRequiresProviderIdentityAndMatchingConfirmation(t *testing.T) {
	value := validAttempt(ActionReply)
	value.TriggerMessageID = ""
	if !errors.Is(value.Validate(), ErrInvalidAttempt) {
		t.Fatal("missing trigger ID was accepted")
	}
	value = validAttempt(ActionLeave)
	value.State = StateTargetReplyConfirmed
	if !errors.Is(value.Validate(), ErrInvalidAttempt) {
		t.Fatal("reply confirmation was accepted for leave")
	}
}
