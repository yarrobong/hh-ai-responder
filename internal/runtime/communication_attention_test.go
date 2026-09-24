package runtime

import (
	"encoding/json"
	"testing"
	"time"

	"hh-ai-responder/internal/careeragent"
	"hh-ai-responder/internal/usecase/communicationworkitem"
)

func TestCommunicationWorkItemActionabilityUsesCurrentConversationState(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	invitedAt := now.Add(-48 * time.Hour)
	base := communicationworkitem.WorkItem{ID: "interview-old", Type: communicationworkitem.TypeInterview, Status: communicationworkitem.StatusManualReview, MessageID: "invite-old", RequiresReview: true, CreatedAt: invitedAt, UpdatedAt: invitedAt}
	conversation := EmployerConversation{ID: "conversation-1", Status: ConversationInterview, Messages: []ConversationMessage{{ID: "invite-old", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Timestamp: invitedAt, Text: "Приглашаем на интервью"}}}

	t.Run("historical invitation without explicit schedule remains reviewable", func(t *testing.T) {
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: base, Conversation: conversation, Now: now})
		if !decision.Actionable {
			t.Fatalf("undated interview was suppressed: %+v", decision)
		}
	})

	t.Run("newer same-type invitation supersedes old", func(t *testing.T) {
		newerAt := now.Add(-time.Hour)
		conversation.Messages = append(conversation.Messages, ConversationMessage{ID: "invite-new", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Timestamp: newerAt, Text: "Новое приглашение на интервью"})
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: base, Conversation: conversation, CurrentItems: []communicationworkitem.WorkItem{{ID: "interview-new", Type: communicationworkitem.TypeInterview, MessageID: "invite-new"}}, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionSuperseded)
	})

	t.Run("candidate response suppresses old invitation", func(t *testing.T) {
		conversation.Messages = append(conversation.Messages[:1], ConversationMessage{ID: "candidate-reply", Sender: ConversationSenderCandidate, Direction: ConversationOutgoing, Timestamp: now.Add(-time.Hour), Text: "Готов"})
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: base, Conversation: conversation, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionResponded)
	})

	t.Run("explicit interview time expires without timezone guessing", func(t *testing.T) {
		item := base
		item.ScheduledDate, item.ScheduledTime, item.Timezone = "23.09.2026", "15:00", "UTC"
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: item, Conversation: EmployerConversation{Status: ConversationInterview}, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionExpired)
	})

	t.Run("rejected application closes item", func(t *testing.T) {
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: base, Conversation: conversation, Application: JobApplication{Status: ApplicationRejected}, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionApplicationClosed)
	})

	t.Run("archived vacancy closes item", func(t *testing.T) {
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: base, Conversation: conversation, Vacancy: &Vacancy{ID: 42, Archived: true}, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionVacancyClosed)
	})

	t.Run("done item is resolved", func(t *testing.T) {
		item := base
		item.Status = communicationworkitem.StatusDone
		decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: item, Conversation: conversation, Now: now})
		assertSuppressedReason(t, decision, CommunicationAttentionResolved)
	})
}

func TestCommunicationWorkItemActionabilityHandlesExpiredTestAndFollowUp(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Hour)
	testItem := communicationworkitem.WorkItem{ID: "test-1", Type: communicationworkitem.TypeTest, Status: communicationworkitem.StatusManualReview, DueAt: &deadline, RequiresReview: true}
	decision := IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: testItem, Now: now})
	assertSuppressedReason(t, decision, CommunicationAttentionExpired)

	followUpAt := now.Add(-24 * time.Hour)
	followUp := communicationworkitem.WorkItem{ID: "follow-up-1", Type: communicationworkitem.TypeFollowUp, Status: communicationworkitem.StatusPending, DueAt: &followUpAt}
	conversation := EmployerConversation{ID: "conversation-follow-up", Messages: []ConversationMessage{{ID: "new-employer", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Timestamp: now.Add(-time.Hour), Text: "Есть новости по вакансии"}}}
	conversation.LastEmployerMessageAt = func() *time.Time { value := now.Add(-time.Hour); return &value }()
	decision = IsCommunicationWorkItemActionable(CommunicationWorkItemActionabilityInput{Item: followUp, Conversation: conversation, Now: now})
	assertSuppressedReason(t, decision, CommunicationAttentionConversationAdvanced)
}

