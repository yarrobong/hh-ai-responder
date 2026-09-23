package communicationworkitem

import (
	"testing"
	"time"

	"hh-ai-responder/internal/usecase/conversationpolicy"
)

func TestProjectCommunicationWorkItemsKeepsExplicitFieldsAndFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		text     string
		typeWant Type
		check    func(*testing.T, WorkItem)
	}{
		{"interview", "Приглашаем на интервью 24.09 в 15:30", TypeInterview, func(t *testing.T, item WorkItem) {
			if item.ScheduledDate != "24.09" || item.ScheduledTime != "15:30" || item.Timezone != "" || !item.RequiresReview || item.Status != StatusManualReview {
				t.Fatalf("interview details were guessed: %+v", item)
			}
		}},
		{"test", "Выполните тест до 30.09 по ссылке https://example.test/task", TypeTest, func(t *testing.T, item WorkItem) {
			if item.Link != "https://example.test/task" || item.Deadline != "30.09" || !item.RequiresReview || item.Status != StatusManualReview {
				t.Fatalf("test fields were not projected: %+v", item)
			}
		}},
		{"offer", "Готовы сделать оффер", TypeOffer, func(t *testing.T, item WorkItem) {
			if !item.RequiresReview || item.Status != StatusManualReview {
				t.Fatalf("offer was not manual-only: %+v", item)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := Project(Input{ConversationID: "c-1", ApplicationID: "a-1", VacancyID: 42, MessageID: "m-1", MessageText: tc.text, Classification: conversationpolicy.ClassifyMessage(tc.text), Now: now})
			if len(items) != 1 || items[0].Type != tc.typeWant || items[0].Key == "" || items[0].ID == "" {
				t.Fatalf("unexpected items: %+v", items)
			}
			tc.check(t, items[0])
		})
	}
}

func TestProjectCommunicationWorkItemsIsIdempotentAndDeduplicatesByIdentity(t *testing.T) {
	input := Input{ConversationID: "c-1", ApplicationID: "a-1", MessageID: "m-1", MessageText: "Готовы сделать оффер", Classification: conversationpolicy.ClassifyMessage("Готовы сделать оффер")}
	first := Project(input)
	second := Project(input)
	if len(first) != 1 || len(second) != 1 || first[0].Key != second[0].Key || first[0].ID != second[0].ID {
		t.Fatalf("work item identity is not stable: first=%+v second=%+v", first, second)
	}
}
