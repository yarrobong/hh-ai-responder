package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHHNegotiationsObservedContainerAndPagination(t *testing.T) {
	// Synthetic shape copied from the public field structure, with no private data.
	raw := []byte(`<html><script>{"unrelated":{"id":999,"status":"RESPONSE"}}</script><script>{"redirectConfig":{},"applicantNegotiations":{"topicList":[{"id":71,"vacancyId":42,"lastState":"DISCARD","initialTopicType":"RESPONSE_BY_APPLICANT","chatId":51,"creationTime":"2026-09-01T12:00:00+03:00","lastModified":"2026-09-02T12:00:00+03:00"}],"paging":{"next":{"page":1,"disabled":false}}},"vacanciesShort":{"vacanciesList":[{"vacancyId":42,"name":"Fixture role","company":{"name":"Fixture company"}}]}}</script></html>`)
	records, next, ok, err := parseHHNegotiations(raw)
	requireKnowledgeOK(t, err)
	if !ok || next != "1" || len(records) != 1 || records[0].ExternalID != "71" || records[0].Company != "Fixture company" || records[0].Vacancy.ID != 42 || records[0].Status != "DISCARD" || records[0].CreatedAt.IsZero() {
		t.Fatalf("bad parse: %+v next %q", records, next)
	}
	if _, err := parseHHApplicationPage([]byte(`<html><script>{"random":"page"}</script></html>`)); err == nil {
		t.Fatal("unrecognized HTML was treated as empty applications")
	}
	if records, _, _, err := parseHHNegotiations([]byte(`{"applicantNegotiations":{"topicList":[],"paging":{"next":{"disabled":true}}}}`)); err != nil || len(records) != 0 {
		t.Fatal("valid empty list failed")
	}
}
func TestHHChatReadUsesFromCursorAndNoWrite(t *testing.T) {
	oldLogger := logger
	logger = NewLogger(io.Discard, LevelError)
	t.Cleanup(func() { logger = oldLogger })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("HH write attempted")
		}
		if r.URL.Query().Get("from") != "cursor_2" {
			t.Error("pagination cursor not passed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chats":{"items":[],"nextFrom":"cursor_3","found":42},"resources":{}}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	r := &HHAIResponder{ctx: context.Background(), baseURL: base, chatURL: server.URL, client: server.Client()}
	r.jar = &MemoryPersistentJar{cookies: map[string][]*http.Cookie{}}
	r.requester = NewHHRequester(context.Background(), r.client, 0)
	client := NewHHAIResponderReadClient(r)
	page, err := client.ReadConversations(context.Background(), "cursor_2")
	requireKnowledgeOK(t, err)
	if page.NextCursor != "cursor_3" || calls != 1 {
		t.Fatal("cursor lost")
	}
}
func TestHHMessageSenderSystemAndPartialHistory(t *testing.T) {
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	data := ChatDataResponse{Chat: ChatDetail{CurrentParticipantID: "applicant-participant", Messages: ChatMessages{HasMore: true, Items: []ChatMessage{
		{ID: 1, Type: "SIMPLE", ParticipantID: "applicant-participant", Text: "Candidate fixture", CreationTime: ts},
		{ID: 2, Type: "PARTICIPANT_LEFT", ParticipantID: "other", CreationTime: ts.Add(time.Hour)},
		{ID: 3, Type: "SIMPLE", ParticipantID: "hr", Text: "Employer fixture", CreationTime: ts.Add(2 * time.Hour)},
	}}}, Resources: ExtendedResources{Participants: map[string]ParticipantDetail{"hr": {Type: "EMPLOYER_USER"}}, NegotiationTopics: map[string]NegotiationTopic{"topic": {CurrentApplicantState: "DISCARD"}}}}
	record := hhChatRecord(ChatListItem{Id: 5, Resources: ChatItemResources{NegotiationTopic: []string{"topic"}}}, &data, &ChatsResponse{})
	if record.Messages[0].Sender != "candidate" || record.Messages[1].Sender != "system" || record.Messages[2].Sender != "employer" || record.Status != "DISCARD" || record.Metadata["history_incomplete"] != "true" {
		t.Fatal("bad typed mapping")
	}
	messages, warnings := mapHHMessages(record.Messages)
	if len(messages) != 3 || len(warnings) != 0 || messages[1].Text != "" {
		t.Fatal("system event was lost")
	}
}
func TestHHImportRetainsEvidenceDismissAndAnnotations(t *testing.T) {
	vacancies, apps, conversations, state := newSyncStores(t)
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	svc := NewHHReadSyncServiceWithOptions(nil, HHReadSyncServiceOptions{Vacancies: vacancies, Applications: apps, Conversations: conversations, StatePath: state})
	record := HHConversationRecord{ExternalID: "c", VacancyID: 42, Status: "RESPONSE", CreatedAt: ts, UpdatedAt: ts, Messages: []HHMessageRecord{{ExternalID: "m", Sender: "candidate", Text: "original", Timestamp: ts}}}
	_, err := svc.importConversation(record)
	requireKnowledgeOK(t, err)
	c, _ := conversations.GetByHHConversationID("c")
	c.FollowUpState = ConversationFollowUpDismissed
	c.Summary.TopicsDiscussed = []string{"retained"}
	_, err = conversations.UpsertConversation(c)
	requireKnowledgeOK(t, err)
	record.Messages[0].Sender = "employer"
	record.Messages = append(record.Messages, HHMessageRecord{ExternalID: "new", Sender: "employer", Text: "new", Timestamp: ts.Add(time.Hour)})
	_, err = svc.importConversation(record)
	requireKnowledgeOK(t, err)
	c, _ = conversations.GetByHHConversationID("c")
	if c.Messages[0].Sender != ConversationSenderCandidate || c.Messages[0].Text != "original" || len(c.Messages) != 2 || c.HHMetadata["warning_message_content_conflict"] != "true" || c.FollowUpState != ConversationFollowUpDismissed || len(c.Summary.TopicsDiscussed) != 1 {
		t.Fatal("history rewritten or warning/local state lost")
	}
	out, err := svc.importConversation(record)
	requireKnowledgeOK(t, err)
	if out != syncUnchanged {
		t.Fatal("repeat import not idempotent", out)
	}
}
func TestFollowUpAnalyticsWaitAndNoInventedCounts(t *testing.T) {
	in, now := followUpFixture()
	in.Conversation.Messages = []ConversationMessage{followUpMessage("c", ConversationSenderCandidate, now.Add(-4*24*time.Hour)), followUpMessage("e", ConversationSenderEmployer, now.Add(-3*24*time.Hour))}
	in.Conversation.Status = ConversationCandidateActionRequired
	d := CareerSnapshot{Applications: []JobApplication{in.Application}, Conversations: []EmployerConversation{in.Conversation}}
	a := d.FollowUpAnalytics(DefaultFollowUpPolicy(), now)
	if a.EmployerResponseSamples != 1 || a.AverageEmployerResponseHours != nil || a.FollowUpDrafted != 0 {
		t.Fatal("small dataset produces conclusions", a)
	}
	if f := d.FollowUps(DefaultFollowUpPolicy(), now)[0]; f.PreviousFollowUps != 0 {
		t.Fatal("ordinary candidate message counted as followup")
	}
}
func TestCareerAuditStaleDraftAndClarification(t *testing.T) {
	in, now := followUpFixture()
	in.Conversation.Messages = []ConversationMessage{followUpMessage("new", ConversationSenderEmployer, now)}
	d := CareerSnapshot{Applications: []JobApplication{in.Application}, Conversations: []EmployerConversation{in.Conversation}, Drafts: []AIDraft{{ID: "d", ApplicationID: "a", ConversationID: "c", Type: AIDraftFollowUp, Status: AIDraftGenerated, CreatedAt: now.Add(-time.Hour)}}, Clarifications: []CandidateClarificationRequest{{ID: "q", Status: ClarificationPending, ApplicationID: "a"}}}
	report := BuildCareerAuditReport(d, DefaultFollowUpPolicy(), now)
	if report.WarningCounts["stale_ai_draft"] != 1 || report.WarningCounts["unresolved_clarification"] == 0 {
		t.Fatal("stale/pending warnings missing")
	}
	raw, _ := json.Marshal(report)
	if strings.Contains(string(raw), "Fixture message") {
		t.Fatal("audit leaked private message")
	}
}

