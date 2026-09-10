package runtime

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestConversationStorageContract_RestartPreservesExternalIDsTimestampsStateAndImmutableHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), EmployerConversationsFilename)
	createdAt := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	firstAt := time.Date(2026, 8, 5, 8, 15, 0, 0, time.UTC)
	secondAt := time.Date(2026, 8, 5, 8, 20, 0, 0, time.UTC)
	store := NewConversationStore(path)
	conversation, err := store.UpsertConversation(EmployerConversation{
		ID: "conversation-fixed", VacancyID: 42, HHConversationID: "hh-chat-42", CompanyName: "Fixture Company",
		VacancyTitle: "Python/Django", VacancyDescription: "Integrate APIs", CreatedAt: createdAt,
		HHUpdatedAt: secondAt, RawStatus: "employer_replied", HHMetadata: map[string]string{"chat_state": "open"},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.AppendMessage(conversation.ID, ConversationMessage{ID: "message-employer", ExternalID: "hh-message-1", Timestamp: firstAt, Sender: ConversationSenderEmployer, Text: "Есть ли опыт Django?", Source: ConversationSourceHH, Direction: ConversationIncoming})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendMessage(conversation.ID, ConversationMessage{ID: "message-candidate", ExternalID: "hh-message-2", Timestamp: secondAt, Sender: ConversationSenderCandidate, Text: "Да, использовал Django.", Source: ConversationSourceHHWrite, Direction: ConversationOutgoing})
	if err != nil {
		t.Fatal(err)
	}
	claim := CandidateConversationClaim{Text: second.Text, RelatedSkill: "Django", MessageID: second.ID, CreatedAt: secondAt}
	if err := store.RecordCandidateClaim(conversation.ID, claim); err != nil {
		t.Fatal(err)
	}
	waitingSince := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	if err := store.UpdateConversationState(conversation.ID, ConversationState{Status: ConversationWaitingEmployer, NextAction: "wait", WaitingSince: &waitingSince, FollowUpState: ConversationFollowUpEligible}); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetConversation(conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastEmployerMessageAt == nil || !before.LastEmployerMessageAt.Equal(firstAt) || before.LastCandidateMessageAt == nil ||
		!before.LastCandidateMessageAt.Equal(secondAt) || before.LastActivityAt == nil || !before.LastActivityAt.Equal(secondAt) ||
		len(before.Summary.CandidateClaims) != 1 {
		t.Fatalf("conversation activity/history contract not established: %+v", before)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	restarted := NewConversationStore(path)
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	after, err := restarted.GetConversation("conversation-fixed")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("conversation restart changed domain state: after=%+v before=%+v", after, before)
	}
	if after.HHConversationID != "hh-chat-42" || after.Messages[0].ExternalID != "hh-message-1" || after.Messages[1].ExternalID != "hh-message-2" ||
		after.Messages[0].Timestamp != firstAt || after.Messages[1].Timestamp != secondAt || after.WaitingSince == nil || !after.WaitingSince.Equal(waitingSince) {
		t.Fatalf("conversation IDs/timestamps/state changed: %+v", after)
	}

	// The append-only history remains immutable after restart. A conflicting
	// replay with the same external message id must fail rather than rewrite it.
	if _, err := restarted.AppendMessage(after.ID, ConversationMessage{ID: first.ID, ExternalID: first.ExternalID, Timestamp: firstAt, Sender: ConversationSenderEmployer, Text: "rewritten", Source: ConversationSourceHH, Direction: ConversationIncoming}); err == nil {
		t.Fatal("replayed message rewrote immutable history")
	}
	unchanged, err := restarted.GetConversation(after.ID)
	if err != nil || !reflect.DeepEqual(unchanged, after) {
		t.Fatalf("conflicting replay changed history: got=%+v err=%v", unchanged, err)
	}
}
