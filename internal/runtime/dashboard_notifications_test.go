package runtime

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestNotificationSnapshotPreservesEngineInputs(t *testing.T) {
	s := dashboardTestServer(t)
	dashboardFixture(t, s)

	legacy := s.careerSnapshotLocal()
	optimized := s.loadNotificationSnapshot()
	if !reflect.DeepEqual(legacy.Applications, optimized.Applications) {
		t.Fatal("notification snapshot changed applications")
	}
	if !reflect.DeepEqual(legacy.Events, optimized.Events) {
		t.Fatal("notification snapshot changed application events")
	}
	if !reflect.DeepEqual(legacy.Conversations, optimized.Conversations) {
		t.Fatal("notification snapshot changed conversation history")
	}
	if !reflect.DeepEqual(legacy.Clarifications, optimized.Clarifications) {
		t.Fatal("notification snapshot changed clarifications")
	}
	if len(optimized.Vacancies) != 0 || len(optimized.Drafts) != 0 || !reflect.DeepEqual(optimized.Sync, HHSyncState{}) {
		t.Fatalf("notification snapshot loaded unused collections: vacancies=%d drafts=%d sync=%+v", len(optimized.Vacancies), len(optimized.Drafts), optimized.Sync)
	}
}

func TestNotificationCalculationHasLogicalParityForReadModel(t *testing.T) {
	s := dashboardTestServer(t)
	dashboardFixture(t, s)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	legacy := s.careerSnapshotLocal()
	optimized := s.loadNotificationSnapshot()

	seed := NewNotificationStore("")
	seedEngine := NewCandidateNotificationEngine(seed, nil, nil, nil, s.FollowUpPolicy, 15*time.Minute)
	if _, err := seedEngine.Calculate(legacy, now); err != nil {
		t.Fatal(err)
	}
	clone := func() *NotificationStore {
		store := NewNotificationStore("")
		store.notifications = append([]CandidateNotification(nil), seed.notifications...)
		store.workflowStates = map[string]string{}
		for key, value := range seed.workflowStates {
			store.workflowStates[key] = value
		}
		return store
	}
	legacyStore, optimizedStore := clone(), clone()
	legacyEngine := NewCandidateNotificationEngine(legacyStore, nil, nil, nil, s.FollowUpPolicy, 15*time.Minute)
	optimizedEngine := NewCandidateNotificationEngine(optimizedStore, nil, nil, nil, s.FollowUpPolicy, 15*time.Minute)
	legacyResult, err := legacyEngine.Calculate(legacy, now)
	if err != nil {
		t.Fatal(err)
	}
	optimizedResult, err := optimizedEngine.Calculate(optimized, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacyResult, optimizedResult) {
		t.Fatalf("notification logical projection changed:\nlegacy=%+v\noptimized=%+v", legacyResult, optimizedResult)
	}
}