func TestHHSyncCLIAllTargetAndFailureReport(t *testing.T) {
	target, err := hhSyncTarget([]string{"sync"})
	requireKnowledgeOK(t, err)
	if target != "all" {
		t.Fatal(target)
	}
	for _, args := range [][]string{{}, {"sync", "bad"}, {"sync", "conversations", "extra"}} {
		if _, err := hhSyncTarget(args); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
	v, a, c, state := newSyncStores(t)
	service := NewHHReadSyncServiceWithOptions(nil, HHReadSyncServiceOptions{Vacancies: v, Applications: a, Conversations: c, StatePath: state})
	var output strings.Builder
	if err := writeHHSyncReport(service, "all", &output); err == nil || !json.Valid([]byte(output.String())) {
		t.Fatal("failed sync must return error and JSON diagnostics")
	}
}
func TestHHEmptyTextMessageIsRetainedWithWarning(t *testing.T) {
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	data := ChatDataResponse{Chat: ChatDetail{CurrentParticipantID: "me", Messages: ChatMessages{Items: []ChatMessage{{ID: 1, ParticipantID: "me", Type: "SIMPLE", CreationTime: ts}}}}}
	record := hhChatRecord(ChatListItem{Id: 2}, &data, &ChatsResponse{})
	messages, warnings := mapHHMessages(record.Messages)
	if len(messages) != 1 || len(warnings) != 0 || !messages[0].ContentUnavailable || record.Metadata["warning_message_content_unavailable"] != "true" {
		t.Fatal("empty HH content silently lost")
	}
	requireKnowledgeOK(t, messages[0].Validate())
}
func TestOrphanClarificationDoesNotLeakToOtherConversations(t *testing.T) {
	in, now := followUpFixture()
	c := in.Conversation
	c.Messages = []ConversationMessage{followUpMessage("employer", ConversationSenderEmployer, now.Add(-time.Hour))}
	c.Status = ConversationCandidateActionRequired
	d := CareerSnapshot{Conversations: []EmployerConversation{c}, Clarifications: []CandidateClarificationRequest{{ID: "q", ConversationID: "different", Status: ClarificationPending}}}
	if state := d.ResolveConversation(c, now); state.Status != ConversationCandidateActionRequired {
		t.Fatal("unrelated clarification changed state", state)
	}
}

func TestHHWorkflowAnnotationNormalizesWithoutRewritingEvidence(t *testing.T) {
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	old := EmployerConversation{ID: "c", Messages: []ConversationMessage{{ID: "m", ExternalID: "m", Timestamp: ts, Text: "", Sender: ConversationSenderCandidate, Direction: ConversationOutgoing, Source: ConversationSourceHH, ContentUnavailable: true}}, HHMetadata: map[string]string{"warning_message_content_unavailable": "true"}}
	incoming := EmployerConversation{HHMetadata: map[string]string{}, Messages: []ConversationMessage{{ID: "m", ExternalID: "m", Timestamp: ts, Sender: ConversationSenderSystem, Direction: ConversationIncoming, Source: ConversationSourceHH, HHSystemEvent: true}}}
	mergeHHConversation(old, &incoming)
	m := incoming.Messages[0]
	if !m.HHSystemEvent || m.Sender != old.Messages[0].Sender || !sameConversationMessage(m, old.Messages[0]) || len(deliveredMessages(incoming.Messages)) != 0 || incoming.HHMetadata["warning_message_content_unavailable"] != "" {
		t.Fatal("workflow event affected human history or rewrote original evidence")
	}
	data := ChatDataResponse{Chat: ChatDetail{Messages: ChatMessages{Items: []ChatMessage{{ID: 1, Type: "SIMPLE", CreationTime: ts, WorkflowTransition: &WorkflowTransition{ApplicantState: "RESPONSE"}}}}}}
	r := hhChatRecord(ChatListItem{Id: 2}, &data, &ChatsResponse{})
	if !r.Messages[0].SystemEvent || r.Messages[0].ContentUnavailable || r.Messages[0].Sender != "system" {
		t.Fatal("explicit workflow not recognized")
	}
}
