package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

func TestQualityLogRecordManyPersistsAllNewEventsOnceAndReloads(t *testing.T) {
	store, saves := countingQualityLogStore(t)
	events := qualityNotificationEvents(100)
	if err := store.RecordMany(events); err != nil {
		t.Fatal(err)
	}
	if *saves != 1 {
		t.Fatalf("expected one durable save for batch, got %d", *saves)
	}
	assertQualityNotificationEvents(t, store.List(), events)

	reloaded := NewQualityLogStore(store.path)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	assertQualityNotificationEvents(t, reloaded.List(), events)
}

func TestQualityLogRecordManyRemovesPerObservationSaveAmplification(t *testing.T) {
	events := qualityNotificationEvents(100)
	legacy, legacySaves := countingQualityLogStore(t)
	for _, event := range events {
		if err := legacy.Record(event); err != nil {
			t.Fatal(err)
		}
	}
	if *legacySaves != len(events) {
		t.Fatalf("single-record path saved %d times for %d events", *legacySaves, len(events))
	}

	batched, batchSaves := countingQualityLogStore(t)
	if err := batched.RecordMany(events); err != nil {
		t.Fatal(err)
	}
	if *batchSaves != 1 {
		t.Fatalf("batch path saved %d times for %d events", *batchSaves, len(events))
	}
}

func TestQualityLogRecordManySkipsAllDuplicatesWithoutSaving(t *testing.T) {
	store, saves := countingQualityLogStore(t)
	events := qualityNotificationEvents(100)
	if err := store.RecordMany(events); err != nil {
		t.Fatal(err)
	}
	*saves = 0
	beforeEvents := store.List()
	beforeRaw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordMany(events); err != nil {
		t.Fatal(err)
	}
	if *saves != 0 || !reflect.DeepEqual(store.List(), beforeEvents) {
		t.Fatalf("duplicates changed store: saves=%d events=%d", *saves, len(store.List()))
	}
	afterRaw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRaw, beforeRaw) {
		t.Fatal("duplicate batch changed the quality-log file")
	}
}

func TestQualityLogRecordManyMixedBatchPersistsNewEventsOnceInOrder(t *testing.T) {
	store, saves := countingQualityLogStore(t)
	initial := qualityNotificationEvents(50)
	if err := store.RecordMany(initial); err != nil {
		t.Fatal(err)
	}
	*saves = 0
	mixed := append(append([]QualityLogEvent{}, initial[25:]...), qualityNotificationEventsFrom(100, 75)...)
	if err := store.RecordMany(mixed); err != nil {
		t.Fatal(err)
	}
	if *saves != 1 {
		t.Fatalf("expected one durable save for mixed batch, got %d", *saves)
	}
	expected := append(append([]QualityLogEvent{}, initial...), qualityNotificationEventsFrom(100, 75)...)
	assertQualityNotificationEvents(t, store.List(), expected)
}

func TestQualityLogRecordManySingleAndEmptyBatches(t *testing.T) {
	store, saves := countingQualityLogStore(t)
	event := qualityNotificationEvents(1)[0]
	if err := store.Record(event); err != nil {
		t.Fatal(err)
	}
	if *saves != 1 || len(store.List()) != 1 {
		t.Fatalf("single record mismatch: saves=%d events=%d", *saves, len(store.List()))
	}
	before := store.List()
	if err := store.RecordMany(nil); err != nil {
		t.Fatal(err)
	}
	if *saves != 1 || !reflect.DeepEqual(store.List(), before) {
		t.Fatal("empty batch changed quality log")
	}
}

func TestQualityLogRecordManySaveFailureDoesNotPublishPartialBatch(t *testing.T) {
	store := NewQualityLogStore(filepath.Join(t.TempDir(), QualityLogFilename))
	wantErr := errors.New("fixture persistence failure")
	store.persist = func(string, []byte) error { return wantErr }
	if err := store.RecordMany(qualityNotificationEvents(100)); !errors.Is(err, wantErr) {
		t.Fatalf("expected persistence error, got %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("failed batch was published in memory: %d events", len(store.List()))
	}
	if _, err := os.Stat(store.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed batch left a quality-log file: %v", err)
	}
}

func TestNotificationsEndpointBatchesShownFeedbackAndPreservesProjection(t *testing.T) {
	s := dashboardTestServer(t)
	qualityLog, saves := countingQualityLogStore(t)
	s.QualityLog = qualityLog
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	s.Notifications.notifications = make([]CandidateNotification, 0, 100)
	for i := 0; i < 100; i++ {
		s.Notifications.notifications = append(s.Notifications.notifications, CandidateNotification{
			ID: fmt.Sprintf("n-%03d", i), Type: NotificationStatusChanged, Fingerprint: fmt.Sprintf("fixture-%03d", i),
			Message: "fixture", CreatedAt: now.Add(time.Duration(i) * time.Second), Priority: NotificationPriorityMedium, Lifecycle: NotificationNew,
		})
	}
	first := dashboardDecode[struct {
		Notifications []CandidateNotification `json:"notifications"`
		Unread        int                     `json:"unread"`
	}](t, dashboardRequest(s, "GET", "/api/notifications", ""))
	if len(first.Notifications) != 100 || first.Unread != 100 || *saves != 1 {
		t.Fatalf("unexpected first projection/save count: notifications=%d unread=%d saves=%d", len(first.Notifications), first.Unread, *saves)
	}
	*saves = 0
	second := dashboardDecode[struct {
		Notifications []CandidateNotification `json:"notifications"`
		Unread        int                     `json:"unread"`
	}](t, dashboardRequest(s, "GET", "/api/notifications", ""))
	if !reflect.DeepEqual(first, second) || *saves != 0 {
		t.Fatalf("repeated GET changed projection or saved duplicates: equal=%v saves=%d", reflect.DeepEqual(first, second), *saves)
	}
}

func countingQualityLogStore(t *testing.T) (*QualityLogStore, *int) {
	t.Helper()
	store := NewQualityLogStore(filepath.Join(t.TempDir(), QualityLogFilename))
	saves := 0
	store.persist = func(path string, raw []byte) error {
		saves++
		return persistQualityLogFile(path, raw)
	}
	return store, &saves
}

func qualityNotificationEvents(count int) []QualityLogEvent {
	return qualityNotificationEventsFrom(0, count)
}

func qualityNotificationEventsFrom(start, count int) []QualityLogEvent {
	events := make([]QualityLogEvent, 0, count)
	for i := start; i < start+count; i++ {
		events = append(events, QualityLogEvent{ObservationKey: "notification/shown/n-" + strconv.Itoa(i), EventType: "notification_feedback", NotificationID: "n-" + strconv.Itoa(i), Timestamp: time.Date(2026, 9, 11, 0, 0, i, 0, time.UTC), NotificationEvent: "shown"})
	}
	return events
}

func assertQualityNotificationEvents(t *testing.T, got, want []QualityLogEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ObservationKey != want[i].ObservationKey || got[i].NotificationID != want[i].NotificationID {
			t.Fatalf("event %d=%+v, want %+v", i, got[i], want[i])
		}
	}
}