func TestNotificationsOverviewIsBoundedAndPreservesFullOrdering(t *testing.T) {
	s := dashboardTestServer(t)
	s.QualityLog = NewQualityLogStore("")
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	priorities := []NotificationPriority{
		NotificationPriorityLow, NotificationPriorityMedium, NotificationPriorityHigh,
		NotificationPriorityCritical, NotificationPriorityLow, NotificationPriorityMedium,
		NotificationPriorityHigh, NotificationPriorityCritical, NotificationPriorityLow,
		NotificationPriorityMedium, NotificationPriorityHigh, NotificationPriorityLow,
	}
	s.Notifications.notifications = make([]CandidateNotification, 0, len(priorities))
	for i, priority := range priorities {
		s.Notifications.notifications = append(s.Notifications.notifications, CandidateNotification{
			ID: notificationFixtureID(i), Type: NotificationStatusChanged, RelatedApplicationID: "application-" + notificationFixtureID(i),
			RelatedConversationID: "conversation-" + notificationFixtureID(i), Fingerprint: "fixture-" + notificationFixtureID(i),
			Message: "fixture", CreatedAt: now.Add(time.Duration(i) * time.Minute), Priority: priority, Lifecycle: NotificationNew,
		})
	}

	full := dashboardDecode[struct {
		Notifications []CandidateNotification `json:"notifications"`
		Unread        int                     `json:"unread"`
	}](t, dashboardRequest(s, "GET", "/api/notifications", ""))
	overview := dashboardDecode[struct {
		Notifications []notificationOverviewItem `json:"notifications"`
		Unread        int                        `json:"unread"`
	}](t, dashboardRequest(s, "GET", "/api/notifications/overview", ""))
	if len(full.Notifications) != len(priorities) || full.Unread != len(priorities) {
		t.Fatalf("full notification contract changed: count=%d unread=%d", len(full.Notifications), full.Unread)
	}
	if len(overview.Notifications) != notificationOverviewLimit || overview.Unread != len(priorities) {
		t.Fatalf("overview bound/unread mismatch: count=%d unread=%d", len(overview.Notifications), overview.Unread)
	}
	for i, item := range overview.Notifications {
		want := projectNotificationOverview(full.Notifications[i])
		if !reflect.DeepEqual(item, want) {
			t.Fatalf("overview item %d=%+v, want %+v", i, item, want)
		}
	}
	if full.Notifications[0].Priority != NotificationPriorityCritical || full.Notifications[1].Priority != NotificationPriorityCritical {
		t.Fatalf("global priority ordering was not preserved: %+v", full.Notifications[:2])
	}
	if len(s.QualityLog.List()) != len(priorities) {
		t.Fatalf("shown feedback changed with bounded projection: got %d events", len(s.QualityLog.List()))
	}
	if full.Notifications[0].Fingerprint == "" || full.Notifications[0].RelatedApplicationID == "" {
		t.Fatal("full notification fields were removed")
	}
}

func TestNotificationReadGroupsStayBounded(t *testing.T) {
	s := dashboardTestServer(t)
	before := perfSnapshot()
	if err := s.refreshNotifications(time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	after := perfSnapshot()
	for _, operation := range []string{
		"dashboard.notifications.read.applications",
		"dashboard.notifications.read.application_events",
		"dashboard.notifications.read.conversations",
		"dashboard.notifications.read.clarifications",
	} {
		if after[operation].Calls-before[operation].Calls != 1 {
			t.Fatalf("%s calls=%d, want 1", operation, after[operation].Calls-before[operation].Calls)
		}
	}
	for _, operation := range []string{"dashboard.notifications.read.vacancies", "dashboard.notifications.read.drafts"} {
		if after[operation].Calls != before[operation].Calls {
			t.Fatalf("unused notification read group %s was called", operation)
		}
	}
}

func TestNotificationsOverviewSupportsEmptyAndBelowLimit(t *testing.T) {
	for _, count := range []int{0, 3, notificationOverviewLimit} {
		t.Run("count="+strconv.Itoa(count), func(t *testing.T) {
			s := dashboardTestServer(t)
			s.QualityLog = NewQualityLogStore("")
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			for i := 0; i < count; i++ {
				s.Notifications.notifications = append(s.Notifications.notifications, CandidateNotification{
					ID: notificationFixtureID(i), Type: NotificationStatusChanged, Fingerprint: "fixture-" + notificationFixtureID(i),
					Message: "fixture", CreatedAt: now.Add(time.Duration(i) * time.Minute), Priority: NotificationPriorityMedium, Lifecycle: NotificationNew,
				})
			}
			got := dashboardDecode[struct {
				Notifications []notificationOverviewItem `json:"notifications"`
				Unread        int                        `json:"unread"`
			}](t, dashboardRequest(s, "GET", "/api/notifications/overview", ""))
			if len(got.Notifications) != count || got.Unread != count {
				t.Fatalf("count=%d unread=%d, want %d", len(got.Notifications), got.Unread, count)
			}
		})
	}
}

func notificationFixtureID(index int) string {
	return strconv.Itoa(index)
}
