package reliabilityinspection

import (
	"context"
	"errors"
	"testing"
	"time"

	applicationattempt "hh-ai-responder/internal/applicationattempt"
	autochatattempt "hh-ai-responder/internal/autochatattempt"
	applicationport "hh-ai-responder/internal/ports/applicationattempt"
	autochatport "hh-ai-responder/internal/ports/autochatattempt"
)

type applicationReaderFake struct {
	values []applicationattempt.Attempt
	err    error
}

func (f applicationReaderFake) List(context.Context, applicationport.ReadQuery) ([]applicationattempt.Attempt, error) {
	return f.values, f.err
}
func (f applicationReaderFake) GetByID(context.Context, string) (applicationattempt.Attempt, error) {
	if f.err != nil {
		return applicationattempt.Attempt{}, f.err
	}
	return f.values[0], nil
}

type autoChatReaderFake struct {
	values []autochatattempt.Attempt
	err    error
}

func (f autoChatReaderFake) List(context.Context, autochatport.ReadQuery) ([]autochatattempt.Attempt, error) {
	return f.values, f.err
}
func (f autoChatReaderFake) GetByID(context.Context, string) (autochatattempt.Attempt, error) {
	if f.err != nil {
		return autochatattempt.Attempt{}, f.err
	}
	return f.values[0], nil
}

func TestApplicationClassificationCoversDomainStates(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	values := []applicationattempt.Attempt{
		{AttemptID: "sending", VacancyID: 1, ResumeID: "r", State: applicationattempt.StateSending, CreatedAt: now, UpdatedAt: now},
		{AttemptID: "accepted", VacancyID: 2, ResumeID: "r", State: applicationattempt.StateAccepted, CreatedAt: now, UpdatedAt: now},
		{AttemptID: "rejected", VacancyID: 3, ResumeID: "r", State: applicationattempt.StateRejected, CreatedAt: now, UpdatedAt: now},
		{AttemptID: "not-sent", VacancyID: 4, ResumeID: "r", State: applicationattempt.StateNotSent, CreatedAt: now, UpdatedAt: now},
		{AttemptID: "uncertain", VacancyID: 5, ResumeID: "r", State: applicationattempt.StateDeliveryUncertain, CreatedAt: now, UpdatedAt: now},
		{AttemptID: "confirmed", VacancyID: 6, ResumeID: "r", State: applicationattempt.StateTargetResponseConfirmed, CreatedAt: now, UpdatedAt: now},
	}
	service := NewService(applicationReaderFake{values: values}, nil)
	got, err := service.ListApplications(context.Background(), ApplicationFilter{Limit: 10, NeedsAttention: false})
	if err != nil {
		t.Fatal(err)
	}
	want := map[applicationattempt.State]Classification{
		applicationattempt.StateSending:                 ClassificationUnresolved,
		applicationattempt.StateAccepted:                ClassificationAccepted,
		applicationattempt.StateRejected:                ClassificationReplayable,
		applicationattempt.StateNotSent:                 ClassificationNotSent,
		applicationattempt.StateDeliveryUncertain:       ClassificationUnresolved,
		applicationattempt.StateTargetResponseConfirmed: ClassificationConfirmed,
	}
	for _, value := range got {
		if value.Classification != want[value.State] {
			t.Fatalf("state %s classified as %s", value.State, value.Classification)
		}
	}
	if got[0].DisplayLabel == "Не отправлено: запрос к HH не выполнялся" {
		t.Fatal("SENDING was rendered as NOT_SENT")
	}
	if got[4].DisplayLabel != "Возможно отправлено; повторная отправка запрещена" {
		t.Fatalf("uncertain label = %q", got[4].DisplayLabel)
	}
}

func TestAutoChatClassificationAndTriggerIdentity(t *testing.T) {
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	values := []autochatattempt.Attempt{
		{AttemptID: "m1", ConversationID: "c", TriggerMessageID: "M1", ActionType: autochatattempt.ActionReply, State: autochatattempt.StateDeliveryUncertain, CreatedAt: now, UpdatedAt: now, RequestKey: "request-secret-M1"},
		{AttemptID: "m2", ConversationID: "c", TriggerMessageID: "M2", ActionType: autochatattempt.ActionReply, State: autochatattempt.StateAccepted, CreatedAt: now, UpdatedAt: now.Add(time.Minute), RequestKey: "request-secret-M2"},
		{AttemptID: "m3", ConversationID: "c", TriggerMessageID: "M3", ActionType: autochatattempt.ActionLeave, State: autochatattempt.StateRejected, CreatedAt: now, UpdatedAt: now.Add(2 * time.Minute), RequestKey: "request-secret-M3"},
	}
	service := NewService(nil, autoChatReaderFake{values: values})
	got, err := service.ListAutoChats(context.Background(), AutoChatFilter{Limit: 10, NeedsAttention: false, ConversationID: stringPtr("c")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].TriggerMessageID != "M1" || got[1].TriggerMessageID != "M2" || got[2].TriggerMessageID != "M3" {
		t.Fatalf("trigger identity was lost: %+v", got)
	}
	for _, value := range got {
		if value.ProviderRequestKey == "request-secret-M1" || value.ProviderRequestKey == "request-secret-M2" || value.ProviderRequestKey == "request-secret-M3" {
			t.Fatalf("request key was not masked: %q", value.ProviderRequestKey)
		}
	}
}

func TestInspectionRejectsInvalidBoundsAndSurfacesStoreError(t *testing.T) {
	service := NewService(applicationReaderFake{err: errors.New("corrupt store")}, nil)
	if _, err := service.ListApplications(context.Background(), ApplicationFilter{Limit: MaxLimit + 1}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, err := service.ListApplications(context.Background(), ApplicationFilter{Limit: 1, NeedsAttention: true}); err == nil || !stringsContains(err.Error(), "corrupt store") {
		t.Fatalf("store error was hidden: %v", err)
	}
}

func stringPtr(value string) *string { return &value }

func stringsContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
