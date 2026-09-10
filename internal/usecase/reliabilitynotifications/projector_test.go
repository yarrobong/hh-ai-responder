package reliabilitynotifications

import (
	"context"
	"errors"
	"testing"
)

type storeFake struct {
	records []Record
	err     error
}

func (s *storeFake) UpsertReliability(_ context.Context, value Record) error {
	if s.err != nil {
		return s.err
	}
	for i := range s.records {
		if s.records[i].Key == value.Key {
			s.records[i] = value
			return nil
		}
	}
	s.records = append(s.records, value)
	return nil
}

func TestApplicationUncertaintyIsAdvisoryAndDeduplicatedByAttempt(t *testing.T) {
	store := &storeFake{}
	projector := NewProjector(store)
	event := ApplicationOutcome{AttemptID: "A1", VacancyID: 42, State: "DELIVERY_UNCERTAIN"}
	if err := projector.ProjectApplicationOutcome(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := projector.ProjectApplicationOutcome(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 1 || store.records[0].Kind != ApplicationDeliveryUncertain || store.records[0].AttemptID != "A1" || store.records[0].VacancyID != 42 {
		t.Fatalf("unexpected records: %+v", store.records)
	}
	if store.records[0].Message == "Отклик не отправлен." || store.records[0].Message == "Ошибка отправки — повторить" {
		t.Fatalf("uncertainty was overclaimed: %q", store.records[0].Message)
	}
}

func TestApplicationConfirmationEvolvesSameIncidentWithoutReleasingAttempt(t *testing.T) {
	store := &storeFake{}
	projector := NewProjector(store)
	if err := projector.ProjectApplicationOutcome(context.Background(), ApplicationOutcome{AttemptID: "A1", VacancyID: 42, State: "SENDING"}); err != nil {
		t.Fatal(err)
	}
	if err := projector.ProjectApplicationReconciliation(context.Background(), ApplicationReconciliation{AttemptID: "A1", VacancyID: 42, Status: "CONFIRMED"}); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 1 || !store.records[0].Resolve || store.records[0].Kind != ApplicationResponseConfirmed || store.records[0].Message != "Отклик подтверждён на HH." {
		t.Fatalf("confirmation did not evolve the incident: %+v", store.records)
	}
}

func TestAutoChatConfirmationPreservesCausalityBoundary(t *testing.T) {
	store := &storeFake{}
	projector := NewProjector(store)
	base := AutoChatOutcome{AttemptID: "A1", ConversationID: "C1", TriggerMessageID: "M1", ActionType: "REPLY", State: "DELIVERY_UNCERTAIN"}
	if err := projector.ProjectAutoChatOutcome(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if err := projector.ProjectAutoChatReconciliation(context.Background(), AutoChatReconciliation{AttemptID: "A1", ConversationID: "C1", TriggerMessageID: "M1", ActionType: "REPLY", Status: "CONFIRMED", EvidenceKind: "TRIGGER_RESPONSE_CONFIRMED"}); err != nil {
		t.Fatal(err)
	}
	if len(store.records) != 1 || !store.records[0].Resolve || store.records[0].ActionType != "REPLY" {
		t.Fatalf("unexpected confirmation: %+v", store.records)
	}
	if store.records[0].Message == "Автоответ успешно доставлен." || store.records[0].Message == "Ответ в диалоге подтверждён на HH." {
		t.Fatalf("confirmation overclaimed causality: %q", store.records[0].Message)
	}
}

func TestAutoChatLeaveUnsupportedUpdatesOneIncident(t *testing.T) {
	store := &storeFake{}
	projector := NewProjector(store)
	base := AutoChatOutcome{AttemptID: "L1", ConversationID: "C1", TriggerMessageID: "M1", ActionType: "LEAVE", State: "SENDING"}
	if err := projector.ProjectAutoChatOutcome(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := projector.ProjectAutoChatReconciliation(context.Background(), AutoChatReconciliation{AttemptID: "L1", ConversationID: "C1", TriggerMessageID: "M1", ActionType: "LEAVE", Status: "UNSUPPORTED"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.records) != 1 || store.records[0].Kind != AutoChatLeaveReconciliationUnsupported || store.records[0].Resolve {
		t.Fatalf("leave unsupported was duplicated or resolved: %+v", store.records)
	}
	if store.records[0].Message == "Выход из диалога подтверждён." {
		t.Fatal("leave was falsely confirmed")
	}
}

func TestProjectionFailureIsReturnedWithoutSafetySemantics(t *testing.T) {
	store := &storeFake{err: errors.New("notification disk full")}
	err := NewProjector(store).ProjectApplicationOutcome(context.Background(), ApplicationOutcome{AttemptID: "A1", VacancyID: 42, State: "DELIVERY_UNCERTAIN"})
	if err == nil {
		t.Fatal("expected advisory persistence error")
	}
}
