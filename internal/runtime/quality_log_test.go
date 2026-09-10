package runtime

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStage26DashboardFeedbackActionsAreLocalOnly(t *testing.T) {
	s := dashboardTestServer(t)
	_, conversation := dashboardFixture(t, s)
	wrong := `{"correct":false,"corrected_state":"NO_REPLY_NEEDED"}`
	if response := dashboardRequest(s, "POST", "/api/conversations/"+conversation.ID+"/classification-feedback", wrong); response.Code != 200 {
		t.Fatalf("classification feedback status=%d body=%s", response.Code, response.Body.String())
	}
	draft, err := s.Drafts.Create(AIDraft{ID: "stage26-draft", Type: AIDraftEmployerReply, ConversationID: conversation.ID, InputMessageID: "employer", Text: "Здравствуйте!", Status: AIDraftGenerated, DecisionReason: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Drafts.Save(); err != nil {
		t.Fatal(err)
	}
	if response := dashboardRequest(s, "POST", "/api/drafts/"+draft.ID+"/feedback", `{"accepted":false,"edited_text":"Добрый день!","reason_category":"плохая формулировка"}`); response.Code != 200 {
		t.Fatalf("draft feedback status=%d body=%s", response.Code, response.Body.String())
	}
	report := BuildQualityReport(s.QualityLog.List())
	if report.ClassificationIncorrect != 1 || report.DraftsEdited != 1 {
		t.Fatalf("unexpected quality report: %+v", report)
	}
	if s.Notifications != nil && len(s.Notifications.List()) > 0 {
		t.Fatal("feedback unexpectedly touched HH notifications")
	}
}

func TestQualityLogRecordsFeedbackWithoutConversationBodies(t *testing.T) {
	path := filepath.Join(t.TempDir(), QualityLogFilename)
	store := NewQualityLogStore(path)
	if err := store.Record(QualityLogEvent{EventType: "classification", ObservationKey: "classification/c1/m1/NEEDS_REPLY", ConversationID: "c1", EmployerMessageID: "m1", Timestamp: time.Now().UTC(), WorkflowState: WorkflowNeedsReply, ReplyPolicy: ReplyRequired, CandidateContextStatus: "ready", DraftGenerated: true, NotificationCreated: true, DecisionReasonCodes: []string{"state:NEEDS_REPLY"}}); err != nil {
		t.Fatal(err)
	}
	correct := false
	if err := store.Record(QualityLogEvent{EventType: "classification", ConversationID: "c1", EmployerMessageID: "m1", Timestamp: time.Now().UTC(), WorkflowState: WorkflowNeedsReply, ClassificationCorrect: &correct, CorrectedState: "NO_REPLY_NEEDED"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(QualityLogEvent{EventType: "draft_feedback", ConversationID: "c1", DraftID: "d1", Timestamp: time.Now().UTC(), DraftEdited: true, OriginalDraft: "Здравствуйте!", EditedDraft: "Добрый день!", DraftReasonCategory: "звучит как AI"}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewQualityLogStore(path)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	report := BuildQualityReport(reloaded.List())
	if report.TotalReviewed != 1 || report.ClassificationIncorrect != 1 || report.DraftsEdited != 1 || report.TopDraftProblems[0].Value != "звучит как AI" {
		t.Fatalf("unexpected report: %+v", report)
	}
	raw := strings.Join([]string{reloaded.List()[0].OriginalDraft, reloaded.List()[0].EditedDraft}, " ")
	if strings.Contains(raw, "employer message") {
		t.Fatal("quality log unexpectedly contains a conversation body")
	}
}

func TestQualityLogObservationDeduplication(t *testing.T) {
	store := NewQualityLogStore("")
	event := QualityLogEvent{EventType: "classification", ObservationKey: "same", ConversationID: "c1", Timestamp: time.Now().UTC()}
	if err := store.Record(event); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(event); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 1 {
		t.Fatalf("duplicate observation was recorded: %+v", store.List())
	}
}
