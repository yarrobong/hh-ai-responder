package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func conversationTestTime() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC) }

func conversationTestStore(t *testing.T) (*ConversationStore, EmployerConversation) {
	t.Helper()
	s := NewConversationStore(filepath.Join(t.TempDir(), EmployerConversationsFilename))
	requireKnowledgeOK(t, s.Load())
	c, err := s.UpsertConversation(EmployerConversation{VacancyID: 123, HHConversationID: "hh-fixture", CompanyName: "Fixture Company", VacancyTitle: "Python Django developer"})
	requireKnowledgeOK(t, err)
	return s, c
}

func conversationTestMessage(external string, sender ConversationSender, text string, minute int) ConversationMessage {
	direction := ConversationIncoming
	if sender == ConversationSenderCandidate {
		direction = ConversationOutgoing
	}
	return ConversationMessage{ExternalID: external, Timestamp: conversationTestTime().Add(time.Duration(minute) * time.Minute), Sender: sender, Text: text, Source: ConversationSourceHH, Direction: direction}
}

func conversationTestAppend(t *testing.T, s *ConversationStore, id string, message ConversationMessage) ConversationMessage {
	t.Helper()
	got, err := s.AppendMessage(id, message)
	requireKnowledgeOK(t, err)
	return got
}

func TestConversationCreateSaveLoadAndLookup(t *testing.T) {
	s, c := conversationTestStore(t)
	if c.ID == "" || c.Status != ConversationApplied || c.FollowUpState != ConversationFollowUpNone || c.CreatedAt.IsZero() || c.LastActivityAt != nil {
		t.Fatalf("invalid new conversation: %+v", c)
	}
	if _, err := os.Stat(s.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("in-memory creation wrote a file")
	}
	requireKnowledgeOK(t, s.Save())
	info, err := os.Stat(s.path)
	requireKnowledgeOK(t, err)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("private store mode is %v", info.Mode())
	}
	loaded := NewConversationStore(s.path)
	requireKnowledgeOK(t, loaded.Load())
	got, err := loaded.GetByHHConversationID(c.HHConversationID)
	requireKnowledgeOK(t, err)
	if !reflect.DeepEqual(c, got) {
		t.Fatal("conversation changed on roundtrip")
	}
	byVacancy, err := loaded.GetByVacancyID(123)
	requireKnowledgeOK(t, err)
	if len(byVacancy) != 1 || byVacancy[0].ID != c.ID {
		t.Fatal("vacancy lookup failed")
	}
	if _, err := loaded.GetConversation("missing"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("missing lookup: %v", err)
	}
	if _, err := loaded.GetByHHConversationID(""); !errors.Is(err, ErrConversationNotFound) {
		t.Fatal("empty HH id matched")
	}
	before := loaded.GetConversationStats()
	if _, err := loaded.UpsertConversation(EmployerConversation{ID: "collision", HHConversationID: c.HHConversationID}); err == nil {
		t.Fatal("duplicate HH identity accepted")
	}
	if loaded.GetConversationStats() != before {
		t.Fatal("failed upsert changed store")
	}
}

func TestConversationAppendOrderDedupAndActivity(t *testing.T) {
	s, c := conversationTestStore(t)
	incoming := conversationTestMessage("e-1", ConversationSenderEmployer, "  Есть ли опыт Django?\n", 2)
	outgoing := conversationTestMessage("c-1", ConversationSenderCandidate, "Да, использовал Django.\n", 3)
	one := conversationTestAppend(t, s, c.ID, incoming)
	two := conversationTestAppend(t, s, c.ID, outgoing)
	// Older imports retain append order, without moving activity backwards.
	older := conversationTestAppend(t, s, c.ID, conversationTestMessage("e-0", ConversationSenderEmployer, "Здравствуйте", 1))
	before, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	duplicate := conversationTestAppend(t, s, c.ID, incoming)
	if duplicate.ID != one.ID {
		t.Fatal("duplicate import got a new local id")
	}
	after, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("duplicate import mutated conversation")
	}
	if len(after.Messages) != 3 || after.Messages[0].ID != one.ID || after.Messages[1].ID != two.ID || after.Messages[2].ID != older.ID || after.Messages[0].Text != incoming.Text {
		t.Fatal("original order or text changed")
	}
	if after.LastEmployerMessageAt == nil || !after.LastEmployerMessageAt.Equal(incoming.Timestamp) || after.LastCandidateMessageAt == nil || !after.LastCandidateMessageAt.Equal(outgoing.Timestamp) || !after.LastActivityAt.Equal(outgoing.Timestamp) {
		t.Fatalf("wrong activity: %+v", after)
	}
	timeline, err := s.GetConversationTimeline(c.ID)
	requireKnowledgeOK(t, err)
	if timeline[0].ID != older.ID || timeline[1].ID != one.ID || timeline[2].ID != two.ID {
		t.Fatal("timeline is not chronological")
	}
	incoming.Text = "changed"
	if _, err := s.AppendMessage(c.ID, incoming); err == nil {
		t.Fatal("same external id replaced original text")
	}
	requireKnowledgeOK(t, s.Save())
	requireKnowledgeOK(t, s.Load())
	loaded, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	if !reflect.DeepEqual(after, loaded) {
		t.Fatal("message order changed on disk")
	}
}

