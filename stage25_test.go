package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStage25NotificationLifecyclePriorityDedupAndAutoResolve(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	c := workflowTestConversation(t, "stage25-chat", "Есть ли опыт с Django?", ConversationSenderEmployer, "")
	c.Messages[0].Timestamp = now.Add(-time.Hour)
	c.refreshActivity()
	storePath := filepath.Join(t.TempDir(), NotificationEventsFilename)
	store := NewNotificationStore(storePath)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	engine := NewCandidateNotificationEngine(store, nil, nil, nil, DefaultFollowUpPolicy(), time.Minute)
	snapshot := CareerSnapshot{Conversations: []EmployerConversation{c}}
	if _, err := engine.Calculate(snapshot, now); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 1 || store.List()[0].Priority != NotificationPriorityHigh || store.List()[0].Lifecycle != NotificationNew {
		t.Fatalf("unexpected initial notification: %+v", store.List())
	}
	if _, err := engine.Calculate(snapshot, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 1 || engine.LastDeduplicated == 0 {
		t.Fatalf("restart/sync duplicate was not suppressed: %+v dedup=%d", store.List(), engine.LastDeduplicated)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewNotificationStore(storePath)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCandidateNotificationEngine(reloaded, nil, nil, nil, DefaultFollowUpPolicy(), time.Minute).Calculate(snapshot, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 {
		t.Fatalf("persisted fingerprint did not deduplicate: %+v", reloaded.List())
	}

	c.Messages = append(c.Messages, ConversationMessage{ID: "candidate-1", Timestamp: now.Add(2 * time.Hour), Sender: ConversationSenderCandidate, Direction: ConversationOutgoing, Source: ConversationSourceHH, Text: "Да, использовал Django."})
	c.refreshActivity()
	if _, err := engine.Calculate(CareerSnapshot{Conversations: []EmployerConversation{c}}, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if reloaded := store.List()[0]; reloaded.Lifecycle != NotificationResolved || reloaded.ResolvedAt == nil {
		t.Fatalf("candidate reply did not resolve notification: %+v", reloaded)
	}

	info := workflowTestConversation(t, "stage25-info", "Спасибо за отклик, рассмотрим резюме", ConversationSenderEmployer, "")
	if _, err := engine.Calculate(CareerSnapshot{Conversations: []EmployerConversation{info}}, now); err != nil {
		t.Fatal(err)
	}
	for _, n := range store.List() {
		if n.RelatedConversationID == info.ID && n.Type == NotificationNewEmployerMessage {
			t.Fatalf("courtesy message created notification: %+v", n)
		}
	}
}

func TestStage25TodayViewExcludesResolvedAndBuildsDeterministicSummary(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	needsReply := workflowTestConversation(t, "today-reply", "Есть опыт с Python?", ConversationSenderEmployer, "")
	needsAction := workflowTestConversation(t, "today-action", "Заполните анкету", ConversationSenderEmployer, "")
	interview := workflowTestConversation(t, "today-interview", "Приглашаем на собеседование завтра", ConversationSenderEmployer, "")
	items := []CandidateInboxItem{
		{Conversation: needsReply, Workflow: CareerWorkflowProjection{State: WorkflowNeedsReply, FollowUp: &WorkflowFollowUpDetails{}}},
		{Conversation: needsAction, Workflow: CareerWorkflowProjection{State: WorkflowNeedsUserAction, FollowUp: &WorkflowFollowUpDetails{}}},
		{Conversation: interview, Workflow: CareerWorkflowProjection{State: WorkflowInterview, FollowUp: &WorkflowFollowUpDetails{}}},
	}
	notifications := []CandidateNotification{
		{ID: "active", Type: NotificationInterviewDetected, Lifecycle: NotificationNew, Priority: NotificationPriorityHigh, CreatedAt: now},
		{ID: "resolved", Type: NotificationNewEmployerMessage, Lifecycle: NotificationResolved, Priority: NotificationPriorityHigh, CreatedAt: now},
	}
	view := buildTodayView(CandidateInbox{Items: items}, notifications, DailyRefreshState{LastRefreshAt: now}, now)
	if len(view.NeedsReply) != 1 || len(view.NeedsAction) != 1 || len(view.Interviews) != 1 || len(view.Notifications) != 1 {
		t.Fatalf("today buckets incorrect: %+v", view)
	}
	if view.Summary.Text != "Сегодня: 1 нужно ответить, 1 действия, 0 follow-up, 1 интервью." {
		t.Fatalf("unexpected summary: %q", view.Summary.Text)
	}
}

func TestStage25NotificationLifecycleEndpoints(t *testing.T) {
	s := dashboardTestServer(t)
	now := time.Now().UTC()
	s.Notifications.notifications = append(s.Notifications.notifications, CandidateNotification{ID: "n1", Type: NotificationStatusChanged, Fingerprint: "status-change/test", Message: "changed", CreatedAt: now, Priority: NotificationPriorityLow, Lifecycle: NotificationNew})
	if w := dashboardRequest(s, "POST", "/api/notifications/n1/seen", "{}"); w.Code != 200 {
		t.Fatalf("seen status=%d", w.Code)
	}
	if s.Notifications.notifications[0].Lifecycle != NotificationSeen {
		t.Fatal("seen lifecycle not persisted in memory")
	}
	if w := dashboardRequest(s, "POST", "/api/notifications/n1/snooze", `{"until":"2099-01-01T00:00:00Z"}`); w.Code != 200 {
		t.Fatalf("snooze status=%d", w.Code)
	}
	if s.Notifications.notifications[0].Lifecycle != NotificationSnoozed {
		t.Fatal("snoozed lifecycle not persisted in memory")
	}
	if w := dashboardRequest(s, "POST", "/api/notifications/n1/resolve", "{}"); w.Code != 200 {
		t.Fatalf("resolve status=%d", w.Code)
	}
	if s.Notifications.notifications[0].Lifecycle != NotificationResolved {
		t.Fatal("resolved lifecycle not persisted in memory")
	}
}

func TestStage25LocalDataset261HasNoNotificationSpam(t *testing.T) {
	if _, err := os.Stat("employer_conversations.json"); err != nil {
		t.Skip("private local 261-conversation dataset is not present")
	}
	s, err := loadDashboard(context.Background(), ".", Config{DryRun: true, AIBaseURL: "http://127.0.0.1:1", AITimeout: time.Second, AIConnectTimeout: time.Second, AIAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Conversations.conversations); got != 261 {
		t.Skipf("Stage 25 dataset check expects 261 conversations, got %d", got)
	}
	store := NewNotificationStore("")
	store.notifications = append([]CandidateNotification{}, s.Notifications.notifications...)
	store.workflowStates = map[string]string{}
	engine := NewCandidateNotificationEngine(store, s.Applications, s.Conversations, s.Clarifications, s.FollowUpPolicy, time.Minute)
	snapshot := s.careerSnapshotLocal()
	if _, err := engine.Calculate(snapshot, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	fingerprints := map[string]bool{}
	for _, n := range store.notifications {
		if fingerprints[n.Fingerprint] {
			t.Fatalf("duplicate notification fingerprint: %s", n.Fingerprint)
		}
		fingerprints[n.Fingerprint] = true
	}
	inbox, err := s.inbox()
	if err != nil {
		t.Fatal(err)
	}
	if inbox.ImportantCount < 0 || inbox.ImportantCount > len(inbox.Items) {
		t.Fatalf("invalid actionable counter: %d/%d", inbox.ImportantCount, len(inbox.Items))
	}
	terminal := 0
	for _, item := range inbox.Items {
		if item.Workflow.State == WorkflowTerminal {
			terminal++
		}
	}
	if inbox.ImportantCount > len(inbox.Items)-terminal {
		t.Fatalf("terminal conversations leaked into actionable counter: important=%d terminal=%d items=%d", inbox.ImportantCount, terminal, len(inbox.Items))
	}
	start := time.Now()
	if response := dashboardRequest(s, "GET", "/api/inbox", ""); response.Code != 200 {
		t.Fatalf("inbox status=%d", response.Code)
	} else {
		t.Logf("cold inbox=%s", time.Since(start))
	}
	start = time.Now()
	if response := dashboardRequest(s, "GET", "/api/today", ""); response.Code != 200 {
		t.Fatalf("today status=%d: %s", response.Code, response.Body.String())
	} else {
		t.Logf("cold today=%s", time.Since(start))
	}
	todayView, err := s.today()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("TODAY needs_reply=%d needs_action=%d interviews=%d follow_ups=%d new_changes=%d", len(todayView.NeedsReply), len(todayView.NeedsAction), len(todayView.Interviews), len(todayView.FollowUps), len(todayView.NewChanges))
	t.Logf("conversations=%d notifications=%d active=%d created=%d resolved=%d deduplicated=%d snoozed=%d", len(snapshot.Conversations), len(store.notifications), inbox.ImportantCount, engine.LastCreated, countNotificationLifecycle(store.notifications, NotificationResolved), engine.LastDeduplicated, countNotificationLifecycle(s.Notifications.notifications, NotificationSnoozed))
}

func countNotificationLifecycle(values []CandidateNotification, lifecycle NotificationLifecycle) int {
	count := 0
	for _, value := range values {
		if value.Lifecycle == lifecycle {
			count++
		}
	}
	return count
}
