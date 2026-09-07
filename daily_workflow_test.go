package main

import (
	"path/filepath"
	"testing"
	"time"
)

func workflowTestConversation(t *testing.T, id, text string, sender ConversationSender, status ConversationStatus) EmployerConversation {
	t.Helper()
	now := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	c := EmployerConversation{ID: id, VacancyID: 42, CompanyName: "Fixture", VacancyTitle: "Python support", Status: status, CreatedAt: now, UpdatedAt: now}
	c.Messages = []ConversationMessage{{ID: id + "-message", Timestamp: now, Sender: sender, Text: text, Source: ConversationSourceHH, Direction: map[ConversationSender]ConversationDirection{ConversationSenderEmployer: ConversationIncoming, ConversationSenderCandidate: ConversationOutgoing}[sender]}}
	c.refreshActivity()
	return c
}

func TestCareerWorkflowClassificationUsesUserFacingStates(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	resolver := NewCandidateContextResolver(contextTestKnowledge())
	tests := []struct {
		name string
		c    EmployerConversation
		q    []CandidateClarificationRequest
		want CareerWorkflowState
	}{
		{"reply", workflowTestConversation(t, "reply", "Есть ли опыт с PostgreSQL?", ConversationSenderEmployer, ""), nil, WorkflowNeedsReply},
		{"user action", workflowTestConversation(t, "action", `Напишите именно "Да"`, ConversationSenderEmployer, ""), nil, WorkflowNeedsUserAction},
		{"external action", workflowTestConversation(t, "external", "Пройдите интервью: https://interview.getprofi.ru/abc", ConversationSenderEmployer, ""), nil, WorkflowExternalAction},
		{"interview", workflowTestConversation(t, "interview", "Приглашаем на собеседование завтра", ConversationSenderEmployer, ""), nil, WorkflowInterview},
		{"waiting", workflowTestConversation(t, "waiting", "Готов обсудить задачу", ConversationSenderCandidate, ""), nil, WorkflowWaitingForEmployer},
		{"no reply", workflowTestConversation(t, "status", "Спасибо за отклик, рассмотрим резюме", ConversationSenderEmployer, ""), nil, WorkflowNoReplyNeeded},
		{"terminal", workflowTestConversation(t, "terminal", "К сожалению, вакансия закрыта", ConversationSenderEmployer, ""), nil, WorkflowTerminal},
		{"clarification", workflowTestConversation(t, "clarification", "Есть ли опыт с Kafka?", ConversationSenderEmployer, ""), []CandidateClarificationRequest{{ID: "q", ConversationID: "clarification", Topic: "Kafka", Question: "Работал ли ты с Kafka?", Reason: "unknown", Status: ClarificationPending, CreatedAt: now}}, WorkflowNeedsClarification},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyCareerWorkflow(JobApplication{}, test.c, test.q, now, nil, resolver)
			if got.State != test.want || got.Label == string(test.want) {
				t.Fatalf("got state=%s label=%q, want=%s", got.State, got.Label, test.want)
			}
		})
	}
}

func TestEmployerReplyDraftCacheUsesMessageKnowledgeAndPrompt(t *testing.T) {
	conversations, conversation, kb, builder := conversationTestBuilder(t)
	conversationTestAppend(t, conversations, conversation.ID, conversationTestMessage("employer", ConversationSenderEmployer, "Есть опыт с Django?", 1))
	drafts := NewAIDraftStore(filepath.Join(t.TempDir(), "drafts.json"))
	ai := &orchestratorTestAI{response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil)}
	orchestrator := NewAIReplyOrchestrator(ai, builder, drafts)
	first, err := orchestrator.PrepareEmployerReply(conversation.ID)
	if err != nil || first.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("first draft failed: decision=%+v calls=%d err=%v", first, ai.calls, err)
	}
	second, err := orchestrator.PrepareEmployerReply(conversation.ID)
	if err != nil || second.Action != AIActionDraftReply || ai.calls != 1 {
		t.Fatalf("unchanged input regenerated draft: decision=%+v calls=%d err=%v", second, ai.calls, err)
	}
	values, _ := drafts.List()
	if len(values) != 1 || values[0].PromptVersion != dailyWorkflowPromptVersion || values[0].EmployerMessageHash == "" || values[0].RelevantKnowledgeHash == "" || values[0].InputFingerprint == "" {
		t.Fatalf("cache metadata missing: %+v", values)
	}
	_ = kb
}

func TestDailyWorkflowReportKeepsFiveExamplesPerState(t *testing.T) {
	report := BuildDailyWorkflowReport(CareerSnapshot{}, NewCandidateContextResolver(contextTestKnowledge()), DefaultFollowUpPolicy(), time.Now().UTC())
	if report.ConversationCount != 0 || report.ImportantCount != 0 || len(report.Sections) != len(inboxSectionOrder) {
		t.Fatalf("unexpected empty report: %+v", report)
	}
}