func TestCommunicationAttentionEvidenceAndQueueUseStableIdentity(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	sourceAt := now.Add(-time.Hour)
	workItem := communicationworkitem.WorkItem{ID: "work-1", Type: communicationworkitem.TypeInterview, Status: communicationworkitem.StatusManualReview, MessageID: "message-1", RequiresReview: true, CreatedAt: sourceAt, UpdatedAt: sourceAt}
	conversation := EmployerConversation{ID: "conversation-1", Status: ConversationInterview, Messages: []ConversationMessage{{ID: "message-1", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Timestamp: sourceAt, Text: "Приглашаем на интервью"}}}
	telemetry := communicationTelemetryItemFromWorkItem(workItem, conversation)
	evidence, err := json.Marshal(communicationRunEvidence{Type: string(workItem.Type), MessageID: telemetry.MessageID, SourceMessageAt: telemetry.SourceMessageAt, RequiresReview: true, WorkItemStatus: string(workItem.Status)})
	if err != nil {
		t.Fatal(err)
	}
	runItem := careeragent.AgentRunItem{ID: "run-item-1", RunID: "daily-1", TargetType: "communication_work_item", TargetID: workItem.ID, ConversationID: conversation.ID, VacancyID: 42, Stage: careeragent.AgentRunStageCommunication, Status: careeragent.AgentRunItemStatusReviewRequired, DecisionCode: string(workItem.Type), Evidence: evidence, CreatedAt: sourceAt}
	queue := attentionFromRunItemsForSnapshot([]careeragent.AgentRunItem{runItem}, dashboardSnapshot{conversations: []EmployerConversation{conversation}}, now)
	if len(queue) != 1 || queue[0].ID != "communication_work_item:work-1" {
		t.Fatalf("unexpected active queue: %+v", queue)
	}
	duplicates := careeragent.BuildAttentionQueue(append(queue, queue[0]))
	if len(duplicates) != 1 {
		t.Fatalf("semantic communication identity was not deduplicated: %+v", duplicates)
	}
}

func TestDailyCommunicationStageExposesSuppressionDiagnostics(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	conversation := EmployerConversation{ID: "conversation-rejected", ApplicationID: "application-rejected", VacancyID: 42, Status: ConversationRejected, Messages: []ConversationMessage{{ID: "rejection", Sender: ConversationSenderEmployer, Direction: ConversationIncoming, Timestamp: now.Add(-time.Hour), Text: "Мы выбрали другого кандидата"}}}
	projection := classifyCareerWorkflow(JobApplication{}, conversation, nil, now, nil, nil)
	result, err := dailyCommunicationStageResultWithState(CommunicationRunReport{Conversations: 1}, CandidateInbox{Items: []CandidateInboxItem{{Conversation: conversation, Workflow: projection}}}, "daily-1", now, map[string]JobApplication{"application-rejected": {ID: "application-rejected", Status: ApplicationRejected}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Attention) != 0 || result.Summary.HistoricalAttention != 1 || result.Summary.AttentionSuppressed[string(CommunicationAttentionApplicationClosed)] != 1 {
		t.Fatalf("suppression diagnostics lost: %+v", result)
	}
	if len(result.Items) != 1 || len(result.Items[0].Evidence) == 0 {
		t.Fatalf("historical evidence was not persisted in stage result: %+v", result.Items)
	}
}

func assertSuppressedReason(t *testing.T, decision CommunicationWorkItemActionability, want CommunicationAttentionSuppressionReason) {
	t.Helper()
	if decision.Actionable || decision.SuppressionReason != want {
		t.Fatalf("decision=%+v want suppression=%s", decision, want)
	}
}
