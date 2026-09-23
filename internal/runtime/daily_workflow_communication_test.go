package runtime

import (
	"testing"
	"time"
)

func TestCareerWorkflowProjectsCommunicationWorkItemsWithoutApprovingActions(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	c := EmployerConversation{
		ID: "conversation-1", ApplicationID: "application-1", VacancyID: 42, Status: ConversationCandidateActionRequired,
		Messages: []ConversationMessage{{ID: "employer-1", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Text: "Приглашаем на интервью 24.09 в 15:30", Timestamp: now}},
	}
	a := JobApplication{ID: "application-1", VacancyID: 42, Status: ApplicationApplied, RawStatus: "response", Source: ApplicationSourceHH}
	projection := classifyCareerWorkflow(a, c, nil, now, nil, nil)
	if len(projection.CommunicationItems) != 1 || projection.CommunicationItems[0].Type != "INTERVIEW" {
		t.Fatalf("communication work item was not projected: %+v", projection)
	}
	if !projection.CommunicationItems[0].RequiresReview || projection.CommunicationItems[0].Status != "MANUAL_REVIEW" {
		t.Fatalf("interview action was not manual-only: %+v", projection.CommunicationItems[0])
	}
}

func TestCommunicationInboxBucketCoversPhase3Types(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		text   string
		bucket string
	}{
		{"Выполните тестовое задание до 30.09 по ссылке https://example.test", "TEST_TASK"},
		{"Готовы сделать вам оффер", "OFFER"},
		{"Мы выбрали другого кандидата", "REJECTED"},
	}
	for _, tc := range cases {
		c := EmployerConversation{ID: "conversation-" + tc.bucket, VacancyID: 42, Messages: []ConversationMessage{{ID: "message-" + tc.bucket, Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Text: tc.text, Timestamp: now}}}
		projection := classifyCareerWorkflow(JobApplication{}, c, nil, now, nil, nil)
		item := CandidateInboxItem{Conversation: c, Workflow: projection}
		if got := communicationInboxBucket(item); got != tc.bucket {
			t.Fatalf("text %q bucket=%q want=%q items=%+v", tc.text, got, tc.bucket, projection.CommunicationItems)
		}
	}
}