func TestConversationDetachedSnapshotsAndImmutableHistory(t *testing.T) {
	s, c := conversationTestStore(t)
	m := conversationTestAppend(t, s, c.ID, conversationTestMessage("c", ConversationSenderCandidate, "Использовал Django в BizonVR", 1))
	claim := CandidateConversationClaim{Text: m.Text, RelatedSkill: "Django", RelatedProject: "BizonVR", MessageID: m.ID, CreatedAt: conversationTestTime().Add(time.Hour)}
	requireKnowledgeOK(t, s.RecordCandidateClaim(c.ID, claim))
	requireKnowledgeOK(t, s.RecordCandidateClaim(c.ID, claim))
	before, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	got, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	got.Messages[0].Text = "rewritten"
	got.Summary.CandidateClaims[0].Text = "rewritten"
	if _, err := s.UpsertConversation(got); err == nil {
		t.Fatal("upsert rewrote original history")
	}
	got = before
	got.Messages = nil
	if _, err := s.UpsertConversation(got); err == nil {
		t.Fatal("upsert erased history")
	}
	if err := s.UpdateSummary(c.ID, ConversationSummary{}); err == nil {
		t.Fatal("summary erased recorded claims")
	}
	list, err := s.ListConversations()
	requireKnowledgeOK(t, err)
	list[0].Messages[0].Text = "changed list"
	timeline, err := s.GetConversationTimeline(c.ID)
	requireKnowledgeOK(t, err)
	timeline[0].Text = "changed timeline"
	after, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed mutation or returned slice altered storage")
	}
}

func TestConversationDraftIsNotSentAndClaimsNeedOriginal(t *testing.T) {
	s, c := conversationTestStore(t)
	draft := conversationTestMessage("", ConversationSenderCandidate, "Использовал Kubernetes", 1)
	draft.Source = ConversationSourceAIDraft
	m := conversationTestAppend(t, s, c.ID, draft)
	claim := CandidateConversationClaim{Text: m.Text, MessageID: m.ID, CreatedAt: conversationTestTime()}
	if err := s.RecordCandidateClaim(c.ID, claim); err == nil {
		t.Fatal("draft became a sent claim")
	}
	got, err := s.GetConversation(c.ID)
	requireKnowledgeOK(t, err)
	if got.LastCandidateMessageAt != nil || got.LastActivityAt != nil {
		t.Fatal("draft counted as activity")
	}
	incoming := conversationTestAppend(t, s, c.ID, conversationTestMessage("e", ConversationSenderEmployer, "Вы знаете Kubernetes", 2))
	claim.MessageID, claim.Text = incoming.ID, incoming.Text
	if err := s.RecordCandidateClaim(c.ID, claim); err == nil {
		t.Fatal("employer message became candidate claim")
	}
	candidate := conversationTestAppend(t, s, c.ID, conversationTestMessage("c", ConversationSenderCandidate, "Я изучаю Python", 3))
	claim.MessageID, claim.Text = candidate.ID, "Коммерческий опыт 5 лет"
	if err := s.RecordCandidateClaim(c.ID, claim); err == nil {
		t.Fatal("invented claim excerpt accepted")
	}
}

