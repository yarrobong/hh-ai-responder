package runtime

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestDashboardOverviewInboxIsBoundedAndMatchesFullTopCards(t *testing.T) {
	s := dashboardTestServer(t)
	s.backgroundContext = nil
	for i := 0; i < overviewInboxCardLimit+2; i++ {
		conversation, err := s.Conversations.UpsertConversation(EmployerConversation{
			VacancyID:        0,
			HHConversationID: "overview-hh-" + string(rune('a'+i)),
			CompanyName:      "Overview Company",
			VacancyTitle:     "Python integration specialist",
		})
		if err != nil {
			t.Fatal(err)
		}
		conversationTestAppend(t, s.Conversations, conversation.ID, conversationTestMessage(
			"overview-message-"+string(rune('a'+i)), ConversationSenderEmployer, "Расскажите о Python", i+1,
		))
	}

	full := dashboardDecode[CandidateInbox](t, dashboardRequest(s, "GET", "/api/inbox?contract=full", ""))
	overview := dashboardDecode[overviewInbox](t, dashboardRequest(s, "GET", "/api/inbox/overview?contract=overview", ""))
	if len(full.Items) != overviewInboxCardLimit+2 {
		t.Fatalf("full Inbox was bounded unexpectedly: %d items", len(full.Items))
	}
	if len(overview.Items) != overviewInboxCardLimit {
		t.Fatalf("Overview returned %d items, want %d", len(overview.Items), overviewInboxCardLimit)
	}
	for i, item := range overview.Items {
		fullItem := full.Items[i]
		if item.Conversation.ID != fullItem.Conversation.ID || item.Conversation.CompanyName != fullItem.Conversation.CompanyName || item.Conversation.VacancyTitle != fullItem.Conversation.VacancyTitle {
			t.Fatalf("top-%d conversation mismatch: overview=%+v full=%+v", i, item.Conversation, fullItem.Conversation)
		}
		if fullItem.LatestMessage == nil || item.LatestMessage == nil || item.LatestMessage.Text != fullItem.LatestMessage.Text || !item.LatestMessage.Timestamp.Equal(fullItem.LatestMessage.Timestamp) {
			t.Fatalf("top-%d latest message mismatch: overview=%+v full=%+v", i, item.LatestMessage, fullItem.LatestMessage)
		}
		if item.Workflow.State != fullItem.Workflow.State || item.Workflow.WhatToDo != fullItem.Workflow.WhatToDo {
			t.Fatalf("top-%d workflow mismatch: overview=%+v full=%+v", i, item.Workflow, fullItem.Workflow)
		}
	}
	if len(full.Items[0].Conversation.Messages) == 0 {
		t.Fatal("full Inbox lost complete message history")
	}
}

func TestDashboardOverviewInboxJSONOmitsUnusedHistoryAndBodies(t *testing.T) {
	s := dashboardTestServer(t)
	c, err := s.Conversations.UpsertConversation(EmployerConversation{CompanyName: "Company", VacancyTitle: "Support"})
	if err != nil {
		t.Fatal(err)
	}
	conversationTestAppend(t, s.Conversations, c.ID, conversationTestMessage("message", ConversationSenderEmployer, "Есть опыт?", 1))
	raw := dashboardRequest(s, "GET", "/api/inbox/overview", "")
	value := dashboardDecode[map[string]any](t, raw)
	items, ok := value["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected Overview JSON: %s", raw.Body.String())
	}
	item := items[0].(map[string]any)
	conversation := item["conversation"].(map[string]any)
	if _, ok := conversation["messages"]; ok {
		t.Fatal("Overview response included message history")
	}
	if _, ok := conversation["vacancy_description"]; ok {
		t.Fatal("Overview response included vacancy body")
	}
	workflow := item["workflow"].(map[string]any)
	if len(workflow) != 2 {
		t.Fatalf("Overview workflow contains unused fields: %#v", workflow)
	}
}

type overviewCountingConversationRepository struct {
	s1ConversationRepository
	overviewCalls int
}

func (r *overviewCountingConversationRepository) ListForOverview(context.Context) ([]EmployerConversation, error) {
	r.overviewCalls++
	values := append([]EmployerConversation(nil), r.values...)
	return overviewConversationValues(values), nil
}

func TestOverviewConversationReadUsesOneBoundedRepositoryGroup(t *testing.T) {
	value := EmployerConversation{ID: "overview", Messages: []ConversationMessage{
		conversationTestMessage("old", ConversationSenderCandidate, "Добрый день", 1),
		conversationTestMessage("new", ConversationSenderEmployer, "Есть опыт?", 2),
	}}
	repository := &overviewCountingConversationRepository{s1ConversationRepository: s1ConversationRepository{values: []EmployerConversation{value}}}
	store := newConversationStoreFromRepository(repository)
	got, err := store.ListConversationsForOverview()
	if err != nil {
		t.Fatal(err)
	}
	if repository.overviewCalls != 1 || len(got) != 1 || len(got[0].Messages) != 1 || got[0].Messages[0].ExternalID != "new" {
		t.Fatalf("unexpected bounded read: calls=%d values=%+v", repository.overviewCalls, got)
	}
}

func TestDashboardOverviewInboxEmptyAndPartialDatasets(t *testing.T) {
	for _, count := range []int{0, overviewInboxCardLimit - 1, overviewInboxCardLimit, overviewInboxCardLimit + 1} {
		t.Run("count="+strconv.Itoa(count), func(t *testing.T) {
			s := dashboardTestServer(t)
			s.backgroundContext = nil
			for i := 0; i < count; i++ {
				c, err := s.Conversations.UpsertConversation(EmployerConversation{CompanyName: "Company", VacancyTitle: "Role"})
				if err != nil {
					t.Fatal(err)
				}
				conversationTestAppend(t, s.Conversations, c.ID, conversationTestMessage("m"+string(rune('a'+i)), ConversationSenderCandidate, "Спасибо", i+1))
			}
			got := dashboardDecode[overviewInbox](t, dashboardRequest(s, "GET", "/api/inbox/overview", ""))
			want := count
			if want > overviewInboxCardLimit {
				want = overviewInboxCardLimit
			}
			if len(got.Items) != want {
				t.Fatalf("returned %d items, want %d", len(got.Items), want)
			}
		})
	}
}

func TestDashboardOverviewSchedulesEligibleDraftOutsideReadPath(t *testing.T) {
	ai := &blockingDraftAI{
		response: validOrchestratorDecision("draft_reply", "Да, использовал Django.", []string{"Django"}, nil),
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	s, _ := asyncDraftTestServer(t, ai)
	if _, err := s.inboxOverview(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("Overview did not preserve eligible draft scheduling")
	}
	if code := dashboardRequest(s, "GET", "/api/inbox/overview?while_draft_waits=1", "").Code; code != 200 {
		t.Fatalf("Overview was blocked by background draft preparation: HTTP %d", code)
	}
	close(ai.release)
	waitDraftWorkersDone(t, s)
}
