package hhwritepreflight

import (
	"context"
	"errors"
	"testing"
	"time"

	"hh-ai-responder/internal/usecase/writeapproval"
)

type chatReaderFake struct {
	state ChatState
	err   error
	calls int
}

func (f *chatReaderFake) ReadChatState(context.Context, string) (ChatState, error) {
	f.calls++
	return f.state, f.err
}

func authorization() writeapproval.AuthorizationEvidence {
	return writeapproval.AuthorizationEvidence{ApprovalID: "a1", ConversationID: "c1", LastMessageID: "m1"}
}

func TestPreflightChatMessagePassesUnchangedState(t *testing.T) {
	reader := &chatReaderFake{state: ChatState{
		ExternalID: "hh-1", LastMessageID: "m1", State: "candidate_action_required",
		ReplyRequirement: ReplyRequirementRequired, MessageCount: 2,
	}}
	service := NewService(Dependencies{Chats: reader, Now: func() time.Time { return time.Unix(10, 0).UTC() }})
	result := service.PreflightChatMessage(context.Background(), ChatInput{
		Authorization: authorization(), ActionType: writeapproval.ActionConversationReply,
		ExpectedExternalID: "hh-1", LocalMessageCount: 2,
	})
	if !result.Passed() || result.Status != StatusPassed || reader.calls != 1 {
		t.Fatalf("unchanged chat was not accepted: %+v calls=%d", result, reader.calls)
	}
	if result.Evidence.ObservedAt.IsZero() || result.Evidence.ObservedMessageID != "m1" {
		t.Fatalf("fresh evidence was not returned: %+v", result.Evidence)
	}
}

func TestPreflightChatMessageBlocksCandidateReplyAndNewEmployerMessage(t *testing.T) {
	tests := []struct {
		name  string
		state ChatState
		want  Status
	}{
		{name: "candidate replied", state: ChatState{ExternalID: "hh-1", LastMessageID: "c1", State: "waiting_employer"}, want: StatusStale},
		{name: "new employer message", state: ChatState{ExternalID: "hh-1", LastMessageID: "m2", State: "candidate_action_required", ReplyRequirement: ReplyRequirementRequired}, want: StatusStale},
		{name: "terminal", state: ChatState{ExternalID: "hh-1", LastMessageID: "m1", State: "closed"}, want: StatusBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(Dependencies{Chats: &chatReaderFake{state: test.state}})
			result := service.PreflightChatMessage(context.Background(), ChatInput{Authorization: authorization(), ActionType: writeapproval.ActionConversationReply, ExpectedExternalID: "hh-1", LocalMessageCount: 1})
			if result.Status != test.want || result.Passed() || len(result.Reasons) == 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}

func TestPreflightChatMessageFailsClosedOnReadErrorOrCancellation(t *testing.T) {
	reader := &chatReaderFake{err: errors.New("timeout")}
	service := NewService(Dependencies{Chats: reader})
	result := service.PreflightChatMessage(context.Background(), ChatInput{Authorization: authorization(), ExpectedExternalID: "hh-1"})
	if result.Status != StatusUnavailable || result.Passed() || reader.calls != 1 {
		t.Fatalf("read error did not fail closed: %+v calls=%d", result, reader.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = service.PreflightChatMessage(ctx, ChatInput{Authorization: authorization(), ExpectedExternalID: "hh-1"})
	if result.Status != StatusUnavailable || !errors.Is(result.Err, context.Canceled) || reader.calls != 1 {
		t.Fatalf("cancellation crossed the read boundary: %+v calls=%d", result, reader.calls)
	}
}

type vacancyReaderFake struct {
	state VacancyResponseState
	calls int
}

func (f *vacancyReaderFake) ReadVacancyResponseState(context.Context, int) (VacancyResponseState, error) {
	f.calls++
	return f.state, nil
}

func knownVacancyState() VacancyResponseState {
	return VacancyResponseState{
		VacancyID: 42, ArchivedKnown: true, AlreadyRespondedKnown: true,
		CanApplyKnown: true, CanApply: true, TestPresentKnown: true,
		LetterRequiredKnown: true, ResponseURL: "https://hh.example/applicant/vacancy_response?vacancyId=42",
	}
}

func TestPreflightVacancyResponseBlocksDuplicateAndUnknownState(t *testing.T) {
	state := knownVacancyState()
	state.AlreadyResponded = true
	reader := &vacancyReaderFake{state: state}
	result := NewService(Dependencies{Vacancies: reader}).PreflightVacancyResponse(context.Background(), VacancyInput{VacancyID: 42})
	if result.Status != StatusBlocked || result.Passed() || reader.calls != 1 {
		t.Fatalf("duplicate response was not blocked: %+v", result)
	}
	state = knownVacancyState()
	state.CanApplyKnown = false
	result = NewService(Dependencies{Vacancies: &vacancyReaderFake{state: state}}).PreflightVacancyResponse(context.Background(), VacancyInput{VacancyID: 42})
	if result.Status != StatusManualReview || result.Passed() {
		t.Fatalf("unknown application state was not fail-closed: %+v", result)
	}
}

func TestCompareTestMetadataFailsClosedWithoutRemapping(t *testing.T) {
	want := TestMetadata{UIDPK: "u1", GUID: "g1", StartTime: "t1", Required: "yes", Tasks: []TestTask{{ID: 1, ChoiceIDs: []string{"10", "11"}}}}
	changed := want
	changed.Tasks = []TestTask{{ID: 1, ChoiceIDs: []string{"10", "12"}}}
	result := CompareTestMetadata(42, want, changed)
	if result.Passed() || result.Status != StatusStale || len(result.Reasons) == 0 {
		t.Fatalf("changed option metadata was accepted: %+v", result)
	}
}