func TestConversationStateStatsAndSummaryAPI(t *testing.T) {
	s, first := conversationTestStore(t)
	statuses := []ConversationStatus{ConversationWaitingEmployer, ConversationWaitingEmployer, ConversationCandidateActionRequired, ConversationInterview, ConversationOffer, ConversationRejected, ConversationClosed, "custom_review"}
	for _, status := range statuses {
		_, err := s.UpsertConversation(EmployerConversation{Status: status, VacancyID: 123})
		requireKnowledgeOK(t, err)
	}
	want := ConversationStats{Total: 9, WaitingEmployer: 2, CandidateActionRequired: 1, Interview: 1, Offer: 1, Rejected: 1}
	if got := s.GetConversationStats(); got != want {
		t.Fatalf("stats=%+v want=%+v", got, want)
	}
	filtered, err := s.ListConversationsByStatus(ConversationWaitingEmployer)
	requireKnowledgeOK(t, err)
	if len(filtered) != 2 {
		t.Fatal("status filter failed")
	}
	now := conversationTestTime()
	requireKnowledgeOK(t, s.UpdateConversationState(first.ID, ConversationState{Status: ConversationWaitingEmployer, WaitingSince: &now, FollowUpState: ConversationFollowUpEligible, NextAction: "Проверить ответ вручную"}))
	now = now.Add(time.Hour)
	summary := ConversationSummary{TopicsDiscussed: []string{"Django"}, PendingQuestions: []string{"Уточнить задачу"}}
	requireKnowledgeOK(t, s.UpdateSummary(first.ID, summary))
	summary.TopicsDiscussed[0] = "changed"
	got, err := s.GetConversation(first.ID)
	requireKnowledgeOK(t, err)
	if got.WaitingSince.Equal(now) || got.Summary.TopicsDiscussed[0] != "Django" || got.FollowUpState != ConversationFollowUpEligible || got.LastActivityAt != nil {
		t.Fatal("state/summary aliased input or implied activity")
	}
	if err := s.UpdateConversationState(first.ID, ConversationState{Status: "INVALID STATUS"}); err == nil {
		t.Fatal("invalid status accepted")
	}
	requireKnowledgeOK(t, s.Save())
	requireKnowledgeOK(t, s.Load())
	if s.GetConversationStats().WaitingEmployer != 3 {
		t.Fatal("state did not survive reload")
	}
}

func TestConversationLoadAndSaveFailuresPreserveState(t *testing.T) {
	s, c := conversationTestStore(t)
	requireKnowledgeOK(t, s.Save())
	good, err := os.ReadFile(s.path)
	requireKnowledgeOK(t, err)
	for _, raw := range []string{`{`, `null`, `{"version":2,"conversations":[]}`, `{"version":1,"conversations":null}`, `{"version":1,"conversations":[],"unknown":true}`, string(good) + `{}`} {
		requireKnowledgeOK(t, os.WriteFile(s.path, []byte(raw), 0o600))
		if err := s.Load(); err == nil {
			t.Fatalf("malformed store accepted: %q", raw)
		}
		got, err := s.GetConversation(c.ID)
		requireKnowledgeOK(t, err)
		if !reflect.DeepEqual(c, got) {
			t.Fatal("failed load replaced memory")
		}
	}
	requireKnowledgeOK(t, os.WriteFile(s.path, good, 0o600))
	s.conversations[0].Status = "bad status"
	if err := s.Save(); err == nil {
		t.Fatal("invalid state saved")
	}
	after, err := os.ReadFile(s.path)
	requireKnowledgeOK(t, err)
	if !bytes.Equal(good, after) {
		t.Fatal("failed save changed existing file")
	}
	requireKnowledgeOK(t, s.Load())
	// Force rename failure after staging; the temporary file must be removed.
	s.path = filepath.Join(filepath.Dir(s.path), "destination-directory")
	requireKnowledgeOK(t, os.Mkdir(s.path, 0o700))
	if err := s.Save(); err == nil {
		t.Fatal("rename onto directory succeeded unexpectedly")
	}
	temps, err := filepath.Glob(filepath.Join(filepath.Dir(s.path), ".employer_conversations-*.tmp"))
	requireKnowledgeOK(t, err)
	if len(temps) != 0 {
		t.Fatal("private temporary snapshots left behind")
	}
	if NewConversationStore("").Load() == nil || NewConversationStore("").Save() == nil {
		t.Fatal("empty path accepted")
	}
}

func TestConversationRejectsInvalidMessageAndDiskDuplicate(t *testing.T) {
	s, c := conversationTestStore(t)
	valid := conversationTestMessage("e", ConversationSenderEmployer, "Django?", 1)
	for _, mutate := range []func(*ConversationMessage){
		func(m *ConversationMessage) { m.Timestamp = time.Time{} },
		func(m *ConversationMessage) { m.Direction = ConversationOutgoing },
		func(m *ConversationMessage) { m.Source = "unsupported" },
		func(m *ConversationMessage) { m.Sender = "unsupported" },
	} {
		bad := valid
		mutate(&bad)
		if _, err := s.AppendMessage(c.ID, bad); err == nil {
			t.Fatal("invalid message accepted")
		}
	}
	conversationTestAppend(t, s, c.ID, valid)
	requireKnowledgeOK(t, s.Save())
	file := conversationStoreFile{Version: 1, Conversations: s.conversations}
	file.Conversations = append(file.Conversations, file.Conversations[0])
	raw, err := json.Marshal(file)
	requireKnowledgeOK(t, err)
	requireKnowledgeOK(t, os.WriteFile(s.path, raw, 0o600))
	if s.Load() == nil {
		t.Fatal("duplicate conversation loaded")
	}
	if s.GetConversationStats().Total != 1 {
		t.Fatal("failed load changed store")
	}
}
