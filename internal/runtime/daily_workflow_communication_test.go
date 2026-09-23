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
